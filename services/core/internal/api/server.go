package api

import (
	"cloudforge/core/internal/auth"
	"cloudforge/core/internal/cloudflare"
	"cloudforge/core/internal/model"
	"cloudforge/core/internal/monitoring"
	"cloudforge/core/internal/persistence"
	"cloudforge/core/internal/projects"
	"cloudforge/core/internal/supervisor"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Server struct {
	Store            persistence.Repository
	Auth             *auth.Auth
	Supervisor       *supervisor.Supervisor
	Cloud            *cloudflare.Manager
	Key              string
	Development      bool
	WebDir           string
	DataDir          string
	Autostart        func(bool) error
	AutostartEnabled func() bool
	mu               sync.RWMutex
	system           monitoring.System
	limit            limiter
	loginGate        chan struct{}
	connections      chan struct{}
}

func New(s persistence.Repository, sup *supervisor.Supervisor, cf *cloudflare.Manager, key, dataDir, webDir string, development bool) *Server {
	v := &Server{Store: s, Auth: auth.New(s), Supervisor: sup, Cloud: cf, Key: key, DataDir: dataDir, WebDir: webDir, Development: development, loginGate: make(chan struct{}, 2), connections: make(chan struct{}, 16)}
	v.system = monitoring.Inspect(dataDir)
	return v
}
func (s *Server) Monitor(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			system := monitoring.Inspect(s.DataDir)
			s.mu.Lock()
			s.system = system
			s.mu.Unlock()
		}
	}
}
func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, e error) {
	write(w, status, map[string]string{"error": e.Error()})
}
func decode(w http.ResponseWriter, r *http.Request, out any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("Content-Type deve ser application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return fmt.Errorf("dados inválidos: %w", e)
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("corpo JSON inválido")
	}
	return nil
}
func security(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
}
func (s *Server) Snapshot() map[string]any {
	s.mu.RLock()
	system := s.system
	s.mu.RUnlock()
	auto := false
	if s.AutostartEnabled != nil {
		auto = s.AutostartEnabled()
	}
	return map[string]any{"ready": s.Auth.Ready(), "system": system, "projects": s.Supervisor.Views(), "cloudflare": s.Cloud.Config(), "autostart": auto}
}
func (s *Server) Local(w http.ResponseWriter, r *http.Request) {
	security(w)
	host := r.Host
	if h, _, e := net.SplitHostPort(host); e == nil {
		host = h
	}
	if host != "127.0.0.1" && host != "localhost" && host != "[::1]" && host != "::1" {
		fail(w, 403, errors.New("host administrativo inválido"))
		return
	}
	if r.Header.Get("Origin") != "" {
		fail(w, 403, errors.New("administração disponível somente pelo Electron"))
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.Key)) != 1 {
		fail(w, 401, errors.New("chave administrativa inválida"))
		return
	}
	path := strings.Trim(r.URL.Path, "/")
	if path == "api/health" && r.Method == "GET" {
		write(w, 200, map[string]any{"ok": true, "service": "cloudforge", "version": "1.0.0", "apiVersion": 3})
		return
	}
	if path == "api/state" && r.Method == "GET" {
		write(w, 200, s.Snapshot())
		return
	}
	if path == "api/events" && r.Method == "GET" {
		s.stream(w, r, true, auth.Session{})
		return
	}
	if path == "api/setup" && r.Method == "POST" {
		var body struct {
			Username     string `json:"username"`
			Password     string `json:"password"`
			Confirmation string `json:"confirmation"`
		}
		if e := decode(w, r, &body); e != nil {
			fail(w, 400, e)
			return
		}
		if body.Password != body.Confirmation {
			fail(w, 400, errors.New("as senhas não coincidem"))
			return
		}
		if e := s.Auth.Bootstrap(body.Username, body.Password); e != nil {
			fail(w, 400, e)
			return
		}
		write(w, 201, map[string]bool{"ok": true})
		return
	}
	if !s.Auth.Ready() {
		fail(w, 403, errors.New("crie o administrador antes de continuar"))
		return
	}
	if path == "api/projects/inspect" && r.Method == "POST" {
		var b struct {
			Path string `json:"path"`
		}
		if e := decode(w, r, &b); e != nil {
			fail(w, 400, e)
			return
		}
		d, e := projects.Detect(b.Path)
		if e != nil {
			fail(w, 400, e)
			return
		}
		write(w, 200, d)
		return
	}
	if path == "api/projects" {
		if r.Method == "GET" {
			write(w, 200, s.Supervisor.Views())
			return
		}
		if r.Method == "POST" {
			var b struct {
				Project  model.Project `json:"project"`
				Approved bool          `json:"approved"`
			}
			if e := decode(w, r, &b); e != nil {
				fail(w, 400, e)
				return
			}
			if !b.Approved {
				fail(w, 400, errors.New("confirme a execução do código e dos scripts de instalação deste projeto confiável"))
				return
			}
			if e := projects.New(&b.Project); e != nil {
				fail(w, 400, e)
				return
			}
			if e := s.Store.SaveProject(b.Project); e != nil {
				fail(w, 409, errors.New("não foi possível salvar; verifique se a pasta já está cadastrada"))
				return
			}
			s.Supervisor.Add(b.Project)
			write(w, 201, b.Project)
			return
		}
	}
	if strings.HasPrefix(path, "api/projects/") {
		s.projectLocal(w, r, strings.Split(strings.TrimPrefix(path, "api/projects/"), "/"))
		return
	}
	if path == "api/cloudflare/connect" && r.Method == "POST" {
		var b struct {
			Token string `json:"token"`
		}
		if e := decode(w, r, &b); e != nil {
			fail(w, 400, e)
			return
		}
		if e := s.Cloud.Client.Connect(r.Context(), b.Token); e != nil {
			fail(w, 400, e)
			return
		}
		write(w, 200, map[string]bool{"ok": true})
		return
	}
	if path == "api/cloudflare/zones" && r.Method == "GET" {
		zones, e := s.Cloud.Client.Zones(r.Context())
		if e != nil {
			fail(w, 400, e)
			return
		}
		write(w, 200, zones)
		return
	}
	if path == "api/cloudflare/login" && r.Method == "POST" {
		if e := s.Cloud.StartBrowserLogin(); e != nil {
			fail(w, 400, e)
			return
		}
		write(w, 202, s.Cloud.BrowserStatus())
		return
	}
	if path == "api/cloudflare/cancel-login" && r.Method == "POST" {
		s.Cloud.CancelLogin()
		write(w, 200, map[string]bool{"ok": true})
		return
	}
	if path == "api/cloudflare/provision" && r.Method == "POST" {
		var b struct {
			ZoneID    string `json:"zoneId"`
			Domain    string `json:"domain"`
			Subdomain string `json:"subdomain"`
			Confirmed bool   `json:"confirmed"`
		}
		if e := decode(w, r, &b); e != nil {
			fail(w, 400, e)
			return
		}
		if !b.Confirmed {
			fail(w, 400, errors.New("confirme o endereço antes de criar túnel e DNS"))
			return
		}
		var cfg model.CloudConfig
		var e error
		if b.Domain != "" {
			cfg, e = s.Cloud.ProvisionLocal(r.Context(), b.Domain, b.Subdomain)
		} else {
			cfg, e = s.Cloud.Provision(r.Context(), b.ZoneID, b.Subdomain)
		}
		if e != nil {
			fail(w, 400, e)
			return
		}
		write(w, 200, cfg)
		return
	}
	if path == "api/cloudflare/status" && r.Method == "GET" {
		write(w, 200, s.Cloud.Status(r.Context()))
		return
	}
	if path == "api/cloudflare/domains" && r.Method == "GET" {
		write(w, 200, s.Cloud.Domains())
		return
	}
	if path == "api/cloudflare/panel-domain" && r.Method == "POST" {
		var body struct {
			Domain    string `json:"domain"`
			Subdomain string `json:"subdomain"`
			Confirmed bool   `json:"confirmed"`
		}
		if err := decode(w, r, &body); err != nil {
			fail(w, 400, err)
			return
		}
		if !body.Confirmed {
			fail(w, 400, errors.New("confirme a troca do endereço do painel"))
			return
		}
		warning, err := s.Cloud.ChangePanel(r.Context(), body.Domain, body.Subdomain)
		if err != nil {
			fail(w, 400, err)
			return
		}
		write(w, 200, map[string]string{"warning": warning})
		return
	}
	if path == "api/cloudflare/verify" && r.Method == "POST" {
		if e := s.Cloud.Verify(r.Context()); e != nil {
			fail(w, 503, e)
			return
		}
		write(w, 200, map[string]bool{"ok": true})
		return
	}
	if path == "api/cloudflare/recover" && r.Method == "POST" {
		if e := s.Cloud.Recover(r.Context()); e != nil {
			fail(w, 503, e)
			return
		}
		write(w, 200, map[string]bool{"ok": true})
		return
	}
	if path == "api/cloudflare/install" && r.Method == "POST" {
		var b struct {
			Confirmed bool `json:"confirmed"`
		}
		if e := decode(w, r, &b); e != nil || !b.Confirmed {
			fail(w, 400, errors.New("confirme a instalação oficial do cloudflared"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
		defer cancel()
		if e := s.Cloud.Install(ctx); e != nil {
			fail(w, 400, e)
			return
		}
		s.Cloud.Start()
		write(w, 200, map[string]bool{"ok": true})
		return
	}
	if path == "api/sessions" && r.Method == "GET" {
		sessions, e := s.Auth.Sessions()
		if e != nil {
			fail(w, 500, e)
			return
		}
		write(w, 200, sessions)
		return
	}
	if strings.HasPrefix(path, "api/sessions/") && r.Method == "DELETE" {
		if e := s.Auth.Revoke(strings.TrimPrefix(path, "api/sessions/")); e != nil {
			fail(w, 500, e)
			return
		}
		write(w, 200, map[string]bool{"ok": true})
		return
	}
	if path == "api/password" && r.Method == "POST" {
		var b struct {
			Current      string `json:"current"`
			Next         string `json:"next"`
			Confirmation string `json:"confirmation"`
		}
		if e := decode(w, r, &b); e != nil {
			fail(w, 400, e)
			return
		}
		if b.Next != b.Confirmation {
			fail(w, 400, errors.New("as senhas não coincidem"))
			return
		}
		if e := s.Auth.ChangePassword(b.Current, b.Next); e != nil {
			fail(w, 400, e)
			return
		}
		write(w, 200, map[string]bool{"ok": true})
		return
	}
	if path == "api/autostart" && r.Method == "POST" {
		var b struct {
			Enabled   bool `json:"enabled"`
			Confirmed bool `json:"confirmed"`
		}
		if e := decode(w, r, &b); e != nil {
			fail(w, 400, e)
			return
		}
		if !b.Confirmed || s.Autostart == nil {
			fail(w, 400, errors.New("confirme a alteração da inicialização automática"))
			return
		}
		if e := s.Autostart(b.Enabled); e != nil {
			fail(w, 400, e)
			return
		}
		write(w, 200, map[string]bool{"ok": true})
		return
	}
	http.NotFound(w, r)
}
func (s *Server) projectLocal(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) == 1 && r.Method == http.MethodDelete {
		id := parts[0]
		if _, err := s.Store.Project(id); err != nil {
			fail(w, 404, errors.New("projeto não encontrado"))
			return
		}
		warning, err := s.Cloud.RemoveProject(r.Context(), id, func() error { return s.Supervisor.Remove(id) })
		if err != nil {
			fail(w, 409, err)
			return
		}
		write(w, 200, map[string]string{"warning": warning})
		return
	}
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	id, section := parts[0], parts[1]
	p, e := s.Store.Project(id)
	if e != nil {
		fail(w, 404, errors.New("projeto não encontrado"))
		return
	}
	if section == "action" && r.Method == "POST" {
		s.action(w, r, id)
		return
	}
	if section == "logs" && r.Method == "GET" {
		logs, e := s.Supervisor.Logs(id)
		if e != nil {
			fail(w, 404, e)
			return
		}
		write(w, 200, logs)
		return
	}
	if section == "terminal" && r.Method == "GET" {
		s.terminal(w, r, id)
		return
	}
	if section == "terminal-enabled" && r.Method == "PATCH" {
		var body struct {
			Enabled *bool `json:"enabled"`
		}
		if err := decode(w, r, &body); err != nil {
			fail(w, 400, err)
			return
		}
		if body.Enabled == nil {
			fail(w, 400, errors.New("informe se o terminal deve ficar ativo"))
			return
		}
		if err := s.Supervisor.SetTerminalEnabled(id, *body.Enabled); err != nil {
			fail(w, 500, err)
			return
		}
		write(w, 200, map[string]bool{"ok": true})
		return
	}
	if section == "files" && r.Method == "GET" {
		files, e := projects.Files(p.Path, r.URL.Query().Get("path"))
		if e != nil {
			fail(w, 400, e)
			return
		}
		write(w, 200, files)
		return
	}
	if section == "file" && r.Method == "GET" {
		content, e := projects.Read(p.Path, r.URL.Query().Get("path"))
		if e != nil {
			fail(w, 400, e)
			return
		}
		write(w, 200, map[string]string{"content": content})
		return
	}
	if section == "route" && r.Method == "POST" {
		var b struct {
			Domain    string `json:"domain"`
			Subdomain string `json:"subdomain"`
			Port      int    `json:"port"`
			Confirmed bool   `json:"confirmed"`
		}
		if e = decode(w, r, &b); e != nil {
			fail(w, 400, e)
			return
		}
		if !b.Confirmed {
			fail(w, 400, errors.New("confirme a publicação deste endereço"))
			return
		}
		route, warning, e := s.Cloud.RouteTo(r.Context(), id, b.Domain, b.Subdomain, b.Port)
		if e != nil {
			fail(w, 400, e)
			return
		}
		write(w, 200, struct {
			model.Route
			Warning string `json:"warning,omitempty"`
		}{route, warning})
		return
	}
	if section == "route" && r.Method == "DELETE" {
		warning, err := s.Cloud.RemoveRoute(r.Context(), id)
		if err != nil {
			fail(w, 400, err)
			return
		}
		write(w, 200, map[string]string{"warning": warning})
		return
	}
	if section == "port" && r.Method == "PATCH" {
		var body struct {
			Port *int `json:"port"`
		}
		if err := decode(w, r, &body); err != nil {
			fail(w, 400, err)
			return
		}
		if body.Port == nil {
			fail(w, 400, errors.New("informe a porta do projeto"))
			return
		}
		if err := s.Cloud.ValidatePort(*body.Port, true); err != nil {
			fail(w, 400, err)
			return
		}
		if err := s.Cloud.UpdateProjectPort(func() error { return s.Supervisor.SetPort(id, *body.Port) }); err != nil {
			fail(w, 409, err)
			return
		}
		write(w, 200, map[string]bool{"ok": true})
		return
	}
	http.NotFound(w, r)
}
func (s *Server) action(w http.ResponseWriter, r *http.Request, id string) {
	var b struct {
		Action string `json:"action"`
	}
	if e := decode(w, r, &b); e != nil {
		fail(w, 400, e)
		return
	}
	op, e := s.Supervisor.Action(id, b.Action)
	if e != nil {
		fail(w, 409, e)
		return
	}
	write(w, 202, map[string]string{"operationId": op})
}
func (s *Server) remoteViews() []map[string]any {
	out := []map[string]any{}
	for _, v := range s.Supervisor.Views() {
		errorMessage := ""
		if v.RuntimeInfo.LastError != "" {
			errorMessage = "Falha na execução. Consulte os detalhes no aplicativo desktop."
		}
		execution := map[string]any{"actualState": v.RuntimeInfo.ActualState, "startedAt": v.RuntimeInfo.StartedAt, "cpu": v.RuntimeInfo.CPU, "memory": v.RuntimeInfo.Memory, "lastError": errorMessage}
		out = append(out, map[string]any{"id": v.ID, "name": v.Name, "runtime": v.Runtime, "execution": execution})
	}
	return out
}
func (s *Server) cookieName() string {
	if s.Development {
		return "cf_session_dev"
	}
	return "__Host-cloudforge-session"
}
func (s *Server) csrfName() string {
	if s.Development {
		return "cf_csrf_dev"
	}
	return "__Host-cloudforge-csrf"
}
func (s *Server) setCookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: !s.Development, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
}
func (s *Server) origin(r *http.Request) bool {
	expected := "https://" + s.Cloud.Config().AdminHost
	if s.Development {
		expected = "http://" + r.Host
	}
	return r.Header.Get("Origin") == expected
}
func (s *Server) session(r *http.Request) (auth.Session, string, error) {
	cookie, e := r.Cookie(s.cookieName())
	if e != nil {
		return auth.Session{}, "", e
	}
	session, e := s.Auth.Session(cookie.Value)
	return session, cookie.Value, e
}
func (s *Server) Public(w http.ResponseWriter, r *http.Request) {
	security(w)
	if !s.Development {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
	}
	path := r.URL.Path
	api := strings.HasPrefix(path, "/api/")
	if !api {
		s.static(w, r)
		return
	}
	if path == "/api/health" && r.Method == "GET" {
		write(w, 200, map[string]any{"ok": true, "service": "cloudforge"})
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !s.Development {
		if forwarded := r.Header.Get("CF-Connecting-IP"); net.ParseIP(forwarded) != nil {
			ip = forwarded
		}
	}
	if !s.limit.allow("all:"+ip, 180, time.Minute) {
		fail(w, 429, errors.New("muitas requisições; aguarde um minuto"))
		return
	}
	if r.Method != "GET" && !s.origin(r) {
		fail(w, 403, errors.New("origem da requisição não autorizada"))
		return
	}
	if path == "/api/session" && r.Method == "GET" {
		session, _, e := s.session(r)
		csrfCookie, _ := r.Cookie(s.csrfName())
		if e == nil && csrfCookie != nil && s.Auth.ValidCSRF(session, csrfCookie.Value) {
			write(w, 200, map[string]any{"authenticated": true, "username": session.Username, "expiresAt": session.ExpiresAt, "csrf": csrfCookie.Value})
			return
		}
		csrf := auth.Token()
		s.setCookie(w, s.csrfName(), csrf, 3600)
		write(w, 200, map[string]any{"authenticated": false, "csrf": csrf})
		return
	}
	if path == "/api/login" && r.Method == "POST" {
		if !s.limit.allow("login:"+ip, 5, 15*time.Minute) || !s.limit.allow("login-global", 40, 15*time.Minute) {
			fail(w, 429, errors.New("tentativas demais; aguarde 15 minutos"))
			return
		}
		csrf, e := r.Cookie(s.csrfName())
		if e != nil || subtle.ConstantTimeCompare([]byte(csrf.Value), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
			fail(w, 403, errors.New("proteção CSRF inválida; recarregue a página"))
			return
		}
		var b struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if e = decode(w, r, &b); e != nil {
			fail(w, 400, e)
			return
		}
		select {
		case s.loginGate <- struct{}{}:
			defer func() { <-s.loginGate }()
		default:
			fail(w, 429, errors.New("autenticação ocupada; tente novamente"))
			return
		}
		token, next, e := s.Auth.Login(b.Username, b.Password, r.UserAgent())
		if e != nil {
			fail(w, 401, e)
			return
		}
		s.setCookie(w, s.cookieName(), token, 30*86400)
		s.setCookie(w, s.csrfName(), next, 30*86400)
		write(w, 200, map[string]any{"authenticated": true, "csrf": next, "username": b.Username})
		return
	}
	session, token, e := s.session(r)
	if e != nil {
		fail(w, 401, errors.New("sua sessão expirou; entre novamente"))
		return
	}
	if r.Method != "GET" && !s.Auth.ValidCSRF(session, r.Header.Get("X-CSRF-Token")) {
		fail(w, 403, errors.New("proteção CSRF inválida"))
		return
	}
	if path == "/api/logout" && r.Method == "POST" {
		if e = s.Auth.Logout(token); e != nil {
			fail(w, 500, e)
			return
		}
		s.setCookie(w, s.cookieName(), "", -1)
		s.setCookie(w, s.csrfName(), "", -1)
		write(w, 200, map[string]bool{"ok": true})
		return
	}
	if path == "/api/renew" && r.Method == "POST" {
		if !s.limit.allow("renew:"+session.ID, 6, time.Minute) {
			fail(w, 429, errors.New("aguarde para renovar a sessão"))
			return
		}
		next, csrf, e := s.Auth.Renew(token, r.UserAgent())
		if e != nil {
			fail(w, 401, e)
			return
		}
		s.setCookie(w, s.cookieName(), next, 30*86400)
		s.setCookie(w, s.csrfName(), csrf, 30*86400)
		write(w, 200, map[string]string{"csrf": csrf})
		return
	}
	if path == "/api/projects" && r.Method == "GET" {
		write(w, 200, s.remoteViews())
		return
	}
	if path == "/api/events" && r.Method == "GET" {
		if !s.origin(r) {
			fail(w, 403, errors.New("origem WebSocket inválida"))
			return
		}
		s.stream(w, r, false, session)
		return
	}
	if strings.HasPrefix(path, "/api/projects/") && strings.HasSuffix(path, "/action") && r.Method == "POST" {
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) == 4 && s.limit.allow("action:"+session.ID, 30, time.Minute) {
			s.action(w, r, parts[2])
			return
		}
	}
	http.NotFound(w, r)
}
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.NotFound(w, r)
		return
	}
	if s.WebDir == "" {
		http.Error(w, "Dashboard ainda não compilada. Execute npm run build.", 503)
		return
	}
	clean := strings.TrimPrefix(filepath.Clean("/"+r.URL.Path), string(os.PathSeparator))
	file := filepath.Join(s.WebDir, clean)
	info, e := os.Stat(file)
	if e != nil || info.IsDir() {
		file = filepath.Join(s.WebDir, "index.html")
	}
	http.ServeFile(w, r, file)
}
func (s *Server) stream(w http.ResponseWriter, r *http.Request, local bool, session auth.Session) {
	select {
	case s.connections <- struct{}{}:
		defer func() { <-s.connections }()
	default:
		fail(w, 429, errors.New("limite de conexões em tempo real atingido"))
		return
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		if local {
			return r.Header.Get("Origin") == ""
		}
		return s.origin(r)
	}}
	ws, e := upgrader.Upgrade(w, r, nil)
	if e != nil {
		return
	}
	defer ws.Close()
	ws.SetReadLimit(1024)
	_ = ws.SetReadDeadline(time.Now().Add(60 * time.Second))
	ws.SetPongHandler(func(string) error { return ws.SetReadDeadline(time.Now().Add(60 * time.Second)) })
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, e := ws.ReadMessage(); e != nil {
				return
			}
		}
	}()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		var payload any
		if local {
			payload = s.Snapshot()
			if projectID := r.URL.Query().Get("project"); projectID != "" {
				if logs, e := s.Supervisor.Logs(projectID); e == nil {
					data := payload.(map[string]any)
					data["logProject"] = projectID
					data["logs"] = logs
				}
			}
		} else {
			if _, _, e := s.session(r); e != nil {
				return
			}
			payload = map[string]any{"projects": s.remoteViews()}
		}
		_ = ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if e = ws.WriteJSON(payload); e != nil {
			return
		}
		_ = ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second))
		select {
		case <-done:
			return
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

type rate struct {
	count int
	until time.Time
}
type limiter struct {
	mu      sync.Mutex
	entries map[string]rate
}

func (l *limiter) allow(key string, max int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = map[string]rate{}
	}
	now := time.Now()
	if len(l.entries) > 4096 {
		for k, v := range l.entries {
			if now.After(v.until) {
				delete(l.entries, k)
			}
		}
		if len(l.entries) > 8192 {
			return false
		}
	}
	v := l.entries[key]
	if now.After(v.until) {
		v = rate{until: now.Add(window)}
	}
	v.count++
	l.entries[key] = v
	return v.count <= max
}
