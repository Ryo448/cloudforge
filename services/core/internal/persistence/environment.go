package persistence

import (
	"cloudforge/core/internal/model"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zalando/go-keyring"
	"path/filepath"
)

type EnvironmentVault interface {
	Get(string) (string, error)
	Set(string, string) error
}

type osEnvironmentVault struct{ service string }

func (v osEnvironmentVault) Get(key string) (string, error) { return keyring.Get(v.service, key) }
func (v osEnvironmentVault) Set(key, value string) error    { return keyring.Set(v.service, key, value) }

func environmentVault(path string) EnvironmentVault {
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	return osEnvironmentVault{service: "Cloudforge-environment-" + hex.EncodeToString(sum[:8])}
}

func (s *Store) environmentKey(create bool) ([]byte, error) {
	s.envMu.Lock()
	defer s.envMu.Unlock()
	encoded, err := s.EnvironmentVault.Get("master")
	if errors.Is(err, keyring.ErrNotFound) && create {
		var existing int
		if err := s.DB.QueryRow("SELECT count(*) FROM settings WHERE key LIKE 'environment:%'").Scan(&existing); err != nil {
			return nil, err
		}
		if existing > 0 {
			return nil, errors.New("chave de variáveis não encontrada no cofre do sistema")
		}
		key := make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		if err = s.EnvironmentVault.Set("master", base64.StdEncoding.EncodeToString(key)); err != nil {
			return nil, err
		}
		return key, nil
	}
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		return nil, errors.New("chave de variáveis do sistema inválida")
	}
	return key, nil
}

func (s *Store) Environment(id string) ([]model.EnvironmentVariable, error) {
	var encoded string
	if err := s.Setting("environment:"+id, &encoded); err != nil {
		return nil, err
	}
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	key, err := s.environmentKey(false)
	if err != nil {
		return nil, fmt.Errorf("cofre de variáveis indisponível: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, errors.New("dados de variáveis inválidos")
	}
	raw, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], []byte(id))
	if err != nil {
		return nil, errors.New("não foi possível descriptografar as variáveis")
	}
	var values []model.EnvironmentVariable
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	return values, nil
}

func (s *Store) SetEnvironment(id string, values []model.EnvironmentVariable) error {
	raw, err := json.Marshal(values)
	if err != nil {
		return err
	}
	key, err := s.environmentKey(true)
	if err != nil {
		return fmt.Errorf("cofre de variáveis indisponível: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	sealed := gcm.Seal(nonce, nonce, raw, []byte(id))
	return s.Set("environment:"+id, base64.StdEncoding.EncodeToString(sealed))
}
