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
	"runtime"
	"strings"
	"testing"
)

func TestPostgresTunnelRequiresAccessAndPreservesHTTPIngress(t *testing.T) {
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
	cfg := model.CloudConfig{Mode: "local", AccountID: account, ZoneID: zone, Domain: "example.com", AdminHost: "panel.example.com", TunnelID: id}
	if err := store.SaveCloudConfig(cfg); err != nil {
		t.Fatal(err)
	}
	m := New(store, dir, "http://127.0.0.1:28932", "test")
	t.Cleanup(m.Stop)
	if err = os.MkdirAll(filepath.Dir(certPath(m.localHome())), 0700); err != nil {
		t.Fatal(err)
	}
	cert := pem.EncodeToMemory(&pem.Block{Type: "ARGO TUNNEL TOKEN", Bytes: []byte(`{"accountID":"` + account + `","zoneID":"` + zone + `","apiToken":"fixture"}`)})
	if err = os.WriteFile(certPath(m.localHome()), cert, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(dir, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	binary := "cloudflared"
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	// The manager can prepare its configuration without starting a real connector.
	if err = os.WriteFile(filepath.Join(dir, "bin", binary), nil, 0600); err != nil {
		t.Fatal(err)
	}
	m.runLocalCLI = func(_ context.Context, args ...string) ([]byte, error) {
		if got := strings.Join(args, " "); got != "route dns "+id+" postgres.example.com" {
			t.Fatalf("unexpected cloudflared command: %s", got)
		}
		return nil, nil
	}
	relayOpened, relayClosed := false, false
	m.OpenPostgresRelay = func(owner, database string) (int, context.CancelFunc, error) {
		if owner != "owner" || database != "database" {
			t.Fatalf("wrong relay destination: %q, %q", owner, database)
		}
		relayOpened = true
		return 41235, func() { relayClosed = true }, nil
	}
	aud := strings.Repeat("a", 64)
	input := PostgresInput{Domain: "example.com", Subdomain: "postgres", Team: "example.cloudflareaccess.com"}
	if _, err := m.AddPostgresTunnel(ctx, "owner", "database", input); err == nil || relayOpened {
		t.Fatal("publication without AUD must fail before opening a relay")
	}
	input.Audience = aud
	view, err := m.AddPostgresTunnel(ctx, "owner", "database", input)
	m.stopConnector()
	if err != nil || view.Hostname != "postgres.example.com" || !relayOpened {
		t.Fatalf("could not configure PostgreSQL: %+v %v", view, err)
	}
	if got := m.PostgresTunnel("other", "database", false); got.Hostname != "" {
		t.Fatal("route disclosed to another owner")
	}
	reopened := New(store, dir, m.Origin, m.Installation)
	reopened.OpenPostgresRelay = m.OpenPostgresRelay
	reopened.restorePostgresRelays(cfg)
	if got := reopened.PostgresTunnel("owner", "database", false); got.Hostname != view.Hostname {
		t.Fatal("route was not persisted")
	}
	if _, err = reopened.localRun("unused", cfg); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(m.localHome(), "config.yml")
	contents, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, part := range []string{`hostname: "postgres.example.com"`, `service: "tcp://127.0.0.1:41235"`, `"teamName":"example"`, `"required":true`, `"audTag":["` + aud + `"]`, `service: "http://127.0.0.1:28932"`} {
		if !strings.Contains(text, part) {
			t.Errorf("configuration missing %s: %s", part, text)
		}
	}
	if strings.Index(text, "tcp://") > strings.Index(text, m.Origin) {
		t.Fatal("the PostgreSQL route was shadowed by the HTTP catch-all")
	}
	if cli := os.Getenv("CLOUDFORGE_TEST_CLOUDFLARED"); cli != "" {
		out, err := exec.Command(cli, "tunnel", "--config", config, "ingress", "validate").CombinedOutput()
		if err != nil {
			t.Fatalf("cloudflared rejected the generated configuration: %v\n%s", err, out)
		}
	}
	var rules []ingressRule
	cf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Config struct{ Ingress []ingressRule }
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		rules = body.Config.Ingress
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer cf.Close()
	reopened.Client = &Client{BaseURL: cf.URL, HTTP: cf.Client(), Vault: memoryVault{"api-token": "test"}}
	if err = reopened.ingress(ctx, cfg, []model.Route{{Hostname: "app.example.com"}}); err != nil {
		t.Fatal(err)
	}
	if len(rules) != 4 || rules[0].Hostname != cfg.AdminHost || rules[1].Hostname != "app.example.com" ||
		rules[2].Hostname != view.Hostname || rules[2].OriginRequest["access"] == nil || rules[3].Service != "http_status:404" {
		t.Fatalf("incorrect remote ingress: %+v", rules)
	}
	dns := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": []DNS{}})
	}))
	defer dns.Close()
	m.Client = &Client{BaseURL: dns.URL, HTTP: dns.Client()}
	if _, err = m.RemovePostgresTunnel(ctx, "owner", "database"); err != nil || !relayClosed {
		t.Fatalf("could not disable the PostgreSQL route: %v", err)
	}
	if rules := m.postgresIngress(cfg); len(rules) != 0 {
		t.Fatalf("disabled route maintained its ingress: %+v", rules)
	}
}
