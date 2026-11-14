package databases

import (
	"bytes"
	"cloudforge/core/internal/persistence"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

type testVault map[string]string

func (v testVault) Get(key string) (string, error) {
	if value, ok := v[key]; ok {
		return value, nil
	}
	return "", keyring.ErrNotFound
}

func (v testVault) Set(key, value string) error { v[key] = value; return nil }

func testManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	store, err := persistence.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	store.EnvironmentVault = testVault{}
	m := New(store, "test-installation-key")
	m.dockerReady, m.dockerChecked = true, true
	return m
}

func TestConcurrentReservationAndOwnership(t *testing.T) {
	m := testManager(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.Create(ctx, "owner", Input{Name: "Produção", Engine: "postgresql"})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if wins != 1 {
		t.Fatalf("número de reservas: %d", wins)
	}
	list, err := m.List(ctx, "owner")
	if err != nil || len(list.Databases) != 1 {
		t.Fatalf("listagem: %v %v", list, err)
	}
	first := list.Databases[0]
	if _, _, err = m.Detail("other", first.ID, time.Hour); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = m.Change("other", first.ID, Change{Action: "delete", Confirmation: first.Name}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = m.Reveal("other", first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	other, err := m.Create(ctx, "other", Input{Name: "Outro", Engine: "postgresql"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := m.get("owner", first.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.get("other", other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.allocatePort(&a); err != nil {
		t.Fatal(err)
	}
	if err = m.allocatePort(&b); err != nil {
		t.Fatal(err)
	}
	if a.ReservedPort == 0 || b.ReservedPort == 0 || a.ReservedPort == b.ReservedPort {
		t.Fatalf("portas: %d, %d", a.ReservedPort, b.ReservedPort)
	}
	again, err := m.get("owner", first.ID)
	if err != nil || again.ReservedPort != a.ReservedPort {
		t.Fatalf("reserva persistida: %v %v", again, err)
	}
}

func TestCreateMemoryLimit(t *testing.T) {
	m := testManager(t)
	ctx := context.Background()
	for _, tc := range []struct {
		engine string
		input  *int
		want   int
	}{
		{"mongodb", nil, defaultMemoryMB},
		{"redis", intPointer(0), 0},
		{"mysql", intPointer(2048), 2048},
	} {
		created, err := m.Create(ctx, "owner", Input{Name: tc.engine, Engine: tc.engine, MemoryMB: tc.input})
		if err != nil {
			t.Fatal(err)
		}
		r, err := m.get("owner", created.ID)
		if err != nil || r.MemoryMB == nil || *r.MemoryMB != tc.want {
			t.Fatalf("memória persistida para %s: %v, %v", tc.engine, r.MemoryMB, err)
		}
	}
	for _, value := range []int{-1, 1, 1048577} {
		if _, err := m.Create(ctx, "owner", Input{Name: "Inválido", Engine: "postgresql", MemoryMB: &value}); err == nil {
			t.Fatalf("limite inválido aceito: %d", value)
		}
	}
}

func intPointer(value int) *int { return &value }

func TestCredentials(t *testing.T) {
	m := testManager(t)
	i, err := m.Create(context.Background(), "owner", Input{Name: "Banco", Engine: "mongodb"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.get("owner", i.ID)
	if err != nil {
		t.Fatal(err)
	}
	password := strings.Repeat("f", 64)
	material, err := m.issueTLS(r)
	if err != nil {
		t.Fatal(err)
	}
	s := secret{Admin: strings.Repeat("a", 64), Password: password, Reveal: true, TLS: material}
	if err = m.saveSecrets(r, s); err != nil {
		t.Fatal(err)
	}
	r.Action, r.State, r.Port, r.CredentialsPending = "", "running", 15401, true
	r.TLSValidated = true
	m.ready[r.ID] = "validated"
	if err = m.save(r); err != nil {
		t.Fatal(err)
	}
	var encoded []byte
	if err = m.db.QueryRow("SELECT secret FROM hosted_databases WHERE id=?", r.ID).Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), password) {
		t.Fatal("credencial em texto aberto")
	}
	creds, err := m.Reveal("owner", r.ID)
	if err != nil || creds.Password != password || !strings.Contains(creds.URL, "@127.0.0.1:15401/cloudforge?authSource=cloudforge") {
		t.Fatal("credencial inválida", err)
	}
	if _, err = m.Reveal("owner", r.ID); err == nil {
		t.Fatal("credencial exibida mais de uma vez")
	}
	current, _, err := m.Detail("owner", r.ID, time.Hour)
	if err != nil || current.CredentialsPending || strings.Contains(current.URL, password) {
		t.Fatal("senha exposta no detalhe", err)
	}

}

func TestLocalDatabaseMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("CREATE TABLE schema_migrations(version TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	for _, migration := range []string{"001_initial.sql", "002_databases.sql"} {
		data, err := os.ReadFile(filepath.Join("..", "persistence", "migrations", migration))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(data)); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec("INSERT INTO schema_migrations VALUES(?)", migration); err != nil {
			t.Fatal(err)
		}
	}
	const data = `{"View":{"id":"legacy","name":"Banco","engine":"mysql","host":"old.example.test","port":15100,
		"publication":"verified","credentialsPending":true},"Desired":"running","Action":"domain"}`
	for _, statement := range []string{
		"INSERT INTO hosted_databases VALUES('legacy','owner','mysql',15100,'old.example.test','" + data + "',NULL)",
		"INSERT INTO hosted_database_metrics VALUES ('legacy',1,'{}')",
	} {
		if _, err = db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	legacyKey := make([]byte, 32)
	box, err := newSecretBox(legacyKey)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, box.NonceSize())
	const legacyPlaintext = `{"Admin":"legacy-admin","Password":"legacy-password","Reveal":false}`
	legacySecret := box.Seal(nonce, nonce, []byte(legacyPlaintext), []byte("legacy"))
	if _, err = db.Exec("UPDATE hosted_databases SET secret=? WHERE id='legacy'", legacySecret); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := persistence.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.EnvironmentVault = testVault{"hosted-database-master": base64.StdEncoding.EncodeToString(legacyKey)}
	m := New(store, "test-installation-key")
	r, err := m.get("owner", "legacy")
	if err != nil || r.Host != localHost || r.Port != 0 || r.ReservedPort != 0 || r.Action != "" || r.CredentialsPending || r.Desired != "running" {
		t.Fatalf("registro migrado: %+v (%v)", r, err)
	}
	credentials, err := m.secrets(r)
	if err != nil || credentials.Admin != "legacy-admin" || credentials.Password != "legacy-password" || credentials.Reveal {
		t.Fatal("credenciais não foram preservadas", err)
	}
	store.EnvironmentVault = testVault{}
	m = New(store, "test-installation-key")
	credentials, err = m.secrets(r)
	if err != nil || credentials.Password != "legacy-password" || credentials.Reveal {
		t.Fatal("credenciais migradas indisponíveis", err)
	}
	for _, expected := range []int{1, 0} {
		var count int
		if err = store.DB.QueryRow("SELECT count(*) FROM hosted_database_metrics").Scan(&count); err != nil || count != expected {
			t.Fatalf("métricas: %d, esperado %d (%v)", count, expected, err)
		}
		if _, err = store.DB.Exec("DELETE FROM hosted_databases WHERE id='legacy'"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDockerDatabases(t *testing.T) {
	if os.Getenv("CLOUDFORGE_TEST_DATABASES") != "1" {
		t.Skip("defina CLOUDFORGE_TEST_DATABASES=1 para executar com Docker Linux")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	t.Cleanup(cancel)
	if !(docker{}).available(ctx) {
		t.Fatal("Docker Linux indisponível")
	}
	for _, engine := range []string{"postgresql", "mysql", "mongodb", "redis"} {
		t.Run(engine, func(t *testing.T) {
			t.Parallel()
			m := testManager(t)
			i, err := m.Create(ctx, "test", Input{Name: "Integration " + engine, Engine: engine})
			if err != nil {
				t.Fatal(err)
			}
			r, err := m.get("test", i.ID)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				clean, stop := context.WithTimeout(context.Background(), time.Minute)
				defer stop()
				if err := m.docker.remove(clean, r); err != nil {
					t.Error(err)
				}
			})
			apply := func(label string) {
				t.Helper()
				if err := m.reconcile(ctx, &r); err != nil {
					out, _ := m.docker.command(ctx, nil, "ps", "-a", "--filter", "name="+containerName(r), "--format", "{{.Status}}")
					t.Fatalf("%s: %v (%s)", label, err, strings.TrimSpace(string(out)))
				}
			}
			apply("create")
			if r.State != "running" || r.Port == 0 || !r.CredentialsPending {
				t.Fatalf("estado inicial: %v", r.State)
			}
			original, err := m.secrets(r)
			if err != nil {
				t.Fatal(err)
			}
			operate := func(action string) {
				t.Helper()
				if _, err := m.Change("test", r.ID, Change{Action: action, Confirmation: r.Name}); err != nil {
					t.Fatal(err)
				}
				r, err = m.get("test", r.ID)
				if err != nil {
					t.Fatal(err)
				}
				apply(action)
			}
			runData := func(write bool) {
				t.Helper()
				shell, err := persistenceProbe(r.Engine, write)
				if err != nil {
					t.Fatal(err)
				}
				current, err := m.secrets(r)
				if err != nil {
					t.Fatal(err)
				}
				out, err := m.docker.run(ctx, r, "/bin/sh", strings.NewReader(current.Password+"\n"), "-ec", shell)
				if err != nil || (!write && !strings.Contains(string(out), "preserved")) {
					t.Fatalf("persistência: %v", err)
				}
			}
			runData(true)
			port := r.Port
			// Simulate upgrading a running TLS instance from a release that
			// has not issued any client material.
			old := original
			oldMaterial := *original.TLS
			oldMaterial.Client = nil
			old.TLS = &oldMaterial
			if err = m.saveSecrets(r, old); err != nil {
				t.Fatal(err)
			}
			if engine == "redis" {
				const disableClientTLS = `IFS= read -r REDISCLI_AUTH
export REDISCLI_AUTH
exec redis-cli --tls --cacert /run/cloudforge/ca.crt -h 127.0.0.1 -p 6379 --user cfadmin -e CONFIG SET tls-auth-clients no`
				if _, err = m.docker.run(ctx, r, "/bin/sh", strings.NewReader(old.Admin+"\n"), "-ec", disableClientTLS); err != nil {
					t.Fatal(err)
				}
			}
			started := *r.StartedAt
			delete(m.ready, r.ID) // Core has restarted; Docker has not.
			apply("backfill de cliente")
			original, err = m.secrets(r)
			if err != nil || !started.Equal(*r.StartedAt) || original.Password != old.Password || original.Admin != old.Admin ||
				!bytes.Equal(original.TLS.PEM, old.TLS.PEM) || !bytes.Equal(original.TLS.Key, old.TLS.Key) || !bytes.Equal(original.TLS.CA, old.TLS.CA) {
				t.Fatal("backfill alterou o container, as credenciais ou o TLS do servidor", err)
			}
			runData(false)
			before, _, err := m.Certificate("test", i.ID, "pem")
			if err != nil {
				t.Fatal(err)
			}
			operate("certificate")
			after, _, err := m.Certificate("test", i.ID, "pem")
			if err != nil || bytes.Equal(before, after) || r.Port != port {
				t.Fatal("o certificado ativo não foi redefinido na mesma porta", err)
			}
			newTLS, err := m.secrets(r)
			if err != nil || newTLS.Admin != original.Admin || newTLS.Password != original.Password || !bytes.Equal(newTLS.TLS.CA, original.TLS.CA) {
				t.Fatal("a redefinição não preservou a CA ou a senha", err)
			}
			key, _, err := m.Certificate("test", i.ID, "key")
			if err != nil || bytes.Equal(key, original.TLS.Client.Key) || !bytes.Equal(key, newTLS.TLS.Client.Key) {
				t.Fatal("a redefinição não atualizou a chave de cliente para download", err)
			}
			runData(false)
			if r.Engine == "redis" {
				// The new certificate may already be serving when the final
				// commit fails. The previous certificate must be served again.
				if _, err = m.db.Exec(`CREATE TRIGGER reject_rotation BEFORE UPDATE OF secret ON hosted_databases
					BEGIN SELECT RAISE(ABORT, 'test rotation persistence failure'); END;`); err != nil {
					t.Fatal(err)
				}
				operate("certificate")
				if _, err = m.db.Exec("DROP TRIGGER reject_rotation"); err != nil {
					t.Fatal(err)
				}
				restored, _, err := m.Certificate("test", i.ID, "pem")
				if err != nil || !bytes.Equal(restored, after) || r.Issue == "" {
					t.Fatal("não restaurou e revalidou o certificado ativo", err)
				}
				runData(false)
			}
			operate("stop")
			operate("start")
			restarted, _, err := m.Certificate("test", i.ID, "pem")
			if err != nil || !bytes.Equal(restarted, after) {
				t.Fatal("o certificado não sobreviveu ao reinício", err)
			}
			runData(false)
			operate("restart")
			if r.Port != port {
				t.Fatal("a porta mudou")
			}
			runData(false)
			// Simulate a legacy container still holding the existing data volume.
			if r.Engine == "mysql" {
				const requireTLS = `IFS= read -r admin
export MYSQL_PWD="$admin"
exec mysql --protocol=SOCKET -u root -e "ALTER USER 'cloudforge'@'%' REQUIRE SSL;"`
				if _, err = m.docker.run(ctx, r, "/bin/sh", strings.NewReader(original.Admin+"\n"), "-ec", requireTLS); err != nil {
					t.Fatal(err)
				}
			}
			operate("stop")
			if err = m.docker.lifecycle(ctx, r, "rm"); err != nil {
				t.Fatal(err)
			}
			name := containerName(r)
			cfg := engines[r.Engine]
			if _, err = m.docker.command(ctx, nil, "create", "--pull", "never", "--name", name,
				"--label", "cloudforge.database="+r.ID, "--label", "cloudforge.database.local=1", "--network", name,
				"--publish", fmt.Sprintf("127.0.0.1:%d:%d", port, cfg.Port),
				"--mount", "type=volume,source="+name+",target="+cfg.Data,
				"--entrypoint", "/bin/sh", cfg.Image, "-c", "sleep 600"); err != nil {
				t.Fatal(err)
			}
			operate("start")
			runData(false)
			if r.Engine == "mysql" {
				const localLogin = `IFS= read -r password
export MYSQL_PWD="$password"
exec mysql --protocol=TCP -h 127.0.0.1 -u cloudforge --ssl-mode=DISABLED --get-server-public-key -Ns cloudforge -e 'SELECT 1'`
				if _, err := m.docker.run(ctx, r, "/bin/sh", strings.NewReader(original.Password+"\n"), "-ec", localLogin); err == nil {
					t.Fatal("MySQL aceitou uma conexão sem TLS")
				}
			}
			if _, exists, err := m.docker.inspectNamed(ctx, r, name+"-legacy"); err != nil || exists {
				t.Fatal("container legado não foi removido", err)
			}
			operate("password")
			rotated, err := m.secrets(r)
			if err != nil || original.Password == rotated.Password {
				t.Fatal("senha não alterada", err)
			}
			runData(false)
			runtime, _, err := m.docker.inspect(ctx, r)
			if err != nil || runtime.LocalPort != port || !runtime.Local {
				t.Fatal("container indisponível", err)
			}
			if _, err = m.docker.metrics(ctx, r); err != nil {
				t.Fatal(err)
			}
			operate("delete")
			if _, err = m.get("test", i.ID); !errors.Is(err, ErrNotFound) {
				t.Fatal("registro permaneceu", err)
			}
			if _, exists, err := m.docker.inspect(ctx, r); err != nil || exists {
				t.Fatal("container permaneceu", err)
			}
		})
	}
}

func persistenceProbe(engine string, write bool) (string, error) {
	var query, read, command string
	switch engine {
	case "postgresql":
		query, read = "CREATE TABLE records (value text); INSERT INTO records VALUES ('preserved');", "SELECT value FROM records;"
		command = `PGPASSWORD="$password" PGSSLMODE=verify-full PGSSLROOTCERT=/run/cloudforge/ca.crt psql -XtA -h 127.0.0.1 -U cloudforge -d cloudforge -v ON_ERROR_STOP=1 -c "%s"`
	case "mysql":
		query, read = "CREATE TABLE records (value varchar(20)); INSERT INTO records VALUES ('preserved');", "SELECT value FROM records;"
		command = `MYSQL_PWD="$password" mysql --protocol=TCP --ssl-mode=VERIFY_IDENTITY --ssl-ca=/run/cloudforge/ca.crt -h localhost -u cloudforge -Ns cloudforge -e "%s"`
	case "mongodb":
		query, read = "db.records.insertOne({value:'preserved'})", "print(db.records.findOne().value)"
		command = `mongosh --quiet --tls --tlsCAFile /run/cloudforge/ca.crt --host 127.0.0.1 --username cloudforge --password "$password" --authenticationDatabase cloudforge --eval "%s" cloudforge`
	case "redis":
		query, read = "SET records preserved", "GET records"
		command = `REDISCLI_AUTH="$password" redis-cli --tls --cacert /run/cloudforge/ca.crt -h 127.0.0.1 --user cloudforge -e %s`
	default:
		return "", errors.New("tecnologia desconhecida")
	}
	if !write {
		query = read
	}
	return "IFS= read -r password\n" + fmt.Sprintf(command, query), nil
}
