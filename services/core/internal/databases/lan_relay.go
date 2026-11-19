package databases

import (
	"context"
	"io"
	"net"
	"slices"
	"strconv"
	"time"
)

// The external listener forwards the PostgreSQL protocol without terminating TLS.
func listenPostgresLAN(ctx context.Context, network *net.IPNet, port int) (context.CancelFunc, error) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: network.IP, Port: port})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	context.AfterFunc(ctx, func() { _ = listener.Close() })
	target := net.JoinHostPort(localHost, strconv.Itoa(port))
	go func() {
		defer cancel()
		connections := make(chan struct{}, 128)
		for {
			client, err := listener.AcceptTCP()
			if err != nil {
				return
			}
			if !network.Contains(client.RemoteAddr().(*net.TCPAddr).IP) {
				_ = client.Close()
				continue
			}
			select {
			case connections <- struct{}{}:
				go func() {
					defer func() { <-connections }()
					forwardPostgresLAN(ctx, client, target)
				}()
			default:
				_ = client.Close()
			}
		}
	}()
	return func() {
		_ = listener.Close()
		cancel()
	}, nil
}

func forwardPostgresLAN(ctx context.Context, client *net.TCPConn, target string) {
	defer client.Close()
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp4", target)
	if err != nil {
		return
	}
	upstream := conn.(*net.TCPConn)
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
	if _, err = io.Copy(client, upstream); err != nil {
		_ = client.Close()
		_ = upstream.Close()
	}
	_ = client.CloseWrite()
	<-sent
}

type postgresLANInstance struct {
	record
	stamp string
	hosts []string
}

func (m *Manager) postgresLANInstances() ([]int, []postgresLANInstance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	records, err := m.records("")
	if err != nil {
		return nil, nil, err
	}
	var ports []int
	var databases []postgresLANInstance
	for _, r := range records {
		if r.Engine != "postgresql" || r.ReservedPort < 1 || r.ReservedPort > 65535 {
			continue
		}
		ports = append(ports, r.ReservedPort)
		if r.State == "running" && r.Desired == "running" && m.dockerReady && m.tlsAvailable(r) {
			databases = append(databases, postgresLANInstance{r, m.ready[r.ID], m.lanHosts[r.ID]})
		}
	}
	slices.Sort(ports)
	return ports, databases, nil
}

type postgresLANRelay struct {
	stop  context.CancelFunc
	stamp string
	scope string
}

func (m *Manager) reconcilePostgresLANRelays(ctx context.Context, databases []postgresLANInstance, ports []int, relays map[string]postgresLANRelay) {
	networks, _ := postgresLANNetworks()
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
				stop, err := listenPostgresLAN(ctx, network, db.Port)
				if err != nil {
					continue
				}
				config := credentials.TLS.probeTLSConfig()
				config.ServerName = host
				var authenticated int
				if err = queryPostgres(ctx, config, host, db.Port, Username, credentials.Password, DatabaseName, "SELECT 1", &authenticated); err != nil || authenticated != 1 {
					stop()
					continue
				}
				relays[key] = postgresLANRelay{stop, db.stamp, network.String()}
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
