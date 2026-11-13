package api

import (
	"bytes"
	"cloudforge/core/internal/cloudflare"
	"cloudforge/core/internal/model"
	"cloudforge/core/internal/persistence"
	"cloudforge/core/internal/supervisor"
	"context"
	"encoding/json"
	"github.com/zalando/go-keyring"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
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
func TestLocalEnvironmentModes(t *testing.T) {
	s := fixture(t)
	vault := &testEnvironmentVault{values: map[string]string{}}
	s.Store.(*persistence.Store).EnvironmentVault = vault
	if err := s.Auth.Bootstrap("admin", "a-long-test-password"); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	project := model.Project{ID: "environment-test", Name: "Environment", Runtime: "node", Path: root}
	if err := s.Store.SaveProject(project); err != nil {
		t.Fatal(err)
	}
	url := "/api/projects/" + project.ID + "/environment"
	response := invoke(s, true, "GET", url, "", "", "", nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"mode":"empty"`) {
		t.Fatalf("empty mode: %d %s", response.Code, response.Body.String())
	}
	response = invoke(s, true, "PUT", url, `{"mode":"exclusive","variables":[{"key":"DB_HOST","value":"memory-host"}]}`, "", "", nil)
	if response.Code != 200 {
		t.Fatalf("exclusive save: %d %s", response.Code, response.Body.String())
	}
	var stored string
	if err := s.Store.Setting("environment:"+project.ID, &stored); err != nil || strings.Contains(stored, "memory-host") {
		t.Fatalf("exclusive variable was not encrypted: %v", err)
	}
	response = invoke(s, true, "GET", url, "", "", "", nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"value":"memory-host"`) {
		t.Fatalf("exclusive load: %d %s", response.Code, response.Body.String())
	}
	delete(vault.values, "master")
	response = invoke(s, true, "PUT", url, `{"mode":"exclusive","variables":[{"key":"DB_HOST","value":"other-host"}]}`, "", "", nil)
	if response.Code != 500 {
		t.Fatalf("missing vault key was silently rotated: %d %s", response.Code, response.Body.String())
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("# note\nDB_HOST=old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	response = invoke(s, true, "GET", url, "", "", "", nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"mode":"file"`) {
		t.Fatalf("file mode: %d %s", response.Code, response.Body.String())
	}
	response = invoke(s, true, "PUT", url, `{"mode":"file","file":".env","variables":[{"key":"DB_HOST","value":"new"}]}`, "", "", nil)
	if response.Code != 200 {
		t.Fatalf("file save: %d %s", response.Code, response.Body.String())
	}
	raw, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil || !strings.Contains(string(raw), "# note\nDB_HOST=new") {
		t.Fatalf("file content: %q, %v", raw, err)
	}
}

type testEnvironmentVault struct{ values map[string]string }

func (v *testEnvironmentVault) Get(id string) (string, error) {
	value, ok := v.values[id]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return value, nil
}
func (v *testEnvironmentVault) Set(id, value string) error { v.values[id] = value; return nil }
func (v *testEnvironmentVault) Delete(id string) error     { delete(v.values, id); return nil }
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
	if w = invoke(s, false, "PATCH", "/api/projects/test-project/startup", `{}`, "http://127.0.0.1", csrf, cookies); w.Code != 404 {
		t.Fatalf("startup settings exposed on the public API: %d", w.Code)
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

func TestProjectStartupUpdate(t *testing.T) {
	s := fixture(t)
	if err := s.Auth.Bootstrap("admin", "fixture-long-password"); err != nil {
		t.Fatal(err)
	}
	for _, runtime := range []string{"node", "python"} {
		t.Run(runtime, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			for name, content := range map[string]string{
				"package.json": `{"scripts":{"start":"node app.js","dev":"node app.js"}}`,
				"app.py":       "",
				"worker.py":    "",
			} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			p := model.Project{
				ID: runtime, Name: "Startup", Runtime: runtime, Path: root,
				WorkingDirectory: root, DesiredState: "stopped", Port: 8080,
			}
			var invalid, valid string
			if runtime == "node" {
				p.StartCommand = []string{"npm", "run", "start"}
				invalid = `{"startCommand":["npm","run","missing"]}`
				valid = `{"startCommand":["npm","run","dev"]}`
			} else {
				p.EntryPoint = filepath.Join(root, "app.py")
				p.Arguments = []string{"--old"}
				invalid = `{"entryPoint":"package.json"}`
				valid = `{"entryPoint":"worker.py","arguments":["--label","two words",""]}`
			}
			if err := s.Store.SaveProject(p); err != nil {
				t.Fatal(err)
			}
			s.Supervisor.Add(p)
			path := "/api/projects/" + p.ID + "/startup"
			update := func(body string, status int) {
				t.Helper()
				w := invoke(s, true, "PATCH", path, body, "", "", nil)
				if w.Code != status {
					t.Fatalf("%s: got %d %s", body, w.Code, w.Body.String())
				}
			}
			check := func(project model.Project, command, args []string, entry string) {
				t.Helper()
				if !slices.Equal(project.StartCommand, command) || !slices.Equal(project.Arguments, args) ||
					project.EntryPoint != entry || project.DesiredState != "stopped" || project.Port != p.Port {
					t.Fatalf("unexpected project settings: %+v", project)
				}
			}
			stored := func() model.Project {
				t.Helper()
				project, err := s.Store.Project(p.ID)
				if err != nil {
					t.Fatal(err)
				}
				return project
			}
			update(invalid, 400)
			check(stored(), p.StartCommand, p.Arguments, p.EntryPoint)
			update(valid, 200)
			command, args := p.StartCommand, p.Arguments
			entry := p.EntryPoint
			if runtime == "node" {
				command = []string{"npm", "run", "dev"}
			} else {
				args = []string{"--label", "two words", ""}
				entry = filepath.Join(root, "worker.py")
			}
			check(stored(), command, args, entry)
			for _, view := range s.Supervisor.Views() {
				if view.ID == p.ID {
					check(view.Project, command, args, entry)
					if view.RuntimeInfo.ActualState != "offline" || view.RuntimeInfo.PID != 0 {
						t.Fatalf("saving started the project: %+v", view.RuntimeInfo)
					}
					return
				}
			}
			t.Fatal("project missing from supervisor")
		})
	}
}

func TestLocalFileEditorAndMedia(t *testing.T) {
	s := fixture(t)
	if err := s.Auth.Bootstrap("admin", "fixture-long-password"); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.py"), []byte("print('old')\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cover.png"), []byte("image-bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	p := model.Project{ID: "files-test", Name: "Files", Runtime: "python", Path: root, WorkingDirectory: root, DesiredState: "stopped", Port: 8080, EntryPoint: filepath.Join(root, "app.py")}
	if err := s.Store.SaveProject(p); err != nil {
		t.Fatal(err)
	}
	s.Supervisor.Add(p)
	fileURL := "/api/projects/files-test/file?path=app.py"
	response := invoke(s, true, "GET", fileURL, "", "", "", nil)
	if response.Code != 200 {
		t.Fatalf("read: %d %s", response.Code, response.Body.String())
	}
	var read struct {
		Content string `json:"content"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &read); err != nil || read.Content != "print('old')\n" || read.Version == "" {
		t.Fatalf("read: %+v %v", read, err)
	}
	body, _ := json.Marshal(map[string]string{"path": "app.py", "content": "print('new')\n", "version": read.Version})
	response = invoke(s, true, "PUT", "/api/projects/files-test/file", string(body), "", "", nil)
	if response.Code != 200 {
		t.Fatalf("save: %d %s", response.Code, response.Body.String())
	}
	response = invoke(s, true, "PUT", "/api/projects/files-test/file", string(body), "", "", nil)
	if response.Code != 409 {
		t.Fatalf("stale save: %d %s", response.Code, response.Body.String())
	}
	response = invoke(s, true, "GET", "/api/projects/files-test/media?path=cover.png", "", "", "", nil)
	if response.Code != 200 || response.Body.String() != "image-bytes" || response.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("media: %d %q %q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
	response = invoke(s, false, "GET", "/api/projects/files-test/media?path=cover.png", "", "", "", nil)
	if response.Code == 200 {
		t.Fatal("public interface exposed project media")
	}
}
