package cloudflare

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

//go:embed connector-release.json
var connectorRelease []byte

func (m *Manager) Install(ctx context.Context) error {
	if !m.op.TryLock() {
		return errors.New("operação em andamento")
	}
	defer m.op.Unlock()
	if _, err := m.Binary(); err == nil {
		return nil
	}
	assetName := "cloudflared-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		assetName += ".exe"
	}
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		return errors.New("instalação automática disponível em Windows e Linux")
	}
	var release struct {
		Version string            `json:"version"`
		SHA256  map[string]string `json:"sha256"`
	}
	if err := json.Unmarshal(connectorRelease, &release); err != nil {
		return err
	}
	expected := release.SHA256[assetName]
	if len(expected) != sha256.Size*2 {
		return errors.New("conector não incluído para esta arquitetura; instale o cloudflared oficial no PATH")
	}
	{
		req, e := http.NewRequestWithContext(ctx, "GET", "https://github.com/cloudflare/cloudflared/releases/download/"+release.Version+"/"+assetName, nil)
		if e != nil {
			return e
		}
		client := *m.Client.HTTP
		client.Timeout = 3 * time.Minute
		download, e := client.Do(req)
		if e != nil {
			return e
		}
		defer download.Body.Close()
		if download.StatusCode != 200 {
			return fmt.Errorf("falha ao baixar cloudflared (HTTP %d)", download.StatusCode)
		}
		dir := filepath.Join(m.DataDir, "bin")
		if e = os.MkdirAll(dir, 0700); e != nil {
			return e
		}
		file, e := os.CreateTemp(dir, "cloudflared-download-*")
		if e != nil {
			return e
		}
		temp := file.Name()
		defer os.Remove(temp)
		hash := sha256.New()
		n, e := io.Copy(io.MultiWriter(file, hash), io.LimitReader(download.Body, 128*1024*1024))
		file.Close()
		if e != nil {
			return e
		}
		if n == 128*1024*1024 {
			return errors.New("download ultrapassou limite")
		}
		if hex.EncodeToString(hash.Sum(nil)) != expected {
			return errors.New("checksum cloudflared inválido")
		}
		if e = os.Chmod(temp, 0700); e != nil {
			return e
		}
		name := "cloudflared"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if e = os.Rename(temp, filepath.Join(dir, name)); e != nil {
			return e
		}
		return nil
	}
}
