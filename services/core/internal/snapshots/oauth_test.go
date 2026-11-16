package snapshots

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOAuthLoopbackPKCERefreshAndDisconnect(t *testing.T) {
	s := testService(t)
	d := s.drive
	d.state = driveState{Configured: true}
	d.clientID, d.clientSecret = "desktop-client", "desktop-secret"
	d.vault = &memoryVault{}
	signed := make(chan url.Values, 1)
	allowRefresh := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			if r.ParseForm() != nil || r.PostForm.Get("client_id") != "desktop-client" || r.PostForm.Get("client_secret") != "desktop-secret" {
				http.Error(w, "client", 400)
				return
			}
			if r.PostForm.Get("grant_type") == "authorization_code" {
				signed <- r.PostForm
				_ = json.NewEncoder(w).Encode(credentials{Access: "first-access", Refresh: "first-refresh", Seconds: 3600})
				return
			}
			if r.PostForm.Get("refresh_token") != "first-refresh" {
				http.Error(w, "refresh", 400)
				return
			}
			if !allowRefresh {
				w.WriteHeader(400)
				_ = json.NewEncoder(w).Encode(credentials{Error: "invalid_grant"})
				return
			}
			_ = json.NewEncoder(w).Encode(credentials{Access: "new-access", Seconds: 3600})
		case "/about":
			if r.Header.Get("Authorization") != "Bearer first-access" {
				w.WriteHeader(401)
				return
			}
			_, _ = w.Write([]byte("{\"user\":{\"permissionId\":\"google-user\",\"emailAddress\":\"backup@example.test\"}}"))
		case "/files":
			if r.Header.Get("Authorization") != "Bearer new-access" {
				w.WriteHeader(401)
				return
			}
			_, _ = w.Write([]byte("{\"files\":[]}"))
		case "/revoke":
			if r.ParseForm() != nil || r.PostForm.Get("token") != "first-refresh" {
				http.Error(w, "revoke", 400)
				return
			}
			w.WriteHeader(200)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	d.client, d.tokenURL, d.revokeURL, d.api = server.Client(), server.URL+"/token", server.URL+"/revoke", server.URL
	authorization, err := s.Connect()
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(authorization)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "accounts.google.com" || q.Get("scope") != driveScope || q.Get("code_challenge_method") != "S256" ||
		q.Get("access_type") != "offline" || q.Get("state") == "" {
		t.Fatal("incorrect Google authorization URL")
	}
	redirect := q.Get("redirect_uri")
	resp, err := server.Client().Get(redirect + "?state=invalid&code=code")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatal("invalid state accepted")
	}
	resp, err = server.Client().Get(redirect + "?state=" + q.Get("state") + "&code=code")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("OAuth callback: %d", resp.StatusCode)
	}
	var values url.Values
	select {
	case values = <-signed:
	case <-time.After(5 * time.Second):
		t.Fatal("Google authorization code not exchanged")
	}
	proof := sha256.Sum256([]byte(values.Get("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(proof[:]) != q.Get("code_challenge") ||
		values.Get("redirect_uri") != redirect || values.Get("code") != "code" {
		t.Fatal("invalid PKCE exchange")
	}
	deadline := time.Now().Add(5 * time.Second)
	for d.status().Pending && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	state := d.status()
	if state.Account == nil || state.Account.ID != "google-user" || state.Error != "" {
		t.Fatalf("authorization: %+v", state)
	}
	var persisted account
	if s.store.Setting("snapshotDriveAccount", &persisted) != nil || persisted.ID != "google-user" {
		t.Fatalf("account identity not persisted: %+v", persisted)
	}
	if _, err = d.list(t.Context(), "google-user", property("cfKind", "machine")); err != nil {
		t.Fatalf("401 should refresh and retry: %v", err)
	}
	d.mu.Lock()
	d.expires = time.Now().Add(-time.Minute)
	d.mu.Unlock()
	allowRefresh = false
	if _, err = d.accessToken(t.Context(), "google-user", ""); err != errRevoked || d.status().Error != errRevoked.Error() {
		t.Fatalf("revoked authorization: %v", err)
	}
	if err := s.Disconnect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if token, _ := d.vault.Get(); token != "" || d.status().Account != nil {
		t.Fatal("authorization remained after disconnect")
	}
}

func TestConfigureDriveClientInVault(t *testing.T) {
	s := testService(t)
	d := s.drive
	d.state = driveState{}
	d.clientVault = &memoryVault{}
	client := OAuthClient{ID: "example-desktop.apps.googleusercontent.com", Secret: "example-CI_secret"}
	if err := s.ConfigureDrive(OAuthClient{ID: "wrong-client", Secret: client.Secret}); err == nil {
		t.Fatal("an invalid client identifier was accepted")
	}
	if err := s.ConfigureDrive(client); err != nil {
		t.Fatal(err)
	}
	status, err := json.Marshal(s.DriveStatus())
	if err != nil || !s.DriveStatus().Configured || strings.Contains(string(status), client.ID) ||
		strings.Contains(string(status), client.Secret) {
		t.Fatalf("unexpected drive status: %s (%v)", status, err)
	}
	reopened := &drive{clientVault: d.clientVault}
	reopened.loadClient()
	if !reopened.status().Configured || reopened.clientID != client.ID || reopened.clientSecret != client.Secret {
		t.Fatal("client credentials were not restored from the vault")
	}
	d.state.Pending = true
	if err := s.ConfigureDrive(client); err == nil {
		t.Fatal("client credentials changed during authorization")
	}
	d.state.Pending, d.state.Managed = false, true
	if err := s.ConfigureDrive(client); err == nil {
		t.Fatal("client credentials changed despite environment configuration")
	}
}
