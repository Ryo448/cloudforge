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

func TestFailureBackoffResetsLogsAndFlushesFinalLine(t *testing.T) {
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
	j, _ := sup.get(p.ID)
	j.logs.Add("system", "before-first-start")
	if _, e = sup.Action(p.ID, "start"); e != nil {
		t.Fatal(e)
	}
	await(t, 10*time.Second, func() bool {
		logs, _ := sup.Logs(p.ID)
		for _, line := range logs {
			if line.Message == "final-partial-line" {
				return true
			}
		}
		return false
	})
	await(t, 40*time.Second, func() bool {
		state := sup.Views()[0].RuntimeInfo
		return state.ActualState == "online" && state.Restarts > 0
	})
	checkLogs := func(old ...string) {
		t.Helper()
		await(t, 5*time.Second, func() bool {
			for _, line := range j.logs.Lines() {
				if line.Message == "restarted-after-failure" {
					return true
				}
			}
			return false
		})
		for _, line := range j.logs.Lines() {
			for _, message := range old {
				if line.Message == message {
					t.Fatalf("old log %q remains after launch", message)
				}
			}
		}
	}
	checkLogs("before-first-start", "final-partial-line")
	first := sup.Views()[0].RuntimeInfo.PID
	marker := j.logs.Lines()[len(j.logs.Lines())-1].Seq
	j.logs.Add("system", "before-manual-restart")
	rotated := filepath.Join(sup.logdir, p.ID+".jsonl.1")
	if err := os.WriteFile(rotated, []byte("old rotated logs"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := sup.Action(p.ID, "restart"); err != nil {
		t.Fatal(err)
	}
	await(t, 25*time.Second, func() bool {
		state := sup.Views()[0].RuntimeInfo
		return state.ActualState == "online" && state.PID != 0 && state.PID != first
	})
	checkLogs("before-manual-restart")
	if j.logs.Lines()[0].Seq <= marker {
		t.Fatal("log sequence was reused after restart")
	}
	if _, err := os.Stat(rotated); !os.IsNotExist(err) {
		t.Fatalf("rotated logs remain after restart: %v", err)
	}
	for _, line := range NewLogbook(sup.logdir, p.ID).Lines() {
		if line.Message == "before-first-start" || line.Message == "before-manual-restart" {
			t.Fatal("old logs remain on disk after restart")
		}
	}
	if _, err := sup.Action(p.ID, "stop"); err != nil {
		t.Fatal(err)
	}
	await(t, 15*time.Second, func() bool {
		if sup.Views()[0].RuntimeInfo.ActualState != "offline" || !j.op.TryLock() {
			return false
		}
		j.op.Unlock()
		return true
	})
	j.logs.Add("system", "before-second-start")
	if _, err := sup.Action(p.ID, "start"); err != nil {
		t.Fatal(err)
	}
	await(t, 25*time.Second, func() bool { return sup.Views()[0].RuntimeInfo.ActualState == "online" })
	checkLogs("before-second-start", "Aplicação parada. Rotas e túnel preservados.")
}
