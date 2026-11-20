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
	"unicode"
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
func validateUsername(username string) (string, error) {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 64 {
		return "", errors.New("nome de usuário deve ter entre 3 e 64 caracteres")
	}
	for _, r := range username {
		if unicode.IsControl(r) {
			return "", errors.New("nome de usuário contém caracteres inválidos")
		}
	}
	return username, nil
}
func (a *Auth) Bootstrap(username, password string) error {
	username, e := validateUsername(username)
	if e != nil {
		return e
	}
	hash, e := Hash(password)
	if e != nil {
		return e
	}
	return a.Store.CreateAdmin(persistence.User{ID: ID(), Username: username, PasswordHash: hash, CreatedAt: time.Now().Unix()})
}
func (a *Auth) Authenticate(username, password string) (persistence.User, error) {
	user, e := a.Store.UserByUsername(strings.TrimSpace(username))
	if e != nil {
		Verify(a.dummy, password)
		return persistence.User{}, errors.New("usuário ou senha inválidos")
	}
	if !Verify(user.PasswordHash, password) {
		return persistence.User{}, errors.New("usuário ou senha inválidos")
	}
	return user, nil
}
func (a *Auth) Login(username, password, device string) (string, string, error) {
	user, e := a.Authenticate(username, password)
	if e != nil {
		return "", "", e
	}
	return a.issue("", user.ID, user.Username, user.AuthEpoch, device)
}
func (a *Auth) IssueFederated(issuerNodeID, userID, username string, authEpoch int64, device string) (string, string, error) {
	if issuerNodeID == "" || userID == "" {
		return "", "", errors.New("identidade federada inválida")
	}
	return a.issue(issuerNodeID, userID, username, authEpoch, device)
}
func newSession(issuerNodeID, userID, username string, authEpoch int64, device string) (Session, string, string) {
	token, csrf := Token(), Token()
	if len(device) > 256 {
		device = device[:256]
	}
	if authEpoch <= 0 {
		authEpoch = 1
	}
	now := time.Now().Unix()
	return Session{
		ID:              ID(),
		IssuerNodeID:    issuerNodeID,
		UserID:          userID,
		Username:        username,
		TokenHash:       Digest(token),
		CSRFHash:        Digest(csrf),
		CreatedAt:       now,
		ExpiresAt:       now + 30*86400,
		Device:          device,
		AuthEpoch:       authEpoch,
		LastOriginCheck: now,
	}, token, csrf
}
func (a *Auth) issue(issuerNodeID, userID, username string, authEpoch int64, device string) (string, string, error) {
	session, token, csrf := newSession(issuerNodeID, userID, username, authEpoch, device)
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
	session, next, csrf := newSession(old.IssuerNodeID, old.UserID, old.Username, old.AuthEpoch, device)
	session.LastOriginCheck = old.LastOriginCheck
	return next, csrf, a.Store.RotateSession(old.ID, session)
}
func (a *Auth) Sessions() ([]Session, error) { return a.Store.ListSessions() }
func (a *Auth) Revoke(id string) error       { return a.Store.RevokeSession(id) }
func (a *Auth) RevokeIssuer(issuerNodeID string) error {
	return a.Store.RevokeSessionsByIssuer(issuerNodeID)
}
func (a *Auth) RevokePrincipal(issuerNodeID, userID string) error {
	return a.Store.RevokeSessionsByPrincipal(issuerNodeID, userID)
}
func (a *Auth) RevokeBelowEpoch(issuerNodeID, userID string, minEpoch int64) error {
	return a.Store.RevokeSessionsByEpoch(issuerNodeID, userID, minEpoch)
}
func (a *Auth) TouchOriginCheck(sessionID string, checkedAt int64) error {
	return a.Store.UpdateSessionOriginCheck(sessionID, checkedAt)
}
func (a *Auth) ChangeUsername(currentPassword, nextUsername string) error {
	_, _, _, err := a.ChangeUsernameEpoch(currentPassword, nextUsername)
	return err
}
func (a *Auth) ChangeUsernameEpoch(currentPassword, nextUsername string) (string, string, int64, error) {
	user, e := a.Store.Admin()
	if e != nil {
		return "", "", 0, e
	}
	if !Verify(user.PasswordHash, currentPassword) {
		return "", "", 0, errors.New("senha atual inválida")
	}
	username, e := validateUsername(nextUsername)
	if e != nil {
		return "", "", 0, e
	}
	if username == user.Username {
		return "", "", 0, errors.New("o novo nome de usuário deve ser diferente do atual")
	}
	if e = a.Store.SetUsernameAndRevoke(user.ID, username); e != nil {
		return "", "", 0, e
	}
	updated, err := a.Store.UserByID(user.ID)
	if err != nil {
		return user.ID, username, user.AuthEpoch + 1, nil
	}
	return updated.ID, updated.Username, updated.AuthEpoch, nil
}
func (a *Auth) ChangePassword(current, next string) error {
	_, _, err := a.ChangePasswordEpoch(current, next)
	return err
}
func (a *Auth) ChangePasswordEpoch(current, next string) (string, int64, error) {
	user, e := a.Store.Admin()
	if e != nil {
		return "", 0, e
	}
	if !Verify(user.PasswordHash, current) {
		return "", 0, errors.New("senha atual inválida")
	}
	hash, e := Hash(next)
	if e != nil {
		return "", 0, e
	}
	if e = a.Store.SetPasswordAndRevoke(user.ID, hash); e != nil {
		return "", 0, e
	}
	updated, err := a.Store.UserByID(user.ID)
	if err != nil {
		return user.ID, user.AuthEpoch + 1, nil
	}
	return updated.ID, updated.AuthEpoch, nil
}
