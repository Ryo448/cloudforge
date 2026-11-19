package databases

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"
)

type certificate struct {
	PEM []byte
	Key []byte
}

type serverTLS struct {
	certificate
	CA     []byte
	Client *certificate `json:",omitempty"`
}

func issueCertificate(template, issuer *x509.Certificate, signer *rsa.PrivateKey) (certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 159))
	if err != nil {
		return certificate{}, err
	}
	template.SerialNumber = serial.Add(serial, big.NewInt(1))
	if signer == nil {
		signer = key
	}
	der, err := x509.CreateCertificate(rand.Reader, template, issuer, &key.PublicKey, signer)
	if err != nil {
		return certificate{}, err
	}
	return certificate{
		PEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		Key: pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}),
	}, nil
}

func (m *Manager) authority() (certificate, error) {
	m.keyMu.Lock()
	defer m.keyMu.Unlock()
	box, err := newSecretBox(m.localKey[:])
	if err != nil {
		return certificate{}, err
	}
	aad := []byte("cloudforge-database-tls-ca")
	var raw []byte
	err = m.db.QueryRow("SELECT material FROM database_tls_authority WHERE singleton=1").Scan(&raw)
	if err == nil {
		var ca certificate
		if len(raw) < box.NonceSize() {
			return ca, errors.New("CA local inválida")
		}
		plain, err := box.Open(nil, raw[:box.NonceSize()], raw[box.NonceSize():], aad)
		if err != nil {
			return ca, errors.New("não foi possível abrir a CA local")
		}
		err = json.Unmarshal(plain, &ca)
		return ca, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return certificate{}, err
	}
	now := time.Now()
	template := &x509.Certificate{
		Subject:   pkix.Name{CommonName: "Cloudforge Database CA"},
		NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(20, 0, 0),
		IsCA: true, BasicConstraintsValid: true, MaxPathLenZero: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	ca, err := issueCertificate(template, template, nil)
	if err != nil {
		return certificate{}, err
	}
	plain, err := json.Marshal(ca)
	if err != nil {
		return certificate{}, err
	}
	nonce := make([]byte, box.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return certificate{}, err
	}
	_, err = m.db.Exec("INSERT INTO database_tls_authority VALUES (1,?)", box.Seal(nonce, nonce, plain, aad))
	return ca, err
}

func (m *Manager) tlsSigner() ([]byte, *x509.Certificate, *rsa.PrivateKey, error) {
	ca, err := m.authority()
	if err != nil {
		return nil, nil, nil, err
	}
	pair, err := tls.X509KeyPair(ca.PEM, ca.Key)
	if err != nil || pair.Leaf == nil || !pair.Leaf.IsCA {
		return nil, nil, nil, errors.New("par de chaves da CA local inválido")
	}
	signer, ok := pair.PrivateKey.(*rsa.PrivateKey)
	if !ok {
		return nil, nil, nil, errors.New("tipo de chave da CA local inválido")
	}
	return ca.PEM, pair.Leaf, signer, nil
}

func (m *Manager) issueTLS(r record) (*serverTLS, error) {
	ca, issuer, signer, err := m.tlsSigner()
	if err != nil {
		return nil, err
	}
	var addresses []net.IP
	if r.Engine == "postgresql" {
		addresses, err = postgresLANAddresses()
		if err != nil {
			return nil, err
		}
	}
	leaf, err := issueServerTLS(r, issuer, signer, addresses)
	if err != nil {
		return nil, err
	}
	client, err := issueClientTLS(r, issuer, signer)
	if err != nil {
		return nil, err
	}
	result := &serverTLS{certificate: leaf, CA: ca, Client: client}
	return result, result.validate(r)
}

func issueServerTLS(r record, issuer *x509.Certificate, signer *rsa.PrivateKey, addresses []net.IP) (certificate, error) {
	now := time.Now()
	template := &x509.Certificate{
		Subject:   pkix.Name{CommonName: containerName(r)},
		NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(2, 0, 0),
		DNSNames:    []string{"localhost", containerName(r)},
		IPAddresses: append([]net.IP{net.ParseIP(localHost), net.IPv6loopback}, addresses...),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	template.NotAfter = minTime(template.NotAfter, issuer.NotAfter)
	return issueCertificate(template, issuer, signer)
}

func postgresLANAddresses() ([]net.IP, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, errors.New("não foi possível consultar os endereços de rede do PostgreSQL")
	}
	var addresses []net.IP
	for _, adapter := range interfaces {
		name := strings.ToLower(adapter.Name)
		// Container and WSL interfaces come and go as workloads start.
		dockerBridge := strings.HasPrefix(name, "br-") && len(name) == 15
		if adapter.Flags&net.FlagUp == 0 || adapter.Flags&(net.FlagLoopback|net.FlagPointToPoint) != 0 ||
			name == "docker0" || dockerBridge || strings.HasPrefix(name, "vethernet (wsl") || name == "vethernet (default switch)" {
			continue
		}
		assigned, err := adapter.Addrs()
		if err != nil {
			continue // An interface can disappear between enumeration and lookup.
		}
		for _, address := range assigned {
			network, ok := address.(*net.IPNet)
			if !ok {
				continue
			}
			ip := network.IP.To4()
			if ip != nil && (ip.IsGlobalUnicast() || ip.IsLinkLocalUnicast()) {
				addresses = append(addresses, ip)
			}
		}
	}
	slices.SortFunc(addresses, func(a, b net.IP) int { return bytes.Compare(a, b) })
	return slices.CompactFunc(addresses, net.IP.Equal), nil
}

func (m *Manager) postgresTLSForLAN(r record, material *serverTLS, addresses []net.IP) (*serverTLS, error) {
	pair, err := tls.X509KeyPair(material.PEM, material.Key)
	if err != nil {
		return nil, err
	}
	missingAddress := slices.ContainsFunc(addresses, func(ip net.IP) bool {
		return pair.Leaf.VerifyHostname(ip.String()) != nil
	})
	if !missingAddress {
		return material, nil
	}
	ca, issuer, signer, err := m.tlsSigner()
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(ca, material.CA) {
		return nil, errors.New("a CA do banco é diferente da CA local")
	}
	leaf, err := issueServerTLS(r, issuer, signer, addresses)
	if err != nil {
		return nil, err
	}
	updated := *material
	updated.certificate = leaf
	return &updated, updated.validate(r)
}

func clientCommonName(r record) string { return containerName(r) + "-client" }

func issueClientTLS(r record, issuer *x509.Certificate, signer *rsa.PrivateKey) (*certificate, error) {
	now := time.Now()
	template := &x509.Certificate{
		Subject:   pkix.Name{CommonName: clientCommonName(r)},
		NotBefore: now.Add(-5 * time.Minute), NotAfter: minTime(now.AddDate(2, 0, 0), issuer.NotAfter),
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	pair, err := issueCertificate(template, issuer, signer)
	return &pair, err
}

// Backfill only the client pair for an instance already provisioned with TLS.
func (m *Manager) backfillClientTLS(r record, s secret) (secret, error) {
	if s.TLS == nil || s.TLS.Client != nil {
		return s, nil
	}
	ca, issuer, signer, err := m.tlsSigner()
	if err != nil {
		return s, err
	}
	if !bytes.Equal(ca, s.TLS.CA) {
		return s, errors.New("a CA do banco é diferente da CA local")
	}
	material := *s.TLS
	material.Client, err = issueClientTLS(r, issuer, signer)
	if err != nil {
		return s, err
	}
	if _, err = material.clientPair(r); err != nil {
		return s, err
	}
	s.TLS = &material
	return s, m.saveSecrets(r, s)
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (s *serverTLS) validate(r record) error {
	if s == nil {
		return errors.New("certificado TLS ausente")
	}
	pair, err := tls.X509KeyPair(s.PEM, s.Key)
	if err != nil {
		return errors.New("certificado e chave TLS incompatíveis")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(s.CA) {
		return errors.New("certificado da CA inválido")
	}
	for _, host := range []string{localHost, "localhost", containerName(r)} {
		if _, err = pair.Leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: host}); err != nil {
			return errors.New("certificado TLS inválido para a instância")
		}
	}
	_, err = s.clientPair(r)
	return err
}

func (s *serverTLS) clientPair(r record) (tls.Certificate, error) {
	if s.Client == nil {
		return tls.Certificate{}, errors.New("certificado TLS de cliente ausente")
	}
	pair, err := tls.X509KeyPair(s.Client.PEM, s.Client.Key)
	if err != nil {
		return tls.Certificate{}, errors.New("certificado e chave TLS de cliente incompatíveis")
	}
	leaf := pair.Leaf
	if leaf.IsCA || leaf.Subject.CommonName != clientCommonName(r) ||
		leaf.KeyUsage&x509.KeyUsageDigitalSignature == 0 || len(leaf.ExtKeyUsage) != 1 || leaf.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		return tls.Certificate{}, errors.New("certificado TLS de cliente inválido para a instância")
	}
	// Never allow a server or CA key to become downloadable, even if it is
	// accidentally reused when issuing the client certificate.
	for _, publicPEM := range [][]byte{s.PEM, s.CA} {
		block, _ := pem.Decode(publicPEM)
		if block == nil {
			return tls.Certificate{}, errors.New("certificado TLS da instância inválido")
		}
		public, err := x509.ParseCertificate(block.Bytes)
		if err != nil || bytes.Equal(leaf.RawSubjectPublicKeyInfo, public.RawSubjectPublicKeyInfo) {
			return tls.Certificate{}, errors.New("chave TLS de cliente inválida para a instância")
		}
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(s.CA)
	if _, err = leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return tls.Certificate{}, errors.New("certificado TLS de cliente não validado pela CA")
	}
	return pair, nil
}

// dialTLS checks the exact certificate on the published port. PostgreSQL and
// MySQL negotiate TLS in their wire protocol before the TLS handshake.
func dialTLS(ctx context.Context, r record, s *serverTLS, port int) (*tls.Conn, error) {
	return dialDatabaseTLS(ctx, r, s, port, nil)
}

func dialDatabaseTLS(ctx context.Context, r record, s *serverTLS, port int, client *tls.Certificate) (*tls.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp4", net.JoinHostPort(localHost, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	if err = negotiateTLS(conn, r.Engine); err != nil {
		conn.Close()
		return nil, err
	}
	config := s.probeTLSConfig()
	requested := false
	if client != nil {
		// TLS 1.2 completes only after the server has verified the client
		// certificate. With TLS 1.3 the client can return before that check.
		config.MaxVersion = tls.VersionTLS12
		config.GetClientCertificate = func(request *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			requested = true
			return client, request.SupportsCertificate(client)
		}
	}
	secure := tls.Client(conn, config)
	if err = secure.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	if client != nil && !requested {
		secure.Close()
		return nil, errClientTLSNotRequested
	}
	return secure, nil
}

func (s *serverTLS) probeTLSConfig() *tls.Config {
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(s.CA)
	expected, _ := pem.Decode(s.PEM)
	return &tls.Config{
		MinVersion: tls.VersionTLS12, ServerName: localHost, RootCAs: roots,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if expected == nil || len(cs.PeerCertificates) == 0 || !bytes.Equal(cs.PeerCertificates[0].Raw, expected.Bytes) {
				return errors.New("o banco apresentou um certificado TLS diferente do esperado")
			}
			return nil
		},
	}
}

var errClientTLSNotRequested = errors.New("o banco não solicitou o certificado TLS de cliente")

func negotiateTLS(conn net.Conn, engine string) error {
	switch engine {
	case "postgresql":
		// SSLRequest: length 8, request code 80877103.
		if _, err := conn.Write([]byte{0, 0, 0, 8, 4, 210, 22, 47}); err != nil {
			return err
		}
		var reply [1]byte
		if _, err := io.ReadFull(conn, reply[:]); err != nil || reply[0] != 'S' {
			return errors.New("PostgreSQL não aceitou TLS")
		}
	case "mysql":
		var header [4]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			return err
		}
		size := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
		if size < 18 || size > 4096 || header[3] != 0 {
			return errors.New("saudação inválida do MySQL")
		}
		packet := make([]byte, size)
		if _, err := io.ReadFull(conn, packet); err != nil {
			return err
		}
		versionEnd := bytes.IndexByte(packet[1:], 0) + 1
		flagsOffset := versionEnd + 14
		const flags = (1 << 9) | (1 << 11) // CLIENT_PROTOCOL_41 | CLIENT_SSL
		if packet[0] != 10 || versionEnd == 0 || flagsOffset+2 > len(packet) ||
			binary.LittleEndian.Uint16(packet[flagsOffset:])&flags != flags {
			return errors.New("MySQL não anunciou suporte a TLS")
		}
		var request [36]byte
		request[0], request[3], request[12] = 32, 1, 45
		binary.LittleEndian.PutUint32(request[4:], flags)
		binary.LittleEndian.PutUint32(request[8:], 1<<24)
		_, err := conn.Write(request[:])
		return err
	}
	return nil
}

func (m *Manager) Certificate(owner, id, format string) ([]byte, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.get(owner, id)
	if err != nil {
		return nil, "", err
	}
	if format != "pem" && format != "crt" && format != "key" {
		return nil, "", ErrNotFound
	}
	if !m.tlsAvailable(r) {
		return nil, "", ErrConflict
	}
	s, err := m.secrets(r)
	if err != nil {
		return nil, "", err
	}
	if s.TLS == nil || s.TLS.Client == nil {
		return nil, "", ErrConflict
	}
	if _, err = s.TLS.clientPair(r); err != nil {
		return nil, "", ErrConflict
	}
	switch format {
	case "crt":
		return s.TLS.CA, "ca.crt", nil
	case "key":
		return s.TLS.Client.Key, "client.key", nil
	default:
		return s.TLS.Client.PEM, "client.pem", nil
	}
}
