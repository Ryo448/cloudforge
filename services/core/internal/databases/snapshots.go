package databases

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var snapshotEngineNames = map[string]string{
	"postgresql": "PostgreSQL", "mysql": "MySQL", "mongodb": "MongoDB", "redis": "Redis",
}

var ErrSnapshotBusy = errors.New("aguarde a operação em andamento no banco de dados")

func SnapshotEngineName(engine string) string { return snapshotEngineNames[engine] }

func (m *Manager) SnapshotDatabase(owner, id string) (Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.get(owner, id)
	return r.Instance, err
}

func (m *Manager) SnapshotReady(owner, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.get(owner, id)
	return err == nil && m.dockerReady && !m.active[id] && r.State == "running" && m.tlsAvailable(r)
}

type SnapshotDatabase struct {
	record
	secret secret
	ctx    context.Context
}

// ReserveSnapshot shares the lifecycle gate with reconciliation, credential changes and deletion.
func (m *Manager) ReserveSnapshot(ctx context.Context, owner, id string) (*SnapshotDatabase, func(), error) {
	m.mu.Lock()
	r, err := m.get(owner, id)
	if err == nil && !m.dockerReady {
		err = ErrUnavailable
	}
	if err == nil && m.active[id] {
		err = ErrSnapshotBusy
	}
	if err == nil && (r.State != "running" || r.Action != "" || !m.tlsAvailable(r)) {
		err = ErrConflict
	}
	var s secret
	if err == nil {
		s, err = m.secrets(r)
	}
	if err != nil {
		m.mu.Unlock()
		return nil, nil, err
	}
	m.active[id] = true
	m.mu.Unlock()
	release := func() {
		m.mu.Lock()
		delete(m.active, id)
		m.mu.Unlock()
		m.notify()
	}
	state, exists, err := m.docker.inspect(ctx, r)
	if err != nil || !exists || !state.Running ||
		m.docker.ready(ctx, r, s.TLS, s.Password, state.LocalPort) != nil {
		release()
		return nil, nil, errors.New("o banco de destino precisa estar online e aceitar conexões")
	}
	return &SnapshotDatabase{r, s, ctx}, release, nil
}

func (d *SnapshotDatabase) exec(input io.Reader, output io.Writer, args ...string) error {
	return dockerCommand(d.ctx, input, output, append([]string{"exec", "-i", containerName(d.record)}, args...)...)
}

var engineVersion = regexp.MustCompile("[0-9]+\\.[0-9]+(?:\\.[0-9]+)?")

func (d *SnapshotDatabase) Version() (string, error) {
	var out bytes.Buffer
	binary := map[string]string{"postgresql": "postgres", "mysql": "mysqld", "mongodb": "mongod", "redis": "redis-server"}[d.Engine]
	if err := d.exec(nil, &out, binary, "--version"); err == nil {
		if version := engineVersion.FindString(out.String()); version != "" {
			return version, nil
		}
	}
	return "", errors.New("não foi possível identificar a versão do servidor")
}

func SnapshotFormat(engine string) string {
	return map[string]string{"postgresql": "dump", "mysql": "sql.gz", "mongodb": "archive.gz", "redis": "rdb"}[engine]
}

func (d *SnapshotDatabase) Dump(out io.Writer, progress func(string)) error {
	progress("Gerando backup")
	input := strings.NewReader(d.secret.Admin + "\n")
	switch d.Engine {
	case "postgresql":
		return d.exec(input, out, "/bin/sh", "-ec", pgAdmin+
			"exec pg_dump -w -U cfadmin -d cloudforge --format=custom")
	case "mysql":
		progress("Gerando e compactando backup")
		compressed := gzip.NewWriter(out)
		err := d.exec(input, compressed, "/bin/sh", "-ec", mysqlAdmin+
			"exec mysqldump --protocol=SOCKET -u root --single-transaction --quick --routines --events --triggers --hex-blob --no-tablespaces --set-gtid-purged=OFF cloudforge")
		return errors.Join(err, compressed.Close())
	case "mongodb":
		progress("Gerando e compactando backup")
		return d.exec(input, out, "/bin/sh", "-ec", mongoAdmin+
			"exec mongodump --host 127.0.0.1 --port 27017 --ssl --sslCAFile /run/cloudforge/ca.crt --username cfadmin --password \"$password\" --authenticationDatabase admin --db cloudforge --archive --gzip")
	case "redis":
		return d.dumpRedis(input, out)
	}
	return errors.New("SGBD sem suporte a snapshots")
}

const pgAdmin = "IFS= read -r PGPASSWORD; export PGPASSWORD\n"
const pgClient = "IFS= read -r PGPASSWORD; export PGPASSWORD PGSSLMODE=verify-full PGSSLROOTCERT=/run/cloudforge/ca.crt\n"
const mysqlAdmin = "IFS= read -r MYSQL_PWD; export MYSQL_PWD\n"
const mongoAdmin = "IFS= read -r password\n"
const redisAdmin = "IFS= read -r REDISCLI_AUTH; export REDISCLI_AUTH\n"

func (d *SnapshotDatabase) Restore(file *os.File, format, sourceVersion string, remote bool, progress func(string)) error {
	progress("Validando backup")
	version, err := d.Version()
	if err != nil {
		return err
	}
	if sourceVersion != "" && !compatibleVersion(d.Engine, sourceVersion, version) {
		return fmt.Errorf("versões incompatíveis (%s → %s)", sourceVersion, version)
	}
	if !supportsSnapshotFormat(d.Engine, format) {
		return errors.New("formato incompatível com o banco de destino")
	}
	var contents io.Reader = file
	if strings.HasSuffix(format, ".gz") {
		z, err := gzip.NewReader(file)
		if err != nil {
			return errors.New("arquivo gzip inválido")
		}
		defer z.Close()
		contents = z
	}
	reader := bufio.NewReaderSize(contents, 16*1024)
	head, err := reader.Peek(16)
	if err != nil {
		return errors.New("arquivo vazio, truncado ou corrompido")
	}
	switch d.Engine {
	case "postgresql":
		if format == "dump" {
			if !bytes.HasPrefix(head, []byte("PGDMP")) {
				return errors.New("arquivo fora do formato customizado do PostgreSQL")
			}
			// Validate the archive header and TOC without applying SQL.
			var listing bytes.Buffer
			if err := d.exec(reader, &listing, "pg_restore", "--list"); err != nil {
				return errors.New("arquivo PostgreSQL corrompido ou gerado por uma versão incompatível")
			}
			if !compatibleDumpHeader(d.Engine, listing.String(), version) {
				return errors.New("o backup foi gerado por uma versão mais recente do PostgreSQL")
			}
			if _, err := file.Seek(0, io.SeekStart); err != nil {
				return err
			}
			reader.Reset(file)
		} else if err := validateSQL(reader, d.Engine, version); err != nil {
			return err
		}
	case "mysql":
		if err := validateSQL(reader, d.Engine, version); err != nil {
			return err
		}
		// Validate the full gzip stream before a non-transactional restore.
		if z, ok := contents.(*gzip.Reader); ok {
			if _, err := io.Copy(io.Discard, reader); err != nil {
				return errors.New("arquivo gzip corrompido")
			}
			if _, err := file.Seek(0, io.SeekStart); err != nil {
				return err
			}
			if err := z.Reset(file); err != nil {
				return err
			}
			reader.Reset(z)
		}
	case "mongodb":
		if !bytes.HasPrefix(head, []byte{0x6d, 0xe2, 0x99, 0x81}) {
			return errors.New("arquivo fora do formato archive do mongodump")
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return err
		}
		reader.Reset(file)
	case "redis":
		if !bytes.HasPrefix(head, []byte("REDIS")) {
			return errors.New("arquivo fora do formato RDB do Redis")
		}
	}

	progress("Restaurando banco de dados")
	adminInput := io.MultiReader(strings.NewReader(d.secret.Admin+"\n"), reader)
	passwordInput := io.MultiReader(strings.NewReader(d.secret.Password+"\n"), reader)
	switch d.Engine {
	case "postgresql":
		if format == "dump" {
			return d.exec(passwordInput, io.Discard, "/bin/sh", "-ec", pgClient+
				"exec pg_restore -w -h 127.0.0.1 -U cloudforge -d cloudforge --clean --if-exists --no-owner --no-privileges --single-transaction --exit-on-error")
		}
		safeSQL, err := restrictedSQL(reader)
		if err != nil {
			return err
		}
		defer safeSQL.Close()
		return d.exec(io.MultiReader(strings.NewReader(d.secret.Password+"\n"), safeSQL), io.Discard, "/bin/sh", "-ec", pgClient+
			"exec psql -X -w -h 127.0.0.1 -U cloudforge -d cloudforge -v ON_ERROR_STOP=1 --single-transaction -f -")
	case "mysql":
		// The app account can only change data in cloudforge.
		return d.exec(passwordInput, io.Discard, "/bin/sh", "-ec", mysqlAdmin+
			"exec mysql --binary-mode --local-infile=0 --protocol=TCP --ssl-mode=VERIFY_IDENTITY --ssl-ca=/run/cloudforge/ca.crt -h localhost -u cloudforge cloudforge")
	case "mongodb":
		if !remote {
			if err := d.validateMongo(file); err != nil {
				return err
			}
			reader.Reset(file)
		}
		return d.exec(adminInput, io.Discard, "/bin/sh", "-ec", mongoAdmin+mongoRestore+" --drop")
	case "redis":
		return d.restoreRedis(adminInput)
	}
	return errors.New("SGBD sem suporte a restauração")
}

const mongoRestore = "exec mongorestore --host 127.0.0.1 --port 27017 --ssl --sslCAFile /run/cloudforge/ca.crt --username cfadmin --password \"$password\" --authenticationDatabase admin --nsInclude 'cloudforge.*' --archive --gzip --stopOnError"

func (d *SnapshotDatabase) validateMongo(file *os.File) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	input := io.MultiReader(strings.NewReader(d.secret.Admin+"\n"), file)
	if err := d.exec(input, io.Discard, "/bin/sh", "-ec", mongoAdmin+mongoRestore+" --dryRun"); err != nil {
		return errors.New("archive inválido ou incompatível com o MongoDB de destino")
	}
	_, err := file.Seek(0, io.SeekStart)
	return err
}

func supportsSnapshotFormat(engine, format string) bool {
	return SnapshotFormat(engine) == format || (format == "sql" && (engine == "mysql" || engine == "postgresql"))
}

func SnapshotImportFormat(engine, filename string) (string, error) {
	for _, format := range []string{"dump", "sql", "sql.gz", "archive.gz", "rdb"} {
		if strings.HasSuffix(strings.ToLower(filename), "."+format) && supportsSnapshotFormat(engine, format) {
			return format, nil
		}
	}
	return "", errors.New("a extensão do arquivo não é compatível com o banco de destino")
}

func compatibleVersion(engine, source, target string) bool {
	sourceParts := strings.Split(source, ".")
	targetParts := strings.Split(target, ".")
	a, _ := strconv.Atoi(sourceParts[0])
	b, _ := strconv.Atoi(targetParts[0])
	if a == 0 || b == 0 {
		return false
	}
	if engine == "postgresql" {
		return a <= b
	}
	if engine == "mysql" {
		return a == b && len(sourceParts) >= 2 && len(targetParts) >= 2 && sourceParts[1] == targetParts[1]
	}
	return a == b
}

func compatibleDumpHeader(engine, text, target string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, ";- "))
		if strings.HasPrefix(line, "Dumped from database version") || strings.HasPrefix(line, "Server version") ||
			strings.HasPrefix(line, "Dumped by pg_dump version") {
			if source := engineVersion.FindString(line); source != "" && !compatibleVersion(engine, source, target) {
				return false
			}
		}
	}
	return true
}

func validateSQL(reader *bufio.Reader, engine, version string) error {
	header, _ := reader.Peek(16 * 1024)
	text := strings.TrimSpace(strings.TrimPrefix(string(header), "\ufeff"))
	if strings.HasPrefix(text, "PGDMP") || strings.ContainsRune(text, 0) ||
		(engine == "mysql" && strings.Contains(text, "PostgreSQL database dump")) ||
		(engine == "postgresql" && strings.Contains(text, "MySQL dump")) {
		return errors.New("arquivo SQL inválido ou de outro SGBD")
	}
	sqlStart := regexp.MustCompile("(?im)^\\s*(?:--|/\\*|#|\\\\restrict\\b|(?:CREATE|INSERT|SET|ALTER|DROP|COPY|BEGIN|START|USE|TRUNCATE|UPDATE|DELETE|SELECT|WITH)\\b)")
	if !sqlStart.MatchString(text) || !compatibleDumpHeader(engine, text, version) {
		return errors.New("arquivo SQL inválido ou versão incompatível")
	}
	return nil
}
