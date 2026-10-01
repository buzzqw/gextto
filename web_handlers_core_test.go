package gextto

import (
	"os"
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
