package gextto

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoreTailLinesAlignsUTF8Window(t *testing.T) {
	path := t.TempDir() + "/gextto.log"
	content := "xx" + strings.Repeat("😀\n", 60000)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	lines := coreTailLines(path, 500)
	if len(lines) != 500 {
		t.Fatalf("coreTailLines returned %d lines, want 500", len(lines))
	}
	if lines[0] != "😀" || lines[len(lines)-1] != "😀" {
		t.Fatalf("unexpected UTF-8 log lines: first=%q last=%q", lines[0], lines[len(lines)-1])
	}
}

func TestCoreLogFilesAndResolve(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gextto.log"), []byte("current\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gextto.log.2"), []byte("older\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	files := coreLogFiles(dir)
	want := []string{"gextto.log", "gextto.log.2"}
	if len(files) != len(want) || files[0] != want[0] || files[1] != want[1] {
		t.Fatalf("coreLogFiles = %#v, want %#v", files, want)
	}

	if path, name := coreResolveLog(dir, "gextto.log.2"); name != "gextto.log.2" || path != filepath.Join(dir, "gextto.log.2") {
		t.Fatalf("coreResolveLog backup = (%q,%q)", path, name)
	}
	// Path traversal and unknown names fall back to the current log.
	if path, name := coreResolveLog(dir, "../../etc/passwd"); name != "gextto.log" || path != filepath.Join(dir, "gextto.log") {
		t.Fatalf("coreResolveLog traversal = (%q,%q)", path, name)
	}
}
