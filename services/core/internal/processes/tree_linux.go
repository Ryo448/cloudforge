//go:build !windows

package processes

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

type Tree struct{ cmd *exec.Cmd }

func Configure(cmd *exec.Cmd)             { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func Attach(cmd *exec.Cmd) (*Tree, error) { return &Tree{cmd}, nil }
func (t *Tree) Stop(done <-chan struct{}) {
	_ = syscall.Kill(-t.cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	_ = syscall.Kill(-t.cmd.Process.Pid, syscall.SIGKILL)
}
func StopRecovered(pid int) error {
	group, e := syscall.Getpgid(pid)
	if e != nil {
		return e
	}
	if group != pid {
		return errors.New("processo anterior não é líder do grupo esperado")
	}
	if e = syscall.Kill(-pid, syscall.SIGTERM); e != nil {
		return e
	}
	for i := 0; i < 50; i++ {
		if syscall.Kill(pid, 0) != nil {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return syscall.Kill(-pid, syscall.SIGKILL)
}
func (t *Tree) Close() { _ = syscall.Kill(-t.cmd.Process.Pid, syscall.SIGKILL) }
