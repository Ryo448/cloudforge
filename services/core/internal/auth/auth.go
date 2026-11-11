package auth

import (
	"cloudforge/core/internal/persistence"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"golang.org/x/crypto/argon2"
	"strings"
	"time"
)

func Token() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func ID() string             { return Token()[:22] }
func Digest(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func Hash(password string) (string, error) {
	if len(password) < 12 || len(password) > 1024 {
		return "", errors.New("use uma senha de 12 a 1024 caracteres")
	}
	salt := make([]byte, 16)
	if _, e := rand.Read(salt); e != nil {
		return "", e
	}
	key := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=2$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}
func Verify(encoded, password string) bool {
	if len(password) > 1024 {
		return false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=65536,t=3,p=2" {
		return false
	}
	salt, e := base64.RawStdEncoding.DecodeString(parts[4])
	if e != nil || len(salt) != 16 {
		return false
	}
	want, e := base64.RawStdEncoding.DecodeString(parts[5])
	if e != nil || len(want) != 32 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(want, got) == 1
}

type Auth struct {
	Store persistence.AuthRepository
	dummy string
}
type Session = persistence.SessionRecord

func New(s persistence.AuthRepository) *Auth { dummy, _ := Hash(Token()); return &Auth{s, dummy} }
func (a *Auth) Ready() bool                  { ready, e := a.Store.HasUsers(); return ready && e == nil }
func (a *Auth) Bootstrap(username, password string) error {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 64 {
		return errors.New("nome de usuário deve ter entre 3 e 64 caracteres")
	}
	hash, e := Hash(password)
	if e != nil {
		return e
	}
	return a.Store.CreateAdmin(persistence.User{ID: ID(), Username: username, PasswordHash: hash, CreatedAt: time.Now().Unix()})
}
func (a *Auth) Login(username, password, device string) (string, string, error) {
	user, e := a.Store.UserByUsername(username)
	if e != nil {
		Verify(a.dummy, password)
		return "", "", errors.New("usuário ou senha inválidos")
	}
	if !Verify(user.PasswordHash, password) {
		return "", "", errors.New("usuário ou senha inválidos")
	}
	return a.issue(user.ID, device)
}
func newSession(userID, device string) (Session, string, string) {
	token, csrf := Token(), Token()
	if len(device) > 256 {
		device = device[:256]
	}
	now := time.Now().Unix()
	return Session{ID: ID(), UserID: userID, TokenHash: Digest(token), CSRFHash: Digest(csrf), CreatedAt: now, ExpiresAt: now + 30*86400, Device: device}, token, csrf
}
func (a *Auth) issue(userID, device string) (string, string, error) {
	session, token, csrf := newSession(userID, device)
	return token, csrf, a.Store.CreateSession(session)
}
func (a *Auth) Session(token string) (Session, error) { return a.Store.LookupSession(Digest(token)) }
func (a *Auth) ValidCSRF(session Session, value string) bool {
	return subtle.ConstantTimeCompare([]byte(session.CSRFHash), []byte(Digest(value))) == 1
}
func (a *Auth) Logout(token string) error { return a.Store.RevokeToken(Digest(token)) }
func (a *Auth) Renew(token, device string) (string, string, error) {
	old, e := a.Session(token)
	if e != nil {
		return "", "", e
	}
	session, next, csrf := newSession(old.UserID, device)
	return next, csrf, a.Store.RotateSession(old.ID, session)
}
func (a *Auth) Sessions() ([]Session, error) { return a.Store.ListSessions() }
func (a *Auth) Revoke(id string) error       { return a.Store.RevokeSession(id) }
func (a *Auth) ChangePassword(current, next string) error {
	user, e := a.Store.Admin()
	if e != nil {
		return e
	}
	if !Verify(user.PasswordHash, current) {
		return errors.New("senha atual inválida")
	}
	hash, e := Hash(next)
	if e != nil {
		return e
	}
	return a.Store.SetPasswordAndRevoke(user.ID, hash)
}
