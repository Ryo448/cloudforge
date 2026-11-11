//go:build !windows

package supervisor

import (
	"io"
	"os"
	"os/exec"
)

func terminalCommand(cmd *exec.Cmd) (*exec.Cmd, io.WriteCloser, error) {
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env, "TERM=xterm-256color")
	input, err := cmd.StdinPipe()
	return cmd, input, err
}
