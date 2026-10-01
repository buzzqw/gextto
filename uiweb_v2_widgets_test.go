package gextto

// uiweb_v2_widgets_test.go covers the widgets added to close the v2 migration:
// duplicate cleanup, database optimization, RAM disk, folder-rename review,
// rename progress, OAuth output, background jobs, torrent upload and the
// per-key translation editor.

import (
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
	for _, want := range []string{"Trakt", "Simkl", "Avvia accesso", "FlareSolverr"} {
		if !strings.Contains(body, want) {
			t.Fatalf("integrations page missing %q", want)
		}
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
	if code, body := v2Request(t, server, http.MethodPost, "/v2/oauth/poll", url.Values{"path": {"/api/trakt/auth/poll"}, "code": {"123456"}}); code != http.StatusOK {
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
}
