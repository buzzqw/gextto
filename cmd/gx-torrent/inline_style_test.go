package main

// inline_style_test.go keeps presentation out of the daemon's markup: a page
// carries a class, theme.css holds what the class looks like. A value that comes
// from the data may ride as a custom property — style="--pct:42%" — because it
// is data, not a decision about how the page should look.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDaemonPagesCarryNoInlineStyles(t *testing.T) {
	dir := filepath.Join("..", "..", "internal", "webui", "assets")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the assets folder: %v", err)
	}
	inline := regexp.MustCompile(`style="`)
	pages := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !(strings.HasSuffix(name, ".html") || strings.HasSuffix(name, ".tmpl")) {
			continue
		}
		pages++
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(body)
		for _, loc := range inline.FindAllStringIndex(text, -1) {
			value := text[loc[1]:]
			if end := strings.IndexByte(value, '"'); end >= 0 {
				value = value[:end]
			}
			if strings.HasPrefix(value, "--") {
				continue
			}
			line := strings.Count(text[:loc[0]], "\n") + 1
			t.Errorf("%s:%d carries an inline style: %q — use a class, or a custom property when the value comes from the data", name, line, value)
		}
	}
	if pages == 0 {
		t.Fatal("no page was read: the guard is not looking at the real pages")
	}
}
