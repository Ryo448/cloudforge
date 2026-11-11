package cloudflare

import (
	"cloudforge/core/internal/auth"
	"cloudforge/core/internal/model"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (m *Manager) zoneDirectory() string { return filepath.Join(m.localHome(), "zones") }

func (m *Manager) certificateForZone(id string) (string, certificate, error) {
	path := certPath(m.localHome())
	if cert, err := readCertificate(path); err == nil && cert.ZoneID == id {
		return path, cert, nil
	}
	if !resourceID.MatchString(id) {
		return "", certificate{}, errors.New("domínio inválido")
	}
	path = filepath.Join(m.zoneDirectory(), id+".pem")
	cert, err := readCertificate(path)
	if err == nil && cert.ZoneID != id {
		err = errors.New("autorize este domínio novamente na Cloudflare")
	}
	return path, cert, err
}

func (m *Manager) authorizeAdditionalZone(ctx context.Context, cfg model.CloudConfig, cert certificate, path string) error {
	if cfg.Mode != "local" || cfg.AccountID != cert.AccountID {
		return errors.New("escolha um domínio da mesma conta Cloudflare que possui o túnel")
	}
	var zone Zone
	check, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := m.Client.callToken(check, cert.Token, "GET", "/zones/"+cert.ZoneID, nil, &zone); err != nil || zone.ID != cert.ZoneID || !validDomain(zone.Name) {
		return errors.New("não foi possível confirmar o nome do domínio autorizado")
	}
	zone.Account.ID = cert.AccountID
	dir := m.zoneDirectory()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if first, err := readCertificate(certPath(m.localHome())); err == nil && first.ZoneID == cfg.ZoneID && first.ZoneID != cert.ZoneID {
		original := Zone{ID: cfg.ZoneID, Name: cfg.Domain}
		original.Account.ID = cfg.AccountID
		if err := m.saveZone(original); err != nil {
			return err
		}
	}
	// Keep the original cert available for zones already served by this tunnel.
	destination := filepath.Join(dir, cert.ZoneID+".pem")
	if first, err := readCertificate(certPath(m.localHome())); err == nil && first.ZoneID == cert.ZoneID {
		destination = certPath(m.localHome())
	}
	if err := os.Rename(path, destination); err != nil {
		return err
	}
	return m.saveZone(zone)
}

func (m *Manager) saveZone(zone Zone) error {
	data, err := json.Marshal(zone)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.zoneDirectory(), zone.ID+".json"), data, 0600)
}

func (m *Manager) zonesFor(cfg model.CloudConfig) map[string]string {
	zones := map[string]string{}
	if cfg.Domain != "" {
		zones[cfg.Domain] = cfg.ZoneID
	}
	if cfg.Mode != "local" {
		return zones
	}
	var first struct{ ID, Name string }
	data, _ := os.ReadFile(filepath.Join(m.localHome(), "selected-zone.json"))
	if json.Unmarshal(data, &first) == nil && validDomain(first.Name) {
		if _, cert, err := m.certificateForZone(first.ID); err == nil && cert.AccountID == cfg.AccountID {
			zones[first.Name] = first.ID
		}
	}
	entries, _ := os.ReadDir(m.zoneDirectory())
	for _, entry := range entries {
		id := strings.TrimSuffix(entry.Name(), ".json")
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || !resourceID.MatchString(id) {
			continue
		}
		var zone Zone
		data, _ := os.ReadFile(filepath.Join(m.zoneDirectory(), entry.Name()))
		if json.Unmarshal(data, &zone) != nil || zone.ID != id || zone.Account.ID != cfg.AccountID || !validDomain(zone.Name) {
			continue
		}
		if _, cert, err := m.certificateForZone(id); err == nil && cert.AccountID == cfg.AccountID {
			zones[zone.Name] = id
		}
	}
	return zones
}

func (m *Manager) Domains() []string {
	zones := m.zonesFor(m.Config())
	domains := make([]string, 0, len(zones))
	for name := range zones {
		domains = append(domains, name)
	}
	sort.Strings(domains)
	return domains
}

func (m *Manager) destination(cfg model.CloudConfig, domain, sub string) (string, string, error) {
	if cfg.TunnelID == "" {
		return "", "", errors.New("configure o endereço do painel primeiro")
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		domain = cfg.Domain
	}
	id := m.zonesFor(cfg)[domain]
	if id == "" {
		return "", "", errors.New("autorize esse domínio na seção Cloudflare")
	}
	host, err := Host(sub, domain)
	return id, host, err
}

func (m *Manager) ValidatePort(port int, optional bool) error {
	if port == 0 && optional {
		return nil
	}
	if port < 1 || port > 65535 {
		return errors.New("use uma porta entre 1 e 65535")
	}
	origin, _ := url.Parse(m.Origin)
	if port == 28931 || (origin != nil && strconv.Itoa(port) == origin.Port()) {
		return errors.New("essa porta é reservada pelo Cloudforge")
	}
	return nil
}

// Keep a port edit from racing a publication using the saved port.
func (m *Manager) UpdateProjectPort(update func() error) error {
	if !m.op.TryLock() {
		return errors.New("aguarde a operação Cloudflare em andamento")
	}
	defer m.op.Unlock()
	return update()
}

func (m *Manager) Route(ctx context.Context, projectID, sub string, port int) (model.Route, error) {
	route, _, err := m.RouteTo(ctx, projectID, "", sub, port)
	return route, err
}

func (m *Manager) RouteTo(ctx context.Context, projectID, domain, sub string, port int) (model.Route, string, error) {
	if cfg := m.Config(); cfg.Mode != "local" {
		if domain != "" && domain != cfg.Domain {
			return model.Route{}, "", errors.New("escolha o domínio do túnel")
		}
		route, err := m.routeLegacy(ctx, projectID, sub, port)
		return route, "", err
	}
	if !m.op.TryLock() {
		return model.Route{}, "", errors.New("operação Cloudflare em andamento")
	}
	defer m.op.Unlock()
	cfg := m.Config()
	zone, host, err := m.destination(cfg, domain, sub)
	if err != nil {
		return model.Route{}, "", err
	}
	if host == cfg.AdminHost {
		return model.Route{}, "", errors.New("esse endereço é usado pelo painel")
	}
	project, err := m.Store.Project(projectID)
	if err != nil {
		return model.Route{}, "", err
	}
	if port == 0 {
		port = project.Port
	}
	if err = m.ValidatePort(port, false); err != nil {
		return model.Route{}, "", err
	}
	routes, err := m.Store.Routes()
	if err != nil {
		return model.Route{}, "", err
	}
	var old model.Route
	for _, route := range routes {
		if route.ProjectID == projectID {
			old = route
		} else if route.Hostname == host {
			return model.Route{}, "", errors.New("esse endereço é usado por outro projeto")
		}
	}
	route := model.Route{ID: old.ID, ProjectID: projectID, ZoneID: zone, Hostname: host, Port: port}
	if route.ID == "" {
		route.ID = auth.ID()
	}
	if old.Hostname != host {
		target := cfg
		target.ZoneID = zone
		if err = m.localDNS(ctx, target, host); err != nil {
			return model.Route{}, "", err
		}
	} else {
		route.DNSRecordID = old.DNSRecordID
	}
	if err = m.Store.SaveRoute(route); err != nil {
		return model.Route{}, "", err
	}
	if old.Hostname != "" && old.Hostname != host {
		return route, m.retireDNS(ctx, cfg, old), nil
	}
	return route, "", nil
}

func (m *Manager) ChangePanel(ctx context.Context, domain, sub string) (string, error) {
	if !m.op.TryLock() {
		return "", errors.New("operação Cloudflare em andamento")
	}
	defer m.op.Unlock()
	old := m.Config()
	zone, host, err := m.destination(old, domain, sub)
	if err != nil || host == old.AdminHost {
		return "", err
	}
	routes, err := m.Store.Routes()
	if err != nil {
		return "", err
	}
	for _, route := range routes {
		if route.Hostname == host {
			return "", errors.New("esse endereço é usado por um projeto")
		}
	}
	next := old
	next.ZoneID, next.AdminHost = zone, host
	next.Domain = strings.SplitN(host, ".", 2)[1]
	if old.Mode == "local" {
		if err = m.localDNS(ctx, next, host); err != nil {
			return "", err
		}
	} else {
		if _, err = m.dns(ctx, next, host); err != nil {
			return "", err
		}
		if err = m.ingress(ctx, next, routes); err != nil {
			_ = m.retireDNS(ctx, next, model.Route{ZoneID: zone, Hostname: host})
			return "", err
		}
	}
	if err = m.Store.SaveCloudConfig(next); err != nil {
		if old.Mode != "local" {
			_ = m.ingress(ctx, old, routes)
		}
		return "", err
	}
	m.mu.Lock()
	m.cfg = next
	m.mu.Unlock()
	return m.retireDNS(ctx, old, model.Route{ZoneID: old.ZoneID, Hostname: old.AdminHost}), nil
}

func (m *Manager) RemoveRoute(ctx context.Context, projectID string) (string, error) {
	return m.removePublication(ctx, projectID, false, func() error { return m.Store.DeleteRoute(projectID) })
}

func (m *Manager) RemoveProject(ctx context.Context, projectID string, removeLocal func() error) (string, error) {
	return m.removePublication(ctx, projectID, true, removeLocal)
}

func (m *Manager) removePublication(ctx context.Context, projectID string, deletingProject bool, removeLocal func() error) (string, error) {
	if !m.op.TryLock() {
		return "", errors.New("operação Cloudflare em andamento")
	}
	defer m.op.Unlock()
	if deletingProject {
		if err := m.noPending(); err != nil {
			return "", err
		}
	}
	cfg := m.Config()
	routes, err := m.Store.Routes()
	if err != nil {
		return "", err
	}
	var remove model.Route
	remaining := make([]model.Route, 0, len(routes))
	for _, route := range routes {
		if route.ProjectID == projectID {
			remove = route
		} else {
			remaining = append(remaining, route)
		}
	}
	if remove.Hostname == "" {
		if deletingProject {
			return "", removeLocal()
		}
		return "", nil
	}
	if cfg.Mode != "local" && cfg.TunnelID != "" {
		if err = m.ingress(ctx, cfg, remaining); err != nil {
			return "", err
		}
	}
	if err = removeLocal(); err != nil {
		if cfg.Mode != "local" && cfg.TunnelID != "" {
			_ = m.ingress(ctx, cfg, routes)
		}
		return "", err
	}
	return m.retireDNS(ctx, cfg, remove), nil
}

// Retire only a CNAME still pointing to this installation's tunnel.
func (m *Manager) retireDNS(ctx context.Context, cfg model.CloudConfig, route model.Route) string {
	if route.Hostname == "" || cfg.TunnelID == "" {
		return ""
	}
	warning := fmt.Sprintf("O endereço %s saiu do Cloudforge, mas o registro DNS não foi removido. Confira-o na Cloudflare.", route.Hostname)
	call := m.Client.Call
	if cfg.Mode == "local" {
		_, cert, err := m.certificateForZone(route.ZoneID)
		if err != nil || cert.AccountID != cfg.AccountID {
			return warning
		}
		call = func(ctx context.Context, method, path string, body, out any) error {
			return m.Client.callToken(ctx, cert.Token, method, path, body, out)
		}
	}
	path := "/zones/" + url.PathEscape(route.ZoneID) + "/dns_records"
	var records []DNS
	if err := call(ctx, "GET", path+"?name="+url.QueryEscape(route.Hostname), nil, &records); err != nil {
		return warning
	}
	for _, record := range records {
		if record.Type != "CNAME" || record.Name != route.Hostname || strings.TrimSuffix(record.Content, ".") != cfg.TunnelID+".cfargotunnel.com" {
			continue
		}
		if route.DNSRecordID != "" && route.DNSRecordID != record.ID {
			continue
		}
		if cfg.Mode != "local" && record.Comment != "Cloudforge "+m.Installation {
			continue
		}
		if record.ID == "" {
			return warning
		}
		if err := call(ctx, "DELETE", path+"/"+url.PathEscape(record.ID), nil, nil); err != nil {
			return warning
		}
		return ""
	}
	if len(records) > 0 {
		return warning
	}
	return ""
}
