package cloudflare

import (
	"cloudforge/core/internal/auth"
	"cloudforge/core/internal/persistence"
	"cloudforge/core/internal/processes"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const accessTCPSetting = "cloudflare_access_tcp"

type AccessTCPConnection struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	Port     int    `json:"port"`
	Enabled  bool   `json:"enabled"`
	Status   string `json:"status,omitempty"`
	Issue    string `json:"issue,omitempty"`
}

type accessTCPRun struct {
	cancel context.CancelFunc
	done   chan struct{}
	status string
	issue  string
}

type AccessTCP struct {
	mu          sync.Mutex
	store       persistence.Repository
	binary      func() (string, error)
	install     func(context.Context) error
	connections []AccessTCPConnection
	running     map[string]*accessTCPRun
	closed      bool
}

func newAccessTCP(m *Manager) *AccessTCP {
	tcp := &AccessTCP{store: m.Store, binary: m.Binary, install: m.Install, running: make(map[string]*accessTCPRun)}
	var saved []AccessTCPConnection
	if err := m.Store.Setting(accessTCPSetting, &saved); err == nil {
		for _, c := range saved {
			if c.ID != "" && validAccessHostname(c.Hostname) && c.Port > 0 && c.Port <= 65535 &&
				!slices.ContainsFunc(tcp.connections, func(existing AccessTCPConnection) bool {
					return existing.Port == c.Port || existing.ID == c.ID
				}) {
				tcp.connections = append(tcp.connections, c)
			}
		}
	}
	return tcp
}

func validAccessHostname(host string) bool {
	return validDomain(host) && net.ParseIP(host) == nil
}

func (a *AccessTCP) Restore() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	for _, c := range a.connections {
		if c.Enabled && a.running[c.ID] == nil {
			a.start(c)
		}
	}
}

func (a *AccessTCP) Close() {
	a.mu.Lock()
	a.closed = true
	runs := make([]*accessTCPRun, 0, len(a.running))
	for _, run := range a.running {
		runs = append(runs, run)
		run.cancel()
	}
	a.mu.Unlock()
	for _, run := range runs {
		<-run.done
	}
}

func (a *AccessTCP) List() []AccessTCPConnection {
	a.mu.Lock()
	defer a.mu.Unlock()
	list := make([]AccessTCPConnection, 0, len(a.connections))
	for _, c := range a.connections {
		c.Status, c.Issue = "stopped", ""
		if run := a.running[c.ID]; run != nil && c.Enabled {
			c.Status, c.Issue = run.status, run.issue
		}
		list = append(list, c)
	}
	return list
}

func (a *AccessTCP) Add(host string, port int) error {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if !validAccessHostname(host) {
		return errors.New("informe um domínio válido, sem protocolo ou caminho")
	}
	if port < 1 || port > 65535 {
		return errors.New("use uma porta entre 1 e 65535")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return errors.New("serviço de conexões TCP indisponível")
	}
	for _, c := range a.connections {
		if c.Port == port {
			return errors.New("essa porta já pertence a uma conexão TCP cadastrada")
		}
	}
	if !accessTCPPortFree(port) {
		return errors.New("a porta local está ocupada")
	}
	c := AccessTCPConnection{ID: auth.ID(), Hostname: host, Port: port, Enabled: true}
	next := append(slices.Clone(a.connections), c)
	if err := a.store.Set(accessTCPSetting, next); err != nil {
		return err
	}
	a.connections = next
	a.start(c)
	return nil
}

func (a *AccessTCP) Change(id, action string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return errors.New("serviço de conexões TCP indisponível")
	}
	index := slices.IndexFunc(a.connections, func(c AccessTCPConnection) bool { return c.ID == id })
	if index < 0 {
		return errors.New("conexão TCP não encontrada")
	}
	next := slices.Clone(a.connections)
	current := next[index]
	run := a.running[id]
	switch action {
	case "start":
		if current.Enabled {
			return nil
		}
		if run != nil {
			return errors.New("aguarde o encerramento da conexão TCP")
		}
		if !accessTCPPortFree(current.Port) {
			return errors.New("a porta local está ocupada")
		}
		next[index].Enabled = true
	case "stop":
		next[index].Enabled = false
	case "delete":
		next = slices.Delete(next, index, index+1)
	default:
		return errors.New("operação inválida")
	}
	if err := a.store.Set(accessTCPSetting, next); err != nil {
		return err
	}
	a.connections = next
	if action == "start" {
		a.start(next[index])
	} else if run != nil {
		run.cancel()
	}
	return nil
}

func (a *AccessTCP) start(c AccessTCPConnection) {
	ctx, cancel := context.WithCancel(context.Background())
	run := &accessTCPRun{cancel: cancel, done: make(chan struct{}), status: "starting"}
	a.running[c.ID] = run
	go func() {
		defer func() {
			a.mu.Lock()
			delete(a.running, c.ID)
			close(run.done)
			a.mu.Unlock()
		}()
		report := func(status, issue string) {
			a.mu.Lock()
			run.status, run.issue = status, issue
			a.mu.Unlock()
		}
		for delay := time.Second; ctx.Err() == nil; delay = min(delay*2, time.Minute) {
			report("starting", "")
			err := a.run(ctx, c, func() {
				delay = time.Second
				report("listening", "")
			})
			if ctx.Err() != nil {
				return
			}
			report("retrying", err.Error())
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}
	}()
}

func accessTCPAddress(port int) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}

func accessTCPPortFree(port int) bool {
	listener, err := net.Listen("tcp4", accessTCPAddress(port))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

func (a *AccessTCP) run(ctx context.Context, c AccessTCPConnection, listening func()) error {
	if !accessTCPPortFree(c.Port) {
		return errors.New("a porta local está ocupada")
	}
	binary, err := a.binary()
	if err != nil {
		if err = a.install(ctx); err != nil {
			return err
		}
		binary, err = a.binary()
		if err != nil {
			return errors.New("cloudflared indisponível")
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	cmd := exec.Command(binary, "access", "tcp", "--hostname", c.Hostname, "--url", accessTCPAddress(c.Port))
	cmd.Env = slices.DeleteFunc(os.Environ(), func(value string) bool {
		key, _, _ := strings.Cut(value, "=")
		return strings.HasPrefix(strings.ToUpper(key), "TUNNEL_")
	})
	processes.Configure(cmd)
	processes.ConfigureBackground(cmd)
	if err := cmd.Start(); err != nil {
		return errors.New("não foi possível iniciar o cloudflared")
	}
	tree, err := processes.Attach(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return errors.New("não foi possível gerenciar o processo cloudflared")
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	defer func() { tree.Close(); <-done }()
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return errors.New("o cloudflared encerrou a conexão TCP")
		case <-deadline.C:
			return errors.New("o cloudflared não abriu a porta local")
		case <-ticker.C:
			// Do not dial the proxy here: a real connection can trigger a browser login.
			if !accessTCPPortFree(c.Port) {
				listening()
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-done:
					return errors.New("o cloudflared encerrou a conexão TCP")
				}
			}
		}
	}
}
