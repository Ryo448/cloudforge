package cloudflare

import (
	"cloudforge/core/internal/model"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const (
	panelCleanupOpKind = "cloudflare-panel-cleanup"
	panelCleanupOpID   = "panel-cleanup"
)

type panelCleanupOutcome int

const (
	panelCleanupRemoved panelCleanupOutcome = iota
	panelCleanupAbsent
	panelCleanupExternal
)

type panelCleanupPayload struct {
	Host     string `json:"host"`
	ZoneID   string `json:"zoneId"`
	TunnelID string `json:"tunnelId"`
	Mode     string `json:"mode"`
	RecordID string `json:"recordId,omitempty"`
}

// UnpublishPanel disables the local web panel on this installation while preserving
// all project HTTP routes, database TCP routes, and the Cloudflare tunnel itself.
func (m *Manager) UnpublishPanel(ctx context.Context) (string, error) {
	if !m.op.TryLock() {
		return "", errors.New("operação Cloudflare em andamento")
	}
	defer m.op.Unlock()

	old := m.Config()
	if old.TunnelID == "" {
		next := old
		next.PanelDisabled = true
		next.AdminHost = ""
		next.PanelCleanupError = ""
		if err := m.Store.SaveCloudConfig(next); err != nil {
			return "", err
		}
		m.mu.Lock()
		m.cfg = next
		m.mu.Unlock()
		return "", nil
	}

	oldHost := old.AdminHost
	oldZoneID := old.ZoneID
	if oldHost == "" {
		oldHost = old.RetiredAdminHost
		oldZoneID = old.RetiredAdminZoneID
	}

	next := old
	next.PanelDisabled = true
	next.AdminHost = ""
	if oldHost != "" {
		next.RetiredAdminHost = oldHost
		next.RetiredAdminZoneID = oldZoneID
	}
	next.PanelCleanupError = ""

	if err := m.Store.SaveCloudConfig(next); err != nil {
		return "", err
	}
	m.mu.Lock()
	wasRunning := m.cancel != nil
	m.cfg = next
	m.mu.Unlock()

	var ingressErr error
	if next.Mode != "local" {
		routes, err := m.Store.Routes()
		if err != nil {
			ingressErr = err
		} else {
			ingressErr = m.ingress(ctx, next, routes)
		}
	} else if wasRunning {
		m.stopConnector()
		m.Start()
	}

	if oldHost == "" {
		if ingressErr != nil {
			return m.recordPanelCleanupPending(next, "", "", ingressErr)
		}
		return "", nil
	}

	outcome, dnsErr := m.retirePanelDNS(ctx, next, oldZoneID, oldHost, next.RetiredAdminRecord)
	if ingressErr != nil || dnsErr != nil {
		combined := dnsErr
		if ingressErr != nil && dnsErr != nil {
			combined = fmt.Errorf("ingresso: %v; DNS: %v", ingressErr, dnsErr)
		} else if ingressErr != nil {
			combined = ingressErr
		}
		return m.recordPanelCleanupPending(next, oldZoneID, oldHost, combined)
	}

	next.PanelCleanupError = ""
	_ = m.Store.SaveCloudConfig(next)
	m.mu.Lock()
	m.cfg = next
	m.mu.Unlock()
	_ = m.Store.Operation(panelCleanupOpID, panelCleanupOpKind, "complete", panelCleanupPayload{
		Host: oldHost, ZoneID: oldZoneID, TunnelID: next.TunnelID, Mode: next.Mode,
	}, "")

	if outcome == panelCleanupExternal {
		return fmt.Sprintf("O painel foi bloqueado localmente, mas o CNAME %s não aponta mais para este túnel e foi preservado.", oldHost), nil
	}
	return "", nil
}

func (m *Manager) recordPanelCleanupPending(cfg model.CloudConfig, zoneID, host string, cause error) (string, error) {
	msg := cause.Error()
	cfg.PanelCleanupError = msg
	if host != "" {
		cfg.RetiredAdminHost = host
		cfg.RetiredAdminZoneID = zoneID
	}
	_ = m.Store.SaveCloudConfig(cfg)
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
	_ = m.Store.Operation(panelCleanupOpID, panelCleanupOpKind, "needs-cleanup", panelCleanupPayload{
		Host: host, ZoneID: zoneID, TunnelID: cfg.TunnelID, Mode: cfg.Mode, RecordID: cfg.RetiredAdminRecord,
	}, msg)
	warning := fmt.Sprintf("O painel antigo (%s) foi bloqueado no roteador, mas a remoção do DNS na Cloudflare falhou (%s). Você pode tentar novamente.", host, msg)
	if host == "" {
		warning = fmt.Sprintf("O painel foi bloqueado no roteador, mas a atualização na Cloudflare falhou (%s). Você pode tentar novamente.", msg)
	}
	return warning, nil
}

// RetryPanelCleanup retries pending DNS/ingress cleanup for an unpublished panel
// while keeping the router block active.
func (m *Manager) RetryPanelCleanup(ctx context.Context) error {
	if !m.op.TryLock() {
		return errors.New("operação Cloudflare em andamento")
	}
	defer m.op.Unlock()
	return m.retryPanelCleanupLocked(ctx)
}

func (m *Manager) retryPanelCleanupLocked(ctx context.Context) error {
	cfg := m.Config()
	if !cfg.PanelDisabled || cfg.TunnelID == "" {
		_ = m.Store.Operation(panelCleanupOpID, panelCleanupOpKind, "complete", panelCleanupPayload{}, "")
		return nil
	}
	if cfg.Mode != "local" {
		routes, err := m.Store.Routes()
		if err != nil {
			return err
		}
		if err := m.ingress(ctx, cfg, routes); err != nil {
			cfg.PanelCleanupError = err.Error()
			_ = m.Store.SaveCloudConfig(cfg)
			m.mu.Lock()
			m.cfg = cfg
			m.mu.Unlock()
			_ = m.Store.Operation(panelCleanupOpID, panelCleanupOpKind, "needs-cleanup", panelCleanupPayload{
				Host: cfg.RetiredAdminHost, ZoneID: cfg.RetiredAdminZoneID, TunnelID: cfg.TunnelID, Mode: cfg.Mode,
			}, err.Error())
			return err
		}
	}
	if cfg.RetiredAdminHost != "" {
		_, err := m.retirePanelDNS(ctx, cfg, cfg.RetiredAdminZoneID, cfg.RetiredAdminHost, cfg.RetiredAdminRecord)
		if err != nil {
			cfg.PanelCleanupError = err.Error()
			_ = m.Store.SaveCloudConfig(cfg)
			m.mu.Lock()
			m.cfg = cfg
			m.mu.Unlock()
			_ = m.Store.Operation(panelCleanupOpID, panelCleanupOpKind, "needs-cleanup", panelCleanupPayload{
				Host: cfg.RetiredAdminHost, ZoneID: cfg.RetiredAdminZoneID, TunnelID: cfg.TunnelID, Mode: cfg.Mode,
			}, err.Error())
			return err
		}
	}
	cfg.PanelCleanupError = ""
	if err := m.Store.SaveCloudConfig(cfg); err != nil {
		return err
	}
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
	return m.Store.Operation(panelCleanupOpID, panelCleanupOpKind, "complete", panelCleanupPayload{
		Host: cfg.RetiredAdminHost, ZoneID: cfg.RetiredAdminZoneID, TunnelID: cfg.TunnelID, Mode: cfg.Mode,
	}, "")
}

// RepublishPanel explicitly republishes a standalone web panel on an existing tunnel
// after the installation was detached from a primary panel.
func (m *Manager) RepublishPanel(ctx context.Context, domain, sub string) (model.CloudConfig, error) {
	if !m.op.TryLock() {
		return model.CloudConfig{}, errors.New("operação Cloudflare em andamento")
	}
	defer m.op.Unlock()

	old := m.Config()
	if !old.PanelDisabled {
		return old, errors.New("o painel desta instalação já está publicado")
	}
	if old.TunnelID == "" {
		next := old
		next.PanelDisabled = false
		next.PanelCleanupError = ""
		next.RetiredAdminHost = ""
		next.RetiredAdminZoneID = ""
		next.RetiredAdminRecord = ""
		if err := m.Store.SaveCloudConfig(next); err != nil {
			return old, err
		}
		m.mu.Lock()
		m.cfg = next
		m.mu.Unlock()
		_ = m.Store.Operation(panelCleanupOpID, panelCleanupOpKind, "cancelled", panelCleanupPayload{}, "")
		return next, nil
	}

	zone, host, err := m.destination(old, domain, sub)
	if err != nil {
		return old, err
	}
	if m.databaseTunnelHostInUse(host) {
		return old, errors.New("esse endereço é usado por um túnel de banco de dados")
	}
	routes, err := m.Store.Routes()
	if err != nil {
		return old, err
	}
	for _, route := range routes {
		if route.Hostname == host {
			return old, errors.New("esse endereço é usado por um projeto")
		}
	}

	next := old
	next.PanelDisabled = false
	next.ZoneID = zone
	next.AdminHost = host
	next.Domain = strings.SplitN(host, ".", 2)[1]
	next.RetiredAdminHost = ""
	next.RetiredAdminZoneID = ""
	next.RetiredAdminRecord = ""
	next.PanelCleanupError = ""

	if old.Mode == "local" {
		if err = m.localDNS(ctx, next, host); err != nil {
			return old, err
		}
	} else {
		// In remote mode, allow reusing an existing owned CNAME if it still points to our tunnel, otherwise create a new CNAME.
		records, err := m.Client.Records(ctx, zone, host)
		if err != nil {
			return old, err
		}
		owned := false
		for _, rec := range records {
			if rec.Type == "CNAME" && strings.EqualFold(rec.Name, host) &&
				strings.EqualFold(strings.TrimSuffix(rec.Content, "."), old.TunnelID+".cfargotunnel.com") &&
				rec.Comment == "Cloudforge "+m.Installation {
				owned = true
				break
			}
		}
		if !owned {
			if _, err = m.dns(ctx, next, host); err != nil {
				return old, err
			}
		}
		if err = m.ingress(ctx, next, routes); err != nil {
			if !owned {
				_ = m.retireDNS(ctx, next, model.Route{ZoneID: zone, Hostname: host})
			}
			return old, err
		}
	}

	if err = m.Store.SaveCloudConfig(next); err != nil {
		if old.Mode != "local" {
			_ = m.ingress(ctx, old, routes)
		}
		return old, err
	}
	m.mu.Lock()
	wasRunning := m.cancel != nil
	m.cfg = next
	m.mu.Unlock()
	_ = m.Store.Operation(panelCleanupOpID, panelCleanupOpKind, "cancelled", panelCleanupPayload{}, "")

	if next.Mode == "local" && wasRunning {
		m.stopConnector()
		m.Start()
	}
	return next, nil
}

// retirePanelDNS verifies that the DNS CNAME still belongs to this installation's tunnel
// before removing it, and returns an error when Cloudflare cannot be reached or rejects deletion.
func (m *Manager) retirePanelDNS(ctx context.Context, cfg model.CloudConfig, zoneID, host, recordID string) (panelCleanupOutcome, error) {
	if host == "" || cfg.TunnelID == "" || zoneID == "" {
		return panelCleanupAbsent, nil
	}
	call := m.Client.Call
	if cfg.Mode == "local" {
		_, cert, err := m.certificateForZone(zoneID)
		if err != nil || cert.AccountID != cfg.AccountID {
			return panelCleanupAbsent, errors.New("certificado da zona Cloudflare indisponível para remover o DNS do painel")
		}
		call = func(ctx context.Context, method, path string, body, out any) error {
			return m.Client.callToken(ctx, cert.Token, method, path, body, out)
		}
	}
	path := "/zones/" + url.PathEscape(zoneID) + "/dns_records"
	var records []DNS
	if err := call(ctx, "GET", path+"?name="+url.QueryEscape(host), nil, &records); err != nil {
		return panelCleanupAbsent, err
	}
	if len(records) == 0 {
		return panelCleanupAbsent, nil
	}
	expectedTarget := strings.ToLower(cfg.TunnelID + ".cfargotunnel.com")
	for _, record := range records {
		if record.Type != "CNAME" || !strings.EqualFold(record.Name, host) ||
			strings.ToLower(strings.TrimSuffix(record.Content, ".")) != expectedTarget {
			continue
		}
		if recordID != "" && recordID != record.ID {
			continue
		}
		if cfg.Mode != "local" && record.Comment != "Cloudforge "+m.Installation {
			continue
		}
		if record.ID == "" {
			return panelCleanupAbsent, errors.New("registro DNS sem identificador")
		}
		if err := call(ctx, "DELETE", path+"/"+url.PathEscape(record.ID), nil, nil); err != nil {
			return panelCleanupAbsent, err
		}
		return panelCleanupRemoved, nil
	}
	return panelCleanupExternal, nil
}
