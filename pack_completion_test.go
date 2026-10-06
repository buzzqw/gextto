package gextto

import (
	"os"
	"path/filepath"
	"testing"
)

// A season pack reaching the generic completion path must record each
// episode with its own file, never with the pack folder.
func TestMarkReleaseCompletedRecordsPackFilesNotFolder(t *testing.T) {
	db := newTestDB(t)
	cfg := &Config{Settings: map[string]string{}}
	release := ParseRelease("Show.S01.1080p.WEB-DL.ITA",
		"magnet:?xt=urn:btih:0123456789012345678901234567890123456789", "test")
	if release == nil || !release.IsPack {
		t.Fatalf("expected a pack release: %+v", release)
	}
	if _, err := db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO series_metadata(series_name,season,episode_count,updated_at) VALUES ('Show',1,2,datetime('now'))"); err != nil {
		t.Fatal(err)
	}
	for _, episode := range []int{0, 1, 2} {
		if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode) SELECT id,1,?1 FROM series WHERE name='Show'", episode); err != nil {
			t.Fatal(err)
		}
	}
	folder := filepath.Join(t.TempDir(), "Show.S01.1080p.WEB-DL.ITA")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Show.S01E01.1080p.mkv", "Show.S01E02.1080p.mkv"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte("video"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := tev_markReleaseCompleted(cfg, db, release, folder, 10); err != nil {
		t.Fatalf("mark completed: %v", err)
	}
	for episode, want := range map[int]string{
		1: filepath.Join(folder, "Show.S01E01.1080p.mkv"),
		2: filepath.Join(folder, "Show.S01E02.1080p.mkv"),
	} {
		var got string
		if err := db.db.QueryRow("SELECT COALESCE(archive_path,'') FROM episodes WHERE season=1 AND episode=?1", episode).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("E%02d archive_path = %q, want %q", episode, got, want)
		}
	}
}
