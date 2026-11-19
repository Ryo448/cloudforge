//go:build !windows

package databases

import (
	"context"
	"errors"
)

func (m *Manager) monitorLANFirewall(context.Context) {}

func ApplyLANFirewallHelper([]string) error {
	return errors.New("o firewall do Windows está indisponível")
}
