package gextto

// uiweb_v2_test.go covers the isolated SSR+HTMX interface under /v2.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestV2SettingsRedirectUsesOnlyKnownLocalTargets(t *testing.T) {
	for _, test := range []struct {
		tab  string
		want string
	}{
		{tab: "sources", want: "/?view=settings&tab=sources"},
		{tab: "https://evil.example/", want: "/?view=settings"},
		{tab: "//evil.example/", want: "/?view=settings"},
	} {
		t.Run(test.tab, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v2/settings", nil)
			response := httptest.NewRecorder()
			v2SettingsRedirect(response, request, test.tab)
			if response.Code != http.StatusSeeOther {
				t.Fatalf("redirect status = %d, want %d", response.Code, http.StatusSeeOther)
			}
			if got := response.Header().Get("Location"); got != test.want {
				t.Fatalf("Location = %q, want %q", got, test.want)
			}
		})
	}
}

func TestV2ContentFilterArchiveCleanupSearchAndBulkDelete(t *testing.T) {
	state := newTestAppState(t)
	if _, err := state.archive.db.Exec(`INSERT INTO archive(title,magnet,source,added_at) VALUES
		('Adult Porno Release 1','magnet:cleanup-1','feed','2026-01-01'),
		('Adult Porno Release 2','magnet:cleanup-2','feed','2026-01-02'),
		('Family Movie','magnet:keep','feed','2026-01-03')`); err != nil {
		t.Fatalf("insert archive fixtures: %v", err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/v2/settings/body?tab=sources", nil)
	if code != http.StatusOK || !strings.Contains(body, "Filtra e Cancella") || !strings.Contains(body, "Pulisci i risultati già archiviati") || !strings.Contains(body, "v2-content-filter-tools") || !strings.Contains(body, "v2-content-filter-section") || !strings.Contains(body, "v2-content-archive-cleanup") {
		t.Fatalf("content archive cleanup panel missing -> %d: %s", code, body)
	}
	if strings.Count(body, `class="panel settings-panel v2-content-filter-tools"`) != 1 || strings.Contains(body, `class="panel settings-panel v2-checkbox-panel-content_filters"`) {
		t.Fatalf("content filters and archive cleanup should share one frame: %s", body)
	}
	if strings.Count(body, `name="value" value="[non-latino]"`) != 1 || strings.Contains(body, "v2-content-archive-prefilters") {
		t.Fatalf("content filter options should appear only once in the shared panel: %s", body)
	}
	code, body = v2Request(t, server, http.MethodPost, "/v2/settings/checkbox", url.Values{"key": {"content_filters"}, "value": {"non_latin"}})
	if code != http.StatusOK || !strings.Contains(body, `id="v2-content-filter-section"`) || strings.Contains(body, "Pulisci i risultati già archiviati") {
		t.Fatalf("saving content filters should update only the inner section -> %d: %s", code, body)
	}
	code, body = v2Request(t, server, http.MethodGet, "/v2/settings/body?tab=sources", nil)
	if code != http.StatusOK || !strings.Contains(body, `name="filter" value="non_latin"`) {
		t.Fatalf("saved content filters should be preselected for archive cleanup -> %d: %s", code, body)
	}
	code, body = v2Request(t, server, http.MethodGet, "/v2/settings/content-archive?q=porno", nil)
	if code != http.StatusOK || !strings.Contains(body, "Adult Porno Release 1") || !strings.Contains(body, "Adult Porno Release 2") {
		t.Fatalf("content archive search -> %d: %s", code, body)
	}
	if !strings.Contains(body, `data-v2-toast-message="Avvio eliminazione dei risultati selezionati."`) || !strings.Contains(body, `data-v2-toast-message="Avvio eliminazione di tutti i 2 risultati corrispondenti."`) {
		t.Fatalf("archive deletion buttons should announce operation start -> %s", body)
	}
	code, body = v2Request(t, server, http.MethodPost, "/v2/settings/content-archive/delete", url.Values{
		"q":          {"porno"},
		"delete_all": {"1"},
	})
	if code != http.StatusOK || !strings.Contains(body, "2 voci eliminate") || !strings.Contains(body, "Nessun risultato") || !strings.Contains(body, `id="v2-toast-region" hx-swap-oob="innerHTML"`) {
		t.Fatalf("bulk content archive delete -> %d: %s", code, body)
	}
	remaining, err := state.archive.Count()
	if err != nil || remaining != 1 {
		t.Fatalf("archive count after cleanup = (%d, %v), want (1, nil)", remaining, err)
	}
}

func TestV2ShellRendersNavigationAndOfficialCss(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/v2", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /v2 -> %d", code)
	}
	for _, want := range []string{`id="v2-page"`, "/v2/static/gextto-ui.css", "/v2/static/htmx.min.js", ">Scarico<", ">Configurazione<"} {
		if !strings.Contains(body, want) {
			t.Fatalf("v2 shell missing %q", want)
		}
	}
	if strings.Contains(body, `class="crumb v2-product-crumb"`) {
		t.Fatalf("v2 topbar should not duplicate the product name above the page title")
	}
	if strings.Count(body, `class="metric"`)+strings.Count(body, `class="metric `) != 8 {
		t.Fatalf("dashboard summary should contain 8 metric tiles")
	}
	code, css := v2Request(t, server, http.MethodGet, "/v2/static/v2.css", nil)
	if code != http.StatusOK || !strings.Contains(css, "grid-template-columns: repeat(8, minmax(0, 1fr))") {
		t.Fatalf("dashboard eight-column layout missing -> %d", code)
	}
	if strings.Contains(body, `src="/ui/static/gextto-ui.js"`) {
		t.Fatal("v2 page must not load the classic UI script")
	}
	code, body = v2Request(t, server, http.MethodGet, "/v2/partial/chrome", nil)
	if code != http.StatusOK || !strings.Contains(body, `id="v2-live-top-metrics"`) {
		t.Fatalf("live chrome partial -> %d", code)
	}
	code, body = v2Request(t, server, http.MethodGet, "/v2/partial/chrome?mobile=1", nil)
	if code != http.StatusOK || !strings.Contains(body, `id="v2-live-mobile-metrics"`) {
		t.Fatalf("live mobile chrome partial -> %d", code)
	}
}

func TestV2IsTheDefaultRootUI(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	code, body := v2Request(t, server, http.MethodGet, "/", nil)
	if code != http.StatusOK || !strings.Contains(body, `id="v2-page"`) {
		t.Fatalf("GET / should render the official v2 shell -> %d", code)
	}
	if strings.Contains(body, `src="/ui/static/gextto-ui.js"`) {
		t.Fatal("official root UI must not load the classic script")
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

func TestV2MovieHistoryIsAvailableFromLibrary(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/v2?view=movies&tab=downloaded", nil)
	if code != http.StatusOK {
		t.Fatalf("movie history -> %d", code)
	}
	for _, marker := range []string{"Film monitorati", "Film scaricati", "Nessun film scaricato.", `href="/?view=movies&amp;tab=downloaded"`} {
		if !strings.Contains(body, marker) {
			t.Fatalf("movie history missing %q", marker)
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
	for _, marker := range []string{"Backup", "Prossima ricerca", "automatica", "Cerca in archivio", "dashboard-feed", "Carica risultati", "dashboard-actions-next-grid", "dashboard-consumption-panel", "dashboard-calendar-panel"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("dashboard missing %q", marker)
		}
	}
	if !strings.Contains(body, "data-v2-dashboard-search") {
		t.Fatalf("dashboard search form missing")
	}
	code, script := v2Request(t, server, http.MethodGet, "/v2/static/v2-core.js", nil)
	if code != http.StatusOK || !strings.Contains(script, "/api/search/archive") || !strings.Contains(script, "/api/search/dashboard") || !strings.Contains(script, "data-v2-dashboard-search-filter") {
		t.Fatalf("dashboard two-phase search missing -> %d", code)
	}
	if strings.Index(body, "dashboard-actions-next-grid") > strings.Index(body, "dashboard-search-panel") || strings.Index(body, "Ultimo ciclo") > strings.Index(body, "Consumo banda") {
		t.Fatalf("dashboard panel order is incorrect")
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
	if strings.Index(body, ">Rimuovi tag<") > strings.Index(body, `placeholder="Nuovo tag"`) {
		t.Fatal("new tag field should appear to the right of the remove-tag action")
	}
}

func TestV2RunCycleShowsDashboardConfirmation(t *testing.T) {
	state := newTestAppState(t)
	state.cfg.Active = true
	config, err := json.Marshal(state.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state.config_path, config, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CompleteSetup(state.cfg); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodPost, "/v2/run-cycle", url.Values{"domain": {"series"}})
	if code != http.StatusOK || !strings.Contains(body, `class="v2-toast`) || !strings.Contains(body, "Monitoraggio Serie TV avviato.") {
		t.Fatalf("cycle confirmation missing: status=%d body=%q", code, body)
	}
	// V2RunCycle deliberately returns before the monitoring goroutine completes.
	// The state writes in t.TempDir(), so do not let t.TempDir cleanup race that
	// goroutine on slower CI runners.
	deadline := time.Now().Add(10 * time.Second)
	for state.manualCyclePending.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if state.manualCyclePending.Load() {
		t.Fatal("manual cycle did not finish before test cleanup")
	}
}

func TestV2TmdbMovieAddKeepsMovieKind(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	form := url.Values{
		"kind":     {"movie"},
		"name":     {"The Rush - Corsa contro il tempo"},
		"year":     {"2026"},
		"tmdb_id":  {"1377237"},
		"language": {"ita"},
		"redirect": {"/v2?view=search"},
	}
	code, _ := v2Request(t, server, http.MethodPost, "/v2/tmdb/add", form)
	if code != http.StatusNoContent {
		t.Fatalf("movie add -> %d, want %d", code, http.StatusNoContent)
	}
	cfg, err := LoadConfig(state.config_path)
	if err != nil {
		t.Fatal(err)
	}
	foundMovie := false
	for _, movie := range cfg.Movies {
		if movie.Name == "The Rush - Corsa contro il tempo" {
			foundMovie = true
		}
	}
	for _, series := range cfg.Series {
		if series.Name == "The Rush - Corsa contro il tempo" {
			t.Fatal("movie was added to the series library")
		}
	}
	if !foundMovie {
		t.Fatal("movie was not added to the movie library")
	}
}

func TestV2TmdbSeriesAddKeepsSubtitle(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	form := url.Values{
		"kind":     {"series"},
		"name":     {"Subtitle Test Series"},
		"tmdb_id":  {"987654"},
		"language": {"ita"},
		"subtitle": {"ita,eng"},
		"seasons":  {"1+"},
	}
	code, _ := v2Request(t, server, http.MethodPost, "/v2/tmdb/add", form)
	if code != http.StatusOK && code != http.StatusNoContent {
		t.Fatalf("series add -> %d", code)
	}
	cfg, err := LoadConfig(state.config_path)
	if err != nil {
		t.Fatal(err)
	}
	for _, series := range cfg.Series {
		if series.Name == "Subtitle Test Series" {
			if series.Subtitle != "ita,eng" {
				t.Fatalf("subtitle = %q, want ita,eng", series.Subtitle)
			}
			return
		}
	}
	t.Fatal("series was not added")
}

func TestV2DownloadsPageAndActions(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/v2?view=downloads", nil)
	if code != http.StatusOK || !strings.Contains(body, `id="v2-torrents-wrap"`) || !strings.Contains(body, "Download in sessione") || strings.Contains(body, "Download session") || !strings.Contains(body, `Auto: on`) || !strings.Contains(body, `every 5s`) {
		t.Fatalf("downloads page -> %d", code)
	}
	code, body = v2Request(t, server, http.MethodPost, "/v2/downloads/table", url.Values{"auto_state": {"1"}})
	if code != http.StatusOK || !strings.Contains(body, `name="auto_state" value="1"`) || !strings.Contains(body, `every 5s`) {
		t.Fatalf("automatic refresh state -> %d", code)
	}
	code, body = v2Request(t, server, http.MethodPost, "/v2/downloads/settings", url.Values{"op": {"clear_completed"}})
	if code != http.StatusOK || !strings.Contains(body, `id="v2-toast-region" hx-swap-oob="innerHTML"`) || !strings.Contains(body, "Nessun torrent completato da rimuovere") {
		t.Fatalf("clear completed toast -> %d: %s", code, body)
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

func TestV2TorrentColumnsMatchRenderedRowCells(t *testing.T) {
	// A torrent row has selection + every sortable column + actions. Keeping
	// Stato in this list prevents the header from drifting one cell left.
	if len(v2TorrentColumns) != 8 || v2TorrentColumns[1] != (v2Column{Key: "state", Label: "Stato"}) {
		t.Fatalf("torrent columns = %#v, want Stato after Nome", v2TorrentColumns)
	}
	if !v2ValidSort("state") {
		t.Fatal("state must be a valid torrent sort key")
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
	if code != http.StatusOK || !strings.Contains(body, "Solo torrent") || !strings.Contains(body, "Sposta su NAS / Archivia") || !strings.Contains(body, `title="Rimuove il torrent dalla sessione`) || !strings.Contains(body, `hx-disabled-elt="this"`) || !strings.Contains(body, `v2-torrent-deadbeef`) {
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

func TestV2ScoreGroupsCanBeAddedEditedAndRemoved(t *testing.T) {
	state := newTestAppState(t)
	if err := saveConfigSetting(state.cfg.DataDir, "score_bonus_ita", "100"); err != nil {
		t.Fatalf("seed score bonus setting: %v", err)
	}
	if err := saveConfigSetting(state.cfg.DataDir, "score_res_1080p", "1000"); err != nil {
		t.Fatalf("seed score resolution setting: %v", err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/v2?view=settings&tab=scores", nil)
	if code != http.StatusOK || !strings.Contains(body, "Gruppi custom") || !strings.Contains(body, `name="name"`) {
		t.Fatalf("score groups editor -> %d: %s", code, body)
	}
	if !strings.Contains(body, `name="value" id="input-score_res_2160p" value="2000"`) || !strings.Contains(body, `type="number"`) {
		t.Fatalf("canonical score controls missing on fresh configuration: %s", body)
	}
	if strings.Index(body, "Gruppi custom") < strings.Index(body, "Risoluzione") {
		t.Fatalf("custom score groups should appear after the score sections: %s", body)
	}
	if strings.Contains(body, "score_bonus_ita") {
		t.Fatalf("unsupported Italian-language score bonus should not be exposed: %s", body)
	}

	code, body = v2Request(t, server, http.MethodPost, "/v2/settings/score-groups", url.Values{"op": {"add"}, "name": {"TBK"}, "score": {"125"}})
	if code != http.StatusOK || !strings.Contains(body, "tbk") || !strings.Contains(body, `value="125"`) {
		t.Fatalf("add score group -> %d: %s", code, body)
	}
	code, body = v2Request(t, server, http.MethodPost, "/v2/settings/score-groups", url.Values{"op": {"save"}, "name": {"tbk"}, "score": {"-25"}})
	if code != http.StatusOK || !strings.Contains(body, `value="-25"`) {
		t.Fatalf("edit score group -> %d: %s", code, body)
	}
	code, body = v2Request(t, server, http.MethodPost, "/v2/settings/score-groups", url.Values{"op": {"delete"}, "name": {"tbk"}})
	if code != http.StatusOK || !strings.Contains(body, "Gruppo rimosso") || strings.Contains(body, `value="-25"`) {
		t.Fatalf("delete score group -> %d: %s", code, body)
	}
	code, body = v2Request(t, server, http.MethodPost, "/v2/settings/score-groups", url.Values{"op": {"add"}, "name": {"../../other"}, "score": {"1"}})
	if code != http.StatusOK || !strings.Contains(body, "Nome gruppo non valido") {
		t.Fatalf("reject invalid score group -> %d: %s", code, body)
	}
	code, body = v2Request(t, server, http.MethodPost, "/v2/settings/score-groups", url.Values{"op": {"add"}, "name": {"unknown"}, "score": {"1"}})
	if code != http.StatusOK || !strings.Contains(body, "Nome gruppo non valido") {
		t.Fatalf("reject reserved unknown score group -> %d: %s", code, body)
	}
}

func TestV2LogsAndStaticAndNoJSFallback(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	if code, body := v2Request(t, server, http.MethodGet, "/v2?view=logs", nil); code != http.StatusOK || !strings.Contains(body, "v2-logs-view") {
		t.Fatalf("logs page -> %d", code)
	}
	if code, css := v2Request(t, server, http.MethodGet, "/v2/static/v2.css", nil); code != http.StatusOK || !strings.Contains(css, ".logs-toolbar") {
		t.Fatalf("logs toolbar layout missing -> %d", code)
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
	// Unknown views render a safe placeholder without exposing a legacy UI.
	if code, body := v2Request(t, server, http.MethodGet, "/v2?view=view-inesistente", nil); code != http.StatusOK || !strings.Contains(body, "Questa sezione non è disponibile.") || strings.Contains(body, "UI classica") {
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
	if code, body := v2Request(t, server, http.MethodGet, "/v2/table?view=archive&q=Minions&web=1", nil); code != http.StatusOK || !strings.Contains(body, "v2-table-body-archive") {
		t.Fatalf("archive table with web search -> %d", code)
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
	// A Weekly Pack force action has a dedicated endpoint, so the user receives
	// an explicit queued/already-queued result instead of a silently refreshed
	// generic table.
	weeklyAction := v2RenderAction("comics-t1", map[string]any{
		"pack_date": "2026-09-23", "magnet": "magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
	}, uiAction{Label: "Forza", Kind: "comic-weekly-force"}, uiTableSpec{})
	if !strings.Contains(weeklyAction, `hx-post="/v2/comics/weekly/force"`) || strings.Contains(weeklyAction, "/v2/table/action") {
		t.Fatalf("weekly force action = %q", weeklyAction)
	}
	if code, body := v2Request(t, server, http.MethodPost, "/v2/comics/weekly/force", url.Values{"date": {"2026-09-23"}}); code != http.StatusOK || !strings.Contains(body, "link del Weekly Pack non disponibile") {
		t.Fatalf("weekly force unavailable-link response -> %d: %s", code, body)
	}

	// Archive row action properly substitutes JSON placeholders and includes toolbar filters.
	archiveAction := v2RenderAction("archive", map[string]any{
		"title":  "Minions 2026",
		"magnet": "magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
		"source": "CYBER",
	}, uiAction{
		Label:  "Scarica",
		Method: "POST",
		Path:   "/api/archive/batch-download",
		Body:   `{"items":[{"title":"{title}","magnet":"{magnet}","source":"{source}"}]}`,
	}, uiTableSpec{})
	if !strings.Contains(archiveAction, `hx-include="closest .panel form.toolbar"`) {
		t.Fatalf("archiveAction missing toolbar hx-include: %s", archiveAction)
	}
	if !strings.Contains(archiveAction, `\"title\":\"Minions 2026\"`) {
		t.Fatalf("archiveAction missing substituted title: %s", archiveAction)
	}

	// Action execution preserves filter query 'q' and returns an out-of-band toast notice.
	code, body := v2Request(t, server, http.MethodPost, "/v2/table/action", url.Values{
		"view":   {"archive"},
		"path":   {"/api/archive/batch-download"},
		"method": {"POST"},
		"body":   {`{"items":[{"title":"Minions 2026","magnet":"magnet:?xt=urn:btih:0123456789012345678901234567890123456789","source":"CYBER"}]}`},
		"q":      {"minions"},
	})
	if code != http.StatusOK {
		t.Fatalf("archive action -> %d", code)
	}
	if !strings.Contains(body, `hx-swap-oob="innerHTML"`) || !strings.Contains(body, `id="v2-toast-region"`) {
		t.Fatalf("archive action response missing OOB toast notice: %s", body)
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
		"archive": {"Aggiungi all&#39;archivio", "Cerca anche nel web", "archive-search-input"},
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

func TestV2LibraryLinksUseCanonicalRoot(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{{Name: "Test Show"}}, []MovieConfig{{ID: 7, Name: "Test Movie"}}); err != nil {
		t.Fatalf("SaveLibrary: %v", err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	for view, marker := range map[string]string{
		"series": `/?view=series&amp;series=Test+Show`,
		"movies": `/?view=movies&amp;movie=7`,
	} {
		code, body := v2Request(t, server, http.MethodGet, "/v2?view="+view, nil)
		if code != http.StatusOK || !strings.Contains(body, marker) {
			t.Fatalf("%s detail link missing: status=%d", view, code)
		}
	}
}

func TestV2NavigationUsesCanonicalRootURLs(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/?view=dashboard", nil)
	if code != http.StatusOK {
		t.Fatalf("root dashboard -> %d", code)
	}
	if strings.Contains(body, `href="/v2?view=`) {
		t.Fatal("visible navigation must use canonical root URLs")
	}
	if !strings.Contains(body, `href="/?view=dashboard"`) {
		t.Fatal("dashboard link must use canonical root URL")
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
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{{Name: "Test Show", Seasons: "*", Quality: "1080p", Language: "ita"}}, []MovieConfig{{ID: 7, Name: "Test Movie", Year: "2020", Enabled: true, LanguageRequirements: `[{"language":"ita","required":true}]`}}); err != nil {
		t.Fatalf("SaveLibrary: %v", err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/v2?view=series&series=Test%20Show", nil)
	if code != http.StatusOK || !strings.Contains(body, "Test Show") || !strings.Contains(body, "Episodi") || !strings.Contains(body, `data-v2-browse-for="archive_path"`) {
		t.Fatalf("series detail -> %d", code)
	}
	code, body = v2Request(t, server, http.MethodGet, "/?view=series&series=Test%20Show", nil)
	if code != http.StatusOK || !strings.Contains(body, "Test Show") || !strings.Contains(body, `data-v2-browse-for="archive_path"`) {
		t.Fatalf("root series detail -> %d", code)
	}
	code, body = v2Request(t, server, http.MethodGet, "/v2?view=movies&movie=7", nil)
	if code != http.StatusOK || !strings.Contains(body, "Test Movie") || !strings.Contains(body, `name="language_requirements" value="ita"`) || strings.Contains(body, `name="language_requirements" value="[{\"language\":\"ita\"`) {
		t.Fatalf("movie detail -> %d", code)
	}
	code, body = v2Request(t, server, http.MethodGet, "/?view=movies&movie=7", nil)
	if code != http.StatusOK || !strings.Contains(body, "Test Movie") {
		t.Fatalf("root movie detail -> %d", code)
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
	// The unified filter/cleanup block saves only its filter section.
	if code, body := v2Request(t, server, http.MethodPost, "/v2/settings/checkbox", url.Values{"key": {"content_filters"}, "value": {"[porno]"}}); code != http.StatusOK || !strings.Contains(body, `id="v2-content-filter-section"`) {
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

// TestV2HealthTilesPollIndependently checks the Salute tiles refresh on their
// own schedules: Stato, Memoria and Uptime every 5s, Disco dati hourly.
func TestV2HealthTilesPollIndependently(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/v2?view=health", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /v2?view=health -> %d", code)
	}
	for _, want := range []string{
		`id="v2-health-tile-status" hx-get="/v2/partial/health/tile?name=status" hx-trigger="every 5s"`,
		`id="v2-health-tile-memory" hx-get="/v2/partial/health/tile?name=memory" hx-trigger="every 5s"`,
		`id="v2-health-tile-uptime" hx-get="/v2/partial/health/tile?name=uptime" hx-trigger="every 5s"`,
		`id="v2-health-tile-disk" hx-get="/v2/partial/health/tile?name=disk" hx-trigger="every 3600s"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("health page missing %q", want)
		}
	}

	for _, name := range []string{"status", "memory", "disk", "uptime"} {
		code, body = v2Request(t, server, http.MethodGet, "/v2/partial/health/tile?name="+name, nil)
		if code != http.StatusOK {
			t.Fatalf("tile %s -> %d", name, code)
		}
		if !strings.Contains(body, `id="v2-health-tile-`+name+`"`) {
			t.Fatalf("tile %s body missing its id: %s", name, body)
		}
	}

	if code, _ := v2Request(t, server, http.MethodGet, "/v2/partial/health/tile?name=bogus", nil); code != http.StatusNotFound {
		t.Fatalf("unknown tile -> %d, want 404", code)
	}
}

// TestV2ActionFlash covers the toast text derived from a forwarded action.
func TestV2ActionFlash(t *testing.T) {
	message, isErr := v2ActionFlash("/api/maintenance/clean-trash", []byte(`{"ok":true,"files":3}`), http.StatusOK)
	if isErr || !strings.Contains(message, "3") {
		t.Fatalf("clean trash success -> %q err=%v", message, isErr)
	}
	message, isErr = v2ActionFlash("/api/maintenance/clean-trash", []byte(`{"ok":true,"files":0}`), http.StatusOK)
	if isErr || !strings.Contains(message, "già vuoto") {
		t.Fatalf("clean trash empty -> %q err=%v", message, isErr)
	}
	message, isErr = v2ActionFlash("/api/backup", []byte(`{"ok":false,"error":"disco pieno"}`), http.StatusInternalServerError)
	if !isErr || !strings.Contains(message, "disco pieno") {
		t.Fatalf("error flash -> %q err=%v", message, isErr)
	}
	message, isErr = v2ActionFlash("/api/scan-all-archives", []byte(`{"ok":true}`), http.StatusOK)
	if isErr || message == "" {
		t.Fatalf("generic success -> %q err=%v", message, isErr)
	}
}

// TestV2MaintenanceTrashActionAndFlash checks the Pulisci trash action empties
// the whole trash and that a redirect flash is rendered as a global toast.
func TestV2MaintenanceTrashActionAndFlash(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/v2?view=maintenance", nil)
	if code != http.StatusOK {
		t.Fatalf("maintenance page -> %d", code)
	}
	if !strings.Contains(body, `path" value="/api/maintenance/clean-trash"`) {
		t.Fatal("maintenance page is missing the clean-trash action")
	}
	if !strings.Contains(body, `force&#34;:true`) {
		t.Fatal("clean-trash action must force-empty the whole trash")
	}
	if !strings.Contains(body, `id="v2-toast-region"`) {
		t.Fatal("global toast region missing from the shell")
	}

	// A flash arriving from a redirect is rendered and clears itself.
	code, body = v2Request(t, server, http.MethodGet, "/v2?view=maintenance&toast=Cestino+svuotato%3A+3+elementi&toast_err=0", nil)
	if code != http.StatusOK {
		t.Fatalf("maintenance with flash -> %d", code)
	}
	if !strings.Contains(body, `Cestino svuotato: 3 elementi`) || !strings.Contains(body, `class="v2-toast"`) {
		t.Fatalf("flash toast not rendered: %s", body)
	}

	// The action endpoint reports its outcome through the redirect URL. The test
	// state runs in dry-run, so clean-trash fails and the toast is an error.
	response, err := http.PostForm(server.URL+"/v2/section/action", url.Values{
		"view":   {"maintenance"},
		"path":   {"/api/maintenance/clean-trash"},
		"method": {"POST"},
		"body":   {`{"force":true}`},
	})
	if err != nil {
		t.Fatalf("section action: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("section action -> %d, want 204", response.StatusCode)
	}
	if redirect := response.Header.Get("HX-Redirect"); !strings.Contains(redirect, "toast_err=1") || !strings.Contains(redirect, "view=maintenance") {
		t.Fatalf("action redirect missing toast: %q", redirect)
	}
}

// TestV2LogsSelectorReadsChosenFile checks the Log page offers the available
// gextto log files (current first) and reads the one the user selects.
func TestV2LogsSelectorReadsChosenFile(t *testing.T) {
	state := newTestAppState(t)
	dir := state.cfg.DataDir
	if err := os.WriteFile(filepath.Join(dir, "gextto.log"), []byte("CURRENT-MARKER\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gextto.log.1"), []byte("BACKUP-MARKER\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, page := v2Request(t, server, http.MethodGet, "/v2?view=logs", nil)
	if code != http.StatusOK {
		t.Fatalf("logs page -> %d", code)
	}
	for _, want := range []string{
		`name="log"`,
		`value="gextto.log" selected`,
		`gextto.log (attivo)`,
		`value="gextto.log.1"`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("logs page missing %q", want)
		}
	}

	code, body := v2Request(t, server, http.MethodGet, "/v2/partial/logs?log=gextto.log.1", nil)
	if code != http.StatusOK || !strings.Contains(body, "BACKUP-MARKER") || strings.Contains(body, "CURRENT-MARKER") {
		t.Fatalf("partial did not read the selected backup -> %d: %s", code, body)
	}
}

func TestSafeV2Redirect(t *testing.T) {
	fallback := "/v2?view=settings"
	tests := []struct {
		input string
		want  string
	}{
		{"/v2", "/v2"},
		{"/v2/settings?tab=advanced", "/v2/settings?tab=advanced"},
		{"https://evil.com/v2", fallback},
		{"//evil.com", fallback},
		{"/\\evil.com", fallback},
		{"/v2/..//evil.com", "/v2/..//evil.com"},
		{"javascript:alert(1)", fallback},
		{"", fallback},
		{"/?view=search", fallback},
	}
	for _, tc := range tests {
		got := safeV2Redirect(tc.input, fallback)
		if got != tc.want {
			t.Errorf("safeV2Redirect(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
