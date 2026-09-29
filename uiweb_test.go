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
	code, _, body = webGet(t, server, "/ui?view=health")
	if code != http.StatusOK || !strings.Contains(string(body), `data-auto-load="false"`) || !strings.Contains(string(body), "Premi Aggiorna per verificare le sorgenti.") || !strings.Contains(string(body), "Esegue ora il controllo delle sorgenti configurate") || !strings.Contains(string(body), "Inserisci una query (per esempio ita 1080p)") {
		t.Fatalf("health sources table should be manual-only")
	}
	code, _, body = webGet(t, server, "/ui/partial/torrents")
	if code != http.StatusOK || !strings.Contains(string(body), `data-torrents-slot`) {
		t.Fatalf("torrent partial missing refresh slot")
	}
}

// TestUiMaintenanceParity pins the rextto-style maintenance layout: grouped
// actions, duplicates, a real RAM disk control, trash and backup, without a
// duplicate source check or the useless ports panel.
// TestUiIntegrationsParity pins the rextto-style integrations page: Trakt and
// Simkl panels plus the Jellyfin/Plex and indexer configuration forms.
func TestUiIntegrationsParity(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, _, body := webGet(t, server, "/ui?view=integrations")
	if code != http.StatusOK {
		t.Fatalf("integrations -> %d", code)
	}
	html := string(body)
	for _, marker := range []string{
		"Trakt", "Simkl", "Jellyfin", "Plex", "FlareSolverr", "Indexer Torznab",
		`data-setting-key="jellyfin_url"`, `data-setting-key="jellyfin_api_key"`,
		`data-setting-key="plex_url"`, `data-setting-key="plex_token"`,
		`data-setting-key="flaresolverr_url"`, `data-list-editor`,
		`data-api="/api/jellyfin/refresh"`, `data-api="/api/plex/refresh"`,
	} {
		if !strings.Contains(html, marker) {
			t.Fatalf("integrations page missing %q", marker)
		}
	}
}

func TestUiMaintenanceParity(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, _, body := webGet(t, server, "/ui?view=maintenance")
	if code != http.StatusOK {
		t.Fatalf("maintenance -> %d", code)
	}
	html := string(body)
	for _, marker := range []string{
		"data-duplicates", "data-duplicates-preview", "data-duplicates-clean",
		"data-ramdisk", "data-ramdisk-paths", "data-db-optimize", "data-trash-open",
		`data-endpoint="/api/db/prune"`, `data-api="/api/backup"`,
		"Cartella cloud", "FTP password", "Percorso locale di una cartella già montata", "Test FTP", "Verifica connessione, login, cartella remota", `&#34;key&#34;:&#34;path&#34;,&#34;label&#34;:&#34;Nome&#34;`,
	} {
		if !strings.Contains(html, marker) {
			t.Fatalf("maintenance page missing %q", marker)
		}
	}
	if strings.Contains(html, "Diagnostica sorgenti") || strings.Contains(html, "data-sources-probe") {
		t.Fatalf("maintenance page should not duplicate the source check")
	}
	scanIndex := strings.Index(html, `data-folder-rename-scan`)
	acceptIndex := strings.Index(html, `data-folder-rename-accept-all`)
	applyIndex := strings.Index(html, `data-folder-rename-apply`)
	if scanIndex < 0 || acceptIndex < 0 || applyIndex < 0 || !(scanIndex < acceptIndex && acceptIndex < applyIndex) {
		t.Fatalf("folder rename actions should be ordered scan, accept all, apply")
	}
	for _, forbidden := range []string{">Porte<", "/api/config/check-ports"} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("maintenance page should not contain %q", forbidden)
		}
	}
}

func TestUiLogsAndFeedParity(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, _, body := webGet(t, server, "/ui?view=logs")
	if code != http.StatusOK {
		t.Fatalf("logs -> %d", code)
	}
	for _, marker := range []string{"data-logs-view", "data-logs-filter", "data-logs-lines", "data-logs-follow"} {
		if !strings.Contains(string(body), marker) {
			t.Fatalf("logs page missing %q", marker)
		}
	}

	code, _, body = webGet(t, server, "/ui?view=dashboard")
	if code != http.StatusOK {
		t.Fatalf("dashboard -> %d", code)
	}
	for _, marker := range []string{"data-dashboard-feed", "data-dashboard-feed-body", "Ultimi trovati nelle sorgenti", "data-release-filter", "data-release-status"} {
		if !strings.Contains(string(body), marker) {
			t.Fatalf("dashboard missing %q", marker)
		}
	}

	code, _, body = webGet(t, server, "/ui?view=search")
	if code != http.StatusOK {
		t.Fatalf("search -> %d", code)
	}
	for _, marker := range []string{"data-discover", "data-discover-results", "data-discover-calendar", `data-discover-kind="series"`, `data-discover-mode="popular"`, "data-release-filter"} {
		if !strings.Contains(string(body), marker) {
			t.Fatalf("explore page missing %q", marker)
		}
	}
}

// TestUiHistoryFolderColumn pins the "Cartella libreria / NAS" rendering: the
// full absolute path is only a tooltip, the visible cell is the destination
// folder.
func TestUiHistoryFolderColumn(t *testing.T) {
	state := newTestAppState(t)
	page := uiDownloadsPageFor(state)
	found := false
	for _, section := range page.Panels {
		if section.Kind != "table" || section.Table.Title != "Storico download" {
			continue
		}
		if !strings.Contains(section.Table.ColumnsJSON, `"format":"folder"`) {
			t.Fatalf("history columns missing folder format: %s", section.Table.ColumnsJSON)
		}
		if !strings.Contains(section.Table.ColumnsJSON, `"key":"source"`) {
			t.Fatalf("history columns missing source: %s", section.Table.ColumnsJSON)
		}
		if !strings.Contains(section.Table.ColumnsJSON, `"key":"completed_at","label":"Concluso","format":"datetime"`) {
			t.Fatalf("history columns missing compact datetime format: %s", section.Table.ColumnsJSON)
		}
		if section.Table.PageSize != 10 {
			t.Fatalf("history table page size = %d, want 10", section.Table.PageSize)
		}
		found = true
	}
	if !found {
		t.Fatal("history download table not found")
	}
}

func TestTorrentHistoryDisplayNameRemovesProviderSuffix(t *testing.T) {
	cases := []struct {
		name, source, want string
	}{
		{name: "Movie.2026.1080p-ExtTo", source: "ExtTo", want: "Movie.2026.1080p"},
		{name: "Show.S01E01-Knaben", source: "prowlarr:Knaben", want: "Show.S01E01"},
		{name: "CIA.S01E10-12.1080p.WEB-DL.ITA.ENG.AAC2.0.H.265-G66 [ExtTo]", source: "ExtTo", want: "CIA.S01E10-12.1080p.WEB-DL.ITA.ENG.AAC2.0.H.265-G66"},
		{name: "The Knaben", source: "Knaben", want: "The Knaben"},
	}
	for _, testCase := range cases {
		if got := gh4_historyDisplayName(testCase.name, testCase.source); got != testCase.want {
			t.Errorf("gh4_historyDisplayName(%q, %q) = %q, want %q", testCase.name, testCase.source, got, testCase.want)
		}
	}
}

func TestUiListAndActionPages(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	cases := map[string]string{
		"series":       `data-endpoint="/api/config/library"`,
		"movies":       `data-endpoint="/api/config/library"`,
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
		// The Indexer Torznab editor lives only in Integrazioni (no duplicate).
		{"/ui?view=integrations", `data-list-editor`},
		// The manager type picker is a dropdown, not free text.
		{"/ui?view=integrations", `data-kind="select"`},
		// Every indexer row offers a "Testa" action.
		{"/ui?view=integrations", `data-list-test`},
		{"/ui?view=series", `series_link`},
		{"/ui?view=movies", `movie_link`},
		{"/ui?view=comics", `/api/comics/{id}/enabled`},
		{"/ui?view=comics", `data-comics-download`},
		{"/ui?view=downloads", "HTTP fumetti nella stessa lista di lavoro"},
		{"/ui?view=logs", `data-logs-lines`},
	}
	for _, check := range checks {
		code, _, body := webGet(t, server, check.path)
		if code != http.StatusOK || !strings.Contains(string(body), check.marker) {
			t.Fatalf("GET %s -> %d, missing %q", check.path, code, check.marker)
		}
	}
	code, _, settingsBody := webGet(t, server, "/ui?view=settings&tab=libtorrent")
	if code != http.StatusOK {
		t.Fatalf("GET settings -> %d", code)
	}
	if strings.Contains(string(settingsBody), "chip-count") {
		t.Fatal("settings tab count badges should not be rendered")
	}
	if strings.Contains(string(settingsBody), `data-nav="archive" title="Archivio" data-nav-count`) {
		t.Fatal("archive count badge should not be rendered")
	}
}

// TestUiShellServesOwnStylesheet verifies the new UI ships its own stylesheet
// (the compiled server-rendered theme) and no longer depends on a frontend
// bundle under /pkg.
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
	for _, marker := range []string{`class="main-shell"`, `class="topbar"`, `class="top-actions"`, `class="sidebar"`, `class="brand"`, `Gextto · EXpert Torrent Transfer Orchestrator`, `data-metric="cpu"`, `data-theme-toggle`} {
		if !strings.Contains(html, marker) {
			t.Fatalf("shell missing %q", marker)
		}
	}
	cssCode, _, css := webGet(t, server, "/ui/static/gextto-ui.css")
	if cssCode != http.StatusOK || len(css) < 10_000 {
		t.Fatalf("GET /ui/static/gextto-ui.css -> %d (%d bytes)", cssCode, len(css))
	}
	for _, marker := range []string{
		".app-shell { flex-direction: row; }",
		"@media (max-width: 900px)",
		".settings-tab-select-wrap",
		// Mobile usability markers: snap navigation, touch targets, two-column
		// dashboard, safe-area aware dialogs and contained table scrolling.
		"scroll-snap-type: x proximity",
		"overscroll-behavior-x: contain",
		"env(safe-area-inset-bottom)",
		"@media (max-width: 380px)",
	} {
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
	for _, marker := range []string{`data-settings-index=`, `data-settings-search`, `data-settings-tab-select`, `class="chip`, `data-setting-key="`, `data-setting-status`, `settings-rows`, `data-settings-savebar`} {
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
	code, _, body = webGet(t, server, "/ui?view=settings&tab=libtorrent")
	if code != http.StatusOK || !strings.Contains(string(body), `value="0.0.0.0:6881-6891"`) || !strings.Contains(string(body), "Il valore proposto va bene nella maggior parte dei casi") {
		t.Fatalf("libtorrent settings should propose listen interfaces")
	}
	// An unknown tab falls back to the first available tab, never a blank page.
	code, _, body = webGet(t, server, "/ui?view=settings&tab=does-not-exist")
	if code != http.StatusOK || !strings.Contains(string(body), "settings-rows") {
		t.Fatalf("unknown tab should fall back, got %d", code)
	}
}

func TestUiSettingDefaultsMatchSafeRuntimeDefaults(t *testing.T) {
	want := map[string]string{
		"active":                       "false",
		"libtorrent_proxy_port":        "0",
		"libtorrent_listen_interfaces": "0.0.0.0:6881-6891",
		"qbittorrent_poll_interval_ms": "1500",
		"trash_retention_days":         "0",
	}
	for key, expected := range want {
		if got := uiSettingDefaults[key]; got != expected {
			t.Errorf("ui default %s = %q, want %q", key, got, expected)
		}
	}
	if got, want := uiSettingDefaults["blacklist"], strings.Join(defaultBlacklist(), "\n"); got != want {
		t.Errorf("ui default blacklist = %q, want %q", got, want)
	}
}

func TestAnacrolixSettingsHaveExplanatoryTooltips(t *testing.T) {
	for _, key := range []string{
		"anacrolix_path_mappings", "anacrolix_data_dir", "anacrolix_listen_port",
		"anacrolix_tcp", "anacrolix_utp", "anacrolix_dht", "anacrolix_pex",
		"anacrolix_trackers", "anacrolix_upnp", "anacrolix_dht_bootstrap_nodes",
		"anacrolix_max_conns_per_torrent", "anacrolix_download_limit_kib",
		"anacrolix_upload_limit_kib", "anacrolix_piece_hashers",
		"anacrolix_max_unverified_mb", "anacrolix_ipfilter_path",
		"anacrolix_apply_ip_filter", "anacrolix_proxy_type", "anacrolix_proxy_host",
		"anacrolix_proxy_port", "anacrolix_proxy_user", "anacrolix_proxy_password",
	} {
		if strings.TrimSpace(uiSettingTooltip(key)) == "" {
			t.Errorf("missing tooltip for %s", key)
		}
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

func TestUiCleanupActionIsSelect(t *testing.T) {
	move := uiSettingFieldFor("cleanup_action", "Azione cleanup", "move")
	if move.Kind != "select" || len(move.Options) != 2 || !move.Options[0].Selected || move.Options[1].Selected {
		t.Fatalf("unexpected move cleanup field: %+v", move)
	}
	delete := uiSettingFieldFor("cleanup_action", "Azione cleanup", "delete")
	if delete.Kind != "select" || len(delete.Options) != 2 || delete.Options[0].Selected || !delete.Options[1].Selected {
		t.Fatalf("unexpected delete cleanup field: %+v", delete)
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
					if action.Kind != "" || action.Path == "" {
						continue
					}
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
			if action.Kind != "" || action.Path == "" {
				continue
			}
			add(action.Method, action.Path)
		}
		for _, action := range decodeActions(t, spec.DownloadsActionsJSON) {
			add(action.Method, action.Path)
		}
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
		"POST /api/maintenance/clean-duplicates", "POST /api/db/action",
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

// TestUiSourceLabel keeps long feed/indexer URLs compact in the Archivio table
// while the raw value stays available in the tooltip.
func TestUiSourceLabel(t *testing.T) {
	cases := map[string]string{
		"https://rss24h.torrentleech.org/a4ae63ba": "torrentleech",
		"https://www.example.com:8080/torznab":     "example",
		"http://sub.tracker.co.uk/feed":            "tracker",
		"jackett":                                  "jackett",
		"":                                         "",
	}
	for raw, want := range cases {
		if got := uiSourceLabel(raw); got != want {
			t.Fatalf("uiSourceLabel(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestTmdbItemInLibrary proves the "Già in lista" badge matches by external id
// and by name, so the discovery wall never offers a duplicate insert.
func TestTmdbItemInLibrary(t *testing.T) {
	name := "Existing Show"
	cfg := &Config{
		Series: []SeriesConfig{{Name: "Existing Show", TmdbID: "111"}},
		Movies: []MovieConfig{{Name: "Existing Movie", Year: "2024", TmdbID: "222"}},
	}
	if !gh_tmdbItemInLibrary(cfg, "series", TmdbItem{ID: 111}) {
		t.Fatal("series id match not detected")
	}
	if !gh_tmdbItemInLibrary(cfg, "series", TmdbItem{Name: &name, ID: 999}) {
		t.Fatal("name match not detected")
	}
	if gh_tmdbItemInLibrary(cfg, "series", TmdbItem{Name: stringPtr("Brand New"), ID: 999}) {
		t.Fatal("unknown series must not be in library")
	}
	if !gh_tmdbItemInLibrary(cfg, "movie", TmdbItem{ID: 222}) {
		t.Fatal("movie id match not detected")
	}
	if gh_tmdbItemInLibrary(cfg, "movie", TmdbItem{ID: 0, Title: stringPtr("Brand New Movie")}) {
		t.Fatal("unknown movie must not be in library")
	}
}

// TestUiArchiveAndComicsActions checks the Archivio explain action and that the
// comic cycle button lives only in the dashboard, not on the Fumetti page.
func TestUiArchiveAndComicsActions(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, _, archive := webGet(t, server, "/ui?view=archive")
	if code != http.StatusOK {
		t.Fatalf("GET archive -> %d", code)
	}
	if !strings.Contains(string(archive), "Perché non questo?") {
		t.Fatal("archive table is missing the explain action")
	}
	if !strings.Contains(string(archive), "release-explain") {
		t.Fatal("archive table is missing the release-explain action kind")
	}

	code, _, comics := webGet(t, server, "/ui?view=comics")
	if code != http.StatusOK {
		t.Fatalf("GET comics -> %d", code)
	}
	if strings.Contains(string(comics), "/api/comics/cycle") {
		t.Fatal("Fumetti page must not offer the comic cycle button (dashboard only)")
	}
}

// TestUiDashboardOrdering keeps the quick actions and the next-search panel at
// the top of the dashboard, above the "Controllo libreria e download" intro.
func TestUiDashboardOrdering(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, _, body := webGet(t, server, "/ui?view=dashboard")
	if code != http.StatusOK {
		t.Fatalf("GET dashboard -> %d", code)
	}
	html := string(body)
	intro := strings.Index(html, "Controllo libreria e download")
	actions := strings.Index(html, "Azioni rapide")
	next := strings.Index(html, "Prossima ricerca automatica")
	if intro < 0 || actions < 0 || next < 0 {
		t.Fatalf("dashboard markers missing: intro=%d actions=%d next=%d", intro, actions, next)
	}
	if actions > intro {
		t.Fatal("quick actions must appear above the intro")
	}
	if next > intro {
		t.Fatal("next automatic search must appear above the intro")
	}
}

// TestUiManualFollowsLanguage verifies the bundled manual switches with the
// active interface language.
func TestUiManualFollowsLanguage(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	if err := state.i18n.SetLanguage("it"); err != nil {
		t.Fatalf("set it: %v", err)
	}
	code, _, body := webGet(t, server, "/ui?view=manual")
	if code != http.StatusOK || !strings.Contains(string(body), "Manuale utente") {
		t.Fatalf("italian manual not served (%d)", code)
	}
	if err := state.i18n.SetLanguage("en"); err != nil {
		t.Fatalf("set en: %v", err)
	}
	code, _, body = webGet(t, server, "/ui?view=manual")
	if code != http.StatusOK || !strings.Contains(string(body), "User Manual") {
		t.Fatalf("english manual not served (%d)", code)
	}
	if err := state.i18n.SetLanguage("de"); err != nil {
		t.Fatalf("set de: %v", err)
	}
	code, _, body = webGet(t, server, "/ui?view=manual")
	if code != http.StatusOK || !strings.Contains(string(body), "User Manual") {
		t.Fatalf("German manual fallback not served (%d)", code)
	}
	if err := state.i18n.SetLanguage("fr"); err != nil {
		t.Fatalf("set fr: %v", err)
	}
	code, _, body = webGet(t, server, "/ui?view=manual")
	if code != http.StatusOK || !strings.Contains(string(body), "User Manual") {
		t.Fatalf("French manual fallback not served (%d)", code)
	}
	for _, lang := range []string{"es", "pl"} {
		if err := state.i18n.SetLanguage(lang); err != nil {
			t.Fatalf("set %s: %v", lang, err)
		}
		code, _, body = webGet(t, server, "/ui?view=manual")
		if code != http.StatusOK || !strings.Contains(string(body), "User Manual") {
			t.Fatalf("%s manual fallback not served (%d)", lang, code)
		}
	}
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
