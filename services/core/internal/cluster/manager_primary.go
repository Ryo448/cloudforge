package cluster

import (
	"cloudforge/core/internal/auth"
	"cloudforge/core/internal/databases"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"net/http"
	"strings"
	"time"
)

type InviteResult struct {
	Invite             string `json:"invite"`
	InviteID           string `json:"inviteId"`
	ExpiresAt          int64  `json:"expiresAt"`
	PrimaryURL         string `json:"primaryUrl"`
	PrimaryFingerprint string `json:"primaryFingerprint"`
}

// CreateInvite enables primary mode if standalone, generates a single-use short-lived
// invite secret, stores only its SHA-256 hash, and returns the encoded invite once.
func (m *Manager) CreateInvite(createdByUserID, primaryURLOverride string) (InviteResult, error) {
	st, err := m.loadState()
	if err != nil {
		return InviteResult{}, err
	}
	if st.Role == RoleSecondary {
		return InviteResult{}, errors.New("este computador está vinculado a um painel principal e não pode gerar convites")
	}
	primaryURL := ""
	if m.Provider != nil {
		primaryURL = m.Provider.LocalPrimaryURL()
	}
	if primaryURL == "" && m.Development && primaryURLOverride != "" {
		primaryURL = strings.TrimRight(strings.TrimSpace(primaryURLOverride), "/")
	}
	if primaryURL == "" {
		return InviteResult{}, errors.New("configure o domínio público do painel na seção Cloudflare antes de adicionar computadores")
	}
	id, err := m.ensureIdentity(false)
	if err != nil {
		return InviteResult{}, err
	}
	st, err = m.loadState()
	if err != nil {
		return InviteResult{}, err
	}
	if st.Role != RolePrimary {
		st.Role = RolePrimary
		st.LinkStatus = LinkStatusNone
		if err := m.saveState(st); err != nil {
			return InviteResult{}, err
		}
	}

	secret, err := randomSecret()
	if err != nil {
		return InviteResult{}, err
	}
	inviteID := auth.ID()
	expiresAt := time.Now().Add(m.InviteTTL).Unix()
	if err := m.insertInvite(inviteID, secretDigest(secret), createdByUserID, expiresAt); err != nil {
		return InviteResult{}, err
	}
	payload := InvitePayload{
		Version:            ProtocolVersion,
		InviteID:           inviteID,
		PrimaryURL:         primaryURL,
		PrimaryID:          m.InstallationID,
		PrimaryFingerprint: id.Fingerprint,
		PrimaryName:        st.DisplayName,
		Secret:             secret,
		ExpiresAt:          expiresAt,
	}
	token, err := EncodeInvite(payload)
	if err != nil {
		return InviteResult{}, err
	}
	m.RecordAudit(AuditEntry{
		ActorNodeID:  m.InstallationID,
		ActorUserID:  createdByUserID,
		TargetNodeID: m.InstallationID,
		ResourceType: "invite",
		ResourceID:   inviteID,
		Operation:    "invite.create",
		Result:       "success",
		Detail:       fmt.Sprintf("expiresAt=%d", expiresAt),
	})
	return InviteResult{
		Invite:             token,
		InviteID:           inviteID,
		ExpiresAt:          expiresAt,
		PrimaryURL:         primaryURL,
		PrimaryFingerprint: id.Fingerprint,
	}, nil
}

// ServeWS handles the dedicated WebSocket endpoint on Y's public domain and runs
// mutual TLS 1.3 authentication inside the channel.
func (m *Manager) ServeWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Reject browser-originated WebSocket connections; only Core daemons connect here.
	if r.Header.Get("Origin") != "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if m.Role() != RolePrimary {
		http.NotFound(w, r)
		return
	}
	id, err := m.ensureIdentity(false)
	if err != nil {
		http.Error(w, "cluster identity unavailable", http.StatusServiceUnavailable)
		return
	}
	select {
	case m.connGate <- struct{}{}:
		defer func() { <-m.connGate }()
	default:
		http.Error(w, "cluster connection limit reached", http.StatusTooManyRequests)
		return
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:  8192,
		WriteBufferSize: 8192,
		CheckOrigin:     func(r *http.Request) bool { return r.Header.Get("Origin") == "" },
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	rawConn := newWSNetConn(ws)
	tlsConn := tls.Server(rawConn, serverTLSConfig(id))
	defer tlsConn.Close()

	_ = tlsConn.SetDeadline(time.Now().Add(15 * time.Second))
	if err := tlsConn.HandshakeContext(r.Context()); err != nil {
		return
	}
	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return
	}
	peerCert, peerPub, peerFP, err := verifyPeerSelfSignedCert(state.PeerCertificates[0].Raw)
	if err != nil {
		return
	}
	peerNodeID := peerCert.Subject.CommonName
	if peerNodeID == "" || peerNodeID == m.InstallationID {
		return
	}

	// Read Hello frame with strict deadline.
	_ = tlsConn.SetReadDeadline(time.Now().Add(15 * time.Second))
	env, err := readFrame(tlsConn)
	if err != nil || env.Type != MsgHello {
		return
	}
	var hello HelloMsg
	if json.Unmarshal(env.Payload, &hello) != nil || hello.NodeID != peerNodeID {
		return
	}

	peer := newPeerConn(peerNodeID, peerFP, peerPub, tlsConn, false)
	defer peer.close()

	// Permanently reject revoked public keys.
	if revoked, _ := m.isKeyRevoked(peerFP); revoked {
		_ = peer.send(MsgHelloAck, "", env.ID, HelloAckMsg{
			OK:        false,
			Error:     "chave revogada; faça um novo pareamento",
			ErrorCode: "revoked",
		})
		return
	}

	switch hello.Mode {
	case "pair":
		m.servePairingConn(r.Context(), peer, id, env.ID)
	case "session":
		m.serveApprovedSession(r.Context(), peer, env.ID)
	default:
		_ = peer.send(MsgHelloAck, "", env.ID, HelloAckMsg{OK: false, Error: "modo de conexão inválido"})
	}
}

func (m *Manager) servePairingConn(ctx context.Context, peer *peerConn, primaryID *Identity, helloReqID string) {
	select {
	case m.pairGate <- struct{}{}:
		defer func() { <-m.pairGate }()
	default:
		_ = peer.send(MsgHelloAck, "", helloReqID, HelloAckMsg{OK: false, Error: "muitas solicitações de pareamento simultâneas"})
		return
	}

	localState := m.State()
	primaryURL := ""
	if m.Provider != nil {
		primaryURL = m.Provider.LocalPrimaryURL()
	}
	if err := peer.send(MsgHelloAck, "", helloReqID, HelloAckMsg{
		OK:          true,
		PrimaryID:   m.InstallationID,
		PrimaryName: localState.DisplayName,
		PrimaryURL:  primaryURL,
		HeartbeatMs: m.HeartbeatInterval.Milliseconds(),
	}); err != nil {
		return
	}

	// Expect pair_request strictly; reject any inventory, auth, or command before approval!
	_ = peer.conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	reqEnv, err := readFrame(peer.conn)
	if err != nil || reqEnv.Type != MsgPairRequest {
		return
	}
	var req PairRequestMsg
	if json.Unmarshal(reqEnv.Payload, &req) != nil {
		return
	}
	if req.NodeID != peer.nodeID {
		_ = peer.send(MsgPairResponse, "", reqEnv.ID, PairResponseMsg{Accepted: false, Error: "identificador da instalação divergente"})
		return
	}
	_, reqFP, err := parsePublicKeyPEM(req.PublicKeyPEM)
	if err != nil || !constantTimeEqual(reqFP, peer.fingerprint) {
		_ = peer.send(MsgPairResponse, "", reqEnv.ID, PairResponseMsg{Accepted: false, Error: "chave pública não corresponde ao certificado mTLS"})
		return
	}

	code, expiresAt, errCode, err := m.validateAndBindInvite(req, peer.fingerprint, m.InstallationID, primaryID.Fingerprint)
	if err != nil {
		_ = peer.send(MsgPairResponse, "", reqEnv.ID, PairResponseMsg{Accepted: false, Error: err.Error(), ErrorCode: errCode})
		return
	}

	// Register pending peer so approval/rejection notifies X immediately.
	m.mu.Lock()
	if old, ok := m.pendingPeers[peer.nodeID]; ok && old != peer {
		old.close()
	}
	m.pendingPeers[peer.nodeID] = peer
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		if m.pendingPeers[peer.nodeID] == peer {
			delete(m.pendingPeers, peer.nodeID)
		}
		m.mu.Unlock()
	}()

	if err := peer.send(MsgPairResponse, "", reqEnv.ID, PairResponseMsg{
		Accepted:         true,
		Status:           NodeStatusPending,
		VerificationCode: code,
		PrimaryID:        m.InstallationID,
		PrimaryName:      localState.DisplayName,
		PrimaryURL:       primaryURL,
		ExpiresAt:        expiresAt,
	}); err != nil {
		return
	}

	// While pending, keep reading only to detect disconnect or illegal early frames (inventory/auth/cmd rejected!).
	readErr := make(chan error, 1)
	go func() {
		for {
			_ = peer.conn.SetReadDeadline(time.Now().Add(m.HeartbeatTimeout))
			frame, err := readFrame(peer.conn)
			if err != nil {
				readErr <- err
				return
			}
			// Only heartbeat is allowed while awaiting approval.
			if frame.Type == MsgHeartbeat {
				var hb HeartbeatMsg
				_ = json.Unmarshal(frame.Payload, &hb)
				_ = peer.send(MsgHeartbeatAck, "", frame.ID, HeartbeatAckMsg{SentAt: hb.SentAt, ServerTime: time.Now().Unix()})
				continue
			}
			// Reject any premature inventory, login, or command before approval!
			readErr <- errors.New("operação não permitida antes da aprovação")
			return
		}
	}()

	waitTimer := time.NewTimer(time.Until(time.Unix(expiresAt, 0).Add(5 * time.Second)))
	defer waitTimer.Stop()

	select {
	case approved := <-peer.approveCh:
		_ = peer.send(MsgPairApproved, "", "", approved)
		// Give client a moment to receive approval before closing so it transitions cleanly into an approved mTLS session.
		time.Sleep(150 * time.Millisecond)
		return
	case rejected := <-peer.rejectCh:
		_ = peer.send(MsgPairRejected, "", "", rejected)
		time.Sleep(100 * time.Millisecond)
		return
	case <-readErr:
		return
	case <-waitTimer.C:
		return
	case <-ctx.Done():
		return
	}
}

func (m *Manager) serveApprovedSession(ctx context.Context, peer *peerConn, helloReqID string) {
	node, err := m.getNode(peer.nodeID)
	if err != nil || node.Status != NodeStatusApproved || !constantTimeEqual(node.PublicKeyFP, peer.fingerprint) {
		_ = peer.send(MsgHelloAck, "", helloReqID, HelloAckMsg{
			OK:        false,
			Error:     "computador não aprovado ou chave pública divergente",
			ErrorCode: "not_approved",
		})
		return
	}
	peer.approved = true
	localState := m.State()
	primaryURL := ""
	if m.Provider != nil {
		primaryURL = m.Provider.LocalPrimaryURL()
	}

	m.mu.Lock()
	if prev, ok := m.peers[peer.nodeID]; ok && prev != peer {
		prev.close()
	}
	m.peers[peer.nodeID] = peer
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		if m.peers[peer.nodeID] == peer {
			delete(m.peers, peer.nodeID)
		}
		m.mu.Unlock()
	}()

	if err := peer.send(MsgHelloAck, "", helloReqID, HelloAckMsg{
		OK:          true,
		PrimaryID:   m.InstallationID,
		PrimaryName: localState.DisplayName,
		PrimaryURL:  primaryURL,
		AccessSlug:  node.AccessSlug,
		HeartbeatMs: m.HeartbeatInterval.Milliseconds(),
	}); err != nil {
		return
	}

	// Touch last_seen_at immediately on authenticated mTLS connect.
	peer.touchHeartbeat()
	m.updateNodeHeartbeat(peer.nodeID, HeartbeatMsg{
		SentAt: time.Now().Unix(),
		Tunnel: TunnelHealthReport{Configured: node.TunnelConfigured, Healthy: node.TunnelHealthy, Status: node.TunnelStatus},
	})

	for {
		select {
		case <-ctx.Done():
			return
		case <-peer.closed:
			return
		default:
		}
		_ = peer.conn.SetReadDeadline(time.Now().Add(m.HeartbeatTimeout))
		env, err := readFrame(peer.conn)
		if err != nil {
			return
		}
		peer.touchHeartbeat()

		if env.ReplyTo != "" {
			peer.deliverReply(env)
			continue
		}

		switch env.Type {
		case MsgHeartbeat:
			var hb HeartbeatMsg
			if json.Unmarshal(env.Payload, &hb) != nil {
				return
			}
			m.updateNodeHeartbeat(peer.nodeID, hb)
			m.reconcileRemoteUserEpochs(peer.nodeID, hb.Users)
			curPrimaryURL := ""
			if m.Provider != nil {
				curPrimaryURL = m.Provider.LocalPrimaryURL()
			}
			if err := peer.send(MsgHeartbeatAck, "", env.ID, HeartbeatAckMsg{
				SentAt:     hb.SentAt,
				ServerTime: time.Now().Unix(),
				PrimaryURL: curPrimaryURL,
			}); err != nil {
				return
			}
		case MsgInventorySync:
			var inv InventorySyncMsg
			if json.Unmarshal(env.Payload, &inv) != nil {
				return
			}
			if err := m.saveNodeInventory(peer.nodeID, inv); err != nil {
				continue
			}
			m.reconcileRemoteUserEpochs(peer.nodeID, inv.Users)
			freshNode, _ := m.getNode(peer.nodeID)
			curPrimaryURL := ""
			if m.Provider != nil {
				curPrimaryURL = m.Provider.LocalPrimaryURL()
			}
			if err := peer.send(MsgInventoryAck, "", env.ID, InventoryAckMsg{
				Seq:        inv.Seq,
				Confirmed:  true,
				PrimaryURL: curPrimaryURL,
				AccessSlug: freshNode.AccessSlug,
			}); err != nil {
				return
			}
		case MsgAuthRevoked:
			var rev AuthRevokedMsg
			if json.Unmarshal(env.Payload, &rev) == nil && rev.UserID != "" && rev.AuthEpoch > 0 {
				if m.Auth != nil {
					_ = m.Auth.RevokeBelowEpoch(peer.nodeID, rev.UserID, rev.AuthEpoch)
				}
				if cur, err := m.getNode(peer.nodeID); err == nil {
					found := false
					updated := false
					for i := range cur.RemoteUsers {
						if cur.RemoteUsers[i].UserID == rev.UserID {
							found = true
							if cur.RemoteUsers[i].AuthEpoch < rev.AuthEpoch {
								cur.RemoteUsers[i].AuthEpoch = rev.AuthEpoch
								updated = true
							}
							if rev.Username != "" && cur.RemoteUsers[i].Username != rev.Username {
								cur.RemoteUsers[i].Username = rev.Username
								updated = true
							}
						}
					}
					if !found && rev.Username != "" {
						cur.RemoteUsers = append(cur.RemoteUsers, RemoteUserSummary{
							UserID:    rev.UserID,
							Username:  rev.Username,
							AuthEpoch: rev.AuthEpoch,
						})
						updated = true
					}
					if updated {
						raw, _ := json.Marshal(cur.RemoteUsers)
						_, _ = m.db.Exec("UPDATE cluster_nodes SET remote_users_json=? WHERE id=?", string(raw), peer.nodeID)
					}
				}
				if rev.Username != "" {
					_, _ = m.db.Exec(
						"UPDATE cluster_principal_grants SET username=?,updated_at=? WHERE issuer_node_id=? AND user_id=?",
						rev.Username, time.Now().Unix(), peer.nodeID, rev.UserID,
					)
				}
				m.RecordAudit(AuditEntry{
					ActorNodeID:  peer.nodeID,
					ActorUserID:  rev.UserID,
					TargetNodeID: m.InstallationID,
					ResourceType: "session",
					ResourceID:   peer.nodeID + ":" + rev.UserID,
					Operation:    "auth.revoked",
					Result:       "success",
					Detail:       fmt.Sprintf("epoch=%d reason=%s", rev.AuthEpoch, rev.Reason),
				})
			}
		default:
			// Unknown or disallowed message type from secondary closes the channel immediately.
			return
		}
	}
}

func (m *Manager) reconcileRemoteUserEpochs(nodeID string, users []RemoteUserSummary) {
	if m.Auth == nil || len(users) == 0 {
		return
	}
	for _, u := range users {
		if u.UserID != "" && u.AuthEpoch > 0 {
			_ = m.Auth.RevokeBelowEpoch(nodeID, u.UserID, u.AuthEpoch)
		}
	}
}

// NodeOnline returns true strictly when an authenticated mTLS peer is connected
// AND has sent a heartbeat within HeartbeatTimeout.
func (m *Manager) NodeOnline(nodeID string) bool {
	if nodeID == "" || nodeID == m.InstallationID {
		return true
	}
	m.mu.RLock()
	peer := m.peers[nodeID]
	m.mu.RUnlock()
	if peer == nil || !peer.approved {
		return false
	}
	select {
	case <-peer.closed:
		return false
	default:
	}
	return peer.heartbeatAge() <= m.HeartbeatTimeout
}

func (m *Manager) livePeer(nodeID string) (*peerConn, error) {
	m.mu.RLock()
	peer := m.peers[nodeID]
	m.mu.RUnlock()
	if peer == nil || !peer.approved || peer.heartbeatAge() > m.HeartbeatTimeout {
		return nil, errors.New("computador offline")
	}
	select {
	case <-peer.closed:
		return nil, errors.New("computador offline")
	default:
	}
	return peer, nil
}

// ApproveNode atomically approves a pending computer and consumes its invite.
func (m *Manager) ApproveNode(nodeID string, actor Actor) (NodeRecord, error) {
	node, err := m.approveNodeTx(nodeID, actor)
	if err != nil {
		return NodeRecord{}, err
	}
	localState := m.State()
	primaryURL := ""
	if m.Provider != nil {
		primaryURL = m.Provider.LocalPrimaryURL()
	}
	m.mu.RLock()
	pending := m.pendingPeers[nodeID]
	m.mu.RUnlock()
	if pending != nil {
		select {
		case pending.approveCh <- PairApprovedMsg{
			NodeID:      node.ID,
			PrimaryID:   m.InstallationID,
			PrimaryName: localState.DisplayName,
			PrimaryURL:  primaryURL,
			AccessSlug:  node.AccessSlug,
			HeartbeatMs: m.HeartbeatInterval.Milliseconds(),
		}:
		default:
		}
	}
	return node, nil
}

// RejectNode rejects a pending pairing request and invalidates its invite.
func (m *Manager) RejectNode(nodeID string, actor Actor) error {
	_, err := m.rejectNodeTx(nodeID, actor)
	if err != nil {
		return err
	}
	m.mu.RLock()
	pending := m.pendingPeers[nodeID]
	m.mu.RUnlock()
	if pending != nil {
		select {
		case pending.rejectCh <- PairRejectedMsg{Reason: "solicitação rejeitada pelo administrador"}:
		default:
		}
	}
	return nil
}

type NodeUpdateInput struct {
	DisplayName  *string  `json:"displayName,omitempty"`
	AccessSlug   *string  `json:"accessSlug,omitempty"`
	LoginEnabled *bool    `json:"loginEnabled,omitempty"`
	DelegatedOps []string `json:"delegatedOps,omitempty"`
}

func (m *Manager) UpdateNode(nodeID string, in NodeUpdateInput, actor Actor) (NodeRecord, error) {
	node, err := m.getNode(nodeID)
	if err != nil || node.Status != NodeStatusApproved {
		return NodeRecord{}, errors.New("computador não encontrado")
	}
	if in.DisplayName != nil {
		clean, err := sanitizeDisplayName(*in.DisplayName)
		if err != nil {
			return NodeRecord{}, err
		}
		node.DisplayName = clean
	}
	if in.AccessSlug != nil {
		cleanSlug, err := validAccessSlug(*in.AccessSlug)
		if err != nil {
			return NodeRecord{}, err
		}
		var count int
		if err := m.db.QueryRow("SELECT count(*) FROM cluster_nodes WHERE access_slug=? AND id!=?", cleanSlug, nodeID).Scan(&count); err != nil {
			return NodeRecord{}, err
		}
		if count > 0 {
			return NodeRecord{}, errors.New("esse nome curto de acesso já está em uso por outro computador")
		}
		node.AccessSlug = cleanSlug
	}
	loginRevoked := false
	if in.LoginEnabled != nil {
		if node.LoginEnabled && !*in.LoginEnabled {
			loginRevoked = true
		}
		node.LoginEnabled = *in.LoginEnabled
	}
	if in.DelegatedOps != nil {
		for _, op := range in.DelegatedOps {
			if op != "projects:*" && op != "databases:*" && !IsAllowedCommandKind(op) {
				return NodeRecord{}, errors.New("operação delegada inválida: " + op)
			}
		}
		node.DelegatedOps = in.DelegatedOps
	}
	opsRaw, _ := json.Marshal(node.DelegatedOps)
	loginInt := 0
	if node.LoginEnabled {
		loginInt = 1
	}
	if _, err := m.db.Exec(
		"UPDATE cluster_nodes SET display_name=?,access_slug=?,login_enabled=?,delegated_ops=? WHERE id=?",
		node.DisplayName, node.AccessSlug, loginInt, string(opsRaw), nodeID,
	); err != nil {
		return NodeRecord{}, err
	}
	if loginRevoked && m.Auth != nil {
		_ = m.Auth.RevokeIssuer(nodeID)
	}
	m.RecordAudit(AuditEntry{
		ActorNodeID:   actor.IssuerNodeID,
		ActorUserID:   actor.UserID,
		ActorUsername: actor.Username,
		TargetNodeID:  nodeID,
		ResourceType:  "computer",
		ResourceID:    nodeID,
		Operation:     "node.update",
		Result:        "success",
		Detail:        fmt.Sprintf("name=%s slug=%s login=%v", node.DisplayName, node.AccessSlug, node.LoginEnabled),
	})
	return m.getNode(nodeID)
}

// RemoveNode permanently revokes the device identity, closes any active connection,
// invalidates dependent sessions, removes active inventory/node rows, and preserves audit + revocation.
// Works identically whether the computer is online or offline.
func (m *Manager) RemoveNode(nodeID string, actor Actor) error {
	_, err := m.removeNodeTx(nodeID, "removed_by_admin", actor)
	if err != nil {
		return err
	}
	if m.Auth != nil {
		_ = m.Auth.RevokeIssuer(nodeID)
	}
	m.mu.Lock()
	peer := m.peers[nodeID]
	delete(m.peers, nodeID)
	pending := m.pendingPeers[nodeID]
	delete(m.pendingPeers, nodeID)
	m.mu.Unlock()

	if peer != nil {
		_ = peer.send(MsgNodeRevoked, auth.ID(), "", NodeRevokedMsg{Reason: "removido pelo administrador do painel principal"})
		time.Sleep(50 * time.Millisecond)
		peer.close()
	}
	if pending != nil {
		_ = pending.send(MsgNodeRevoked, auth.ID(), "", NodeRevokedMsg{Reason: "removido pelo administrador"})
		pending.close()
	}
	return nil
}

func hasRemoteUser(users []RemoteUserSummary, username string) bool {
	for _, u := range users {
		if u.Username == username {
			return true
		}
	}
	return false
}

func (m *Manager) ensurePrincipalGrant(nodeID, userID, username string) {
	if nodeID == "" || userID == "" {
		return
	}
	now := time.Now().Unix()
	_, _ = m.db.Exec(
		`INSERT INTO cluster_principal_grants(issuer_node_id,user_id,username,scope,granted_by,granted_at,updated_at)
		 VALUES(?,?,?,'origin_only','',0,?)
		 ON CONFLICT(issuer_node_id,user_id) DO UPDATE SET username=excluded.username,updated_at=excluded.updated_at`,
		nodeID, userID, username, now,
	)
}

// AuthenticateFederated forwards login credentials exclusively to the selected
// online, approved, login-enabled secondary node over mTLS.
func (m *Manager) AuthenticateFederated(ctx context.Context, slug, username, password, clientIP string) (node NodeRecord, resp AuthVerifyResp, available bool, err error) {
	node, err = m.getApprovedNodeBySlug(slug)
	if err != nil || !node.LoginEnabled {
		return NodeRecord{}, AuthVerifyResp{}, false, errors.New("computador de origem indisponível ou não autorizado para login")
	}
	peer, err := m.livePeer(node.ID)
	if err != nil {
		return node, AuthVerifyResp{}, false, errors.New("computador de origem indisponível no momento")
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	env, err := peer.call(callCtx, MsgAuthVerify, AuthVerifyReq{
		Username: username,
		Password: password,
		ClientIP: clientIP,
	})
	if err != nil {
		return node, AuthVerifyResp{}, false, errors.New("não foi possível contatar o computador de origem")
	}
	if env.Type != MsgAuthVerifyResult || json.Unmarshal(env.Payload, &resp) != nil {
		return node, AuthVerifyResp{}, false, errors.New("resposta inválida do computador de origem")
	}
	if resp.OK && resp.UserID != "" {
		m.ensurePrincipalGrant(node.ID, resp.UserID, resp.Username)
	}
	return node, resp, true, nil
}

// AuthenticateFederatedByCredentials resolves a federated login using only
// username and password across approved, login-enabled secondary computers.
// It checks computers whose synced user list matches username first, followed
// by any online computer whose user list has not synced yet.
func (m *Manager) AuthenticateFederatedByCredentials(ctx context.Context, username, password, clientIP string) (node NodeRecord, resp AuthVerifyResp, matchedOffline bool, err error) {
	cleanUser := strings.TrimSpace(username)
	if cleanUser == "" {
		return NodeRecord{}, AuthVerifyResp{
			OK:         false,
			StatusCode: http.StatusUnauthorized,
			Error:      "usuário ou senha inválidos",
		}, false, nil
	}
	nodes, err := m.listNodes(NodeStatusApproved)
	if err != nil {
		return NodeRecord{}, AuthVerifyResp{}, false, err
	}

	var exactMatches []NodeRecord
	var unsyncedNodes []NodeRecord
	for _, n := range nodes {
		if !n.LoginEnabled {
			continue
		}
		if hasRemoteUser(n.RemoteUsers, cleanUser) {
			exactMatches = append(exactMatches, n)
		} else if len(n.RemoteUsers) == 0 {
			unsyncedNodes = append(unsyncedNodes, n)
		}
	}

	candidates := append(exactMatches, unsyncedNodes...)
	rateLimited := false
	for _, candidate := range candidates {
		isExact := hasRemoteUser(candidate.RemoteUsers, cleanUser)
		peer, peerErr := m.livePeer(candidate.ID)
		if peerErr != nil {
			if isExact {
				matchedOffline = true
			}
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		env, callErr := peer.call(callCtx, MsgAuthVerify, AuthVerifyReq{
			Username: cleanUser,
			Password: password,
			ClientIP: clientIP,
		})
		cancel()
		if callErr != nil {
			if isExact {
				matchedOffline = true
			}
			continue
		}
		var verifyResp AuthVerifyResp
		if env.Type != MsgAuthVerifyResult || json.Unmarshal(env.Payload, &verifyResp) != nil {
			if isExact {
				matchedOffline = true
			}
			continue
		}
		if verifyResp.OK && verifyResp.UserID != "" {
			m.ensurePrincipalGrant(candidate.ID, verifyResp.UserID, verifyResp.Username)
			return candidate, verifyResp, false, nil
		}
		if verifyResp.StatusCode == http.StatusTooManyRequests {
			rateLimited = true
		}
	}

	if rateLimited {
		return NodeRecord{}, AuthVerifyResp{
			OK:         false,
			StatusCode: http.StatusTooManyRequests,
			Error:      "tentativas demais; aguarde 15 minutos",
		}, false, nil
	}
	return NodeRecord{}, AuthVerifyResp{
		OK:         false,
		StatusCode: http.StatusUnauthorized,
		Error:      "usuário ou senha inválidos",
	}, matchedOffline, nil
}

// ValidateSession verifies local or federated sessions, enforcing node approval,
// login enablement, origin auth_epoch propagation, and bounded offline validation grace.
func (m *Manager) ValidateSession(ctx context.Context, session auth.Session) (SessionAccess, error) {
	localState := m.State()
	if session.IssuerNodeID == "" || session.IssuerNodeID == m.InstallationID {
		return SessionAccess{
			Session:            session,
			IssuerNodeID:       m.InstallationID,
			IssuerNodeName:     localState.DisplayName,
			Scope:              ScopeUnified,
			CanManageComputers: true,
		}, nil
	}

	node, err := m.getNode(session.IssuerNodeID)
	if err != nil || node.Status != NodeStatusApproved || !node.LoginEnabled {
		if m.Auth != nil {
			_ = m.Auth.Revoke(session.ID)
		}
		return SessionAccess{}, errors.New("vínculo ou permissão de login revogados; entre novamente")
	}
	// Check cached user auth_epoch from recent heartbeat/inventory.
	for _, u := range node.RemoteUsers {
		if u.UserID == session.UserID && u.AuthEpoch > session.AuthEpoch {
			if m.Auth != nil {
				_ = m.Auth.Revoke(session.ID)
			}
			return SessionAccess{}, errors.New("credenciais da conta de origem foram alteradas; entre novamente")
		}
	}

	now := time.Now().Unix()
	if peer, err := m.livePeer(node.ID); err == nil {
		if now-session.LastOriginCheck >= int64(m.OriginCheckInterval.Seconds()) {
			checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			env, callErr := peer.call(checkCtx, MsgAuthStatus, AuthStatusReq{UserID: session.UserID})
			cancel()
			if callErr == nil && env.Type == MsgAuthStatusResult {
				var st AuthStatusResp
				if json.Unmarshal(env.Payload, &st) == nil {
					if !st.Exists || st.AuthEpoch > session.AuthEpoch {
						if m.Auth != nil {
							_ = m.Auth.Revoke(session.ID)
						}
						return SessionAccess{}, errors.New("conta de origem revogada ou senha alterada; entre novamente")
					}
					if m.Auth != nil {
						_ = m.Auth.TouchOriginCheck(session.ID, now)
					}
					session.LastOriginCheck = now
				}
			}
		}
	} else {
		// Origin computer is offline: enforce bounded offline unverified grace window!
		maxAge := int64(m.MaxOriginUnverifiedWindow.Seconds())
		if maxAge < 1 {
			maxAge = 1
		}
		if session.LastOriginCheck > 0 && now-session.LastOriginCheck > maxAge {
			if m.Auth != nil {
				_ = m.Auth.Revoke(session.ID)
			}
			return SessionAccess{}, errors.New("prazo de validação da conta de origem expirou enquanto o computador está offline; entre novamente quando ele reconectar")
		}
	}

	grant, err := m.GetPrincipalGrant(session.IssuerNodeID, session.UserID)
	if err != nil {
		return SessionAccess{}, err
	}
	scope := grant.Scope
	if scope != ScopeUnified {
		scope = ScopeOriginOnly
	}
	return SessionAccess{
		Session:            session,
		IssuerNodeID:       node.ID,
		IssuerNodeName:     node.DisplayName,
		Scope:              scope,
		CanManageComputers: scope == ScopeUnified,
	}, nil
}

func (m *Manager) CanAccessNode(access SessionAccess, targetNodeID string) bool {
	if targetNodeID == "" {
		targetNodeID = m.InstallationID
	}
	if access.Scope == ScopeUnified {
		if targetNodeID == m.InstallationID {
			return true
		}
		node, err := m.getNode(targetNodeID)
		return err == nil && node.Status == NodeStatusApproved
	}
	return targetNodeID == access.IssuerNodeID
}

// IssueStepUpGrant revalidates the caller's credentials at their account's origin
// computer and issues a short-lived, single-use, target/resource/action-bound grant.
func (m *Manager) IssueStepUpGrant(ctx context.Context, access SessionAccess, targetNodeID, resourceID, action, username, password, clientIP string) (token string, grant StepUpGrant, statusCode int, err error) {
	if !IsValidStepUpAction(action) {
		return "", StepUpGrant{}, http.StatusBadRequest, errors.New("ação sensível inválida")
	}
	if targetNodeID == "" {
		targetNodeID = m.InstallationID
	}
	if resourceID == "" {
		return "", StepUpGrant{}, http.StatusBadRequest, errors.New("recurso inválido")
	}
	if !m.CanAccessNode(access, targetNodeID) {
		return "", StepUpGrant{}, http.StatusForbidden, errors.New("acesso negado ao computador de destino")
	}

	// Revalidate credentials at the account's origin computer!
	if access.IssuerNodeID == "" || access.IssuerNodeID == m.InstallationID {
		if m.Provider == nil {
			return "", StepUpGrant{}, http.StatusServiceUnavailable, errors.New("serviço de autenticação indisponível")
		}
		uid, _, _, code, authErr := m.Provider.AuthenticateLocal(username, password, clientIP)
		if authErr != nil {
			return "", StepUpGrant{}, code, authErr
		}
		if uid != access.Session.UserID {
			return "", StepUpGrant{}, http.StatusBadRequest, errors.New("usuário ou senha inválidos")
		}
	} else {
		peer, peerErr := m.livePeer(access.IssuerNodeID)
		if peerErr != nil {
			return "", StepUpGrant{}, http.StatusServiceUnavailable, errors.New("computador de origem da conta indisponível para revalidação")
		}
		callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		env, callErr := peer.call(callCtx, MsgAuthReauth, AuthReauthReq{
			UserID:       access.Session.UserID,
			Username:     username,
			Password:     password,
			TargetNodeID: targetNodeID,
			ResourceID:   resourceID,
			Action:       action,
			ClientIP:     clientIP,
		})
		cancel()
		if callErr != nil {
			return "", StepUpGrant{}, http.StatusServiceUnavailable, errors.New("não foi possível revalidar a identidade no computador de origem")
		}
		var resp AuthReauthResp
		if env.Type != MsgAuthReauthResult || json.Unmarshal(env.Payload, &resp) != nil {
			return "", StepUpGrant{}, http.StatusBadGateway, errors.New("resposta inválida na revalidação")
		}
		if !resp.OK {
			code := resp.StatusCode
			if code == 0 {
				code = http.StatusBadRequest
			}
			msg := resp.Error
			if msg == "" {
				msg = "usuário ou senha inválidos"
			}
			return "", StepUpGrant{}, code, errors.New(msg)
		}
	}

	id, err := m.ensureIdentity(false)
	if err != nil {
		return "", StepUpGrant{}, http.StatusInternalServerError, err
	}
	now := time.Now()
	grant = StepUpGrant{
		GrantID:      auth.ID(),
		IssuerNodeID: access.IssuerNodeID,
		UserID:       access.Session.UserID,
		TargetNodeID: targetNodeID,
		ResourceID:   resourceID,
		Action:       action,
		IssuedAt:     now.Unix(),
		ExpiresAt:    now.Add(m.StepUpTTL).Unix(),
	}
	if err := SignStepUpGrant(id.PrivateKey, &grant); err != nil {
		return "", StepUpGrant{}, http.StatusInternalServerError, err
	}
	rawToken, err := randomSecret()
	if err != nil {
		return "", StepUpGrant{}, http.StatusInternalServerError, err
	}
	hash := secretDigest(rawToken)
	m.stepUpMu.Lock()
	for k, v := range m.stepUpGrants {
		if now.After(v.expiresAt) {
			delete(m.stepUpGrants, k)
		}
	}
	m.stepUpGrants[hash] = storedStepUp{grant: grant, expiresAt: now.Add(m.StepUpTTL)}
	m.stepUpMu.Unlock()

	m.RecordAudit(AuditEntry{
		ActorNodeID:   access.IssuerNodeID,
		ActorUserID:   access.Session.UserID,
		ActorUsername: access.Session.Username,
		TargetNodeID:  targetNodeID,
		ResourceType:  "database",
		ResourceID:    resourceID,
		Operation:     "stepup." + action,
		Result:        "success",
	})
	return rawToken, grant, http.StatusOK, nil
}

// ConsumeStepUpGrant atomically consumes a single-use step-up token bound strictly
// to the caller, target node, resource, and action.
func (m *Manager) ConsumeStepUpGrant(rawToken string, access SessionAccess, targetNodeID, resourceID, action string) (StepUpGrant, error) {
	if rawToken == "" {
		return StepUpGrant{}, errors.New("reconfirmação de identidade obrigatória para esta ação sensível")
	}
	if targetNodeID == "" {
		targetNodeID = m.InstallationID
	}
	hash := secretDigest(rawToken)
	now := time.Now()
	m.stepUpMu.Lock()
	stored, ok := m.stepUpGrants[hash]
	if ok {
		delete(m.stepUpGrants, hash) // Single-use: consume atomically!
	}
	m.stepUpMu.Unlock()
	if !ok || now.After(stored.expiresAt) {
		return StepUpGrant{}, errors.New("autorização sensível expirada ou já utilizada; reconfirme sua senha")
	}
	g := stored.grant
	if g.IssuerNodeID != access.IssuerNodeID || g.UserID != access.Session.UserID ||
		g.TargetNodeID != targetNodeID || g.ResourceID != resourceID || g.Action != action {
		return StepUpGrant{}, errors.New("autorização sensível não corresponde ao usuário, computador, instância ou ação")
	}
	return g, nil
}

// DispatchCommand routes an explicit typed operation strictly to targetNodeID,
// enforcing scope, node delegation, idempotency, and audit logging.
func (m *Manager) DispatchCommand(ctx context.Context, access SessionAccess, targetNodeID string, cmd ExecCommandReq) (json.RawMessage, int, error) {
	if !IsAllowedCommandKind(cmd.Kind) {
		return nil, http.StatusBadRequest, errors.New("operação não suportada")
	}
	if targetNodeID == "" || targetNodeID == m.InstallationID {
		return nil, http.StatusBadRequest, errors.New("destino remoto inválido")
	}
	if !m.CanAccessNode(access, targetNodeID) {
		return nil, http.StatusForbidden, errors.New("sem permissão para operar recursos deste computador")
	}
	node, err := m.getNode(targetNodeID)
	if err != nil || node.Status != NodeStatusApproved {
		return nil, http.StatusNotFound, errors.New("computador não encontrado")
	}
	if !OperationDelegated(node.DelegatedOps, cmd.Kind) {
		return nil, http.StatusForbidden, errors.New("este computador não delegou esta operação")
	}
	if cmd.OperationID == "" {
		cmd.OperationID = auth.ID()
	}
	cmd.Actor = Actor{
		IssuerNodeID: access.IssuerNodeID,
		UserID:       access.Session.UserID,
		Username:     access.Session.Username,
	}

	isMutating := cmd.Kind == CmdProjectAction || cmd.Kind == CmdDatabaseCreate || cmd.Kind == CmdDatabaseChange || cmd.Kind == CmdDatabaseReveal
	if isMutating {
		existing, inserted, opErr := m.tryBeginRemoteOp(RemoteOpRecord{
			OperationID:  cmd.OperationID,
			Direction:    "outbound",
			TargetNodeID: targetNodeID,
			ResourceType: commandResourceType(cmd.Kind),
			ResourceID:   cmd.ResourceID,
			Action:       cmd.Kind,
			Status:       "in_progress",
		})
		if opErr != nil {
			return nil, http.StatusInternalServerError, opErr
		}
		if !inserted {
			switch existing.Status {
			case "completed":
				return json.RawMessage(existing.ResultJSON), http.StatusOK, nil
			case "failed":
				return nil, http.StatusConflict, errors.New(existing.ErrorText)
			default:
				return nil, http.StatusConflict, errors.New("operação já enviada com resultado desconhecido ou em andamento; verifique o estado antes de repetir")
			}
		}
	}

	peer, err := m.livePeer(targetNodeID)
	if err != nil {
		if isMutating {
			m.finishRemoteOp(cmd.OperationID, "failed", "", "computador offline")
		}
		m.RecordAudit(AuditEntry{
			ActorNodeID:   cmd.Actor.IssuerNodeID,
			ActorUserID:   cmd.Actor.UserID,
			ActorUsername: cmd.Actor.Username,
			TargetNodeID:  targetNodeID,
			ResourceType:  commandResourceType(cmd.Kind),
			ResourceID:    cmd.ResourceID,
			Operation:     cmd.Kind,
			OperationID:   cmd.OperationID,
			Result:        "blocked_offline",
		})
		return nil, http.StatusServiceUnavailable, errors.New("computador offline; ações remotas ficam bloqueadas até a reconexão")
	}

	callCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	env, err := peer.call(callCtx, MsgExecCommand, cmd)
	if err != nil {
		if isMutating {
			// Mark outcome UNKNOWN so reconnect or retry with the same operationId NEVER repeats restart/delete!
			m.finishRemoteOp(cmd.OperationID, "unknown", "", err.Error())
		}
		m.RecordAudit(AuditEntry{
			ActorNodeID:   cmd.Actor.IssuerNodeID,
			ActorUserID:   cmd.Actor.UserID,
			ActorUsername: cmd.Actor.Username,
			TargetNodeID:  targetNodeID,
			ResourceType:  commandResourceType(cmd.Kind),
			ResourceID:    cmd.ResourceID,
			Operation:     cmd.Kind,
			OperationID:   cmd.OperationID,
			Result:        "unknown",
			Detail:        err.Error(),
		})
		return nil, http.StatusGatewayTimeout, errors.New("conexão perdida durante a operação; resultado desconhecido — não será repetida automaticamente")
	}
	var resp ExecCommandResp
	if env.Type != MsgExecResult || json.Unmarshal(env.Payload, &resp) != nil {
		if isMutating {
			m.finishRemoteOp(cmd.OperationID, "unknown", "", "resposta inválida")
		}
		return nil, http.StatusBadGateway, errors.New("resposta inválida do computador remoto")
	}
	if !resp.OK {
		code := resp.StatusCode
		if code == 0 {
			code = http.StatusBadRequest
		}
		if isMutating {
			m.finishRemoteOp(cmd.OperationID, "failed", "", resp.Error)
		}
		m.RecordAudit(AuditEntry{
			ActorNodeID:   cmd.Actor.IssuerNodeID,
			ActorUserID:   cmd.Actor.UserID,
			ActorUsername: cmd.Actor.Username,
			TargetNodeID:  targetNodeID,
			ResourceType:  commandResourceType(cmd.Kind),
			ResourceID:    cmd.ResourceID,
			Operation:     cmd.Kind,
			OperationID:   cmd.OperationID,
			Result:        "error",
			Detail:        resp.Error,
		})
		return nil, code, errors.New(resp.Error)
	}
	code := resp.StatusCode
	if code == 0 {
		code = http.StatusOK
	}
	if isMutating {
		m.finishRemoteOp(cmd.OperationID, "completed", string(resp.Data), "")
	}
	m.RecordAudit(AuditEntry{
		ActorNodeID:   cmd.Actor.IssuerNodeID,
		ActorUserID:   cmd.Actor.UserID,
		ActorUsername: cmd.Actor.Username,
		TargetNodeID:  targetNodeID,
		ResourceType:  commandResourceType(cmd.Kind),
		ResourceID:    cmd.ResourceID,
		Operation:     cmd.Kind,
		OperationID:   cmd.OperationID,
		Result:        "success",
	})
	return resp.Data, code, nil
}

func commandResourceType(kind string) string {
	if strings.HasPrefix(kind, "project.") {
		return "project"
	}
	if strings.HasPrefix(kind, "database.") {
		return "database"
	}
	return "computer"
}

type ComputerView struct {
	ID               string              `json:"id"`
	DisplayName      string              `json:"displayName"`
	AccessSlug       string              `json:"accessSlug,omitempty"`
	Hostname         string              `json:"hostname"`
	OS               string              `json:"os"`
	Arch             string              `json:"arch"`
	IsPrimary        bool                `json:"isPrimary"`
	Status           string              `json:"status"`
	Online           bool                `json:"online"`
	LastSeenAt       int64               `json:"lastSeenAt"`
	LastInventoryAt  int64               `json:"lastInventoryAt,omitempty"`
	VerificationCode string              `json:"verificationCode,omitempty"`
	LoginEnabled     bool                `json:"loginEnabled"`
	DelegatedOps     []string            `json:"delegatedOps"`
	Tunnel           TunnelHealthReport  `json:"tunnel"`
	DockerReady      bool                `json:"dockerReady"`
	DockerStatus     string              `json:"dockerStatus,omitempty"`
	Users            []RemoteUserSummary `json:"users,omitempty"`
	Grants           []PrincipalGrant    `json:"grants,omitempty"`
}

func (m *Manager) ComputersSnapshot(ctx context.Context) map[string]any {
	st := m.State()
	host, goOS, goArch := m.hostnameOSArch()
	localTunnel := TunnelHealthReport{Status: "not-configured"}
	localDockerReady := false
	localDockerStatus := ""
	localPrimaryURL := ""
	var localUsers []RemoteUserSummary
	if m.Provider != nil {
		localTunnel = m.Provider.LocalTunnelHealth(ctx)
		localPrimaryURL = m.Provider.LocalPrimaryURL()
		localUsers = m.Provider.LocalAdminUsers()
		if dbView, err := m.Provider.LocalDatabases(ctx); err == nil {
			localDockerReady = dbView.Docker
			localDockerStatus = string(dbView.DockerStatus)
		}
	}

	primaryView := ComputerView{
		ID:           m.InstallationID,
		DisplayName:  st.DisplayName,
		Hostname:     host,
		OS:           goOS,
		Arch:         goArch,
		IsPrimary:    true,
		Status:       NodeStatusApproved,
		Online:       true,
		LastSeenAt:   time.Now().Unix(),
		LoginEnabled: true,
		DelegatedOps: DefaultDelegatedOps,
		Tunnel:       localTunnel,
		DockerReady:  localDockerReady,
		DockerStatus: localDockerStatus,
		Users:        localUsers,
	}

	allNodes, _ := m.listNodes("")
	allGrants, _ := m.ListPrincipalGrants()
	grantsByNode := map[string][]PrincipalGrant{}
	for _, g := range allGrants {
		grantsByNode[g.IssuerNodeID] = append(grantsByNode[g.IssuerNodeID], g)
	}

	pending := []ComputerView{}
	computers := []ComputerView{}
	for _, n := range allNodes {
		view := ComputerView{
			ID:               n.ID,
			DisplayName:      n.DisplayName,
			AccessSlug:       n.AccessSlug,
			Hostname:         n.Hostname,
			OS:               n.OS,
			Arch:             n.Arch,
			Status:           n.Status,
			Online:           n.Status == NodeStatusApproved && m.NodeOnline(n.ID),
			LastSeenAt:       n.LastSeenAt,
			LastInventoryAt:  n.LastInventoryAt,
			VerificationCode: n.VerificationCode,
			LoginEnabled:     n.LoginEnabled,
			DelegatedOps:     n.DelegatedOps,
			Tunnel:           TunnelHealthReport{Configured: n.TunnelConfigured, Healthy: n.TunnelHealthy, Status: n.TunnelStatus},
			DockerReady:      n.DockerReady,
			DockerStatus:     n.DockerStatus,
			Users:            n.RemoteUsers,
			Grants:           grantsByNode[n.ID],
		}
		if n.Status == NodeStatusPending {
			pending = append(pending, view)
		} else if n.Status == NodeStatusApproved {
			computers = append(computers, view)
		}
	}
	audit, _ := m.ListAudit(50)
	return map[string]any{
		"role":       st.Role,
		"primaryUrl": localPrimaryURL,
		"primary":    primaryView,
		"pending":    pending,
		"computers":  computers,
		"audit":      audit,
	}
}

// AuthorizedNodes returns the list of computers visible to access (for UI filters & DB creation).
func (m *Manager) AuthorizedNodes(ctx context.Context, access SessionAccess) []ComputerView {
	st := m.State()
	out := []ComputerView{}
	if m.CanAccessNode(access, m.InstallationID) {
		host, goOS, goArch := m.hostnameOSArch()
		localTunnel := TunnelHealthReport{Status: "not-configured"}
		localDockerReady := false
		localDockerStatus := ""
		if m.Provider != nil {
			localTunnel = m.Provider.LocalTunnelHealth(ctx)
			if dbView, err := m.Provider.LocalDatabases(ctx); err == nil {
				localDockerReady = dbView.Docker
				localDockerStatus = string(dbView.DockerStatus)
			}
		}
		out = append(out, ComputerView{
			ID:           m.InstallationID,
			DisplayName:  st.DisplayName,
			Hostname:     host,
			OS:           goOS,
			Arch:         goArch,
			IsPrimary:    true,
			Status:       NodeStatusApproved,
			Online:       true,
			LastSeenAt:   time.Now().Unix(),
			LoginEnabled: true,
			DelegatedOps: DefaultDelegatedOps,
			Tunnel:       localTunnel,
			DockerReady:  localDockerReady,
			DockerStatus: localDockerStatus,
		})
	}
	approved, _ := m.listNodes(NodeStatusApproved)
	for _, n := range approved {
		if !m.CanAccessNode(access, n.ID) {
			continue
		}
		out = append(out, ComputerView{
			ID:              n.ID,
			DisplayName:     n.DisplayName,
			AccessSlug:      n.AccessSlug,
			Hostname:        n.Hostname,
			OS:              n.OS,
			Arch:            n.Arch,
			Status:          n.Status,
			Online:          m.NodeOnline(n.ID),
			LastSeenAt:      n.LastSeenAt,
			LastInventoryAt: n.LastInventoryAt,
			LoginEnabled:    n.LoginEnabled,
			DelegatedOps:    n.DelegatedOps,
			Tunnel:          TunnelHealthReport{Configured: n.TunnelConfigured, Healthy: n.TunnelHealthy, Status: n.TunnelStatus},
			DockerReady:     n.DockerReady,
			DockerStatus:    n.DockerStatus,
		})
	}
	return out
}

type AggregatedProject struct {
	SanitizedProject
	NodeID         string `json:"nodeId"`
	NodeName       string `json:"nodeName"`
	NodeOnline     bool   `json:"nodeOnline"`
	NodeLastSeenAt int64  `json:"nodeLastSeenAt"`
}

func (m *Manager) AggregatedProjects(access SessionAccess, filterNodeID string) []AggregatedProject {
	st := m.State()
	out := []AggregatedProject{}
	now := time.Now().Unix()

	if (filterNodeID == "" || filterNodeID == m.InstallationID) && m.CanAccessNode(access, m.InstallationID) && m.Provider != nil {
		for _, p := range m.Provider.LocalProjects() {
			out = append(out, AggregatedProject{
				SanitizedProject: p,
				NodeID:           m.InstallationID,
				NodeName:         st.DisplayName,
				NodeOnline:       true,
				NodeLastSeenAt:   now,
			})
		}
	}

	approved, _ := m.listNodes(NodeStatusApproved)
	for _, n := range approved {
		if filterNodeID != "" && filterNodeID != n.ID {
			continue
		}
		if !m.CanAccessNode(access, n.ID) {
			continue
		}
		inv, err := m.loadNodeInventory(n.ID)
		if err != nil {
			continue
		}
		online := m.NodeOnline(n.ID)
		lastSeen := n.LastSeenAt
		if inv.UpdatedAt > lastSeen {
			lastSeen = inv.UpdatedAt
		}
		for _, p := range inv.Projects {
			out = append(out, AggregatedProject{
				SanitizedProject: p,
				NodeID:           n.ID,
				NodeName:         n.DisplayName,
				NodeOnline:       online,
				NodeLastSeenAt:   lastSeen,
			})
		}
	}
	return out
}

type AggregatedDatabase struct {
	databases.Instance
	NodeID         string `json:"nodeId"`
	NodeName       string `json:"nodeName"`
	NodeOnline     bool   `json:"nodeOnline"`
	NodeLastSeenAt int64  `json:"nodeLastSeenAt"`
}

type AggregatedDatabaseNode struct {
	NodeID       string `json:"nodeId"`
	NodeName     string `json:"nodeName"`
	IsPrimary    bool   `json:"isPrimary"`
	Online       bool   `json:"online"`
	Docker       bool   `json:"docker"`
	DockerStatus string `json:"dockerStatus,omitempty"`
	LastSeenAt   int64  `json:"lastSeenAt"`
}

type AggregatedDatabaseCatalog struct {
	Databases    []AggregatedDatabase     `json:"databases"`
	Nodes        []AggregatedDatabaseNode `json:"nodes"`
	Docker       bool                     `json:"docker"`
	DockerStatus string                   `json:"dockerStatus,omitempty"`
	MemoryLimit  int                      `json:"memoryLimit"`
	CPU          float64                  `json:"cpu"`
}

func (m *Manager) AggregatedDatabases(ctx context.Context, access SessionAccess) (AggregatedDatabaseCatalog, error) {
	st := m.State()
	cat := AggregatedDatabaseCatalog{
		Databases:   []AggregatedDatabase{},
		Nodes:       []AggregatedDatabaseNode{},
		MemoryLimit: 1024 * 1024 * 1024,
		CPU:         1.0,
	}
	now := time.Now().Unix()

	if m.CanAccessNode(access, m.InstallationID) && m.Provider != nil {
		localView, err := m.Provider.LocalDatabases(ctx)
		if err == nil {
			cat.MemoryLimit = localView.MemoryLimit
			cat.CPU = localView.CPU
			if localView.Docker {
				cat.Docker = true
			}
			cat.DockerStatus = string(localView.DockerStatus)
			cat.Nodes = append(cat.Nodes, AggregatedDatabaseNode{
				NodeID:       m.InstallationID,
				NodeName:     st.DisplayName,
				IsPrimary:    true,
				Online:       true,
				Docker:       localView.Docker,
				DockerStatus: string(localView.DockerStatus),
				LastSeenAt:   now,
			})
			for _, db := range localView.Databases {
				db.LANFirewall = ""
				cat.Databases = append(cat.Databases, AggregatedDatabase{
					Instance:       db,
					NodeID:         m.InstallationID,
					NodeName:       st.DisplayName,
					NodeOnline:     true,
					NodeLastSeenAt: now,
				})
			}
		}
	}

	approved, _ := m.listNodes(NodeStatusApproved)
	for _, n := range approved {
		if !m.CanAccessNode(access, n.ID) {
			continue
		}
		online := m.NodeOnline(n.ID)
		if online && n.DockerReady {
			cat.Docker = true
		}
		if cat.DockerStatus == "" && n.DockerStatus != "" {
			cat.DockerStatus = n.DockerStatus
		}
		inv, err := m.loadNodeInventory(n.ID)
		if err != nil {
			continue
		}
		lastSeen := n.LastSeenAt
		if inv.UpdatedAt > lastSeen {
			lastSeen = inv.UpdatedAt
		}
		cat.Nodes = append(cat.Nodes, AggregatedDatabaseNode{
			NodeID:       n.ID,
			NodeName:     n.DisplayName,
			IsPrimary:    false,
			Online:       online,
			Docker:       n.DockerReady,
			DockerStatus: n.DockerStatus,
			LastSeenAt:   lastSeen,
		})
		for _, db := range inv.Databases {
			db.LANFirewall = ""
			cat.Databases = append(cat.Databases, AggregatedDatabase{
				Instance:       db,
				NodeID:         n.ID,
				NodeName:       n.DisplayName,
				NodeOnline:     online,
				NodeLastSeenAt: lastSeen,
			})
		}
	}
	return cat, nil
}
