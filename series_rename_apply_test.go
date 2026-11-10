package gextto

// series_rename_apply_test.go pins the library rename of one series
// (seriesRenameApply): the preview changes nothing, an execution renames the
// tracked episodes and records their new paths, leaves the right names alone,
// renames and links the files the database does not know, and discards an
// untracked copy worse than the one already there.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type renameEnv struct {
	s       *AppState
	archive string
}

// newRenameEnv configures Series A with renaming on and no metadata key, so
// the episode titles fall back to "Episodio N" without a network call.
func newRenameEnv(t *testing.T) *renameEnv {
	t.Helper()
	s := newTestAppState(t)
	s.config_path = t.TempDir() // latestConfig falls back to s.cfg
	cfg := s.cfg
	cfg.RenameEpisodes = true
	cfg.TmdbAPIKey = nil
	archive := filepath.Join(t.TempDir(), "Series A")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.Series = []SeriesConfig{{Name: "Series A", ArchivePath: archive, Enabled: true}}
	if _, err := s.db.db.Exec("INSERT INTO series(name) VALUES ('Series A')"); err != nil {
		t.Fatal(err)
	}
	return &renameEnv{s: s, archive: archive}
}

// track writes an episode file and records it in the database.
func (e *renameEnv) track(t *testing.T, name string, season, episode int64, title string) string {
	t.Helper()
	path := filepath.Join(e.archive, name)
	writeVideo(t, path, 4000)
	// The import records the score of the release, as a completion does.
	score := e.s.cfg.FileScore(name, "series", "Series A")
	if _, err := e.s.db.db.Exec(`INSERT INTO episodes(series_id,season,episode,title,downloaded_at,archive_path,quality_score)
		SELECT id,?1,?2,?3,'2026-01-01 00:00:00',?4,?5 FROM series WHERE name='Series A'`, season, episode, title, path, score); err != nil {
		t.Fatal(err)
	}
	return path
}

func (e *renameEnv) apply(t *testing.T, execute bool) map[string]any {
	t.Helper()
	status, value := seriesRenameApply(e.s, "Series A", execute, false, false)
	result, ok := value.(map[string]any)
	if status != 200 || !ok {
		t.Fatalf("rename -> %d %#v", status, value)
	}
	return result
}

func (e *renameEnv) storedPath(t *testing.T, season, episode int64) string {
	t.Helper()
	var path string
	if err := e.s.db.db.QueryRow(`SELECT e.archive_path FROM episodes e JOIN series s ON s.id=e.series_id
		WHERE s.name='Series A' AND season=?1 AND episode=?2`, season, episode).Scan(&path); err != nil {
		t.Fatal(err)
	}
	return path
}

// The preview lists the rename and touches nothing; the execution renames the
// file and records the new path; a second execution has nothing to do.
func TestSeriesRenamePreviewThenExecute(t *testing.T) {
	e := newRenameEnv(t)
	original := e.track(t, "Series.A.S01E01.1080p.WEB.H264-GROUP.mkv", 1, 1, "Series.A.S01E01.1080p.WEB.H264-GROUP")

	preview := e.apply(t, false)
	if items, _ := preview["items"].([]any); len(items) != 1 {
		t.Fatalf("preview items = %#v, want one rename", preview["items"])
	}
	if _, err := os.Stat(original); err != nil {
		t.Fatalf("the preview moved the file: %v", err)
	}

	done := e.apply(t, true)
	if got := bgCountKey(done, "renamed_count"); got != 1 {
		t.Fatalf("renamed_count = %d, want 1", got)
	}
	stored := e.storedPath(t, 1, 1)
	if stored == original || !strings.HasPrefix(stored, e.archive) {
		t.Fatalf("stored path = %q, want the new name inside %q", stored, e.archive)
	}
	if _, err := os.Stat(stored); err != nil {
		t.Fatalf("the renamed file is missing: %v", err)
	}
	if _, err := os.Stat(original); err == nil {
		t.Fatal("the old name is still there")
	}

	again := e.apply(t, true)
	if got := bgCountKey(again, "renamed_count"); got != 0 || bgCountKey(again, "already_ok_count") != 1 {
		t.Fatalf("second pass: renamed %d, already ok %d; want 0 and 1", got, bgCountKey(again, "already_ok_count"))
	}
}

// A file the database does not know is renamed and linked, so the next pass
// sees it as tracked.
func TestSeriesRenameLinksAnUntrackedFile(t *testing.T) {
	e := newRenameEnv(t)
	untracked := filepath.Join(e.archive, "Series.A.S01E02.1080p.WEB.H264-GROUP.mkv")
	writeVideo(t, untracked, 4000)

	done := e.apply(t, true)
	if got := bgCountKey(done, "renamed_count"); got != 1 {
		t.Fatalf("renamed_count = %d, want 1: %#v", got, done["items"])
	}
	stored := e.storedPath(t, 1, 2)
	if _, err := os.Stat(stored); err != nil || stored == untracked {
		t.Fatalf("linked path = %q (%v), want the renamed file", stored, err)
	}
	again := e.apply(t, true)
	if got := bgCountKey(again, "renamed_count"); got != 0 {
		t.Fatalf("second pass renamed %d files", got)
	}
}

// An untracked copy worse than the tracked episode is discarded; the tracked
// one stays, even when its renamed name (the default format) no longer says
// its quality.
func TestSeriesRenameDiscardsAWorseUntrackedCopy(t *testing.T) {
	e := newRenameEnv(t)
	e.track(t, "Series.A.S01E03.2160p.BluRay.REMUX.HDR.DTS-HD.MA.5.1-GROUP.mkv", 1, 3, "Series.A.S01E03.2160p.BluRay.REMUX.HDR.DTS-HD.MA.5.1-GROUP")
	e.apply(t, true) // renamed to the default format, quality not in the name
	tracked := e.storedPath(t, 1, 3)
	worse := filepath.Join(e.archive, "Series.A.S01E03.480p.HDTV.x264-OLD.mkv")
	writeVideo(t, worse, 2000)

	done := e.apply(t, true)
	if got := bgCountKey(done, "discarded_count"); got != 1 {
		t.Fatalf("discarded_count = %d, want 1: %#v", got, done["items"])
	}
	if _, err := os.Stat(worse); err == nil {
		t.Fatal("the worse copy is still in the library")
	}
	if info, err := os.Stat(tracked); err != nil || info.Size() < 4000 {
		t.Fatalf("the tracked episode was replaced or left the library: %v", err)
	}
}

// An unknown series and a disabled rename are refused without touching files.
func TestSeriesRenameRefusals(t *testing.T) {
	e := newRenameEnv(t)
	if status, _ := seriesRenameApply(e.s, "Series Z", true, false, false); status != 404 {
		t.Fatalf("unknown series -> %d, want 404", status)
	}
	path := e.track(t, "Series.A.S01E04.1080p.WEB.H264-GROUP.mkv", 1, 4, "Series.A.S01E04.1080p.WEB.H264-GROUP")
	e.s.cfg.RenameEpisodes = false
	if status, _ := seriesRenameApply(e.s, "Series A", true, false, false); status != 409 {
		t.Fatalf("rename disabled -> %d, want 409", status)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("a refused rename moved the file: %v", err)
	}
}
