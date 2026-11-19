package cloudflare

import (
	"cloudforge/core/internal/persistence"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAccessTCPRunsAndRestoresMultipleConnections(t *testing.T) {
	cli := os.Getenv("CLOUDFORGE_TEST_CLOUDFLARED")
	if cli == "" {
		t.Skip("defina CLOUDFORGE_TEST_CLOUDFLARED com o executável oficial")
	}
	dir := t.TempDir()
	store, err := persistence.Open(filepath.Join(dir, "cloudforge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	newManager := func() *AccessTCP {
		tcp := New(store, dir, "http://127.0.0.1:28932", "test").AccessTCP
		tcp.binary = func() (string, error) { return cli, nil }
		return tcp
	}
	manager := newManager()
	defer manager.Close()

	reserve := func() *net.TCPListener {
		t.Helper()
		listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = listener.Close() })
		return listener
	}
	first, second := reserve(), reserve()
	firstPort, secondPort := first.Addr().(*net.TCPAddr).Port, second.Addr().(*net.TCPAddr).Port
	if err = manager.Add("https://postgres.example.com/", firstPort); err == nil {
		t.Fatal("a conexão deveria recusar uma URL")
	}
	if err = manager.Add("postgres.example.com", firstPort); err == nil {
		t.Fatal("a conexão deveria recusar uma porta ocupada")
	}
	_ = first.Close()
	_ = second.Close()
	if err = manager.Add("postgres.example.com", firstPort); err != nil {
		t.Fatal(err)
	}
	if err = manager.Add("redis.example.com", secondPort); err != nil {
		t.Fatal(err)
	}
	if err = manager.Add("mysql.example.com", firstPort); err == nil {
		t.Fatal("a conexão deveria recusar uma porta já registrada")
	}

	wait := func(tcp *AccessTCP, first, second string) {
		t.Helper()
		deadline := time.Now().Add(12 * time.Second)
		for time.Now().Before(deadline) {
			list := tcp.List()
			if len(list) == 2 && list[0].Status == first && list[1].Status == second {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("status esperados %s e %s, recebidos %+v", first, second, tcp.List())
	}
	wait(manager, "listening", "listening")
	if err = manager.Change(manager.List()[0].ID, "stop"); err != nil {
		t.Fatal(err)
	}
	manager.Close()
	if !accessTCPPortFree(firstPort) || !accessTCPPortFree(secondPort) {
		t.Fatal("as portas deveriam ser liberadas no encerramento")
	}
	reopened := newManager()
	defer reopened.Close()
	reopened.Restore()
	wait(reopened, "stopped", "listening")
	if !accessTCPPortFree(firstPort) {
		t.Fatal("a conexão pausada foi restaurada")
	}
	if err = reopened.Change(reopened.List()[0].ID, "start"); err != nil {
		t.Fatal(err)
	}
	wait(reopened, "listening", "listening")
	if err = reopened.Change(reopened.List()[0].ID, "delete"); err != nil {
		t.Fatal(err)
	}
	if list := reopened.List(); len(list) != 1 || list[0].Port != secondPort {
		t.Fatalf("a exclusão alterou outra conexão: %+v", list)
	}
}
