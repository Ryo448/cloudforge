package cloudflare

import (
	"cloudforge/core/internal/persistence"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

type memoryVault map[string]string

func (v memoryVault) Get(k string) (string, error) { return v[k], nil }
func (v memoryVault) Set(k, val string) error      { v[k] = val; return nil }
func TestProvisionRollsBackDNSFailure(t *testing.T) {
	deleted := false
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		result := any(nil)
		success := true
		switch {
		case r.URL.Path == "/zones":
			result = []map[string]any{{"id": "zone", "name": "example.com", "account": map[string]string{"id": "account"}}}
		case r.Method == "GET" && r.URL.Path == "/zones/zone/dns_records":
			result = []any{}
		case r.Method == "POST" && r.URL.Path == "/accounts/account/cfd_tunnel":
			result = map[string]string{"id": "tunnel", "token": "test-tunnel-token"}
		case r.Method == "POST" && r.URL.Path == "/zones/zone/dns_records":
			success = false
		case r.Method == "DELETE" && r.URL.Path == "/accounts/account/cfd_tunnel/tunnel":
			deleted = true
		}
		json.NewEncoder(w).Encode(map[string]any{"success": success, "result": result, "errors": []map[string]any{{"code": 1000, "message": "DNS test failure"}}})
	}))
	defer api.Close()
	dir := t.TempDir()
	store, e := persistence.Open(filepath.Join(dir, "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	m := New(store, dir, "http://127.0.0.1:28932", "test")
	m.Client = &Client{api.URL, api.Client(), memoryVault{"api-token": "test-token"}}
	if _, e = m.Provision(context.Background(), "zone", "panel"); e == nil {
		t.Fatal("DNS failure ignored")
	}
	if !deleted || m.Config().TunnelID != "" {
		t.Fatalf("partial state: deleted=%v config=%+v error=%v", deleted, m.Config(), e)
	}
	pending, e := store.PendingOperations()
	if e != nil || len(pending) != 0 {
		t.Fatalf("rollback did not settle journal: %v %v", pending, e)
	}
}
func TestHostValidation(t *testing.T) {
	for _, s := range []string{"", "-bad", "bad-", "a.b", "foo/evil", "UPPER/"} {
		if _, e := Host(s, "example.com"); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
	if host, e := Host("api", "example.com"); e != nil || host != "api.example.com" {
		t.Fatal(host, e)
	}
}
