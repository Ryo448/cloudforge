package databases

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lib/pq"
)

const postgresManifestName = "manifest.json"
const postgresManifestVersion = 1
const postgresManifestLimit = 1 << 20

var postgresRestoreName = regexp.MustCompile(`^cf_snapshot_[0-9a-f]{24}_(new|old)_[0-9]+$`)

var ErrSnapshotCleanup = errors.New("dados restaurados, mas a limpeza das cópias anteriores ficou pendente")

type postgresSnapshotManifest struct {
	Format    int                        `json:"cloudforgePostgresql"`
	Databases []postgresSnapshotDatabase `json:"databases"`
}

type postgresSnapshotDatabase struct {
	Name           string `json:"name"`
	Encoding       string `json:"encoding"`
	LocaleProvider string `json:"localeProvider"`
	Collate        string `json:"collate"`
	CType          string `json:"ctype"`
	Locale         string `json:"locale,omitempty"`
	ICURules       string `json:"icuRules,omitempty"`
}

func (db postgresSnapshotDatabase) createSQL(stage string) (string, error) {
	var provider, locale string
	switch db.LocaleProvider {
	case "c":
		provider = "libc"
	case "b":
		provider = "builtin"
		locale = " BUILTIN_LOCALE " + pq.QuoteLiteral(db.Locale)
	case "i":
		provider = "icu"
		locale = " ICU_LOCALE " + pq.QuoteLiteral(db.Locale)
		if db.ICURules != "" {
			locale += " ICU_RULES " + pq.QuoteLiteral(db.ICURules)
		}
	default:
		return "", errors.New("provedor de ordenação PostgreSQL inválido no snapshot")
	}
	return fmt.Sprintf("CREATE DATABASE %s WITH OWNER cloudforge TEMPLATE template0 ENCODING %s LOCALE_PROVIDER %s LC_COLLATE %s LC_CTYPE %s%s;",
		pq.QuoteIdentifier(stage), pq.QuoteLiteral(db.Encoding), provider, pq.QuoteLiteral(db.Collate), pq.QuoteLiteral(db.CType), locale), nil
}

func postgresDumpEntry(index int) string {
	return fmt.Sprintf("databases/%d.dump", index+1)
}

func postgresApplicationDatabase(name string) bool {
	switch name {
	case "", "postgres", "template0", "template1":
		return false
	}
	_, err := SnapshotDatabaseName("postgresql", name)
	return err == nil && !postgresRestoreName.MatchString(name)
}

func (d *SnapshotDatabase) postgresSnapshotAdmin(sql string, out io.Writer) error {
	return d.exec(strings.NewReader(d.secret.Admin+"\n"+sql), out, "/bin/sh", "-ec", pgAdmin+
		"exec psql -X -w -U cfadmin -d postgres -Atq -v ON_ERROR_STOP=1 -f -")
}

func (d *SnapshotDatabase) postgresSnapshotDatabases() ([]postgresSnapshotDatabase, error) {
	var out bytes.Buffer
	err := d.postgresSnapshotAdmin(`SELECT COALESCE(json_agg(json_build_object(
			'name', datname, 'encoding', pg_encoding_to_char(encoding), 'localeProvider', datlocprovider,
			'collate', datcollate, 'ctype', datctype, 'locale', datlocale, 'icuRules', daticurules
		) ORDER BY datname), '[]')
		FROM pg_database WHERE NOT datistemplate AND datname <> 'postgres';`, &out)
	if err != nil {
		return nil, err
	}
	var databases []postgresSnapshotDatabase
	if err := json.Unmarshal(out.Bytes(), &databases); err != nil {
		return nil, err
	}
	return databases, nil
}

func (d *SnapshotDatabase) dumpPostgresDatabase(out io.Writer, name string) error {
	return d.exec(strings.NewReader(d.secret.Admin+"\n"), out, "/bin/sh", "-ec", pgAdmin+
		"exec pg_dump -w -U cfadmin -d \"$1\" --format=custom", "snapshot", postgresSnapshotDSN(name))
}

func (d *SnapshotDatabase) dumpPostgresInstance(out io.Writer, progress func(string)) error {
	databases, err := d.postgresSnapshotDatabases()
	if err != nil {
		return fmt.Errorf("não foi possível listar os bancos PostgreSQL: %w", err)
	}
	manifest := postgresSnapshotManifest{Format: postgresManifestVersion}
	for _, db := range databases {
		if postgresApplicationDatabase(db.Name) {
			manifest.Databases = append(manifest.Databases, db)
		}
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if len(manifest.Databases) == 0 || len(data) > postgresManifestLimit {
		return errors.New("não foi possível montar a lista de bancos do snapshot PostgreSQL")
	}
	archive := zip.NewWriter(out)
	header, err := archive.Create(postgresManifestName)
	if err != nil {
		return err
	}
	if _, err := header.Write(data); err != nil {
		return err
	}
	for index, db := range manifest.Databases {
		name := db.Name
		progress(fmt.Sprintf("Gerando backup de %q (%d/%d)", name, index+1, len(manifest.Databases)))
		entry, err := archive.CreateHeader(&zip.FileHeader{Name: postgresDumpEntry(index), Method: zip.Store})
		if err != nil {
			return err
		}
		if err := d.dumpPostgresDatabase(entry, name); err != nil {
			return fmt.Errorf("não foi possível exportar o banco PostgreSQL %q: %w", name, err)
		}
	}
	return archive.Close()
}

func readPostgresSnapshot(source *os.File) (postgresSnapshotManifest, map[string]*zip.File, error) {
	var manifest postgresSnapshotManifest
	info, err := source.Stat()
	if err != nil {
		return manifest, nil, err
	}
	archive, err := zip.NewReader(source, info.Size())
	if err != nil {
		return manifest, nil, errors.New("arquivo ZIP inválido ou corrompido")
	}
	entries := make(map[string]*zip.File, len(archive.File))
	for _, entry := range archive.File {
		if !entry.Mode().IsRegular() || entries[entry.Name] != nil {
			return manifest, nil, errors.New("entradas inválidas ou duplicadas no ZIP PostgreSQL")
		}
		entries[entry.Name] = entry
	}
	header := entries[postgresManifestName]
	if header == nil {
		return manifest, nil, errors.New("o ZIP precisa ser um snapshot PostgreSQL gerado pelo Cloudforge")
	}
	if header.UncompressedSize64 > postgresManifestLimit {
		return manifest, nil, errors.New("lista de bancos inválida no ZIP PostgreSQL")
	}
	reader, err := header.Open()
	if err != nil {
		return manifest, nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, postgresManifestLimit+1))
	if err != nil || len(data) > postgresManifestLimit || json.Unmarshal(data, &manifest) != nil ||
		manifest.Format != postgresManifestVersion || len(manifest.Databases) == 0 || len(entries) != len(manifest.Databases)+1 {
		return manifest, nil, errors.New("lista de bancos inválida no ZIP PostgreSQL")
	}
	seen := make(map[string]bool, len(manifest.Databases))
	for index, db := range manifest.Databases {
		name := db.Name
		entry := entries[postgresDumpEntry(index)]
		if !postgresApplicationDatabase(name) || seen[name] || entry == nil || entry.UncompressedSize64 < 16 ||
			entry.Method != zip.Store || entry.CompressedSize64 != entry.UncompressedSize64 {
			return manifest, nil, errors.New("banco inválido, duplicado ou ausente no ZIP PostgreSQL")
		}
		seen[name] = true
	}
	return manifest, entries, nil
}

func (d *SnapshotDatabase) restorePostgresDump(input io.Reader, name string, clean bool) error {
	command := "exec pg_restore -w -h 127.0.0.1 -U cloudforge -d \"$1\" --format=custom --no-owner --no-privileges --single-transaction --exit-on-error"
	if clean {
		command += " --clean --if-exists"
	} else {
		command += " --no-tablespaces"
	}
	return d.exec(io.MultiReader(strings.NewReader(d.secret.Password+"\n"), input), io.Discard,
		"/bin/sh", "-ec", pgClient+command, "restore", postgresSnapshotDSN(name))
}

// Copy through EOF to check the ZIP entry CRC before handing it to pg_restore.
func (d *SnapshotDatabase) restorePostgresEntry(entry *zip.File, dir, name string) error {
	source, err := entry.Open()
	if err != nil {
		return err
	}
	defer source.Close()
	temp, err := os.CreateTemp(dir, "cloudforge-postgres-*.part")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	buf := make([]byte, 256*1024)
	for {
		if err := d.ctx.Err(); err != nil {
			return err
		}
		n, err := source.Read(buf)
		if n > 0 {
			if _, writeErr := temp.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	if _, err := temp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return d.restorePostgresDump(temp, name, false)
}

func (d *SnapshotDatabase) restorePostgresInstance(file *os.File, progress func(string)) error {
	manifest, entries, err := readPostgresSnapshot(file)
	if err != nil {
		return err
	}
	current, err := d.postgresSnapshotDatabases()
	if err != nil {
		return fmt.Errorf("não foi possível listar os bancos de destino: %w", err)
	}
	exists := make(map[string]bool, len(current))
	for _, db := range current {
		exists[db.Name] = true
	}
	key, err := randomHex(12)
	if err != nil {
		return err
	}
	var staged, replaced []string
	defer func() { _ = d.dropPostgresSnapshotDatabases(staged) }()
	var swap strings.Builder
	swap.WriteString("BEGIN;\nSET LOCAL lock_timeout = '5s';\n")
	for index, db := range manifest.Databases {
		name := db.Name
		progress(fmt.Sprintf("Preparando banco %q (%d/%d)", name, index+1, len(manifest.Databases)))
		stage := fmt.Sprintf("cf_snapshot_%s_new_%d", key, index)
		stageID := pq.QuoteIdentifier(stage)
		create, err := db.createSQL(stage)
		if err != nil {
			return err
		}
		staged = append(staged, stage)
		if err := d.postgresSnapshotAdmin(create, io.Discard); err != nil {
			return fmt.Errorf("não foi possível preparar o banco %q: %w", name, err)
		}
		if err := d.restorePostgresEntry(entries[postgresDumpEntry(index)], filepath.Dir(file.Name()), stage); err != nil {
			return fmt.Errorf("não foi possível restaurar o banco %q: %w", name, err)
		}
		nameID := pq.QuoteIdentifier(name)
		if exists[name] {
			old := fmt.Sprintf("cf_snapshot_%s_old_%d", key, index)
			oldID := pq.QuoteIdentifier(old)
			fmt.Fprintf(&swap, "ALTER DATABASE %s RENAME TO %s;\nALTER DATABASE %s ALLOW_CONNECTIONS false;\n", nameID, oldID, oldID)
			replaced = append(replaced, old)
		}
		fmt.Fprintf(&swap, "ALTER DATABASE %s RENAME TO %s;\n", stageID, nameID)
	}
	swap.WriteString("COMMIT;\n")
	progress("Aplicando snapshot PostgreSQL")
	if err := d.postgresSnapshotAdmin(swap.String(), io.Discard); err != nil {
		return errors.New("não foi possível aplicar o snapshot. Encerre as conexões com os bancos de destino e tente novamente")
	}
	staged = nil
	if err := d.dropPostgresSnapshotDatabases(replaced); err != nil {
		return ErrSnapshotCleanup
	}
	return nil
}

func (d *SnapshotDatabase) dropPostgresSnapshotDatabases(names []string) error {
	if len(names) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cleanup := *d
	cleanup.ctx = ctx
	var result error
	for _, name := range names {
		err := cleanup.postgresSnapshotAdmin("DROP DATABASE IF EXISTS "+pq.QuoteIdentifier(name)+" WITH (FORCE);", io.Discard)
		result = errors.Join(result, err)
	}
	return result
}
