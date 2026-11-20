package routing

import (
	"cloudforge/core/internal/cloudflare"
	"cloudforge/core/internal/model"
	"cloudforge/core/internal/persistence"
	"cloudforge/core/internal/supervisor"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestOfflineRouteIsPreserved(t *testing.T) {
	dir := t.TempDir()
	s, e := persistence.Open(filepath.Join(dir, "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	p := model.Project{ID: "project", Name: "Offline", Path: dir, Runtime: "python", DesiredState: "stopped"}
	if e = s.SaveProject(p); e != nil {
		t.Fatal(e)
	}
	route := model.Route{ID: "route", ProjectID: p.ID, Hostname: "app.example.com", Port: 3000}
	if e = s.SaveRoute(route); e != nil {
		t.Fatal(e)
	}
	sup := supervisor.New(s, dir)
	sup.Add(p)
	defer sup.Shutdown()
	cf := cloudflare.New(s, dir, "http://127.0.0.1:28932", "test")
	router := &Router{Store: s, Cloud: cf, Supervisor: sup, Dashboard: http.NotFoundHandler()}
	req := httptest.NewRequest("GET", "https://app.example.com/", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 503 {
		t.Fatalf("offline application should return 503, got %d", w.Code)
	}
	saved, _ := s.Routes()
	if len(saved) != 1 {
		t.Fatal("offline route was removed")
	}
	req = httptest.NewRequest("GET", "https://unconfigured.example.com/", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatal("unknown host was routed")
	}
}

func TestUnpublishedPanelBlockedWhileProjectRoutesKeepWorking(t *testing.T) {
	dir := t.TempDir()
	s, err := persistence.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SaveCloudConfig(model.CloudConfig{
		Mode:             "local",
		AccountID:        "0123456789abcdef0123456789abcdef",
		ZoneID:           "abcdef0123456789abcdef0123456789",
		Domain:           "example.com",
		AdminHost:        "",
		TunnelID:         "12345678-1234-4234-8234-123456789abc",
		Name:             "cloudforge-x",
		PanelDisabled:    true,
		RetiredAdminHost: "painel-x.example.com",
	}); err != nil {
		t.Fatal(err)
	}
	p := model.Project{ID: "proj-x", Name: "WebApp", Path: dir, Runtime: "node", DesiredState: "stopped"}
	if err := s.SaveProject(p); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRoute(model.Route{ID: "r-x", ProjectID: p.ID, Hostname: "app-x.example.com", Port: 3001}); err != nil {
		t.Fatal(err)
	}
	sup := supervisor.New(s, dir)
	sup.Add(p)
	defer sup.Shutdown()
	cf := cloudflare.New(s, dir, "http://127.0.0.1:28932", "node-x")
	dashboardHit := false
	router := &Router{
		Store:      s,
		Cloud:      cf,
		Supervisor: sup,
		Dashboard: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			dashboardHit = true
			w.WriteHeader(http.StatusOK)
		}),
		Development: true,
	}

	for _, host := range []string{"painel-x.example.com", "127.0.0.1", "localhost"} {
		req := httptest.NewRequest("GET", "https://"+host+"/api/health", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound || dashboardHit {
			t.Fatalf("unpublished panel allowed host %s: status=%d dashboardHit=%v", host, w.Code, dashboardHit)
		}
	}

	// Project route on the same node must still be matched by the router (503 since project is stopped).
	req := httptest.NewRequest("GET", "https://app-x.example.com/", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("project route broken after panel unpublish: got %d", w.Code)
	}
}
