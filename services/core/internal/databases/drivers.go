package databases

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

var errPasswordRejected = errors.New("o banco recusou a autenticação")

const passwordProbeScript = `IFS= read -r password
export LC_ALL=C
set +e
result=$(
exec 2>&1
case "$1" in
mysql) export MYSQL_PWD="$password"; exec mysql --connect-timeout=5 --protocol=TCP --ssl-mode=VERIFY_IDENTITY --ssl-ca=/run/cloudforge/ca.crt -h "$2" -P "$3" -u "$4" --batch --skip-column-names "$5" -e 'SELECT 1';;
mongodb) exec mongosh --quiet --tls --tlsCAFile /run/cloudforge/ca.crt --host "$2" --port "$3" --username "$4" --password "$password" --authenticationDatabase "$5" --eval 'if (!db.adminCommand({ping:1}).ok) quit(1); print("1")';;
esac
)
status=$?
if [ "$status" = 0 ]; then printf '%s\n' "$result"; exit 0; fi
case "$result" in
  *"Access denied for user"*|*"Authentication failed"*)
    printf '%s\n' REJECTED ;;
esac`

func (d docker) probe(ctx context.Context, r record, host string, port int, user, password, database string) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := d.run(ctx, r, "/bin/sh", strings.NewReader(password+"\n"), "-ec", passwordProbeScript, "probe", r.Engine, host, strconv.Itoa(port), user, database)
	if err == nil && strings.TrimSpace(string(out)) == "REJECTED" {
		return errPasswordRejected
	}
	if err != nil || strings.TrimSpace(string(out)) != "1" {
		return errors.New("autenticação local do banco ainda indisponível")
	}
	return nil
}

func redisProbe(conn io.ReadWriter, user, password string) error {
	if _, err := fmt.Fprintf(conn, "*3\r\n$4\r\nAUTH\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n*1\r\n$4\r\nPING\r\n", len(user), user, len(password), password); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(conn, 256)
	auth, err := reader.ReadSlice('\n')
	if strings.HasPrefix(string(auth), "-WRONGPASS ") {
		return errPasswordRejected
	}
	if err != nil || string(auth) != "+OK\r\n" {
		return errors.New("autenticação local do Redis falhou")
	}
	reply, err := reader.ReadSlice('\n')
	if err != nil || string(reply) != "+PONG\r\n" {
		return errors.New("autenticação local do Redis falhou")
	}
	return nil
}

const manageUser = `IFS= read -r admin
case "$1" in
postgresql) export PGPASSWORD="$admin"; exec psql -X -w -U cfadmin -d cloudforge -v ON_ERROR_STOP=1;;
mysql) export MYSQL_PWD="$admin"; exec mysql --protocol=SOCKET -u root cloudforge;;
mongodb) exec mongosh --quiet --tls --tlsCAFile /run/cloudforge/ca.crt --host 127.0.0.1 --username cfadmin --password "$admin" --authenticationDatabase admin --eval "$(cat)" cloudforge;;
redis) export REDISCLI_AUTH="$admin"; exec redis-cli --tls --cacert /run/cloudforge/ca.crt -h 127.0.0.1 -p 6379 --user cfadmin -e -x ACL SETUSER cloudforge resetpass;;
esac`

const postgresAdminPrivileges = "ALTER ROLE cloudforge WITH SUPERUSER CREATEDB CREATEROLE;\n"

func (d docker) ensurePostgresAdmin(ctx context.Context, r record, admin string) error {
	_, err := d.run(ctx, r, "/bin/sh", strings.NewReader(admin+"\n"+postgresAdminPrivileges), "-ec", manageUser, "manage", r.Engine)
	if err != nil {
		return errors.New("não foi possível conceder privilégios administrativos à conta cloudforge")
	}
	return nil
}

func (d docker) setPassword(ctx context.Context, r record, s secret, password string) error {
	var statement string
	switch r.Engine {
	case "postgresql":
		statement = fmt.Sprintf(`DO $$ BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='cloudforge') THEN CREATE ROLE cloudforge LOGIN; END IF;
  ALTER ROLE cloudforge PASSWORD '%s';
  END $$;
  REVOKE CONNECT ON DATABASE postgres FROM PUBLIC;
  ALTER DATABASE cloudforge OWNER TO cloudforge;
  ALTER SCHEMA public OWNER TO cloudforge;
  `, password) + postgresAdminPrivileges
	case "mysql":
		statement = fmt.Sprintf(`CREATE USER IF NOT EXISTS 'cloudforge'@'%%' IDENTIFIED BY '%[1]s' REQUIRE SSL;
  ALTER USER 'cloudforge'@'%%' IDENTIFIED BY '%[1]s' REQUIRE SSL;
  GRANT ALL PRIVILEGES ON cloudforge.* TO 'cloudforge'@'%%';
  `, password)
	case "mongodb":
		statement = fmt.Sprintf(`const u = db.getUser("cloudforge");
  if (u) db.changeUserPassword("cloudforge", "%[1]s");
  else db.createUser({user:"cloudforge",pwd:"%[1]s",roles:[{role:"readWrite",db:"cloudforge"},{role:"dbAdmin",db:"cloudforge"}]});
  `, password)
	case "redis":
		statement = fmt.Sprintf("#%x", passwordHash(password))
	}
	out, err := d.run(ctx, r, "/bin/sh", strings.NewReader(s.Admin+"\n"+statement), "-ec", manageUser, "manage", r.Engine)
	if err != nil {
		return errors.New("não foi possível atualizar a conta cloudforge")
	}
	if r.Engine == "redis" && strings.TrimSpace(string(out)) != "OK" {
		return errors.New("Redis recusou a atualização da senha")
	}
	return nil
}

func passwordHash(password string) [32]byte {
	return sha256.Sum256([]byte(password))
}

func queryPostgres(ctx context.Context, config *tls.Config, host string, port int, user, password, database, query string, dest ...any) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// The registration lasts only for this query and is unique to its TLS config.
	key := fmt.Sprintf("pqgo-cloudforge-%p", config)
	if err := pq.RegisterTLSConfig(key, config); err != nil {
		return err
	}
	defer pq.RegisterTLSConfig(key, nil)
	connector, err := pq.NewConnectorConfig(pq.Config{
		Host: host, Port: uint16(port), User: user, Password: password, Database: database,
		SSLMode: pq.SSLMode(key), ConnectTimeout: 5 * time.Second,
	})
	if err != nil {
		return err
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	return db.QueryRowContext(ctx, query).Scan(dest...)
}

func (d docker) ready(ctx context.Context, r record, material *serverTLS, password string, localPort int) error {
	if r.Engine == "postgresql" {
		var authenticated int
		err := queryPostgres(ctx, material.probeTLSConfig(), localHost, localPort, Username, password, DatabaseName, "SELECT 1", &authenticated)
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "28P01" {
			return errPasswordRejected
		}
		if err != nil || authenticated != 1 {
			return errors.New("autenticação TLS do PostgreSQL indisponível na porta publicada")
		}
		return nil
	}
	conn, err := dialTLS(ctx, r, material, localPort)
	if err != nil {
		return err
	}
	if r.Engine == "redis" {
		defer conn.Close()
		return redisProbe(conn, Username, password)
	}
	_ = conn.Close()
	host := localHost
	if r.Engine == "mysql" {
		// The CLI uses DNS hostname matching; the external probe above
		// independently verifies the IP SAN on the published endpoint.
		host = "localhost"
	}
	return d.probe(ctx, r, host, engines[r.Engine].Port, Username, password, DatabaseName)
}

func (d docker) clientTLSReady(ctx context.Context, r record, material *serverTLS, admin string, port int) error {
	pair, err := material.clientPair(r)
	if err != nil {
		return err
	}
	probe := func() error {
		conn, err := dialDatabaseTLS(ctx, r, material, port, &pair)
		if err == nil {
			_ = conn.Close()
		}
		return err
	}
	if err = probe(); !errors.Is(err, errClientTLSNotRequested) {
		return err
	}
	// Existing TLS containers can enable optional verification in place.
	// PostgreSQL reloads a persisted setting; Redis's old command line is
	// reapplied on restart, so reenable it whenever the endpoint needs it.
	const postgres = `ALTER SYSTEM SET ssl_ca_file = '/run/cloudforge/ca.crt';
SELECT pg_reload_conf();
`
	const redis = `IFS= read -r REDISCLI_AUTH
export REDISCLI_AUTH
exec redis-cli --tls --cacert /run/cloudforge/ca.crt -h 127.0.0.1 -p 6379 --user cfadmin -e CONFIG SET tls-auth-clients optional`
	switch r.Engine {
	case "postgresql":
		_, err = d.run(ctx, r, "/bin/sh", strings.NewReader(admin+"\n"+postgres), "-ec", manageUser, "manage", r.Engine)
	case "redis":
		var out []byte
		out, err = d.run(ctx, r, "/bin/sh", strings.NewReader(admin+"\n"), "-ec", redis)
		if err == nil && strings.TrimSpace(string(out)) != "OK" {
			err = errors.New("Redis recusou a verificação TLS de cliente opcional")
		}
	default:
		// MySQL and MongoDB already load the CA and request client
		// certificates without requiring them for password authentication.
		return err
	}
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		if err = probe(); !errors.Is(err, errClientTLSNotRequested) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
