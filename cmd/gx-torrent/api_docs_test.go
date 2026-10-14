package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// apiRoutePattern matches the "METHOD /api/vN/..." literal of a HandleFunc
// registration in the daemon source, the same way the routes are written in
// api.go and api_qbit.go.
var apiRoutePattern = regexp.MustCompile(`HandleFunc\("((?:GET|POST|DELETE|PUT) /api/v[12][^"]*)"`)

// TestAPIRoutesAreDocumented keeps the daemon's /api/v1 and /api/v2 tables in
// docs/API.md aligned with the registered routes: a route added in the code but
// not written down (or the other way round) fails here, so the document cannot
// silently drift from the daemon.
func TestAPIRoutesAreDocumented(t *testing.T) {
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	registered := map[string]bool{}
	for _, path := range sources {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range apiRoutePattern.FindAllStringSubmatch(string(raw), -1) {
			registered[m[1]] = true
		}
	}
	if len(registered) == 0 {
		t.Fatal("no /api/v1 or /api/v2 route found in the daemon source")
	}

	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "API.md"))
	if err != nil {
		t.Skip("API documentation is unavailable outside the source tree")
	}
	documented := map[string]bool{}
	contents := string(raw)
	if idx := strings.Index(contents, "## gx-torrent daemon routes"); idx >= 0 {
		for _, line := range strings.Split(contents[idx:], "\n") {
			parts := strings.Split(line, "`")
			if len(parts) != 3 || !strings.HasPrefix(parts[0], "| ") || !strings.HasSuffix(parts[2], " |") {
				continue
			}
			method := strings.TrimSpace(strings.Trim(strings.TrimSpace(parts[0]), "|"))
			if method == "GET" || method == "POST" || method == "DELETE" || method == "PUT" {
				documented[method+" "+parts[1]] = true
			}
		}
	}
	if len(documented) == 0 {
		t.Fatal("no gx-torrent daemon route documented in docs/API.md")
	}

	for route := range registered {
		if !documented[route] {
			t.Errorf("registered daemon route is not documented: %s", route)
		}
	}
	for route := range documented {
		if !registered[route] {
			t.Errorf("documented daemon route is not registered: %s", route)
		}
	}
}
