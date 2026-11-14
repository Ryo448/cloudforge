package databases

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type engine struct {
	Image string
	Port  int
	Data  string
}

var engines = map[string]engine{
	"postgresql": {"postgres:17.9-bookworm", 5432, "/var/lib/postgresql/data"},
	"mysql":      {"mysql:8.4.8", 3306, "/var/lib/mysql"},
	"mongodb":    {"mongo:8.0.20-noble", 27017, "/data/db"},
	"redis":      {"redis:7.4.8-bookworm", 6379, "/data"},
}

const (
	Username        = "cloudforge"
	DatabaseName    = "cloudforge"
	localHost       = "127.0.0.1"
	memoryLimit     = 1024 * 1024 * 1024
	defaultMemoryMB = 1024
	cpuLimit        = 1.0
)

type Input struct {
	Name     string `json:"name"`
	Engine   string `json:"engine"`
	MemoryMB *int   `json:"memoryMb"`
}

type Instance struct {
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	Engine             string     `json:"engine"`
	MemoryMB           *int       `json:"memoryMb,omitempty"`
	Host               string     `json:"host"`
	Port               int        `json:"port"`
	State              string     `json:"state"`
	Issue              string     `json:"issue,omitempty"`
	Desired            string     `json:"-"`
	StartedAt          *time.Time `json:"startedAt"`
	CreatedAt          time.Time  `json:"createdAt"`
	URL                string     `json:"url"`
	CredentialsPending bool       `json:"credentialsPending"`
	TLSValidated       bool       `json:"tlsValidated"`
	CanRotateTLS       bool       `json:"canRotateTls,omitempty"`
	Latest             *Metric    `json:"latest"`
}

type Metric struct {
	Time        int64   `json:"time"`
	CPU         float64 `json:"cpu"`
	Memory      uint64  `json:"memory"`
	MemoryLimit uint64  `json:"memoryLimit"`
	RX          uint64  `json:"rx"`
	TX          uint64  `json:"tx"`
	Storage     *uint64 `json:"storage,omitempty"`
}

type Credentials struct {
	Password string `json:"password"`
	URL      string `json:"url"`
}

type secret struct {
	Admin           string
	Password        string
	PendingPassword string
	Reveal          bool
	TLS             *serverTLS
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func validName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if n := len([]rune(s)); n < 1 || n > 60 {
		return "", errors.New("informe um nome de 1 a 60 caracteres")
	}
	for _, c := range s {
		if !unicode.IsLetter(c) && !unicode.IsNumber(c) && !strings.ContainsRune(" -_.", c) {
			return "", errors.New("use letras, números, espaços, pontos, hífens ou sublinhados")
		}
	}
	return s, nil
}

func (i Instance) connection(password string) string {
	if i.Port == 0 {
		return ""
	}
	u := url.URL{
		Scheme: i.Engine, User: url.UserPassword(Username, password),
		Host: net.JoinHostPort(localHost, strconv.Itoa(i.Port)),
		Path: "/" + DatabaseName,
	}
	switch i.Engine {
	case "postgresql":
		u.RawQuery = "sslmode=verify-full"
	case "mysql":
		u.RawQuery = "ssl-mode=VERIFY_IDENTITY"
	case "mongodb":
		u.RawQuery = "authSource=" + DatabaseName + "&tls=true"
	case "redis":
		u.Scheme = "rediss"
		u.Path = "/0"
	}
	if password == "" {
		u.User = url.UserPassword(Username, "PASSWORD")
	}
	return u.String()
}
