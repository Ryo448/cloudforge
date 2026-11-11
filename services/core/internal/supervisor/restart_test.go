package supervisor

import (
	"cloudforge/core/internal/model"
	"cloudforge/core/internal/persistence"
	"cloudforge/core/internal/projects"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestFailureBackoffAndFinalLogLine(t *testing.T) {
	if _, e := exec.LookPath("node"); e != nil {
		t.Skip("Node not installed")
	}
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"restart-fixture","version":"1.0.0","private":true}`), 0600)
	os.WriteFile(filepath.Join(root, "main.cjs"), []byte(`const fs=require('node:fs');if(!fs.existsSync('attempt.txt')){fs.writeFileSync('attempt.txt','1');process.stdout.write('final-partial-line');process.exitCode=1;}else{console.log('restarted-after-failure');setInterval(()=>{},1000);}`), 0600)
	store, e := persistence.Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	p := model.Project{Name: "Restart", Runtime: "node", Path: root, StartCommand: []string{"node", "main.cjs"}, RestartPolicy: "on-failure"}
	if e = projects.New(&p); e != nil {
		t.Fatal(e)
	}
	if e = store.SaveProject(p); e != nil {
		t.Fatal(e)
	}
	sup := New(store, t.TempDir())
	sup.Add(p)
	defer sup.Shutdown()
	if _, e = sup.Action(p.ID, "start"); e != nil {
		t.Fatal(e)
	}
	await(t, 40*time.Second, func() bool {
		state := sup.Views()[0].RuntimeInfo
		return state.ActualState == "online" && state.Restarts > 0
	})
	logs, _ := sup.Logs(p.ID)
	found := false
	for _, line := range logs {
		if line.Message == "final-partial-line" {
			found = true
		}
	}
	if !found {
		t.Fatal("final stdout line without newline was lost")
	}
}
