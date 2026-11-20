package persistence

import (
	"errors"
	"time"
)

type User struct {
	ID           string
	Username     string
	PasswordHash string
	CreatedAt    int64
	AuthEpoch    int64
}
type SessionRecord struct {
	ID              string `json:"id"`
	IssuerNodeID    string `json:"issuerNodeId,omitempty"`
	UserID          string `json:"-"`
	Username        string `json:"username"`
	ExpiresAt       int64  `json:"expiresAt"`
	Device          string `json:"device"`
	CSRFHash        string `json:"-"`
	TokenHash       string `json:"-"`
	CreatedAt       int64  `json:"-"`
	AuthEpoch       int64  `json:"-"`
	LastOriginCheck int64  `json:"-"`
}
type AuthRepository interface {
	HasUsers() (bool, error)
	CreateAdmin(User) error
	UserByUsername(string) (User, error)
	UserByID(string) (User, error)
	Admin() (User, error)
	CreateSession(SessionRecord) error
	LookupSession(string) (SessionRecord, error)
	RevokeToken(string) error
	RotateSession(string, SessionRecord) error
	ListSessions() ([]SessionRecord, error)
	RevokeSession(string) error
	SetUsernameAndRevoke(string, string) error
	SetPasswordAndRevoke(string, string) error
	RevokeSessionsByIssuer(string) error
	RevokeSessionsByPrincipal(string, string) error
	RevokeSessionsByEpoch(string, string, int64) error
	UpdateSessionOriginCheck(string, int64) error
}

func (s *Store) HasUsers() (bool, error) {
	var n int
	e := s.DB.QueryRow("SELECT count(*) FROM users").Scan(&n)
	return n > 0, e
}
func (s *Store) CreateAdmin(u User) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var n int
	if e = tx.QueryRow("SELECT count(*) FROM users").Scan(&n); e != nil {
		return e
	}
	if n != 0 {
		return errors.New("administrador já foi configurado")
	}
	epoch := u.AuthEpoch
	if epoch <= 0 {
		epoch = 1
	}
	if _, e = tx.Exec("INSERT INTO users(id,username,password_hash,created_at,updated_at,auth_epoch) VALUES(?,?,?,?,?,?)", u.ID, u.Username, u.PasswordHash, u.CreatedAt, u.CreatedAt, epoch); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) UserByUsername(name string) (User, error) {
	var u User
	e := s.DB.QueryRow("SELECT id,username,password_hash,auth_epoch FROM users WHERE username=?", name).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.AuthEpoch)
	return u, e
}
func (s *Store) UserByID(id string) (User, error) {
	var u User
	e := s.DB.QueryRow("SELECT id,username,password_hash,auth_epoch FROM users WHERE id=?", id).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.AuthEpoch)
	return u, e
}
func (s *Store) Admin() (User, error) {
	var u User
	e := s.DB.QueryRow("SELECT id,username,password_hash,auth_epoch FROM users LIMIT 1").Scan(&u.ID, &u.Username, &u.PasswordHash, &u.AuthEpoch)
	return u, e
}
func (s *Store) CreateSession(r SessionRecord) error {
	_, e := s.DB.Exec("DELETE FROM sessions WHERE expires_at<? OR revoked_at<?", time.Now().Unix()-7*86400, time.Now().Unix()-7*86400)
	if e != nil {
		return e
	}
	epoch := r.AuthEpoch
	if epoch <= 0 {
		epoch = 1
	}
	lastCheck := r.LastOriginCheck
	if lastCheck <= 0 {
		lastCheck = r.CreatedAt
	}
	_, e = s.DB.Exec(
		"INSERT INTO sessions(id,issuer_node_id,user_id,username,token_hash,csrf_hash,created_at,expires_at,revoked_at,device_info,auth_epoch,last_origin_check) VALUES(?,?,?,?,?,?,?,?,NULL,?,?,?)",
		r.ID, r.IssuerNodeID, r.UserID, r.Username, r.TokenHash, r.CSRFHash, r.CreatedAt, r.ExpiresAt, r.Device, epoch, lastCheck,
	)
	return e
}
func (s *Store) LookupSession(hash string) (SessionRecord, error) {
	var r SessionRecord
	e := s.DB.QueryRow(
		"SELECT id,issuer_node_id,user_id,username,expires_at,device_info,csrf_hash,created_at,auth_epoch,last_origin_check FROM sessions WHERE token_hash=? AND revoked_at IS NULL AND expires_at>?",
		hash, time.Now().Unix(),
	).Scan(&r.ID, &r.IssuerNodeID, &r.UserID, &r.Username, &r.ExpiresAt, &r.Device, &r.CSRFHash, &r.CreatedAt, &r.AuthEpoch, &r.LastOriginCheck)
	if e != nil {
		return r, e
	}
	if r.IssuerNodeID == "" {
		u, err := s.UserByID(r.UserID)
		if err != nil {
			return SessionRecord{}, err
		}
		if u.AuthEpoch > r.AuthEpoch {
			return SessionRecord{}, errors.New("sessão revogada após alteração de senha")
		}
		r.Username = u.Username
	}
	return r, nil
}
func (s *Store) RevokeToken(hash string) error {
	_, e := s.DB.Exec("UPDATE sessions SET revoked_at=? WHERE token_hash=?", time.Now().Unix(), hash)
	return e
}
func (s *Store) RotateSession(oldID string, r SessionRecord) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	result, e := tx.Exec("UPDATE sessions SET revoked_at=? WHERE id=? AND revoked_at IS NULL AND expires_at>?", time.Now().Unix(), oldID, time.Now().Unix())
	if e != nil {
		return e
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return errors.New("sessão já renovada ou expirada")
	}
	epoch := r.AuthEpoch
	if epoch <= 0 {
		epoch = 1
	}
	lastCheck := r.LastOriginCheck
	if lastCheck <= 0 {
		lastCheck = r.CreatedAt
	}
	_, e = tx.Exec(
		"INSERT INTO sessions(id,issuer_node_id,user_id,username,token_hash,csrf_hash,created_at,expires_at,revoked_at,device_info,auth_epoch,last_origin_check) VALUES(?,?,?,?,?,?,?,?,NULL,?,?,?)",
		r.ID, r.IssuerNodeID, r.UserID, r.Username, r.TokenHash, r.CSRFHash, r.CreatedAt, r.ExpiresAt, r.Device, epoch, lastCheck,
	)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) ListSessions() ([]SessionRecord, error) {
	rows, e := s.DB.Query("SELECT id,issuer_node_id,user_id,username,expires_at,device_info FROM sessions WHERE revoked_at IS NULL AND expires_at>?", time.Now().Unix())
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []SessionRecord{}
	for rows.Next() {
		var r SessionRecord
		if e = rows.Scan(&r.ID, &r.IssuerNodeID, &r.UserID, &r.Username, &r.ExpiresAt, &r.Device); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) RevokeSession(id string) error {
	_, e := s.DB.Exec("UPDATE sessions SET revoked_at=? WHERE id=?", time.Now().Unix(), id)
	return e
}
func (s *Store) SetUsernameAndRevoke(userID, username string) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	if _, e = tx.Exec("UPDATE users SET username=?,updated_at=?,auth_epoch=auth_epoch+1 WHERE id=?", username, now, userID); e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE sessions SET revoked_at=? WHERE issuer_node_id='' AND user_id=? AND revoked_at IS NULL", now, userID); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) SetPasswordAndRevoke(userID, hash string) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	if _, e = tx.Exec("UPDATE users SET password_hash=?,updated_at=?,auth_epoch=auth_epoch+1 WHERE id=?", hash, now, userID); e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE sessions SET revoked_at=? WHERE issuer_node_id='' AND user_id=? AND revoked_at IS NULL", now, userID); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) RevokeSessionsByIssuer(issuerNodeID string) error {
	_, e := s.DB.Exec("UPDATE sessions SET revoked_at=? WHERE issuer_node_id=? AND revoked_at IS NULL", time.Now().Unix(), issuerNodeID)
	return e
}
func (s *Store) RevokeSessionsByPrincipal(issuerNodeID, userID string) error {
	_, e := s.DB.Exec("UPDATE sessions SET revoked_at=? WHERE issuer_node_id=? AND user_id=? AND revoked_at IS NULL", time.Now().Unix(), issuerNodeID, userID)
	return e
}
func (s *Store) RevokeSessionsByEpoch(issuerNodeID, userID string, minEpoch int64) error {
	_, e := s.DB.Exec("UPDATE sessions SET revoked_at=? WHERE issuer_node_id=? AND user_id=? AND auth_epoch<? AND revoked_at IS NULL", time.Now().Unix(), issuerNodeID, userID, minEpoch)
	return e
}
func (s *Store) UpdateSessionOriginCheck(sessionID string, checkedAt int64) error {
	_, e := s.DB.Exec("UPDATE sessions SET last_origin_check=? WHERE id=? AND revoked_at IS NULL", checkedAt, sessionID)
	return e
}
