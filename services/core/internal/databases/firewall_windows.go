package databases

import (
	"cloudforge/core/internal/processes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

//go:embed postgres_firewall.ps1
var postgresFirewallScript string

func (m *Manager) monitorLANFirewall(ctx context.Context) {
	ruleName := fmt.Sprintf("Cloudforge PostgreSQL LAN %x", m.localKey[:8])
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var lastPorts []int
	var nextCheck time.Time
	attempted := false
	for {
		m.mu.Lock()
		records, err := m.records("")
		m.mu.Unlock()
		if err == nil {
			var ports []int
			for _, r := range records {
				if r.Engine == "postgresql" && r.ReservedPort > 0 && r.ReservedPort <= 65535 {
					ports = append(ports, r.ReservedPort)
				}
			}
			slices.Sort(ports)
			changed := !slices.Equal(ports, lastPorts)
			if changed {
				lastPorts = ports
				attempted = false
			}
			if changed || time.Now().After(nextCheck) {
				mismatch, err := checkPostgresFirewall(ctx, ruleName, ports, !attempted)
				if err == nil {
					attempted = false
				} else if mismatch {
					attempted = true
				}
				nextCheck = time.Now().Add(time.Minute)
				m.mu.Lock()
				if err == nil {
					m.firewallPorts = ports
				} else {
					m.firewallPorts = nil
					if !m.firewallIssue {
						log.Printf("bancos: falha ao configurar firewall para PostgreSQL: %v", err)
					}
				}
				m.firewallIssue = err != nil
				m.mu.Unlock()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func dockerBackendPath() string {
	dirs := dockerDesktopDirectories()
	if cli, err := dockerExecutable(); err == nil {
		dirs = append(dirs, filepath.Dir(filepath.Dir(filepath.Dir(cli))))
	}
	for _, dir := range dirs {
		path := filepath.Join(dir, "resources", "com.docker.backend.exe")
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func checkPostgresFirewall(ctx context.Context, name string, ports []int, allowPrompt bool) (bool, error) {
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return false, err
	}
	powershell := filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe")
	program := dockerBackendPath()
	if len(ports) != 0 && program == "" {
		return false, errors.New("processo do Docker Desktop não encontrado")
	}
	values := make([]string, 0, len(ports))
	for _, port := range ports {
		values = append(values, strconv.Itoa(port))
	}
	psQuote := func(value string) string {
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	}
	checkScript := "& {\n" + postgresFirewallScript + "\n} " + psQuote(name) + " " + psQuote(strings.Join(values, ",")) + " " + psQuote(program)
	probe := func() error {
		check, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return runFirewallPowerShell(check, powershell, checkScript)
	}
	err = probe()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 10 {
		return false, err
	}

	script := checkScript + " -Apply"
	if !windows.GetCurrentProcessToken().IsElevated() {
		if !allowPrompt {
			return true, errors.New("alteração da regra ainda não autorizada")
		}
		script = "$ErrorActionPreference='Stop'; $p = Start-Process -FilePath " + psQuote(powershell) +
			" -Verb RunAs -WindowStyle Hidden -Wait -PassThru -ArgumentList '-NoProfile','-NonInteractive','-EncodedCommand','" +
			encodedPowerShell(script) + "'; exit $p.ExitCode"
	}
	apply, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err = runFirewallPowerShell(apply, powershell, script); err != nil {
		return true, err
	}
	return true, probe()
}

func runFirewallPowerShell(ctx context.Context, executable, script string) error {
	cmd := exec.CommandContext(ctx, executable, "-NoProfile", "-NonInteractive", "-EncodedCommand", encodedPowerShell(script))
	processes.ConfigureBackground(cmd)
	return cmd.Run()
}

func encodedPowerShell(script string) string {
	words := utf16.Encode([]rune(script))
	raw := make([]byte, 2*len(words))
	for i, word := range words {
		binary.LittleEndian.PutUint16(raw[2*i:], word)
	}
	return base64.StdEncoding.EncodeToString(raw)
}
