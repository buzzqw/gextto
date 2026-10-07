package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestEveryUsedKeyIsTranslated scans the package sources for literal catalog
// keys passed to T/Format and checks each one exists: a missing key is shown
// raw on screen (e.g. "settings.hint").
func TestEveryUsedKeyIsTranslated(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`\.(?:T|Format)\("([a-z0-9_]+\.[a-z0-9_.]+)"`)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range pattern.FindAllStringSubmatch(string(source), -1) {
			if _, ok := catalog[match[1]]; !ok {
				t.Errorf("%s: key %q is not in the catalog", file, match[1])
			}
		}
	}
}
