package cluster

import (
	"cloudforge/core/internal/databases"
	"cloudforge/core/internal/model"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

const (
	ProtocolVersion = 1
	MaxFrameSize    = 2 * 1024 * 1024 // 2 MiB max frame size over mTLS

	RoleStandalone = "standalone"
	RolePrimary    = "primary"
	RoleSecondary  = "secondary"

	NodeStatusPending  = "pending"
	NodeStatusApproved = "approved"
	NodeStatusRejected = "rejected"

	LinkStatusNone            = ""
	LinkStatusPendingApproval = "pending_approval"
	LinkStatusSyncing         = "syncing"
	LinkStatusLinked          = "linked"
	LinkStatusRevoked         = "revoked"
	LinkStatusRejected        = "rejected"

	ScopeOriginOnly = "origin_only"
	ScopeUnified    = "unified"

	// Explicit typed message kinds on the mTLS cluster channel.
	MsgHello            = "hello"
	MsgHelloAck         = "hello_ack"
	MsgPairRequest      = "pair_request"
	MsgPairResponse     = "pair_response"
	MsgPairApproved     = "pair_approved"
	MsgPairRejected     = "pair_rejected"
	MsgHeartbeat        = "heartbeat"
	MsgHeartbeatAck     = "heartbeat_ack"
	MsgInventorySync    = "inventory_sync"
	MsgInventoryAck     = "inventory_ack"
	MsgAuthVerify       = "auth_verify"
	MsgAuthVerifyResult = "auth_verify_result"
	MsgAuthReauth       = "auth_reauth"
	MsgAuthReauthResult = "auth_reauth_result"
	MsgAuthStatus       = "auth_status"
	MsgAuthStatusResult = "auth_status_result"
	MsgAuthRevoked      = "auth_revoked"
	MsgExecCommand      = "exec_command"
	MsgExecResult       = "exec_result"
	MsgNodeRevoked      = "node_revoked"

	// Explicit typed command operations allowed across nodes (no terminal, files, or firewall).
	CmdProjectAction       = "project.action"
	CmdDatabaseList        = "database.list"
	CmdDatabaseCreate      = "database.create"
	CmdDatabaseDetail      = "database.detail"
	CmdDatabaseChange      = "database.change"
	CmdDatabaseReveal      = "database.reveal"
	CmdDatabaseCredentials = "database.credentials"
	CmdDatabaseCertificate = "database.certificate"
	CmdDatabaseTunnelGet   = "database.tunnel.get"

	// Sensitive step-up action identifiers.
	StepUpDatabasePassword  = "database.password"
	StepUpDatabaseExportKey = "database.export_key"
	StepUpDatabaseDelete    = "database.delete"
)

var DefaultDelegatedOps = []string{"projects:*", "databases:*"}

func IsAllowedCommandKind(kind string) bool {
	switch kind {
	case CmdProjectAction,
		CmdDatabaseList,
		CmdDatabaseCreate,
		CmdDatabaseDetail,
		CmdDatabaseChange,
		CmdDatabaseReveal,
		CmdDatabaseCredentials,
		CmdDatabaseCertificate,
		CmdDatabaseTunnelGet:
		return true
	default:
		return false
	}
}

func OperationDelegated(delegated []string, kind string) bool {
	if !IsAllowedCommandKind(kind) {
		return false
	}
	group, _, _ := strings.Cut(kind, ".")
	if group == "project" {
		group = "projects"
	} else if group == "database" {
		group = "databases"
	}
	return slices.Contains(delegated, kind) || slices.Contains(delegated, group+":*")
}

func IsValidStepUpAction(action string) bool {
	switch action {
	case StepUpDatabasePassword, StepUpDatabaseExportKey, StepUpDatabaseDelete:
		return true
	default:
		return false
	}
}

type Envelope struct {
	Version int             `json:"v"`
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	ReplyTo string          `json:"replyTo,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

func writeFrame(w io.Writer, env Envelope) error {
	env.Version = ProtocolVersion
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	if len(data) > MaxFrameSize {
		return errors.New("mensagem excede o limite do protocolo")
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(data)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func readFrame(r io.Reader) (Envelope, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Envelope{}, err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > MaxFrameSize {
		return Envelope{}, fmt.Errorf("tamanho de mensagem inválido (%d)", size)
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		return Envelope{}, err
	}
	var env Envelope
	if err := json.Unmarshal(buf, &env); err != nil {
		return Envelope{}, fmt.Errorf("quadro JSON inválido: %w", err)
	}
	if env.Version != ProtocolVersion {
		return Envelope{}, fmt.Errorf("versão de protocolo não suportada (%d)", env.Version)
	}
	return env, nil
}

type HelloMsg struct {
	NodeID      string `json:"nodeId"`
	Mode        string `json:"mode"` // "pair" or "session"
	DisplayName string `json:"displayName"`
	Hostname    string `json:"hostname"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	AppVersion  string `json:"appVersion"`
}

type HelloAckMsg struct {
	OK          bool   `json:"ok"`
	Error       string `json:"error,omitempty"`
	ErrorCode   string `json:"errorCode,omitempty"` // "revoked", "not_approved", "protocol_mismatch"
	PrimaryID   string `json:"primaryId,omitempty"`
	PrimaryName string `json:"primaryName,omitempty"`
	PrimaryURL  string `json:"primaryUrl,omitempty"`
	AccessSlug  string `json:"accessSlug,omitempty"`
	HeartbeatMs int64  `json:"heartbeatMs,omitempty"`
}

type PairRequestMsg struct {
	InviteID     string   `json:"inviteId"`
	Secret       string   `json:"secret"`
	NodeID       string   `json:"nodeId"`
	DisplayName  string   `json:"displayName"`
	Hostname     string   `json:"hostname"`
	OS           string   `json:"os"`
	Arch         string   `json:"arch"`
	PublicKeyPEM string   `json:"publicKeyPem"`
	DelegatedOps []string `json:"delegatedOps,omitempty"`
}

type PairResponseMsg struct {
	Accepted         bool   `json:"accepted"`
	Status           string `json:"status,omitempty"` // "pending"
	VerificationCode string `json:"verificationCode,omitempty"`
	PrimaryID        string `json:"primaryId,omitempty"`
	PrimaryName      string `json:"primaryName,omitempty"`
	PrimaryURL       string `json:"primaryUrl,omitempty"`
	ExpiresAt        int64  `json:"expiresAt,omitempty"`
	Error            string `json:"error,omitempty"`
	ErrorCode        string `json:"errorCode,omitempty"`
}

type PairApprovedMsg struct {
	NodeID      string `json:"nodeId"`
	PrimaryID   string `json:"primaryId"`
	PrimaryName string `json:"primaryName"`
	PrimaryURL  string `json:"primaryUrl"`
	AccessSlug  string `json:"accessSlug"`
	HeartbeatMs int64  `json:"heartbeatMs"`
}

type PairRejectedMsg struct {
	Reason string `json:"reason"`
}

type RemoteUserSummary struct {
	UserID    string `json:"userId"`
	Username  string `json:"username"`
	AuthEpoch int64  `json:"authEpoch"`
}

type TunnelHealthReport struct {
	Configured bool   `json:"configured"`
	Healthy    bool   `json:"healthy"`
	Status     string `json:"status"`
}

type HeartbeatMsg struct {
	SentAt int64               `json:"sentAt"`
	Tunnel TunnelHealthReport  `json:"tunnel"`
	Users  []RemoteUserSummary `json:"users,omitempty"`
}

type HeartbeatAckMsg struct {
	SentAt     int64  `json:"sentAt"`
	ServerTime int64  `json:"serverTime"`
	PrimaryURL string `json:"primaryUrl,omitempty"`
}

// SanitizedProject carries only remote-safe project metadata (no local paths or secrets).
type SanitizedProject struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Runtime      string              `json:"runtime"`
	Hostname     string              `json:"hostname,omitempty"`
	Execution    SanitizedExecution  `json:"execution"`
	UsageHistory []model.UsageSample `json:"usageHistory,omitempty"`
}

type SanitizedExecution struct {
	ActualState string     `json:"actualState"`
	StartedAt   *time.Time `json:"startedAt"`
	CPU         *float64   `json:"cpu"`
	Memory      *uint64    `json:"memory"`
	LastError   string     `json:"lastError"`
}

type InventorySyncMsg struct {
	Seq          uint64               `json:"seq"`
	SyncedAt     int64                `json:"syncedAt"`
	Hostname     string               `json:"hostname"`
	OS           string               `json:"os"`
	Arch         string               `json:"arch"`
	Tunnel       TunnelHealthReport   `json:"tunnel"`
	DockerReady  bool                 `json:"dockerReady"`
	DockerStatus string               `json:"dockerStatus,omitempty"`
	Projects     []SanitizedProject   `json:"projects"`
	Databases    []databases.Instance `json:"databases"`
	Users        []RemoteUserSummary  `json:"users,omitempty"`
	DelegatedOps []string             `json:"delegatedOps,omitempty"`
}

type InventoryAckMsg struct {
	Seq        uint64 `json:"seq"`
	Confirmed  bool   `json:"confirmed"`
	PrimaryURL string `json:"primaryUrl,omitempty"`
	AccessSlug string `json:"accessSlug,omitempty"`
}

type AuthVerifyReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	ClientIP string `json:"clientIp,omitempty"`
}

type AuthVerifyResp struct {
	OK         bool   `json:"ok"`
	UserID     string `json:"userId,omitempty"`
	Username   string `json:"username,omitempty"`
	AuthEpoch  int64  `json:"authEpoch,omitempty"`
	StatusCode int    `json:"statusCode,omitempty"`
	Error      string `json:"error,omitempty"`
}

type AuthReauthReq struct {
	UserID       string `json:"userId"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	TargetNodeID string `json:"targetNodeId"`
	ResourceID   string `json:"resourceId"`
	Action       string `json:"action"`
	ClientIP     string `json:"clientIp,omitempty"`
}

type AuthReauthResp struct {
	OK         bool   `json:"ok"`
	AuthEpoch  int64  `json:"authEpoch,omitempty"`
	StatusCode int    `json:"statusCode,omitempty"`
	Error      string `json:"error,omitempty"`
}

type AuthStatusReq struct {
	UserID string `json:"userId"`
}

type AuthStatusResp struct {
	Exists    bool   `json:"exists"`
	Username  string `json:"username,omitempty"`
	AuthEpoch int64  `json:"authEpoch,omitempty"`
}

type AuthRevokedMsg struct {
	UserID    string `json:"userId"`
	Username  string `json:"username,omitempty"`
	AuthEpoch int64  `json:"authEpoch"`
	Reason    string `json:"reason"`
}

type Actor struct {
	IssuerNodeID string `json:"issuerNodeId"`
	UserID       string `json:"userId"`
	Username     string `json:"username"`
}

type StepUpGrant struct {
	GrantID      string `json:"grantId"`
	IssuerNodeID string `json:"issuerNodeId"`
	UserID       string `json:"userId"`
	TargetNodeID string `json:"targetNodeId"`
	ResourceID   string `json:"resourceId"`
	Action       string `json:"action"`
	IssuedAt     int64  `json:"issuedAt"`
	ExpiresAt    int64  `json:"expiresAt"`
	Signature    string `json:"signature"`
}

type ExecCommandReq struct {
	OperationID string          `json:"operationId"`
	Kind        string          `json:"kind"`
	ResourceID  string          `json:"resourceId,omitempty"`
	Actor       Actor           `json:"actor"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	StepUp      *StepUpGrant    `json:"stepUp,omitempty"`
}

type ExecCommandResp struct {
	OK         bool            `json:"ok"`
	StatusCode int             `json:"statusCode,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
	Error      string          `json:"error,omitempty"`
}

type NodeRevokedMsg struct {
	Reason string `json:"reason"`
}
