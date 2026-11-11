//go:build windows

package supervisor

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"os/exec"
)

func terminalCommand(cmd *exec.Cmd) (*exec.Cmd, io.WriteCloser, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	config, err := json.Marshal(struct {
		Path string   `json:"path"`
		Args []string `json:"args"`
		Dir  string   `json:"dir"`
	}{cmd.Path, cmd.Args[1:], cmd.Dir})
	if err != nil {
		return nil, nil, err
	}
	helper := exec.Command(self, "--pty-child")
	helper.Dir = cmd.Dir
	env := os.Environ()
	if cmd.Env != nil {
		env = cmd.Env
	}
	helper.Env = append(env, "TERM=xterm-256color", "CLOUDFORGE_PTY_CHILD="+base64.StdEncoding.EncodeToString(config))
	input, err := helper.StdinPipe()
	return helper, input, err
}
