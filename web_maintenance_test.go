package gextto

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRestoreSourceNeverOverwritesALibraryFile: rename(2) replaces the
// target silently, so a file already holding the restored name is reported
// and left alone.
func TestRestoreSourceNeverOverwritesALibraryFile(t *testing.T) {
	state := newTestAppState(t)
	library := filepath.Join(state.cfg.DataDir, "library")
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{{Name: "Show", Enabled: true, Seasons: "1+", ArchivePath: library}}, nil); err != nil {
		t.Fatal(err)
	}
	TouchConfigGeneration()
	current := filepath.Join(library, "Show - S01E01 - Titolo - [1080p][h264][AAC][IT].mkv")
	existing := filepath.Join(library, "Show - S01E01 - Titolo - [WEB-DL][1080p][h264][AAC][IT].mkv")
	if err := os.MkdirAll(library, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{current, existing} {
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	archiveTestEpisode(t, state.db, "Show", 1, 1, current)
	rec := httptest.NewRecorder()
	RestoreSource(rec, httptest.NewRequest("POST", "/api/maintenance/restore-source", strings.NewReader(`{}`)), state)
	if rec.Code != 200 {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Count  int `json:"count"`
		Errors int `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Count != 0 || out.Errors != 1 {
		t.Fatalf("restore preview = %+v, want the collision reported", out)
	}
	if data, _ := os.ReadFile(existing); string(data) != filepath.Base(existing) {
		t.Fatal("the existing library file was touched")
	}
}

// TestBatchDownloadAcceptsTorrentOnlyListing covers the indexarr rows that
// carry only a `.torrent` link: the batch endpoint used the legacy add path,
// which read the magnet field alone and refused the release with «invalid
// magnet hash» even though the listing parsed fine and its link was usable.
func TestBatchDownloadAcceptsTorrentOnlyListing(t *testing.T) {
	magnet := "magnet:?xt=urn:btih:" + strings.Repeat("a", 40) + "&dn=Show+S01E01"
	link := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, magnet, http.StatusFound)
	}))
	defer link.Close()

	state := newCycleState(t)
	if err := CompleteSetup(state.cfg); err != nil {
		t.Fatalf("complete setup: %v", err)
	}
	server := httptest.NewServer(Router(state))
	defer server.Close()

	payload := fmt.Sprintf(`{"items":[{"title":"Show S01E01 1080p ITA","source":"indexarr","magnet":%q}]}`, link.URL+"/dl/1.torrent")
	status, body := webPostJSON(t, server, "/api/archive/batch-download", payload)
	if status != http.StatusOK || !strings.Contains(string(body), `"accepted":1`) {
		t.Fatalf("batch download -> %d: %s", status, body)
	}
	if _, err := state.db.TorrentMeta(strings.Repeat("a", 40)); err != nil {
		t.Fatalf("torrent not registered: %v", err)
	}
}
