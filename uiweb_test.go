package gextto

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

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
	if strings.Contains(html, `class="nav-group-label"`) {
		t.Fatal("navigation should not render visual group separators")
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
	if code != http.StatusOK || !strings.Contains(string(body), `data-dashboard-recent-list`) {
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

func TestUiDownloadsToolbarParity(t *testing.T) {
	state := newTestAppState(t)
	fake := newFakeQB()
	server := httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(server.Close)
	fake.setTorrents([]qbittorrent.Torrent{{
		Hash:     "abcd",
		Name:     "Example.Release.1080p",
		State:    "downloading",
		Progress: 0.25,
		Size:     4096,
	}})
	cfg := state.cfg
	cfg.Settings["torrent_backend"] = BackendQbittorrent
	cfg.Settings["qbittorrent_url"] = server.URL
	for key, value := range map[string]string{
		"libtorrent_temp_dl_limit":      "512",
		"libtorrent_temp_ul_limit":      "128",
		"libtorrent_temp_limit_enabled": "1",
		"libtorrent_temp_limit_until":   strconv.FormatInt(time.Now().Unix()+1800, 10),
		"download_tags":                 `["Comic","Serie"]`,
		"auto_remove_completed":         "yes",
	} {
		if err := SaveSetting(state.cfg.DataDir, key, value); err != nil {
			t.Fatalf("SaveSetting %s: %v", key, err)
		}
	}
	engine, err := newQbittorrentEngine(cfg)
	if err != nil {
		t.Fatalf("newQbittorrentEngine: %v", err)
	}
	state.setActiveEngine(engine)
	if err := state.db.SetTorrentTag("abcd", "Comic"); err != nil {
		t.Fatalf("SetTorrentTag: %v", err)
	}

	api := httptest.NewServer(Router(state))
	t.Cleanup(api.Close)
	code, _, body := webGet(t, api, "/ui?view=downloads")
	if code != http.StatusOK {
		t.Fatalf("downloads -> %d", code)
	}
	html := string(body)
	for _, marker := range []string{
		"Download session",
		"data-temp-dl",
		`value="512"`,
		`value="128"`,
		"data-download-tag-filter",
		"data-download-tag-select",
		"data-download-remove-tag",
		"data-auto-remove-completed",
		`data-sort="eta"`,
		"data-torrent-detail-panel",
		`data-torrent-subdetail="general"`,
		`data-torrent-subdetail="limits"`,
		`data-torrent-subdetail="storage"`,
		"tag-chip",
	} {
		if !strings.Contains(html, marker) {
			t.Fatalf("downloads page missing %q", marker)
		}
	}
	// The configured temporary limits are prefilled and the tag catalog is
	// offered in the toolbar.
	if !strings.Contains(html, "<option value=\"Comic\">Comic</option>") {
		t.Fatalf("tag catalog missing from toolbar")
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
		"dashboard": "Ultimi download",
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
	code, _, body := webGet(t, server, "/ui?view=downloads")
	if code != http.StatusOK || !strings.Contains(string(body), `data-torrent-add`) || !strings.Contains(string(body), `data-torrents-slot`) || !strings.Contains(string(body), `data-download-bulk`) || !strings.Contains(string(body), `data-torrent-subdetail`) {
		t.Fatalf("downloads missing add form or refresh slot")
	}
	code, _, body = webGet(t, server, "/ui/partial/torrents")
	if code != http.StatusOK || !strings.Contains(string(body), `data-torrents-slot`) {
		t.Fatalf("torrent partial missing refresh slot")
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
		{"/ui?view=settings&tab=i18n", `data-i18n-editor`},
		{"/ui?view=settings&tab=advanced", `data-list-editor`},
		{"/ui?view=settings&tab=sources", `data-sources-editor`},
		{"/ui?view=settings&tab=sources", `data-list-editor`},
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

// TestUiShellServesOwnStylesheet verifies the new UI ships its own stylesheet
// (the compiled classic theme) and no longer depends on the Leptos /pkg bundle.
func TestUiShellServesOwnStylesheet(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, _, body := webGet(t, server, "/")
	if code != http.StatusOK {
		t.Fatalf("GET / -> %d", code)
	}
	html := string(body)
	if !strings.Contains(html, "/ui/static/gextto-ui.css") {
		t.Fatal("shell does not load the new UI stylesheet")
	}
	if strings.Contains(html, "/pkg/ui.css") {
		t.Fatal("shell still depends on the legacy stylesheet")
	}
	// The classic shell structure must be present (top bar + main frame).
	for _, marker := range []string{`class="main-shell"`, `class="topbar"`, `class="top-actions"`, `class="sidebar"`, `class="brand"`, `data-metric="cpu"`, `data-theme-toggle`} {
		if !strings.Contains(html, marker) {
			t.Fatalf("shell missing %q", marker)
		}
	}
	cssCode, _, css := webGet(t, server, "/ui/static/gextto-ui.css")
	if cssCode != http.StatusOK || len(css) < 10_000 {
		t.Fatalf("GET /ui/static/gextto-ui.css -> %d (%d bytes)", cssCode, len(css))
	}
	for _, marker := range []string{".app-shell { flex-direction: row; }", "@media (max-width: 900px)", ".settings-tab-select-wrap"} {
		if !strings.Contains(string(css), marker) {
			t.Fatalf("stylesheet missing responsive marker %q", marker)
		}
	}
}

func TestUiReadActionsDoNotSendJSONBodies(t *testing.T) {
	code, err := fs.ReadFile(uiwebFS, "uiweb/static/gextto-ui.js")
	if err != nil {
		t.Fatalf("read embedded UI client: %v", err)
	}
	if !strings.Contains(string(code), `requestMethod === "GET" || requestMethod === "HEAD"`) {
		t.Fatal("UI client must omit bodies for GET/HEAD requests")
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

// TestUiDashboardParity checks that the dashboard exposes the same data and
// quick actions as the classic interface (metrics, global search, cycle
// actions, consumption, recent downloads).
func TestUiDashboardParity(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, _, body := webGet(t, server, "/ui?view=dashboard")
	if code != http.StatusOK {
		t.Fatalf("dashboard -> %d", code)
	}
	html := string(body)
	for _, marker := range []string{
		"Controllo libreria e download", "dashboard-explore-panel", "DRY-RUN",
		"Serie TV configurate", "Film configurati", "Magnet in archivio", "Spazio libero",
		"Visti nei feed", "Torrent in sessione", "Prossima ricerca automatica",
		"Consumo banda", "Azioni rapide", "Ultimo ciclo", "Sessione",
		`data-cycle="full"`, `data-cycle="series"`, `data-cycle="movies"`, `data-cycle="comics"`,
		`data-ui-search-post`, `data-endpoint="/api/search"`, `data-add="/api/search/add"`,
		`data-api="/api/backup"`,
	} {
		if !strings.Contains(html, marker) {
			t.Fatalf("dashboard missing %q", marker)
		}
	}
}

// TestUiSettingsTabsAndSearch verifies the redesigned settings page: one tab
// rendered at a time, chip navigation, a searchable index and the special
// editors reachable through their own tabs.
func TestUiSettingsTabsAndSearch(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, _, body := webGet(t, server, "/ui?view=settings")
	html := string(body)
	if code != http.StatusOK {
		t.Fatalf("settings -> %d", code)
	}
	for _, marker := range []string{`data-settings-index=`, `data-settings-search`, `data-settings-tab-select`, `class="chip`, `data-setting-key="`, `data-setting-status`, `settings-grid`} {
		if !strings.Contains(html, marker) {
			t.Fatalf("settings page missing %q", marker)
		}
	}
	// Each tab query selects exactly one section.
	for path, marker := range map[string]string{
		"/ui?view=settings&tab=i18n":     "data-i18n-editor",
		"/ui?view=settings&tab=advanced": "data-list-editor",
		"/ui?view=settings&tab=sources":  "data-sources-editor",
	} {
		code, _, body := webGet(t, server, path)
		if code != http.StatusOK || !strings.Contains(string(body), marker) {
			t.Fatalf("GET %s -> %d, missing %q", path, code, marker)
		}
	}
	// An unknown tab falls back to the first available tab, never a blank page.
	code, _, body = webGet(t, server, "/ui?view=settings&tab=does-not-exist")
	if code != http.StatusOK || !strings.Contains(string(body), "settings-grid") {
		t.Fatalf("unknown tab should fall back, got %d", code)
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

// TestUiListEditorShape pins the load/save shape of each structured list editor
// (the GET response and the POST body differ, and a mismatch would make "Salva"
// fail with HTTP 400).
func TestUiListEditorShape(t *testing.T) {
	byTitle := map[string]uiListEditor{}
	for _, editor := range append([]uiListEditor{uiIndexerEditor}, uiAdvancedEditors...) {
		byTitle[editor.Title] = editor
	}
	checks := map[string]struct {
		get, unwrap, wrap, postKey string
	}{
		"Indexer Torznab":       {"/api/config", "indexers", "", "indexers"},
		"Filtri per sorgente":   {"/api/config/source-filters", "filters", "filters", ""},
		"Regole tag → cartella": {"/api/tag-dir-rules", "items", "", ""},
		"Event hook":            {"/api/event-hooks", "items", "", ""},
		"Cartelle osservate":    {"/api/watched-folders", "items", "", ""},
	}
	for title, want := range checks {
		editor, ok := byTitle[title]
		if !ok {
			t.Errorf("missing list editor %q", title)
			continue
		}
		if editor.GetPath != want.get || editor.Unwrap != want.unwrap || editor.Wrap != want.wrap || editor.PostKey != want.postKey {
			t.Errorf("%s: get/unwrap/wrap/postKey = %q/%q/%q/%q, want %q/%q/%q/%q",
				title, editor.GetPath, editor.Unwrap, editor.Wrap, editor.PostKey,
				want.get, want.unwrap, want.wrap, want.postKey)
		}
		if len(editor.Fields) == 0 {
			t.Errorf("%s: no fields", title)
		}
	}
}

// TestUiBoolValuesPreserveSpelling guards against the settings select rewriting
// "yes" to "true" (or "1"/"on"), which would break strict readers such as the
// comics weekly check.
func TestUiBoolValuesPreserveSpelling(t *testing.T) {
	cases := []struct {
		value      string
		want       bool
		trueValue  string
		falseValue string
	}{
		{"yes", true, "yes", "no"},
		{"no", false, "yes", "no"},
		{"YES", true, "yes", "no"},
		{"true", true, "true", "false"},
		{"1", true, "1", "0"},
		{"0", false, "1", "0"},
		{"on", true, "on", "off"},
	}
	for _, tc := range cases {
		got, trueValue, falseValue := uiBoolValues(tc.value)
		if got != tc.want || trueValue != tc.trueValue || falseValue != tc.falseValue {
			t.Errorf("uiBoolValues(%q) = %v,%q,%q want %v,%q,%q",
				tc.value, got, trueValue, falseValue, tc.want, tc.trueValue, tc.falseValue)
		}
	}
	field := uiSettingFieldFor("gap_filling", "Gap filling", "yes")
	if field.Kind != "bool" || !field.BoolValue || field.TrueValue != "yes" || field.FalseValue != "no" {
		t.Fatalf("unexpected bool field: %+v", field)
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
	addPanels := func(sections []uiPageSection) {
		for _, section := range sections {
			if section.Kind == "table" && section.Table.Endpoint != "" {
				add("GET", section.Table.Endpoint)
				for _, action := range decodeActions(t, section.Table.ActionsJSON) {
					add(action.Method, action.Path)
				}
			}
			if section.Kind == "form" && section.Form.Path != "" {
				add(section.Form.Method, section.Form.Path)
			}
			for _, button := range section.Action.Buttons {
				if button.Path != "" {
					add(button.Method, button.Path)
				}
			}
		}
	}
	state := newTestAppState(t)

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
	// Panels pages: library, archive, comics, maintenance, integrations and the
	// download page. Every section table/action/form must map to a real route.
	for _, view := range []string{"series", "movies", "gaps", "archive", "blocklist", "comics", "maintenance", "integrations"} {
		page, ok := uiPanelsPageFor(view, state)
		if !ok {
			t.Fatalf("missing panels page %s", view)
		}
		addPanels(page.Sections)
	}
	addPanels(uiDownloadsPageFor(state).Panels)
	// Search page.
	search, _ := uiSearchPageFor("search")
	add("POST", search.Endpoint)
	add("POST", search.AddPath)
	// Settings, library and generic JSON editors.
	add("GET", "/api/config")
	add("GET", "/api/config/library")
	add("POST", "/api/config/library")
	add("POST", "/api/config/settings")
	// Dashboard quick actions and global search.
	add("POST", "/api/run_now")
	add("POST", "/api/backup")
	add("POST", "/api/search")
	add("POST", "/api/search/add")
	for _, editor := range append([]uiListEditor{uiIndexerEditor}, uiAdvancedEditors...) {
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
