package api

import (
	"bytes"
	"cloudforge/core/internal/cloudflare"
	"cloudforge/core/internal/model"
	"cloudforge/core/internal/persistence"
	"cloudforge/core/internal/supervisor"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	store, e := persistence.Open(filepath.Join(dir, "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	sup := supervisor.New(store, dir)
	cloud := cloudflare.New(store, dir, "http://127.0.0.1:28932", "test")
	s := New(store, sup, cloud, "local-test-key-not-a-default", dir, "", true)
	t.Cleanup(func() { sup.Shutdown(); store.Close() })
	return s
}
func invoke(s *Server, local bool, method, path, body, origin, csrf string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	if local {
		r.Header.Set("Authorization", "Bearer "+s.Key)
		s.Local(w, r)
	} else {
		s.Public(w, r)
	}
	return w
}
func TestPublicBoundaryAndCSRF(t *testing.T) {
	s := fixture(t)
	if e := s.Auth.Bootstrap("admin", "a-long-test-password"); e != nil {
		t.Fatal(e)
	}
	w := invoke(s, false, "GET", "/api/projects", "", "", "", nil)
	if w.Code != 401 {
		t.Fatal("unauthenticated access accepted")
	}
	w = invoke(s, false, "GET", "/api/session", "", "", "", nil)
	var pre map[string]any
	json.Unmarshal(w.Body.Bytes(), &pre)
	cookies := w.Result().Cookies()
	csrf := pre["csrf"].(string)
	body := `{"username":"admin","password":"a-long-test-password"}`
	w = invoke(s, false, "POST", "/api/login", body, "http://evil.test", csrf, cookies)
	if w.Code != 403 {
		t.Fatal("foreign origin accepted")
	}
	w = invoke(s, false, "POST", "/api/login", body, "http://127.0.0.1", "wrong", cookies)
	if w.Code != 403 {
		t.Fatal("invalid CSRF accepted")
	}
	w = invoke(s, false, "POST", "/api/login", body, "http://127.0.0.1", csrf, cookies)
	if w.Code != 200 {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
	}
	cookies = w.Result().Cookies()
	var login map[string]any
	json.Unmarshal(w.Body.Bytes(), &login)
	csrf = login["csrf"].(string)
	for _, c := range cookies {
		if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
			t.Fatal("unsafe cookie")
		}
	}
	p := model.Project{ID: "test-project", Name: "Test", Path: "C:/private-path", Runtime: "python", DesiredState: "stopped"}
	if e := s.Store.SaveProject(p); e != nil {
		t.Fatal(e)
	}
	s.Supervisor.Add(p)
	w = invoke(s, false, "GET", "/api/projects", "", "", "", cookies)
	if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte("private-path")) {
		t.Fatal("public view leaked project directory")
	}
	for _, path := range []string{"/api/projects/inspect", "/api/cloudflare/connect", "/api/projects/test-project/files", "/api/setup"} {
		w = invoke(s, false, "POST", path, `{}`, "http://127.0.0.1", csrf, cookies)
		if w.Code != 404 {
			t.Fatalf("privileged endpoint exposed: %s => %d", path, w.Code)
		}
	}
	if w = invoke(s, false, "DELETE", "/api/projects/test-project", "", "http://127.0.0.1", csrf, cookies); w.Code != 404 {
		t.Fatalf("project deletion exposed on the public API: %d", w.Code)
	}
	w = invoke(s, false, "POST", "/api/logout", `{}`, "http://127.0.0.1", csrf, cookies)
	if w.Code != 200 {
		t.Fatal("logout failed")
	}
	w = invoke(s, false, "GET", "/api/projects", "", "", "", cookies)
	if w.Code != 401 {
		t.Fatal("revoked session accepted")
	}
	_ = context.Background()
}

func TestDeleteProjectKeepsSourceAndOtherProjects(t *testing.T) {
	s := fixture(t)
	if err := s.Auth.Bootstrap("admin", "fixture-long-password"); err != nil {
		t.Fatal(err)
	}
	removed := model.Project{ID: "removed", Name: "Remove", Path: t.TempDir(), Runtime: "python", DesiredState: "stopped"}
	other := model.Project{ID: "keep", Name: "Keep", Path: t.TempDir(), Runtime: "node", DesiredState: "stopped"}
	writeFile := func(path string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	source := filepath.Join(removed.Path, "app.py")
	writeFile(source)
	for _, p := range []model.Project{removed, other} {
		if err := s.Store.SaveProject(p); err != nil {
			t.Fatal(err)
		}
		s.Supervisor.Add(p)
		if err := s.Store.SaveRoute(model.Route{ID: "route-" + p.ID, ProjectID: p.ID, Hostname: p.ID + ".example.com", Port: 3000}); err != nil {
			t.Fatal(err)
		}
		if err := s.Store.SaveDependencies(model.Dependencies{ProjectID: p.ID}); err != nil {
			t.Fatal(err)
		}
		if err := s.Store.Runtime(model.Runtime{ProjectID: p.ID, ActualState: "offline"}); err != nil {
			t.Fatal(err)
		}
		if err := s.Store.Operation("op-"+p.ID, "process", "pending", map[string]string{"projectId": p.ID}, ""); err != nil {
			t.Fatal(err)
		}
		writeFile(filepath.Join(s.DataDir, "logs", p.ID+".jsonl"))
		writeFile(filepath.Join(s.DataDir, "logs", p.ID+".jsonl.1"))
	}
	w := invoke(s, true, "DELETE", "/api/projects/removed", "", "", "", nil)
	if w.Code != 200 {
		t.Fatalf("delete failed: %d %s", w.Code, w.Body.String())
	}
	if data, err := os.ReadFile(source); err != nil || string(data) != "original" {
		t.Fatalf("source changed: %v", err)
	}
	for _, name := range []string{"removed.jsonl", "removed.jsonl.1"} {
		if _, err := os.Stat(filepath.Join(s.DataDir, "logs", name)); !os.IsNotExist(err) {
			t.Fatalf("log retained: %s %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(s.DataDir, "logs", "keep.jsonl.1")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.Project(removed.ID); err == nil {
		t.Fatal("project retained")
	}
	if _, err := s.Store.Dependencies(removed.ID); err == nil {
		t.Fatal("dependency metadata retained")
	}
	if _, err := s.Store.PreviousRuntime(removed.ID); err == nil {
		t.Fatal("runtime metadata retained")
	}
	routes, err := s.Store.Routes()
	if err != nil || len(routes) != 1 || routes[0].ProjectID != other.ID {
		t.Fatalf("unexpected routes: %+v %v", routes, err)
	}
	operations, err := s.Store.PendingOperations()
	if err != nil || len(operations) != 1 || operations[0]["id"] != "op-keep" {
		t.Fatalf("unexpected operations: %+v %v", operations, err)
	}
	if projects := s.Supervisor.Views(); len(projects) != 1 || projects[0].ID != other.ID {
		t.Fatalf("unexpected projects: %+v", projects)
	}
}
func TestLocalAuthorization(t *testing.T) {
	s := fixture(t)
	r := httptest.NewRequest("GET", "http://127.0.0.1/api/state", nil)
	w := httptest.NewRecorder()
	s.Local(w, r)
	if w.Code != 401 {
		t.Fatal("missing local key accepted")
	}
	w = invoke(s, true, "GET", "/api/state", "", "https://untrusted.test", "", nil)
	if w.Code != 403 {
		t.Fatal("renderer origin allowed on privileged API")
	}
}

func TestProjectPortChangesPublicDestinationAndKeepsProjectStopped(t *testing.T) {
	s := fixture(t)
	if err := s.Auth.Bootstrap("admin", "fixture-long-password"); err != nil {
		t.Fatal(err)
	}
	p := model.Project{ID: "project", Name: "HTTP", Path: t.TempDir(), Runtime: "python", DesiredState: "stopped", Port: 9999}
	if err := s.Store.SaveProject(p); err != nil {
		t.Fatal(err)
	}
	s.Supervisor.Add(p)
	if err := s.Store.SaveRoute(model.Route{ID: "route", ProjectID: p.ID, Hostname: "app.example.com", Port: 9999}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"port":28931}`, 400}, {`{"port":0}`, 409}, {`{"port":80}`, 200},
	} {
		w := invoke(s, true, "PATCH", "/api/projects/project/port", tc.body, "", "", nil)
		if w.Code != tc.status {
			t.Fatalf("%s: got %d %s", tc.body, w.Code, w.Body.String())
		}
	}
	view := s.Supervisor.Views()[0]
	if view.Port != 80 || view.RoutePort != 80 || view.Hostname != "app.example.com" || view.DesiredState != "stopped" || view.RuntimeInfo.PID != 0 {
		t.Fatalf("unexpected state: %+v", view)
	}
	stored, err := s.Store.Project(p.ID)
	if err != nil || stored.Port != 80 {
		t.Fatalf("port not persisted: %+v %v", stored, err)
	}
}
