package supervisor

import (
	"cloudforge/core/internal/model"
	"cloudforge/core/internal/persistence"
	"cloudforge/core/internal/projects"
	"github.com/zalando/go-keyring"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type environmentTestVault struct{ values map[string]string }

func (v *environmentTestVault) Get(id string) (string, error) {
	value, ok := v.values[id]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return value, nil
}
func (v *environmentTestVault) Set(id, value string) error { v.values[id] = value; return nil }
func (v *environmentTestVault) Delete(id string) error     { delete(v.values, id); return nil }

func TestEnvironmentReachesProjectProcess(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node not installed")
	}
	for _, mode := range []string{"file", "exclusive"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"environment-fixture","version":"1.0.0","private":true}`), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "main.cjs"), []byte(`console.log('environment-value='+process.env.DB_HOST)`), 0600); err != nil {
				t.Fatal(err)
			}
			if mode == "file" {
				if err := os.WriteFile(filepath.Join(root, ".env"), []byte("DB_HOST=file-host\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			store, err := persistence.Open(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			store.EnvironmentVault = &environmentTestVault{values: map[string]string{}}
			p := model.Project{Name: "Environment", Runtime: "node", Path: root, StartCommand: []string{"node", "main.cjs"}, RestartPolicy: "never"}
			if err := projects.New(&p); err != nil {
				t.Fatal(err)
			}
			if err := store.SaveProject(p); err != nil {
				t.Fatal(err)
			}
			if mode == "exclusive" {
				if err := store.SetEnvironment(p.ID, []model.EnvironmentVariable{{Key: "DB_HOST", Value: "memory-host"}}); err != nil {
					t.Fatal(err)
				}
			}
			sup := New(store, t.TempDir())
			sup.Add(p)
			defer sup.Shutdown()
			if _, err := sup.Action(p.ID, "start"); err != nil {
				t.Fatal(err)
			}
			want := "environment-value=file-host"
			if mode == "exclusive" {
				want = "environment-value=memory-host"
			}
			await(t, 30*time.Second, func() bool {
				logs, _ := sup.Logs(p.ID)
				for _, line := range logs {
					if line.Message == want {
						return true
					}
				}
				return false
			})
		})
	}
}
