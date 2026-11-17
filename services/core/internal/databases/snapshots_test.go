package databases

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSnapshotCompatibility(t *testing.T) {
	for _, tc := range []struct {
		engine, source, target string
		expected               bool
	}{
		{"postgresql", "16.9", "17.9", true}, {"postgresql", "18.1", "17.9", false},
		{"mysql", "5.7", "8.4", false}, {"mongo", "8.0", "8.1", true},
		{"redis", "unknown", "7.4", false},
	} {
		if compatibleVersion(tc.engine, tc.source, tc.target) != tc.expected {
			t.Errorf("unexpected compatibility for %+v", tc)
		}
	}
	for _, tc := range []struct {
		engine, name string
		valid        bool
	}{
		{"postgresql", "example.dump", true}, {"postgresql", "example.SQL", true}, {"postgresql", "example.zip", true},
		{"mysql", "example.sql.gz", true}, {"mysql", "example.dump", false},
		{"mongodb", "example.archive.gz", true}, {"redis", "example.rdb", true},
	} {
		if _, err := SnapshotImportFormat(tc.engine, tc.name); (err == nil) != tc.valid {
			t.Errorf("unexpected import for %+v (%v)", tc, err)
		}
	}
	if err := validateSQL(bufio.NewReaderSize(strings.NewReader("-- PostgreSQL database dump\nSELECT 1;"), 16384), "mysql", "8.4"); err == nil {
		t.Fatal("MySQL accepted a PostgreSQL dump")
	}
}

func TestRestrictedSQLPreservesLargeLines(t *testing.T) {
	line := strings.Repeat("é", 25000)
	source := "-- PostgreSQL database dump\n\\restrict OriginalKey\n" + line + "\n\\unrestrict OriginalKey\n"
	reader, err := restrictedSQL(bufio.NewReaderSize(strings.NewReader(source), 16384))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.HasPrefix(text, "\\restrict ") || strings.Contains(text, "OriginalKey") || !strings.Contains(text, line) {
		t.Fatal("the SQL wrapper lost data or preserved the original restriction key")
	}
}

func TestPostgresSnapshotArchiveObjects(t *testing.T) {
	empty := ";     TOC Entries: 5\n; Selected TOC Entries:\n5; 2615 2200 SCHEMA - public cloudforge\n"
	if hasPostgresSnapshotObjects(empty) {
		t.Fatal("the default public schema was counted as a restorable object")
	}
	for _, entry := range []string{
		"6; 2615 16401 SCHEMA - app cloudforge",
		"7; 1259 16402 TABLE public accounts cloudforge",
		"8; 0 16402 TABLE DATA public accounts cloudforge",
	} {
		if !hasPostgresSnapshotObjects(empty + entry + "\n") {
			t.Fatalf("restorable entry was ignored: %s", entry)
		}
	}
}

func TestPostgresSnapshotInstanceDocker(t *testing.T) {
	if os.Getenv("CLOUDFORGE_SNAPSHOT_DOCKER_TEST") != "1" {
		t.Skip("requires local Docker and the Cloudforge PostgreSQL image")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	m := testManager(t)
	source := disposableSnapshotDB(t, ctx, m, "Origem snapshot instância", "postgresql")
	target := disposableSnapshotDB(t, ctx, m, "Destino snapshot instância", "postgresql")
	admin := func(db *SnapshotDatabase, database, statement string) string {
		t.Helper()
		var out bytes.Buffer
		if err := db.exec(strings.NewReader(db.secret.Admin+"\n"+statement), &out,
			"/bin/sh", "-ec", pgAdmin+"exec psql -X -w -U cfadmin -d \"$1\" -Atq -v ON_ERROR_STOP=1",
			"setup", postgresSnapshotDSN(database)); err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(out.String())
	}
	admin(source, DatabaseName, `CREATE DATABASE "NCT" OWNER cloudforge TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C';`)
	admin(source, "NCT", `CREATE TABLE usuarios(value text); INSERT INTO usuarios VALUES ('snapshot NCT');`)
	admin(target, DatabaseName, `CREATE TABLE stale(value text);`)
	archive, err := os.Create(filepath.Join(t.TempDir(), "snapshot.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	trace := func(stage string) { t.Log(stage) }
	if err := source.Dump(archive, trace); err != nil {
		t.Fatal(err)
	}
	manifest, entries, err := readPostgresSnapshot(archive)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Databases) != 2 || manifest.Databases[0].Name != "NCT" || manifest.Databases[1].Name != "cloudforge" {
		t.Fatalf("snapshot databases: %+v", manifest.Databases)
	}
	if err := target.Restore(archive, "zip", "", false, "", trace); err != nil {
		t.Fatal(err)
	}
	if got := admin(target, "NCT", "SET ROLE cloudforge; SELECT value FROM usuarios;"); got != "snapshot NCT" {
		t.Fatalf("restored row: %q", got)
	}
	if got := admin(target, "NCT", "SELECT datcollate FROM pg_catalog.pg_database WHERE datname = current_database();"); got != "C" {
		t.Fatalf("restored collation: %q", got)
	}
	if got := admin(target, DatabaseName, "SELECT to_regclass('stale') IS NULL;"); got != "t" {
		t.Fatalf("empty database was not restored: %q", got)
	}
	admin(target, "NCT", "UPDATE usuarios SET value='destino';")
	offset, err := entries[postgresDumpEntry(1)].DataOffset()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := archive.WriteAt([]byte{0}, offset); err != nil {
		t.Fatal(err)
	}
	if err := target.Restore(archive, "zip", "", false, "", trace); err == nil {
		t.Fatal("the corrupted second database was accepted")
	}
	if got := admin(target, "NCT", "SELECT value FROM usuarios;"); got != "destino" {
		t.Fatalf("first database changed after restore failed: %q", got)
	}
	if got := admin(target, DatabaseName, "SELECT count(*) FROM pg_database WHERE datname LIKE 'cf_snapshot_%';"); got != "0" {
		t.Fatalf("restore left temporary databases: %s", got)
	}
}

func TestSnapshotDockerRoundTrip(t *testing.T) {
	if os.Getenv("CLOUDFORGE_SNAPSHOT_DOCKER_TEST") != "1" {
		t.Skip("requires local Docker and the Cloudforge engine images")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Minute)
	defer cancel()
	for _, engine := range []string{"postgresql", "mysql", "mongodb", "redis"} {
		t.Run(engine, func(t *testing.T) {
			m := testManager(t)
			if !m.docker.available(ctx) {
				t.Fatal("Docker local indisponível")
			}
			source := disposableSnapshotDB(t, ctx, m, "Snapshot origem", engine)
			target := disposableSnapshotDB(t, ctx, m, "Snapshot destino", engine)
			if got := snapshotSample(t, source, "create"); got != "" {
				t.Logf("preparo: %s", got)
			}
			version, err := source.Version()
			if err != nil {
				t.Fatal(err)
			}
			file, err := os.Create(filepath.Join(t.TempDir(), "snapshot."+SnapshotFormat(engine)))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err = source.Dump(file, func(s string) { t.Log(s) }); err != nil {
				t.Fatal(err)
			}
			snapshotSample(t, source, "change")
			if _, err = file.Seek(0, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			if err = source.Restore(file, SnapshotFormat(engine), version, true, "", func(s string) { t.Log(s) }); err != nil {
				t.Fatal(err)
			}
			if _, err = file.Seek(0, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			if err = target.Restore(file, SnapshotFormat(engine), version, false, "", func(s string) { t.Log(s) }); err != nil {
				t.Fatal(err)
			}
			if got := snapshotSample(t, source, "read"); got != "cloudforge-snapshot" {
				t.Fatalf("origem: %q", got)
			}
			if got := snapshotSample(t, target, "read"); got != "cloudforge-snapshot" {
				t.Fatalf("destino: %q", got)
			}
			if engine == "postgresql" {
				legacy, err := os.Create(filepath.Join(t.TempDir(), "legacy.dump"))
				if err != nil {
					t.Fatal(err)
				}
				defer legacy.Close()
				if err := source.dumpPostgresDatabase(legacy, DatabaseName); err != nil {
					t.Fatal(err)
				}
				if _, err := legacy.Seek(0, io.SeekStart); err != nil {
					t.Fatal(err)
				}
				snapshotSample(t, target, "change")
				if err := target.Restore(legacy, "dump", "", false, DatabaseName, func(s string) { t.Log(s) }); err != nil {
					t.Fatal(err)
				}
				if got := snapshotSample(t, target, "read"); got != "cloudforge-snapshot" {
					t.Fatalf("legacy dump: %q", got)
				}
				sql, err := os.Create(filepath.Join(t.TempDir(), "plain.sql"))
				if err != nil {
					t.Fatal(err)
				}
				defer sql.Close()
				if _, err = io.WriteString(sql, "-- PostgreSQL database dump\n\\restrict OriginalKey\nDROP TABLE IF EXISTS snapshot_sample;\nCREATE TABLE snapshot_sample(value text);\nCOPY snapshot_sample FROM stdin;\ncloudforge-snapshot\n\\.\n\\unrestrict OriginalKey\n"); err != nil {
					t.Fatal(err)
				}
				if _, err = sql.Seek(0, io.SeekStart); err != nil {
					t.Fatal(err)
				}
				if err = target.Restore(sql, "sql", "", false, "", func(s string) { t.Log(s) }); err != nil {
					t.Fatal(err)
				}
				if got := snapshotSample(t, target, "read"); got != "cloudforge-snapshot" {
					t.Fatalf("SQL local: %q", got)
				}
			}
		})
	}
}

func disposableSnapshotDB(t *testing.T, ctx context.Context, m *Manager, name, engine string) *SnapshotDatabase {
	t.Helper()
	id, err := m.Create(ctx, "snapshot-test", Input{Name: name, Engine: engine})
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.get("snapshot-test", id.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		clean, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := m.docker.remove(clean, r); err != nil {
			t.Error(err)
		}
	})
	if err = m.reconcile(ctx, &r); err != nil {
		t.Fatal(err)
	}
	db, release, err := m.ReserveSnapshot(ctx, "snapshot-test", r.ID)
	if err != nil {
		t.Fatalf("reservar %s (%s): %v", engine, r.State, err)
	}
	t.Cleanup(release)
	return db
}

func snapshotSample(t *testing.T, d *SnapshotDatabase, action string) string {
	t.Helper()
	var out bytes.Buffer
	var input string
	var args []string
	switch d.Engine {
	case "postgresql":
		input = d.secret.Password + "\n"
		sql := "SELECT value FROM snapshot_sample;"
		if action == "create" {
			sql = "CREATE TABLE snapshot_sample(value text); INSERT INTO snapshot_sample VALUES ('cloudforge-snapshot');"
		} else if action == "change" {
			sql = "UPDATE snapshot_sample SET value='after-snapshot';"
		}
		args = []string{"/bin/sh", "-ec", pgClient + "exec psql -X -w -h 127.0.0.1 -U cloudforge -d cloudforge -Atq -v ON_ERROR_STOP=1 -c \"$1\"", "sample", sql}
	case "mysql":
		input = d.secret.Password + "\n"
		sql := "SELECT value FROM snapshot_sample;"
		if action == "create" {
			sql = "CREATE TABLE snapshot_sample(value varchar(64)); INSERT INTO snapshot_sample VALUES ('cloudforge-snapshot');"
		} else if action == "change" {
			sql = "UPDATE snapshot_sample SET value='after-snapshot';"
		}
		args = []string{"/bin/sh", "-ec", mysqlAdmin + "exec mysql --protocol=TCP --ssl-mode=VERIFY_IDENTITY --ssl-ca=/run/cloudforge/ca.crt -h localhost -u cloudforge --batch --skip-column-names cloudforge -e \"$1\"", "sample", sql}
	case "mongodb":
		input = d.secret.Admin + "\n"
		js := "print(db.snapshot_sample.findOne().value)"
		if action == "create" {
			js = "db.snapshot_sample.insertOne({value:'cloudforge-snapshot'})"
		} else if action == "change" {
			js = "db.snapshot_sample.updateOne({},{$set:{value:'after-snapshot'}})"
		}
		args = []string{"/bin/sh", "-ec", mongoAdmin + "exec mongosh --quiet --tls --tlsCAFile /run/cloudforge/ca.crt --host 127.0.0.1 --username cfadmin --password \"$password\" --authenticationDatabase admin --eval \"$1\" cloudforge", "sample", js}
	case "redis":
		input = d.secret.Admin + "\n"
		args = []string{"/bin/sh", "-ec", redisAdmin + "exec " + redisCLI + " -n 3 \"$@\"", "sample"}
		if action == "create" {
			args = append(args, "SET", "snapshot_sample", "cloudforge-snapshot", "EX", "600")
		} else if action == "change" {
			args = append(args, "SET", "snapshot_sample", "after-snapshot")
		} else {
			args = append(args, "GET", "snapshot_sample")
		}
	}
	if err := d.exec(strings.NewReader(input), &out, args...); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out.String())
}
