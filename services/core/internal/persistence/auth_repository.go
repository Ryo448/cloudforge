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
}
type SessionRecord struct {
	ID        string `json:"id"`
	UserID    string `json:"-"`
	Username  string `json:"username"`
	ExpiresAt int64  `json:"expiresAt"`
	Device    string `json:"device"`
	CSRFHash  string `json:"-"`
	TokenHash string `json:"-"`
	CreatedAt int64  `json:"-"`
}
type AuthRepository interface {
	HasUsers() (bool, error)
	CreateAdmin(User) error
	UserByUsername(string) (User, error)
	Admin() (User, error)
	CreateSession(SessionRecord) error
	LookupSession(string) (SessionRecord, error)
	RevokeToken(string) error
	RotateSession(string, SessionRecord) error
	ListSessions() ([]SessionRecord, error)
	RevokeSession(string) error
	SetPasswordAndRevoke(string, string) error
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
	if _, e = tx.Exec("INSERT INTO users VALUES(?,?,?,?,?)", u.ID, u.Username, u.PasswordHash, u.CreatedAt, u.CreatedAt); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) UserByUsername(name string) (User, error) {
	var u User
	e := s.DB.QueryRow("SELECT id,username,password_hash FROM users WHERE username=?", name).Scan(&u.ID, &u.Username, &u.PasswordHash)
	return u, e
}
func (s *Store) Admin() (User, error) {
	var u User
	e := s.DB.QueryRow("SELECT id,username,password_hash FROM users LIMIT 1").Scan(&u.ID, &u.Username, &u.PasswordHash)
	return u, e
}
func (s *Store) CreateSession(r SessionRecord) error {
	_, e := s.DB.Exec("DELETE FROM sessions WHERE expires_at<? OR revoked_at<?", time.Now().Unix()-7*86400, time.Now().Unix()-7*86400)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec("INSERT INTO sessions VALUES(?,?,?,?,?,?,NULL,?)", r.ID, r.UserID, r.TokenHash, r.CSRFHash, r.CreatedAt, r.ExpiresAt, r.Device)
	return e
}
func (s *Store) LookupSession(hash string) (SessionRecord, error) {
	var r SessionRecord
	e := s.DB.QueryRow("SELECT s.id,s.user_id,u.username,s.expires_at,s.device_info,s.csrf_hash FROM sessions s JOIN users u ON u.id=s.user_id WHERE token_hash=? AND revoked_at IS NULL AND expires_at>?", hash, time.Now().Unix()).Scan(&r.ID, &r.UserID, &r.Username, &r.ExpiresAt, &r.Device, &r.CSRFHash)
	return r, e
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
	_, e = tx.Exec("INSERT INTO sessions VALUES(?,?,?,?,?,?,NULL,?)", r.ID, r.UserID, r.TokenHash, r.CSRFHash, r.CreatedAt, r.ExpiresAt, r.Device)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) ListSessions() ([]SessionRecord, error) {
	rows, e := s.DB.Query("SELECT s.id,u.username,s.expires_at,s.device_info FROM sessions s JOIN users u ON u.id=s.user_id WHERE revoked_at IS NULL AND expires_at>?", time.Now().Unix())
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []SessionRecord{}
	for rows.Next() {
		var r SessionRecord
		if e = rows.Scan(&r.ID, &r.Username, &r.ExpiresAt, &r.Device); e != nil {
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
func (s *Store) SetPasswordAndRevoke(userID, hash string) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("UPDATE users SET password_hash=?,updated_at=? WHERE id=?", hash, time.Now().Unix(), userID); e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE sessions SET revoked_at=? WHERE user_id=?", time.Now().Unix(), userID); e != nil {
		return e
	}
	return tx.Commit()
}
