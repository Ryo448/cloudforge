package cluster

import (
	"cloudforge/core/internal/auth"
	"cloudforge/core/internal/databases"
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/zalando/go-keyring"
	"os"
	"runtime"
	"sync"
	"time"
)

// LocalProvider bridges local Core managers (supervisor, databases, cloudflare, auth)
// into the cluster subsystem without introducing import cycles.
type LocalProvider interface {
	LocalProjects() []SanitizedProject
	LocalDatabases(ctx context.Context) (databases.View, error)
	LocalTunnelHealth(ctx context.Context) TunnelHealthReport
	LocalAdminUsers() []RemoteUserSummary
	LocalPrimaryURL() string
	AuthenticateLocal(username, password, clientIP string) (userID, usernameOut string, authEpoch int64, statusCode int, err error)
	ExecuteLocalCommand(ctx context.Context, cmd ExecCommandReq) (json.RawMessage, int, error)
	UnpublishLocalPanel(ctx context.Context) (string, error)
}

type SessionAccess struct {
	Session            auth.Session
	IssuerNodeID       string
	IssuerNodeName     string
	Scope              string
	CanManageComputers bool
}

type storedStepUp struct {
	grant     StepUpGrant
	expiresAt time.Time
}

type peerConn struct {
	nodeID        string
	fingerprint   string
	publicKey     *ecdsa.PublicKey
	conn          *tls.Conn
	writeMu       sync.Mutex
	mu            sync.Mutex
	lastHeartbeat time.Time
	pending       map[string]chan Envelope
	closed        chan struct{}
	closeOnce     sync.Once
	approved      bool
	approveCh     chan PairApprovedMsg
	rejectCh      chan PairRejectedMsg
}

func newPeerConn(nodeID, fp string, pub *ecdsa.PublicKey, conn *tls.Conn, approved bool) *peerConn {
	return &peerConn{
		nodeID:        nodeID,
		fingerprint:   fp,
		publicKey:     pub,
		conn:          conn,
		lastHeartbeat: time.Now(),
		pending:       map[string]chan Envelope{},
		closed:        make(chan struct{}),
		approved:      approved,
		approveCh:     make(chan PairApprovedMsg, 1),
		rejectCh:      make(chan PairRejectedMsg, 1),
	}
}

func (p *peerConn) send(msgType, id, replyTo string, payload any) error {
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		raw = b
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	_ = p.conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	return writeFrame(p.conn, Envelope{
		Version: ProtocolVersion,
		Type:    msgType,
		ID:      id,
		ReplyTo: replyTo,
		Payload: raw,
	})
}

func (p *peerConn) close() {
	p.closeOnce.Do(func() {
		close(p.closed)
		_ = p.conn.Close()
		p.mu.Lock()
		for id, ch := range p.pending {
			close(ch)
			delete(p.pending, id)
		}
		p.mu.Unlock()
	})
}

func (p *peerConn) touchHeartbeat() {
	p.mu.Lock()
	p.lastHeartbeat = time.Now()
	p.mu.Unlock()
}

func (p *peerConn) heartbeatAge() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return time.Since(p.lastHeartbeat)
}

func (p *peerConn) call(ctx context.Context, msgType string, payload any) (Envelope, error) {
	reqID := auth.ID()
	ch := make(chan Envelope, 1)
	p.mu.Lock()
	select {
	case <-p.closed:
		p.mu.Unlock()
		return Envelope{}, errors.New("conexão com o computador encerrada")
	default:
		p.pending[reqID] = ch
	}
	p.mu.Unlock()

	cleanup := func() {
		p.mu.Lock()
		delete(p.pending, reqID)
		p.mu.Unlock()
	}

	if err := p.send(msgType, reqID, "", payload); err != nil {
		cleanup()
		return Envelope{}, err
	}

	select {
	case env, ok := <-ch:
		cleanup()
		if !ok {
			return Envelope{}, errors.New("conexão perdida durante a operação")
		}
		return env, nil
	case <-ctx.Done():
		cleanup()
		return Envelope{}, ctx.Err()
	case <-p.closed:
		cleanup()
		return Envelope{}, errors.New("conexão perdida durante a operação")
	}
}

func (p *peerConn) deliverReply(env Envelope) bool {
	if env.ReplyTo == "" {
		return false
	}
	p.mu.Lock()
	ch, ok := p.pending[env.ReplyTo]
	if ok {
		delete(p.pending, env.ReplyTo)
	}
	p.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- env:
	default:
	}
	return true
}

// Manager coordinates standalone, primary, and secondary cluster roles.
type Manager struct {
	InstallationID string
	Auth           *auth.Auth
	Provider       LocalProvider
	Vault          KeyVault
	Development    bool
	AppVersion     string
	Dialer         *websocket.Dialer

	HeartbeatInterval         time.Duration
	HeartbeatTimeout          time.Duration
	InventoryInterval         time.Duration
	ReconnectBaseDelay        time.Duration
	ReconnectMaxDelay         time.Duration
	InviteTTL                 time.Duration
	StepUpTTL                 time.Duration
	MaxOriginUnverifiedWindow time.Duration
	OriginCheckInterval       time.Duration

	db           *sql.DB
	mu           sync.RWMutex
	pairMu       sync.Mutex
	identity     *Identity
	peers        map[string]*peerConn
	pendingPeers map[string]*peerConn
	connGate     chan struct{}
	pairGate     chan struct{}

	// Secondary outbound client state.
	clientMu         sync.Mutex
	clientCancel     context.CancelFunc
	clientDone       chan struct{}
	clientConn       *peerConn
	clientConnected  bool
	clientLastErr    string
	clientPrimaryPub *ecdsa.PublicKey
	pairSecret       string
	inventorySeq     uint64
	inventoryWake    chan struct{}

	// Step-up single-use grant stores (Y issues/consumes; X guards against replay).
	stepUpMu         sync.Mutex
	stepUpGrants     map[string]storedStepUp
	usedStepUpGrants map[string]time.Time

	rootCtx context.Context
}

func New(db *sql.DB, installationID string, authenticator *auth.Auth, vault KeyVault, development bool) *Manager {
	if vault == nil {
		vault = OSKeyVault{Installation: installationID}
	}
	return &Manager{
		InstallationID:            installationID,
		Auth:                      authenticator,
		Vault:                     vault,
		Development:               development,
		AppVersion:                "development",
		HeartbeatInterval:         15 * time.Second,
		HeartbeatTimeout:          45 * time.Second,
		InventoryInterval:         5 * time.Second,
		ReconnectBaseDelay:        1 * time.Second,
		ReconnectMaxDelay:         60 * time.Second,
		InviteTTL:                 10 * time.Minute,
		StepUpTTL:                 60 * time.Second,
		MaxOriginUnverifiedWindow: 15 * time.Minute,
		OriginCheckInterval:       60 * time.Second,
		db:                        db,
		peers:                     map[string]*peerConn{},
		pendingPeers:              map[string]*peerConn{},
		connGate:                  make(chan struct{}, 64),
		pairGate:                  make(chan struct{}, 4),
		inventoryWake:             make(chan struct{}, 1),
		stepUpGrants:              map[string]storedStepUp{},
		usedStepUpGrants:          map[string]time.Time{},
		rootCtx:                   context.Background(),
	}
}

func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	m.rootCtx = ctx
	m.mu.Unlock()

	st, err := m.loadState()
	if err != nil {
		return
	}
	if st.Role == RoleSecondary || (st.LinkStatus == LinkStatusPendingApproval && st.PrimaryURL != "") || st.LinkStatus == LinkStatusSyncing {
		m.startSecondaryLoop()
	}
}

func (m *Manager) Stop() {
	m.stopSecondaryLoop()
	m.mu.Lock()
	peers := make([]*peerConn, 0, len(m.peers)+len(m.pendingPeers))
	for _, p := range m.peers {
		peers = append(peers, p)
	}
	for _, p := range m.pendingPeers {
		peers = append(peers, p)
	}
	m.peers = map[string]*peerConn{}
	m.pendingPeers = map[string]*peerConn{}
	m.mu.Unlock()
	for _, p := range peers {
		p.close()
	}
}

func (m *Manager) State() LocalState {
	st, err := m.loadState()
	if err != nil {
		return LocalState{Role: RoleStandalone, DisplayName: defaultDisplayName(), DelegatedOps: DefaultDelegatedOps}
	}
	return st
}

func (m *Manager) Role() string {
	return m.State().Role
}

func (m *Manager) ensureIdentity(rotate bool) (*Identity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !rotate && m.identity != nil {
		return m.identity, nil
	}
	if !rotate {
		privPEM, err := m.Vault.Get(privateKeyVaultKey)
		if err == nil && privPEM != "" {
			id, err := loadIdentity(m.InstallationID, privPEM)
			if err == nil {
				m.identity = id
				st, _ := m.loadState()
				if st.PublicKeyFP != id.Fingerprint || st.PublicKeyPEM != id.PublicKeyPEM {
					st.PublicKeyPEM = id.PublicKeyPEM
					st.PublicKeyFP = id.Fingerprint
					_ = m.saveState(st)
				}
				return id, nil
			}
		} else if err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return nil, fmt.Errorf("armazenamento seguro indisponível para chave da instalação: %w", err)
		}
	}
	id, privPEM, err := generateIdentity(m.InstallationID)
	if err != nil {
		return nil, err
	}
	if err := m.Vault.Set(privateKeyVaultKey, privPEM); err != nil {
		return nil, fmt.Errorf("não foi possível proteger a chave privada no armazenamento seguro do sistema: %w", err)
	}
	m.identity = id
	st, err := m.loadState()
	if err != nil {
		return nil, err
	}
	st.PublicKeyPEM = id.PublicKeyPEM
	st.PublicKeyFP = id.Fingerprint
	if err := m.saveState(st); err != nil {
		return nil, err
	}
	return id, nil
}

func (m *Manager) UpdateLocalProfile(displayName string) (LocalState, error) {
	clean, err := sanitizeDisplayName(displayName)
	if err != nil {
		return LocalState{}, err
	}
	st, err := m.loadState()
	if err != nil {
		return LocalState{}, err
	}
	st.DisplayName = clean
	if err := m.saveState(st); err != nil {
		return LocalState{}, err
	}
	return st, nil
}

func (m *Manager) EnablePrimary() (LocalState, error) {
	st, err := m.loadState()
	if err != nil {
		return LocalState{}, err
	}
	if st.Role == RoleSecondary {
		return st, errors.New("este computador está vinculado como secundário; remova o vínculo antes de torná-lo principal")
	}
	if m.Provider != nil && m.Provider.LocalPrimaryURL() == "" && !m.Development {
		return st, errors.New("configure o domínio público do painel na seção Cloudflare antes de ativar o modo principal")
	}
	if _, err := m.ensureIdentity(false); err != nil {
		return st, err
	}
	st, err = m.loadState()
	if err != nil {
		return st, err
	}
	st.Role = RolePrimary
	st.LinkStatus = LinkStatusNone
	if err := m.saveState(st); err != nil {
		return st, err
	}
	return st, nil
}

func (m *Manager) Summary() map[string]any {
	st := m.State()
	m.clientMu.Lock()
	clientConnected := m.clientConnected
	clientErr := m.clientLastErr
	m.clientMu.Unlock()

	pendingCount := 0
	approvedCount := 0
	onlineCount := 0
	if st.Role == RolePrimary {
		if nodes, err := m.listNodes(""); err == nil {
			for _, n := range nodes {
				if n.Status == NodeStatusPending {
					pendingCount++
				} else if n.Status == NodeStatusApproved {
					approvedCount++
					if m.NodeOnline(n.ID) {
						onlineCount++
					}
				}
			}
		}
	}
	return map[string]any{
		"installationId":   m.InstallationID,
		"role":             st.Role,
		"displayName":      st.DisplayName,
		"accessSlug":       st.AccessSlug,
		"primaryUrl":       st.PrimaryURL,
		"primaryId":        st.PrimaryID,
		"primaryName":      st.PrimaryName,
		"linkStatus":       st.LinkStatus,
		"verificationCode": st.VerificationCode,
		"linkedAt":         st.LinkedAt,
		"channelOnline":    clientConnected,
		"channelError":     clientErr,
		"pendingCount":     pendingCount,
		"computersCount":   approvedCount,
		"onlineCount":      onlineCount,
	}
}

func (m *Manager) TriggerInventorySync() {
	select {
	case m.inventoryWake <- struct{}{}:
	default:
	}
}

func (m *Manager) NotifyLocalPasswordChanged(userID string, newEpoch int64) {
	// Secondary pushes immediate revocation notice to Primary over mTLS.
	m.clientMu.Lock()
	conn := m.clientConn
	m.clientMu.Unlock()
	if conn != nil {
		_ = conn.send(MsgAuthRevoked, auth.ID(), "", AuthRevokedMsg{
			UserID:    userID,
			AuthEpoch: newEpoch,
			Reason:    "password_changed",
		})
	}
	m.TriggerInventorySync()
}

func (m *Manager) NotifyLocalUsernameChanged(userID, username string, newEpoch int64) {
	// Secondary pushes immediate revocation and username update to Primary over mTLS.
	m.clientMu.Lock()
	conn := m.clientConn
	m.clientMu.Unlock()
	if conn != nil {
		_ = conn.send(MsgAuthRevoked, auth.ID(), "", AuthRevokedMsg{
			UserID:    userID,
			Username:  username,
			AuthEpoch: newEpoch,
			Reason:    "username_changed",
		})
	}
	m.TriggerInventorySync()
}

func (m *Manager) hostnameOSArch() (string, string, string) {
	host, _ := os.Hostname()
	if host == "" {
		host = "unknown"
	}
	return host, runtime.GOOS, runtime.GOARCH
}
