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
