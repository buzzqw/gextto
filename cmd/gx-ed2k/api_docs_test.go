package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestAPIRoutesAreDocumented keeps the gx-ed2k /api/v1 table in docs/API.md
// aligned with the routes registered in api.go. It is the sibling of
// cmd/gx-torrent's guard: a route added here without a line in the document
// (or the reverse) fails the suite.
func TestAPIRoutesAreDocumented(t *testing.T) {
	registered := map[string]bool{}
	raw, err := os.ReadFile("api.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range regexp.MustCompile(`HandleFunc\("((?:GET|POST|DELETE|PUT) /api/v1[^"]*)"`).FindAllStringSubmatch(string(raw), -1) {
		registered[match[1]] = true
	}
	if len(registered) == 0 {
		t.Fatal("no /api/v1 route found in api.go")
	}
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "API.md"))
	if err != nil {
		t.Skip("API documentation is unavailable outside the source tree")
	}
	contents := string(doc)
	start := strings.Index(contents, "## gx-ed2k daemon routes")
	if start < 0 {
		t.Fatal("docs/API.md has no gx-ed2k section")
	}
	documented := map[string]bool{}
	row := regexp.MustCompile("^\\|\\s*(GET|POST|DELETE|PUT)\\s*\\|\\s*`([^`]+)`\\s*\\|")
	for _, line := range strings.Split(contents[start:], "\n") {
		match := row.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		path := match[2]
		if index := strings.IndexByte(path, '?'); index >= 0 {
			path = path[:index]
		}
		documented[match[1]+" "+path] = true
	}
	if len(documented) == 0 {
		t.Fatal("no gx-ed2k route documented")
	}
	for route := range registered {
		if !documented[route] {
			t.Errorf("registered gx-ed2k route is not documented: %s", route)
		}
	}
	for route := range documented {
		if !registered[route] {
			t.Errorf("documented gx-ed2k route is not registered: %s", route)
		}
	}
}
