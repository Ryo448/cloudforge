package projects

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectionAndPaths(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"start":"node index.js"}}`), 0600)
	os.WriteFile(filepath.Join(root, "helper.py"), []byte("print('secondary')"), 0600)
	d, e := Detect(root)
	if e != nil || len(d.Runtimes) != 1 || d.Runtimes[0] != "node" {
		t.Fatalf("secondary Python file must not override Node: %+v %v", d, e)
	}
	os.WriteFile(filepath.Join(root, "requirements.txt"), nil, 0600)
	d, e = Detect(root)
	if e != nil || len(d.Runtimes) != 2 {
		t.Fatal("mixed manifests must require a selection")
	}
	if _, e = SafePath(root, "../outside"); e == nil {
		t.Fatal("path traversal accepted")
	}
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	os.WriteFile(secret, []byte("secret"), 0600)
	if e = os.Symlink(outside, filepath.Join(root, "external")); e == nil {
		if _, e = Read(root, "external/secret.txt"); e == nil {
			t.Fatal("symlink escaped project")
		}
	}
}

func TestWriteTextChecksVersionAndProjectBoundary(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "app.py")
	if err := os.WriteFile(path, []byte("print('old')\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, version, err := ReadText(root, "app.py")
	if err != nil {
		t.Fatal(err)
	}
	next, err := WriteText(root, "app.py", "print('new')\n", version)
	if err != nil || next == version {
		t.Fatalf("save: version=%q err=%v", next, err)
	}
	if _, err := WriteText(root, "app.py", "overwrite", version); err == nil {
		t.Fatal("stale version overwrote the file")
	}
	content, _, err := ReadText(root, "app.py")
	if err != nil || content != "print('new')\n" {
		t.Fatalf("content after conflict: %q, %v", content, err)
	}
	if _, err := WriteText(root, "../outside", "bad", next); err == nil {
		t.Fatal("write escaped the project")
	}
}
