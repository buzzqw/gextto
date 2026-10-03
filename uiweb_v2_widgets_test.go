package gextto

// uiweb_v2_widgets_test.go covers the widgets added to close the v2 migration:
// duplicate cleanup, database optimization, RAM disk, folder-rename review,
// rename progress, OAuth output, background jobs, torrent upload and the
// per-key translation editor.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestV2WidgetsPanelsRender(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/v2?view=maintenance", nil)
	if code != http.StatusOK {
		t.Fatalf("maintenance page -> %d", code)
	}
	for _, want := range []string{
		"Ottimizzazione database", "RAM disk", "Rinomina contenuto cartella",
		"Progresso rinomina", "Operazioni in background", "Duplicati video in libreria",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("maintenance page missing %q", want)
		}
	}
	if strings.Contains(body, "non è ancora migrato") {
		t.Fatal("maintenance page still shows unmigrated placeholders")
	}

	code, body = v2Request(t, server, http.MethodGet, "/v2?view=integrations", nil)
	if code != http.StatusOK {
		t.Fatalf("integrations page -> %d", code)
	}
	for _, want := range []string{"Simkl", "Avvia accesso", "FlareSolverr"} {
		if !strings.Contains(body, want) {
			t.Fatalf("integrations page missing %q", want)
		}
	}
	if strings.Contains(body, "Trakt") {
		t.Fatal("integrations page still contains Trakt")
	}
	if strings.Contains(body, "non è ancora migrato") {
		t.Fatal("integrations page still shows unmigrated placeholders")
	}
}

func TestV2DownloadsUploadAndTags(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/v2?view=downloads", nil)
	if code != http.StatusOK {
		t.Fatalf("downloads page -> %d", code)
	}
	for _, want := range []string{"Aggiungi torrent", "File .torrent", "Assegna tag", "Rimuovi tag"} {
		if !strings.Contains(body, want) {
			t.Fatalf("downloads page missing %q", want)
		}
	}

	// No torrent and no file: the endpoint explains what is missing.
	if code, body := v2Request(t, server, http.MethodPost, "/v2/downloads/add", url.Values{}); code != http.StatusOK || !strings.Contains(body, "inserisci un magnet") {
		t.Fatalf("empty add -> %d: %s", code, body)
	}

	// A bulk tag action reuses the classic torrent tag API.
	if code, _ := v2Request(t, server, http.MethodPost, "/v2/downloads/table", url.Values{"bulk": {"tag"}, "tag": {"test"}, "hash": {"deadbeef"}}); code != http.StatusOK {
		t.Fatalf("tag bulk action -> %d", code)
	}
}

func TestV2MaintenanceWidgetEndpoints(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	if code, body := v2Request(t, server, http.MethodPost, "/v2/maintenance/duplicates", url.Values{"execute": {"0"}}); code != http.StatusOK || !strings.Contains(body, "Anteprima duplicati") {
		t.Fatalf("duplicates preview -> %d", code)
	}
	if code, body := v2Request(t, server, http.MethodPost, "/v2/maintenance/db", url.Values{"action": {"refresh"}}); code != http.StatusOK || !strings.Contains(body, "Ottimizzazione database") {
		t.Fatalf("db refresh -> %d", code)
	} else if strings.Contains(body, "presente") && strings.Contains(body, "<td class=\"numeric\">0 B</td>") {
		t.Fatalf("db refresh returned 0 B for present database files: %s", body)
	}
	if code, body := v2Request(t, server, http.MethodPost, "/v2/maintenance/ramdisk", url.Values{}); code != http.StatusOK || !strings.Contains(body, "RAM disk") {
		t.Fatalf("ramdisk refresh -> %d", code)
	}
	if code, body := v2Request(t, server, http.MethodPost, "/v2/maintenance/folder-rename/scan", url.Values{"path": {""}}); code != http.StatusOK || !strings.Contains(body, "Rinomina contenuto cartella") {
		t.Fatalf("folder rename scan -> %d", code)
	}
	if code, body := v2Request(t, server, http.MethodGet, "/v2/partial/rename-progress", nil); code != http.StatusOK || !strings.Contains(body, "Nessuna rinomina") {
		t.Fatalf("rename progress -> %d", code)
	}
	if code, body := v2Request(t, server, http.MethodGet, "/v2/partial/jobs", nil); code != http.StatusOK || !strings.Contains(body, "Operazioni in background") {
		t.Fatalf("jobs partial -> %d", code)
	}
}

func TestV2OAuthAndTranslationKey(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	// A forged path outside /api must never be forwarded.
	if code, body := v2Request(t, server, http.MethodPost, "/v2/oauth/start", url.Values{"path": {"http://evil.example/x"}}); code != http.StatusOK || !strings.Contains(body, "endpoint non valido") {
		t.Fatalf("oauth forged path -> %d: %s", code, body)
	}
	if code, body := v2Request(t, server, http.MethodPost, "/v2/oauth/poll", url.Values{"path": {"/api/simkl/auth/poll"}, "code": {"123456"}}); code != http.StatusOK {
		t.Fatalf("oauth poll -> %d: %s", code, body)
	}
	if code, body := v2Request(t, server, http.MethodPost, "/v2/settings/i18n/set", url.Values{"lang": {"en"}, "key": {"Chiave"}, "value": {"Key"}}); code != http.StatusOK || !strings.Contains(body, "v2-i18n-table") {
		t.Fatalf("i18n set -> %d", code)
	}
	if items, err := state.i18n.List("en"); err != nil {
		t.Fatalf("i18n list: %v", err)
	} else {
		found := false
		for _, item := range items {
			if item.Key == "Chiave" && item.Value == "Key" {
				found = true
			}
		}
		if !found {
			t.Fatal("translation not persisted")
		}
	}
}

func TestTraktIntegrationRoutesRemoved(t *testing.T) {
	server := httptest.NewServer(Router(newTestAppState(t)))
	t.Cleanup(server.Close)

	for _, request := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/trakt/status"},
		{http.MethodPost, "/api/trakt/auth/start"},
		{http.MethodGet, "/api/trakt/calendar"},
		{http.MethodPost, "/api/trakt/scrobble"},
	} {
		code, body := v2Request(t, server, request.method, request.path, nil)
		if code != http.StatusNotFound {
			t.Errorf("removed route %s %s -> %d: %s", request.method, request.path, code, body)
		}
	}
}

func TestV2SeriesRenamePreviewPanel(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{{Name: "Test Show", Seasons: "*"}}, nil); err != nil {
		t.Fatalf("SaveLibrary: %v", err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/v2/series/rename-preview?series=Test%20Show", nil)
	if code != http.StatusOK || !strings.Contains(body, "Anteprima rinomina") {
		t.Fatalf("rename preview -> %d", code)
	}
	if !strings.Contains(body, "Test Show") {
		t.Fatalf("rename preview missing series name")
	}
	// The execute endpoint re-renders the same panel.
	if code, body := v2Request(t, server, http.MethodPost, "/v2/series/rename-execute", url.Values{"series": {"Test Show"}}); code != http.StatusOK || !strings.Contains(body, "Anteprima rinomina") {
		t.Fatalf("rename execute -> %d", code)
	}
}

func TestV2RenameCompositionPreview(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodPost, "/v2/settings/rename-preview", url.Values{"format": {"custom"}, "template": {"{Serie} - {Stagione}{Episodio} [{Risoluzione}]"}})
	if code != http.StatusOK || !strings.Contains(body, "Nome Serie - S01E02 [1080p].mkv") {
		t.Fatalf("rename preview -> %d: %s", code, body)
	}
	if code, body := v2Request(t, server, http.MethodGet, "/v2?view=settings&tab=rename", nil); code != http.StatusOK || !strings.Contains(body, "v2-rename-preview-code") {
		t.Fatalf("rename tab preview -> %d", code)
	}
}

func TestV2TmdbExploreEndpoints(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	// No TMDB key in the hermetic state: the calendar fragment reports it.
	if code, body := v2Request(t, server, http.MethodGet, "/v2/tmdb/calendar", nil); code != http.StatusOK || !strings.Contains(body, "TMDB API key") {
		t.Fatalf("tmdb calendar -> %d: %s", code, body)
	}
	// An empty search prompt does not call TMDB.
	if code, body := v2Request(t, server, http.MethodPost, "/v2/tmdb/search", url.Values{"kind": {"series"}, "query": {""}}); code != http.StatusOK || !strings.Contains(body, "Inserisci un titolo") {
		t.Fatalf("tmdb empty search -> %d", code)
	}
	// A forged add without an id is rejected but rendered as a fragment.
	if code, _ := v2Request(t, server, http.MethodPost, "/v2/tmdb/add", url.Values{"kind": {"series"}, "name": {"X"}}); code != http.StatusOK {
		t.Fatalf("tmdb add -> %d", code)
	}
	// The list-page TMDB form uses the same server-rendered result fragment.
	if code, body := v2Request(t, server, http.MethodPost, "/v2/section/form", url.Values{
		"view": {"series"}, "path": {"/api/tmdb/search"}, "render": {"tmdb"},
		"kind": {"series"}, "query": {"Example"},
	}); code != http.StatusOK || !strings.Contains(body, "TMDB API key") {
		t.Fatalf("tmdb section form -> %d: %s", code, body)
	}
	if code, body := v2Request(t, server, http.MethodGet, "/v2/tmdb/manual?kind=series&query=Titolo+da+cercare&redirect=%2Fv2%3Fview%3Dseries", nil); code != http.StatusOK ||
		!strings.Contains(body, `name="quality"`) || !strings.Contains(body, `name="language"`) ||
		!strings.Contains(body, `name="subtitle"`) || !strings.Contains(body, `data-preset-for="subtitle"`) ||
		!strings.Contains(body, `name="archive_path"`) || !strings.Contains(body, `data-v2-browse-for="archive_path"`) ||
		!strings.Contains(body, `hx-target="#v2-tmdb-add-result"`) || !strings.Contains(body, `value="Titolo da cercare"`) {
		t.Fatalf("tmdb manual form -> %d: %s", code, body)
	}
	if code, body := v2Request(t, server, http.MethodGet, "/v2?view=series", nil); code != http.StatusOK || !strings.Contains(body, `id="v2-modal"`) {
		t.Fatalf("series add modal target -> %d", code)
	}
}

// TestV2DuplicatesPreviewFeedbackAndRanks covers the v2 duplicate panel: the
// rank fields must decode from the JSON (they need explicit tags) and a preview
// that finds nothing must say so instead of looking like nothing happened.
func TestV2DuplicatesPreviewFeedbackAndRanks(t *testing.T) {
	raw := []byte(`{"ok":true,"count":1,"items":[{"series":"Example","season":1,"episode":1,"path":"/x.mkv","resolution_rank":720,"best_rank":1080}]}`)
	var payload struct {
		Count int
		Items []v2DuplicatesItem
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].ResolutionRank != 720 || payload.Items[0].BestRank != 1080 {
		t.Fatalf("ranks not decoded: %+v", payload.Items)
	}

	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, page := v2Request(t, server, http.MethodGet, "/v2?view=maintenance", nil)
	if code != http.StatusOK {
		t.Fatalf("maintenance -> %d", code)
	}
	for _, want := range []string{
		`data-v2-toast-title="Duplicati video"`,
		`data-v2-toast-message="Analisi avviata`,
		`data-v2-toast-message="Pulizia avviata`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("maintenance page missing %q", want)
		}
	}

	if code, body := v2Request(t, server, http.MethodPost, "/v2/maintenance/duplicates", url.Values{"execute": {"0"}}); code != http.StatusOK || !strings.Contains(body, "Nessun duplicato inferiore trovato") {
		t.Fatalf("preview with no results -> %d: %s", code, body)
	}
}

// TestV2DuplicatesFoundMessage checks the toast text names the duplicates.
func TestV2DuplicatesFoundMessage(t *testing.T) {
	items := []v2DuplicatesItem{
		{Path: "/a/Example - S01E01 - [480p].avi"},
		{Path: "/a/Example - S02E02 - [720p].mkv"},
	}
	message := v2DuplicatesFoundMessage(items, 2)
	if !strings.Contains(message, "2") || !strings.Contains(message, "Example - S01E01 - [480p].avi") {
		t.Fatalf("message = %q", message)
	}
	many := make([]v2DuplicatesItem, 5)
	for index := range many {
		many[index] = v2DuplicatesItem{Path: fmt.Sprintf("/a/f%d.mkv", index)}
	}
	if capped := v2DuplicatesFoundMessage(many, 5); !strings.Contains(capped, "e altri 2") {
		t.Fatalf("capped message = %q", capped)
	}
	if single := v2DuplicatesFoundMessage([]v2DuplicatesItem{{Path: "/a/only.mkv"}}, 1); !strings.Contains(single, "Trovato 1 duplicato inferiore") {
		t.Fatalf("singular message = %q", single)
	}
}

// TestV2DuplicatesPanelRendersOOBToast checks a scan that finds something emits
// an out-of-band toast (and the table still lists the files).
func TestV2DuplicatesPanelRendersOOBToast(t *testing.T) {
	var buffer bytes.Buffer
	view := v2DuplicatesView{
		Scanned: true,
		Count:   1,
		Items: []v2DuplicatesItem{{
			Series:         "Example",
			Season:         1,
			Episode:        1,
			Path:           "/a/Example - S01E01 - [480p].avi",
			ResolutionRank: 480,
			BestRank:       1080,
		}},
		Notify:        true,
		NotifyMessage: "Trovati 1 duplicati inferiori: Example - S01E01 - [480p].avi.",
	}
	if err := v2Templates.ExecuteTemplate(&buffer, "v2_duplicates_panel", view); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buffer.String()
	for _, want := range []string{`hx-swap-oob="true"`, "Trovati 1 duplicati inferiori", "480 (migliore 1080)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("panel missing %q: %s", want, out)
		}
	}
}
