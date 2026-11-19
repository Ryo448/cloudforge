package databases

import (
	"cloudforge/core/internal/processes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
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
	id := hex.EncodeToString(m.localKey[:8])
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	relays := make(map[string]postgresLANRelay)
	defer func() {
		for _, relay := range relays {
			relay.stop()
		}
	}()
	var lastPorts, allowed []int
	var nextCheck time.Time
	var result <-chan error
	failed := false
	for {
		ports, databases, snapshotErr := m.postgresLANInstances()
		changed := !slices.Equal(ports, lastPorts)
		if snapshotErr == nil && result == nil && (changed || time.Now().After(nextCheck)) {
			if changed {
				failed = false
			}
			lastPorts = ports
			nextCheck = time.Now().Add(time.Minute)
			var status string
			probe, cancel := context.WithTimeout(ctx, 10*time.Second)
			state, err := readPostgresFirewall(probe, id)
			cancel()
			allowed = state.ports
			switch {
			case err != nil || state.policy:
				if len(ports) > 0 {
					status = "policy"
				}
			case !slices.Equal(ports, allowed) || (len(ports) == 0 && state.exists):
				status = "consent"
				if failed {
					status = "blocked"
				}
			default:
				failed = false
			}
			m.mu.Lock()
			m.firewallPorts, m.firewallTarget = allowed, ports
			if m.firewallStatus != "pending" {
				m.firewallStatus = status
			}
			m.mu.Unlock()
		}
		m.reconcilePostgresLANRelays(ctx, databases, allowed, relays)
		select {
		case <-ctx.Done():
			return
		case approved := <-m.firewallApproval:
			current, _, err := m.postgresLANInstances()
			if err != nil || !slices.Equal(approved, current) {
				m.mu.Lock()
				m.firewallStatus = "checking"
				m.mu.Unlock()
				nextCheck = time.Time{}
				continue
			}
			done := make(chan error, 1)
			result = done
			go func() {
				apply, cancel := context.WithTimeout(ctx, 2*time.Minute)
				defer cancel()
				done <- authorizePostgresFirewall(apply, id, approved)
			}()
		case err := <-result:
			result = nil
			// Treat the attempt as unsuccessful until the next read confirms the rule.
			failed = true
			if err != nil && ctx.Err() == nil {
				log.Printf("bancos: alteração do firewall para PostgreSQL: %v", err)
			}
			m.mu.Lock()
			m.firewallStatus = "checking"
			m.mu.Unlock()
			nextCheck = time.Time{}
		case <-ticker.C:
		}
	}
}

type postgresFirewallState struct {
	ports  []int
	exists bool
	policy bool
}

func readPostgresFirewall(ctx context.Context, id string) (postgresFirewallState, error) {
	out, err := runPostgresFirewall(ctx, id, "-", false)
	if err != nil {
		return postgresFirewallState{}, err
	}
	switch value := strings.TrimSpace(string(out)); value {
	case "absent":
		return postgresFirewallState{}, nil
	case "invalid":
		return postgresFirewallState{exists: true}, nil
	case "policy":
		return postgresFirewallState{policy: true}, nil
	default:
		ports, err := firewallPortList(value)
		return postgresFirewallState{ports: ports, exists: true}, err
	}
}

func authorizePostgresFirewall(ctx context.Context, id string, ports []int) error {
	values := make([]string, 0, len(ports))
	for _, port := range ports {
		values = append(values, strconv.Itoa(port))
	}
	argument := strings.Join(values, ",")
	if argument == "" {
		argument = "-"
	}
	if windows.GetCurrentProcessToken().IsElevated() {
		_, err := runPostgresFirewall(ctx, id, argument, true)
		return err
	}
	core, err := os.Executable()
	if err != nil {
		return err
	}
	// The GUI Core is the executable shown in UAC. Only this helper gets elevated.
	script := "$ErrorActionPreference='Stop'; $p = Start-Process -FilePath " + psQuote(core) +
		" -Verb RunAs -WindowStyle Hidden -Wait -PassThru -ArgumentList '--postgres-lan-firewall'," +
		psQuote(id) + "," + psQuote(argument) + "; exit $p.ExitCode"
	_, err = runFirewallPowerShell(ctx, script)
	return err
}

func ApplyLANFirewallHelper(args []string) error {
	if len(args) != 2 || len(args[0]) != 16 || !windows.GetCurrentProcessToken().IsElevated() {
		return errors.New("autorização do Windows necessária")
	}
	if _, err := hex.DecodeString(args[0]); err != nil {
		return err
	}
	if _, err := firewallPortList(args[1]); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := runPostgresFirewall(ctx, args[0], args[1], true)
	return err
}

func firewallPortList(value string) ([]int, error) {
	if value == "-" {
		return nil, nil
	}
	var ports []int
	for _, entry := range strings.Split(value, ",") {
		port, err := strconv.Atoi(entry)
		if err != nil || port < 1 || port > 65535 || strconv.Itoa(port) != entry {
			return nil, errors.New("porta inválida na regra do PostgreSQL")
		}
		ports = append(ports, port)
	}
	slices.Sort(ports)
	return slices.Compact(ports), nil
}

func runPostgresFirewall(ctx context.Context, id, ports string, apply bool) ([]byte, error) {
	core, err := os.Executable()
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("Cloudforge PostgreSQL LAN %s", id)
	script := "& {\n" + postgresFirewallScript + "\n} " + psQuote(name) + " " + psQuote(ports) + " " + psQuote(core)
	if apply {
		script += " -Apply"
	}
	return runFirewallPowerShell(ctx, script)
}

func psQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func runFirewallPowerShell(ctx context.Context, script string) ([]byte, error) {
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return nil, err
	}
	executable := filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe")
	words := utf16.Encode([]rune(script))
	raw := make([]byte, 2*len(words))
	for i, word := range words {
		binary.LittleEndian.PutUint16(raw[2*i:], word)
	}
	cmd := exec.CommandContext(ctx, executable, "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(raw))
	processes.ConfigureBackground(cmd)
	return cmd.Output()
}
