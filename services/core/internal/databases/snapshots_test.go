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
		{"postgresql", "example.dump", true}, {"postgresql", "example.SQL", true},
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
			if err = source.Restore(file, SnapshotFormat(engine), version, true, func(s string) { t.Log(s) }); err != nil {
				t.Fatal(err)
			}
			if _, err = file.Seek(0, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			if err = target.Restore(file, SnapshotFormat(engine), version, false, func(s string) { t.Log(s) }); err != nil {
				t.Fatal(err)
			}
			if got := snapshotSample(t, source, "read"); got != "cloudforge-snapshot" {
				t.Fatalf("origem: %q", got)
			}
			if got := snapshotSample(t, target, "read"); got != "cloudforge-snapshot" {
				t.Fatalf("destino: %q", got)
			}
			if engine == "postgresql" {
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
				if err = target.Restore(sql, "sql", "", false, func(s string) { t.Log(s) }); err != nil {
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
