package cloudflare

import (
	"cloudforge/core/internal/model"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Preserve routes from releases that published only PostgreSQL.
const databaseTunnelSetting = "cloudflare_postgres"

var accessAudience = regexp.MustCompile(`^[a-f0-9]{64}$`)

type DatabaseTunnelInput struct {
	Domain    string `json:"domain"`
	Subdomain string `json:"subdomain"`
	Team      string `json:"team"`
	Audience  string `json:"audience"`
}

type DatabaseTunnel struct {
	Owner      string `json:"owner"`
	DatabaseID string `json:"databaseId"`
	TunnelID   string `json:"tunnelId"`
	ZoneID     string `json:"zoneId"`
	Hostname   string `json:"hostname"`
	Team       string `json:"team"`
	Audience   string `json:"audience"`
	RecordID   string `json:"recordId,omitempty"`
}

type DatabaseTunnelView struct {
	Hostname      string   `json:"hostname,omitempty"`
	Domains       []string `json:"domains,omitempty"`
	DefaultDomain string   `json:"defaultDomain,omitempty"`
	Warning       string   `json:"warning,omitempty"`
}

type databaseRelay struct {
	port int
	stop context.CancelFunc
}

func (m *Manager) DatabaseTunnel(owner, id string, local bool) DatabaseTunnelView {
	cfg := m.Config()
	view := DatabaseTunnelView{}
	if local && cfg.TunnelID != "" {
		view.Domains = m.Domains()
		view.DefaultDomain = cfg.Domain
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, item := range m.databaseTunnels {
		if item.Owner == owner && item.DatabaseID == id && item.TunnelID == cfg.TunnelID && validDomain(item.Hostname) {
			view.Hostname = item.Hostname
			break
		}
	}
	return view
}

func (m *Manager) databaseTunnelHostInUse(host string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.ContainsFunc(m.databaseTunnels, func(item DatabaseTunnel) bool { return item.Hostname == host })
}

func (m *Manager) AddDatabaseTunnel(ctx context.Context, owner, id string, in DatabaseTunnelInput) (DatabaseTunnelView, error) {
	if !m.op.TryLock() {
		return DatabaseTunnelView{}, errors.New("operação Cloudflare em andamento")
	}
	defer m.op.Unlock()
	if err := m.noPending(); err != nil {
		return DatabaseTunnelView{}, err
	}
	cfg := m.Config()
	zone, host, err := m.destination(cfg, in.Domain, in.Subdomain)
	if err != nil {
		return DatabaseTunnelView{}, err
	}
	team := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(in.Team)), ".cloudflareaccess.com")
	aud := strings.ToLower(strings.TrimSpace(in.Audience))
	if !label.MatchString(team) || !accessAudience.MatchString(aud) {
		return DatabaseTunnelView{}, errors.New("informe a equipe e o AUD do aplicativo Cloudflare Access")
	}
	if existing := m.DatabaseTunnel(owner, id, false); existing.Hostname != "" {
		return DatabaseTunnelView{}, errors.New("desative a rota TCP existente antes de cadastrar outra")
	}
	if (cfg.AdminHost != "" && host == cfg.AdminHost) || (cfg.RetiredAdminHost != "" && cfg.PanelCleanupError != "" && host == cfg.RetiredAdminHost) || m.databaseTunnelHostInUse(host) {
		return DatabaseTunnelView{}, errors.New("esse endereço já está em uso no Cloudforge")
	}
	routes, err := m.Store.Routes()
	if err != nil {
		return DatabaseTunnelView{}, err
	}
	for _, route := range routes {
		if route.Hostname == host {
			return DatabaseTunnelView{}, errors.New("esse endereço já está em uso no Cloudforge")
		}
	}
	if m.OpenDatabaseRelay == nil {
		return DatabaseTunnelView{}, errors.New("serviço de túnel do banco indisponível")
	}
	port, stop, err := m.OpenDatabaseRelay(owner, id)
	if err != nil {
		return DatabaseTunnelView{}, err
	}
	keep := false
	defer func() {
		if !keep {
			stop()
		}
	}()
	target := cfg
	target.ZoneID = zone
	item := DatabaseTunnel{Owner: owner, DatabaseID: id, TunnelID: cfg.TunnelID, ZoneID: zone, Hostname: host, Team: team, Audience: aud}
	if cfg.Mode == "local" {
		err = m.localDNS(ctx, target, host)
	} else {
		var route model.Route
		route, err = m.dns(ctx, target, host)
		item.RecordID = route.DNSRecordID
	}
	if err != nil {
		return DatabaseTunnelView{}, err
	}
	m.mu.Lock()
	next := append(slices.Clone(m.databaseTunnels), item)
	err = m.Store.Set(databaseTunnelSetting, next)
	if err == nil {
		m.databaseTunnels, m.databaseTunnelConfigured = next, true
		m.databaseRelays[id] = databaseRelay{port, stop}
		keep = true
	}
	m.mu.Unlock()
	if err != nil {
		_ = m.retireDNS(ctx, cfg, item.dnsRoute())
		return DatabaseTunnelView{}, err
	}
	m.stopConnector()
	m.Start()
	return m.DatabaseTunnel(owner, id, true), nil
}

func (m *Manager) RemoveDatabaseTunnel(ctx context.Context, owner, id string) (DatabaseTunnelView, error) {
	if m.DatabaseTunnel(owner, id, false).Hostname == "" {
		return m.DatabaseTunnel(owner, id, true), nil
	}
	if !m.op.TryLock() {
		return DatabaseTunnelView{}, errors.New("operação Cloudflare em andamento")
	}
	defer m.op.Unlock()
	cfg := m.Config()
	m.mu.Lock()
	index := slices.IndexFunc(m.databaseTunnels, func(item DatabaseTunnel) bool {
		return item.Owner == owner && item.DatabaseID == id && item.TunnelID == cfg.TunnelID
	})
	if index < 0 {
		m.mu.Unlock()
		return m.DatabaseTunnel(owner, id, true), nil
	}
	item := m.databaseTunnels[index]
	next := slices.Delete(slices.Clone(m.databaseTunnels), index, index+1)
	err := m.Store.Set(databaseTunnelSetting, next)
	if err == nil {
		m.databaseTunnels, m.databaseTunnelConfigured = next, true
	}
	m.mu.Unlock()
	if err != nil {
		return DatabaseTunnelView{}, err
	}
	// Disconnect the old connector before releasing the private port in its ingress.
	m.stopConnector()
	m.mu.Lock()
	relay := m.databaseRelays[id]
	delete(m.databaseRelays, id)
	m.mu.Unlock()
	if relay.stop != nil {
		relay.stop()
	}
	m.Start()
	view := m.DatabaseTunnel(owner, id, true)
	view.Warning = m.retireDNS(ctx, cfg, item.dnsRoute())
	return view, nil
}

func (m *Manager) DeleteDatabaseTunnel(ctx context.Context, owner, id string) error {
	_, err := m.RemoveDatabaseTunnel(ctx, owner, id)
	return err
}

func (item DatabaseTunnel) dnsRoute() model.Route {
	return model.Route{ZoneID: item.ZoneID, Hostname: item.Hostname, DNSRecordID: item.RecordID}
}

func (m *Manager) restoreDatabaseRelays(cfg model.CloudConfig) {
	if m.OpenDatabaseRelay == nil {
		return
	}
	for _, item := range m.databaseTunnels {
		if item.TunnelID != cfg.TunnelID || m.databaseRelays[item.DatabaseID].port != 0 {
			continue
		}
		port, stop, err := m.OpenDatabaseRelay(item.Owner, item.DatabaseID)
		if err == nil {
			m.databaseRelays[item.DatabaseID] = databaseRelay{port, stop}
		}
	}
}

type ingressRule struct {
	Hostname      string         `json:"hostname,omitempty"`
	Service       string         `json:"service"`
	OriginRequest map[string]any `json:"originRequest,omitempty"`
}

func (m *Manager) databaseTunnelIngress(cfg model.CloudConfig) []ingressRule {
	m.mu.Lock()
	defer m.mu.Unlock()
	var rules []ingressRule
	for _, item := range m.databaseTunnels {
		if item.TunnelID != cfg.TunnelID || !validDomain(item.Hostname) {
			continue
		}
		port := m.databaseRelays[item.DatabaseID].port
		if port < 1 || port > 65535 || !label.MatchString(item.Team) || !accessAudience.MatchString(item.Audience) {
			rules = append(rules, ingressRule{Hostname: item.Hostname, Service: "http_status:503"})
			continue
		}
		rules = append(rules, ingressRule{Hostname: item.Hostname, Service: fmt.Sprintf("tcp://127.0.0.1:%d", port), OriginRequest: map[string]any{
			"access": map[string]any{"required": true, "teamName": item.Team, "audTag": []string{item.Audience}},
		}})
	}
	return rules
}

func (m *Manager) syncDatabaseTunnelIngress(ctx context.Context) error {
	m.mu.Lock()
	configured := m.databaseTunnelConfigured
	m.mu.Unlock()
	if !configured {
		return nil
	}
	if !m.op.TryLock() {
		return errors.New("operação Cloudflare em andamento")
	}
	defer m.op.Unlock()
	routes, err := m.Store.Routes()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// Synchronize before connecting: old rules may refer to ports from a prior Core.
	return m.ingress(ctx, m.Config(), routes)
}
