package gextto

import (
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TestShouldLogQueueCounts checks the "📊 Downloads" logging rule: the first
// dynamic tick after start is skipped (transient counts) and a line is logged
// only when the triple changes.
func TestShouldLogQueueCounts(t *testing.T) {
	if shouldLogQueueCounts(1, "4/0/4", "") {
		t.Fatal("the first tick (unsynced counts) must not be logged")
	}
	if !shouldLogQueueCounts(2, "2/0/2", "") {
		t.Fatal("the first stable state must be logged")
	}
	if shouldLogQueueCounts(3, "2/0/2", "2/0/2") {
		t.Fatal("an unchanged triple must not be logged again")
	}
	if !shouldLogQueueCounts(4, "1/0/3", "2/0/2") {
		t.Fatal("a changed triple must be logged")
	}
}

func TestCompletedArchivePresentAcceptsPersistedFullPayload(t *testing.T) {
	db, _, view, _, processed := seedTestSetup(t)
	path, present := bg_completedArchivePresent(db, view.Hash, view.TotalSize)
	if !present || path != processed {
		t.Fatalf("archive payload = %q, %v; want %q, true", path, present, processed)
	}
}

func TestCompletedArchivePresentRejectsInsufficientPayload(t *testing.T) {
	db, _, view, _, _ := seedTestSetup(t)
	if _, present := bg_completedArchivePresent(db, view.Hash, view.TotalSize+1); present {
		t.Fatal("undersized archive payload was accepted")
	}
}

func TestHousekeepingDoesNotRerunAfterARestart(t *testing.T) {
	now := time.Now()
	fresh := &Config{Settings: map[string]string{}}
	if got := housekeepingFirstDelay(fresh, now); got != 10*time.Minute {
		t.Fatalf("first run ever: delay %v, want 10m", got)
	}
	recent := &Config{Settings: map[string]string{"housekeeping_last_run": strconv.FormatInt(now.Add(-2*time.Hour).Unix(), 10)}}
	if got := housekeepingFirstDelay(recent, now); got < 21*time.Hour || got > 22*time.Hour {
		t.Fatalf("ran 2h ago with a 24h interval: delay %v, want about 22h", got)
	}
	old := &Config{Settings: map[string]string{"housekeeping_last_run": strconv.FormatInt(now.Add(-48*time.Hour).Unix(), 10)}}
	if got := housekeepingFirstDelay(old, now); got != 10*time.Minute {
		t.Fatalf("overdue: delay %v, want 10m", got)
	}
}

func TestCompactSkipsADatabaseWithoutFreeSpace(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE t(x BLOB)"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		if _, err := db.Exec("INSERT INTO t VALUES (randomblob(4000))"); err != nil {
			t.Fatal(err)
		}
	}
	if connectionWorthCompacting(db) {
		t.Fatal("a database without free pages must not be vacuumed")
	}
	if _, err := db.Exec("DELETE FROM t WHERE rowid > 50"); err != nil {
		t.Fatal(err)
	}
	if !connectionWorthCompacting(db) {
		t.Fatal("a database with most pages free should be compacted")
	}
	if err := OptimizeConnection(db, "compact"); err != nil {
		t.Fatal(err)
	}
	if connectionWorthCompacting(db) {
		t.Fatal("compact did not vacuum")
	}
}

// TestSeriesRenameApplyDoesNotProcessARenamedFileTwice is the regression for
// the library check that renamed Wolf Like Me S01E05/E06 at every pass: the
// tracked paths were collected after the renames from the stale database paths,
// so the archive scan saw the just-renamed file as untracked and renamed it
// again (a no-op counted as a rename).
func TestSeriesRenameApplyDoesNotProcessARenamedFileTwice(t *testing.T) {
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	s := newTestAppState(t)
	// LatestConfig must fall back to s.cfg, where this test adds the series:
	// a directory is not a readable config file, so LoadConfig returns an
	// error instead of the defaults.
	s.config_path = t.TempDir()
	cfg := s.cfg
	cfg.RenameEpisodes = true
	cfg.RenameFormat = "full"
	key := "test-key"
	cfg.TmdbAPIKey = &key

	archiveDir := filepath.Join(t.TempDir(), "Show")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.Series = []SeriesConfig{{Name: "Show", ArchivePath: archiveDir, TmdbID: "1", Enabled: true}}
	source := filepath.Join(archiveDir, "Show.S01E05.mkv")
	if err := os.WriteFile(source, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}

	seriesName := "Show"
	season, episode := int64(1), int64(5)
	title := "Show - S01E05 - Episodio 5 - [1080p][h265][EAC3 5.1][IT+EN]"
	if _, err := s.db.db.Exec("INSERT INTO series(name) VALUES (?1)", seriesName); err != nil {
		t.Fatal(err)
	}
	var seriesID int64
	if err := s.db.db.QueryRow("SELECT id FROM series WHERE name=?1", seriesName).Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,downloaded_at,archive_path) VALUES (?1,?2,?3,?4,'2026-01-01 00:00:00',?5)",
		seriesID, season, episode, title, source); err != nil {
		t.Fatal(err)
	}

	_, value := seriesRenameApply(s, seriesName, true, false, false)
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("result = %#v", value)
	}
	if got := bg_countKey(result, "renamed_count"); got != 1 {
		t.Fatalf("renamed_count = %d, want 1 (the same file was processed twice)", got)
	}
	items, _ := result["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1: %#v", len(items), items)
	}

	// A second pass has nothing left to do: the spelling of the audio label is
	// kept and no probe can move the file again.
	_, value = seriesRenameApply(s, seriesName, true, false, false)
	result, _ = value.(map[string]any)
	if got := bg_countKey(result, "renamed_count"); got != 0 {
		t.Fatalf("second pass renamed_count = %d, want 0", got)
	}
	if items, _ := result["items"].([]any); len(items) != 0 {
		t.Fatalf("second pass items = %d, want 0: %#v", len(items), items)
	}
}
