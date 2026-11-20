package cluster

import (
	"cloudforge/core/internal/auth"
	"cloudforge/core/internal/databases"
	"cloudforge/core/internal/persistence"
	"context"
	"encoding/json"
	"errors"
	"github.com/zalando/go-keyring"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memoryKeyVault struct {
	mu     sync.Mutex
	values map[string]string
}

func newMemoryKeyVault() *memoryKeyVault {
	return &memoryKeyVault{values: map[string]string{}}
}

func (v *memoryKeyVault) Get(key string) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	val, ok := v.values[key]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return val, nil
}
func (v *memoryKeyVault) Set(key, val string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.values[key] = val
	return nil
}
func (v *memoryKeyVault) Delete(key string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.values, key)
	return nil
}

type fakeProvider struct {
	mu          sync.Mutex
	primaryURL  string
	projects    []SanitizedProject
	dbs         databases.View
	tunnel      TunnelHealthReport
	users       []RemoteUserSummary
	authFn      func(user, pass, ip string) (string, string, int64, int, error)
	execFn      func(ctx context.Context, cmd ExecCommandReq) (json.RawMessage, int, error)
	unpublished atomic.Int32
	execCalls   atomic.Int32
}

func (f *fakeProvider) LocalProjects() []SanitizedProject {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]SanitizedProject{}, f.projects...)
}
func (f *fakeProvider) LocalDatabases(ctx context.Context) (databases.View, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dbs, nil
}
func (f *fakeProvider) LocalTunnelHealth(ctx context.Context) TunnelHealthReport {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tunnel
}
func (f *fakeProvider) LocalAdminUsers() []RemoteUserSummary {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]RemoteUserSummary{}, f.users...)
}
func (f *fakeProvider) LocalPrimaryURL() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.primaryURL
}
func (f *fakeProvider) AuthenticateLocal(u, p, ip string) (string, string, int64, int, error) {
	if f.authFn != nil {
		return f.authFn(u, p, ip)
	}
	return "", "", 0, http.StatusUnauthorized, errors.New("usuário ou senha inválidos")
}
func (f *fakeProvider) ExecuteLocalCommand(ctx context.Context, cmd ExecCommandReq) (json.RawMessage, int, error) {
	f.execCalls.Add(1)
	if f.execFn != nil {
		return f.execFn(ctx, cmd)
	}
	return json.RawMessage(`{"ok":true}`), http.StatusOK, nil
}
func (f *fakeProvider) UnpublishLocalPanel(ctx context.Context) (string, error) {
	f.unpublished.Add(1)
	return "", nil
}

type nodeFixture struct {
	store    *persistence.Store
	auth     *auth.Auth
	vault    *memoryKeyVault
	provider *fakeProvider
	mgr      *Manager
	server   *httptest.Server
	dbPath   string
}

func newNodeFixture(t *testing.T, id, displayName, adminUser, adminPass string) *nodeFixture {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), id+".db")
	store, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	a := auth.New(store)
	if err := a.Bootstrap(adminUser, adminPass); err != nil {
		t.Fatal(err)
	}
	admin, err := store.Admin()
	if err != nil {
		t.Fatal(err)
	}
	vault := newMemoryKeyVault()
	mgr := New(store.DB, id, a, vault, true)
	mgr.HeartbeatInterval = 120 * time.Millisecond
	mgr.HeartbeatTimeout = 450 * time.Millisecond
	mgr.InventoryInterval = 200 * time.Millisecond
	mgr.ReconnectBaseDelay = 60 * time.Millisecond
	mgr.ReconnectMaxDelay = 200 * time.Millisecond
	_, _ = mgr.UpdateLocalProfile(displayName)

	prov := &fakeProvider{
		tunnel: TunnelHealthReport{Configured: true, Healthy: true, Status: "connected"},
		users:  []RemoteUserSummary{{UserID: admin.ID, Username: admin.Username, AuthEpoch: admin.AuthEpoch}},
		authFn: func(u, p, ip string) (string, string, int64, int, error) {
			user, err := a.Authenticate(u, p)
			if err != nil {
				return "", "", 0, http.StatusUnauthorized, err
			}
			return user.ID, user.Username, user.AuthEpoch, http.StatusOK, nil
		},
	}
	mgr.Provider = prov

	mux := http.NewServeMux()
	mux.HandleFunc("/api/cluster/ws", mgr.ServeWS)
	srv := httptest.NewServer(mux)
	prov.primaryURL = srv.URL

	t.Cleanup(func() {
		mgr.Stop()
		srv.Close()
		_ = store.Close()
	})
	return &nodeFixture{store: store, auth: a, vault: vault, provider: prov, mgr: mgr, server: srv, dbPath: dbPath}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", msg)
}

func TestPairingApprovalInventoryUnpublishRestartAndOfflineRemoval(t *testing.T) {
	y := newNodeFixture(t, "node-y", "Notebook Y", "admin", "password-notebook-y-123")
	x := newNodeFixture(t, "node-x", "Notebook X", "admin", "password-notebook-x-456")

	x.provider.projects = []SanitizedProject{{ID: "shared-id", Name: "Projeto X", Runtime: "node", Execution: SanitizedExecution{ActualState: "online"}}}
	y.provider.projects = []SanitizedProject{{ID: "shared-id", Name: "Projeto Y", Runtime: "python", Execution: SanitizedExecution{ActualState: "offline"}}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	y.mgr.Start(ctx)
	x.mgr.Start(ctx)

	adminY, _ := y.store.Admin()
	inv, err := y.mgr.CreateInvite(adminY.ID, "")
	if err != nil {
		t.Fatalf("CreateInvite failed: %v", err)
	}
	// Verify plaintext invite secret is NOT stored in SQLite.
	decoded, err := DecodeInvite(inv.Invite, true)
	if err != nil {
		t.Fatal(err)
	}
	var storedHash string
	if err := y.store.DB.QueryRow("SELECT secret_hash FROM cluster_invites WHERE id=?", inv.InviteID).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash == decoded.Secret || strings.Contains(storedHash, decoded.Secret) {
		t.Fatal("plaintext invite secret was persisted in database")
	}

	// X links using invite -> must enter pending state with matching verification code, panel NOT unpublished yet!
	linkRes, err := x.mgr.StartLink(ctx, inv.Invite, "Notebook X")
	if err != nil {
		t.Fatalf("StartLink failed: %v", err)
	}
	if linkRes.Status != LinkStatusPendingApproval || linkRes.VerificationCode == "" {
		t.Fatalf("unexpected link result: %+v", linkRes)
	}
	if x.provider.unpublished.Load() != 0 {
		t.Fatal("secondary panel was unpublished BEFORE approval and inventory confirmation")
	}

	// Verify Y shows pending request with identical verification code and has NO approved inventory yet.
	snap := y.mgr.ComputersSnapshot(ctx)
	pending := snap["pending"].([]ComputerView)
	if len(pending) != 1 || pending[0].VerificationCode != linkRes.VerificationCode {
		t.Fatalf("verification code mismatch on Y: pending=%+v want=%s", pending, linkRes.VerificationCode)
	}
	if y.mgr.NodeOnline("node-x") {
		t.Fatal("pending node must not be considered online")
	}

	// Concurrent approval race: fire 5 concurrent approvals; STRICTLY 1 must succeed!
	actorY := Actor{IssuerNodeID: y.mgr.InstallationID, UserID: adminY.ID, Username: adminY.Username}
	var wg sync.WaitGroup
	var okCount atomic.Int32
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := y.mgr.ApproveNode("node-x", actorY); err == nil {
				okCount.Add(1)
			}
		}()
	}
	wg.Wait()
	if okCount.Load() != 1 {
		t.Fatalf("concurrent approval race: expected 1 success, got %d", okCount.Load())
	}

	// Wait for X to establish mTLS session, sync inventory, receive inventory_ack, transition to secondary, and unpublish panel!
	waitFor(t, 5*time.Second, func() bool {
		return y.mgr.NodeOnline("node-x") && x.mgr.Role() == RoleSecondary && x.provider.unpublished.Load() == 1
	}, "X linked, online, and panel unpublished after inventory confirmation")

	// Verify Y has X's inventory and shows separate tunnel health.
	accessY := SessionAccess{IssuerNodeID: y.mgr.InstallationID, Scope: ScopeUnified, CanManageComputers: true}
	projects := y.mgr.AggregatedProjects(accessY, "")
	if len(projects) != 2 {
		t.Fatalf("expected 2 aggregated projects (Y + X), got %+v", projects)
	}

	// Reusing the consumed invite from another node MUST fail.
	z := newNodeFixture(t, "node-z", "Notebook Z", "admin", "password-notebook-z-789")
	if _, err := z.mgr.StartLink(ctx, inv.Invite, "Notebook Z"); err == nil {
		t.Fatal("reused invite was accepted")
	}

	// Simulate Core restart on X: stop X's manager, create a brand-new Manager on the same DB & KeyVault -> must reconnect automatically!
	x.mgr.Stop()
	waitFor(t, 3*time.Second, func() bool { return !y.mgr.NodeOnline("node-x") }, "Y detects X offline after stop")

	// Offline X inventory must remain visible with last known state and NodeOnline == false!
	offlineProjects := y.mgr.AggregatedProjects(accessY, "node-x")
	if len(offlineProjects) != 1 || offlineProjects[0].NodeOnline || offlineProjects[0].NodeLastSeenAt == 0 {
		t.Fatalf("offline inventory lost or marked online: %+v", offlineProjects)
	}
	// Remote action while X is offline must be blocked!
	if _, code, err := y.mgr.DispatchCommand(ctx, accessY, "node-x", ExecCommandReq{OperationID: "op-offline", Kind: CmdProjectAction, ResourceID: "shared-id"}); err == nil || code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 offline block, got code=%d err=%v", code, err)
	}

	// Restart X Core -> must auto-reconnect over mTLS using stored OS vault key & update inventory!
	xRestarted := New(x.store.DB, "node-x", x.auth, x.vault, true)
	xRestarted.HeartbeatInterval = 120 * time.Millisecond
	xRestarted.HeartbeatTimeout = 450 * time.Millisecond
	xRestarted.InventoryInterval = 200 * time.Millisecond
	xRestarted.ReconnectBaseDelay = 60 * time.Millisecond
	xRestarted.ReconnectMaxDelay = 200 * time.Millisecond
	x.provider.projects = append(x.provider.projects, SanitizedProject{ID: "proj-x-2", Name: "Worker X", Runtime: "python"})
	xRestarted.Provider = x.provider
	xRestarted.Start(ctx)
	defer xRestarted.Stop()

	waitFor(t, 5*time.Second, func() bool {
		return y.mgr.NodeOnline("node-x") && len(y.mgr.AggregatedProjects(accessY, "node-x")) == 2
	}, "restarted X auto-reconnected and refreshed inventory")

	// Issue a federated session for X's user on Y, then take X OFFLINE and remove X!
	adminX, _ := x.store.Admin()
	fedToken, _, err := y.auth.IssueFederated("node-x", adminX.ID, adminX.Username, adminX.AuthEpoch, "test-browser")
	if err != nil {
		t.Fatal(err)
	}
	xRestarted.Stop()
	waitFor(t, 3*time.Second, func() bool { return !y.mgr.NodeOnline("node-x") }, "X offline before removal")

	// Remove X while X is OFFLINE!
	if err := y.mgr.RemoveNode("node-x", actorY); err != nil {
		t.Fatalf("RemoveNode while offline failed: %v", err)
	}
	// Federated session from X MUST be immediately revoked!
	if _, err := y.auth.Session(fedToken); err == nil {
		t.Fatal("federated session survived node removal")
	}
	// Active computer list and inventory must exclude X, while audit & revocation remain!
	if projs := y.mgr.AggregatedProjects(accessY, "node-x"); len(projs) != 0 {
		t.Fatalf("removed node resources still active: %+v", projs)
	}
	audit, err := y.mgr.ListAudit(20)
	if err != nil || len(audit) < 2 {
		t.Fatalf("audit logs not preserved: %+v %v", audit, err)
	}

	// Trying to reconnect with the OLD key from X must be permanently rejected and transition X to revoked!
	xOldKey := New(x.store.DB, "node-x", x.auth, x.vault, true)
	xOldKey.HeartbeatInterval = 100 * time.Millisecond
	xOldKey.HeartbeatTimeout = 350 * time.Millisecond
	xOldKey.ReconnectBaseDelay = 50 * time.Millisecond
	xOldKey.Provider = x.provider
	xOldKey.Start(ctx)
	defer xOldKey.Stop()

	waitFor(t, 4*time.Second, func() bool {
		return xOldKey.State().LinkStatus == LinkStatusRevoked
	}, "old revoked key rejected permanently on reconnect")
	if y.mgr.NodeOnline("node-x") {
		t.Fatal("revoked node reconnected with old key")
	}

	// Re-pairing X with a new invite MUST rotate X's keypair and succeed cleanly!
	inv2, err := y.mgr.CreateInvite(adminY.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := xOldKey.StartLink(ctx, inv2.Invite, "Notebook X Novo"); err != nil {
		t.Fatalf("new pairing after revocation failed: %v", err)
	}
	if _, err := y.mgr.ApproveNode("node-x", actorY); err != nil {
		t.Fatalf("approve after re-pairing failed: %v", err)
	}
	waitFor(t, 4*time.Second, func() bool { return y.mgr.NodeOnline("node-x") }, "re-paired X online with fresh key")
}

func TestExpiredInviteAndAttemptLimiting(t *testing.T) {
	y := newNodeFixture(t, "node-y-exp", "Notebook Y", "admin", "password-notebook-y-123")
	x := newNodeFixture(t, "node-x-exp", "Notebook X", "admin", "password-notebook-x-456")
	adminY, _ := y.store.Admin()

	// 1. Expired invite test.
	y.mgr.InviteTTL = 1 * time.Second
	expInv, err := y.mgr.CreateInvite(adminY.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	// Backdate expiry in DB to expire immediately.
	_, _ = y.store.DB.Exec("UPDATE cluster_invites SET expires_at=? WHERE id=?", time.Now().Unix()-10, expInv.InviteID)
	// Also forge client payload with future expiresAt to confirm SERVER rejects expired invite!
	p, _ := DecodeInvite(expInv.Invite, true)
	p.ExpiresAt = time.Now().Unix() + 300
	forgedToken, _ := EncodeInvite(p)
	if _, err := x.mgr.StartLink(context.Background(), forgedToken, "Notebook X"); err == nil || !strings.Contains(err.Error(), "expirado") {
		t.Fatalf("expected server-side expired invite rejection, got %v", err)
	}

	// 2. Wrong secret attempt limit (5 failed attempts lock invite permanently, even with valid secret on 6th!).
	y.mgr.InviteTTL = 5 * time.Minute
	inv, err := y.mgr.CreateInvite(adminY.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	goodPayload, _ := DecodeInvite(inv.Invite, true)
	badPayload := goodPayload
	badPayload.Secret = strings.Repeat("A", 43)
	badToken, _ := EncodeInvite(badPayload)

	for i := range 5 {
		if _, err := x.mgr.StartLink(context.Background(), badToken, "Notebook X"); err == nil {
			t.Fatalf("attempt %d with wrong secret succeeded", i+1)
		}
	}
	// 6th attempt with the REAL secret must be rejected because invite is locked!
	if _, err := x.mgr.StartLink(context.Background(), inv.Invite, "Notebook X"); err == nil || !strings.Contains(err.Error(), "limite") {
		t.Fatalf("locked invite accepted valid secret: %v", err)
	}
}

func TestOperationIdempotencyPreventsDuplicateExecution(t *testing.T) {
	y := newNodeFixture(t, "node-y-idem", "Notebook Y", "admin", "password-notebook-y-123")
	x := newNodeFixture(t, "node-x-idem", "Notebook X", "admin", "password-notebook-x-456")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	y.mgr.Start(ctx)
	x.mgr.Start(ctx)

	adminY, _ := y.store.Admin()
	inv, _ := y.mgr.CreateInvite(adminY.ID, "")
	if _, err := x.mgr.StartLink(ctx, inv.Invite, "Notebook X"); err != nil {
		t.Fatal(err)
	}
	actorY := Actor{IssuerNodeID: y.mgr.InstallationID, UserID: adminY.ID, Username: adminY.Username}
	if _, err := y.mgr.ApproveNode("node-x-idem", actorY); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 4*time.Second, func() bool { return y.mgr.NodeOnline("node-x-idem") && x.mgr.Role() == RoleSecondary }, "linked")

	accessY := SessionAccess{
		Session:            auth.Session{UserID: adminY.ID, Username: adminY.Username},
		IssuerNodeID:       y.mgr.InstallationID,
		Scope:              ScopeUnified,
		CanManageComputers: true,
	}

	// Same operationId dispatched twice must execute ONLY ONCE on X!
	cmd := ExecCommandReq{OperationID: "op-restart-once", Kind: CmdProjectAction, ResourceID: "p1", Payload: json.RawMessage(`{"action":"restart"}`)}
	if _, code, err := y.mgr.DispatchCommand(ctx, accessY, "node-x-idem", cmd); err != nil || code != http.StatusOK {
		t.Fatalf("first dispatch failed: code=%d err=%v", code, err)
	}
	if _, code, err := y.mgr.DispatchCommand(ctx, accessY, "node-x-idem", cmd); err != nil || code != http.StatusOK {
		t.Fatalf("idempotent replay failed: code=%d err=%v", code, err)
	}
	if got := x.provider.execCalls.Load(); got != 1 {
		t.Fatalf("duplicate operationId executed %d times on X, expected 1", got)
	}

	// Simulate mid-operation disconnect: X hangs until connection drops, Y marks op "unknown" and refuses replay!
	started := make(chan struct{})
	x.provider.execFn = func(execCtx context.Context, c ExecCommandReq) (json.RawMessage, int, error) {
		close(started)
		<-execCtx.Done()
		return nil, http.StatusRequestTimeout, execCtx.Err()
	}
	doneErr := make(chan error, 1)
	go func() {
		_, _, err := y.mgr.DispatchCommand(ctx, accessY, "node-x-idem", ExecCommandReq{
			OperationID: "op-mid-disconnect",
			Kind:        CmdProjectAction,
			ResourceID:  "p1",
			Payload:     json.RawMessage(`{"action":"restart"}`),
		})
		doneErr <- err
	}()
	<-started
	// Force-close X's connection mid-flight.
	y.mgr.mu.RLock()
	p := y.mgr.peers["node-x-idem"]
	y.mgr.mu.RUnlock()
	if p != nil {
		p.close()
	}
	if err := <-doneErr; err == nil {
		t.Fatal("expected ambiguous error on mid-operation disconnect")
	}
	// Restore normal execution on X and wait for reconnect.
	x.provider.execFn = nil
	waitFor(t, 4*time.Second, func() bool { return y.mgr.NodeOnline("node-x-idem") }, "X reconnected after mid-op drop")
	beforeReplay := x.provider.execCalls.Load()
	// Retrying with the SAME operationId after reconnect MUST be blocked because outcome is unknown!
	if _, code, err := y.mgr.DispatchCommand(ctx, accessY, "node-x-idem", ExecCommandReq{
		OperationID: "op-mid-disconnect",
		Kind:        CmdProjectAction,
		ResourceID:  "p1",
		Payload:     json.RawMessage(`{"action":"restart"}`),
	}); err == nil || code != http.StatusConflict {
		t.Fatalf("expected 409 conflict blocking replay of unknown-outcome operation, got code=%d err=%v", code, err)
	}
	if x.provider.execCalls.Load() != beforeReplay {
		t.Fatal("unknown-outcome operation was re-executed on X after reconnect")
	}
}

func TestSecondaryUsernameChangePropagatesToPrimary(t *testing.T) {
	y := newNodeFixture(t, "node-y-user", "Notebook Y", "adminy", "password-notebook-y-123")
	x := newNodeFixture(t, "node-x-user", "Notebook X", "adminx", "password-notebook-x-456")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	y.mgr.Start(ctx)
	defer y.mgr.Stop()
	x.mgr.Start(ctx)
	defer x.mgr.Stop()

	adminY, _ := y.store.Admin()
	actorY := Actor{IssuerNodeID: y.mgr.InstallationID, UserID: adminY.ID, Username: adminY.Username}
	inv, err := y.mgr.CreateInvite(adminY.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := x.mgr.StartLink(ctx, inv.Invite, "Notebook X"); err != nil {
		t.Fatal(err)
	}
	if _, err := y.mgr.ApproveNode("node-x-user", actorY); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 4*time.Second, func() bool { return y.mgr.NodeOnline("node-x-user") }, "X online on Y")

	adminX, _ := x.store.Admin()
	if _, err := y.mgr.SetPrincipalGrant("node-x-user", adminX.ID, adminX.Username, ScopeUnified, actorY); err != nil {
		t.Fatal(err)
	}
	fedToken, _, err := y.auth.IssueFederated("node-x-user", adminX.ID, adminX.Username, adminX.AuthEpoch, "test-browser")
	if err != nil {
		t.Fatal(err)
	}

	// Change username on secondary X and notify primary Y.
	uid, nextUsername, newEpoch, err := x.auth.ChangeUsernameEpoch("password-notebook-x-456", "novox")
	if err != nil {
		t.Fatal(err)
	}
	x.provider.mu.Lock()
	x.provider.users = []RemoteUserSummary{{UserID: uid, Username: nextUsername, AuthEpoch: newEpoch}}
	x.provider.mu.Unlock()
	x.mgr.NotifyLocalUsernameChanged(uid, nextUsername, newEpoch)

	// Primary Y must revoke the old federated session and preserve the unified grant with the new username.
	waitFor(t, 4*time.Second, func() bool {
		_, err := y.auth.Session(fedToken)
		if err == nil {
			return false
		}
		grant, gerr := y.mgr.GetPrincipalGrant("node-x-user", uid)
		return gerr == nil && grant.Username == "novox" && grant.Scope == ScopeUnified
	}, "primary revoked federated session and updated grant username")
}
