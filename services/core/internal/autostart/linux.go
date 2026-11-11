//go:build !windows

package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func Configure(enabled bool, dataDir, webDir string) error {
	config, e := os.UserConfigDir()
	if e != nil {
		return e
	}
	path := filepath.Join(config, "systemd", "user", "cloudforge.service")
	if !enabled {
		return exec.Command("systemctl", "--user", "disable", "cloudforge.service").Run()
	}
	binary, e := os.Executable()
	if e != nil {
		return e
	}
	quote := func(s string) string {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`).Replace(s) + `"`
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	unit := fmt.Sprintf("[Unit]\nDescription=Cloudforge Core\nAfter=network-online.target\n[Service]\nType=simple\nExecStart=%s --data %s --web-dir %s\nRestart=on-failure\nRestartSec=5\n[Install]\nWantedBy=default.target\n", quote(binary), quote(dataDir), quote(webDir))
	if e = os.WriteFile(path, []byte(unit), 0600); e != nil {
		return e
	}
	if out, e := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); e != nil {
		return fmt.Errorf("systemd de usuário indisponível: %s", out)
	}
	if out, e := exec.Command("systemctl", "--user", "enable", "cloudforge.service").CombinedOutput(); e != nil {
		return fmt.Errorf("não foi possível ativar systemd: %s", out)
	}
	return nil
}
func Enabled() bool {
	return exec.Command("systemctl", "--user", "is-enabled", "--quiet", "cloudforge.service").Run() == nil
}
