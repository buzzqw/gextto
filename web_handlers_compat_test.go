package gextto

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddAllFromArchiveRegistersFolders(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	root := t.TempDir()
	for _, name := range []string{"Breaking Bad", "The Wire", "lost+found", ".hidden"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A loose file must be ignored.
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	payload, _ := json.Marshal(map[string]any{"root": root})
	status, body := webPostJSON(t, server, "/api/config/add_all_from_archive", string(payload))
	if status != http.StatusOK {
		t.Fatalf("add_all_from_archive -> %d: %s", status, body)
	}
	var result struct {
		OK    bool  `json:"ok"`
		Added int64 `json:"added"`
		Root  string
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if !result.OK || result.Added != 2 {
		t.Fatalf("added = %d ok=%v, want 2: %s", result.Added, result.OK, body)
	}

	// The series are persisted and carry the folder as archive_path.
	code, _, listing := webGet(t, server, "/api/config/library")
	if code != http.StatusOK {
		t.Fatalf("library -> %d", code)
	}
	for _, name := range []string{"Breaking Bad", "The Wire"} {
		if !strings.Contains(string(listing), name) {
			t.Fatalf("library missing %q: %s", name, listing)
		}
		if !strings.Contains(string(listing), filepath.Join(root, name)) {
			t.Fatalf("library missing archive_path for %q: %s", name, listing)
		}
	}
	if strings.Contains(string(listing), "lost+found") || strings.Contains(string(listing), ".hidden") {
		t.Fatalf("system/hidden folders were registered: %s", listing)
	}

	// A second scan is idempotent: nothing is added twice.
	status, body = webPostJSON(t, server, "/api/config/add_all_from_archive", string(payload))
	if status != http.StatusOK || !strings.Contains(string(body), `"added":0`) {
		t.Fatalf("second scan -> %d: %s", status, body)
	}
}

func TestAddAllFromArchiveKeepsExistingSeries(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	// Pre-existing monitored series must not be duplicated nor overwritten.
	if err := SaveLibraryConfig(state.cfg.DataDir, []SeriesConfig{{
		Name: "Breaking Bad", Seasons: "1-3", Quality: "2160p", Enabled: true,
	}}, nil); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	for _, name := range []string{"breaking bad", "Better Call Saul"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	payload, _ := json.Marshal(map[string]any{"root": root})
	status, body := webPostJSON(t, server, "/api/config/add_all_from_archive", string(payload))
	if status != http.StatusOK {
		t.Fatalf("add_all_from_archive -> %d: %s", status, body)
	}
	if !strings.Contains(string(body), `"added":1`) {
		t.Fatalf("added != 1: %s", body)
	}

	cfg := latestConfig(state)
	found := 0
	for _, series := range cfg.Series {
		if strings.EqualFold(series.Name, "Breaking Bad") {
			found++
			if series.Quality != "2160p" || series.Seasons != "1-3" {
				t.Fatalf("existing series was overwritten: %+v", series)
			}
		}
	}
	if found != 1 {
		t.Fatalf("Breaking Bad appears %d times, want 1", found)
	}
}

func TestAddAllFromArchiveValidation(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	// No root and no archive_root configured.
	if status, _ := webPostJSON(t, server, "/api/config/add_all_from_archive", `{}`); status != http.StatusBadRequest {
		t.Fatalf("missing root -> %d, want 400", status)
	}
	// Non-existent path.
	payload, _ := json.Marshal(map[string]any{"root": "/definitely/not/here"})
	if status, _ := webPostJSON(t, server, "/api/config/add_all_from_archive", string(payload)); status != http.StatusBadRequest {
		t.Fatalf("bad root -> %d, want 400", status)
	}
}

func TestAddAllFromArchiveUsesConfiguredRoot(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Severance"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveSetting(state.cfg.DataDir, "archive_root", root); err != nil {
		t.Fatal(err)
	}
	if status, body := webPostJSON(t, server, "/api/config/add_all_from_archive", `{}`); status != http.StatusOK {
		t.Fatalf("configured root -> %d: %s", status, body)
	}
	code, _, listing := webGet(t, server, "/api/config/library")
	if code != http.StatusOK || !strings.Contains(string(listing), "Severance") {
		t.Fatalf("configured root not scanned: %d %s", code, listing)
	}
}

func TestLtMemSuggestEndpoint(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, _, body := webGet(t, server, "/api/system/lt_mem_suggest")
	if code != http.StatusOK {
		t.Fatalf("lt_mem_suggest -> %d: %s", code, body)
	}
	var result struct {
		OK        bool  `json:"ok"`
		TotalMB   int64 `json:"total_mb"`
		CacheSize int64 `json:"cache_size"`
		QueueMB   int64 `json:"queue_mb"`
		PeerList  int64 `json:"peer_list"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if !result.OK || result.TotalMB < 0 || result.PeerList <= 0 || result.QueueMB <= 0 {
		t.Fatalf("unexpected suggestions: %+v", result)
	}
}

func TestDebugTorrentMatchEndpoint(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	if err := SaveLibraryConfig(state.cfg.DataDir, []SeriesConfig{{
		Name: "Severance", Enabled: true, ArchivePath: "/media/tv/Severance",
	}}, nil); err != nil {
		t.Fatal(err)
	}

	code, _, body := webGet(t, server, "/api/debug/torrent_match?name=Severance.S02E03.2160p.WEB-DL.ITA.ENG.H265-TBK")
	if code != http.StatusOK {
		t.Fatalf("torrent_match -> %d: %s", code, body)
	}
	var result struct {
		OK            bool    `json:"ok"`
		Kind          string  `json:"kind"`
		Series        *string `json:"series"`
		Season        *int64  `json:"season"`
		Episode       *int64  `json:"episode"`
		MatchedSeries *string `json:"matched_series"`
		ArchivePath   string  `json:"archive_path"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if !result.OK || result.MatchedSeries == nil || *result.MatchedSeries != "Severance" {
		t.Fatalf("matched_series = %v: %s", result.MatchedSeries, body)
	}
	if result.Season == nil || *result.Season != 2 || result.Episode == nil || *result.Episode != 3 {
		t.Fatalf("season/episode = %v/%v: %s", result.Season, result.Episode, body)
	}
	if result.ArchivePath != "/media/tv/Severance" {
		t.Fatalf("archive_path = %q", result.ArchivePath)
	}

	// An empty name is a valid diagnostic response, not an error.
	code, _, body = webGet(t, server, "/api/debug/torrent_match")
	if code != http.StatusOK || !strings.Contains(string(body), "NO name provided") {
		t.Fatalf("empty name -> %d: %s", code, body)
	}
}

func TestRamdiskCheckEndpoint(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	code, _, body := webGet(t, server, "/api/ramdisk_check?path="+dir)
	if code != http.StatusOK {
		t.Fatalf("ramdisk_check -> %d: %s", code, body)
	}
	var result struct {
		OK        bool   `json:"ok"`
		Writable  bool   `json:"writable"`
		MountType string `json:"mount_type"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if !result.OK || !result.Writable {
		t.Fatalf("temp dir should be writable: %+v", result)
	}

	// Missing path and no path both return a structured error, never a 500.
	code, _, body = webGet(t, server, "/api/ramdisk_check")
	if code != http.StatusOK || !strings.Contains(string(body), "Percorso non specificato") {
		t.Fatalf("no path -> %d: %s", code, body)
	}
	code, _, body = webGet(t, server, "/api/ramdisk_check?path=/definitely/not/here")
	if code != http.StatusOK || !strings.Contains(string(body), `"ok":false`) {
		t.Fatalf("bad path -> %d: %s", code, body)
	}
}

func TestRamdiskViewReportsConfiguredProblems(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	// Nothing configured: valid but not active.
	code, _, body := webGet(t, server, "/api/ramdisk")
	if code != http.StatusOK {
		t.Fatalf("ramdisk -> %d", code)
	}
	var result struct {
		OK           bool   `json:"ok"`
		Configured   any    `json:"configured"`
		ConfiguredOK bool   `json:"configured_ok"`
		Problem      string `json:"problem"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if !result.OK || result.ConfiguredOK || result.Problem != "" {
		t.Fatalf("unconfigured RAM disk should be ok without a problem: %+v", result)
	}

	// A configured path that no longer exists must be flagged explicitly.
	missing := filepath.Join(t.TempDir(), "gone")
	if err := SaveSetting(state.cfg.DataDir, "libtorrent_ramdisk_dir", missing); err != nil {
		t.Fatal(err)
	}
	code, _, body = webGet(t, server, "/api/ramdisk")
	if code != http.StatusOK {
		t.Fatalf("ramdisk -> %d", code)
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if result.ConfiguredOK || !strings.Contains(result.Problem, "non esiste") {
		t.Fatalf("missing configured RAM disk not flagged: %s", body)
	}
}

func TestLicenseEndpoint(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, _, body := webGet(t, server, "/api/license")
	if code != http.StatusOK {
		t.Fatalf("license -> %d", code)
	}
	if !strings.Contains(string(body), "EUPL") {
		t.Fatalf("license body does not contain the EUPL text: %.120s", body)
	}
}
