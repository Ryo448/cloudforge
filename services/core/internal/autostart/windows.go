//go:build windows

package autostart

import (
	"fmt"
	"golang.org/x/sys/windows/registry"
	"os"
	"strings"
)

const runPath = `Software\Microsoft\Windows\CurrentVersion\Run`

func Configure(enabled bool, dataDir, webDir string) error {
	key, _, e := registry.CreateKey(registry.CURRENT_USER, runPath, registry.SET_VALUE)
	if e != nil {
		return fmt.Errorf("não foi possível configurar a inicialização do usuário: %w", e)
	}
	defer key.Close()
	if !enabled {
		e = key.DeleteValue("CloudforgeCore")
		if e == registry.ErrNotExist {
			return nil
		}
		return e
	}
	binary, e := os.Executable()
	if e != nil {
		return e
	}
	args := []string{binary, "--data", dataDir, "--web-dir", webDir}
	for i, arg := range args {
		if strings.ContainsAny(arg, "\r\n") {
			return fmt.Errorf("caminho inválido")
		}
		args[i] = execEscape(arg)
	}
	return key.SetStringValue("CloudforgeCore", strings.Join(args, " "))
}
func execEscape(v string) string { return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"` }
func Enabled() bool {
	key, e := registry.OpenKey(registry.CURRENT_USER, runPath, registry.QUERY_VALUE)
	if e != nil {
		return false
	}
	defer key.Close()
	_, _, e = key.GetStringValue("CloudforgeCore")
	return e == nil
}
