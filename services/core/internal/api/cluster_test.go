package api

import (
	"bytes"
	"cloudforge/core/internal/cloudflare"
	"cloudforge/core/internal/cluster"
	"cloudforge/core/internal/databases"
	"cloudforge/core/internal/model"
	"cloudforge/core/internal/persistence"
	"cloudforge/core/internal/routing"
	"cloudforge/core/internal/supervisor"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type integratedNode struct {
	id         string
	store      *persistence.Store
	sup        *supervisor.Supervisor
	cloud      *cloudflare.Manager
	server     *Server
	router     *routing.Router
	httpServer *httptest.Server
}

func newIntegratedNode(t *testing.T, id, displayName, adminHost, adminPass string) *integratedNode {
	t.Helper()
	dir := t.TempDir()
	store, err := persistence.Open(filepath.Join(dir, "cloudforge.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("installationId", id); err != nil {
		t.Fatal(err)
	}
	sup := supervisor.New(store, dir)
	cf := cloudflare.New(store, dir, "http://127.0.0.1:28932", id)
	key := "local-key-" + id + "-0123456789abcdef"
	srv := New(store, sup, cf, key, dir, "", true)
	srv.Databases = databases.New(store, key)
	srv.Cluster.Vault = &testEnvironmentVault{values: map[string]string{}}
	srv.Cluster.HeartbeatInterval = 120 * time.Millisecond
	srv.Cluster.HeartbeatTimeout = 450 * time.Millisecond
	srv.Cluster.InventoryInterval = 200 * time.Millisecond
	srv.Cluster.ReconnectBaseDelay = 60 * time.Millisecond
	srv.Cluster.ReconnectMaxDelay = 200 * time.Millisecond
	srv.Cluster.OriginCheckInterval = 200 * time.Millisecond
	srv.Cluster.MaxOriginUnverifiedWindow = 2 * time.Second
	if _, err := srv.Cluster.UpdateLocalProfile(displayName); err != nil {
		t.Fatal(err)
	}
	if err := srv.Auth.Bootstrap("admin", adminPass); err != nil {
		t.Fatal(err)
	}

	router := &routing.Router{
		Store:       store,
		Cloud:       cf,
		Supervisor:  sup,
		Dashboard:   http.HandlerFunc(srv.Public),
		Development: true,
	}
	httpSrv := httptest.NewServer(router)
	srv.PrimaryURLOverride = httpSrv.URL

	// Configure a local panel config on the node so we can verify unpublishing on X.
	if adminHost != "" {
		cfg := model.CloudConfig{
			Mode:      "local",
			AccountID: "0123456789abcdef0123456789abcdef",
			ZoneID:    "abcdef0123456789abcdef0123456789",
			Domain:    "example.com",
			AdminHost: adminHost,
			TunnelID:  "12345678-1234-4234-8234-123456789abc",
			Name:      "cloudforge-" + id,
		}
		_ = store.SaveCloudConfig(cfg)
		// Reload manager config from store.
		cfReloaded := cloudflare.New(store, dir, "http://127.0.0.1:28932", id)
		srv.Cloud = cfReloaded
		router.Cloud = cfReloaded
		cf = cfReloaded
	}

	t.Cleanup(func() {
		srv.Cluster.Stop()
		httpSrv.Close()
		cf.Stop()
		sup.Shutdown()
		_ = store.Close()
	})
	return &integratedNode{id: id, store: store, sup: sup, cloud: cf, server: srv, router: router, httpServer: httpSrv}
}

func seedTestDatabase(t *testing.T, node *integratedNode, dbID, name, dbPassword string) {
	t.Helper()
	admin, err := node.store.Admin()
	if err != nil {
		t.Fatal(err)
	}
	// Build a valid test CA + client cert signed by that CA so Certificate("key"/"crt"/"pem") validates cleanly without Docker.
	caPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Cloudforge Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caPriv.PublicKey, caPriv)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	clientPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "cloudforge-db-" + dbID + "-client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	clientDER, err := x509.CreateCertificate(rand.Reader, clientTmpl, caCert, &clientPriv.PublicKey, caPriv)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(clientPriv)
	if err != nil {
		t.Fatal(err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})

	serverPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "cloudforge-server"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTmpl, caCert, &serverPriv.PublicKey, caPriv)
	if err != nil {
		t.Fatal(err)
	}
	serverPKCS8, err := x509.MarshalPKCS8PrivateKey(serverPriv)
	if err != nil {
		t.Fatal(err)
	}
	serverPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER})
	serverKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: serverPKCS8})

	secretPayload := map[string]any{
		"Admin":    "admin-pass",
		"Password": dbPassword,
		"Reveal":   false,
		"TLS": map[string]any{
			"PEM": serverPEM,
			"Key": serverKeyPEM,
			"CA":  caPEM,
			"Client": map[string]any{
				"PEM": certPEM,
				"Key": keyPEM,
			},
		},
	}
	secretJSON, err := json.Marshal(secretPayload)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte(node.server.Key))
	_, _ = mac.Write([]byte("cloudforge-db-v1\x00"))
	block, err := aes.NewCipher(mac.Sum(nil))
	if err != nil {
		t.Fatal(err)
	}
	box, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, box.NonceSize())
	encrypted := append([]byte("cloudforge-db-v1\x00"), box.Seal(nonce, nonce, secretJSON, []byte(dbID))...)
	data := `{"View":{"id":"` + dbID + `","name":"` + name + `","engine":"postgresql","host":"127.0.0.1","port":15432,"state":"stopped","tlsValidated":true,"credentialsPending":false},"Desired":"stopped","Action":""}`
	if _, err := node.store.DB.Exec("INSERT INTO hosted_databases(id,owner,engine,port,data,secret) VALUES(?,?,?,?,?,?)", dbID, admin.ID, "postgresql", 15432, data, encrypted); err != nil {
		t.Fatal(err)
	}
}

func waitUntil(t *testing.T, timeout time.Duration, fn func() bool, desc string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", desc)
}

func publicRequest(t *testing.T, srv *Server, method, path, body, csrf string, cookies []*http.Cookie, extraHeaders map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if method != http.MethodGet {
		r.Header.Set("Origin", "http://127.0.0.1")
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	for k, v := range extraHeaders {
		r.Header.Set(k, v)
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	srv.Public(w, r)
	return w
}

func TestTwoNodeUnifiedPanelFederatedLoginScopingStepUpAndRemoval(t *testing.T) {
	y := newIntegratedNode(t, "node-y", "Notebook Y", "painel-y.example.com", "secret-pass-notebook-Y")
	x := newIntegratedNode(t, "node-x", "Notebook X", "painel-x.example.com", "secret-pass-notebook-X")

	// Seed projects with the EXACT SAME ID on both Y and X to verify strict routing isolation!
	projY := model.Project{ID: "same-project-id", Name: "Projeto Y", Runtime: "python", Path: t.TempDir(), DesiredState: "stopped", Port: 4100}
	projX := model.Project{ID: "same-project-id", Name: "Projeto X", Runtime: "node", Path: t.TempDir(), DesiredState: "stopped", Port: 4200}
	if err := y.store.SaveProject(projY); err != nil {
		t.Fatal(err)
	}
	y.sup.Add(projY)
	if err := x.store.SaveProject(projX); err != nil {
		t.Fatal(err)
	}
	x.sup.Add(projX)
	// Publish an HTTP route for projX on X so we can verify it survives unpublishing X's panel AND removal!
	if err := x.store.SaveRoute(model.Route{ID: "route-x", ProjectID: projX.ID, ZoneID: "abcdef0123456789abcdef0123456789", Hostname: "app-x.example.com", Port: 4200}); err != nil {
		t.Fatal(err)
	}

	// Seed databases with the EXACT SAME ID on both Y and X with distinct passwords!
	sharedDBID := strings.Repeat("a", 32)
	seedTestDatabase(t, y, sharedDBID, "DB Notebook Y", "db-password-ONLY-on-Y")
	seedTestDatabase(t, x, sharedDBID, "DB Notebook X", "db-password-ONLY-on-X")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	y.server.Cluster.Start(ctx)
	x.server.Cluster.Start(ctx)

	// 1. Desktop of Y: generate invite (requires admin reconfirmation!).
	badInvite := invoke(y.server, true, "POST", "/api/cluster/invites", `{"username":"admin","password":"wrong-password"}`, "", "", nil)
	if badInvite.Code != http.StatusBadRequest {
		t.Fatalf("invite without valid admin password should fail: %d", badInvite.Code)
	}
	invResp := invoke(y.server, true, "POST", "/api/cluster/invites", `{"username":"admin","password":"secret-pass-notebook-Y"}`, "", "", nil)
	if invResp.Code != http.StatusCreated {
		t.Fatalf("create invite failed: %d %s", invResp.Code, invResp.Body.String())
	}
	var inv struct {
		Invite   string `json:"invite"`
		InviteID string `json:"inviteId"`
	}
	_ = json.Unmarshal(invResp.Body.Bytes(), &inv)

	// 2. Desktop of X: link to Y.
	linkBody, _ := json.Marshal(map[string]any{"invite": inv.Invite, "displayName": "Notebook X", "confirmed": true})
	linkResp := invoke(x.server, true, "POST", "/api/cluster/link", string(linkBody), "", "", nil)
	if linkResp.Code != http.StatusAccepted {
		t.Fatalf("link on X failed: %d %s", linkResp.Code, linkResp.Body.String())
	}

	// 3. Desktop of Y: approve X.
	approveResp := invoke(y.server, true, "POST", "/api/cluster/nodes/node-x/approve", `{}`, "", "", nil)
	if approveResp.Code != http.StatusOK {
		t.Fatalf("approve on Y failed: %d %s", approveResp.Code, approveResp.Body.String())
	}

	// Wait until X is online, inventory confirmed, role=secondary, and X's panel unpublished!
	waitUntil(t, 5*time.Second, func() bool {
		return y.server.Cluster.NodeOnline("node-x") && x.server.Cluster.Role() == cluster.RoleSecondary && x.cloud.Config().PanelDisabled
	}, "X linked, online, and local panel unpublished")

	// Verify X's old web panel is blocked (404) while X's published app route still responds (503 since project is stopped)!
	oldPanelReq := httptest.NewRequest("GET", "https://painel-x.example.com/api/health", nil)
	oldPanelW := httptest.NewRecorder()
	x.router.ServeHTTP(oldPanelW, oldPanelReq)
	if oldPanelW.Code != http.StatusNotFound {
		t.Fatalf("X's old panel still responded: %d", oldPanelW.Code)
	}
	appXReq := httptest.NewRequest("GET", "https://app-x.example.com/", nil)
	appXW := httptest.NewRecorder()
	x.router.ServeHTTP(appXW, appXReq)
	if appXW.Code != http.StatusServiceUnavailable {
		t.Fatalf("X's published project route broke after panel unpublish: %d", appXW.Code)
	}

	// 4. Privacy check: unauthenticated /api/session does NOT leak machine names, and /api/computers returns 401!
	preSess := publicRequest(t, y.server, "GET", "/api/session", "", "", nil, nil)
	if bytes.Contains(preSess.Body.Bytes(), []byte("Notebook X")) || bytes.Contains(preSess.Body.Bytes(), []byte("notebook-x")) {
		t.Fatalf("unauthenticated /api/session leaked linked machines: %s", preSess.Body.String())
	}
	if compUnauth := publicRequest(t, y.server, "GET", "/api/computers", "", "", nil, nil); compUnauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /api/computers allowed: %d", compUnauth.Code)
	}
	var preData map[string]any
	_ = json.Unmarshal(preSess.Body.Bytes(), &preData)
	cookies := preSess.Result().Cookies()
	csrf := preData["csrf"].(string)

	// 5. Federated login on Y using X's account ("admin" on "notebook-x")!
	// 5a. Y's password must NOT work when authenticating against X!
	wrongPass := publicRequest(t, y.server, "POST", "/api/login", `{"username":"admin","password":"secret-pass-notebook-Y","accountHost":"notebook-x"}`, csrf, cookies, nil)
	if wrongPass.Code != http.StatusUnauthorized {
		t.Fatalf("Y's password accepted for X's admin: %d %s", wrongPass.Code, wrongPass.Body.String())
	}
	// 5b. Unknown host returns uniform privacy-preserving 503.
	unknownHost := publicRequest(t, y.server, "POST", "/api/login", `{"username":"admin","password":"secret-pass-notebook-X","accountHost":"does-not-exist"}`, csrf, cookies, nil)
	if unknownHost.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for unknown account host, got %d %s", unknownHost.Code, unknownHost.Body.String())
	}
	// 5c. Correct password for X's admin succeeds!
	loginX := publicRequest(t, y.server, "POST", "/api/login", `{"username":"admin","password":"secret-pass-notebook-X","accountHost":"notebook-x"}`, csrf, cookies, nil)
	if loginX.Code != http.StatusOK {
		t.Fatalf("federated login for X's admin failed: %d %s", loginX.Code, loginX.Body.String())
	}
	xCookies := loginX.Result().Cookies()
	var xSess map[string]any
	_ = json.Unmarshal(loginX.Body.Bytes(), &xSess)
	xCSRF := xSess["csrf"].(string)
	if xSess["scope"] != cluster.ScopeOriginOnly || xSess["canManageComputers"] != false || xSess["issuerNodeId"] != "node-x" {
		t.Fatalf("unexpected default federated scope: %+v", xSess)
	}

	// 6. Default scope (origin_only): X's admin sees ONLY X's resources and CANNOT operate Y or /api/computers!
	projListX := publicRequest(t, y.server, "GET", "/api/projects", "", "", xCookies, nil)
	if projListX.Code != http.StatusOK || !strings.Contains(projListX.Body.String(), "Projeto X") || strings.Contains(projListX.Body.String(), "Projeto Y") {
		t.Fatalf("restricted X user saw wrong projects: %d %s", projListX.Code, projListX.Body.String())
	}
	if compDenied := publicRequest(t, y.server, "GET", "/api/computers", "", "", xCookies, nil); compDenied.Code != http.StatusForbidden {
		t.Fatalf("restricted X user accessed /api/computers: %d", compDenied.Code)
	}
	if actYDenied := publicRequest(t, y.server, "POST", "/api/nodes/node-y/projects/same-project-id/action", `{"action":"stop"}`, xCSRF, xCookies, nil); actYDenied.Code != http.StatusForbidden {
		t.Fatalf("restricted X user acted on Y project: %d", actYDenied.Code)
	}
	// Verify action targeting node-x routes ONLY to X, never touching Y's same-ID project!
	actX := publicRequest(t, y.server, "POST", "/api/nodes/node-x/projects/same-project-id/action", `{"action":"stop","operationId":"op-stop-x-1"}`, xCSRF, xCookies, nil)
	if actX.Code != http.StatusAccepted {
		t.Fatalf("X user action on X failed: %d %s", actX.Code, actX.Body.String())
	}
	// Verify Y's audit log has ZERO record of target_node_id='node-y' for op-stop-x-1 and recorded target_node_id='node-x'!
	auditEntries, _ := y.server.Cluster.ListAudit(20)
	foundOp := false
	for _, a := range auditEntries {
		if a.OperationID == "op-stop-x-1" {
			foundOp = true
			if a.TargetNodeID != "node-x" {
				t.Fatalf("action on X routed to wrong node: %+v", a)
			}
		}
		// Never leak passwords in audit!
		if strings.Contains(a.Detail, "secret-pass") || strings.Contains(a.Detail, "db-password") {
			t.Fatalf("secret leaked into audit log: %+v", a)
		}
	}
	if !foundOp {
		t.Fatal("audit log missing op-stop-x-1")
	}

	// 7. Explicitly grant UNIFIED ACCESS to X's admin from Y!
	adminX, _ := x.store.Admin()
	grantBody, _ := json.Marshal(map[string]any{
		"issuerNodeId": "node-x",
		"userId":       adminX.ID,
		"username":     "admin",
		"scope":        cluster.ScopeUnified,
	})
	grantResp := invoke(y.server, true, "PUT", "/api/cluster/grants", string(grantBody), "", "", nil)
	if grantResp.Code != http.StatusOK {
		t.Fatalf("grant unified failed: %d %s", grantResp.Code, grantResp.Body.String())
	}

	// Now X's admin (still with ZERO local user row in Y!) sees BOTH Y and X projects & databases AND /api/computers!
	projBoth := publicRequest(t, y.server, "GET", "/api/projects", "", "", xCookies, nil)
	if !strings.Contains(projBoth.Body.String(), "Projeto X") || !strings.Contains(projBoth.Body.String(), "Projeto Y") {
		t.Fatalf("unified X user did not see both projects: %s", projBoth.Body.String())
	}
	dbBoth := publicRequest(t, y.server, "GET", "/api/databases", "", "", xCookies, nil)
	if !strings.Contains(dbBoth.Body.String(), "DB Notebook X") || !strings.Contains(dbBoth.Body.String(), "DB Notebook Y") {
		t.Fatalf("unified X user did not see both databases: %s", dbBoth.Body.String())
	}
	if compAllowed := publicRequest(t, y.server, "GET", "/api/computers", "", "", xCookies, nil); compAllowed.Code != http.StatusOK {
		t.Fatalf("unified X user denied /api/computers: %d %s", compAllowed.Code, compAllowed.Body.String())
	}

	// 8. Sensitive database actions with step-up revalidation at origin computer (X)!
	// 8a. Ver senha on Y's DB using X's federated account: revalidates against X's password!
	pwYBad := publicRequest(t, y.server, "POST", "/api/nodes/node-y/databases/"+sharedDBID+"/password", `{"username":"admin","password":"secret-pass-notebook-Y"}`, xCSRF, xCookies, nil)
	if pwYBad.Code != http.StatusBadRequest {
		t.Fatalf("origin revalidation accepted Y's password for X's account: %d", pwYBad.Code)
	}
	pwYOK := publicRequest(t, y.server, "POST", "/api/nodes/node-y/databases/"+sharedDBID+"/password", `{"username":"admin","password":"secret-pass-notebook-X"}`, xCSRF, xCookies, nil)
	if pwYOK.Code != http.StatusOK || !strings.Contains(pwYOK.Body.String(), "db-password-ONLY-on-Y") || strings.Contains(pwYOK.Body.String(), "db-password-ONLY-on-X") {
		t.Fatalf("password reveal for Y's DB returned wrong result: %d %s", pwYOK.Code, pwYOK.Body.String())
	}
	// 8b. Ver senha on X's DB with the SAME ID returns ONLY X's DB password!
	pwXOK := publicRequest(t, y.server, "POST", "/api/nodes/node-x/databases/"+sharedDBID+"/password", `{"username":"admin","password":"secret-pass-notebook-X"}`, xCSRF, xCookies, nil)
	if pwXOK.Code != http.StatusOK || !strings.Contains(pwXOK.Body.String(), "db-password-ONLY-on-X") || strings.Contains(pwXOK.Body.String(), "db-password-ONLY-on-Y") {
		t.Fatalf("password reveal for X's DB returned wrong result: %d %s", pwXOK.Code, pwXOK.Body.String())
	}

	// 8c. Exporting private key (.key) on X requires single-use step-up token bound to (user, node-x, dbId, database.export_key)!
	keyNoToken := publicRequest(t, y.server, "GET", "/api/nodes/node-x/databases/"+sharedDBID+"/certificates/key", "", "", xCookies, nil)
	if keyNoToken.Code != http.StatusForbidden {
		t.Fatalf("private key export allowed without step-up: %d", keyNoToken.Code)
	}
	// Obtain step-up token bound to node-x + database.export_key!
	stepBody, _ := json.Marshal(map[string]any{
		"username":     "admin",
		"password":     "secret-pass-notebook-X",
		"targetNodeId": "node-x",
		"resourceId":   sharedDBID,
		"action":       cluster.StepUpDatabaseExportKey,
	})
	stepResp := publicRequest(t, y.server, "POST", "/api/auth/step-up", string(stepBody), xCSRF, xCookies, nil)
	if stepResp.Code != http.StatusOK {
		t.Fatalf("step-up failed: %d %s", stepResp.Code, stepResp.Body.String())
	}
	var stepData struct {
		StepUpToken string `json:"stepUpToken"`
	}
	_ = json.Unmarshal(stepResp.Body.Bytes(), &stepData)

	// Trying to use node-x's token on node-y MUST fail (and consumes token)!
	keyWrongNode := publicRequest(t, y.server, "GET", "/api/nodes/node-y/databases/"+sharedDBID+"/certificates/key", "", "", xCookies, map[string]string{"X-Step-Up-Token": stepData.StepUpToken})
	if keyWrongNode.Code != http.StatusForbidden {
		t.Fatalf("step-up token bound to node-x worked on node-y: %d", keyWrongNode.Code)
	}
	// Re-issue valid token for node-x -> works ONCE, replay fails!
	stepResp2 := publicRequest(t, y.server, "POST", "/api/auth/step-up", string(stepBody), xCSRF, xCookies, nil)
	_ = json.Unmarshal(stepResp2.Body.Bytes(), &stepData)
	keyOK := publicRequest(t, y.server, "GET", "/api/nodes/node-x/databases/"+sharedDBID+"/certificates/key", "", "", xCookies, map[string]string{"X-Step-Up-Token": stepData.StepUpToken})
	if keyOK.Code != http.StatusOK || !strings.Contains(keyOK.Body.String(), "PRIVATE KEY") {
		t.Fatalf("private key export failed with valid step-up: %d %s", keyOK.Code, keyOK.Body.String())
	}
	keyReplay := publicRequest(t, y.server, "GET", "/api/nodes/node-x/databases/"+sharedDBID+"/certificates/key", "", "", xCookies, map[string]string{"X-Step-Up-Token": stepData.StepUpToken})
	if keyReplay.Code != http.StatusForbidden {
		t.Fatalf("single-use step-up token was replayed: %d", keyReplay.Code)
	}

	// 8d. Deleting a database requires step-up token, and succeeds once with a valid token!
	delNoStep := publicRequest(t, y.server, "PATCH", "/api/nodes/node-x/databases/"+sharedDBID, `{"action":"delete","confirmation":"DB Notebook X"}`, xCSRF, xCookies, nil)
	if delNoStep.Code != http.StatusForbidden {
		t.Fatalf("database delete allowed without step-up: %d", delNoStep.Code)
	}
	delStepBody, _ := json.Marshal(map[string]any{
		"username":     "admin",
		"password":     "secret-pass-notebook-X",
		"targetNodeId": "node-x",
		"resourceId":   sharedDBID,
		"action":       cluster.StepUpDatabaseDelete,
	})
	delStepResp := publicRequest(t, y.server, "POST", "/api/auth/step-up", string(delStepBody), xCSRF, xCookies, nil)
	if delStepResp.Code != http.StatusOK {
		t.Fatalf("delete step-up failed: %d %s", delStepResp.Code, delStepResp.Body.String())
	}
	var delStepData struct {
		StepUpToken string `json:"stepUpToken"`
	}
	_ = json.Unmarshal(delStepResp.Body.Bytes(), &delStepData)
	delOK := publicRequest(t, y.server, "PATCH", "/api/nodes/node-x/databases/"+sharedDBID, `{"action":"delete","confirmation":"DB Notebook X","operationId":"op-del-x-db"}`, xCSRF, xCookies, map[string]string{"X-Step-Up-Token": delStepData.StepUpToken})
	if delOK.Code != http.StatusAccepted {
		t.Fatalf("database delete failed with valid step-up: %d %s", delOK.Code, delOK.Body.String())
	}
	// Verify Y's database with the EXACT SAME ID was NOT marked for deletion!
	yDBCheck := publicRequest(t, y.server, "GET", "/api/nodes/node-y/databases/"+sharedDBID, "", "", xCookies, nil)
	if yDBCheck.Code != http.StatusOK || !strings.Contains(yDBCheck.Body.String(), `"state":"stopped"`) {
		t.Fatalf("Y's same-ID database was unexpectedly affected by deleting X's DB: %d %s", yDBCheck.Code, yDBCheck.Body.String())
	}

	// 9. Password change on X IMMEDIATELY invalidates X's federated session on Y!
	pwChange := invoke(x.server, true, "POST", "/api/password", `{"current":"secret-pass-notebook-X","next":"new-secret-pass-notebook-X","confirmation":"new-secret-pass-notebook-X"}`, "", "", nil)
	if pwChange.Code != http.StatusOK {
		t.Fatalf("password change on X failed: %d %s", pwChange.Code, pwChange.Body.String())
	}
	waitUntil(t, 3*time.Second, func() bool {
		check := publicRequest(t, y.server, "GET", "/api/projects", "", "", xCookies, nil)
		return check.Code == http.StatusUnauthorized
	}, "X password change propagated to revoke federated session on Y")

	// Re-login with X's NEW password without passing accountHost!
	pre2 := publicRequest(t, y.server, "GET", "/api/session", "", "", nil, nil)
	var pre2Data map[string]any
	_ = json.Unmarshal(pre2.Body.Bytes(), &pre2Data)
	loginXNew := publicRequest(t, y.server, "POST", "/api/login", `{"username":"admin","password":"new-secret-pass-notebook-X"}`, pre2Data["csrf"].(string), pre2.Result().Cookies(), nil)
	if loginXNew.Code != http.StatusOK {
		t.Fatalf("federated login with X's new password failed: %d %s", loginXNew.Code, loginXNew.Body.String())
	}
	xCookiesNew := loginXNew.Result().Cookies()

	// 10. Simulate network drop on X -> Y detects offline, blocks actions, keeps inventory; reconnect restores online!
	x.server.Cluster.Stop()
	waitUntil(t, 3*time.Second, func() bool { return !y.server.Cluster.NodeOnline("node-x") }, "Y marks X offline after network drop")
	// Verify bounded offline origin validation window: backdate last_origin_check beyond 2s -> session revoked!
	_, _ = y.store.DB.Exec("UPDATE sessions SET last_origin_check=? WHERE issuer_node_id='node-x'", time.Now().Unix()-10)
	if expiredOffline := publicRequest(t, y.server, "GET", "/api/projects", "", "", xCookiesNew, nil); expiredOffline.Code != http.StatusUnauthorized {
		t.Fatalf("federated session stayed valid past offline grace window: %d", expiredOffline.Code)
	}

	// Reconnect X -> online again!
	x.server.Cluster.Start(ctx)
	waitUntil(t, 4*time.Second, func() bool { return y.server.Cluster.NodeOnline("node-x") }, "X reconnected and online again")

	// Re-login X user without accountHost, then REMOVE X from Y -> sessions revoked, old key permanently blocked, X's published app STILL works!
	pre3 := publicRequest(t, y.server, "GET", "/api/session", "", "", nil, nil)
	var pre3Data map[string]any
	_ = json.Unmarshal(pre3.Body.Bytes(), &pre3Data)
	loginX3 := publicRequest(t, y.server, "POST", "/api/login", `{"username":"admin","password":"new-secret-pass-notebook-X"}`, pre3Data["csrf"].(string), pre3.Result().Cookies(), nil)
	if loginX3.Code != http.StatusOK {
		t.Fatalf("re-login X failed: %d %s", loginX3.Code, loginX3.Body.String())
	}
	xCookies3 := loginX3.Result().Cookies()

	removeResp := invoke(y.server, true, "DELETE", "/api/cluster/nodes/node-x", "", "", "", nil)
	if removeResp.Code != http.StatusOK {
		t.Fatalf("remove X failed: %d %s", removeResp.Code, removeResp.Body.String())
	}
	if afterRemove := publicRequest(t, y.server, "GET", "/api/projects", "", "", xCookies3, nil); afterRemove.Code != http.StatusUnauthorized {
		t.Fatalf("session survived node removal: %d", afterRemove.Code)
	}
	waitUntil(t, 3*time.Second, func() bool { return x.server.Cluster.State().LinkStatus == cluster.LinkStatusRevoked }, "X marked revoked after removal")

	// X's published project HTTP route STILL works after removal, and X's old panel stays unpublished (requires explicit Desktop action)!
	appXAfter := httptest.NewRecorder()
	x.router.ServeHTTP(appXAfter, httptest.NewRequest("GET", "https://app-x.example.com/", nil))
	if appXAfter.Code != http.StatusServiceUnavailable {
		t.Fatalf("X's published project route stopped working after removal: %d", appXAfter.Code)
	}
	if !x.cloud.Config().PanelDisabled {
		t.Fatal("X's standalone panel auto-republished without explicit Desktop action")
	}
}

// controllableTCPProxy simulates real network drops and recoveries between X and Y.
type controllableTCPProxy struct {
	target string
	ln     net.Listener
	down   atomic.Bool
	mu     sync.Mutex
	conns  map[net.Conn]struct{}
}

func newControllableTCPProxy(t *testing.T, targetAddr string) *controllableTCPProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &controllableTCPProxy{target: targetAddr, ln: ln, conns: map[net.Conn]struct{}{}}
	go func() {
		for {
			client, err := ln.Accept()
			if err != nil {
				return
			}
			if p.down.Load() {
				_ = client.Close()
				continue
			}
			upstream, err := net.DialTimeout("tcp", p.target, 3*time.Second)
			if err != nil {
				_ = client.Close()
				continue
			}
			p.track(client)
			p.track(upstream)
			go func() {
				defer p.untrack(client)
				defer p.untrack(upstream)
				_, _ = io.Copy(upstream, client)
			}()
			go func() {
				defer p.untrack(upstream)
				defer p.untrack(client)
				_, _ = io.Copy(client, upstream)
			}()
		}
	}()
	t.Cleanup(func() { p.close() })
	return p
}

func (p *controllableTCPProxy) track(c net.Conn) {
	p.mu.Lock()
	p.conns[c] = struct{}{}
	p.mu.Unlock()
}

func (p *controllableTCPProxy) untrack(c net.Conn) {
	_ = c.Close()
	p.mu.Lock()
	delete(p.conns, c)
	p.mu.Unlock()
}

func (p *controllableTCPProxy) dropNetwork() {
	p.down.Store(true)
	p.mu.Lock()
	for c := range p.conns {
		_ = c.Close()
	}
	p.mu.Unlock()
}

func (p *controllableTCPProxy) restoreNetwork() {
	p.down.Store(false)
}

func (p *controllableTCPProxy) close() {
	p.down.Store(true)
	_ = p.ln.Close()
	p.dropNetwork()
}

func TestTwoNodeRealNetworkOutageReconnectAndLiveServicePreservation(t *testing.T) {
	y := newIntegratedNode(t, "node-y-net", "Notebook Y", "painel-y.example.com", "secret-pass-notebook-Y")
	x := newIntegratedNode(t, "node-x-net", "Notebook X", "painel-x.example.com", "secret-pass-notebook-X")

	// Start a real upstream HTTP server mimicking a live project running on Notebook X.
	appBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("live-service-on-notebook-x"))
	}))
	defer appBackend.Close()
	appURL, _ := url.Parse(appBackend.URL)
	appPort, _ := strconv.Atoi(appURL.Port())

	// Register a real Node/Python project directory on X with route app-live.example.com -> appPort.
	projDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(projDir, "app.py"), []byte("import time; time.sleep(60)\n"), 0600)
	projX := model.Project{ID: "live-app-x", Name: "Live App X", Runtime: "python", Path: projDir, WorkingDirectory: projDir, EntryPoint: filepath.Join(projDir, "app.py"), DesiredState: "stopped", Port: appPort}
	if err := x.store.SaveProject(projX); err != nil {
		t.Fatal(err)
	}
	x.sup.Add(projX)
	if err := x.store.SaveRoute(model.Route{ID: "route-live-x", ProjectID: projX.ID, ZoneID: "abcdef0123456789abcdef0123456789", Hostname: "app-live.example.com", Port: appPort}); err != nil {
		t.Fatal(err)
	}

	// Route X -> Y through a controllable TCP proxy so we can simulate a real network outage and recovery!
	yParsed, _ := url.Parse(y.httpServer.URL)
	proxy := newControllableTCPProxy(t, yParsed.Host)
	y.server.PrimaryURLOverride = "http://" + proxy.ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	y.server.Cluster.Start(ctx)
	x.server.Cluster.Start(ctx)

	// Pair X to Y through the live network proxy.
	invResp := invoke(y.server, true, "POST", "/api/cluster/invites", `{"username":"admin","password":"secret-pass-notebook-Y"}`, "", "", nil)
	if invResp.Code != http.StatusCreated {
		t.Fatalf("invite failed: %d %s", invResp.Code, invResp.Body.String())
	}
	var inv struct{ Invite string }
	_ = json.Unmarshal(invResp.Body.Bytes(), &inv)
	linkBody, _ := json.Marshal(map[string]any{"invite": inv.Invite, "displayName": "Notebook X", "confirmed": true})
	if w := invoke(x.server, true, "POST", "/api/cluster/link", string(linkBody), "", "", nil); w.Code != http.StatusAccepted {
		t.Fatalf("link failed: %d %s", w.Code, w.Body.String())
	}
	if w := invoke(y.server, true, "POST", "/api/cluster/nodes/node-x-net/approve", `{}`, "", "", nil); w.Code != http.StatusOK {
		t.Fatalf("approve failed: %d %s", w.Code, w.Body.String())
	}
	waitUntil(t, 5*time.Second, func() bool {
		return y.server.Cluster.NodeOnline("node-x-net") && x.server.Cluster.Role() == cluster.RoleSecondary && x.cloud.Config().PanelDisabled
	}, "linked over proxy")

	// Drop the network between X and Y!
	proxy.dropNetwork()
	waitUntil(t, 3*time.Second, func() bool {
		return !y.server.Cluster.NodeOnline("node-x-net")
	}, "Y detects X offline on real TCP drop")

	// Restore the network between X and Y -> X must auto-reconnect without user action!
	proxy.restoreNetwork()
	waitUntil(t, 5*time.Second, func() bool {
		return y.server.Cluster.NodeOnline("node-x-net")
	}, "X auto-reconnected after network restoration")

	// Remove X -> verify old panel remains 404, X is revoked, and X's route stays intact!
	if w := invoke(y.server, true, "DELETE", "/api/cluster/nodes/node-x-net", "", "", "", nil); w.Code != http.StatusOK {
		t.Fatalf("remove failed: %d %s", w.Code, w.Body.String())
	}
	waitUntil(t, 3*time.Second, func() bool {
		return x.server.Cluster.State().LinkStatus == cluster.LinkStatusRevoked
	}, "X revoked")
	routes, err := x.store.Routes()
	if err != nil || len(routes) != 1 || routes[0].Hostname != "app-live.example.com" {
		t.Fatalf("X's service route was lost after removal: %+v %v", routes, err)
	}
}

func publicRequestFromIP(t *testing.T, srv *Server, remoteAddr, method, path, body, csrf string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
	r.RemoteAddr = remoteAddr
	r.Header.Set("Content-Type", "application/json")
	if method != http.MethodGet {
		r.Header.Set("Origin", "http://127.0.0.1")
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	srv.Public(w, r)
	return w
}

func TestAutomaticLoginResolutionPrimarySecondaryAndUnifiedScope(t *testing.T) {
	y := newIntegratedNode(t, "node-primary", "Principal Y", "painel-y.example.com", "primary-secret-password")
	x := newIntegratedNode(t, "node-extra-x", "Extra X", "painel-x.example.com", "extra-x-secret-password")
	z := newIntegratedNode(t, "node-extra-z", "Extra Z", "painel-z.example.com", "extra-z-secret-password")

	// Give Extra Z a distinct username ("maria") while Extra X shares "admin" with Primary Y.
	if err := z.server.Auth.ChangeUsername("extra-z-secret-password", "maria"); err != nil {
		t.Fatal(err)
	}

	projY := model.Project{ID: "proj-y", Name: "Projeto Principal", Runtime: "python", Path: t.TempDir(), DesiredState: "stopped", Port: 4100}
	projX := model.Project{ID: "proj-x", Name: "Projeto Extra X", Runtime: "node", Path: t.TempDir(), DesiredState: "stopped", Port: 4200}
	projZ := model.Project{ID: "proj-z", Name: "Projeto Extra Z", Runtime: "node", Path: t.TempDir(), DesiredState: "stopped", Port: 4300}
	for _, pair := range []struct {
		n *integratedNode
		p model.Project
	}{{y, projY}, {x, projX}, {z, projZ}} {
		if err := pair.n.store.SaveProject(pair.p); err != nil {
			t.Fatal(err)
		}
		pair.n.sup.Add(pair.p)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	y.server.Cluster.Start(ctx)
	x.server.Cluster.Start(ctx)
	z.server.Cluster.Start(ctx)

	linkNode := func(extra *integratedNode, displayName string) {
		invResp := invoke(y.server, true, "POST", "/api/cluster/invites", `{"username":"admin","password":"primary-secret-password"}`, "", "", nil)
		if invResp.Code != http.StatusCreated {
			t.Fatalf("invite failed: %d %s", invResp.Code, invResp.Body.String())
		}
		var inv struct{ Invite string }
		_ = json.Unmarshal(invResp.Body.Bytes(), &inv)
		body, _ := json.Marshal(map[string]any{"invite": inv.Invite, "displayName": displayName, "confirmed": true})
		if w := invoke(extra.server, true, "POST", "/api/cluster/link", string(body), "", "", nil); w.Code != http.StatusAccepted {
			t.Fatalf("link %s failed: %d %s", extra.id, w.Code, w.Body.String())
		}
		if w := invoke(y.server, true, "POST", "/api/cluster/nodes/"+extra.id+"/approve", `{}`, "", "", nil); w.Code != http.StatusOK {
			t.Fatalf("approve %s failed: %d %s", extra.id, w.Code, w.Body.String())
		}
		waitUntil(t, 5*time.Second, func() bool {
			return y.server.Cluster.NodeOnline(extra.id) && extra.server.Cluster.Role() == cluster.RoleSecondary
		}, extra.id+" linked and online")
	}

	linkNode(x, "Extra X")
	linkNode(z, "Extra Z")

	loginOnlyCreds := func(ip, username, password string) (*httptest.ResponseRecorder, map[string]any) {
		pre := publicRequestFromIP(t, y.server, ip, "GET", "/api/session", "", "", nil)
		var preData map[string]any
		_ = json.Unmarshal(pre.Body.Bytes(), &preData)
		payload, _ := json.Marshal(map[string]string{"username": username, "password": password})
		res := publicRequestFromIP(t, y.server, ip, "POST", "/api/login", string(payload), preData["csrf"].(string), pre.Result().Cookies())
		var data map[string]any
		_ = json.Unmarshal(res.Body.Bytes(), &data)
		return res, data
	}

	// 1. Primary account (username + password only) -> full access across Y, X, and Z.
	resY, sessY := loginOnlyCreds("10.0.0.1:1000", "admin", "primary-secret-password")
	if resY.Code != http.StatusOK || sessY["issuerNodeId"] != "node-primary" || sessY["scope"] != cluster.ScopeUnified || sessY["canManageComputers"] != true {
		t.Fatalf("primary login failed or wrong scope: %d %+v", resY.Code, sessY)
	}
	projsY := publicRequestFromIP(t, y.server, "10.0.0.1:1000", "GET", "/api/projects", "", "", resY.Result().Cookies())
	if !strings.Contains(projsY.Body.String(), "Projeto Principal") || !strings.Contains(projsY.Body.String(), "Projeto Extra X") || !strings.Contains(projsY.Body.String(), "Projeto Extra Z") {
		t.Fatalf("primary did not see all projects: %s", projsY.Body.String())
	}

	// 2. Extra X account (same username "admin", different password, NO accountHost) -> resolves to X with origin_only!
	resX, sessX := loginOnlyCreds("10.0.0.2:1000", "admin", "extra-x-secret-password")
	if resX.Code != http.StatusOK || sessX["issuerNodeId"] != "node-extra-x" || sessX["scope"] != cluster.ScopeOriginOnly || sessX["canManageComputers"] != false {
		t.Fatalf("extra X auto login failed or wrong scope: %d %+v", resX.Code, sessX)
	}
	projsX := publicRequestFromIP(t, y.server, "10.0.0.2:1000", "GET", "/api/projects", "", "", resX.Result().Cookies())
	if !strings.Contains(projsX.Body.String(), "Projeto Extra X") || strings.Contains(projsX.Body.String(), "Projeto Principal") || strings.Contains(projsX.Body.String(), "Projeto Extra Z") {
		t.Fatalf("extra X origin_only saw wrong projects: %s", projsX.Body.String())
	}

	// 3. Extra Z account (unique username "maria", NO accountHost) -> resolves to Z with origin_only!
	resZ, sessZ := loginOnlyCreds("10.0.0.3:1000", "maria", "extra-z-secret-password")
	if resZ.Code != http.StatusOK || sessZ["issuerNodeId"] != "node-extra-z" || sessZ["scope"] != cluster.ScopeOriginOnly || sessZ["canManageComputers"] != false {
		t.Fatalf("extra Z auto login failed or wrong scope: %d %+v", resZ.Code, sessZ)
	}
	projsZ := publicRequestFromIP(t, y.server, "10.0.0.3:1000", "GET", "/api/projects", "", "", resZ.Result().Cookies())
	if !strings.Contains(projsZ.Body.String(), "Projeto Extra Z") || strings.Contains(projsZ.Body.String(), "Projeto Principal") || strings.Contains(projsZ.Body.String(), "Projeto Extra X") {
		t.Fatalf("extra Z origin_only saw wrong projects: %s", projsZ.Body.String())
	}

	// 4. Grant unified access to Extra X -> logging in with only username + password returns unified scope and ALL projects!
	adminX, _ := x.store.Admin()
	grantUnified, _ := json.Marshal(map[string]any{
		"issuerNodeId": "node-extra-x",
		"userId":       adminX.ID,
		"username":     "admin",
		"scope":        cluster.ScopeUnified,
	})
	if w := invoke(y.server, true, "PUT", "/api/cluster/grants", string(grantUnified), "", "", nil); w.Code != http.StatusOK {
		t.Fatalf("grant unified failed: %d %s", w.Code, w.Body.String())
	}
	resXUnified, sessXUnified := loginOnlyCreds("10.0.0.4:1000", "admin", "extra-x-secret-password")
	if resXUnified.Code != http.StatusOK || sessXUnified["issuerNodeId"] != "node-extra-x" || sessXUnified["scope"] != cluster.ScopeUnified || sessXUnified["canManageComputers"] != true {
		t.Fatalf("extra X unified auto login failed or wrong scope: %d %+v", resXUnified.Code, sessXUnified)
	}
	projsXUnified := publicRequestFromIP(t, y.server, "10.0.0.4:1000", "GET", "/api/projects", "", "", resXUnified.Result().Cookies())
	if !strings.Contains(projsXUnified.Body.String(), "Projeto Principal") || !strings.Contains(projsXUnified.Body.String(), "Projeto Extra X") || !strings.Contains(projsXUnified.Body.String(), "Projeto Extra Z") {
		t.Fatalf("extra X unified did not see all projects: %s", projsXUnified.Body.String())
	}

	// 5. Revoke unified access on Extra X -> returns to origin_only!
	revokeUnified, _ := json.Marshal(map[string]any{
		"issuerNodeId": "node-extra-x",
		"userId":       adminX.ID,
		"username":     "admin",
		"scope":        cluster.ScopeOriginOnly,
	})
	if w := invoke(y.server, true, "PUT", "/api/cluster/grants", string(revokeUnified), "", "", nil); w.Code != http.StatusOK {
		t.Fatalf("revoke unified failed: %d %s", w.Code, w.Body.String())
	}
	resXRestricted, sessXRestricted := loginOnlyCreds("10.0.0.5:1000", "admin", "extra-x-secret-password")
	if resXRestricted.Code != http.StatusOK || sessXRestricted["scope"] != cluster.ScopeOriginOnly || sessXRestricted["canManageComputers"] != false {
		t.Fatalf("extra X after revoke should be origin_only: %d %+v", resXRestricted.Code, sessXRestricted)
	}

	// 6. Disabling loginEnabled on Extra Z blocks login for "maria"!
	if w := invoke(y.server, true, "PATCH", "/api/cluster/nodes/node-extra-z", `{"loginEnabled":false}`, "", "", nil); w.Code != http.StatusOK {
		t.Fatalf("disable login on Z failed: %d %s", w.Code, w.Body.String())
	}
	resZDisabled, _ := loginOnlyCreds("10.0.0.6:1000", "maria", "extra-z-secret-password")
	if resZDisabled.Code != http.StatusUnauthorized {
		t.Fatalf("disabled Z account should be rejected with 401, got %d %s", resZDisabled.Code, resZDisabled.Body.String())
	}

	// 7. Re-enable loginEnabled on Z and take Z offline -> returns 503 origin_unavailable for "maria"!
	if w := invoke(y.server, true, "PATCH", "/api/cluster/nodes/node-extra-z", `{"loginEnabled":true}`, "", "", nil); w.Code != http.StatusOK {
		t.Fatalf("re-enable login on Z failed: %d %s", w.Code, w.Body.String())
	}
	z.server.Cluster.Stop()
	waitUntil(t, 3*time.Second, func() bool { return !y.server.Cluster.NodeOnline("node-extra-z") }, "Z marked offline")
	resZOffline, dataZOffline := loginOnlyCreds("10.0.0.7:1000", "maria", "extra-z-secret-password")
	if resZOffline.Code != http.StatusServiceUnavailable || dataZOffline["code"] != "origin_unavailable" {
		t.Fatalf("offline Z account should return 503 origin_unavailable, got %d %+v", resZOffline.Code, dataZOffline)
	}

	// 8. Wrong password returns 401!
	resBad, _ := loginOnlyCreds("10.0.0.8:1000", "admin", "completely-wrong-password")
	if resBad.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password should return 401, got %d %s", resBad.Code, resBad.Body.String())
	}
}
