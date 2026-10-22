package gextto

// uiweb_static_guards_test.go holds cheap static guards for two classes of bug
// that local `make test` used to miss, both of which happened once:
//
//   - a UI button or table action pointing at an API route that was removed
//     (the dead "Applica ora" button that answered 404);
//   - the Playwright suite driving a setting or a tab that was renamed in the
//     code (the suite kept the old `libtorrent_*` ids and failed only in CI).
//
// They read the sources, so they need no browser and run with the Go suite.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// routePlaceholder normalises the {param} placeholders so two spellings of the
// same route (e.g. {hash} and {id}) compare equal.
var routePlaceholder = regexp.MustCompile(`\{[^}]+\}`)

func normaliseRoute(path string) string {
	return routePlaceholder.ReplaceAllString(path, "{}")
}

// TestUIActionPathsAreRegisteredRoutes fails when a button or table action in
// the Go-rendered UI points at an /api/ path with no registered route, so a
// removed endpoint cannot leave a dead button that answers 404.
func TestUIActionPathsAreRegisteredRoutes(t *testing.T) {
	Router(newTestAppState(t))
	known := map[string]bool{}
	for _, route := range RegisteredRoutes() {
		if _, path, ok := strings.Cut(route, " "); ok {
			known[normaliseRoute(path)] = true
		}
	}

	files, _ := filepath.Glob("uiweb*.go")
	templates, _ := filepath.Glob("uiweb/v2/templates/*.html")
	files = append(files, templates...)
	goPath := regexp.MustCompile(`Path:\s*"([^"]+)"`)
	htmlPath := regexp.MustCompile(`"Path"\s+"([^"]+)"`)
	var missing []string
	seen := map[string]bool{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		re := goPath
		if strings.HasSuffix(path, ".html") {
			re = htmlPath
		}
		for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
			target := strings.TrimSpace(m[1])
			if !strings.HasPrefix(target, "/api/") || seen[target] {
				continue
			}
			seen[target] = true
			if !known[normaliseRoute(target)] {
				missing = append(missing, filepath.Base(path)+" -> "+target)
			}
		}
	}
	if len(missing) > 0 {
		t.Errorf("UI actions point at routes that are not registered:\n  %s", strings.Join(missing, "\n  "))
	}
	if len(seen) == 0 {
		t.Fatal("no UI action path found: the Path pattern no longer matches the UI sources")
	}
}

// TestEnd2EndSelectorsReferenceRealSettings fails when the Playwright suite
// drives a setting (#input-<key> / #v2-setting-<key>) or a settings tab that no
// longer exists, so a rename cannot leave the suite red on stale ids.
func TestEnd2EndSelectorsReferenceRealSettings(t *testing.T) {
	tests, _ := filepath.Glob("uiweb/end2end/tests/*.ts")
	if len(tests) == 0 {
		t.Skip("end2end tests are unavailable outside the source tree")
	}
	keys := map[string]bool{}
	for _, def := range append(append([]uiSettingDef{}, uiSettingsIndex...), uiScoreSettingDefs...) {
		keys[def.Key] = true
	}
	selector := regexp.MustCompile(`#(?:input|v2-setting)-([a-zA-Z0-9_]+)`)
	tab := regexp.MustCompile(`tab=([a-zA-Z0-9_-]+)`)
	// `tab=` is also used by the Maintenance tabs and the Series Missing tab,
	// which are not settings tabs.
	maintenanceTabs := map[string]bool{"general": true, "archive-sql": true, "import": true, "gaps": true}
	var missing []string
	checked := 0
	for _, path := range tests {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, m := range selector.FindAllStringSubmatch(text, -1) {
			checked++
			if !keys[m[1]] {
				missing = append(missing, filepath.Base(path)+": setting "+m[1])
			}
		}
		for _, m := range tab.FindAllStringSubmatch(text, -1) {
			if maintenanceTabs[m[1]] {
				continue
			}
			checked++
			if uiSettingsCanonicalTab(m[1]) == "" {
				missing = append(missing, filepath.Base(path)+": tab "+m[1])
			}
		}
	}
	if len(missing) > 0 {
		t.Errorf("end2end tests reference settings or tabs that do not exist:\n  %s", strings.Join(missing, "\n  "))
	}
	if checked == 0 {
		t.Fatal("no setting or tab reference found in the end2end tests: the patterns no longer match")
	}
}
