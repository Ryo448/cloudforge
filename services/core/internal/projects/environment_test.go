package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvironmentFileEditingPreservesOtherLines(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".env")
	if err := os.WriteFile(path, []byte("# keep this comment\nDB_HOST=localhost\nDB_PORT=5432\n"), 0600); err != nil {
		t.Fatal(err)
	}
	files, err := EnvironmentFiles(root)
	if err != nil || len(files) != 1 || files[0] != ".env" {
		t.Fatalf("files = %v, %v", files, err)
	}
	values, err := ReadEnvironment(root, ".env")
	if err != nil || len(values) != 2 || values[0].Value != "localhost" {
		t.Fatalf("values = %v, %v", values, err)
	}
	if err := WriteEnvironment(root, ".env", []EnvironmentVariable{{Key: "DB_HOST", Value: "db.internal"}, {Key: "API_TOKEN", Value: "two words"}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, expected := range []string{"# keep this comment", "DB_HOST=db.internal", `API_TOKEN="two words"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q in %q", expected, text)
		}
	}
	if strings.Contains(text, "DB_PORT=") {
		t.Fatalf("removed variable remained: %q", text)
	}
}

func TestEnvironmentSkipsTemplatesAndRejectsDuplicateKeys(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env.example"), []byte("TOKEN=example"), 0600); err != nil {
		t.Fatal(err)
	}
	files, err := EnvironmentFiles(root)
	if err != nil || len(files) != 0 {
		t.Fatalf("files = %v, %v", files, err)
	}
	if err := ValidateEnvironment([]EnvironmentVariable{{Key: "DB_HOST", Value: "a"}, {Key: "DB_HOST", Value: "b"}}); err == nil {
		t.Fatal("duplicate key accepted")
	}
}
func TestEnvironmentParsesQuotesAndComments(t *testing.T) {
	for _, test := range []struct{ line, key, value string }{
		{`API.URL="https://example.test/a #b" # note`, "API.URL", "https://example.test/a #b"},
		{`DB-HOST=db.internal # note`, "DB-HOST", "db.internal"},
	} {
		key, value, ok := parseEnvironmentLine(test.line)
		if !ok || key != test.key || value != test.value {
			t.Fatalf("parse %q = %q, %q, %v", test.line, key, value, ok)
		}
	}
}
