package databases

import (
	"context"
	"errors"
	"net"
	"strconv"
	"time"
)

func (m *Manager) ConfigurePostgresTunnel(owner, id string, publish func() error) error {
	m.mu.Lock()
	r, err := m.get(owner, id)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	if m.active[id] {
		m.mu.Unlock()
		return ErrConflict
	}
	if r.Engine != "postgresql" || r.State != "running" || !m.tlsAvailable(r) {
		m.mu.Unlock()
		return errors.New("inicie o PostgreSQL e aguarde a validação do TLS")
	}
	m.active[id] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.active, id)
		m.mu.Unlock()
	}()
	return publish()
}

// OpenPostgresTunnel gives cloudflared a private port dedicated to one managed instance.
func (m *Manager) OpenPostgresTunnel(owner, id string) (int, context.CancelFunc, error) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return 0, nil, err
	}
	stop := servePostgresRelay(context.Background(), listener, net.IP.IsLoopback, func(ctx context.Context) (net.Conn, error) {
		return m.dialPostgresTunnel(ctx, owner, id)
	})
	return listener.Addr().(*net.TCPAddr).Port, stop, nil
}

func (m *Manager) dialPostgresTunnel(ctx context.Context, owner, id string) (net.Conn, error) {
	m.mu.Lock()
	r, err := m.get(owner, id)
	if err != nil || r.Engine != "postgresql" || r.State != "running" || !m.tlsAvailable(r) || r.Port < 1 || r.Port != r.ReservedPort {
		m.mu.Unlock()
		return nil, ErrUnavailable
	}
	s, err := m.secrets(r)
	started := m.ready[r.ID]
	m.mu.Unlock()
	if err != nil || s.TLS == nil {
		return nil, ErrUnavailable
	}
	probe, err := dialTLS(ctx, r, s.TLS, r.Port)
	if err != nil {
		return nil, ErrUnavailable
	}
	_ = probe.Close()

	m.mu.Lock()
	defer m.mu.Unlock()
	current, err := m.get(owner, id)
	if err != nil || current.State != "running" || current.Port != r.Port || started != m.ready[id] || !m.tlsAvailable(current) {
		return nil, ErrUnavailable
	}
	dialer := net.Dialer{Timeout: 2 * time.Second}
	return dialer.DialContext(ctx, "tcp4", net.JoinHostPort(localHost, strconv.Itoa(r.Port)))
}
