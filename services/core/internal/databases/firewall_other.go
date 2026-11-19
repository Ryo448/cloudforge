//go:build !windows

package databases

import "context"

func (m *Manager) monitorLANFirewall(context.Context) {}
