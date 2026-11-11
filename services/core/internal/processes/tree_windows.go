//go:build windows

package processes

import (
	"golang.org/x/sys/windows"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

type Tree struct {
	job windows.Handle
	cmd *exec.Cmd
}

func Configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}
func Attach(cmd *exec.Cmd) (*Tree, error) {
	job, e := windows.CreateJobObject(nil, nil)
	if e != nil {
		return nil, e
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, e = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafePointer(&info)), uint32(unsafeSizeof(info))); e != nil {
		windows.CloseHandle(job)
		return nil, e
	}
	p, e := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if e != nil {
		windows.CloseHandle(job)
		return nil, e
	}
	defer windows.CloseHandle(p)
	if e = windows.AssignProcessToJobObject(job, p); e != nil {
		windows.CloseHandle(job)
		return nil, e
	}
	return &Tree{job, cmd}, nil
}
func (t *Tree) Stop(done <-chan struct{}) {
	_ = windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(t.cmd.Process.Pid))
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	_ = windows.TerminateJobObject(t.job, 1)
}
func (t *Tree) Close() { _ = windows.TerminateJobObject(t.job, 1); _ = windows.CloseHandle(t.job) }
func StopRecovered(pid int) error {
	cmd := exec.Command("taskkill.exe", "/PID", strconv.Itoa(pid), "/T", "/F")
	Configure(cmd)
	return cmd.Run()
}
