package cluster

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"github.com/zalando/go-keyring"
	"math/big"
	"net/url"
	"strings"
	"time"
)

const (
	invitePrefix       = "cfpair1."
	privateKeyVaultKey = "node-identity-private-key"
)

// KeyVault protects the node's private identity key in OS secure storage.
type KeyVault interface {
	Get(string) (string, error)
	Set(string, string) error
	Delete(string) error
}

type OSKeyVault struct {
	Installation string
}

func (v OSKeyVault) service() string {
	return "Cloudforge-cluster-" + v.Installation
}

func (v OSKeyVault) Get(key string) (string, error) {
	return keyring.Get(v.service(), key)
}

func (v OSKeyVault) Set(key, value string) error {
	return keyring.Set(v.service(), key, value)
}

func (v OSKeyVault) Delete(key string) error {
	err := keyring.Delete(v.service(), key)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// Identity holds a node's cryptographic keypair and self-signed mTLS certificate.
type Identity struct {
	NodeID       string
	PrivateKey   *ecdsa.PrivateKey
	PublicKey    *ecdsa.PublicKey
	PublicKeyPEM string
	Fingerprint  string
	TLSCert      tls.Certificate
}

func generateIdentity(nodeID string) (*Identity, string, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, "", err
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, "", err
	}
	privPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}))
	id, err := buildIdentity(nodeID, priv)
	if err != nil {
		return nil, "", err
	}
	return id, privPEM, nil
}

func loadIdentity(nodeID, privPEM string) (*Identity, error) {
	block, _ := pem.Decode([]byte(privPEM))
	if block == nil {
		return nil, errors.New("chave privada da instalação inválida")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("chave privada da instalação inválida: %w", err)
	}
	priv, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || priv.Curve != elliptic.P256() {
		return nil, errors.New("tipo de chave privada da instalação incompatível")
	}
	return buildIdentity(nodeID, priv)
}

func buildIdentity(nodeID string, priv *ecdsa.PrivateKey) (*Identity, error) {
	spki, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return nil, err
	}
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: spki}))
	fp := fingerprintBytes(spki)

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber:          serial.Add(serial, big.NewInt(1)),
		Subject:               pkix.Name{CommonName: nodeID, Organization: []string{"Cloudforge Node"}},
		NotBefore:             now.Add(-10 * time.Minute),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &Identity{
		NodeID:       nodeID,
		PrivateKey:   priv,
		PublicKey:    &priv.PublicKey,
		PublicKeyPEM: pubPEM,
		Fingerprint:  fp,
		TLSCert: tls.Certificate{
			Certificate: [][]byte{der},
			PrivateKey:  priv,
			Leaf:        leaf,
		},
	}, nil
}

func fingerprintBytes(spki []byte) string {
	sum := sha256.Sum256(spki)
	return hex.EncodeToString(sum[:])
}

func parsePublicKeyPEM(pubPEM string) (*ecdsa.PublicKey, string, error) {
	block, _ := pem.Decode([]byte(pubPEM))
	if block == nil {
		return nil, "", errors.New("chave pública inválida")
	}
	raw, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, "", errors.New("chave pública inválida")
	}
	pub, ok := raw.(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		return nil, "", errors.New("algoritmo de chave pública não suportado")
	}
	spki, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, "", err
	}
	return pub, fingerprintBytes(spki), nil
}

func verifyPeerSelfSignedCert(rawCert []byte) (*x509.Certificate, *ecdsa.PublicKey, string, error) {
	cert, err := x509.ParseCertificate(rawCert)
	if err != nil {
		return nil, nil, "", errors.New("certificado mTLS inválido")
	}
	now := time.Now().UTC()
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return nil, nil, "", errors.New("certificado mTLS expirado ou ainda não válido")
	}
	if err := cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature); err != nil {
		return nil, nil, "", errors.New("assinatura do certificado mTLS inválida")
	}
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		return nil, nil, "", errors.New("chave pública do certificado mTLS inválida")
	}
	spki, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, nil, "", err
	}
	return cert, pub, fingerprintBytes(spki), nil
}

// VerificationCode computes a short authentication string independently on both
// computers so the administrator can compare them before approving pairing.
func VerificationCode(primaryID, primaryFP, nodeID, nodeFP, inviteID string) string {
	input := fmt.Sprintf("cloudforge-pair-v1:%s:%s:%s:%s:%s", primaryID, primaryFP, nodeID, nodeFP, inviteID)
	sum := sha256.Sum256([]byte(input))
	hexStr := strings.ToUpper(hex.EncodeToString(sum[:4]))
	return hexStr[:4] + "-" + hexStr[4:8]
}

func secretDigest(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func constantTimeEqual(a, b string) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// InvitePayload is encoded inside the single-use invite token shared from Y to X.
type InvitePayload struct {
	Version            int    `json:"v"`
	InviteID           string `json:"inviteId"`
	PrimaryURL         string `json:"primaryUrl"`
	PrimaryID          string `json:"primaryId"`
	PrimaryFingerprint string `json:"primaryFingerprint"`
	PrimaryName        string `json:"primaryName"`
	Secret             string `json:"secret"`
	ExpiresAt          int64  `json:"expiresAt"`
}

func EncodeInvite(p InvitePayload) (string, error) {
	p.Version = ProtocolVersion
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return invitePrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeInvite(token string, allowInsecureHTTP bool) (InvitePayload, error) {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, invitePrefix) {
		return InvitePayload{}, errors.New("convite inválido; copie o código completo gerado no computador principal")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, invitePrefix))
	if err != nil || len(raw) > 8192 {
		return InvitePayload{}, errors.New("convite malformado")
	}
	var p InvitePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return InvitePayload{}, errors.New("convite malformado")
	}
	if p.Version != ProtocolVersion {
		return InvitePayload{}, errors.New("versão de convite incompatível")
	}
	if p.InviteID == "" || p.PrimaryID == "" || len(p.PrimaryFingerprint) != 64 || len(p.Secret) < 32 {
		return InvitePayload{}, errors.New("convite incompleto")
	}
	u, err := url.Parse(strings.TrimSpace(p.PrimaryURL))
	if err != nil || u.Host == "" {
		return InvitePayload{}, errors.New("endereço do computador principal inválido no convite")
	}
	if u.Scheme != "https" && !(allowInsecureHTTP && u.Scheme == "http") {
		return InvitePayload{}, errors.New("o endereço do painel principal deve usar HTTPS")
	}
	p.PrimaryURL = strings.TrimRight(u.String(), "/")
	return p, nil
}

func canonicalStepUpPayload(g StepUpGrant) []byte {
	return []byte(fmt.Sprintf("cloudforge-stepup-v1\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%d\x00%d",
		g.GrantID, g.IssuerNodeID, g.UserID, g.TargetNodeID, g.ResourceID, g.Action, g.IssuedAt, g.ExpiresAt))
}

func SignStepUpGrant(priv *ecdsa.PrivateKey, g *StepUpGrant) error {
	if priv == nil {
		return errors.New("identidade criptográfica indisponível")
	}
	hash := sha256.Sum256(canonicalStepUpPayload(*g))
	sig, err := ecdsa.SignASN1(rand.Reader, priv, hash[:])
	if err != nil {
		return err
	}
	g.Signature = base64.RawURLEncoding.EncodeToString(sig)
	return nil
}

func VerifyStepUpGrant(pub *ecdsa.PublicKey, g StepUpGrant) error {
	if pub == nil {
		return errors.New("chave pública do principal indisponível")
	}
	if g.GrantID == "" || g.IssuerNodeID == "" || g.UserID == "" || g.TargetNodeID == "" || g.ResourceID == "" || g.Action == "" {
		return errors.New("autorização sensível incompleta")
	}
	now := time.Now().Unix()
	if g.ExpiresAt < now-30 || g.IssuedAt > now+60 || g.ExpiresAt-g.IssuedAt > 120 {
		return errors.New("autorização sensível expirada ou inválida")
	}
	sig, err := base64.RawURLEncoding.DecodeString(g.Signature)
	if err != nil {
		return errors.New("assinatura da autorização sensível inválida")
	}
	hash := sha256.Sum256(canonicalStepUpPayload(g))
	if !ecdsa.VerifyASN1(pub, hash[:], sig) {
		return errors.New("assinatura da autorização sensível rejeitada")
	}
	return nil
}
