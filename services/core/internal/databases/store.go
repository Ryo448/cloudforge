package databases

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type record struct {
	Instance
	Owner        string
	Action       string
	ReservedPort int
}

type stored struct {
	View    Instance
	Desired string
	Action  string
}

func (r record) encode() ([]byte, error) {
	view := r.Instance
	view.Latest = nil
	view.LANHosts = nil
	view.LANFirewall = ""
	view.CanRotateTLS = false
	view.URL = view.connection("")
	return json.Marshal(stored{view, r.Desired, r.Action})
}

func loadRow(row interface{ Scan(...any) error }) (record, error) {
	var r record
	var raw []byte
	var port sql.NullInt64
	if err := row.Scan(&r.Owner, &port, &raw); err != nil {
		return r, err
	}
	var data stored
	if err := json.Unmarshal(raw, &data); err != nil {
		return r, err
	}
	r.Instance = data.View
	r.Desired, r.Action, r.ReservedPort = data.Desired, data.Action, int(port.Int64)
	return r, nil
}

func (m *Manager) get(owner, id string) (record, error) {
	r, err := loadRow(m.db.QueryRow("SELECT owner,port,data FROM hosted_databases WHERE id=? AND owner=?", id, owner))
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func (m *Manager) records(owner string) ([]record, error) {
	query := "SELECT owner,port,data FROM hosted_databases"
	args := []any{}
	if owner != "" {
		query += " WHERE owner=?"
		args = append(args, owner)
	}
	rows, err := m.db.Query(query+" ORDER BY id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []record{}
	for rows.Next() {
		r, err := loadRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func (m *Manager) save(r record) error {
	raw, err := r.encode()
	if err != nil {
		return err
	}
	_, err = m.db.Exec("UPDATE hosted_databases SET data=? WHERE id=?", raw, r.ID)
	return err
}

func (m *Manager) setPort(r *record, port int) error {
	_, err := m.db.Exec("UPDATE hosted_databases SET port=NULLIF(?,0) WHERE id=?", port, r.ID)
	if err != nil {
		return conflict(err)
	}
	r.ReservedPort = port
	return nil
}

func (m *Manager) secrets(r record) (secret, error) {
	var raw []byte
	var out secret
	if err := m.db.QueryRow("SELECT secret FROM hosted_databases WHERE id=?", r.ID).Scan(&raw); err != nil {
		return out, err
	}
	if len(raw) == 0 {
		return out, nil
	}
	var box cipher.AEAD
	var err error
	local := bytes.HasPrefix(raw, []byte(localSecretPrefix))
	if local {
		box, err = newSecretBox(m.localKey[:])
		raw = raw[len(localSecretPrefix):]
	} else {
		box, err = m.legacySecretBox()
	}
	if err != nil {
		return out, err
	}
	if len(raw) < box.NonceSize() {
		return out, errors.New("credenciais criptografadas inválidas")
	}
	bytes, err := box.Open(nil, raw[:box.NonceSize()], raw[box.NonceSize():], []byte(r.ID))
	if err != nil {
		return out, errors.New("falha ao abrir credenciais protegidas")
	}
	if err = json.Unmarshal(bytes, &out); err != nil {
		return out, err
	}
	if !local {
		err = m.saveSecrets(r, out)
	}
	return out, err
}

func (m *Manager) saveSecrets(r record, s secret) error {
	raw, err := m.encryptSecrets(r, s)
	if err != nil {
		return err
	}
	_, err = m.db.Exec("UPDATE hosted_databases SET secret=? WHERE id=?", raw, r.ID)
	return err
}

func (m *Manager) saveRecordAndSecrets(r record, s secret) error {
	raw, err := m.encryptSecrets(r, s)
	if err != nil {
		return err
	}
	data, err := r.encode()
	if err != nil {
		return err
	}
	_, err = m.db.Exec("UPDATE hosted_databases SET secret=?,data=? WHERE id=?", raw, data, r.ID)
	return err
}

func (m *Manager) encryptSecrets(r record, s secret) ([]byte, error) {
	box, err := newSecretBox(m.localKey[:])
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, box.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return append([]byte(localSecretPrefix), box.Seal(nonce, nonce, raw, []byte(r.ID))...), nil
}

const localSecretPrefix = "cloudforge-db-v1\x00"

func deriveLocalKey(installationKey string) [32]byte {
	mac := hmac.New(sha256.New, []byte(installationKey))
	_, _ = mac.Write([]byte(localSecretPrefix))
	return [32]byte(mac.Sum(nil))
}

func (m *Manager) legacySecretBox() (cipher.AEAD, error) {
	m.keyMu.Lock()
	defer m.keyMu.Unlock()
	stored, err := m.vault.Get("hosted-database-master")
	if err != nil {
		return nil, fmt.Errorf("cofre do sistema indisponível: %w", err)
	}
	key, err := base64.StdEncoding.DecodeString(stored)
	if err != nil || len(key) != 32 {
		return nil, errors.New("chave dos bancos inválida")
	}
	return newSecretBox(key)
}

func newSecretBox(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (m *Manager) history(r record, window time.Duration) ([]Metric, error) {
	rows, err := m.db.Query("SELECT data FROM hosted_database_metrics WHERE database_id=? AND sampled_at>=? ORDER BY sampled_at", r.ID, time.Now().Add(-window).UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	samples := []Metric{}
	for rows.Next() {
		var raw []byte
		var s Metric
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		samples = append(samples, s)
	}
	return samples, rows.Err()
}

func (m *Manager) addMetric(r record, s Metric) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if _, err = m.db.Exec("INSERT OR REPLACE INTO hosted_database_metrics VALUES(?,?,?)", r.ID, s.Time, raw); err != nil {
		return err
	}
	_, err = m.db.Exec("DELETE FROM hosted_database_metrics WHERE database_id=? AND sampled_at<?", r.ID, time.Now().Add(-24*time.Hour).UnixMilli())
	return err
}
