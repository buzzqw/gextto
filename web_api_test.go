package gextto

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
)

// newTestAppState builds a fully wired, hermetic AppState backed by temporary
// databases and a temporary data dir. It is shared by the web API tests and the
// engine cycle tests (same package).
//
// Hermeticity:
// - every SQLite file lives under t.TempDir();
// - the data/state/download/trash paths are redirected inside t.TempDir() so
// no handler can touch the repository's ./data directory;
// - DryRun is true and Active is false, so no real download starts;
// - the libtorrent client is a session-less dry-run client: the engine has no
// native session and List/Stats are pure in-memory no-ops.
//
// NOTE: the task brief suggested passing `nil` for torrents. That is not safe:
// `(*LibtorrentClient).List`/`Stats` dereference the receiver and panic on a nil
// pointer, and both `GET /api/status` and `GET /api/torrents` call them. A
// session-less client is the documented dry-run wiring (`NewLibtorrentClient`
// with `cfg.DryRun == true`) and keeps the state usable, so we use it here.
func newTestAppState(t *testing.T) *AppState {
	t.Helper()
	dir := t.TempDir()

	db, err := OpenDatabase(filepath.Join(dir, "gextto.db"))
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	archive, err := OpenArchive(filepath.Join(dir, "archive.db"))
	if err != nil {
		t.Fatalf("OpenArchive: %v", err)
	}
	comics, err := OpenComicsDb(filepath.Join(dir, "comics.db"))
	if err != nil {
		t.Fatalf("OpenComicsDb: %v", err)
	}
	i18n, err := OpenI18nDb(filepath.Join(dir, "i18n.db"))
	if err != nil {
		t.Fatalf("OpenI18nDb: %v", err)
	}

	engine := NewEngine().WithDB(db)

	cfg := DefaultConfig()
	cfg.DataDir = dir
	cfg.StateDir = filepath.Join(dir, "state")
	cfg.LibtorrentDir = filepath.Join(dir, "downloads")
	libtorrentTemp := filepath.Join(dir, "incomplete")
	cfg.LibtorrentTempDir = &libtorrentTemp
	trash := filepath.Join(dir, "trash")
	cfg.TrashPath = &trash
	cfg.DryRun = true
	cfg.Active = false
	// Write a minimal configuration file pointing at the temporary directory so
	// LatestConfig reloads from an isolated config database instead of falling
	// back to `./data` (which would make the tests depend on the environment).
	configPath := filepath.Join(dir, "gextto.json")
	configJSON, err := json.Marshal(map[string]any{
		"data_dir":            dir,
		"state_dir":           filepath.Join(dir, "state"),
		"libtorrent_dir":      filepath.Join(dir, "downloads"),
		"libtorrent_temp_dir": filepath.Join(dir, "incomplete"),
		"trash_path":          filepath.Join(dir, "trash"),
		"dry_run":             true,
		"active":              false,
	})
	if err != nil {
		t.Fatalf("marshal test config: %v", err)
	}
	if err := os.WriteFile(configPath, configJSON, 0o644); err != nil {
		t.Fatalf("write test config: %v", err)
	}

	torrents := &LibtorrentClient{DryRun: true}

	state := NewAppState(
		&cfg,
		configPath,
		i18n,
		db,
		archive,
		comics,
		engine,
		torrents,
		FromConfig(&cfg),
		NewTmdbClientWithLanguage(nil, "it-IT"),
	)
	if state == nil {
		t.Fatal("NewAppState returned nil")
	}
	return state
}

// webGet performs a GET against the test server and returns status, headers and
// body.
func webGet(t *testing.T, server *httptest.Server, path string) (int, http.Header, []byte) {
	t.Helper()
	response, err := http.Get(server.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("GET %s: read body: %v", path, err)
	}
	return response.StatusCode, response.Header, body
}

func TestBrowserHandlerDownloadsUseGexttoEndpoints(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	tests := []struct {
		file string
		want []string
	}{
		{file: "gextto-magnet", want: []string{"Gextto", "/api/send-magnet"}},
		{file: "gextto-torrent", want: []string{"Gextto", "/api/send-magnet", "/api/upload-torrent"}},
		{file: "gextto-magnet.desktop", want: []string{"Name=Gextto Magnet Handler", "gextto-magnet"}},
		{file: "gextto-torrent.desktop", want: []string{"Name=Gextto Torrent Handler", "gextto-torrent"}},
		{file: "install.sh", want: []string{"Gextto - installazione handler", "/api/status", "gextto-magnet"}},
	}
	for _, test := range tests {
		code, headers, body := webGet(t, server, "/api/browser-handlers/download?file="+url.QueryEscape(test.file))
		if code != http.StatusOK {
			t.Fatalf("download %s -> %d", test.file, code)
		}
		if got := headers.Get("Content-Disposition"); !strings.Contains(got, test.file) {
			t.Fatalf("download %s content disposition = %q", test.file, got)
		}
		text := string(body)
		for _, marker := range test.want {
			if !strings.Contains(text, marker) {
				t.Errorf("download %s missing %q", test.file, marker)
			}
		}
	}
}

// webPostJSON performs a POST with a JSON body and returns status and body.
func webPostJSON(t *testing.T, server *httptest.Server, path, payload string) (int, []byte) {
	t.Helper()
	response, err := http.Post(server.URL+path, "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("POST %s: read body: %v", path, err)
	}
	return response.StatusCode, body
}

func TestWebApiStatus(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	status, header, body := webGet(t, server, "/api/status")
	if status != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", status, body)
	}
	if contentType := header.Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("content-type = %q", contentType)
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode status: %v (%s)", err, body)
	}
	if decoded["name"] != "gextto" {
		t.Fatalf("name = %v", decoded["name"])
	}
	if decoded["dry_run"] != true {
		t.Fatalf("dry_run = %v", decoded["dry_run"])
	}
}

func TestWebApiHealth(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	status, _, body := webGet(t, server, "/api/health")
	if status != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", status, body)
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode health: %v (%s)", err, body)
	}
	if _, ok := decoded["status"]; !ok {
		t.Fatalf("health report missing status: %s", body)
	}
}

func TestWebApiConfigIsObject(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	status, _, body := webGet(t, server, "/api/config")
	if status != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", status, body)
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("config is not a JSON object: %v (%s)", err, body)
	}
}

func TestWebApiSeriesAndMoviesHaveItemsArrays(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	for _, path := range []string{"/api/series", "/api/movies"} {
		status, _, body := webGet(t, server, path)
		if status != http.StatusOK {
			t.Fatalf("GET %s: status = %d, body = %s", path, status, body)
		}
		decoded := map[string]any{}
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatalf("GET %s: decode: %v (%s)", path, err, body)
		}
		items, ok := decoded["items"].([]any)
		if !ok {
			t.Fatalf("GET %s: items is not an array: %s", path, body)
		}
		if len(items) != 0 {
			t.Fatalf("GET %s: expected an empty items array, got %d", path, len(items))
		}
	}
}

func TestWebApiTorrentsIsEmptyArray(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	status, _, body := webGet(t, server, "/api/torrents")
	if status != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", status, body)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("torrents is not a JSON array: %v (%s)", err, body)
	}
	if len(items) != 0 {
		t.Fatalf("expected an empty array, got %d items: %s", len(items), body)
	}
}

func TestWebApiComics(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	status, _, body := webGet(t, server, "/api/comics")
	if status != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", status, body)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("comics is not a JSON array: %v (%s)", err, body)
	}
}

func TestWebApiIndexAndUIAsset(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	status, header, body := webGet(t, server, "/")
	if status != http.StatusOK {
		t.Fatalf("GET /: status = %d", status)
	}
	if contentType := header.Get("Content-Type"); contentType != "text/html; charset=utf-8" {
		t.Fatalf("GET /: content-type = %q", contentType)
	}
	if !strings.Contains(string(body), "<html") {
		t.Fatalf("GET /: body does not contain <html: %s", body)
	}

	status, _, asset := webGet(t, server, "/v2/static/v2-core.js")
	if status != http.StatusOK {
		t.Fatalf("GET /v2/static/v2-core.js: status = %d", status)
	}
	if len(asset) == 0 {
		t.Fatal("GET /v2/static/v2-core.js: empty body")
	}

	for _, path := range []string{"/legacy", "/ui", "/ui/", "/ui/static/gextto-ui.css", "/pkg/ui.js", "/pkg/ui_bg.wasm"} {
		status, _, _ := webGet(t, server, path)
		if status != http.StatusNotFound {
			t.Fatalf("GET %s: status = %d, want %d", path, status, http.StatusNotFound)
		}
	}
}

func TestSeriesSearchMissingRefreshesNewSeriesMetadata(t *testing.T) {
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tv/1399" {
			t.Fatalf("TMDB path = %q, want /tv/1399", r.URL.Path)
		}
		tmdbWriteJSON(t, w, `{"seasons":[{"season_number":1,"episode_count":3}]}`)
	})

	state := newTestAppState(t)
	key := "test-key"
	cfg := *state.cfg
	cfg.TmdbAPIKey = &key
	cfg.Series = []SeriesConfig{{Name: "Star Wars: The Clone Wars", Seasons: "1+", TmdbID: "1399", Enabled: true}}
	state.config_cache = &ConfigCache{
		generation: ConfigGeneration(),
		cfg:        &cfg,
		valid:      true,
	}

	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	path := "/api/series/" + url.PathEscape("Star Wars: The Clone Wars") + "/search-missing"
	status, body := webPostJSON(t, server, path, `{}`)
	if status != http.StatusOK {
		t.Fatalf("POST %s: status = %d, body = %s", path, status, body)
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode response: %v (%s)", err, body)
	}
	if decoded["searched"] != float64(3) {
		t.Fatalf("searched = %v, want 3", decoded["searched"])
	}
	if decoded["metadata_available"] != true {
		t.Fatalf("metadata_available = %v, want true", decoded["metadata_available"])
	}
}

func TestWebApiI18n(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	status, _, body := webGet(t, server, "/api/i18n?lang=it")
	if status != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", status, body)
	}
}

func TestWebApiBackup(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	status, _, body := webGet(t, server, "/api/backup")
	if status != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", status, body)
	}
}

// TestWebApiSaveSettingPersists posts a setting and re-reads it straight from
// the config SQLite database.
//
// The task brief asked for the key "test_setting", but that key is NOT writable
// through this endpoint: both the Go handler and the original validate the
// key against an allow-list and answer 400 "setting is not writable through
// this endpoint". We therefore use a key with an allowed prefix ("notify_") to
// prove the persistence round-trip, and additionally pin the documented
// rejection of an arbitrary key.
func TestWebApiSaveSettingPersists(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	const key = "notify_test_setting"
	status, body := webPostJSON(t, server, "/api/config/settings", `{"key":"`+key+`","value":"1"}`)
	if status != http.StatusOK {
		t.Fatalf("POST settings: status = %d, body = %s", status, body)
	}

	configDB, err := OpenConfigDB(filepath.Join(state.cfg.DataDir, "gextto_config.db"))
	if err != nil {
		t.Fatalf("OpenConfigDB: %v", err)
	}
	defer configDB.Close()

	var value string
	if err := configDB.QueryRow("SELECT value FROM settings WHERE key=?1", key).Scan(&value); err != nil {
		t.Fatalf("setting %q not persisted: %v", key, err)
	}
	if value != "1" {
		t.Fatalf("setting %q = %q, want %q", key, value, "1")
	}

	// Arbitrary keys are rejected by design (documented contract).
	status, body = webPostJSON(t, server, "/api/config/settings", `{"key":"test_setting","value":"1"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("POST disallowed setting: status = %d, body = %s", status, body)
	}
}

func TestWebApiUnknownRoute404(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	status, _, _ := webGet(t, server, "/api/nonexistent-route")
	if status != http.StatusNotFound {
		t.Fatalf("unknown route status = %d, want 404", status)
	}
}
