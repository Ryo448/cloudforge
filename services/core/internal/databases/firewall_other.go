//go:build !windows

package databases

import (
	"context"
	"errors"
	"time"
)

func (m *Manager) monitorDatabaseLAN(ctx context.Context) {
	relays := make(map[string]databaseLANRelay)
	defer func() {
		for _, relay := range relays {
			relay.stop()
		}
	}()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		ports, databases, _ := m.databaseLANInstances()
		m.reconcileDatabaseLANRelays(ctx, databases, ports, relays)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func ApplyLANFirewallHelper([]string) error {
	return errors.New("o firewall do Windows está indisponível")
}
