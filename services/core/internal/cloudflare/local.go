package cloudflare

import (
	"cloudforge/core/internal/model"
	"cloudforge/core/internal/processes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var tunnelID = regexp.MustCompile(`^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)
var resourceID = regexp.MustCompile(`^[0-9a-f]{32}$`)
var createdID = regexp.MustCompile(`(?m)\b([0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12})\b`)
var routedHost = regexp.MustCompile(`(?:Added CNAME ([a-z0-9.-]+) which will route to this tunnel|([a-z0-9.-]+) is already configured to route to your tunnel|([a-z0-9.-]+) updated to route to your tunnel)`)

type browserStatus struct {
	Pending    bool   `json:"pending"`
	Authorized bool   `json:"authorized"`
	Domain     string `json:"domain,omitempty"`
	Error      string `json:"error,omitempty"`
}

type certificate struct {
	AccountID string `json:"accountID"`
	ZoneID    string `json:"zoneID"`
	Token     string `json:"apiToken"`
}

func validDomain(value string) bool {
	parts := strings.Split(value, ".")
	if len(value) > 253 || len(parts) < 2 || !strings.ContainsAny(parts[len(parts)-1], "abcdefghijklmnopqrstuvwxyz") {
		return false
	}
	for _, part := range parts {
		if !label.MatchString(part) {
			return false
		}
	}
	return true
}

func readCertificate(path string) (certificate, error) {
	var cert certificate
	data, err := os.ReadFile(path)
	if err == nil {
		for len(data) > 0 {
			block, rest := pem.Decode(data)
			if block == nil {
				break
			}
			if block.Type == "ARGO TUNNEL TOKEN" && json.Unmarshal(block.Bytes, &cert) == nil &&
				resourceID.MatchString(cert.AccountID) && resourceID.MatchString(cert.ZoneID) && cert.Token != "" {
				return cert, nil
			}
			data = rest
		}
	}
	return certificate{}, errors.New("autorize um domínio no navegador para continuar")
}

func (m *Manager) localHome() string { return filepath.Join(m.DataDir, "cloudflare-browser") }
func certPath(home string) string    { return filepath.Join(home, ".cloudflared", "cert.pem") }

func (m *Manager) BrowserStatus() browserStatus {
	m.mu.Lock()
	status := browserStatus{Pending: m.loginCancel != nil, Error: m.loginError}
	m.mu.Unlock()
	cert, err := readCertificate(certPath(m.localHome()))
	status.Authorized = err == nil
	if status.Authorized {
		var selected struct{ ID, Name string }
		data, _ := os.ReadFile(filepath.Join(m.localHome(), "selected-zone.json"))
		if json.Unmarshal(data, &selected) == nil && selected.ID == cert.ZoneID && validDomain(selected.Name) {
			status.Domain = selected.Name
		}
	}
	return status
}

func (m *Manager) StartBrowserLogin() error {
	m.mu.Lock()
	if m.cfg.TunnelID != "" && m.cfg.Mode != "local" {
		m.mu.Unlock()
		return errors.New("já existe um túnel ativo nesta instalação")
	}
	if m.loginCancel != nil {
		m.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	m.loginCancel, m.loginError = cancel, ""
	m.mu.Unlock()
	go func() {
		defer cancel()
		err := m.browserLogin(ctx)
		m.mu.Lock()
		defer m.mu.Unlock()
		m.loginCancel = nil
		if err != nil && !errors.Is(ctx.Err(), context.Canceled) {
			m.loginError = err.Error()
		}
	}()
	return nil
}

func (m *Manager) CancelLogin() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loginCancel != nil {
		m.loginCancel()
	}
}

func (m *Manager) browserLogin(ctx context.Context) error {
	binary, err := m.Binary()
	if err != nil {
		if err = m.Install(ctx); err != nil {
			return fmt.Errorf("não foi possível preparar o conector: %w", err)
		}
		binary, err = m.Binary()
		if err != nil {
			return errors.New("conector Cloudflare não encontrado")
		}
	}
	if !m.op.TryLock() {
		return errors.New("há outra operação da Cloudflare em andamento; tente novamente")
	}
	defer m.op.Unlock()
	config := m.Config()
	home := m.localHome()
	if err = os.MkdirAll(home, 0700); err != nil {
		return err
	}
	temp, err := os.MkdirTemp(home, "login-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	cmd := exec.CommandContext(ctx, binary, "tunnel", "login")
	cmd.Env = connectorEnv(temp)
	cmd.Dir = temp
	processes.Configure(cmd)
	if err = cmd.Run(); err != nil {
		return errors.New("o login não foi concluído; abra o navegador padrão, autorize um domínio e tente novamente")
	}
	cert, err := readCertificate(certPath(temp))
	if err != nil {
		return err
	}
	if config.TunnelID != "" {
		return m.authorizeAdditionalZone(ctx, config, cert, certPath(temp))
	}
	if err = os.MkdirAll(filepath.Dir(certPath(home)), 0700); err != nil {
		return err
	}
	if err = os.Rename(certPath(temp), certPath(home)); err != nil {
		return errors.New("não foi possível guardar a autorização da Cloudflare")
	}
	// This token can be zone-scoped without Zone Read. In that case the UI asks for the zone name.
	var zone Zone
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := m.Client.callToken(check, cert.Token, "GET", "/zones/"+cert.ZoneID, nil, &zone); err == nil && zone.ID == cert.ZoneID && validDomain(zone.Name) {
		data, _ := json.Marshal(struct{ ID, Name string }{zone.ID, zone.Name})
		_ = os.WriteFile(filepath.Join(home, "selected-zone.json"), data, 0600)
	} else {
		_ = os.Remove(filepath.Join(home, "selected-zone.json"))
	}
	return nil
}

// Isolate cloudflared from a user's other tunnels and inherited CLI overrides.
func connectorEnv(home string) []string {
	var env []string
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		name = strings.ToUpper(name)
		if name != "HOME" && name != "USERPROFILE" && !strings.HasPrefix(name, "TUNNEL_") && !strings.HasPrefix(name, "CFD_") {
			env = append(env, value)
		}
	}
	return append(env, "HOME="+home, "USERPROFILE="+home)
}

func (m *Manager) localCLI(ctx context.Context, args ...string) ([]byte, error) {
	return m.localCLIWithCert(ctx, certPath(m.localHome()), args...)
}

func (m *Manager) localCLIWithCert(ctx context.Context, path string, args ...string) ([]byte, error) {
	if m.runLocalCLI != nil {
		return m.runLocalCLI(ctx, args...)
	}
	binary, err := m.Binary()
	if err != nil {
		return nil, errors.New("conector Cloudflare não encontrado; clique em Conectar com a Cloudflare")
	}
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	home := m.localHome()
	cmd := exec.CommandContext(ctx, binary, append([]string{"tunnel", "--origincert", path}, args...)...)
	cmd.Dir, cmd.Env = home, connectorEnv(home)
	processes.Configure(cmd)
	if args[0] == "list" {
		// cloudflared writes JSON to stdout and diagnostic messages to stderr.
		return cmd.Output()
	}
	return cmd.CombinedOutput()
}

func (m *Manager) credentials(id string) string {
	return filepath.Join(m.localHome(), ".cloudflared", id+".json")
}

func (m *Manager) localTunnel(ctx context.Context, name string) (string, error) {
	output, err := m.localCLI(ctx, "list", "--name", name, "--output", "json")
	if err != nil {
		return "", errors.New("a Cloudflare não conseguiu listar os túneis; confira o domínio autorizado e a conexão")
	}
	var tunnels []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	// An empty result can be encoded as null. Both null and [] mean no matches.
	if json.Unmarshal(output, &tunnels) != nil {
		return "", errors.New("a lista de túneis da Cloudflare veio em formato inesperado")
	}
	for _, tunnel := range tunnels {
		if tunnel.Name != name || !tunnelID.MatchString(tunnel.ID) {
			continue
		}
		if _, err := os.Stat(m.credentials(tunnel.ID)); err != nil {
			return "", errors.New("o túnel do Cloudforge existe na Cloudflare, mas a credencial local está ausente")
		}
		return tunnel.ID, nil
	}
	output, err = m.localCLI(ctx, "create", name)
	if err != nil {
		return "", errors.New("a Cloudflare não conseguiu criar o túnel; tente novamente")
	}
	for _, id := range createdID.FindAllString(string(output), -1) {
		if _, err := os.Stat(m.credentials(id)); err == nil {
			return id, nil
		}
	}
	return "", errors.New("a Cloudflare criou o túnel, mas a credencial local não foi encontrada")
}

func (m *Manager) localDNS(ctx context.Context, cfg model.CloudConfig, host string) error {
	path, cert, err := m.certificateForZone(cfg.ZoneID)
	if err != nil || cert.AccountID != cfg.AccountID || cert.ZoneID != cfg.ZoneID {
		return errors.New("o domínio deste túnel precisa ser autorizado na Cloudflare")
	}
	output, err := m.localCLIWithCert(ctx, path, "route", "dns", cfg.TunnelID, host)
	if err != nil {
		return errors.New("a Cloudflare não criou o DNS; confira o domínio autorizado e se o subdomínio já está em uso")
	}
	if matches := routedHost.FindStringSubmatch(string(output)); len(matches) == 4 {
		for _, actual := range matches[1:] {
			if actual != "" && strings.TrimSuffix(actual, ".") != host {
				return fmt.Errorf("a Cloudflare retornou outro endereço (%s); confira o domínio autorizado no navegador", actual)
			}
		}
	}
	return nil
}

func (m *Manager) ProvisionLocal(ctx context.Context, domain, sub string) (model.CloudConfig, error) {
	if !m.op.TryLock() {
		return model.CloudConfig{}, errors.New("operação Cloudflare em andamento")
	}
	defer m.op.Unlock()
	if old := m.Config(); old.TunnelID != "" {
		return old, errors.New("já existe um túnel configurado")
	}
	status := m.BrowserStatus()
	if status.Pending {
		return model.CloudConfig{}, errors.New("aguarde a autorização pelo navegador")
	}
	cert, err := readCertificate(certPath(m.localHome()))
	if err != nil {
		return model.CloudConfig{}, err
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	if status.Domain != "" && domain != status.Domain {
		return model.CloudConfig{}, errors.New("escolha o domínio autorizado no navegador")
	}
	host, err := Host(sub, domain)
	if err != nil {
		return model.CloudConfig{}, err
	}
	cfg := model.CloudConfig{Mode: "local", AccountID: cert.AccountID, ZoneID: cert.ZoneID, Domain: domain, AdminHost: host, Name: "cloudforge-" + m.Installation}
	cfg.TunnelID, err = m.localTunnel(ctx, cfg.Name)
	if err == nil {
		err = m.localDNS(ctx, cfg, host)
	}
	if err != nil {
		return model.CloudConfig{}, err
	}
	if err = m.Store.SaveCloudConfig(cfg); err != nil {
		return model.CloudConfig{}, err
	}
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
	m.Start()
	return cfg, nil
}

func (m *Manager) localRun(binary string, cfg model.CloudConfig) (*exec.Cmd, error) {
	if !tunnelID.MatchString(cfg.TunnelID) || !validDomain(cfg.Domain) {
		return nil, errors.New("configuração local do túnel inválida")
	}
	// The Core checks the Host against the panel and registered project routes.
	// Keeping one origin allows authorized zones to change without a restart.
	contents := fmt.Sprintf("tunnel: %s\ncredentials-file: %s\ningress:\n  - service: %s\n", cfg.TunnelID, strconv.Quote(filepath.ToSlash(m.credentials(cfg.TunnelID))), strconv.Quote(m.Origin))
	home := m.localHome()
	config := filepath.Join(home, "config.yml")
	if err := os.WriteFile(config, []byte(contents), 0600); err != nil {
		return nil, err
	}
	cmd := exec.Command(binary, "--no-autoupdate", "tunnel", "--config", config, "run", cfg.TunnelID)
	cmd.Dir, cmd.Env = home, connectorEnv(home)
	return cmd, nil
}

func (m *Manager) checkHTTPS(ctx context.Context, host string) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", "https://"+host+"/api/health", nil)
	if err != nil {
		return errors.New("endereço HTTPS inválido")
	}
	client := *m.Client.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return errors.New("o endereço HTTPS ainda não respondeu; aguarde a conexão e o DNS")
	}
	defer res.Body.Close()
	var health struct{ Service string }
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&health) != nil || health.Service != "cloudforge" {
		return fmt.Errorf("o painel ainda não respondeu pelo domínio (HTTP %d)", res.StatusCode)
	}
	return nil
}
