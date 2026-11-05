package gextto

import (
	"encoding/json"
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
