package cloudflare

import (
	"cloudforge/core/internal/auth"
	"cloudforge/core/internal/model"
	"cloudforge/core/internal/persistence"
	"cloudforge/core/internal/processes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	mu           sync.Mutex
	op           sync.Mutex
	Store        persistence.Repository
	Client       *Client
	DataDir      string
	Origin       string
	Installation string
	cfg          model.CloudConfig
	state        string
	lastError    string
	cancel       context.CancelFunc
	done         chan struct{}
	pid          int
	loginCancel  context.CancelFunc
	loginError   string
	runLocalCLI  func(context.Context, ...string) ([]byte, error)
}

func New(s persistence.Repository, dir, origin, installation string) *Manager {
	m := &Manager{Store: s, Client: NewClient(OSVault{installation}), DataDir: dir, Origin: origin, Installation: installation, state: "not-configured"}
	_ = s.Setting("cloudflare", &m.cfg)
	return m
}
func (m *Manager) Config() model.CloudConfig { m.mu.Lock(); defer m.mu.Unlock(); return m.cfg }
func (m *Manager) Status(ctx context.Context) map[string]any {
	m.mu.Lock()
	cfg, state, last, pid := m.cfg, m.state, m.lastError, m.pid
	m.mu.Unlock()
	connected := false
	if cfg.Mode == "local" && pid > 0 {
		connected = m.checkHTTPS(ctx, cfg.AdminHost) == nil
		if connected {
			state = "connected"
		}
	} else if cfg.TunnelID != "" && cfg.Mode != "local" {
		var conns []map[string]any
		check, cancel := context.WithTimeout(ctx, 4*time.Second)
		if e := m.Client.Call(check, "GET", "/accounts/"+cfg.AccountID+"/cfd_tunnel/"+cfg.TunnelID+"/connections", nil, &conns); e == nil {
			connected = len(conns) > 0
			if connected {
				state = "connected"
			} else if pid > 0 {
				state = "connecting"
			}
		}
		cancel()
	}
	pending, _ := m.Store.PendingOperations()
	return map[string]any{"configuration": cfg, "status": state, "connected": connected, "pid": pid, "lastError": last, "pendingOperations": pending, "browser": m.BrowserStatus()}
}

var label = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func Host(sub, domain string) (string, error) {
	sub = strings.ToLower(strings.TrimSpace(sub))
	domain = strings.ToLower(strings.TrimSpace(domain))
	if !label.MatchString(sub) {
		return "", errors.New("subdomínio inválido: use letras minúsculas, números e hífen")
	}
	if !validDomain(domain) {
		return "", errors.New("informe o domínio selecionado na Cloudflare, sem protocolo ou caminho")
	}
	host := sub + "." + domain
	if len(host) > 253 {
		return "", errors.New("nome de domínio muito longo")
	}
	return host, nil
}

type journal struct {
	Config         model.CloudConfig `json:"config"`
	OldConfig      model.CloudConfig `json:"oldConfig"`
	DNS            []model.Route     `json:"dns"`
	CreatedTunnel  bool              `json:"createdTunnel"`
	PreviousRoutes []model.Route     `json:"previousRoutes"`
	Route          *model.Route      `json:"route,omitempty"`
	CleanupDNS     []model.Route     `json:"cleanupDNS,omitempty"`
}

func (m *Manager) ingress(ctx context.Context, cfg model.CloudConfig, routes []model.Route) error {
	rules := []map[string]string{{"hostname": cfg.AdminHost, "service": m.Origin}}
	for _, r := range routes {
		rules = append(rules, map[string]string{"hostname": r.Hostname, "service": m.Origin})
	}
	rules = append(rules, map[string]string{"service": "http_status:404"})
	return m.Client.Call(ctx, "PUT", "/accounts/"+cfg.AccountID+"/cfd_tunnel/"+cfg.TunnelID+"/configurations", map[string]any{"config": map[string]any{"ingress": rules}}, nil)
}
func (m *Manager) dns(ctx context.Context, cfg model.CloudConfig, host string) (model.Route, error) {
	r := model.Route{ZoneID: cfg.ZoneID, Hostname: host}
	records, e := m.Client.Records(ctx, cfg.ZoneID, host)
	if e != nil {
		return r, e
	}
	target := cfg.TunnelID + ".cfargotunnel.com"
	if len(records) > 0 {
		return r, errors.New("já existe um registro DNS para " + host + "; escolha outro subdomínio")
	}
	var record DNS
	e = m.Client.Call(ctx, "POST", "/zones/"+cfg.ZoneID+"/dns_records", map[string]any{"type": "CNAME", "name": host, "content": target, "proxied": true, "ttl": 1, "comment": "Cloudforge " + m.Installation}, &record)
	r.DNSRecordID = record.ID
	return r, e
}
func (m *Manager) rollback(ctx context.Context, j journal) error {
	var failures []string
	for _, r := range j.DNS {
		if r.DNSRecordID == "" {
			continue
		}
		records, e := m.Client.Records(ctx, r.ZoneID, r.Hostname)
		if e != nil {
			failures = append(failures, e.Error())
			continue
		}
		for _, record := range records {
			if record.ID == r.DNSRecordID && record.Comment == "Cloudforge "+m.Installation {
				if e = m.Client.Call(ctx, "DELETE", "/zones/"+r.ZoneID+"/dns_records/"+record.ID, nil, nil); e != nil {
					failures = append(failures, e.Error())
				}
			}
		}
	}
	if j.CreatedTunnel && j.Config.TunnelID != "" {
		if e := m.Client.Call(ctx, "DELETE", "/accounts/"+j.Config.AccountID+"/cfd_tunnel/"+j.Config.TunnelID, nil, nil); e != nil {
			failures = append(failures, e.Error())
		}
	} else if j.OldConfig.TunnelID != "" {
		if e := m.ingress(ctx, j.OldConfig, j.PreviousRoutes); e != nil {
			failures = append(failures, e.Error())
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}
func (m *Manager) Provision(ctx context.Context, zoneID, sub string) (model.CloudConfig, error) {
	if !m.op.TryLock() {
		return model.CloudConfig{}, errors.New("operação Cloudflare em andamento")
	}
	defer m.op.Unlock()
	old := m.Config()
	if e := m.noPending(); e != nil {
		return old, e
	}
	if old.TunnelID != "" {
		return old, errors.New("túnel já configurado; use as rotas por projeto")
	}
	zones, e := m.Client.Zones(ctx)
	if e != nil {
		return old, e
	}
	var zone Zone
	for _, z := range zones {
		if z.ID == zoneID {
			zone = z
		}
	}
	if zone.ID == "" {
		return old, errors.New("zona não pertence às zonas autorizadas pelo token")
	}
	host, e := Host(sub, zone.Name)
	if e != nil {
		return old, e
	}
	cfg := model.CloudConfig{AccountID: zone.Account.ID, ZoneID: zone.ID, Domain: zone.Name, AdminHost: host, Name: "cloudforge-" + m.Installation}
	routes, e := m.Store.Routes()
	if e != nil {
		return old, e
	}
	j := journal{Config: cfg, OldConfig: old, PreviousRoutes: routes}
	operation := "provision-" + m.Installation
	if e = m.Store.Operation(operation, "cloudflare-provision", "pending", j, ""); e != nil {
		return old, e
	}
	records, e := m.Client.Records(ctx, cfg.ZoneID, host)
	if e != nil {
		return old, e
	}
	if len(records) > 0 {
		return old, errors.New("subdomínio administrativo já possui DNS; escolha outro")
	}
	var tunnel struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	e = m.Client.Call(ctx, "POST", "/accounts/"+cfg.AccountID+"/cfd_tunnel", map[string]any{"name": cfg.Name, "config_src": "cloudflare"}, &tunnel)
	if e != nil {
		return old, e
	}
	cfg.TunnelID = tunnel.ID
	j.Config = cfg
	j.CreatedTunnel = true
	fail := func(cause error) (model.CloudConfig, error) {
		repair, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		rollbackError := m.rollback(repair, j)
		state := "rolled_back"
		if rollbackError != nil {
			state = "needs-recovery"
		}
		_ = m.Store.Operation(operation, "cloudflare-provision", state, j, cause.Error())
		return old, cause
	}
	if e = m.Store.Operation(operation, "cloudflare-provision", "tunnel-created", j, ""); e != nil {
		return fail(e)
	}
	if tunnel.Token == "" {
		e = m.Client.Call(ctx, "GET", "/accounts/"+cfg.AccountID+"/cfd_tunnel/"+cfg.TunnelID+"/token", nil, &tunnel.Token)
		if e != nil {
			return fail(e)
		}
	}
	if e = m.Client.Vault.Set("tunnel-token", tunnel.Token); e != nil {
		return fail(errors.New("não foi possível proteger a credencial do túnel no sistema operacional"))
	}
	if e = m.ingress(ctx, cfg, routes); e != nil {
		return fail(e)
	}
	record, e := m.dns(ctx, cfg, host)
	if e != nil {
		return fail(e)
	}
	j.DNS = append(j.DNS, record)
	if e = m.Store.Operation(operation, "cloudflare-provision", "dns-created", j, ""); e != nil {
		return fail(e)
	}
	if e = m.Store.SaveCloudConfig(cfg); e != nil {
		return fail(e)
	}
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
	if e = m.Store.Operation(operation, "cloudflare-provision", "complete", j, ""); e != nil {
		return cfg, e
	}
	m.Start()
	return cfg, nil
}
func (m *Manager) routeLegacy(ctx context.Context, projectID, sub string, port int) (model.Route, error) {
	if !m.op.TryLock() {
		return model.Route{}, errors.New("operação Cloudflare em andamento")
	}
	defer m.op.Unlock()
	cfg := m.Config()
	if cfg.TunnelID == "" {
		return model.Route{}, errors.New("configure o túnel administrativo primeiro")
	}
	if e := m.noPending(); e != nil {
		return model.Route{}, e
	}
	project, e := m.Store.Project(projectID)
	if e != nil {
		return model.Route{}, e
	}
	if port == 0 {
		port = project.Port
	}
	if port < 1 || port > 65535 {
		return model.Route{}, errors.New("porta deve estar entre 1 e 65535")
	}
	originURL, _ := url.Parse(m.Origin)
	if originURL != nil && (fmt.Sprint(port) == originURL.Port() || port == 28931) {
		return model.Route{}, errors.New("porta reservada para o Core; escolha a porta HTTP do projeto")
	}
	host, e := Host(sub, cfg.Domain)
	if e != nil {
		return model.Route{}, e
	}
	if host == cfg.AdminHost {
		return model.Route{}, errors.New("subdomínio reservado para a dashboard")
	}
	old, e := m.Store.Routes()
	if e != nil {
		return model.Route{}, e
	}
	var previous *model.Route
	routes := []model.Route{}
	for _, r := range old {
		if r.Hostname == host && r.ProjectID != projectID {
			return model.Route{}, errors.New("subdomínio já utilizado")
		}
		if r.ProjectID == projectID {
			copy := r
			previous = &copy
		} else {
			routes = append(routes, r)
		}
	}
	r := model.Route{ID: auth.ID(), ProjectID: projectID, ZoneID: cfg.ZoneID, Hostname: host, Port: port}
	if previous != nil {
		r.ID = previous.ID
	}
	routes = append(routes, r)
	j := journal{Config: cfg, OldConfig: cfg, PreviousRoutes: old, Route: &r}
	if previous != nil && previous.Hostname != host {
		j.CleanupDNS = []model.Route{*previous}
	}
	op := "route-" + auth.ID()
	if e = m.Store.Operation(op, "cloudflare-route", "pending", j, ""); e != nil {
		return r, e
	}
	fail := func(cause error) (model.Route, error) {
		repair, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		status := "rolled_back"
		if m.rollback(repair, j) != nil {
			status = "needs-recovery"
		}
		_ = m.Store.Operation(op, "cloudflare-route", status, j, cause.Error())
		return r, cause
	}
	if previous != nil && previous.Hostname == host {
		records, e := m.Client.Records(ctx, cfg.ZoneID, host)
		if e != nil {
			return fail(e)
		}
		for _, record := range records {
			if record.ID == previous.DNSRecordID && record.Content == cfg.TunnelID+".cfargotunnel.com" && record.Comment == "Cloudforge "+m.Installation {
				r.DNSRecordID = record.ID
			}
		}
		if r.DNSRecordID == "" {
			return fail(errors.New("DNS existente foi alterado externamente; verifique o registro antes de atualizar a rota"))
		}
	}
	if e = m.ingress(ctx, cfg, routes); e != nil {
		return fail(e)
	}
	if r.DNSRecordID == "" {
		record, e := m.dns(ctx, cfg, host)
		if e != nil {
			return fail(e)
		}
		r.DNSRecordID = record.DNSRecordID
		j.DNS = []model.Route{r}
	}
	j.Route = &r
	if e = m.Store.Operation(op, "cloudflare-route", "dns-created", j, ""); e != nil {
		return fail(e)
	}
	if e = m.Store.SaveRoute(r); e != nil {
		return fail(e)
	}
	if e = m.cleanup(ctx, j.CleanupDNS, cfg.TunnelID); e != nil {
		_ = m.Store.Operation(op, "cloudflare-route", "needs-cleanup", j, e.Error())
		return r, errors.New("nova rota salva; a limpeza do DNS anterior precisa de Recuperar operações")
	}
	if e = m.Store.Operation(op, "cloudflare-route", "complete", j, ""); e != nil {
		return r, e
	}
	return r, nil
}
func (m *Manager) Binary() (string, error) {
	name := "cloudflared"
	if filepath.Separator == '\\' {
		name += ".exe"
	}
	if core, e := os.Executable(); e == nil {
		bundled := filepath.Join(filepath.Dir(core), "cloudflared", name)
		if info, e := os.Stat(bundled); e == nil && info.Mode().IsRegular() {
			return bundled, nil
		}
	}
	local := filepath.Join(m.DataDir, "bin", name)
	if info, e := os.Stat(local); e == nil && info.Mode().IsRegular() {
		return local, nil
	}
	return exec.LookPath("cloudflared")
}
func (m *Manager) Start() {
	m.mu.Lock()
	if m.cancel != nil || m.cfg.TunnelID == "" {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.done = make(chan struct{})
	cfg := m.cfg
	m.mu.Unlock()
	go func() {
		defer func() { m.mu.Lock(); m.cancel = nil; m.pid = 0; close(m.done); m.mu.Unlock() }()
		attempt := 0
		for {
			if ctx.Err() != nil {
				return
			}
			binary, e := m.Binary()
			var cmd *exec.Cmd
			var secretError error
			if e == nil {
				if cfg.Mode == "local" {
					cmd, e = m.localRun(binary, cfg)
				} else {
					var token string
					token, secretError = m.Client.Vault.Get("tunnel-token")
					e = secretError
					cmd = exec.Command(binary, "--no-autoupdate", "tunnel", "run")
					cmd.Env = append(os.Environ(), "TUNNEL_TOKEN="+token)
				}
			}
			if e == nil {
				processes.Configure(cmd)
				e = cmd.Start()
				if e == nil {
					tree, attachError := processes.Attach(cmd)
					if attachError != nil {
						cmd.Process.Kill()
						cmd.Wait()
						e = attachError
					} else {
						m.mu.Lock()
						m.state = "connecting"
						m.lastError = ""
						m.pid = cmd.Process.Pid
						m.mu.Unlock()
						done := make(chan struct{})
						go func() { _ = cmd.Wait(); close(done) }()
						select {
						case <-ctx.Done():
							tree.Stop(done)
						case <-done:
						}
						tree.Close()
						m.mu.Lock()
						m.pid = 0
						m.mu.Unlock()
						if ctx.Err() != nil {
							return
						}
						e = errors.New("cloudflared encerrou; tentando reconectar")
					}
				}
			}
			m.mu.Lock()
			m.state = "error"
			m.lastError = "cloudflared indisponível ou desconectado; confira instalação e credenciais"
			if e != nil && secretError == nil {
				m.lastError = e.Error()
			}
			m.mu.Unlock()
			attempt++
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second * time.Duration(1<<min(attempt, 6))):
			}
		}
	}()
}
func (m *Manager) Stop() {
	m.CancelLogin()
	m.mu.Lock()
	cancel, done := m.cancel, m.done
	m.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}
func (m *Manager) Recover(ctx context.Context) error {
	if !m.op.TryLock() {
		return errors.New("operação Cloudflare em andamento")
	}
	defer m.op.Unlock()
	pending, e := m.Store.PendingOperations()
	if e != nil {
		return e
	}
	for _, op := range pending {
		kind, _ := op["kind"].(string)
		if !strings.HasPrefix(kind, "cloudflare-") {
			continue
		}
		raw, ok := op["payload"].(json.RawMessage)
		if !ok {
			return errors.New("diário de recuperação inválido")
		}
		data := string(raw)
		var j journal
		if e = decodeJournal(data, &j); e != nil {
			return e
		}
		if kind == "cloudflare-provision" && j.Config.TunnelID == "" && j.Config.AccountID != "" {
			var tunnels []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			if e = m.Client.Call(ctx, "GET", "/accounts/"+j.Config.AccountID+"/cfd_tunnel?is_deleted=false&name="+url.QueryEscape(j.Config.Name), nil, &tunnels); e != nil {
				return e
			}
			for _, tunnel := range tunnels {
				if tunnel.Name == "cloudforge-"+m.Installation {
					j.Config.TunnelID = tunnel.ID
					j.CreatedTunnel = true
					break
				}
			}
		}
		cfg := m.Config()
		if kind == "cloudflare-provision" && cfg.TunnelID != "" && cfg.TunnelID == j.Config.TunnelID {
			_ = m.Store.Operation(op["id"].(string), kind, "complete", j, "")
			continue
		}
		if kind == "cloudflare-route" && j.Route != nil {
			routes, _ := m.Store.Routes()
			saved := false
			for _, r := range routes {
				if r.ID == j.Route.ID && r.Hostname == j.Route.Hostname && r.Port == j.Route.Port {
					saved = true
				}
			}
			if saved {
				if e = m.cleanup(ctx, j.CleanupDNS, j.Config.TunnelID); e != nil {
					return e
				}
				_ = m.Store.Operation(op["id"].(string), kind, "complete", j, "")
				continue
			}
		}
		if j.Config.TunnelID != "" {
			host := j.Config.AdminHost
			if j.Route != nil {
				host = j.Route.Hostname
			}
			records, recordError := m.Client.Records(ctx, j.Config.ZoneID, host)
			if recordError != nil {
				return recordError
			}
			for _, record := range records {
				if record.Comment == "Cloudforge "+m.Installation && record.Content == j.Config.TunnelID+".cfargotunnel.com" {
					owned := true
					for _, old := range j.PreviousRoutes {
						if old.DNSRecordID == record.ID {
							owned = false
						}
					}
					if owned {
						j.DNS = append(j.DNS, model.Route{ZoneID: j.Config.ZoneID, Hostname: host, DNSRecordID: record.ID})
					}
				}
			}
		}
		if e = m.rollback(ctx, j); e != nil {
			return fmt.Errorf("recuperação pendente da Cloudflare: %w", e)
		}
		if e = m.Store.Operation(op["id"].(string), kind, "rolled_back", j, ""); e != nil {
			return e
		}
	}
	return nil
}
func (m *Manager) noPending() error {
	pending, e := m.Store.PendingOperations()
	if e != nil {
		return e
	}
	for _, op := range pending {
		if strings.HasPrefix(op["kind"].(string), "cloudflare-") {
			return errors.New("há uma operação Cloudflare incompleta; use Recuperar operações antes de continuar")
		}
	}
	return nil
}
func (m *Manager) cleanup(ctx context.Context, routes []model.Route, tunnelID string) error {
	for _, r := range routes {
		records, e := m.Client.Records(ctx, r.ZoneID, r.Hostname)
		if e != nil {
			return e
		}
		for _, record := range records {
			if record.ID == r.DNSRecordID && record.Comment == "Cloudforge "+m.Installation && record.Content == tunnelID+".cfargotunnel.com" {
				if e = m.Client.Call(ctx, "DELETE", "/zones/"+r.ZoneID+"/dns_records/"+record.ID, nil, nil); e != nil {
					return e
				}
			}
		}
	}
	return nil
}
func (m *Manager) PublicURL() string {
	cfg := m.Config()
	if cfg.AdminHost == "" {
		return ""
	}
	return "https://" + cfg.AdminHost
}
func (m *Manager) Verify(ctx context.Context) error {
	if cfg := m.Config(); cfg.Mode == "local" {
		return m.checkHTTPS(ctx, cfg.AdminHost)
	}
	status := m.Status(ctx)
	if status["connected"] != true {
		return errors.New("túnel configurado, mas ainda sem conexão ativa; aguarde e verifique novamente")
	}
	req, e := http.NewRequestWithContext(ctx, "GET", m.PublicURL()+"/api/health", nil)
	if e != nil {
		return e
	}
	resp, e := m.Client.HTTP.Do(req)
	if e != nil {
		return errors.New("túnel conectado, mas a dashboard HTTPS ainda não respondeu")
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("dashboard retornou HTTP %d", resp.StatusCode)
	}
	return nil
}
