package api

import (
	"cloudforge/core/internal/auth"
	"cloudforge/core/internal/cluster"
	"cloudforge/core/internal/databases"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// LocalProvider implementation on *Server so cluster.Manager stays decoupled from api.

func (s *Server) LocalProjects() []cluster.SanitizedProject {
	out := []cluster.SanitizedProject{}
	for _, v := range s.Supervisor.Views() {
		errorMessage := ""
		if v.RuntimeInfo.LastError != "" {
			errorMessage = "Falha na execução. Consulte os detalhes no aplicativo desktop."
		}
		out = append(out, cluster.SanitizedProject{
			ID:       v.ID,
			Name:     v.Name,
			Runtime:  v.Runtime,
			Hostname: v.Hostname,
			Execution: cluster.SanitizedExecution{
				ActualState: v.RuntimeInfo.ActualState,
				StartedAt:   v.RuntimeInfo.StartedAt,
				CPU:         v.RuntimeInfo.CPU,
				Memory:      v.RuntimeInfo.Memory,
				LastError:   errorMessage,
			},
			UsageHistory: v.UsageHistory,
		})
	}
	return out
}

func (s *Server) LocalDatabases(ctx context.Context) (databases.View, error) {
	if s.Databases == nil {
		return databases.View{}, databases.ErrUnavailable
	}
	admin, err := s.Store.Admin()
	if err != nil {
		return databases.View{}, err
	}
	return s.Databases.List(ctx, admin.ID)
}

func (s *Server) LocalTunnelHealth(ctx context.Context) cluster.TunnelHealthReport {
	if s.Cloud == nil {
		return cluster.TunnelHealthReport{Status: "not-configured"}
	}
	cfg := s.Cloud.Config()
	if cfg.TunnelID == "" {
		return cluster.TunnelHealthReport{Configured: false, Healthy: false, Status: "not-configured"}
	}
	st := s.Cloud.Status(ctx)
	connected, _ := st["connected"].(bool)
	status, _ := st["status"].(string)
	if status == "" {
		status = "connecting"
	}
	return cluster.TunnelHealthReport{
		Configured: true,
		Healthy:    connected,
		Status:     status,
	}
}

func (s *Server) LocalAdminUsers() []cluster.RemoteUserSummary {
	admin, err := s.Store.Admin()
	if err != nil {
		return nil
	}
	return []cluster.RemoteUserSummary{{
		UserID:    admin.ID,
		Username:  admin.Username,
		AuthEpoch: admin.AuthEpoch,
	}}
}

func (s *Server) LocalPrimaryURL() string {
	if s.PrimaryURLOverride != "" {
		return strings.TrimRight(s.PrimaryURLOverride, "/")
	}
	if s.Cloud != nil {
		return s.Cloud.PublicURL()
	}
	return ""
}

func (s *Server) AuthenticateLocal(username, password, clientIP string) (string, string, int64, int, error) {
	if s.Auth == nil {
		return "", "", 0, http.StatusServiceUnavailable, errors.New("serviço de autenticação indisponível")
	}
	keyIP := clientIP
	if keyIP == "" {
		keyIP = "remote"
	}
	failKey := "remote-auth-fail:" + keyIP
	if s.limit.exceeded(failKey, 5) || s.limit.exceeded("remote-auth-fail-global", 40) {
		return "", "", 0, http.StatusTooManyRequests, errors.New("tentativas demais; aguarde 15 minutos")
	}
	select {
	case s.loginGate <- struct{}{}:
		defer func() { <-s.loginGate }()
	default:
		return "", "", 0, http.StatusTooManyRequests, errors.New("autenticação ocupada; tente novamente")
	}
	user, err := s.Auth.Authenticate(username, password)
	if err != nil {
		s.limit.hit(failKey, 15*time.Minute)
		s.limit.hit("remote-auth-fail-global", 15*time.Minute)
		return "", "", 0, http.StatusUnauthorized, errors.New("usuário ou senha inválidos")
	}
	s.limit.reset(failKey)
	return user.ID, user.Username, user.AuthEpoch, http.StatusOK, nil
}

func (s *Server) ExecuteLocalCommand(ctx context.Context, cmd cluster.ExecCommandReq) (json.RawMessage, int, error) {
	switch cmd.Kind {
	case cluster.CmdProjectAction:
		var body struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal(cmd.Payload, &body); err != nil {
			return nil, http.StatusBadRequest, errors.New("ação inválida")
		}
		op, err := s.Supervisor.Action(cmd.ResourceID, body.Action)
		if err != nil {
			return nil, http.StatusConflict, err
		}
		raw, _ := json.Marshal(map[string]string{"operationId": op})
		return raw, http.StatusAccepted, nil
	case cluster.CmdDatabaseList:
		if s.Databases == nil {
			return nil, http.StatusServiceUnavailable, databases.ErrUnavailable
		}
		admin, err := s.Store.Admin()
		if err != nil {
			return nil, http.StatusServiceUnavailable, errors.New("administrador local indisponível")
		}
		list, err := s.Databases.List(ctx, admin.ID)
		if err != nil {
			return nil, databaseStatus(err), err
		}
		list.LANFirewall = ""
		raw, _ := json.Marshal(list)
		return raw, http.StatusOK, nil
	case cluster.CmdDatabaseCreate:
		if s.Databases == nil {
			return nil, http.StatusServiceUnavailable, databases.ErrUnavailable
		}
		admin, err := s.Store.Admin()
		if err != nil {
			return nil, http.StatusServiceUnavailable, errors.New("administrador local indisponível")
		}
		var in databases.Input
		if err := json.Unmarshal(cmd.Payload, &in); err != nil {
			return nil, http.StatusBadRequest, errors.New("dados do banco inválidos")
		}
		inst, err := s.Databases.Create(ctx, admin.ID, in)
		if err != nil {
			return nil, databaseStatus(err), err
		}
		raw, _ := json.Marshal(inst)
		return raw, http.StatusAccepted, nil
	case cluster.CmdDatabaseDetail:
		if s.Databases == nil {
			return nil, http.StatusServiceUnavailable, databases.ErrUnavailable
		}
		admin, err := s.Store.Admin()
		if err != nil {
			return nil, http.StatusServiceUnavailable, errors.New("administrador local indisponível")
		}
		var req struct {
			Period string `json:"period"`
		}
		_ = json.Unmarshal(cmd.Payload, &req)
		window := time.Hour
		switch req.Period {
		case "10m":
			window = 10 * time.Minute
		case "24h":
			window = 24 * time.Hour
		}
		inst, metrics, err := s.Databases.Detail(admin.ID, cmd.ResourceID, window)
		if err != nil {
			return nil, databaseStatus(err), err
		}
		inst.LANFirewall = ""
		raw, _ := json.Marshal(map[string]any{"database": inst, "metrics": metrics})
		return raw, http.StatusOK, nil
	case cluster.CmdDatabaseChange:
		if s.Databases == nil {
			return nil, http.StatusServiceUnavailable, databases.ErrUnavailable
		}
		admin, err := s.Store.Admin()
		if err != nil {
			return nil, http.StatusServiceUnavailable, errors.New("administrador local indisponível")
		}
		var change databases.Change
		if err := json.Unmarshal(cmd.Payload, &change); err != nil {
			return nil, http.StatusBadRequest, errors.New("alteração inválida")
		}
		inst, err := s.Databases.Change(admin.ID, cmd.ResourceID, change)
		if err != nil {
			return nil, databaseStatus(err), err
		}
		raw, _ := json.Marshal(inst)
		return raw, http.StatusAccepted, nil
	case cluster.CmdDatabaseReveal:
		if s.Databases == nil {
			return nil, http.StatusServiceUnavailable, databases.ErrUnavailable
		}
		admin, err := s.Store.Admin()
		if err != nil {
			return nil, http.StatusServiceUnavailable, errors.New("administrador local indisponível")
		}
		creds, err := s.Databases.Reveal(admin.ID, cmd.ResourceID)
		if err != nil {
			return nil, databaseStatus(err), err
		}
		raw, _ := json.Marshal(creds)
		return raw, http.StatusOK, nil
	case cluster.CmdDatabaseCredentials:
		if s.Databases == nil {
			return nil, http.StatusServiceUnavailable, databases.ErrUnavailable
		}
		admin, err := s.Store.Admin()
		if err != nil {
			return nil, http.StatusServiceUnavailable, errors.New("administrador local indisponível")
		}
		creds, err := s.Databases.Credentials(admin.ID, cmd.ResourceID)
		if err != nil {
			return nil, databaseStatus(err), err
		}
		raw, _ := json.Marshal(creds)
		return raw, http.StatusOK, nil
	case cluster.CmdDatabaseCertificate:
		if s.Databases == nil {
			return nil, http.StatusServiceUnavailable, databases.ErrUnavailable
		}
		admin, err := s.Store.Admin()
		if err != nil {
			return nil, http.StatusServiceUnavailable, errors.New("administrador local indisponível")
		}
		var req struct {
			Format string `json:"format"`
		}
		if err := json.Unmarshal(cmd.Payload, &req); err != nil {
			return nil, http.StatusBadRequest, errors.New("formato inválido")
		}
		content, filename, err := s.Databases.Certificate(admin.ID, cmd.ResourceID, req.Format)
		if err != nil {
			return nil, databaseStatus(err), err
		}
		raw, _ := json.Marshal(map[string]string{"filename": filename, "content": string(content)})
		return raw, http.StatusOK, nil
	case cluster.CmdDatabaseTunnelGet:
		if s.Databases == nil || s.Cloud == nil {
			return nil, http.StatusNotFound, errors.New("túnel de banco indisponível")
		}
		admin, err := s.Store.Admin()
		if err != nil {
			return nil, http.StatusServiceUnavailable, errors.New("administrador local indisponível")
		}
		if _, _, err := s.Databases.Detail(admin.ID, cmd.ResourceID, 0); err != nil {
			return nil, databaseStatus(err), err
		}
		raw, _ := json.Marshal(s.Cloud.DatabaseTunnel(admin.ID, cmd.ResourceID, false))
		return raw, http.StatusOK, nil
	default:
		return nil, http.StatusForbidden, errors.New("operação não permitida pelo canal remoto")
	}
}

func (s *Server) UnpublishLocalPanel(ctx context.Context) (string, error) {
	if s.Cloud == nil {
		return "", nil
	}
	return s.Cloud.UnpublishPanel(ctx)
}

func databaseStatus(err error) int {
	switch {
	case errors.Is(err, databases.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, databases.ErrConflict):
		return http.StatusConflict
	case errors.Is(err, databases.ErrUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, context.Canceled):
		return http.StatusRequestTimeout
	default:
		return http.StatusBadRequest
	}
}

func (s *Server) localActor() cluster.Actor {
	actor := cluster.Actor{}
	if s.Cluster != nil {
		actor.IssuerNodeID = s.Cluster.InstallationID
	}
	if admin, err := s.Store.Admin(); err == nil {
		actor.UserID = admin.ID
		actor.Username = admin.Username
	}
	return actor
}

// Local loopback Admin API endpoints for Desktop pairing and cluster management.
func (s *Server) clusterLocalRoutes(w http.ResponseWriter, r *http.Request, parts []string) {
	if s.Cluster == nil {
		fail(w, http.StatusServiceUnavailable, errors.New("serviço de múltiplos computadores indisponível"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case len(parts) == 3 && parts[2] == "status" && r.Method == http.MethodGet:
		res := s.Cluster.Summary()
		cfg := s.Cloud.Config()
		res["panelPublished"] = !cfg.PanelDisabled && cfg.AdminHost != ""
		res["panelCleanupPending"] = cfg.PanelCleanupError != ""
		res["panelCleanupError"] = cfg.PanelCleanupError
		res["retiredAdminHost"] = cfg.RetiredAdminHost
		res["localTunnel"] = s.LocalTunnelHealth(r.Context())
		if s.Cluster.Role() == cluster.RolePrimary {
			snap := s.Cluster.ComputersSnapshot(r.Context())
			res["primary"] = snap["primary"]
			res["pending"] = snap["pending"]
			res["computers"] = snap["computers"]
			res["audit"] = snap["audit"]
			res["localPrimaryUrl"] = snap["primaryUrl"]
		}
		write(w, http.StatusOK, res)
	case len(parts) == 3 && parts[2] == "profile" && r.Method == http.MethodPatch:
		var body struct {
			DisplayName string `json:"displayName"`
		}
		if err := decode(w, r, &body); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		st, err := s.Cluster.UpdateLocalProfile(body.DisplayName)
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		write(w, http.StatusOK, st)
	case len(parts) == 4 && parts[2] == "primary" && parts[3] == "enable" && r.Method == http.MethodPost:
		st, err := s.Cluster.EnablePrimary()
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		write(w, http.StatusOK, st)
	case len(parts) == 3 && parts[2] == "invites" && r.Method == http.MethodPost:
		var body struct {
			Username   string `json:"username"`
			Password   string `json:"password"`
			PrimaryURL string `json:"primaryUrl,omitempty"`
		}
		if err := decode(w, r, &body); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		failKey := "cluster-invite-reauth:local"
		if s.limit.exceeded(failKey, 5) {
			fail(w, http.StatusTooManyRequests, errors.New("tentativas demais; aguarde 15 minutos"))
			return
		}
		select {
		case s.loginGate <- struct{}{}:
			defer func() { <-s.loginGate }()
		default:
			fail(w, http.StatusTooManyRequests, errors.New("autenticação ocupada; tente novamente"))
			return
		}
		user, err := s.Auth.Authenticate(body.Username, body.Password)
		if err != nil {
			s.limit.hit(failKey, 15*time.Minute)
			fail(w, http.StatusBadRequest, errors.New("usuário ou senha inválidos"))
			return
		}
		s.limit.reset(failKey)
		inv, err := s.Cluster.CreateInvite(user.ID, body.PrimaryURL)
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		write(w, http.StatusCreated, inv)
	case len(parts) == 3 && parts[2] == "link" && r.Method == http.MethodPost:
		var body struct {
			Invite      string `json:"invite"`
			DisplayName string `json:"displayName"`
			Confirmed   bool   `json:"confirmed"`
		}
		if err := decode(w, r, &body); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		if !body.Confirmed {
			fail(w, http.StatusBadRequest, errors.New("confirme o vínculo com o painel principal"))
			return
		}
		res, err := s.Cluster.StartLink(r.Context(), body.Invite, body.DisplayName)
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		write(w, http.StatusAccepted, res)
	case len(parts) == 3 && parts[2] == "unlink-cancel" && r.Method == http.MethodPost:
		if err := s.Cluster.CancelLink(); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		write(w, http.StatusOK, map[string]bool{"ok": true})
	case len(parts) == 5 && parts[2] == "nodes" && parts[4] == "approve" && r.Method == http.MethodPost:
		node, err := s.Cluster.ApproveNode(parts[3], s.localActor())
		if err != nil {
			fail(w, http.StatusConflict, err)
			return
		}
		write(w, http.StatusOK, node)
	case len(parts) == 5 && parts[2] == "nodes" && parts[4] == "reject" && r.Method == http.MethodPost:
		if err := s.Cluster.RejectNode(parts[3], s.localActor()); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		write(w, http.StatusOK, map[string]bool{"ok": true})
	case len(parts) == 4 && parts[2] == "nodes" && r.Method == http.MethodPatch:
		var body cluster.NodeUpdateInput
		if err := decode(w, r, &body); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		node, err := s.Cluster.UpdateNode(parts[3], body, s.localActor())
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		write(w, http.StatusOK, node)
	case len(parts) == 4 && parts[2] == "nodes" && r.Method == http.MethodDelete:
		if err := s.Cluster.RemoveNode(parts[3], s.localActor()); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		write(w, http.StatusOK, map[string]bool{"ok": true})
	case len(parts) == 3 && parts[2] == "grants" && r.Method == http.MethodPut:
		var body struct {
			IssuerNodeID string `json:"issuerNodeId"`
			UserID       string `json:"userId"`
			Username     string `json:"username,omitempty"`
			Scope        string `json:"scope"`
		}
		if err := decode(w, r, &body); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		grant, err := s.Cluster.SetPrincipalGrant(body.IssuerNodeID, body.UserID, body.Username, body.Scope, s.localActor())
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		write(w, http.StatusOK, grant)
	case len(parts) == 3 && parts[2] == "audit" && r.Method == http.MethodGet:
		entries, err := s.Cluster.ListAudit(100)
		if err != nil {
			fail(w, http.StatusInternalServerError, err)
			return
		}
		write(w, http.StatusOK, entries)
	default:
		http.NotFound(w, r)
	}
}

// Public panel helpers for federated auth, computers area, and node-scoped resource routing.

func (s *Server) sessionAccess(r *http.Request) (cluster.SessionAccess, string, error) {
	session, token, err := s.session(r)
	if err != nil {
		return cluster.SessionAccess{}, "", err
	}
	if s.Cluster == nil {
		return cluster.SessionAccess{
			Session:            session,
			Scope:              cluster.ScopeUnified,
			CanManageComputers: true,
		}, token, nil
	}
	access, err := s.Cluster.ValidateSession(r.Context(), session)
	if err != nil {
		return cluster.SessionAccess{}, "", err
	}
	return access, token, nil
}

func (s *Server) sessionPayload(access cluster.SessionAccess, csrf string) map[string]any {
	primaryID := ""
	primaryName := ""
	if s.Cluster != nil {
		primaryID = s.Cluster.InstallationID
		primaryName = s.Cluster.State().DisplayName
	}
	return map[string]any{
		"authenticated":      true,
		"csrf":               csrf,
		"username":           access.Session.Username,
		"userId":             access.Session.UserID,
		"issuerNodeId":       access.IssuerNodeID,
		"issuerNodeName":     access.IssuerNodeName,
		"scope":              access.Scope,
		"canManageComputers": access.CanManageComputers,
		"primaryNodeId":      primaryID,
		"primaryNodeName":    primaryName,
		"expiresAt":          access.Session.ExpiresAt,
	}
}

func (s *Server) issueFederatedLoginResponse(w http.ResponseWriter, r *http.Request, node cluster.NodeRecord, resp cluster.AuthVerifyResp) {
	token, next, err := s.Auth.IssueFederated(node.ID, resp.UserID, resp.Username, resp.AuthEpoch, r.UserAgent())
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	session, err := s.Auth.Session(token)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	access, err := s.Cluster.ValidateSession(r.Context(), session)
	if err != nil {
		fail(w, http.StatusUnauthorized, err)
		return
	}
	s.setCookie(w, s.cookieName(), token, 30*86400)
	s.setCookie(w, s.csrfName(), next, 30*86400)
	s.Cluster.RecordAudit(cluster.AuditEntry{
		ActorNodeID:   node.ID,
		ActorUserID:   resp.UserID,
		ActorUsername: resp.Username,
		TargetNodeID:  s.Cluster.InstallationID,
		ResourceType:  "session",
		ResourceID:    session.ID,
		Operation:     "auth.federated_login",
		Result:        "success",
	})
	write(w, http.StatusOK, s.sessionPayload(access, next))
}

func (s *Server) publicLogin(w http.ResponseWriter, r *http.Request, ip string) {
	if !s.limit.allow("login:"+ip, 5, 15*time.Minute) || !s.limit.allow("login-global", 40, 15*time.Minute) {
		fail(w, http.StatusTooManyRequests, errors.New("tentativas demais; aguarde 15 minutos"))
		return
	}
	csrf, e := r.Cookie(s.csrfName())
	if e != nil || !constantTimeEqualStr(csrf.Value, r.Header.Get("X-CSRF-Token")) {
		fail(w, http.StatusForbidden, errors.New("proteção CSRF inválida; recarregue a página"))
		return
	}
	var b struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		AccountHost string `json:"accountHost,omitempty"`
	}
	if e = decode(w, r, &b); e != nil {
		fail(w, http.StatusBadRequest, e)
		return
	}
	select {
	case s.loginGate <- struct{}{}:
		defer func() { <-s.loginGate }()
	default:
		fail(w, http.StatusTooManyRequests, errors.New("autenticação ocupada; tente novamente"))
		return
	}

	host := strings.ToLower(strings.TrimSpace(b.AccountHost))
	if host != "" && host != "primary" && host != "principal" {
		// Explicit secondary computer login (kept optional for backward compatibility).
		if s.Cluster == nil || s.Cluster.Role() != cluster.RolePrimary {
			write(w, http.StatusServiceUnavailable, map[string]string{
				"error": "Computador de origem indisponível ou não autorizado para login. Tente novamente ou escolha outra conta.",
				"code":  "origin_unavailable",
			})
			return
		}
		if !s.limit.allow("login-host:"+ip+":"+host, 5, 15*time.Minute) {
			fail(w, http.StatusTooManyRequests, errors.New("tentativas demais; aguarde 15 minutos"))
			return
		}
		node, resp, available, err := s.Cluster.AuthenticateFederated(r.Context(), host, b.Username, b.Password, ip)
		if !available || err != nil {
			write(w, http.StatusServiceUnavailable, map[string]string{
				"error": "Computador de origem indisponível ou não autorizado para login. Tente novamente ou escolha outra conta.",
				"code":  "origin_unavailable",
			})
			return
		}
		if !resp.OK {
			code := resp.StatusCode
			if code == 0 {
				code = http.StatusUnauthorized
			}
			msg := resp.Error
			if msg == "" {
				msg = "usuário ou senha inválidos"
			}
			fail(w, code, errors.New(msg))
			return
		}
		s.issueFederatedLoginResponse(w, r, node, resp)
		return
	}

	// 1. Verify against the primary computer first -> full access.
	if token, next, err := s.Auth.Login(b.Username, b.Password, r.UserAgent()); err == nil {
		s.setCookie(w, s.cookieName(), token, 30*86400)
		s.setCookie(w, s.csrfName(), next, 30*86400)
		session, _ := s.Auth.Session(token)
		access := cluster.SessionAccess{
			Session:            session,
			Scope:              cluster.ScopeUnified,
			CanManageComputers: true,
		}
		if s.Cluster != nil {
			access.IssuerNodeID = s.Cluster.InstallationID
			access.IssuerNodeName = s.Cluster.State().DisplayName
		}
		write(w, http.StatusOK, s.sessionPayload(access, next))
		return
	}

	// 2. Automatically verify against linked secondary computers using only username + password.
	if host == "" && s.Cluster != nil && s.Cluster.Role() == cluster.RolePrimary {
		node, resp, matchedOffline, err := s.Cluster.AuthenticateFederatedByCredentials(r.Context(), b.Username, b.Password, ip)
		if err == nil && resp.OK {
			s.issueFederatedLoginResponse(w, r, node, resp)
			return
		}
		if err == nil && resp.StatusCode == http.StatusTooManyRequests {
			msg := resp.Error
			if msg == "" {
				msg = "tentativas demais; aguarde 15 minutos"
			}
			fail(w, http.StatusTooManyRequests, errors.New(msg))
			return
		}
		if err == nil && matchedOffline {
			if _, primaryErr := s.Store.UserByUsername(strings.TrimSpace(b.Username)); primaryErr != nil {
				write(w, http.StatusServiceUnavailable, map[string]string{
					"error": "O computador de origem desta conta está offline no momento. Tente novamente quando ele estiver conectado.",
					"code":  "origin_unavailable",
				})
				return
			}
		}
	}

	fail(w, http.StatusUnauthorized, errors.New("usuário ou senha inválidos"))
}

func (s *Server) publicStepUp(w http.ResponseWriter, r *http.Request, access cluster.SessionAccess, ip string) {
	w.Header().Set("Cache-Control", "no-store")
	if s.Cluster == nil {
		fail(w, http.StatusServiceUnavailable, errors.New("serviço indisponível"))
		return
	}
	var body struct {
		Username     string `json:"username"`
		Password     string `json:"password"`
		TargetNodeID string `json:"targetNodeId"`
		ResourceID   string `json:"resourceId"`
		Action       string `json:"action"`
	}
	if err := decode(w, r, &body); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	failKey := "stepup-fail:" + ip + ":" + access.IssuerNodeID + ":" + access.Session.UserID
	if s.limit.exceeded(failKey, 5) || s.limit.exceeded("stepup-fail-global", 40) {
		fail(w, http.StatusTooManyRequests, errors.New("tentativas demais; aguarde 15 minutos"))
		return
	}
	token, grant, code, err := s.Cluster.IssueStepUpGrant(
		r.Context(), access, body.TargetNodeID, body.ResourceID, body.Action, body.Username, body.Password, ip,
	)
	if err != nil {
		if code == http.StatusBadRequest || code == http.StatusUnauthorized {
			s.limit.hit(failKey, 15*time.Minute)
			s.limit.hit("stepup-fail-global", 15*time.Minute)
		}
		if code == http.StatusUnauthorized {
			code = http.StatusBadRequest
		}
		fail(w, code, err)
		return
	}
	s.limit.reset(failKey)
	write(w, http.StatusOK, map[string]any{
		"stepUpToken": token,
		"expiresAt":   grant.ExpiresAt,
	})
}

func (s *Server) publicComputersRoutes(w http.ResponseWriter, r *http.Request, access cluster.SessionAccess, parts []string) {
	if s.Cluster == nil || !access.CanManageComputers {
		fail(w, http.StatusForbidden, errors.New("Acesso negado"))
		return
	}
	actor := cluster.Actor{
		IssuerNodeID: access.IssuerNodeID,
		UserID:       access.Session.UserID,
		Username:     access.Session.Username,
	}
	switch {
	case len(parts) == 2 && r.Method == http.MethodGet:
		write(w, http.StatusOK, s.Cluster.ComputersSnapshot(r.Context()))
	case len(parts) == 3 && parts[2] == "primary" && r.Method == http.MethodPatch:
		var body struct {
			DisplayName string `json:"displayName"`
		}
		if err := decode(w, r, &body); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		st, err := s.Cluster.UpdateLocalProfile(body.DisplayName)
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		write(w, http.StatusOK, st)
	case len(parts) == 3 && parts[2] == "grants" && r.Method == http.MethodPut:
		var body struct {
			IssuerNodeID string `json:"issuerNodeId"`
			UserID       string `json:"userId"`
			Username     string `json:"username,omitempty"`
			Scope        string `json:"scope"`
		}
		if err := decode(w, r, &body); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		grant, err := s.Cluster.SetPrincipalGrant(body.IssuerNodeID, body.UserID, body.Username, body.Scope, actor)
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		write(w, http.StatusOK, grant)
	case len(parts) == 4 && parts[3] == "approve" && r.Method == http.MethodPost:
		node, err := s.Cluster.ApproveNode(parts[2], actor)
		if err != nil {
			fail(w, http.StatusConflict, err)
			return
		}
		write(w, http.StatusOK, node)
	case len(parts) == 4 && parts[3] == "reject" && r.Method == http.MethodPost:
		if err := s.Cluster.RejectNode(parts[2], actor); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		write(w, http.StatusOK, map[string]bool{"ok": true})
	case len(parts) == 3 && r.Method == http.MethodPatch:
		var body cluster.NodeUpdateInput
		if err := decode(w, r, &body); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		node, err := s.Cluster.UpdateNode(parts[2], body, actor)
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		write(w, http.StatusOK, node)
	case len(parts) == 3 && r.Method == http.MethodDelete:
		if err := s.Cluster.RemoveNode(parts[2], actor); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		write(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		http.NotFound(w, r)
	}
}

// Strict node-scoped routing on Y's public API: /api/nodes/{nodeId}/(projects|databases)/...
// An action targeting nodeId X NEVER falls back to Y, even if a resource with the same ID exists on Y.
func (s *Server) publicNodeRoutes(w http.ResponseWriter, r *http.Request, access cluster.SessionAccess, ip string, parts []string) {
	if s.Cluster == nil || len(parts) < 4 {
		http.NotFound(w, r)
		return
	}
	targetNodeID := parts[2]
	if !s.Cluster.CanAccessNode(access, targetNodeID) {
		fail(w, http.StatusForbidden, errors.New("Acesso negado"))
		return
	}
	isLocalTarget := targetNodeID == s.Cluster.InstallationID

	// Projects on a specific computer: POST /api/nodes/{nodeId}/projects/{projectId}/action
	if parts[3] == "projects" {
		if len(parts) == 6 && parts[5] == "action" && r.Method == http.MethodPost {
			if !s.limit.allow("action:"+access.Session.ID, 30, time.Minute) {
				fail(w, http.StatusTooManyRequests, errors.New("aguarde para executar outra ação"))
				return
			}
			var body struct {
				Action      string `json:"action"`
				OperationID string `json:"operationId,omitempty"`
			}
			if err := decode(w, r, &body); err != nil {
				fail(w, http.StatusBadRequest, err)
				return
			}
			projectID := parts[4]
			if isLocalTarget {
				op, err := s.Supervisor.Action(projectID, body.Action)
				if err != nil {
					fail(w, http.StatusConflict, err)
					return
				}
				s.Cluster.RecordAudit(cluster.AuditEntry{
					ActorNodeID:   access.IssuerNodeID,
					ActorUserID:   access.Session.UserID,
					ActorUsername: access.Session.Username,
					TargetNodeID:  targetNodeID,
					ResourceType:  "project",
					ResourceID:    projectID,
					Operation:     "project." + body.Action,
					OperationID:   body.OperationID,
					Result:        "success",
				})
				write(w, http.StatusAccepted, map[string]string{"operationId": op})
				return
			}
			payload, _ := json.Marshal(map[string]string{"action": body.Action})
			data, code, err := s.Cluster.DispatchCommand(r.Context(), access, targetNodeID, cluster.ExecCommandReq{
				OperationID: body.OperationID,
				Kind:        cluster.CmdProjectAction,
				ResourceID:  projectID,
				Payload:     payload,
			})
			if err != nil {
				fail(w, code, err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(code)
			_, _ = w.Write(data)
			return
		}
		http.NotFound(w, r)
		return
	}

	// Databases on a specific computer: /api/nodes/{nodeId}/databases...
	if parts[3] == "databases" {
		if r.Method != http.MethodGet && !s.limit.allow("database:"+access.IssuerNodeID+":"+access.Session.UserID, 20, time.Minute) {
			fail(w, http.StatusTooManyRequests, errors.New("aguarde para executar outra operação"))
			return
		}
		s.publicNodeDatabaseRoutes(w, r, access, ip, targetNodeID, isLocalTarget, parts[3:])
		return
	}

	http.NotFound(w, r)
}

func (s *Server) publicNodeDatabaseRoutes(w http.ResponseWriter, r *http.Request, access cluster.SessionAccess, ip, targetNodeID string, isLocalTarget bool, dbParts []string) {
	// dbParts starts with "databases":
	// [databases] -> POST create
	// [databases, id] -> GET detail, PATCH mutate
	// [databases, id, credentials] -> POST one-time reveal
	// [databases, id, password] -> POST view password (sensitive -> step-up!)
	// [databases, id, tunnel] -> GET read-only tunnel info
	// [databases, id, certificates, fmt] -> GET cert (fmt=="key" is sensitive -> step-up!)
	w.Header().Set("Cache-Control", "no-store")

	localOwner := func() (string, error) {
		admin, err := s.Store.Admin()
		if err != nil {
			return "", errors.New("serviço de bancos de dados indisponível")
		}
		return admin.ID, nil
	}

	if len(dbParts) == 1 && r.Method == http.MethodPost {
		var in databases.Input
		if err := decode(w, r, &in); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		if isLocalTarget {
			if s.Databases == nil {
				fail(w, http.StatusServiceUnavailable, databases.ErrUnavailable)
				return
			}
			owner, err := localOwner()
			if err != nil {
				fail(w, http.StatusServiceUnavailable, err)
				return
			}
			inst, err := s.Databases.Create(r.Context(), owner, in)
			if err != nil {
				databaseError(w, err)
				return
			}
			s.Cluster.RecordAudit(cluster.AuditEntry{
				ActorNodeID:   access.IssuerNodeID,
				ActorUserID:   access.Session.UserID,
				ActorUsername: access.Session.Username,
				TargetNodeID:  targetNodeID,
				ResourceType:  "database",
				ResourceID:    inst.ID,
				Operation:     "database.create",
				Result:        "success",
			})
			write(w, http.StatusAccepted, cluster.AggregatedDatabase{
				Instance:       inst,
				NodeID:         targetNodeID,
				NodeName:       s.Cluster.State().DisplayName,
				NodeOnline:     true,
				NodeLastSeenAt: time.Now().Unix(),
			})
			return
		}
		payload, _ := json.Marshal(in)
		data, code, err := s.Cluster.DispatchCommand(r.Context(), access, targetNodeID, cluster.ExecCommandReq{
			OperationID: r.Header.Get("X-Operation-Id"),
			Kind:        cluster.CmdDatabaseCreate,
			Payload:     payload,
		})
		if err != nil {
			fail(w, code, err)
			return
		}
		var inst databases.Instance
		if json.Unmarshal(data, &inst) == nil {
			nodeName := targetNodeID
			for _, n := range s.Cluster.AuthorizedNodes(r.Context(), access) {
				if n.ID == targetNodeID {
					nodeName = n.DisplayName
					break
				}
			}
			write(w, code, cluster.AggregatedDatabase{
				Instance:       inst,
				NodeID:         targetNodeID,
				NodeName:       nodeName,
				NodeOnline:     true,
				NodeLastSeenAt: time.Now().Unix(),
			})
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		_, _ = w.Write(data)
		return
	}

	if len(dbParts) < 2 {
		http.NotFound(w, r)
		return
	}
	dbID := dbParts[1]

	switch {
	case len(dbParts) == 2 && r.Method == http.MethodGet:
		period := r.URL.Query().Get("period")
		if isLocalTarget {
			if s.Databases == nil {
				fail(w, http.StatusServiceUnavailable, databases.ErrUnavailable)
				return
			}
			owner, err := localOwner()
			if err != nil {
				fail(w, http.StatusServiceUnavailable, err)
				return
			}
			window := time.Hour
			switch period {
			case "10m":
				window = 10 * time.Minute
			case "24h":
				window = 24 * time.Hour
			}
			inst, metrics, err := s.Databases.Detail(owner, dbID, window)
			if err != nil {
				databaseError(w, err)
				return
			}
			inst.LANFirewall = ""
			write(w, http.StatusOK, map[string]any{
				"database": cluster.AggregatedDatabase{
					Instance:       inst,
					NodeID:         targetNodeID,
					NodeName:       s.Cluster.State().DisplayName,
					NodeOnline:     true,
					NodeLastSeenAt: time.Now().Unix(),
				},
				"metrics": metrics,
			})
			return
		}
		payload, _ := json.Marshal(map[string]string{"period": period})
		data, code, err := s.Cluster.DispatchCommand(r.Context(), access, targetNodeID, cluster.ExecCommandReq{
			Kind:       cluster.CmdDatabaseDetail,
			ResourceID: dbID,
			Payload:    payload,
		})
		if err != nil {
			fail(w, code, err)
			return
		}
		var detail struct {
			Database databases.Instance `json:"database"`
			Metrics  []databases.Metric `json:"metrics"`
		}
		if json.Unmarshal(data, &detail) == nil {
			nodeName := targetNodeID
			for _, n := range s.Cluster.AuthorizedNodes(r.Context(), access) {
				if n.ID == targetNodeID {
					nodeName = n.DisplayName
					break
				}
			}
			write(w, code, map[string]any{
				"database": cluster.AggregatedDatabase{
					Instance:       detail.Database,
					NodeID:         targetNodeID,
					NodeName:       nodeName,
					NodeOnline:     true,
					NodeLastSeenAt: time.Now().Unix(),
				},
				"metrics": detail.Metrics,
			})
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		_, _ = w.Write(data)

	case len(dbParts) == 2 && r.Method == http.MethodPatch:
		var body struct {
			Action       string `json:"action"`
			Name         string `json:"name,omitempty"`
			Confirmation string `json:"confirmation,omitempty"`
			OperationID  string `json:"operationId,omitempty"`
		}
		if err := decode(w, r, &body); err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		change := databases.Change{Action: body.Action, Name: body.Name, Confirmation: body.Confirmation}
		var stepUpGrant *cluster.StepUpGrant
		if change.Action == "delete" {
			stepToken := strings.TrimSpace(r.Header.Get("X-Step-Up-Token"))
			grant, err := s.Cluster.ConsumeStepUpGrant(stepToken, access, targetNodeID, dbID, cluster.StepUpDatabaseDelete)
			if err != nil {
				fail(w, http.StatusForbidden, err)
				return
			}
			stepUpGrant = &grant
		}
		if isLocalTarget {
			if s.Databases == nil {
				fail(w, http.StatusServiceUnavailable, databases.ErrUnavailable)
				return
			}
			owner, err := localOwner()
			if err != nil {
				fail(w, http.StatusServiceUnavailable, err)
				return
			}
			inst, err := s.Databases.Change(owner, dbID, change)
			if err != nil {
				databaseError(w, err)
				return
			}
			s.Cluster.RecordAudit(cluster.AuditEntry{
				ActorNodeID:   access.IssuerNodeID,
				ActorUserID:   access.Session.UserID,
				ActorUsername: access.Session.Username,
				TargetNodeID:  targetNodeID,
				ResourceType:  "database",
				ResourceID:    dbID,
				Operation:     "database." + change.Action,
				OperationID:   body.OperationID,
				Result:        "success",
			})
			write(w, http.StatusAccepted, cluster.AggregatedDatabase{
				Instance:       inst,
				NodeID:         targetNodeID,
				NodeName:       s.Cluster.State().DisplayName,
				NodeOnline:     true,
				NodeLastSeenAt: time.Now().Unix(),
			})
			return
		}
		opID := body.OperationID
		if opID == "" {
			opID = r.Header.Get("X-Operation-Id")
		}
		payload, _ := json.Marshal(change)
		data, code, err := s.Cluster.DispatchCommand(r.Context(), access, targetNodeID, cluster.ExecCommandReq{
			OperationID: opID,
			Kind:        cluster.CmdDatabaseChange,
			ResourceID:  dbID,
			Payload:     payload,
			StepUp:      stepUpGrant,
		})
		if err != nil {
			fail(w, code, err)
			return
		}
		var inst databases.Instance
		if json.Unmarshal(data, &inst) == nil {
			nodeName := targetNodeID
			for _, n := range s.Cluster.AuthorizedNodes(r.Context(), access) {
				if n.ID == targetNodeID {
					nodeName = n.DisplayName
					break
				}
			}
			write(w, code, cluster.AggregatedDatabase{
				Instance:       inst,
				NodeID:         targetNodeID,
				NodeName:       nodeName,
				NodeOnline:     true,
				NodeLastSeenAt: time.Now().Unix(),
			})
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		_, _ = w.Write(data)

	case len(dbParts) == 3 && dbParts[2] == "credentials" && r.Method == http.MethodPost:
		if isLocalTarget {
			if s.Databases == nil {
				fail(w, http.StatusServiceUnavailable, databases.ErrUnavailable)
				return
			}
			owner, err := localOwner()
			if err != nil {
				fail(w, http.StatusServiceUnavailable, err)
				return
			}
			creds, err := s.Databases.Reveal(owner, dbID)
			if err != nil {
				databaseError(w, err)
				return
			}
			write(w, http.StatusOK, creds)
			return
		}
		data, code, err := s.Cluster.DispatchCommand(r.Context(), access, targetNodeID, cluster.ExecCommandReq{
			OperationID: r.Header.Get("X-Operation-Id"),
			Kind:        cluster.CmdDatabaseReveal,
			ResourceID:  dbID,
		})
		if err != nil {
			fail(w, code, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		_, _ = w.Write(data)

	case len(dbParts) == 3 && dbParts[2] == "password" && r.Method == http.MethodPost:
		s.publicDatabasePassword(w, r, access, ip, targetNodeID, isLocalTarget, dbID)

	case len(dbParts) == 3 && dbParts[2] == "tunnel" && r.Method == http.MethodGet:
		if isLocalTarget {
			if s.Databases == nil || s.Cloud == nil {
				http.NotFound(w, r)
				return
			}
			owner, err := localOwner()
			if err != nil {
				fail(w, http.StatusServiceUnavailable, err)
				return
			}
			if _, _, err := s.Databases.Detail(owner, dbID, 0); err != nil {
				databaseError(w, err)
				return
			}
			write(w, http.StatusOK, s.Cloud.DatabaseTunnel(owner, dbID, false))
			return
		}
		data, code, err := s.Cluster.DispatchCommand(r.Context(), access, targetNodeID, cluster.ExecCommandReq{
			Kind:       cluster.CmdDatabaseTunnelGet,
			ResourceID: dbID,
		})
		if err != nil {
			fail(w, code, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		_, _ = w.Write(data)

	case len(dbParts) == 4 && dbParts[2] == "certificates" && r.Method == http.MethodGet:
		format := dbParts[3]
		var stepUpGrant *cluster.StepUpGrant
		if format == "key" {
			stepToken := strings.TrimSpace(r.Header.Get("X-Step-Up-Token"))
			grant, err := s.Cluster.ConsumeStepUpGrant(stepToken, access, targetNodeID, dbID, cluster.StepUpDatabaseExportKey)
			if err != nil {
				fail(w, http.StatusForbidden, err)
				return
			}
			stepUpGrant = &grant
		}
		if isLocalTarget {
			if s.Databases == nil {
				fail(w, http.StatusServiceUnavailable, databases.ErrUnavailable)
				return
			}
			owner, err := localOwner()
			if err != nil {
				fail(w, http.StatusServiceUnavailable, err)
				return
			}
			content, filename, err := s.Databases.Certificate(owner, dbID, format)
			if err != nil {
				databaseError(w, err)
				return
			}
			if format == "key" {
				s.Cluster.RecordAudit(cluster.AuditEntry{
					ActorNodeID:   access.IssuerNodeID,
					ActorUserID:   access.Session.UserID,
					ActorUsername: access.Session.Username,
					TargetNodeID:  targetNodeID,
					ResourceType:  "database",
					ResourceID:    dbID,
					Operation:     "database.export_key",
					Result:        "success",
				})
			}
			write(w, http.StatusOK, map[string]string{"filename": filename, "content": string(content)})
			return
		}
		payload, _ := json.Marshal(map[string]string{"format": format})
		data, code, err := s.Cluster.DispatchCommand(r.Context(), access, targetNodeID, cluster.ExecCommandReq{
			Kind:       cluster.CmdDatabaseCertificate,
			ResourceID: dbID,
			Payload:    payload,
			StepUp:     stepUpGrant,
		})
		if err != nil {
			fail(w, code, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		_, _ = w.Write(data)

	default:
		http.NotFound(w, r)
	}
}

func (s *Server) publicDatabasePassword(w http.ResponseWriter, r *http.Request, access cluster.SessionAccess, ip, targetNodeID string, isLocalTarget bool, dbID string) {
	w.Header().Set("Cache-Control", "no-store")
	var body struct {
		Username    string `json:"username,omitempty"`
		Password    string `json:"password,omitempty"`
		StepUpToken string `json:"stepUpToken,omitempty"`
	}
	if err := decode(w, r, &body); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}

	stepToken := strings.TrimSpace(r.Header.Get("X-Step-Up-Token"))
	if stepToken == "" {
		stepToken = strings.TrimSpace(body.StepUpToken)
	}

	var grant cluster.StepUpGrant
	if stepToken != "" {
		var err error
		grant, err = s.Cluster.ConsumeStepUpGrant(stepToken, access, targetNodeID, dbID, cluster.StepUpDatabasePassword)
		if err != nil {
			fail(w, http.StatusForbidden, err)
			return
		}
	} else {
		failKey := "db-password-fail:" + ip + ":" + access.IssuerNodeID + ":" + access.Session.UserID
		if s.limit.exceeded(failKey, 5) || s.limit.exceeded("db-password-fail-global", 40) {
			fail(w, http.StatusTooManyRequests, errors.New("tentativas demais; aguarde 15 minutos"))
			return
		}
		tok, _, code, err := s.Cluster.IssueStepUpGrant(
			r.Context(), access, targetNodeID, dbID, cluster.StepUpDatabasePassword, body.Username, body.Password, ip,
		)
		if err != nil {
			if code == http.StatusBadRequest || code == http.StatusUnauthorized {
				s.limit.hit(failKey, 15*time.Minute)
				s.limit.hit("db-password-fail-global", 15*time.Minute)
				code = http.StatusBadRequest
			}
			fail(w, code, err)
			return
		}
		s.limit.reset(failKey)
		grant, err = s.Cluster.ConsumeStepUpGrant(tok, access, targetNodeID, dbID, cluster.StepUpDatabasePassword)
		if err != nil {
			fail(w, http.StatusForbidden, err)
			return
		}
	}

	if isLocalTarget {
		if s.Databases == nil {
			fail(w, http.StatusServiceUnavailable, databases.ErrUnavailable)
			return
		}
		admin, err := s.Store.Admin()
		if err != nil {
			fail(w, http.StatusServiceUnavailable, errors.New("administrador local indisponível"))
			return
		}
		creds, err := s.Databases.Credentials(admin.ID, dbID)
		if err != nil {
			databaseError(w, err)
			return
		}
		s.Cluster.RecordAudit(cluster.AuditEntry{
			ActorNodeID:   access.IssuerNodeID,
			ActorUserID:   access.Session.UserID,
			ActorUsername: access.Session.Username,
			TargetNodeID:  targetNodeID,
			ResourceType:  "database",
			ResourceID:    dbID,
			Operation:     "database.password",
			Result:        "success",
		})
		write(w, http.StatusOK, creds)
		return
	}

	data, code, err := s.Cluster.DispatchCommand(r.Context(), access, targetNodeID, cluster.ExecCommandReq{
		Kind:       cluster.CmdDatabaseCredentials,
		ResourceID: dbID,
		StepUp:     &grant,
	})
	if err != nil {
		fail(w, code, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(data)
}

func constantTimeEqualStr(a, b string) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	return auth.Digest(a) == auth.Digest(b)
}
