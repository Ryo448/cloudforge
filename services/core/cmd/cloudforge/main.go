package main

import (
	"cloudforge/core/internal/api"
	"cloudforge/core/internal/auth"
	"cloudforge/core/internal/autostart"
	"cloudforge/core/internal/cloudflare"
	"cloudforge/core/internal/databases"
	"cloudforge/core/internal/logging"
	"cloudforge/core/internal/persistence"
	"cloudforge/core/internal/processes"
	"cloudforge/core/internal/routing"
	"cloudforge/core/internal/snapshots"
	"cloudforge/core/internal/supervisor"
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"github.com/gofrs/flock"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "--postgres-lan-firewall" {
		if err := databases.ApplyLANFirewallHelper(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--pty-child" {
		if err := processes.RunPTYChild(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if e := run(); e != nil {
		log.Print(e)
		os.Exit(1)
	}
}
func run() error {
	configDir, e := os.UserConfigDir()
	if e != nil {
		return e
	}
	data := flag.String("data", filepath.Join(configDir, "Cloudforge"), "diretório de dados")
	web := flag.String("web-dir", "apps/web/dist", "dashboard compilada")
	adminAddr := flag.String("admin", "127.0.0.1:28931", "API administrativa loopback")
	publicAddr := flag.String("public", "127.0.0.1:28932", "roteador do túnel loopback")
	development := flag.Bool("development", false, "permite dashboard HTTP somente em loopback para desenvolvimento")
	flag.Parse()
	for _, address := range []string{*adminAddr, *publicAddr} {
		host, _, e := net.SplitHostPort(address)
		if e != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return errors.New("listeners devem usar IP loopback")
		}
	}
	*data, e = filepath.Abs(*data)
	if e != nil {
		return e
	}
	*web, e = filepath.Abs(*web)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(*data, 0700); e != nil {
		return e
	}
	log.SetOutput(logging.New(filepath.Join(*data, "core.log")))
	lock := flock.New(filepath.Join(*data, "core.lock"))
	locked, e := lock.TryLock()
	if e != nil {
		return e
	}
	if !locked {
		return errors.New("Core já está em execução neste diretório de dados")
	}
	defer lock.Unlock()
	keyPath := filepath.Join(*data, "admin.key")
	keyBytes, e := os.ReadFile(keyPath)
	if os.IsNotExist(e) {
		keyBytes = []byte(auth.Token())
		e = os.WriteFile(keyPath, keyBytes, 0600)
	}
	if e != nil {
		return e
	}
	key := strings.TrimSpace(string(keyBytes))
	if len(key) < 32 {
		return errors.New("chave administrativa local inválida")
	}
	s, e := persistence.Open(filepath.Join(*data, "cloudforge.db"))
	if e != nil {
		return e
	}
	defer s.Close()
	var installation string
	if s.Setting("installationId", &installation) != nil {
		installation = auth.ID()
		if e = s.Set("installationId", installation); e != nil {
			return e
		}
	}
	sup := supervisor.New(s, *data)
	cf := cloudflare.New(s, *data, "http://"+*publicAddr, installation)
	server := api.New(s, sup, cf, key, *data, *web, *development)
	binary, e := os.Executable()
	if e != nil {
		return e
	}
	file, e := os.Open(binary)
	if e != nil {
		return e
	}
	hash := sha256.New()
	_, e = io.Copy(hash, file)
	file.Close()
	if e != nil {
		return e
	}
	server.BinarySHA256 = fmt.Sprintf("%x", hash.Sum(nil))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	server.Shutdown = cancel
	server.Databases = databases.New(s, key)
	cf.OpenDatabaseRelay = server.Databases.OpenDatabaseTunnel
	server.Databases.RemoveDatabaseTunnel = cf.DeleteDatabaseTunnel
	server.Autostart = func(enabled bool) error { return autostart.Configure(enabled, *data, *web) }
	server.AutostartEnabled = autostart.Enabled
	adminListener, e := net.Listen("tcp", *adminAddr)
	if e != nil {
		return fmt.Errorf("porta administrativa ocupada: %w", e)
	}
	defer adminListener.Close()
	publicListener, e := net.Listen("tcp", *publicAddr)
	if e != nil {
		return fmt.Errorf("porta do roteador ocupada: %w", e)
	}
	defer publicListener.Close()
	if e = sup.Restore(); e != nil {
		return e
	}
	defer sup.Shutdown()
	defer cf.Stop()
	cf.Start()
	defer cf.AccessTCP.Close()
	cf.AccessTCP.Restore()
	dbDone := make(chan struct{})
	go func() { defer close(dbDone); server.Databases.Run(ctx) }()
	defer func() { cancel(); <-dbDone }()
	snapshotService, snapshotErr := snapshots.New(ctx, s, server.Databases, installation, *data)
	if snapshotErr != nil {
		log.Printf("snapshots: serviço indisponível: %v", snapshotErr)
	} else {
		server.Snapshots = snapshotService
		defer snapshotService.Close()
	}
	go server.Monitor(ctx)
	if server.Cluster != nil {
		server.Cluster.Start(ctx)
		defer server.Cluster.Stop()
	}
	go func() {
		repairCtx, stop := context.WithTimeout(ctx, 60*time.Second)
		defer stop()
		if e := cf.Recover(repairCtx); e != nil {
			log.Print(e)
		}
	}()
	admin := &http.Server{Handler: http.HandlerFunc(server.Local), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 * 1024}
	public := &http.Server{Handler: &routing.Router{Store: s, Cloud: cf, Supervisor: sup, Dashboard: http.HandlerFunc(server.Public), Development: *development}, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 * 1024}
	errch := make(chan error, 2)
	go func() { errch <- admin.Serve(adminListener) }()
	go func() { errch <- public.Serve(publicListener) }()
	log.Printf("Cloudforge Core: administração %s; roteador %s", *adminAddr, *publicAddr)
	select {
	case <-ctx.Done():
	case e = <-errch:
		if !errors.Is(e, http.ErrServerClosed) {
			return e
		}
	}
	shutdown, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	_ = admin.Shutdown(shutdown)
	_ = public.Shutdown(shutdown)
	return nil
}
