package runtimes

import (
	"cloudforge/core/internal/model"
	"cloudforge/core/internal/persistence"
	"cloudforge/core/internal/projects"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T, runtime string) (*persistence.Store, model.Project, context.Context) {
	t.Helper()
	root := t.TempDir()
	s, e := persistence.Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	return s, model.Project{Name: "Integration", Path: root, Runtime: runtime, RestartPolicy: "never", WorkingDirectory: root}, ctx
}
func TestNodeDependencyCache(t *testing.T) {
	if _, e := exec.LookPath("node"); e != nil {
		t.Skip("Node.js not installed")
	}
	s, p, ctx := setup(t, "node")
	os.WriteFile(filepath.Join(p.Path, "package.json"), []byte(`{"name":"cloudforge-fixture","version":"1.0.0","private":true,"scripts":{"start":"node index.js"}}`), 0600)
	os.WriteFile(filepath.Join(p.Path, "index.js"), []byte(`console.log('real node output')`), 0600)
	p.StartCommand = []string{"npm", "run", "start"}
	if e := projects.New(&p); e != nil {
		t.Fatal(e)
	}
	if e := s.SaveProject(p); e != nil {
		t.Fatal(e)
	}
	var logs []string
	log := func(_, msg string) { logs = append(logs, msg) }
	cmd, e := Prepare(ctx, s, p, log)
	if e != nil {
		t.Fatal(e)
	}
	cmd.Dir = p.Path
	if out, e := cmd.CombinedOutput(); e != nil || !strings.Contains(string(out), "real node output") {
		t.Fatalf("Node did not execute: %s %v", out, e)
	}
	first, e := s.Dependencies(p.ID)
	if e != nil || first.ManifestHash == "" || first.InstallStatus != "success" {
		t.Fatal("successful installation not persisted")
	}
	if _, e = Prepare(ctx, s, p, log); e != nil {
		t.Fatal(e)
	}
	second, _ := s.Dependencies(p.ID)
	if !first.LastSuccessfulInstall.Equal(second.LastSuccessfulInstall) {
		t.Fatal("unchanged dependencies reinstalled")
	}
	os.WriteFile(filepath.Join(p.Path, "package.json"), []byte(`{"name":"cloudforge-fixture","version":"1.0.1","private":true,"scripts":{"start":"node index.js"}}`), 0600)
	if _, e = Prepare(ctx, s, p, log); e != nil {
		t.Fatal(e)
	}
	third, _ := s.Dependencies(p.ID)
	if third.ManifestHash == second.ManifestHash {
		t.Fatal("changed manifest not installed")
	}
}
func TestPythonVenvAndRequirements(t *testing.T) {
	if _, e := exec.LookPath("python"); e != nil {
		if _, e = exec.LookPath("python3"); e != nil {
			t.Skip("Python not installed")
		}
	}
	s, p, ctx := setup(t, "python")
	os.WriteFile(filepath.Join(p.Path, "main.py"), []byte("print('real python output')\n"), 0600)
	os.WriteFile(filepath.Join(p.Path, "requirements.txt"), []byte("# no third-party dependencies\n"), 0600)
	p.EntryPoint = "main.py"
	if e := projects.New(&p); e != nil {
		t.Fatal(e)
	}
	if e := s.SaveProject(p); e != nil {
		t.Fatal(e)
	}
	log := func(_, msg string) { t.Log(msg) }
	cmd, e := Prepare(ctx, s, p, log)
	if e != nil {
		t.Fatal(e)
	}
	cmd.Dir = p.Path
	if out, e := cmd.CombinedOutput(); e != nil || !strings.Contains(string(out), "real python output") {
		t.Fatalf("Python did not execute: %s %v", out, e)
	}
	first, e := s.Dependencies(p.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Prepare(ctx, s, p, log); e != nil {
		t.Fatal(e)
	}
	second, _ := s.Dependencies(p.ID)
	if !first.LastSuccessfulInstall.Equal(second.LastSuccessfulInstall) {
		t.Fatal("unchanged requirements reinstalled")
	}
	os.WriteFile(filepath.Join(p.Path, "requirements.txt"), []byte("# changed requirements\n"), 0600)
	if _, e = Prepare(ctx, s, p, log); e != nil {
		t.Fatal(e)
	}
	third, _ := s.Dependencies(p.ID)
	if first.ManifestHash == third.ManifestHash {
		t.Fatal("requirements change ignored")
	}
	os.WriteFile(filepath.Join(p.Path, "requirements.txt"), []byte("--not-a-real-pip-option\n"), 0600)
	if _, e = Prepare(ctx, s, p, log); e == nil {
		t.Fatal("failed pip install must prevent startup")
	}
	last, _ := s.Dependencies(p.ID)
	if last.ManifestHash != third.ManifestHash {
		t.Fatal("failed installation advanced successful requirements hash")
	}
}
