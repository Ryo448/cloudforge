package cluster

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"github.com/gorilla/websocket"
	"io"
	"net"
	"sync"
	"time"
)

// wsNetConn adapts a gorilla/websocket connection to net.Conn so standard TLS 1.3
// mutual authentication runs end-to-end inside the WebSocket channel.
type wsNetConn struct {
	ws      *websocket.Conn
	readMu  sync.Mutex
	writeMu sync.Mutex
	reader  io.Reader
}

func newWSNetConn(ws *websocket.Conn) *wsNetConn {
	ws.SetReadLimit(MaxFrameSize + 64*1024)
	return &wsNetConn{ws: ws}
}

func (c *wsNetConn) Read(p []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	for {
		if c.reader == nil {
			msgType, r, err := c.ws.NextReader()
			if err != nil {
				return 0, err
			}
			if msgType != websocket.BinaryMessage {
				return 0, errors.New("tipo de quadro WebSocket inesperado no canal mTLS")
			}
			c.reader = r
		}
		n, err := c.reader.Read(p)
		if errors.Is(err, io.EOF) {
			c.reader = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (c *wsNetConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.ws.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *wsNetConn) Close() error                       { return c.ws.Close() }
func (c *wsNetConn) LocalAddr() net.Addr                { return c.ws.LocalAddr() }
func (c *wsNetConn) RemoteAddr() net.Addr               { return c.ws.RemoteAddr() }
func (c *wsNetConn) SetReadDeadline(t time.Time) error  { return c.ws.SetReadDeadline(t) }
func (c *wsNetConn) SetWriteDeadline(t time.Time) error { return c.ws.SetWriteDeadline(t) }
func (c *wsNetConn) SetDeadline(t time.Time) error {
	if err := c.ws.SetReadDeadline(t); err != nil {
		return err
	}
	return c.ws.SetWriteDeadline(t)
}

func serverTLSConfig(id *Identity) *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{id.TLSCert},
		ClientAuth:   tls.RequireAnyClientCert,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("certificado de cliente obrigatório")
			}
			_, _, _, err := verifyPeerSelfSignedCert(rawCerts[0])
			return err
		},
	}
}

func clientTLSConfig(id *Identity, expectedPrimaryFingerprint string) *tls.Config {
	return &tls.Config{
		MinVersion:         tls.VersionTLS13,
		Certificates:       []tls.Certificate{id.TLSCert},
		InsecureSkipVerify: true, // Verified strictly by pinned primary public key fingerprint below.
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("certificado do principal ausente")
			}
			_, _, fp, err := verifyPeerSelfSignedCert(rawCerts[0])
			if err != nil {
				return err
			}
			if !constantTimeEqual(fp, expectedPrimaryFingerprint) {
				return errors.New("identidade criptográfica do painel principal não corresponde ao convite")
			}
			return nil
		},
	}
}
