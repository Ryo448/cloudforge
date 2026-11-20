package auth

import (
	"cloudforge/core/internal/persistence"
	"path/filepath"
	"strings"
	"testing"
)

func TestPasswordAndSessions(t *testing.T) {
	s, e := persistence.Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a := New(s)
	if a.Ready() {
		t.Fatal("default administrator must not exist")
	}
	if e = a.Bootstrap("admin", "a-long-test-password"); e != nil {
		t.Fatal(e)
	}
	if e = a.Bootstrap("other", "another-long-password"); e == nil {
		t.Fatal("second bootstrap accepted")
	}
	var hash string
	if e = s.DB.QueryRow("SELECT password_hash FROM users").Scan(&hash); e != nil {
		t.Fatal(e)
	}
	if !strings.HasPrefix(hash, "$argon2id$") || strings.Contains(hash, "a-long-test-password") {
		t.Fatal("unsafe password storage")
	}
	if _, e = a.Authenticate("admin", "wrong"); e == nil {
		t.Fatal("invalid password accepted by Authenticate")
	}
	if _, e = a.Authenticate("other", "a-long-test-password"); e == nil {
		t.Fatal("invalid username accepted by Authenticate")
	}
	verified, e := a.Authenticate(" admin ", "a-long-test-password")
	if e != nil || verified.Username != "admin" {
		t.Fatalf("valid credentials rejected by Authenticate: %v", e)
	}
	if sessions, e := a.Sessions(); e != nil || len(sessions) != 0 {
		t.Fatalf("Authenticate created unexpected sessions: %+v %v", sessions, e)
	}
	if _, _, e = a.Login("admin", "wrong", "test"); e == nil {
		t.Fatal("invalid password accepted")
	}
	token, csrf, e := a.Login("admin", "a-long-test-password", "test")
	if e != nil {
		t.Fatal(e)
	}
	session, e := a.Session(token)
	if e != nil || !a.ValidCSRF(session, csrf) || a.ValidCSRF(session, "wrong") {
		t.Fatal("session / CSRF failed")
	}
	var stored string
	s.DB.QueryRow("SELECT token_hash FROM sessions").Scan(&stored)
	if stored == token {
		t.Fatal("raw session persisted")
	}
	next, newCSRF, e := a.Renew(token, "renewed")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.Session(token); e == nil {
		t.Fatal("old token remains usable")
	}
	renewed, e := a.Session(next)
	if e != nil || !a.ValidCSRF(renewed, newCSRF) {
		t.Fatal("rotation failed")
	}
	if e = a.ChangePassword("a-long-test-password", "a-new-long-test-password"); e != nil {
		t.Fatal(e)
	}
	if _, e = a.Session(next); e == nil {
		t.Fatal("password change did not revoke sessions")
	}
}
