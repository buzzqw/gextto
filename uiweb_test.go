package gextto

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/qbittorrent"
)

func TestUiShellRendersNavigationParity(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, _, body := webGet(t, server, "/ui")
	if code != http.StatusOK {
		t.Fatalf("GET /ui -> %d", code)
	}
	html := string(body)
	if !strings.Contains(html, `data-view="dashboard"`) {
		t.Fatalf("shell missing page marker")
	}
	// Every page of the legacy navigation must be reachable from the new shell.
	for _, label := range []string{"Dashboard", "Scarico", "Serie TV", "Film", "Mancanti", "Esplora", "Archivio", "Fumetti", "Configurazione", "Integrazioni", "Manutenzione", "Salute", "Log", "Blocklist", "Manuale", "Licenza"} {
		if !strings.Contains(html, ">"+label+"<") {
			t.Fatalf("navigation missing %q", label)
		}
	}
	if !strings.Contains(html, "/ui/static/gextto-ui.js") {
		t.Fatal("shell does not load the new UI script")
	}
}

func TestUiPartialDashboardRenders(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	code, _, body := webGet(t, server, "/ui/partial/dashboard")
	if code != http.StatusOK || !strings.Contains(string(body), "Stato daemon") {
		t.Fatalf("dashboard partial -> %d: %s", code, body)
	}
}

func TestUiPartialTorrentsEscapesTorrentName(t *testing.T) {
	state := newTestAppState(t)
	fake := newFakeQB()
	server := httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(server.Close)
	fake.setTorrents([]qbittorrent.Torrent{{
		Hash:     "abc",
		Name:     `<script>alert(1)</script>`,
		State:    "downloading",
		Progress: 0.5,
		Size:     1000,
	}})
	cfg := state.cfg
	cfg.Settings["torrent_backend"] = BackendQbittorrent
	cfg.Settings["qbittorrent_url"] = server.URL
	engine, err := newQbittorrentEngine(cfg)
	if err != nil {
		t.Fatalf("newQbittorrentEngine: %v", err)
	}
	state.setActiveEngine(engine)

	api := httptest.NewServer(Router(state))
	t.Cleanup(api.Close)
	code, _, body := webGet(t, api, "/ui/partial/torrents")
	if code != http.StatusOK {
		t.Fatalf("torrents partial -> %d", code)
	}
	html := string(body)
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Fatalf("torrent name was not escaped: %s", html)
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Fatalf("escaped torrent name missing: %s", html)
	}
	if !strings.Contains(html, "In scarico") {
		t.Fatalf("state label missing: %s", html)
	}
}

func TestUiPagesArePublicOnLan(t *testing.T) {
	state := newTestAppState(t)
	// A token may be configured, but the new UI is meant for a trusted LAN and
	// does not gate its pages or partials.
	if err := SaveSetting(state.cfg.DataDir, "api_token", "lan-token"); err != nil {
		t.Fatalf("save token: %v", err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	for _, path := range []string{"/ui", "/ui/partial/dashboard", "/ui/partial/torrents"} {
		if code, _, _ := webGet(t, server, path); code != http.StatusOK {
			t.Fatalf("GET %s -> %d, want 200", path, code)
		}
	}
}

func TestUiServerRenderedPages(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	cases := map[string]string{
		"dashboard": "Stato daemon",
		"downloads": "Scarico",
		"health":    "Percorsi",
		"logs":      "Log",
		"manual":    "Manuale",
		"license":   "Licenza",
	}
	for view, marker := range cases {
		code, _, body := webGet(t, server, "/ui?view="+view)
		if code != http.StatusOK || !strings.Contains(string(body), marker) {
			t.Fatalf("GET /ui?view=%s -> %d, missing %q", view, code, marker)
		}
	}
}

func TestUiListAndActionPages(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	cases := map[string]string{
		"series":       `data-endpoint="/api/series"`,
		"movies":       `data-endpoint="/api/movies"`,
		"gaps":         `data-endpoint="/api/gaps"`,
		"archive":      `data-endpoint="/api/archive"`,
		"blocklist":    `/api/blocklist/{hash}/remove`,
		"maintenance":  `/api/database/rescore`,
		"integrations": `/api/jellyfin/test`,
		"settings":     `data-setting-key="`,
		"search":       `data-ui-search-post`,
		"comics":       `data-endpoint="/api/comics"`,
	}
	for view, marker := range cases {
		code, _, body := webGet(t, server, "/ui?view="+view)
		if code != http.StatusOK || !strings.Contains(string(body), marker) {
			t.Fatalf("GET /ui?view=%s -> %d, missing %q", view, code, marker)
		}
	}
	// Every navigation page is now migrated: no page should fall back.
	for _, item := range uiNavGroups {
		for _, entry := range item.Items {
			code, _, body := webGet(t, server, "/ui?view="+entry.ID)
			if code != http.StatusOK {
				t.Fatalf("GET /ui?view=%s -> %d", entry.ID, code)
			}
			if strings.Contains(string(body), "Apri la UI classica") {
				t.Fatalf("page %s should be migrated, not a fallback", entry.ID)
			}
		}
	}
}

func TestUiDetailAndEditorPages(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	for _, name := range []string{"series_detail", "movie_detail"} {
		if uiwebTemplates.Lookup(name) == nil {
			t.Fatalf("template %s not registered", name)
		}
	}
	checks := []struct {
		path   string
		marker string
	}{
		{"/ui?view=settings", `data-i18n-editor`},
		{"/ui?view=settings", `data-json-editor`},
		{"/ui?view=series", `series_link`},
		{"/ui?view=movies", `movie_link`},
		{"/ui?view=comics", `/api/comics/{id}/enabled`},
		{"/ui?view=comics", `data-comics-download`},
		{"/ui?view=comics", `/api/comics/downloads/{id}/pause`},
	}
	for _, check := range checks {
		code, _, body := webGet(t, server, check.path)
		if code != http.StatusOK || !strings.Contains(string(body), check.marker) {
			t.Fatalf("GET %s -> %d, missing %q", check.path, code, check.marker)
		}
	}
}

func TestUiStateLabelParity(t *testing.T) {
	cases := map[string]string{
		"downloading":          "In scarico",
		"downloading_metadata": "Metadata",
		"stalled":              "In attesa di seed",
		"seeding":              "In seed",
		"finished":             "Completato",
		"checking_files":       "Verifica file",
		"paused":               "In pausa",
		"moving":               "Spostamento",
	}
	for state, want := range cases {
		if got := uiStateLabel(state); got != want {
			t.Fatalf("uiStateLabel(%q) = %q, want %q", state, got, want)
		}
	}
}

// TestUiMagnetURLScheme guards the archive match links: html/template rewrites
// unknown schemes to "#ZgotmplZ", so the magnet must travel as a template.URL
// while javascript: stays rejected.
func TestUiMagnetURLScheme(t *testing.T) {
	if uiMagnetURL("magnet:?xt=urn:btih:abc") == "" {
		t.Fatal("magnet URL rejected")
	}
	if uiMagnetURL("https://example.org/a") == "" {
		t.Fatal("https URL rejected")
	}
	if uiMagnetURL("javascript:alert(1)") != "" {
		t.Fatal("javascript URL accepted")
	}
	var buffer bytes.Buffer
	if err := uiwebTemplates.ExecuteTemplate(&buffer, "movie_detail", uiMovieDetail{
		ID: 1, Name: "X",
		Matches: []uiMovieMatch{{Title: "t", Magnet: uiMagnetURL("magnet:?xt=urn:btih:abc"), Source: "s"}},
	}); err != nil {
		t.Fatalf("render movie_detail: %v", err)
	}
	body := buffer.String()
	if !strings.Contains(body, "magnet:?xt=urn:btih:abc") || strings.Contains(body, "ZgotmplZ") {
		t.Fatalf("magnet link not rendered correctly")
	}
}

// TestUiMovieFormKeepsMetadata guards against the destructive save: UpdateMovie
// overwrites tmdb_id/tvdb_id and the requirement fields with the request body,
// so the edit form must send them back.
func TestUiMovieFormKeepsMetadata(t *testing.T) {
	var buffer bytes.Buffer
	if err := uiwebTemplates.ExecuteTemplate(&buffer, "movie_detail", uiMovieDetail{
		ID: 7, Name: "Film", TmdbID: "123", TvdbID: "456",
		Subtitle: "sub", Exclude: "ex",
		LanguageRequirements: "lang", SubtitleRequirements: "sreq",
	}); err != nil {
		t.Fatalf("render movie_detail: %v", err)
	}
	body := buffer.String()
	for _, marker := range []string{
		`name="tmdb_id" value="123"`,
		`name="tvdb_id" value="456"`,
		`name="language_requirements" value="lang"`,
		`name="subtitle_requirements" value="sreq"`,
		`name="subtitle" value="sub"`,
		`name="exclude" value="ex"`,
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("movie_detail missing %q", marker)
		}
	}
}

// TestUiSettingsHidesEmptyTab ensures the "Punteggi" entry (whose keys are
// dynamic and live under "Altro") does not render as an empty section.
func TestUiSettingsHidesEmptyTab(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	code, _, body := webGet(t, server, "/ui?view=settings")
	if code != http.StatusOK {
		t.Fatalf("settings -> %d", code)
	}
	if strings.Contains(string(body), ">Punteggi<") {
		t.Fatal("empty Punteggi tab should be hidden")
	}
}

// TestUiSettingKindMasksStructuredSecrets ensures a structured value that
// embeds credentials (the `indexers` JSON) is never rendered in clear.
func TestUiSettingKindMasksStructuredSecrets(t *testing.T) {
	cases := map[string]struct {
		key, value string
		want       string
	}{
		"indexers":  {"indexers", `[{"name":"x","api_key":"abc"}]`, "secret"},
		"password":  {"qbittorrent_password", "hunter2", "secret"},
		"feeds":     {"url", `["https://feed.example/rss"]`, "text"},
		"multiline": {"source_filters", "a\nb", "area"},
		"bool":      {"active", "true", "bool"},
	}
	for name, tc := range cases {
		if got := uiSettingKind(tc.key, tc.value); got != tc.want {
			t.Errorf("%s: uiSettingKind(%q) = %q, want %q", name, tc.key, got, tc.want)
		}
	}
}

// TestUiJSONEditorRoundTripShape pins the unwrap/wrap of each structured editor:
// the GET response and the POST body have different shapes, and a mismatch would
// make "Salva" fail with HTTP 400.
func TestUiJSONEditorRoundTripShape(t *testing.T) {
	want := map[string]struct{ unwrap, wrap string }{
		"/api/config/source-filters": {"filters", "filters"},
		"/api/tag-dir-rules":         {"items", ""},
		"/api/event-hooks":           {"items", ""},
		"/api/watched-folders":       {"items", ""},
	}
	if len(uiJSONEditors) != len(want) {
		t.Fatalf("got %d editors, want %d", len(uiJSONEditors), len(want))
	}
	for _, editor := range uiJSONEditors {
		expected, ok := want[editor.GetPath]
		if !ok {
			t.Errorf("unexpected editor %q", editor.GetPath)
			continue
		}
		if editor.Unwrap != expected.unwrap || editor.Wrap != expected.wrap {
			t.Errorf("%s: unwrap/wrap = %q/%q, want %q/%q",
				editor.GetPath, editor.Unwrap, editor.Wrap, expected.unwrap, expected.wrap)
		}
	}
}

// TestUiActionPathsAreRegistered is the formal check that every endpoint the new
// UI links or posts to is actually served by the router. A typo or a removed
// route would otherwise only surface as a broken button at runtime.
func TestUiActionPathsAreRegistered(t *testing.T) {
	param := regexp.MustCompile(`\{[^}]+\}`)
	normalize := func(value string) string { return param.ReplaceAllString(value, "*") }
	routes := map[string]bool{}
	for _, pattern := range RegisteredRoutes() {
		routes[normalize(pattern)] = true
	}
	var checks []string
	add := func(method, path string) {
		checks = append(checks, method+" "+normalize(path))
	}

	// Table pages and their row actions.
	for _, view := range []string{"series", "movies", "gaps", "archive", "blocklist", "comics"} {
		spec, ok := uiTableSpecFor(view)
		if !ok {
			t.Fatalf("missing table spec for %s", view)
		}
		add("GET", spec.Endpoint)
		for _, action := range decodeActions(t, spec.ActionsJSON) {
			add(action.Method, action.Path)
		}
		for _, action := range decodeActions(t, spec.DownloadsActionsJSON) {
			add(action.Method, action.Path)
		}
	}
	// Action pages.
	for _, view := range []string{"maintenance"} {
		page, ok := uiActionsPageFor(view)
		if !ok {
			t.Fatalf("missing actions page %s", view)
		}
		for _, section := range page.Sections {
			for _, button := range section.Buttons {
				add(button.Method, button.Path)
			}
		}
	}
	// Integrations (OAuth + media servers).
	for _, button := range uiActionsPageButtons(uiIntegrationsPageFrom(newTestAppState(t)).Sections) {
		add(button.Method, button.Path)
	}
	// Search page.
	search, _ := uiSearchPageFor("search")
	add("POST", search.Endpoint)
	add("POST", search.AddPath)
	// Settings, library and generic JSON editors.
	add("GET", "/api/config")
	add("GET", "/api/config/library")
	add("POST", "/api/config/library")
	add("POST", "/api/config/settings")
	for _, editor := range uiJSONEditors {
		add("GET", editor.GetPath)
		add("POST", editor.PostPath)
	}
	// i18n, comics manual download, torrents and dashboard, OAuth, detail pages:
	// hardcoded here because they live in the client script or in handlers.
	for _, entry := range []string{
		"GET /api/i18n", "POST /api/i18n", "POST /api/i18n/language",
		"GET /api/i18n/active", "GET /api/i18n/export/{lang}", "POST /api/i18n/import/{lang}", "DELETE /api/i18n/{lang}",
		"POST /api/comics/links", "POST /api/comics/download",
		"GET /api/comics/downloads",
		"POST /api/run_now",
		"POST /api/torrents/{hash}/pause", "POST /api/torrents/{hash}/resume",
		"POST /api/torrents/{hash}/recheck", "POST /api/torrents/{hash}/remove",
		"POST /api/trakt/auth/start", "POST /api/trakt/auth/poll",
		"POST /api/trakt/auth/refresh", "POST /api/trakt/auth/revoke",
		"POST /api/simkl/auth/start", "POST /api/simkl/auth/poll", "POST /api/simkl/auth/revoke",
		"POST /api/episodes/{series}/{season}/{episode}/ignore",
		"POST /api/episodes/{series}/{season}/{episode}/force",
		"POST /api/episodes/{series}/{season}/{episode}/redownload",
		"POST /api/episodes/{series}/{season}/{episode}/search",
		"DELETE /api/episodes/{series}/{season}/{episode}",
		"POST /api/movies/{id}", "DELETE /api/movies/{id}",
		"POST /api/movies/{id}/search", "POST /api/movies/{id}/redownload", "POST /api/movies/{id}/metadata",
	} {
		add(entry[:strings.IndexByte(entry, ' ')], entry[strings.IndexByte(entry, ' ')+1:])
	}

	for _, check := range checks {
		if !routes[check] {
			t.Errorf("new UI references an unregistered route: %s", check)
		}
	}
}

func uiActionsPageButtons(sections []uiActionSection) []uiActionButton {
	var buttons []uiActionButton
	for _, section := range sections {
		buttons = append(buttons, section.Buttons...)
	}
	return buttons
}

func decodeActions(t *testing.T, raw string) []uiAction {
	t.Helper()
	if raw == "" {
		return nil
	}
	var actions []uiAction
	if err := json.Unmarshal([]byte(raw), &actions); err != nil {
		t.Fatalf("invalid action JSON %q: %v", raw, err)
	}
	return actions
}
