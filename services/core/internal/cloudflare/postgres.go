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

const postgresSetting = "cloudflare_postgres"

var accessAudience = regexp.MustCompile(`^[a-f0-9]{64}$`)

type PostgresInput struct {
	Domain    string `json:"domain"`
	Subdomain string `json:"subdomain"`
	Team      string `json:"team"`
	Audience  string `json:"audience"`
}

type PostgresTunnel struct {
	Owner      string `json:"owner"`
	DatabaseID string `json:"databaseId"`
	TunnelID   string `json:"tunnelId"`
	ZoneID     string `json:"zoneId"`
	Hostname   string `json:"hostname"`
	Team       string `json:"team"`
	Audience   string `json:"audience"`
	RecordID   string `json:"recordId,omitempty"`
}

type PostgresView struct {
	Hostname      string   `json:"hostname,omitempty"`
	Domains       []string `json:"domains,omitempty"`
	DefaultDomain string   `json:"defaultDomain,omitempty"`
	Warning       string   `json:"warning,omitempty"`
}

type postgresRelay struct {
	port int
	stop context.CancelFunc
}

func (m *Manager) PostgresTunnel(owner, id string, local bool) PostgresView {
	cfg := m.Config()
	view := PostgresView{}
	if local && cfg.TunnelID != "" {
		view.Domains = m.Domains()
		view.DefaultDomain = cfg.Domain
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, item := range m.postgresTunnels {
		if item.Owner == owner && item.DatabaseID == id && item.TunnelID == cfg.TunnelID && validDomain(item.Hostname) {
			view.Hostname = item.Hostname
			break
		}
	}
	return view
}

func (m *Manager) postgresHostInUse(host string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.ContainsFunc(m.postgresTunnels, func(item PostgresTunnel) bool { return item.Hostname == host })
}

func (m *Manager) AddPostgresTunnel(ctx context.Context, owner, id string, in PostgresInput) (PostgresView, error) {
	if !m.op.TryLock() {
		return PostgresView{}, errors.New("operação Cloudflare em andamento")
	}
	defer m.op.Unlock()
	if err := m.noPending(); err != nil {
		return PostgresView{}, err
	}
	cfg := m.Config()
	zone, host, err := m.destination(cfg, in.Domain, in.Subdomain)
	if err != nil {
		return PostgresView{}, err
	}
	team := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(in.Team)), ".cloudflareaccess.com")
	aud := strings.ToLower(strings.TrimSpace(in.Audience))
	if !label.MatchString(team) || !accessAudience.MatchString(aud) {
		return PostgresView{}, errors.New("informe a equipe e o AUD do aplicativo Cloudflare Access")
	}
	if existing := m.PostgresTunnel(owner, id, false); existing.Hostname != "" {
		return PostgresView{}, errors.New("desative a rota TCP existente antes de cadastrar outra")
	}
	if host == cfg.AdminHost || m.postgresHostInUse(host) {
		return PostgresView{}, errors.New("esse endereço já está em uso no Cloudforge")
	}
	routes, err := m.Store.Routes()
	if err != nil {
		return PostgresView{}, err
	}
	for _, route := range routes {
		if route.Hostname == host {
			return PostgresView{}, errors.New("esse endereço já está em uso no Cloudforge")
		}
	}
	if m.OpenPostgresRelay == nil {
		return PostgresView{}, errors.New("serviço de túnel PostgreSQL indisponível")
	}
	port, stop, err := m.OpenPostgresRelay(owner, id)
	if err != nil {
		return PostgresView{}, err
	}
	keep := false
	defer func() {
		if !keep {
			stop()
		}
	}()
	target := cfg
	target.ZoneID = zone
	item := PostgresTunnel{Owner: owner, DatabaseID: id, TunnelID: cfg.TunnelID, ZoneID: zone, Hostname: host, Team: team, Audience: aud}
	if cfg.Mode == "local" {
		err = m.localDNS(ctx, target, host)
	} else {
		var route model.Route
		route, err = m.dns(ctx, target, host)
		item.RecordID = route.DNSRecordID
	}
	if err != nil {
		return PostgresView{}, err
	}
	m.mu.Lock()
	next := append(slices.Clone(m.postgresTunnels), item)
	err = m.Store.Set(postgresSetting, next)
	if err == nil {
		m.postgresTunnels, m.postgresConfigured = next, true
		m.postgresRelays[id] = postgresRelay{port, stop}
		keep = true
	}
	m.mu.Unlock()
	if err != nil {
		_ = m.retireDNS(ctx, cfg, item.dnsRoute())
		return PostgresView{}, err
	}
	m.stopConnector()
	m.Start()
	return m.PostgresTunnel(owner, id, true), nil
}

func (m *Manager) RemovePostgresTunnel(ctx context.Context, owner, id string) (PostgresView, error) {
	if m.PostgresTunnel(owner, id, false).Hostname == "" {
		return m.PostgresTunnel(owner, id, true), nil
	}
	if !m.op.TryLock() {
		return PostgresView{}, errors.New("operação Cloudflare em andamento")
	}
	defer m.op.Unlock()
	cfg := m.Config()
	m.mu.Lock()
	index := slices.IndexFunc(m.postgresTunnels, func(item PostgresTunnel) bool {
		return item.Owner == owner && item.DatabaseID == id && item.TunnelID == cfg.TunnelID
	})
	if index < 0 {
		m.mu.Unlock()
		return m.PostgresTunnel(owner, id, true), nil
	}
	item := m.postgresTunnels[index]
	next := slices.Delete(slices.Clone(m.postgresTunnels), index, index+1)
	err := m.Store.Set(postgresSetting, next)
	if err == nil {
		m.postgresTunnels, m.postgresConfigured = next, true
	}
	m.mu.Unlock()
	if err != nil {
		return PostgresView{}, err
	}
	// Disconnect the old connector before releasing the private port in its ingress.
	m.stopConnector()
	m.mu.Lock()
	relay := m.postgresRelays[id]
	delete(m.postgresRelays, id)
	m.mu.Unlock()
	if relay.stop != nil {
		relay.stop()
	}
	m.Start()
	view := m.PostgresTunnel(owner, id, true)
	view.Warning = m.retireDNS(ctx, cfg, item.dnsRoute())
	return view, nil
}

func (m *Manager) DeletePostgresTunnel(ctx context.Context, owner, id string) error {
	_, err := m.RemovePostgresTunnel(ctx, owner, id)
	return err
}

func (item PostgresTunnel) dnsRoute() model.Route {
	return model.Route{ZoneID: item.ZoneID, Hostname: item.Hostname, DNSRecordID: item.RecordID}
}

func (m *Manager) restorePostgresRelays(cfg model.CloudConfig) {
	if m.OpenPostgresRelay == nil {
		return
	}
	for _, item := range m.postgresTunnels {
		if item.TunnelID != cfg.TunnelID || m.postgresRelays[item.DatabaseID].port != 0 {
			continue
		}
		port, stop, err := m.OpenPostgresRelay(item.Owner, item.DatabaseID)
		if err == nil {
			m.postgresRelays[item.DatabaseID] = postgresRelay{port, stop}
		}
	}
}

type ingressRule struct {
	Hostname      string         `json:"hostname,omitempty"`
	Service       string         `json:"service"`
	OriginRequest map[string]any `json:"originRequest,omitempty"`
}

func (m *Manager) postgresIngress(cfg model.CloudConfig) []ingressRule {
	m.mu.Lock()
	defer m.mu.Unlock()
	var rules []ingressRule
	for _, item := range m.postgresTunnels {
		if item.TunnelID != cfg.TunnelID || !validDomain(item.Hostname) {
			continue
		}
		port := m.postgresRelays[item.DatabaseID].port
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

func (m *Manager) syncPostgresIngress(ctx context.Context) error {
	m.mu.Lock()
	configured := m.postgresConfigured
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
