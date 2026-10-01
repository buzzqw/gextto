package gextto

// uiweb_v2_test.go covers the isolated SSR+HTMX interface under /v2.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// v2Request always sends HX-Request, exactly like the browser does for the HTMX
// actions (the handlers answer a redirect without it).
func v2Request(t *testing.T, server *httptest.Server, method, path string, form url.Values) (int, string) {
	t.Helper()
	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader(form.Encode())
	}
	request, err := http.NewRequest(method, server.URL+path, body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	request.Header.Set("HX-Request", "true")
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("%s %s: read: %v", method, path, err)
	}
	return response.StatusCode, string(raw)
}

func TestV2ShellRendersNavigationAndReusesClassicCss(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/v2", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /v2 -> %d", code)
	}
	for _, want := range []string{`id="v2-page"`, "/ui/static/gextto-ui.css", "/v2/static/htmx.min.js", ">Scarico<", ">Configurazione<"} {
		if !strings.Contains(body, want) {
			t.Fatalf("v2 shell missing %q", want)
		}
	}
	if strings.Contains(body, `src="/ui/static/gextto-ui.js"`) {
		t.Fatal("v2 page must not load the classic UI script")
	}
}

func TestV2CoversEveryClassicMenuScreen(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	for _, view := range []string{"dashboard", "downloads", "series", "movies", "gaps", "search", "archive", "comics", "settings", "integrations", "maintenance", "health", "logs", "blocklist", "manual", "license"} {
		code, body := v2Request(t, server, http.MethodGet, "/v2?view="+view, nil)
		if code != http.StatusOK {
			t.Fatalf("%s -> %d", view, code)
		}
		if strings.Contains(body, "non è ancora migrata nella UI v2") {
			t.Fatalf("%s still renders the unavailable placeholder", view)
		}
	}
}

func TestV2DashboardAndDownloadControlsMatchClassic(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	code, body := v2Request(t, server, http.MethodGet, "/v2?view=dashboard", nil)
	if code != http.StatusOK {
		t.Fatalf("dashboard -> %d", code)
	}
	for _, marker := range []string{"Backup", "Prossima ricerca automatica", "Cerca in archivio", "dashboard-feed", "Carica risultati"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("dashboard missing %q", marker)
		}
	}
	code, body = v2Request(t, server, http.MethodGet, "/v2?view=downloads", nil)
	if code != http.StatusOK {
		t.Fatalf("downloads -> %d", code)
	}
	for _, marker := range []string{"Pulisci completati", "Elimina completati dopo il seed", "Limite temporaneo", "Nuovo tag", "Prealloca spazio", "Sblocca pin", `title="Incolla un link magnet`, `title="Avvia il download subito`, `title="Ricarica l'elenco dei torrent`, `title="Applica temporaneamente i limiti`, `title="Metti in pausa i torrent selezionati`} {
		if !strings.Contains(body, marker) {
			t.Fatalf("downloads missing %q", marker)
		}
	}
}

func TestV2DownloadsPageAndActions(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/v2?view=downloads", nil)
	if code != http.StatusOK || !strings.Contains(body, `id="v2-torrents-wrap"`) {
		t.Fatalf("downloads page -> %d", code)
	}
	code, body = v2Request(t, server, http.MethodPost, "/v2/downloads/table", url.Values{"refresh": {"1"}})
	if code != http.StatusOK || !strings.Contains(body, `id="v2-torrents-wrap"`) {
		t.Fatalf("refresh -> %d", code)
	}
	code, _ = v2Request(t, server, http.MethodPost, "/v2/downloads/table", url.Values{"row_action": {"pause:deadbeef"}})
	if code != http.StatusOK {
		t.Fatalf("row action -> %d", code)
	}
	code, body = v2Request(t, server, http.MethodPost, "/v2/downloads/table", url.Values{"sort": {"name"}, "sort_click": {"ratio"}})
	if code != http.StatusOK || !strings.Contains(body, `value="ratio"`) {
		t.Fatalf("sort click -> %d", code)
	}
}

func TestV2DetailAndRemoveFragmentsRender(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/v2/downloads/detail?hash=deadbeef&tab=general", nil)
	if code != http.StatusOK || !strings.Contains(body, "v2-detail-panel") || !strings.Contains(body, `title="Mostra la sezione`) {
		t.Fatalf("detail modal -> %d", code)
	}
	code, body = v2Request(t, server, http.MethodGet, "/v2/downloads/remove?hash=deadbeef", nil)
	if code != http.StatusOK || !strings.Contains(body, "Solo torrent") || !strings.Contains(body, `title="Rimuove il torrent dalla sessione`) {
		t.Fatalf("remove modal -> %d", code)
	}
}

func TestV2SettingsPagesAndSave(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/v2?view=settings&tab=daemon", nil)
	if code != http.StatusOK || !strings.Contains(body, "v2-settings-body") {
		t.Fatalf("settings page -> %d", code)
	}
	if strings.Index(body, "Cerca una impostazione per nome o chiave.") > strings.Index(body, `class="chip-row"`) {
		t.Fatalf("settings search hint must precede settings tabs")
	}
	if !strings.Contains(body, `class="settings-search-row"`) {
		t.Fatalf("settings search row missing")
	}
	code, css := v2Request(t, server, http.MethodGet, "/v2/static/v2.css", nil)
	if code != http.StatusOK || !strings.Contains(css, "white-space: nowrap") {
		t.Fatalf("settings search hint nowrap style missing -> %d", code)
	}
	if !strings.Contains(css, "v2-theme-toggle") || !strings.Contains(css, "flex-wrap: wrap") {
		t.Fatalf("topbar compact layout missing")
	}
	code, body = v2Request(t, server, http.MethodGet, "/v2?view=settings&tab=libtorrent", nil)
	if code != http.StatusOK || !strings.Contains(body, "Auto (interfaccia predefinita)") || !strings.Contains(body, `name="value"`) {
		t.Fatalf("outgoing interface select -> %d", code)
	}
	code, body = v2Request(t, server, http.MethodGet, "/v2?view=settings&tab=paths", nil)
	if code != http.StatusOK || !strings.Contains(body, `data-v2-browse-for="libtorrent_dir"`) {
		t.Fatalf("path folder browser -> %d", code)
	}
	code, body = v2Request(t, server, http.MethodGet, "/v2?view=settings&tab=rename", nil)
	if code != http.StatusOK || !strings.Contains(body, "Italiano") || !strings.Contains(body, `value="it-IT"`) || !strings.Contains(body, `value="ita"`) {
		t.Fatalf("language selects -> %d", code)
	}
	code, body = v2Request(t, server, http.MethodGet, "/v2/settings/body?tab=seeding", nil)
	if code != http.StatusOK || !strings.Contains(body, `id="v2-settings-body"`) {
		t.Fatalf("settings body -> %d", code)
	}
	code, body = v2Request(t, server, http.MethodGet, "/v2/settings/search?q=seed", nil)
	if code != http.StatusOK || !strings.Contains(body, "data-table") {
		t.Fatalf("settings search -> %d", code)
	}
	code, body = v2Request(t, server, http.MethodPost, "/v2/settings/save", url.Values{"key": {"refresh_interval"}, "value": {"3600"}})
	if code != http.StatusOK || !strings.Contains(body, "salvata") {
		t.Fatalf("settings save -> %d: %s", code, body)
	}
	code, body = v2Request(t, server, http.MethodPost, "/v2/settings/save", url.Values{"key": {"_internal"}, "value": {"x"}})
	if code != http.StatusOK || !strings.Contains(body, "non modificabile") {
		t.Fatalf("settings save denied -> %d: %s", code, body)
	}
}

func TestV2LogsAndStaticAndNoJSFallback(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	if code, body := v2Request(t, server, http.MethodGet, "/v2?view=logs", nil); code != http.StatusOK || !strings.Contains(body, "v2-logs-view") {
		t.Fatalf("logs page -> %d", code)
	}
	if code, body := v2Request(t, server, http.MethodGet, "/v2/static/htmx.min.js", nil); code != http.StatusOK || !strings.Contains(body, "htmx") {
		t.Fatalf("htmx asset -> %d", code)
	}
	code, body := v2Request(t, server, http.MethodGet, "/v2/static/v2-core.js", nil)
	if code != http.StatusOK || !strings.Contains(body, "htmx:afterRequest") {
		t.Fatalf("settings tab sync script -> %d", code)
	}
	if !strings.Contains(body, "ensureTooltips") {
		t.Fatalf("global tooltip coverage script -> %d", code)
	}
	// Unmigrated views fall back to a placeholder that links to the classic UI.
	if code, body := v2Request(t, server, http.MethodGet, "/v2?view=view-inesistente", nil); code != http.StatusOK || !strings.Contains(body, "non è ancora migrata nella UI v2") {
		t.Fatalf("unavailable placeholder -> %d", code)
	}
	// Migrated list pages render server-side rows.
	if code, body := v2Request(t, server, http.MethodGet, "/v2?view=series", nil); code != http.StatusOK || !strings.Contains(body, "Serie monitorate") {
		t.Fatalf("series table page -> %d", code)
	}

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.PostForm(server.URL+"/v2/downloads/table", url.Values{"refresh": {"1"}})
	if err != nil {
		t.Fatalf("no-js post: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("no-js post -> %d, want 303", response.StatusCode)
	}
}

func TestV2GenericTableFragmentAndActions(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	for _, view := range []string{"series", "movies", "gaps", "archive", "blocklist", "comics"} {
		code, body := v2Request(t, server, http.MethodGet, "/v2/table?view="+view, nil)
		if code != http.StatusOK || !strings.Contains(body, "v2-table-body-"+view) {
			t.Fatalf("table fragment %s -> %d", view, code)
		}
	}
	// Library toggle/remove reuses the same read-modify-write as the classic UI.
	if code, _ := v2Request(t, server, http.MethodPost, "/v2/table/library", url.Values{"view": {"series"}, "scope": {"series"}, "name": {"NonEsiste"}, "mode": {"toggle"}}); code != http.StatusOK {
		t.Fatalf("library toggle -> %d", code)
	}
	// Generic API action is forwarded and the table re-rendered.
	if code, body := v2Request(t, server, http.MethodPost, "/v2/table/action", url.Values{"view": {"blocklist"}, "path": {"/api/blocklist/deadbeef/remove"}, "method": {"POST"}, "body": {"{}"}}); code != http.StatusOK || !strings.Contains(body, "v2-table-body-blocklist") {
		t.Fatalf("table action -> %d", code)
	}
	// A forged path outside /api must not be forwarded.
	if code, _ := v2Request(t, server, http.MethodPost, "/v2/table/action", url.Values{"view": {"blocklist"}, "path": {"http://evil.example/x"}, "method": {"POST"}, "body": {"{}"}}); code != http.StatusOK {
		t.Fatalf("table action forged path -> %d", code)
	}
}

func TestV2LibraryPagesKeepTheirPanelFlows(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	wants := map[string][]string{
		"series":  {"Aggiungi una serie", "Serie monitorate"},
		"movies":  {"Aggiungi un film", "Film monitorati"},
		"gaps":    {"Cerca un episodio mancante", "Episodi mancanti"},
		"archive": {"Aggiungi all&#39;archivio"},
	}
	for view, markers := range wants {
		code, body := v2Request(t, server, http.MethodGet, "/v2?view="+view, nil)
		if code != http.StatusOK {
			t.Fatalf("%s page -> %d", view, code)
		}
		for _, marker := range markers {
			if !strings.Contains(body, marker) {
				t.Fatalf("%s page missing %q", view, marker)
			}
		}
	}
}

func TestV2LibraryLinksStayInsideV2(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{{Name: "Test Show"}}, []MovieConfig{{ID: 7, Name: "Test Movie"}}); err != nil {
		t.Fatalf("SaveLibrary: %v", err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	for view, marker := range map[string]string{
		"series": `/v2?view=series&amp;series=Test+Show`,
		"movies": `/v2?view=movies&amp;movie=7`,
	} {
		code, body := v2Request(t, server, http.MethodGet, "/v2?view="+view, nil)
		if code != http.StatusOK || !strings.Contains(body, marker) {
			t.Fatalf("%s detail link missing: status=%d", view, code)
		}
	}
}

func TestV2PanelsPagesRender(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/v2?view=maintenance", nil)
	if code != http.StatusOK || !strings.Contains(body, "Backup disponibili") || !strings.Contains(body, "Pulizia database") || !strings.Contains(body, "Test FTP") {
		t.Fatalf("maintenance page -> %d", code)
	}
	code, body = v2Request(t, server, http.MethodGet, "/v2?view=integrations", nil)
	if code != http.StatusOK || !strings.Contains(body, "FlareSolverr") {
		t.Fatalf("integrations page -> %d", code)
	}
	code, body = v2Request(t, server, http.MethodGet, "/v2?view=search", nil)
	if code != http.StatusOK || !strings.Contains(body, "Ricerca release") {
		t.Fatalf("search page -> %d", code)
	}
	// An empty query renders the prompt, not an error.
	if code, body := v2Request(t, server, http.MethodPost, "/v2/search", url.Values{"q": {""}}); code != http.StatusOK || !strings.Contains(body, "Inserisci una ricerca") {
		t.Fatalf("empty search -> %d", code)
	}

	// Generic action endpoint answers with HX-Redirect and never forwards a
	// non-/api path.
	response, err := http.PostForm(server.URL+"/v2/section/action", url.Values{"view": {"maintenance"}, "path": {"http://evil.example/x"}, "method": {"POST"}, "body": {"{}"}})
	if err != nil {
		t.Fatalf("section action: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("section action -> %d, want 204", response.StatusCode)
	}
}

func TestV2DetailPagesRender(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{{Name: "Test Show", Seasons: "*", Quality: "1080p", Language: "ita"}}, []MovieConfig{{ID: 7, Name: "Test Movie", Year: "2020", Enabled: true}}); err != nil {
		t.Fatalf("SaveLibrary: %v", err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/v2?view=series&series=Test%20Show", nil)
	if code != http.StatusOK || !strings.Contains(body, "Test Show") || !strings.Contains(body, "Episodi") {
		t.Fatalf("series detail -> %d", code)
	}
	code, body = v2Request(t, server, http.MethodGet, "/v2?view=movies&movie=7", nil)
	if code != http.StatusOK || !strings.Contains(body, "Test Movie") {
		t.Fatalf("movie detail -> %d", code)
	}
	// Saving a series answers HX-Redirect with 204 for an HTMX request.
	saveRequest, err := http.NewRequest(http.MethodPost, server.URL+"/v2/series/save", strings.NewReader(url.Values{"name": {"Test Show"}, "seasons": {"1-3"}, "quality": {"720p"}}.Encode()))
	if err != nil {
		t.Fatalf("series save request: %v", err)
	}
	saveRequest.Header.Set("HX-Request", "true")
	saveRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	saveResponse, err := http.DefaultClient.Do(saveRequest)
	if err != nil {
		t.Fatalf("series save: %v", err)
	}
	saveResponse.Body.Close()
	if saveResponse.StatusCode != http.StatusNoContent || saveResponse.Header.Get("HX-Redirect") == "" {
		t.Fatalf("series save -> %d redirect=%q", saveResponse.StatusCode, saveResponse.Header.Get("HX-Redirect"))
	}
	// Sources modal renders even without results.
	if code, body := v2Request(t, server, http.MethodGet, "/v2/series/sources?series=Test%20Show&season=1&episode=1", nil); code != http.StatusOK || !strings.Contains(body, "v2-sources-title") {
		t.Fatalf("sources modal -> %d", code)
	}
}

func TestV2SettingsStructuredEditors(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	if code, body := v2Request(t, server, http.MethodGet, "/v2?view=settings&tab=sources", nil); code != http.StatusOK || !strings.Contains(body, "Feed RSS") {
		t.Fatalf("sources tab -> %d", code)
	}
	if code, body := v2Request(t, server, http.MethodGet, "/v2?view=settings&tab=advanced", nil); code != http.StatusOK || !strings.Contains(body, "Filtri per sorgente") {
		t.Fatalf("advanced tab -> %d", code)
	}
	if code, body := v2Request(t, server, http.MethodGet, "/v2?view=settings&tab=rename", nil); code != http.StatusOK || !strings.Contains(body, "Composizione del nome") || !strings.Contains(body, "v2-rename-form") {
		t.Fatalf("rename tab -> %d", code)
	}
	if code, body := v2Request(t, server, http.MethodGet, "/v2?view=settings&tab=i18n", nil); code != http.StatusOK || !strings.Contains(body, "v2-i18n-table") {
		t.Fatalf("i18n tab -> %d", code)
	}
	// Add-row fragment for a structured editor.
	if code, body := v2Request(t, server, http.MethodGet, "/v2/settings/editor-row?editor=indexers&index=0&view=settings&tab=advanced", nil); code != http.StatusOK || !strings.Contains(body, "list-row") || !strings.Contains(body, "Testa") {
		t.Fatalf("editor row -> %d", code)
	}
	// Checkbox group save returns the re-rendered panel.
	if code, body := v2Request(t, server, http.MethodPost, "/v2/settings/checkbox", url.Values{"key": {"content_filters"}, "value": {"[porno]"}}); code != http.StatusOK || !strings.Contains(body, "settings-panel") {
		t.Fatalf("checkbox save -> %d", code)
	}
	// Comics page shows the main table and the download queue.
	if code, body := v2Request(t, server, http.MethodGet, "/v2?view=comics", nil); code != http.StatusOK || !strings.Contains(body, "Download fumetti") {
		t.Fatalf("comics page -> %d", code)
	}
	// Trash widget renders (empty state in the hermetic state).
	if code, body := v2Request(t, server, http.MethodGet, "/v2/partial/trash", nil); code != http.StatusOK || !strings.Contains(body, "Cestino") {
		t.Fatalf("trash partial -> %d", code)
	}
}

func TestV2ComicsPageIncludesAllClassicFlows(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/v2?view=comics", nil)
	if code != http.StatusOK {
		t.Fatalf("comics page -> %d", code)
	}
	for _, marker := range []string{
		"Esplora GetComics", "Pianificazione settimanale", "Cerca un Weekly Pack",
		"Storico fumetti", "Storico Weekly Pack", "Scarica da GetComics", "v2-modal",
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("comics page missing %q", marker)
		}
	}
}

func TestV2TorrentDetailExposesEditableTabs(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/v2/downloads/detail?hash=deadbeef&tab=limits", nil)
	if code != http.StatusOK {
		t.Fatalf("detail modal -> %d", code)
	}
	for _, marker := range []string{"Limiti", "Storage", "/v2/downloads/detail/panel"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("detail modal missing %q", marker)
		}
	}
}

func TestV2LanguageSwitch(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.PostForm(server.URL+"/v2/language", url.Values{"lang": {"en"}})
	if err != nil {
		t.Fatalf("language switch: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("language switch -> %d, want 303", response.StatusCode)
	}
	if lang, _ := state.i18n.Language(); lang != "en" {
		t.Fatalf("language not persisted: %q", lang)
	}
}

// TestV2TranslateHTMLMirrorsClientBehaviour locks the server-side translation
// mechanism: text and translatable attributes are translated, script content is
// not, and Italian (no dictionary) is returned byte-for-byte.
func TestV2TranslateHTMLMirrorsClientBehaviour(t *testing.T) {
	raw := `<div><span>Percorsi</span><a title="Percorsi">x</a><script>var Percorsi = 1;</script></div>`
	got := v2TranslateHTML(raw, map[string]string{"Percorsi": "Paths"}, nil)
	if !strings.Contains(got, "<span>Paths</span>") {
		t.Fatalf("text node not translated: %s", got)
	}
	if !strings.Contains(got, `title="Paths"`) {
		t.Fatalf("attribute not translated: %s", got)
	}
	if !strings.Contains(got, `data-v2-title-translated="true"`) {
		t.Fatalf("translated title marker missing: %s", got)
	}
	if !strings.Contains(got, "var Percorsi = 1;") {
		t.Fatalf("script content must not be translated: %s", got)
	}
	if italian := v2TranslateHTML(raw, nil, nil); italian != raw {
		t.Fatalf("Italian must be untouched: %s", italian)
	}
}
