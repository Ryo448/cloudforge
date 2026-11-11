package supervisor

import (
	"cloudforge/core/internal/model"
	"cloudforge/core/internal/persistence"
	"cloudforge/core/internal/projects"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func await(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("condition did not become true before deadline")
}
func TestRealProcessLifecycleAndRecovery(t *testing.T) {
	for _, runtime := range []string{"node", "python"} {
		t.Run(runtime, func(t *testing.T) { verifyLifecycle(t, runtime) })
	}
}

func verifyLifecycle(t *testing.T, runtime string) {
	t.Helper()
	available := func(name string) bool {
		_, err := exec.LookPath(name)
		return err == nil
	}
	if runtime == "node" && (!available("node") || !available("npm")) {
		t.Skip("Node/npm not installed")
	}
	if runtime == "python" && !available("python") && !available("python3") && !available("py") {
		t.Skip("Python not installed")
	}
	root := t.TempDir()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p := model.Project{Name: "Real " + runtime, Runtime: runtime, Path: root, RestartPolicy: "on-failure", Port: port}
	if runtime == "node" {
		write("package.json", `{"name":"lifecycle-fixture","version":"1.0.0","private":true,"scripts":{"start":"node server.js"}}`)
		write("server.js", fmt.Sprintf(`const http=require('node:http'); console.log('started-real-server'); http.createServer((q,s)=>s.end('fixture-alive')).listen(%d,'127.0.0.1');`, port))
		p.StartCommand = []string{"npm", "run", "start"}
	} else {
		write("server.py", fmt.Sprintf(`from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"fixture-alive")

server = ThreadingHTTPServer(("127.0.0.1", %d), Handler)
print("started-real-server", flush=True)
server.serve_forever()
`, port))
		p.EntryPoint = "server.py"
	}
	s, e := persistence.Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	sup := New(s, t.TempDir())
	if e = projects.New(&p); e != nil {
		t.Fatal(e)
	}
	if e = s.SaveProject(p); e != nil {
		t.Fatal(e)
	}
	sup.Add(p)
	defer sup.Shutdown()
	if _, e = sup.Action(p.ID, "start"); e != nil {
		t.Fatal(e)
	}
	alive := func() bool {
		client := http.Client{Timeout: time.Second}
		r, e := client.Get(fmt.Sprintf("http://127.0.0.1:%d", port))
		if e != nil {
			return false
		}
		r.Body.Close()
		return r.StatusCode == 200
	}
	await(t, 45*time.Second, alive)
	first := sup.Views()[0].RuntimeInfo.PID
	if first == 0 {
		t.Fatal("no real PID")
	}
	if _, e = sup.Action(p.ID, "start"); e != nil {
		t.Fatal(e)
	}
	time.Sleep(200 * time.Millisecond)
	if sup.Views()[0].RuntimeInfo.PID != first {
		t.Fatal("duplicate start created a process")
	}
	if _, e = sup.Action(p.ID, "restart"); e != nil {
		t.Fatal(e)
	}
	await(t, 25*time.Second, func() bool {
		return sup.Views()[0].RuntimeInfo.PID != 0 && sup.Views()[0].RuntimeInfo.PID != first && alive()
	})
	logs, _ := sup.Logs(p.ID)
	found := false
	for _, l := range logs {
		if l.Message == "started-real-server" {
			found = true
		}
	}
	if !found {
		t.Fatal("stdout not captured")
	}
	if _, e = sup.Action(p.ID, "stop"); e != nil {
		t.Fatal(e)
	}
	await(t, 15*time.Second, func() bool { return sup.Views()[0].RuntimeInfo.ActualState == "offline" && !alive() })
	stored, _ := s.Project(p.ID)
	if stored.DesiredState != "stopped" {
		t.Fatal("manual stop not persisted")
	}
	restored := New(s, t.TempDir())
	if e = restored.Restore(); e != nil {
		t.Fatal(e)
	}
	defer restored.Shutdown()
	time.Sleep(250 * time.Millisecond)
	if restored.Views()[0].RuntimeInfo.PID != 0 {
		t.Fatal("manually stopped project restarted on recovery")
	}
	restored.Shutdown()
	if e = s.Desired(p.ID, "running"); e != nil {
		t.Fatal(e)
	}
	resume := New(s, t.TempDir())
	if e = resume.Restore(); e != nil {
		t.Fatal(e)
	}
	defer resume.Shutdown()
	await(t, 30*time.Second, alive)
	if resume.Views()[0].RuntimeInfo.PID == 0 {
		t.Fatal("desired running state was not restored")
	}
	if err := resume.Remove(p.ID); err != nil {
		t.Fatal(err)
	}
	if alive() {
		t.Fatal("deleted project's HTTP listener is still running")
	}
	if len(resume.Views()) != 0 {
		t.Fatal("deleted project still supervised")
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("project folder removed: %v", err)
	}
}
