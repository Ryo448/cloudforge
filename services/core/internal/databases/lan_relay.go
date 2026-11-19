package databases

import (
	"context"
	"io"
	"net"
	"slices"
	"strconv"
	"time"
)

// The external listener forwards each database protocol without terminating TLS.
func listenDatabaseLAN(ctx context.Context, network *net.IPNet, port int) (context.CancelFunc, error) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: network.IP, Port: port})
	if err != nil {
		return nil, err
	}
	target := net.JoinHostPort(localHost, strconv.Itoa(port))
	return serveDatabaseRelay(ctx, listener, network.Contains, func(ctx context.Context) (net.Conn, error) {
		dialer := net.Dialer{Timeout: 5 * time.Second}
		return dialer.DialContext(ctx, "tcp4", target)
	}), nil
}

func serveDatabaseRelay(ctx context.Context, listener *net.TCPListener, allowed func(net.IP) bool, dial func(context.Context) (net.Conn, error)) context.CancelFunc {
	ctx, cancel := context.WithCancel(ctx)
	context.AfterFunc(ctx, func() { _ = listener.Close() })
	go func() {
		defer cancel()
		connections := make(chan struct{}, 128)
		for {
			client, err := listener.AcceptTCP()
			if err != nil {
				return
			}
			if !allowed(client.RemoteAddr().(*net.TCPAddr).IP) {
				_ = client.Close()
				continue
			}
			select {
			case connections <- struct{}{}:
				go func() {
					defer func() { <-connections }()
					defer client.Close()
					upstream, err := dial(ctx)
					if err == nil {
						forwardDatabase(ctx, client, upstream.(*net.TCPConn))
					}
				}()
			default:
				_ = client.Close()
			}
		}
	}()
	return func() {
		_ = listener.Close()
		cancel()
	}
}

func forwardDatabase(ctx context.Context, client, upstream *net.TCPConn) {
	defer upstream.Close()
	stop := context.AfterFunc(ctx, func() {
		_ = client.Close()
		_ = upstream.Close()
	})
	defer stop()
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		_, _ = io.Copy(upstream, client)
		_ = upstream.CloseWrite()
	}()
	if _, err := io.Copy(client, upstream); err != nil {
		_ = client.Close()
		_ = upstream.Close()
	}
	_ = client.CloseWrite()
	<-sent
}

type databaseLANInstance struct {
	record
	stamp string
	hosts []string
}

func (m *Manager) databaseLANInstances() ([]int, []databaseLANInstance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	records, err := m.records("")
	if err != nil {
		return nil, nil, err
	}
	var ports []int
	var databases []databaseLANInstance
	for _, r := range records {
		if r.ReservedPort < 1 || r.ReservedPort > 65535 {
			continue
		}
		ports = append(ports, r.ReservedPort)
		if r.State == "running" && r.Desired == "running" && m.dockerReady && m.tlsAvailable(r) {
			databases = append(databases, databaseLANInstance{r, m.ready[r.ID], m.lanHosts[r.ID]})
		}
	}
	slices.Sort(ports)
	return ports, databases, nil
}

type databaseLANRelay struct {
	stop  context.CancelFunc
	stamp string
	scope string
}

func (m *Manager) reconcileDatabaseLANRelays(ctx context.Context, databases []databaseLANInstance, ports []int, relays map[string]databaseLANRelay) {
	networks, _ := databaseLANNetworks()
	published := make(map[string][]string)
	active := make(map[string]bool)
	for _, db := range databases {
		if !slices.Contains(ports, db.Port) {
			continue
		}
		var credentials secret
		for _, host := range db.hosts {
			network := networks[host]
			if network == nil {
				continue
			}
			key := db.ID + "/" + host
			relay, exists := relays[key]
			if exists && (relay.stamp != db.stamp || relay.scope != network.String()) {
				relay.stop()
				delete(relays, key)
				exists = false
			}
			if !exists {
				if credentials.TLS == nil {
					var err error
					credentials, err = m.secrets(db.record)
					if err != nil || credentials.TLS == nil {
						break
					}
				}
				stop, err := listenDatabaseLAN(ctx, network, db.Port)
				if err != nil {
					continue
				}
				probe, err := dialDatabaseTLSAt(ctx, db.record, credentials.TLS, host, db.Port, nil)
				if err != nil {
					stop()
					continue
				}
				_ = probe.Close()
				relays[key] = databaseLANRelay{stop, db.stamp, network.String()}
			}
			active[key] = true
			published[db.ID] = append(published[db.ID], host)
		}
	}
	for key, relay := range relays {
		if !active[key] {
			relay.stop()
			delete(relays, key)
		}
	}
	m.mu.Lock()
	m.lanPublished = published
	m.mu.Unlock()
}
