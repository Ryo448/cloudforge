package cloudflare

import (
	"cloudforge/core/internal/model"
	"cloudforge/core/internal/persistence"
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLocalTunnelHealthDecoupledFromPanelAndUnpublishPreservesServices(t *testing.T) {
	const account = "0123456789abcdef0123456789abcdef"
	const zone = "abcdef0123456789abcdef0123456789"
	const id = "12345678-1234-4234-8234-123456789abc"
	ctx := context.Background()
	dir := t.TempDir()
	store, err := persistence.Open(filepath.Join(dir, "cloudforge.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := model.CloudConfig{
		Mode:      "local",
		AccountID: account,
		ZoneID:    zone,
		Domain:    "example.com",
		AdminHost: "painel-x.example.com",
		TunnelID:  id,
		Name:      "cloudforge-node-x",
	}
	if err := store.SaveCloudConfig(cfg); err != nil {
		t.Fatal(err)
	}
	// Register a project HTTP route that must survive panel unpublication.
	if err := store.SaveProject(model.Project{ID: "proj-1", Name: "API", Path: t.TempDir(), Runtime: "node", DesiredState: "stopped", Port: 4000}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRoute(model.Route{ID: "route-1", ProjectID: "proj-1", ZoneID: zone, Hostname: "api-x.example.com", Port: 4000}); err != nil {
		t.Fatal(err)
	}

	m := New(store, dir, "http://127.0.0.1:28932", "node-x")
	t.Cleanup(m.Stop)

	if err := os.MkdirAll(filepath.Dir(certPath(m.localHome())), 0700); err != nil {
		t.Fatal(err)
	}
	cert := pem.EncodeToMemory(&pem.Block{
		Type:  "ARGO TUNNEL TOKEN",
		Bytes: []byte(`{"accountID":"` + account + `","zoneID":"` + zone + `","apiToken":"fixture"}`),
	})
	if err := os.WriteFile(certPath(m.localHome()), cert, 0600); err != nil {
		t.Fatal(err)
	}

	// Simulate an active DB TCP tunnel on X.
	m.OpenDatabaseRelay = func(owner, dbID string) (int, context.CancelFunc, error) {
		return 41235, func() {}, nil
	}
	m.runLocalCLI = func(_ context.Context, args ...string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "route dns " + id + " db-x.example.com":
			return nil, nil
		case "route dns " + id + " painel-x-novo.example.com":
			return nil, nil
		case "info --output json " + id:
			return []byte(`{"id":"` + id + `","conns":[{"id":"conn-1"}]}`), nil
		default:
			return nil, nil
		}
	}
	if _, err := m.AddDatabaseTunnel(ctx, "owner", "db-1", DatabaseTunnelInput{
		Domain: "example.com", Subdomain: "db-x", Team: "team", Audience: strings.Repeat("a", 64),
	}); err != nil {
		t.Fatal(err)
	}
	m.stopConnector()

	// 1. Mock local cloudflared metrics /ready server.
	readyStatus := http.StatusOK
	readyCount := 4
	var readyMu sync.Mutex
	metricsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ready" {
			http.NotFound(w, r)
			return
		}
		readyMu.Lock()
		code, count := readyStatus, readyCount
		readyMu.Unlock()
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": code, "readyConnections": count})
	}))
	defer metricsServer.Close()

	// 2. Mock Cloudflare DNS API with controllable failure for DELETE.
	var mu sync.Mutex
	failDelete := true
	cnameTarget := id + ".cfargotunnel.com"
	deleted := []string{}
	cfAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		shouldFail := failDelete
		target := cnameTarget
		mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones/"+zone+"/dns_records":
			host := r.URL.Query().Get("name")
			if host == "painel-x.example.com" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"success": true,
					"result":  []DNS{{ID: "rec-panel-x", Name: host, Type: "CNAME", Content: target}},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []DNS{}})
		case r.Method == http.MethodDelete && r.URL.Path == "/zones/"+zone+"/dns_records/rec-panel-x":
			if shouldFail {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"success": false,
					"errors":  []map[string]any{{"code": 500, "message": "temporary dns failure"}},
				})
				return
			}
			mu.Lock()
			deleted = append(deleted, "rec-panel-x")
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]any{}})
		default:
			t.Errorf("unexpected API call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer cfAPI.Close()
	m.Client = &Client{BaseURL: cfAPI.URL, HTTP: cfAPI.Client()}

	// Unpublish panel when DNS deletion fails: router block must activate and cleanup stays pending.
	warning, err := m.UnpublishPanel(ctx)
	if err != nil {
		t.Fatalf("UnpublishPanel returned hard error: %v", err)
	}
	if warning == "" {
		t.Fatal("expected warning when DNS deletion fails")
	}
	updatedCfg := m.Config()
	if !updatedCfg.PanelDisabled || updatedCfg.AdminHost != "" || updatedCfg.RetiredAdminHost != "painel-x.example.com" || updatedCfg.PanelCleanupError == "" {
		t.Fatalf("unexpected config after failed DNS cleanup: %+v", updatedCfg)
	}
	if m.PublicURL() != "" {
		t.Fatalf("PublicURL should be empty after unpublish, got %q", m.PublicURL())
	}

	// Verify local tunnel health works via /ready without any AdminHost!
	m.mu.Lock()
	m.pid = 4242
	m.localMetricsAddr = strings.TrimPrefix(metricsServer.URL, "http://")
	m.mu.Unlock()

	st := m.Status(ctx)
	if st["connected"] != true || st["tunnelHealthy"] != true || st["panelPublished"] != false || st["panelCleanupPending"] != true {
		t.Fatalf("unexpected status with unpublished panel: %+v", st)
	}
	if err := m.Verify(ctx); err != nil {
		t.Fatalf("Verify should succeed via tunnel health when panel is unpublished: %v", err)
	}

	// Simulate tunnel dropping to 0 ready connections.
	readyMu.Lock()
	readyStatus = http.StatusServiceUnavailable
	readyCount = 0
	readyMu.Unlock()
	if st := m.Status(ctx); st["connected"] != false || st["status"] != "connecting" {
		t.Fatalf("expected disconnected status when /ready returns 503: %+v", st)
	}

	// Verify CLI info fallback works when metricsAddr is absent.
	m.mu.Lock()
	m.localMetricsAddr = ""
	m.mu.Unlock()
	if st := m.Status(ctx); st["connected"] != true {
		t.Fatalf("expected CLI info fallback to report connected: %+v", st)
	}

	// Verify local config.yml blocks retired panel with 404 while preserving TCP DB route and HTTP catch-all.
	if _, err := m.localRun("unused", m.Config()); err != nil {
		t.Fatalf("localRun failed after unpublish: %v", err)
	}
	configFile := filepath.Join(m.localHome(), "config.yml")
	rawConfig, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}
	text := string(rawConfig)
	for _, want := range []string{
		`hostname: "db-x.example.com"`,
		`service: "tcp://127.0.0.1:41235"`,
		`hostname: "painel-x.example.com"`,
		`service: "http_status:404"`,
		`service: "http://127.0.0.1:28932"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("local config.yml missing %q:\n%s", want, text)
		}
	}
	// If bundled cloudflared exists in repo, validate generated config.yml syntax directly.
	if bundled, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "dist", "cloudflared", "cloudflared.exe")); err == nil {
		if _, statErr := os.Stat(bundled); statErr == nil {
			if out, err := exec.Command(bundled, "tunnel", "--config", configFile, "ingress", "validate").CombinedOutput(); err != nil {
				t.Fatalf("cloudflared rejected generated config.yml: %v\n%s", err, out)
			}
		}
	}

	// Verify CNAME belonging to another tunnel is NOT deleted on retry.
	mu.Lock()
	failDelete = false
	cnameTarget = "other-tunnel-id.cfargotunnel.com"
	mu.Unlock()
	if err := m.RetryPanelCleanup(ctx); err != nil {
		t.Fatalf("RetryPanelCleanup failed on external CNAME: %v", err)
	}
	mu.Lock()
	delCount := len(deleted)
	cnameTarget = id + ".cfargotunnel.com"
	mu.Unlock()
	if delCount != 0 {
		t.Fatalf("must not delete CNAME pointing to another tunnel, deleted=%v", deleted)
	}

	// Now retry when CNAME belongs to X's tunnel -> must delete cleanly!
	if err := m.RetryPanelCleanup(ctx); err != nil {
		t.Fatalf("RetryPanelCleanup failed: %v", err)
	}
	mu.Lock()
	delCount = len(deleted)
	mu.Unlock()
	if delCount != 1 {
		t.Fatalf("expected panel CNAME to be deleted on retry, deleted=%v", deleted)
	}
	if m.Config().PanelCleanupError != "" {
		t.Fatalf("expected cleanup error cleared, got %q", m.Config().PanelCleanupError)
	}

	// Verify explicit RepublishPanel restores standalone panel cleanly.
	repub, err := m.RepublishPanel(ctx, "example.com", "painel-x-novo")
	if err != nil {
		t.Fatalf("RepublishPanel failed: %v", err)
	}
	if repub.PanelDisabled || repub.AdminHost != "painel-x-novo.example.com" {
		t.Fatalf("unexpected republished config: %+v", repub)
	}
}

func TestRemoteTunnelUnpublishRemovesOnlyPanelIngressAndCNAME(t *testing.T) {
	const account = "acc-remote"
	const zone = "zone-remote"
	const id = "tunnel-remote-x"
	ctx := context.Background()
	dir := t.TempDir()
	store, err := persistence.Open(filepath.Join(dir, "cloudforge.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := model.CloudConfig{
		AccountID: account,
		ZoneID:    zone,
		Domain:    "example.com",
		AdminHost: "painel-remote.example.com",
		TunnelID:  id,
		Name:      "cloudforge-remote-x",
	}
	if err := store.SaveCloudConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveProject(model.Project{ID: "p1", Name: "App", Path: t.TempDir(), Runtime: "python", DesiredState: "stopped", Port: 5000}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRoute(model.Route{ID: "r1", ProjectID: "p1", ZoneID: zone, Hostname: "app-remote.example.com", Port: 5000}); err != nil {
		t.Fatal(err)
	}

	m := New(store, dir, "http://127.0.0.1:28932", "remote-x")
	t.Cleanup(m.Stop)
	m.OpenDatabaseRelay = func(owner, dbID string) (int, context.CancelFunc, error) {
		return 45555, func() {}, nil
	}
	m.mu.Lock()
	m.databaseTunnels = []DatabaseTunnel{{
		Owner: "owner", DatabaseID: "db-1", TunnelID: id, ZoneID: zone,
		Hostname: "pg-remote.example.com", Team: "team", Audience: strings.Repeat("b", 64),
	}}
	m.databaseRelays["db-1"] = databaseRelay{port: 45555, stop: func() {}}
	m.mu.Unlock()

	var mu sync.Mutex
	var lastIngress []ingressRule
	var deletedRecords []string
	cfAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/accounts/"+account+"/cfd_tunnel/"+id+"/configurations":
			var body struct {
				Config struct {
					Ingress []ingressRule `json:"ingress"`
				} `json:"config"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			lastIngress = body.Config.Ingress
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]any{}})
		case r.Method == http.MethodGet && r.URL.Path == "/accounts/"+account+"/cfd_tunnel/"+id+"/connections":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []map[string]any{{"id": "edge-1"}}})
		case r.Method == http.MethodGet && r.URL.Path == "/zones/"+zone+"/dns_records":
			host := r.URL.Query().Get("name")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"result": []DNS{{
					ID:      "dns-panel-remote",
					Name:    host,
					Type:    "CNAME",
					Content: id + ".cfargotunnel.com",
					Comment: "Cloudforge remote-x",
				}},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/zones/"+zone+"/dns_records/dns-panel-remote":
			mu.Lock()
			deletedRecords = append(deletedRecords, "dns-panel-remote")
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]any{}})
		default:
			t.Errorf("unexpected remote API call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer cfAPI.Close()
	m.Client = &Client{BaseURL: cfAPI.URL, HTTP: cfAPI.Client(), Vault: memoryVault{"api-token": "test"}}

	warning, err := m.UnpublishPanel(ctx)
	if err != nil || warning != "" {
		t.Fatalf("remote UnpublishPanel failed: warning=%q err=%v", warning, err)
	}
	mu.Lock()
	ingress := lastIngress
	deleted := deletedRecords
	mu.Unlock()

	// Ingress must NOT contain painel-remote.example.com, MUST contain app-remote.example.com, pg-remote.example.com, and 404 catch-all.
	if len(ingress) != 3 {
		t.Fatalf("expected 3 ingress rules (http app + tcp db + 404), got %+v", ingress)
	}
	if ingress[0].Hostname != "app-remote.example.com" || ingress[1].Hostname != "pg-remote.example.com" || ingress[2].Service != "http_status:404" {
		t.Fatalf("unexpected remote ingress rules after unpublish: %+v", ingress)
	}
	if len(deleted) != 1 || deleted[0] != "dns-panel-remote" {
		t.Fatalf("expected remote panel CNAME deleted, got %v", deleted)
	}
	// Remote tunnel health must still report connected without panel.
	st := m.Status(ctx)
	if st["connected"] != true || st["panelPublished"] != false {
		t.Fatalf("unexpected remote tunnel status: %+v", st)
	}
	if err := m.Verify(ctx); err != nil {
		t.Fatalf("remote Verify failed without panel: %v", err)
	}
}
