package cluster

import (
	"cloudforge/core/internal/auth"
	"cloudforge/core/internal/databases"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"
)

type LocalState struct {
	Role               string   `json:"role"`
	DisplayName        string   `json:"displayName"`
	AccessSlug         string   `json:"accessSlug,omitempty"`
	PublicKeyPEM       string   `json:"-"`
	PublicKeyFP        string   `json:"fingerprint,omitempty"`
	PrimaryURL         string   `json:"primaryUrl,omitempty"`
	PrimaryID          string   `json:"primaryId,omitempty"`
	PrimaryName        string   `json:"primaryName,omitempty"`
	PrimaryFingerprint string   `json:"primaryFingerprint,omitempty"`
	PendingInviteID    string   `json:"-"`
	VerificationCode   string   `json:"verificationCode,omitempty"`
	LinkStatus         string   `json:"linkStatus,omitempty"`
	DelegatedOps       []string `json:"delegatedOps"`
	LinkedAt           int64    `json:"linkedAt,omitempty"`
	UpdatedAt          int64    `json:"updatedAt,omitempty"`
}

type NodeRecord struct {
	ID               string              `json:"id"`
	DisplayName      string              `json:"displayName"`
	AccessSlug       string              `json:"accessSlug"`
	Hostname         string              `json:"hostname"`
	OS               string              `json:"os"`
	Arch             string              `json:"arch"`
	PublicKeyPEM     string              `json:"-"`
	PublicKeyFP      string              `json:"fingerprint"`
	Status           string              `json:"status"`
	VerificationCode string              `json:"verificationCode,omitempty"`
	InviteID         string              `json:"-"`
	LoginEnabled     bool                `json:"loginEnabled"`
	DelegatedOps     []string            `json:"delegatedOps"`
	RequestedAt      int64               `json:"requestedAt"`
	ApprovedAt       int64               `json:"approvedAt,omitempty"`
	LastSeenAt       int64               `json:"lastSeenAt,omitempty"`
	LastInventoryAt  int64               `json:"lastInventoryAt,omitempty"`
	TunnelHealthy    bool                `json:"tunnelHealthy"`
	TunnelStatus     string              `json:"tunnelStatus"`
	TunnelConfigured bool                `json:"tunnelConfigured"`
	DockerReady      bool                `json:"dockerReady"`
	DockerStatus     string              `json:"dockerStatus,omitempty"`
	RemoteUsers      []RemoteUserSummary `json:"users,omitempty"`
}

type NodeInventory struct {
	NodeID    string               `json:"nodeId"`
	Projects  []SanitizedProject   `json:"projects"`
	Databases []databases.Instance `json:"databases"`
	UpdatedAt int64                `json:"updatedAt"`
}

type PrincipalGrant struct {
	IssuerNodeID string `json:"issuerNodeId"`
	UserID       string `json:"userId"`
	Username     string `json:"username"`
	Scope        string `json:"scope"` // "origin_only" or "unified"
	GrantedBy    string `json:"grantedBy,omitempty"`
	GrantedAt    int64  `json:"grantedAt,omitempty"`
	UpdatedAt    int64  `json:"updatedAt,omitempty"`
}

type RemoteOpRecord struct {
	OperationID  string
	Direction    string // "outbound" or "inbound"
	TargetNodeID string
	ResourceType string
	ResourceID   string
	Action       string
	Status       string // "in_progress", "completed", "failed", "unknown"
	ResultJSON   string
	ErrorText    string
	CreatedAt    int64
	UpdatedAt    int64
}

type AuditEntry struct {
	ID            string `json:"id"`
	CreatedAt     int64  `json:"createdAt"`
	ActorNodeID   string `json:"actorNodeId"`
	ActorUserID   string `json:"actorUserId"`
	ActorUsername string `json:"actorUsername"`
	TargetNodeID  string `json:"targetNodeId"`
	ResourceType  string `json:"resourceType"`
	ResourceID    string `json:"resourceId"`
	Operation     string `json:"operation"`
	OperationID   string `json:"operationId,omitempty"`
	Result        string `json:"result"`
	Detail        string `json:"detail,omitempty"`
}

var slugRegex = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$`)

func sanitizeDisplayName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := len([]rune(name)); n < 1 || n > 64 {
		return "", errors.New("informe um nome de exibição de 1 a 64 caracteres")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", errors.New("nome de exibição contém caracteres inválidos")
		}
	}
	return name, nil
}

func slugify(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	prevDash := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
		if b.Len() >= 28 {
			break
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) < 2 {
		s = "pc-" + strings.ToLower(auth.ID()[:6])
	}
	return s
}

func validAccessSlug(slug string) (string, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "primary" || slug == "principal" {
		return "", errors.New("esse nome curto é reservado ao computador principal")
	}
	if !slugRegex.MatchString(slug) || len(slug) < 2 {
		return "", errors.New("nome curto deve ter de 2 a 32 caracteres (letras minúsculas, números e hífen)")
	}
	return slug, nil
}

func defaultDisplayName() string {
	host, err := os.Hostname()
	if err == nil && strings.TrimSpace(host) != "" {
		name := strings.TrimSpace(host)
		if len([]rune(name)) > 64 {
			name = string([]rune(name)[:64])
		}
		return name
	}
	return "Cloudforge"
}

func (m *Manager) loadState() (LocalState, error) {
	var st LocalState
	var opsRaw string
	err := m.db.QueryRow(
		`SELECT role,display_name,access_slug,public_key_pem,public_key_fingerprint,primary_url,primary_id,primary_name,primary_fingerprint,pending_invite_id,verification_code,link_status,delegated_ops,linked_at,updated_at
		 FROM cluster_state WHERE singleton=1`,
	).Scan(
		&st.Role, &st.DisplayName, &st.AccessSlug, &st.PublicKeyPEM, &st.PublicKeyFP,
		&st.PrimaryURL, &st.PrimaryID, &st.PrimaryName, &st.PrimaryFingerprint,
		&st.PendingInviteID, &st.VerificationCode, &st.LinkStatus, &opsRaw, &st.LinkedAt, &st.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		st = LocalState{
			Role:         RoleStandalone,
			DisplayName:  defaultDisplayName(),
			DelegatedOps: append([]string{}, DefaultDelegatedOps...),
			UpdatedAt:    time.Now().Unix(),
		}
		if err := m.saveState(st); err != nil {
			return st, err
		}
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if st.DisplayName == "" {
		st.DisplayName = defaultDisplayName()
	}
	if json.Unmarshal([]byte(opsRaw), &st.DelegatedOps) != nil || len(st.DelegatedOps) == 0 {
		st.DelegatedOps = append([]string{}, DefaultDelegatedOps...)
	}
	return st, nil
}

func (m *Manager) saveState(st LocalState) error {
	if st.Role == "" {
		st.Role = RoleStandalone
	}
	if st.DisplayName == "" {
		st.DisplayName = defaultDisplayName()
	}
	if len(st.DelegatedOps) == 0 {
		st.DelegatedOps = append([]string{}, DefaultDelegatedOps...)
	}
	opsRaw, err := json.Marshal(st.DelegatedOps)
	if err != nil {
		return err
	}
	st.UpdatedAt = time.Now().Unix()
	_, err = m.db.Exec(
		`INSERT INTO cluster_state(singleton,role,display_name,access_slug,public_key_pem,public_key_fingerprint,primary_url,primary_id,primary_name,primary_fingerprint,pending_invite_id,verification_code,link_status,delegated_ops,linked_at,updated_at)
		 VALUES(1,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(singleton) DO UPDATE SET
		 role=excluded.role,display_name=excluded.display_name,access_slug=excluded.access_slug,
		 public_key_pem=excluded.public_key_pem,public_key_fingerprint=excluded.public_key_fingerprint,
		 primary_url=excluded.primary_url,primary_id=excluded.primary_id,primary_name=excluded.primary_name,
		 primary_fingerprint=excluded.primary_fingerprint,pending_invite_id=excluded.pending_invite_id,
		 verification_code=excluded.verification_code,link_status=excluded.link_status,
		 delegated_ops=excluded.delegated_ops,linked_at=excluded.linked_at,updated_at=excluded.updated_at`,
		st.Role, st.DisplayName, st.AccessSlug, st.PublicKeyPEM, st.PublicKeyFP,
		st.PrimaryURL, st.PrimaryID, st.PrimaryName, st.PrimaryFingerprint,
		st.PendingInviteID, st.VerificationCode, st.LinkStatus, string(opsRaw), st.LinkedAt, st.UpdatedAt,
	)
	return err
}

func (m *Manager) isKeyRevoked(fingerprint string) (bool, error) {
	if fingerprint == "" {
		return false, nil
	}
	var count int
	err := m.db.QueryRow("SELECT count(*) FROM cluster_revocations WHERE public_key_fingerprint=?", fingerprint).Scan(&count)
	return count > 0, err
}

func (m *Manager) insertInvite(id, secretHash, createdBy string, expiresAt int64) error {
	now := time.Now().Unix()
	// Mark stale active invites as expired cleanly.
	_, _ = m.db.Exec("UPDATE cluster_invites SET status='expired' WHERE status='active' AND expires_at<=?", now)
	_, err := m.db.Exec(
		"INSERT INTO cluster_invites(id,secret_hash,created_at,expires_at,used_at,attempts,max_attempts,status,created_by_user_id,pending_node_id) VALUES(?,?,?,?,NULL,0,5,'active',?,'')",
		id, secretHash, now, expiresAt, createdBy,
	)
	return err
}

func (m *Manager) validateAndBindInvite(req PairRequestMsg, peerFP, primaryID, primaryFP string) (string, int64, string, error) {
	m.pairMu.Lock()
	defer m.pairMu.Unlock()

	tx, err := m.db.Begin()
	if err != nil {
		return "", 0, "internal", err
	}
	defer tx.Rollback()

	var revCount int
	if err := tx.QueryRow("SELECT count(*) FROM cluster_revocations WHERE public_key_fingerprint=?", peerFP).Scan(&revCount); err != nil {
		return "", 0, "internal", err
	}
	if revCount > 0 {
		return "", 0, "revoked", errors.New("esta chave foi revogada; inicie um novo pareamento")
	}

	var existingStatus string
	err = tx.QueryRow("SELECT status FROM cluster_nodes WHERE id=?", req.NodeID).Scan(&existingStatus)
	if err == nil && existingStatus == NodeStatusApproved {
		return "", 0, "already_linked", errors.New("este computador já está vinculado ao painel principal")
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", 0, "internal", err
	}

	var secretHash, status, pendingNodeID string
	var expiresAt int64
	var attempts, maxAttempts int
	err = tx.QueryRow(
		"SELECT secret_hash,expires_at,attempts,max_attempts,status,pending_node_id FROM cluster_invites WHERE id=?",
		req.InviteID,
	).Scan(&secretHash, &expiresAt, &attempts, &maxAttempts, &status, &pendingNodeID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, "invalid_invite", errors.New("convite inválido ou expirado")
	}
	if err != nil {
		return "", 0, "internal", err
	}

	now := time.Now().Unix()
	if status == "consumed" || status == "rejected" {
		return "", 0, "invite_used", errors.New("convite já utilizado")
	}
	if status == "locked" || attempts >= maxAttempts {
		return "", 0, "invite_locked", errors.New("limite de tentativas do convite atingido")
	}
	if now > expiresAt || status == "expired" {
		_, _ = tx.Exec("UPDATE cluster_invites SET status='expired' WHERE id=?", req.InviteID)
		_ = tx.Commit()
		return "", 0, "invite_expired", errors.New("convite expirado")
	}
	if status == "pending" && pendingNodeID != req.NodeID {
		return "", 0, "invite_busy", errors.New("este convite já está associado a outra solicitação pendente")
	}

	if !constantTimeEqual(secretDigest(req.Secret), secretHash) {
		nextAttempts := attempts + 1
		nextStatus := status
		if nextAttempts >= maxAttempts {
			nextStatus = "locked"
		}
		_, _ = tx.Exec("UPDATE cluster_invites SET attempts=?,status=? WHERE id=?", nextAttempts, nextStatus, req.InviteID)
		_ = tx.Commit()
		return "", 0, "invalid_secret", errors.New("segredo do convite inválido")
	}

	if status == "active" {
		res, err := tx.Exec(
			"UPDATE cluster_invites SET status='pending',pending_node_id=?,attempts=attempts+1 WHERE id=? AND status='active' AND expires_at>?",
			req.NodeID, req.InviteID, now,
		)
		if err != nil {
			return "", 0, "internal", err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return "", 0, "invite_used", errors.New("convite não está mais disponível")
		}
	}

	displayName, err := sanitizeDisplayName(req.DisplayName)
	if err != nil {
		displayName = req.Hostname
		if displayName == "" {
			displayName = "Computador"
		}
	}
	delegated := req.DelegatedOps
	if len(delegated) == 0 {
		delegated = DefaultDelegatedOps
	}
	opsRaw, _ := json.Marshal(delegated)
	code := VerificationCode(primaryID, primaryFP, req.NodeID, peerFP, req.InviteID)
	pendingSlug := "pending-" + strings.ToLower(req.NodeID)

	_, err = tx.Exec(
		`INSERT INTO cluster_nodes(id,display_name,access_slug,hostname,os,arch,public_key_pem,public_key_fingerprint,status,verification_code,invite_id,login_enabled,delegated_ops,requested_at,approved_at,last_seen_at,last_inventory_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,1,?,?,0,?,0)
		 ON CONFLICT(id) DO UPDATE SET
		 display_name=excluded.display_name,hostname=excluded.hostname,os=excluded.os,arch=excluded.arch,
		 public_key_pem=excluded.public_key_pem,public_key_fingerprint=excluded.public_key_fingerprint,
		 status='pending',verification_code=excluded.verification_code,invite_id=excluded.invite_id,
		 delegated_ops=excluded.delegated_ops,requested_at=excluded.requested_at,last_seen_at=excluded.last_seen_at
		 WHERE cluster_nodes.status!='approved'`,
		req.NodeID, displayName, pendingSlug, req.Hostname, req.OS, req.Arch, req.PublicKeyPEM, peerFP,
		NodeStatusPending, code, req.InviteID, string(opsRaw), now, now,
	)
	if err != nil {
		return "", 0, "internal", err
	}
	if err := tx.Commit(); err != nil {
		return "", 0, "internal", err
	}
	return code, expiresAt, "", nil
}

func uniqueAccessSlugTx(tx *sql.Tx, base, excludeNodeID string) (string, error) {
	slug := slugify(base)
	for suffix := 1; suffix < 1000; suffix++ {
		candidate := slug
		if suffix > 1 {
			candidate = fmt.Sprintf("%s-%d", slug, suffix)
		}
		var count int
		if err := tx.QueryRow("SELECT count(*) FROM cluster_nodes WHERE access_slug=? AND id!=?", candidate, excludeNodeID).Scan(&count); err != nil {
			return "", err
		}
		if count == 0 {
			return candidate, nil
		}
	}
	return "", errors.New("não foi possível gerar um nome curto único")
}

func (m *Manager) approveNodeTx(nodeID string, actor Actor) (NodeRecord, error) {
	m.pairMu.Lock()
	defer m.pairMu.Unlock()

	tx, err := m.db.Begin()
	if err != nil {
		return NodeRecord{}, err
	}
	defer tx.Rollback()

	node, err := scanNodeRow(tx.QueryRow(nodeSelectSQL+" WHERE id=? AND status='pending'", nodeID))
	if errors.Is(err, sql.ErrNoRows) {
		return NodeRecord{}, errors.New("solicitação pendente não encontrada ou já processada")
	}
	if err != nil {
		return NodeRecord{}, err
	}

	var revCount int
	if err := tx.QueryRow("SELECT count(*) FROM cluster_revocations WHERE public_key_fingerprint=?", node.PublicKeyFP).Scan(&revCount); err != nil {
		return NodeRecord{}, err
	}
	if revCount > 0 {
		return NodeRecord{}, errors.New("chave pública revogada não pode ser aprovada")
	}

	now := time.Now().Unix()
	res, err := tx.Exec(
		"UPDATE cluster_invites SET status='consumed',used_at=? WHERE id=? AND status='pending' AND pending_node_id=? AND expires_at>?",
		now, node.InviteID, nodeID, now,
	)
	if err != nil {
		return NodeRecord{}, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return NodeRecord{}, errors.New("convite expirado ou já consumido")
	}

	slug, err := uniqueAccessSlugTx(tx, node.DisplayName, nodeID)
	if err != nil {
		return NodeRecord{}, err
	}

	res, err = tx.Exec(
		"UPDATE cluster_nodes SET status='approved',access_slug=?,approved_at=?,verification_code='',last_seen_at=? WHERE id=? AND status='pending'",
		slug, now, now, nodeID,
	)
	if err != nil {
		return NodeRecord{}, err
	}
	n, _ = res.RowsAffected()
	if n != 1 {
		return NodeRecord{}, errors.New("solicitação já foi processada")
	}

	if err := insertAuditTx(tx, AuditEntry{
		ID:            auth.ID(),
		CreatedAt:     now,
		ActorNodeID:   actor.IssuerNodeID,
		ActorUserID:   actor.UserID,
		ActorUsername: actor.Username,
		TargetNodeID:  nodeID,
		ResourceType:  "computer",
		ResourceID:    nodeID,
		Operation:     "node.approve",
		Result:        "success",
		Detail:        fmt.Sprintf("name=%s slug=%s", node.DisplayName, slug),
	}); err != nil {
		return NodeRecord{}, err
	}

	if err := tx.Commit(); err != nil {
		return NodeRecord{}, err
	}
	node.Status = NodeStatusApproved
	node.AccessSlug = slug
	node.ApprovedAt = now
	node.VerificationCode = ""
	return node, nil
}

func (m *Manager) rejectNodeTx(nodeID string, actor Actor) (NodeRecord, error) {
	m.pairMu.Lock()
	defer m.pairMu.Unlock()

	tx, err := m.db.Begin()
	if err != nil {
		return NodeRecord{}, err
	}
	defer tx.Rollback()

	node, err := scanNodeRow(tx.QueryRow(nodeSelectSQL+" WHERE id=? AND status='pending'", nodeID))
	if errors.Is(err, sql.ErrNoRows) {
		return NodeRecord{}, errors.New("solicitação pendente não encontrada")
	}
	if err != nil {
		return NodeRecord{}, err
	}

	now := time.Now().Unix()
	if node.InviteID != "" {
		_, _ = tx.Exec("UPDATE cluster_invites SET status='rejected',used_at=? WHERE id=? AND status IN ('active','pending')", now, node.InviteID)
	}
	if _, err := tx.Exec("DELETE FROM cluster_nodes WHERE id=? AND status='pending'", nodeID); err != nil {
		return NodeRecord{}, err
	}
	if err := insertAuditTx(tx, AuditEntry{
		ID:            auth.ID(),
		CreatedAt:     now,
		ActorNodeID:   actor.IssuerNodeID,
		ActorUserID:   actor.UserID,
		ActorUsername: actor.Username,
		TargetNodeID:  nodeID,
		ResourceType:  "computer",
		ResourceID:    nodeID,
		Operation:     "node.reject",
		Result:        "success",
		Detail:        "name=" + node.DisplayName,
	}); err != nil {
		return NodeRecord{}, err
	}
	return node, tx.Commit()
}

func (m *Manager) removeNodeTx(nodeID, reason string, actor Actor) (NodeRecord, error) {
	tx, err := m.db.Begin()
	if err != nil {
		return NodeRecord{}, err
	}
	defer tx.Rollback()

	node, err := scanNodeRow(tx.QueryRow(nodeSelectSQL+" WHERE id=?", nodeID))
	if errors.Is(err, sql.ErrNoRows) {
		return NodeRecord{}, errors.New("computador não encontrado")
	}
	if err != nil {
		return NodeRecord{}, err
	}

	now := time.Now().Unix()
	if node.PublicKeyFP != "" {
		if _, err := tx.Exec(
			"INSERT INTO cluster_revocations(id,node_id,public_key_fingerprint,revoked_at,reason) VALUES(?,?,?,?,?) ON CONFLICT(public_key_fingerprint) DO NOTHING",
			auth.ID(), node.ID, node.PublicKeyFP, now, reason,
		); err != nil {
			return NodeRecord{}, err
		}
	}
	if node.InviteID != "" {
		_, _ = tx.Exec("UPDATE cluster_invites SET status='rejected',used_at=? WHERE id=? AND status IN ('active','pending')", now, node.InviteID)
	}
	if _, err := tx.Exec("DELETE FROM cluster_principal_grants WHERE issuer_node_id=?", nodeID); err != nil {
		return NodeRecord{}, err
	}
	if _, err := tx.Exec("DELETE FROM cluster_inventory WHERE node_id=?", nodeID); err != nil {
		return NodeRecord{}, err
	}
	if _, err := tx.Exec("DELETE FROM cluster_nodes WHERE id=?", nodeID); err != nil {
		return NodeRecord{}, err
	}
	if err := insertAuditTx(tx, AuditEntry{
		ID:            auth.ID(),
		CreatedAt:     now,
		ActorNodeID:   actor.IssuerNodeID,
		ActorUserID:   actor.UserID,
		ActorUsername: actor.Username,
		TargetNodeID:  nodeID,
		ResourceType:  "computer",
		ResourceID:    nodeID,
		Operation:     "node.remove",
		Result:        "success",
		Detail:        fmt.Sprintf("name=%s reason=%s", node.DisplayName, reason),
	}); err != nil {
		return NodeRecord{}, err
	}
	return node, tx.Commit()
}

const nodeSelectSQL = `SELECT id,display_name,access_slug,hostname,os,arch,public_key_pem,public_key_fingerprint,status,verification_code,invite_id,login_enabled,delegated_ops,requested_at,approved_at,last_seen_at,last_inventory_at,tunnel_healthy,tunnel_status,tunnel_configured,docker_ready,docker_status,remote_users_json FROM cluster_nodes`

func scanNodeRow(row interface{ Scan(...any) error }) (NodeRecord, error) {
	var n NodeRecord
	var loginInt, tunnelHealthyInt, tunnelCfgInt, dockerReadyInt int
	var opsRaw, usersRaw string
	err := row.Scan(
		&n.ID, &n.DisplayName, &n.AccessSlug, &n.Hostname, &n.OS, &n.Arch,
		&n.PublicKeyPEM, &n.PublicKeyFP, &n.Status, &n.VerificationCode, &n.InviteID,
		&loginInt, &opsRaw, &n.RequestedAt, &n.ApprovedAt, &n.LastSeenAt, &n.LastInventoryAt,
		&tunnelHealthyInt, &n.TunnelStatus, &tunnelCfgInt, &dockerReadyInt, &n.DockerStatus, &usersRaw,
	)
	if err != nil {
		return n, err
	}
	n.LoginEnabled = loginInt != 0
	n.TunnelHealthy = tunnelHealthyInt != 0
	n.TunnelConfigured = tunnelCfgInt != 0
	n.DockerReady = dockerReadyInt != 0
	if json.Unmarshal([]byte(opsRaw), &n.DelegatedOps) != nil {
		n.DelegatedOps = append([]string{}, DefaultDelegatedOps...)
	}
	_ = json.Unmarshal([]byte(usersRaw), &n.RemoteUsers)
	return n, nil
}

func (m *Manager) getNode(nodeID string) (NodeRecord, error) {
	return scanNodeRow(m.db.QueryRow(nodeSelectSQL+" WHERE id=?", nodeID))
}

func (m *Manager) getApprovedNodeBySlug(slug string) (NodeRecord, error) {
	return scanNodeRow(m.db.QueryRow(nodeSelectSQL+" WHERE access_slug=? AND status='approved'", strings.ToLower(strings.TrimSpace(slug))))
}

func (m *Manager) listNodes(status string) ([]NodeRecord, error) {
	q := nodeSelectSQL
	var args []any
	if status != "" {
		q += " WHERE status=?"
		args = append(args, status)
	}
	q += " ORDER BY approved_at ASC, requested_at DESC"
	rows, err := m.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NodeRecord{}
	for rows.Next() {
		n, err := scanNodeRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (m *Manager) saveNodeInventory(nodeID string, msg InventorySyncMsg) error {
	now := time.Now().Unix()
	projRaw, err := json.Marshal(msg.Projects)
	if err != nil {
		return err
	}
	dbRaw, err := json.Marshal(msg.Databases)
	if err != nil {
		return err
	}
	usersRaw, err := json.Marshal(msg.Users)
	if err != nil {
		return err
	}
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`INSERT INTO cluster_inventory(node_id,projects_json,databases_json,updated_at) VALUES(?,?,?,?)
		 ON CONFLICT(node_id) DO UPDATE SET projects_json=excluded.projects_json,databases_json=excluded.databases_json,updated_at=excluded.updated_at`,
		nodeID, string(projRaw), string(dbRaw), now,
	); err != nil {
		return err
	}

	tunnelHealthy, tunnelCfg, dockerReady := 0, 0, 0
	if msg.Tunnel.Healthy {
		tunnelHealthy = 1
	}
	if msg.Tunnel.Configured {
		tunnelCfg = 1
	}
	if msg.DockerReady {
		dockerReady = 1
	}
	if _, err := tx.Exec(
		`UPDATE cluster_nodes SET hostname=?,os=?,arch=?,last_seen_at=?,last_inventory_at=?,tunnel_healthy=?,tunnel_status=?,tunnel_configured=?,docker_ready=?,docker_status=?,remote_users_json=? WHERE id=? AND status='approved'`,
		msg.Hostname, msg.OS, msg.Arch, now, now, tunnelHealthy, msg.Tunnel.Status, tunnelCfg, dockerReady, msg.DockerStatus, string(usersRaw), nodeID,
	); err != nil {
		return err
	}
	// Ensure known federated users appear in cluster_principal_grants with default 'origin_only' scope (without overwriting existing unified grants).
	for _, u := range msg.Users {
		if u.UserID == "" {
			continue
		}
		_, _ = tx.Exec(
			`INSERT INTO cluster_principal_grants(issuer_node_id,user_id,username,scope,granted_by,granted_at,updated_at)
			 VALUES(?,?,?,'origin_only','',0,?)
			 ON CONFLICT(issuer_node_id,user_id) DO UPDATE SET username=excluded.username,updated_at=excluded.updated_at`,
			nodeID, u.UserID, u.Username, now,
		)
	}
	return tx.Commit()
}

func (m *Manager) loadNodeInventory(nodeID string) (NodeInventory, error) {
	var inv NodeInventory
	inv.NodeID = nodeID
	var projRaw, dbRaw string
	err := m.db.QueryRow("SELECT projects_json,databases_json,updated_at FROM cluster_inventory WHERE node_id=?", nodeID).Scan(&projRaw, &dbRaw, &inv.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		inv.Projects = []SanitizedProject{}
		inv.Databases = []databases.Instance{}
		return inv, nil
	}
	if err != nil {
		return inv, err
	}
	if err := json.Unmarshal([]byte(projRaw), &inv.Projects); err != nil {
		inv.Projects = []SanitizedProject{}
	}
	if err := json.Unmarshal([]byte(dbRaw), &inv.Databases); err != nil {
		inv.Databases = []databases.Instance{}
	}
	return inv, nil
}

func (m *Manager) updateNodeHeartbeat(nodeID string, msg HeartbeatMsg) {
	now := time.Now().Unix()
	tunnelHealthy, tunnelCfg := 0, 0
	if msg.Tunnel.Healthy {
		tunnelHealthy = 1
	}
	if msg.Tunnel.Configured {
		tunnelCfg = 1
	}
	if len(msg.Users) > 0 {
		usersRaw, _ := json.Marshal(msg.Users)
		_, _ = m.db.Exec(
			"UPDATE cluster_nodes SET last_seen_at=?,tunnel_healthy=?,tunnel_status=?,tunnel_configured=?,remote_users_json=? WHERE id=? AND status='approved'",
			now, tunnelHealthy, msg.Tunnel.Status, tunnelCfg, string(usersRaw), nodeID,
		)
	} else {
		_, _ = m.db.Exec(
			"UPDATE cluster_nodes SET last_seen_at=?,tunnel_healthy=?,tunnel_status=?,tunnel_configured=? WHERE id=? AND status='approved'",
			now, tunnelHealthy, msg.Tunnel.Status, tunnelCfg, nodeID,
		)
	}
}

func (m *Manager) GetPrincipalGrant(issuerNodeID, userID string) (PrincipalGrant, error) {
	var g PrincipalGrant
	err := m.db.QueryRow(
		"SELECT issuer_node_id,user_id,username,scope,granted_by,granted_at,updated_at FROM cluster_principal_grants WHERE issuer_node_id=? AND user_id=?",
		issuerNodeID, userID,
	).Scan(&g.IssuerNodeID, &g.UserID, &g.Username, &g.Scope, &g.GrantedBy, &g.GrantedAt, &g.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return PrincipalGrant{IssuerNodeID: issuerNodeID, UserID: userID, Scope: ScopeOriginOnly}, nil
	}
	return g, err
}

func (m *Manager) ListPrincipalGrants() ([]PrincipalGrant, error) {
	rows, err := m.db.Query("SELECT issuer_node_id,user_id,username,scope,granted_by,granted_at,updated_at FROM cluster_principal_grants ORDER BY issuer_node_id, username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PrincipalGrant{}
	for rows.Next() {
		var g PrincipalGrant
		if err := rows.Scan(&g.IssuerNodeID, &g.UserID, &g.Username, &g.Scope, &g.GrantedBy, &g.GrantedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (m *Manager) SetPrincipalGrant(issuerNodeID, userID, username, scope string, actor Actor) (PrincipalGrant, error) {
	if scope != ScopeOriginOnly && scope != ScopeUnified {
		return PrincipalGrant{}, errors.New("escopo de acesso inválido")
	}
	if issuerNodeID == "" || userID == "" {
		return PrincipalGrant{}, errors.New("identidade vinculada inválida")
	}
	node, err := m.getNode(issuerNodeID)
	if err != nil || node.Status != NodeStatusApproved {
		return PrincipalGrant{}, errors.New("computador vinculado não encontrado")
	}
	if username == "" {
		for _, u := range node.RemoteUsers {
			if u.UserID == userID {
				username = u.Username
				break
			}
		}
	}
	now := time.Now().Unix()
	grantedBy := actor.Username
	if actor.IssuerNodeID != "" && actor.IssuerNodeID != m.InstallationID {
		grantedBy = actor.Username + "@" + actor.IssuerNodeID
	}
	grantedAt := int64(0)
	if scope == ScopeUnified {
		grantedAt = now
	}
	tx, err := m.db.Begin()
	if err != nil {
		return PrincipalGrant{}, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`INSERT INTO cluster_principal_grants(issuer_node_id,user_id,username,scope,granted_by,granted_at,updated_at)
		 VALUES(?,?,?,?,?,?,?)
		 ON CONFLICT(issuer_node_id,user_id) DO UPDATE SET
		 username=CASE WHEN excluded.username!='' THEN excluded.username ELSE cluster_principal_grants.username END,
		 scope=excluded.scope,granted_by=excluded.granted_by,granted_at=excluded.granted_at,updated_at=excluded.updated_at`,
		issuerNodeID, userID, username, scope, grantedBy, grantedAt, now,
	); err != nil {
		return PrincipalGrant{}, err
	}
	if err := insertAuditTx(tx, AuditEntry{
		ID:            auth.ID(),
		CreatedAt:     now,
		ActorNodeID:   actor.IssuerNodeID,
		ActorUserID:   actor.UserID,
		ActorUsername: actor.Username,
		TargetNodeID:  issuerNodeID,
		ResourceType:  "principal",
		ResourceID:    issuerNodeID + ":" + userID,
		Operation:     "grant." + scope,
		Result:        "success",
		Detail:        fmt.Sprintf("user=%s scope=%s", username, scope),
	}); err != nil {
		return PrincipalGrant{}, err
	}
	if err := tx.Commit(); err != nil {
		return PrincipalGrant{}, err
	}
	// If unified access was revoked, immediately invalidate affected sessions so elevated access cannot persist.
	if scope == ScopeOriginOnly && m.Auth != nil {
		_ = m.Auth.RevokePrincipal(issuerNodeID, userID)
	}
	return m.GetPrincipalGrant(issuerNodeID, userID)
}

func (m *Manager) tryBeginRemoteOp(op RemoteOpRecord) (RemoteOpRecord, bool, error) {
	now := time.Now().Unix()
	if op.CreatedAt == 0 {
		op.CreatedAt = now
	}
	op.UpdatedAt = now
	tx, err := m.db.Begin()
	if err != nil {
		return RemoteOpRecord{}, false, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`INSERT OR IGNORE INTO cluster_remote_operations(operation_id,direction,target_node_id,resource_type,resource_id,action,status,result_json,error_text,created_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		op.OperationID, op.Direction, op.TargetNodeID, op.ResourceType, op.ResourceID, op.Action, op.Status, op.ResultJSON, op.ErrorText, op.CreatedAt, op.UpdatedAt,
	)
	if err != nil {
		return RemoteOpRecord{}, false, err
	}
	n, _ := res.RowsAffected()
	if n == 1 {
		return op, true, tx.Commit()
	}
	var existing RemoteOpRecord
	err = tx.QueryRow(
		`SELECT operation_id,direction,target_node_id,resource_type,resource_id,action,status,result_json,error_text,created_at,updated_at
		 FROM cluster_remote_operations WHERE operation_id=?`,
		op.OperationID,
	).Scan(
		&existing.OperationID, &existing.Direction, &existing.TargetNodeID, &existing.ResourceType,
		&existing.ResourceID, &existing.Action, &existing.Status, &existing.ResultJSON,
		&existing.ErrorText, &existing.CreatedAt, &existing.UpdatedAt,
	)
	if err != nil {
		return RemoteOpRecord{}, false, err
	}
	return existing, false, tx.Commit()
}

func (m *Manager) finishRemoteOp(operationID, status, resultJSON, errorText string) {
	now := time.Now().Unix()
	_, _ = m.db.Exec(
		"UPDATE cluster_remote_operations SET status=?,result_json=?,error_text=?,updated_at=? WHERE operation_id=?",
		status, resultJSON, errorText, now, operationID,
	)
}

func insertAuditTx(tx *sql.Tx, entry AuditEntry) error {
	if entry.ID == "" {
		entry.ID = auth.ID()
	}
	if entry.CreatedAt == 0 {
		entry.CreatedAt = time.Now().Unix()
	}
	_, err := tx.Exec(
		`INSERT INTO cluster_audit_logs(id,created_at,actor_node_id,actor_user_id,actor_username,target_node_id,resource_type,resource_id,operation,operation_id,result,detail)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		entry.ID, entry.CreatedAt, entry.ActorNodeID, entry.ActorUserID, entry.ActorUsername,
		entry.TargetNodeID, entry.ResourceType, entry.ResourceID, entry.Operation, entry.OperationID,
		entry.Result, entry.Detail,
	)
	return err
}

func (m *Manager) RecordAudit(entry AuditEntry) {
	if entry.ID == "" {
		entry.ID = auth.ID()
	}
	if entry.CreatedAt == 0 {
		entry.CreatedAt = time.Now().Unix()
	}
	_, _ = m.db.Exec(
		`INSERT INTO cluster_audit_logs(id,created_at,actor_node_id,actor_user_id,actor_username,target_node_id,resource_type,resource_id,operation,operation_id,result,detail)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		entry.ID, entry.CreatedAt, entry.ActorNodeID, entry.ActorUserID, entry.ActorUsername,
		entry.TargetNodeID, entry.ResourceType, entry.ResourceID, entry.Operation, entry.OperationID,
		entry.Result, entry.Detail,
	)
}

func (m *Manager) ListAudit(limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := m.db.Query(
		`SELECT id,created_at,actor_node_id,actor_user_id,actor_username,target_node_id,resource_type,resource_id,operation,operation_id,result,detail
		 FROM cluster_audit_logs ORDER BY created_at DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(
			&e.ID, &e.CreatedAt, &e.ActorNodeID, &e.ActorUserID, &e.ActorUsername,
			&e.TargetNodeID, &e.ResourceType, &e.ResourceID, &e.Operation, &e.OperationID,
			&e.Result, &e.Detail,
		); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
