package cloudflare

import (
	"cloudforge/core/internal/model"
	"cloudforge/core/internal/persistence"
	"cloudforge/core/internal/projects"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestBrowserTunnelRetryAndProjectRoute(t *testing.T) {
	const account = "0123456789abcdef0123456789abcdef"
	const zone = "abcdef0123456789abcdef0123456789"
	const secondZone = "1234567890abcdef1234567890abcdef"
	const id = "12345678-1234-4234-8234-123456789abc"
	dir := t.TempDir()
	store, err := persistence.Open(filepath.Join(dir, "cloudforge.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	m := New(store, dir, "http://127.0.0.1:28932", "test-installation")
	t.Cleanup(m.Stop)
	var mu sync.Mutex
	var deleted []string
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var result any
		switch {
		case r.Method == "GET" && r.URL.Path == "/zones/"+secondZone:
			result = Zone{ID: secondZone, Name: "example.net"}
		case r.Method == "GET" && r.URL.Path == "/zones/"+zone+"/dns_records":
			host := r.URL.Query().Get("name")
			target := id + ".cfargotunnel.com"
			if host == "novo.example.com" {
				target = "outside.example.net"
			}
			result = []DNS{{ID: host, Name: host, Type: "CNAME", Content: target}}
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/zones/"+zone+"/dns_records/"):
			mu.Lock()
			deleted = append(deleted, filepath.Base(r.URL.Path))
			mu.Unlock()
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
	}))
	t.Cleanup(service.Close)
	m.Client = &Client{BaseURL: service.URL, HTTP: service.Client()}
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(certPath(m.localHome()), pem.EncodeToMemory(&pem.Block{Type: "ARGO TUNNEL TOKEN", Bytes: []byte(`{"accountID":"` + account + `","zoneID":"` + zone + `","apiToken":"fixture"}`)}))
	write(filepath.Join(m.localHome(), "selected-zone.json"), []byte(`{"ID":"`+zone+`","Name":"example.com"}`))
	if status := m.BrowserStatus(); !status.Authorized || status.Domain != "example.com" {
		t.Fatalf("authorized domain not restored: %+v", status)
	}
	creates := 0
	m.runLocalCLI = func(_ context.Context, args ...string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "list --name cloudforge-test-installation --output json":
			if creates == 0 {
				return []byte("null\r\n"), nil
			}
			return []byte(`[{"id":"` + id + `","name":"cloudforge-test-installation"}]`), nil
		case "create cloudforge-test-installation":
			creates++
			write(m.credentials(id), []byte("fixture tunnel credential"))
			return []byte("Created tunnel with id " + id), nil
		case "route dns " + id + " ocupado.example.com":
			return nil, errors.New("CNAME exists")
		case "route dns " + id + " painel.example.com":
			return []byte("Added CNAME painel.example.com which will route to this tunnel"), nil
		case "route dns " + id + " app.example.com":
			return []byte("app.example.com is already configured to route to your tunnel"), nil
		case "route dns " + id + " novo.example.com", "route dns " + id + " dashboard.example.net":
			return nil, nil
		default:
			t.Fatalf("unexpected CLI call: %v", args)
			return nil, errors.New("unexpected command")
		}
	}
	ctx := context.Background()
	if _, err := m.ProvisionLocal(ctx, "other.example", "painel"); err == nil || creates != 0 {
		t.Fatal("provisioning accepted a different domain")
	}
	if _, err := m.ProvisionLocal(ctx, "example.com", "ocupado"); err == nil || m.Config().TunnelID != "" {
		t.Fatal("DNS conflict saved a configuration")
	}
	cfg, err := m.ProvisionLocal(ctx, "example.com", "painel")
	if err != nil || cfg.TunnelID != id || cfg.Mode != "local" || creates != 1 {
		t.Fatalf("retry did not reuse the same tunnel: %+v %v", cfg, err)
	}
	project := model.Project{Name: "Project", Runtime: "python", Path: t.TempDir(), EntryPoint: "main.py"}
	write(filepath.Join(project.Path, project.EntryPoint), []byte("print('test')\n"))
	if err = projects.New(&project); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveProject(project); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Route(ctx, project.ID, "app", 28931); err == nil {
		t.Fatal("accepted an administrative port")
	}
	if _, err = m.Route(ctx, project.ID, "ocupado", 3000); err == nil {
		t.Fatal("accepted conflicting DNS")
	}
	route, err := m.Route(ctx, project.ID, "app", 3000)
	if err != nil || route.Hostname != "app.example.com" {
		t.Fatalf("route not saved: %+v %v", route, err)
	}
	stored, err := store.Routes()
	if err != nil || len(stored) != 1 || stored[0].ProjectID != project.ID {
		t.Fatalf("unexpected routes: %+v %v", stored, err)
	}
	if _, err := m.ChangePanel(ctx, "example.com", "app"); err == nil || m.Config().AdminHost != "painel.example.com" {
		t.Fatal("panel accepted a project address")
	}
	newCert := filepath.Join(dir, "new-cert.pem")
	write(newCert, pem.EncodeToMemory(&pem.Block{Type: "ARGO TUNNEL TOKEN", Bytes: []byte(`{"accountID":"` + account + `","zoneID":"` + secondZone + `","apiToken":"fixture"}`)}))
	if err := m.authorizeAdditionalZone(ctx, cfg, certificate{AccountID: account, ZoneID: secondZone, Token: "fixture"}, newCert); err != nil {
		t.Fatal(err)
	}
	if warning, err := m.ChangePanel(ctx, "example.net", "dashboard"); err != nil || warning != "" || m.Config().AdminHost != "dashboard.example.net" || m.Config().TunnelID != id {
		t.Fatalf("failed to move the panel on the same tunnel: %+v %s %v", m.Config(), warning, err)
	}
	updated, warning, err := m.RouteTo(ctx, project.ID, "example.com", "novo", 3000)
	if err != nil || warning != "" || updated.ID != route.ID || updated.Hostname != "novo.example.com" {
		t.Fatalf("failed to move project route on original zone: %+v %s %v", updated, warning, err)
	}
	if warning, err := m.RemoveRoute(ctx, project.ID); err != nil || warning == "" {
		t.Fatalf("should preserve externally edited DNS: %s %v", warning, err)
	}
	stored, err = store.Routes()
	if err != nil || len(stored) != 0 {
		t.Fatalf("route still public: %+v %v", stored, err)
	}
	if _, err := store.Project(project.ID); err != nil {
		t.Fatal("project was removed with route")
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(deleted, ",") != "painel.example.com,app.example.com" {
		t.Fatalf("unexpected DNS deletions: %v", deleted)
	}
}
