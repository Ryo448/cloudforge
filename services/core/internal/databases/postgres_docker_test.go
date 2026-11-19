package databases

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
)

func TestDockerDeleteDatabaseWithClients(t *testing.T) {
	if os.Getenv("CLOUDFORGE_TEST_DATABASES") != "1" {
		t.Skip("defina CLOUDFORGE_TEST_DATABASES=1 para executar com Docker Linux")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	m := testManager(t)
	if !m.docker.available(ctx) {
		t.Fatal("Docker Linux indisponível")
	}
	instance, err := m.Create(ctx, "test", Input{Name: "Exclusão com clientes", Engine: "postgresql"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.get("test", instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	network := containerName(r)
	viewer, other := "cloudforge-dbeaver-"+r.ID, "cloudforge-test-client-"+r.ID
	t.Cleanup(func() {
		clean, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_, _ = m.docker.command(clean, nil, "rm", "-f", viewer, other)
		if err := m.docker.remove(clean, r); err != nil {
			t.Error(err)
		}
	})
	if err = m.reconcile(ctx, &r); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) []byte {
		t.Helper()
		out, err := m.docker.command(ctx, nil, args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	image := engines[r.Engine].Image
	run("run", "-d", "--rm", "--name", viewer, "--network", network, "--user", "8978:8978",
		"--tmpfs", "/opt/cloudbeaver/workspace:rw,nosuid,nodev,uid=8978,gid=8978,mode=0700",
		"--entrypoint", "/bin/sleep", image, "300")
	run("run", "-d", "--rm", "--name", other, "--network", network, "--entrypoint", "/bin/sleep", image, "300")
	r.Action = "delete"
	if err = m.reconcile(ctx, &r); !errors.Is(err, errDatabaseNetworkInUse) {
		t.Fatalf("a rede em uso deveria bloquear a exclusão: %v", err)
	}
	if state, exists, err := m.docker.inspect(ctx, r); err != nil || !exists || !state.Running {
		t.Fatalf("o banco deveria continuar em execução: %v", err)
	}
	if members := run("network", "inspect", "--format", "{{json .Containers}}", network); !bytes.Contains(members, []byte(viewer)) {
		t.Fatal("a conexão do DBeaver deveria ser mantida enquanto a exclusão estiver bloqueada")
	}
	run("rm", "-f", other)
	if err = m.reconcile(ctx, &r); err != nil {
		t.Fatal(err)
	}
	if _, err = m.get(r.Owner, r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("o registro deveria ser excluído: %v", err)
	}
	for _, kind := range []string{"network", "volume"} {
		if out := run(kind, "ls", "--filter", "label=cloudforge.database="+r.ID, "--format", "{{.Name}}"); len(bytes.TrimSpace(out)) != 0 {
			t.Fatalf("recurso ainda presente: %s", out)
		}
	}
}

func TestDockerPostgresAuthentication(t *testing.T) {
	if os.Getenv("CLOUDFORGE_TEST_DATABASES") != "1" {
		t.Skip("defina CLOUDFORGE_TEST_DATABASES=1 para executar com Docker Linux")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	m := testManager(t)
	instance, err := m.Create(ctx, "test", Input{Name: "PostgreSQL HBA", Engine: "postgresql"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.get("test", instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := m.docker.remove(ctx, r); err != nil {
			t.Error(err)
		}
	})
	apply := func() {
		t.Helper()
		if err := m.reconcile(ctx, &r); err != nil {
			t.Fatal(err)
		}
	}
	apply()
	s, err := m.secrets(r)
	if err != nil {
		t.Fatal(err)
	}
	port, started := r.Port, *r.StartedAt
	hosts := m.lanHosts[r.ID]
	relays := make(map[string]postgresLANRelay)
	defer func() {
		for _, relay := range relays {
			relay.stop()
		}
	}()
	verifyLAN := func(material *serverTLS) {
		t.Helper()
		if len(hosts) == 0 {
			t.Log("nenhum IPv4 de rede local disponível para testar a conexão")
			return
		}
		if runtime.GOOS == "windows" {
			_, databases, err := m.postgresLANInstances()
			if err != nil {
				t.Fatal(err)
			}
			m.firewallPorts = []int{port}
			m.reconcilePostgresLANRelays(ctx, databases, []int{port}, relays)
			if len(m.view(r).LANHosts) == 0 {
				t.Fatal("a escuta do Cloudforge não validou a conexão pelo IPv4")
			}
		}
		var lanErr error
		for _, host := range hosts {
			config := material.probeTLSConfig()
			config.ServerName = host
			var connected int
			lanErr = queryPostgres(ctx, config, host, port, Username, s.Password, DatabaseName, "SELECT 1", &connected)
			if lanErr == nil && connected == 1 {
				t.Log("conexão TLS verificada pelo IPv4 da rede local")
				return
			}
		}
		t.Fatalf("falha na conexão TLS pelos IPv4 da rede local: %v", lanErr)
	}
	verifyLAN(s.TLS)
	adminSQL := func(statement string) string {
		t.Helper()
		out, err := m.docker.run(ctx, r, "/bin/sh", strings.NewReader(s.Admin+"\n"+statement), "-ec", manageUser, "manage", r.Engine)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	if active := adminSQL("SHOW hba_file;\n"); !strings.Contains(active, postgresHBAPath) {
		t.Fatalf("HBA em uso: %s", active)
	}
	const createDatabase = `IFS= read -r PGPASSWORD
export PGPASSWORD PGSSLMODE=verify-full PGSSLROOTCERT=/run/cloudforge/ca.crt
exec psql -X -w -h 127.0.0.1 -U cloudforge -d postgres -v ON_ERROR_STOP=1`
	createAsDefaultUser := func(statement string) error {
		_, err := m.docker.run(ctx, r, "/bin/sh", strings.NewReader(s.Password+"\n"+statement), "-ec", createDatabase)
		return err
	}
	if err = createAsDefaultUser("CREATE DATABASE \"NCT\";\n"); err != nil {
		t.Fatal("a conta padrão não conseguiu criar um banco usando a conexão TLS com postgres", err)
	}
	adminSQL(`CREATE DATABASE restricted;
REVOKE CONNECT ON DATABASE restricted FROM PUBLIC;
\connect NCT
SET ROLE cloudforge;
CREATE TABLE records (value text);
INSERT INTO records VALUES ('preserved');
`)
	var sample string
	query := func(user, password, database, statement string, dest ...any) error {
		config := s.TLS.probeTLSConfig()
		pair, err := s.TLS.clientPair(r)
		if err != nil {
			return err
		}
		config.Certificates = append(config.Certificates, pair)
		return queryPostgres(ctx, config, localHost, port, user, password, database, statement, dest...)
	}
	read := func() {
		t.Helper()
		var clientDN string
		err := query(Username, s.Password, "NCT", `SELECT value, client_dn FROM records, pg_stat_ssl WHERE pid = pg_backend_pid() AND ssl`, &sample, &clientDN)
		if err != nil || sample != "preserved" || !strings.Contains(clientDN, clientCommonName(r)) {
			t.Fatalf("consulta com senha e certificado de cliente: %q, %q (%v)", sample, clientDN, err)
		}
	}
	expectSQLState := func(err error, code string) {
		t.Helper()
		var serverErr *pq.Error
		if !errors.As(err, &serverErr) || string(serverErr.Code) != code {
			t.Fatalf("esperado SQLSTATE %s, recebido %v", code, err)
		}
	}
	read()
	adminSQL("ALTER ROLE cloudforge NOSUPERUSER NOCREATEDB NOCREATEROLE;\n")
	expectSQLState(query(Username, s.Password, "restricted", "SELECT 1", &sample), "42501")
	if err = createAsDefaultUser("CREATE DATABASE restored_privileges;\n"); err == nil {
		t.Fatal("a conta antiga conseguiu criar um banco antes da promoção")
	}

	const oldHBA = `local all cfadmin scram-sha-256
hostssl all cfadmin 127.0.0.1/32 scram-sha-256
hostssl all cfadmin ::1/128 scram-sha-256
hostssl cloudforge cloudforge all scram-sha-256
`
	if _, err = m.docker.run(ctx, r, "/bin/sh", strings.NewReader(oldHBA), "-ec", `cat > "$1"; kill -HUP 1`, "legacy-hba", postgresHBAPath); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; {
		err = query(Username, s.Password, "NCT", "SELECT 1", &sample)
		var serverErr *pq.Error
		if errors.As(err, &serverErr) && serverErr.Code == "28000" && strings.Contains(serverErr.Message, "no pg_hba.conf entry") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("a regra antiga não reproduziu a recusa para NCT: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err = m.docker.ready(ctx, r, s.TLS, s.Password, port); err != nil {
		t.Fatalf("a antiga regra deveria aceitar somente o banco padrão: %v", err)
	}
	delete(m.ready, r.ID) // Simulate starting an updated Core.
	apply()
	if !r.StartedAt.Equal(started) {
		t.Fatal("a reconciliação reiniciou o PostgreSQL")
	}
	read()
	if err = createAsDefaultUser("CREATE DATABASE restored_privileges;\n"); err != nil {
		t.Fatal("a conta existente não foi promovida após atualizar o Core", err)
	}
	expectSQLState(query(Username, "incorrect", "NCT", "SELECT 1", &sample), "28P01")
	if err = query(Username, s.Password, "restricted", "SELECT 1", &sample); err != nil {
		t.Fatal("o superusuário deveria conseguir acessar outro banco da instância", err)
	}
	expectSQLState(query("cfadmin", s.Admin, "NCT", "SELECT 1", &sample), "28000")
	plainConnector, err := pq.NewConnectorConfig(pq.Config{
		Host: localHost, Port: uint16(port), User: Username, Password: s.Password, Database: "NCT", SSLMode: pq.SSLModeDisable,
		ConnectTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	plain := sql.OpenDB(plainConnector)
	defer plain.Close()
	expectSQLState(plain.PingContext(ctx), "28000")

	if err = m.docker.lifecycle(ctx, r, "restart", "--time", "10"); err != nil {
		t.Fatal(err)
	}
	apply()
	read()
	peerAddress := func() string {
		t.Helper()
		var address string
		if err := query(Username, s.Password, "NCT", "SELECT inet_client_addr()::text", &address); err != nil {
			t.Fatal(err)
		}
		return address
	}
	before := peerAddress()
	name := containerName(r)
	containerID := func() string {
		t.Helper()
		id, err := m.docker.command(ctx, nil, "inspect", "--type", "container", "--format", "{{.Id}}", name)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(id))
	}
	id := containerID()
	subnet, err := m.docker.command(ctx, nil, "network", "inspect", name, "--format", "{{(index .IPAM.Config 0).Subnet}}")
	if err != nil {
		t.Fatal(err)
	}
	network := name + "-previous-subnet"
	if _, err = m.docker.command(ctx, nil, "network", "disconnect", name, name); err != nil {
		t.Fatal(err)
	}
	if _, err = m.docker.command(ctx, nil, "network", "rm", name); err != nil {
		t.Fatal(err)
	}
	if _, err = m.docker.command(ctx, nil, "network", "create", "--subnet", strings.TrimSpace(string(subnet)), network); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := m.docker.command(ctx, nil, "network", "rm", network); err != nil {
			t.Error(err)
		}
	})
	apply()
	if containerID() != id {
		t.Fatal("a recuperação da rede substituiu o container existente")
	}
	after := peerAddress()
	if after == before {
		t.Fatalf("a alteração da rede não mudou o endereço de origem %s", before)
	}
	t.Logf("origem na porta publicada mudou de %s para %s", before, after)
	read()
	current, err := m.secrets(r)
	if err != nil || r.Port != port || current.Password != s.Password || current.Admin != s.Admin ||
		!bytes.Equal(current.TLS.PEM, s.TLS.PEM) || !bytes.Equal(current.TLS.CA, s.TLS.CA) ||
		!bytes.Equal(current.TLS.Client.PEM, s.TLS.Client.PEM) || !bytes.Equal(current.TLS.Client.Key, s.TLS.Client.Key) {
		t.Fatalf("a recuperação alterou porta, credenciais ou certificados: %v", err)
	}

	if len(hosts) != 0 {
		// Simulate a certificate issued before the host received its current IPv4.
		_, issuer, signer, err := m.tlsSigner()
		if err != nil {
			t.Fatal(err)
		}
		leaf, err := issueServerTLS(r, issuer, signer, nil)
		if err != nil {
			t.Fatal(err)
		}
		old := *current.TLS
		old.certificate = leaf
		current.TLS = &old
		if err = m.saveSecrets(r, current); err != nil {
			t.Fatal(err)
		}
		started = *r.StartedAt
		apply()
		updated, err := m.secrets(r)
		if err != nil || !r.StartedAt.After(started) || r.Port != port || updated.Password != s.Password ||
			!bytes.Equal(updated.TLS.CA, old.CA) || !bytes.Equal(updated.TLS.Client.Key, old.Client.Key) {
			t.Fatalf("a renovação automática não preservou porta, credenciais e identidade do cliente: %v", err)
		}
		verifyLAN(updated.TLS)
		s = updated
		read()
	}
}
