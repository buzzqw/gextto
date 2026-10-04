package gextto

import (
	"path/filepath"
	"testing"
)

// TestMarkReleaseCompletedCreatesMissingEpisode guards against losing the
// archive registration when the episode row was reset or deleted before the
// download completed: completion must re-create the row.
func TestMarkReleaseCompletedCreatesMissingEpisode(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "series.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.db.Close()

	magnet := "magnet:?xt=urn:btih:0123456789012345678901234567890123456789"
	release := ParseRelease("Show.S01E01.1080p.WEB-DL.ITA", magnet, "test")
	if release == nil {
		t.Fatal("release not parsed")
	}

	// The episode row does not exist yet (deleted/reset by the user).
	if err := db.MarkReleaseCompleted(release, "/nas/Show/Show - S01E01.mkv", 1234); err != nil {
		t.Fatalf("mark completed: %v", err)
	}
	var archivePath string
	var size int64
	if err := db.db.QueryRow("SELECT archive_path, size_bytes FROM episodes WHERE series_id=(SELECT id FROM series WHERE name='Show') AND season=1 AND episode=1").Scan(&archivePath, &size); err != nil {
		t.Fatalf("episode row not created: %v", err)
	}
	if archivePath != "/nas/Show/Show - S01E01.mkv" || size != 1234 {
		t.Fatalf("row = %q size=%d", archivePath, size)
	}

	// An existing title and quality score must be preserved on update.
	if err := db.SyncArchiveFileScored("Show", 1, 2, "Original Title", "/nas/Show/S01E02.mkv", 10, 900); err != nil {
		t.Fatalf("sync: %v", err)
	}
	second := ParseRelease("Show.S01E02.1080p.WEB-DL.ITA", magnet, "test")
	if err := db.MarkReleaseCompleted(second, "/nas/Show/S01E02-new.mkv", 2222); err != nil {
		t.Fatalf("mark completed 2: %v", err)
	}
	var title string
	var score int64
	if err := db.db.QueryRow("SELECT title, quality_score, archive_path FROM episodes WHERE series_id=(SELECT id FROM series WHERE name='Show') AND season=1 AND episode=2").Scan(&title, &score, &archivePath); err != nil {
		t.Fatalf("row 2: %v", err)
	}
	if title != "Original Title" || score != 900 {
		t.Fatalf("existing fields not preserved: title=%q score=%d", title, score)
	}
	if archivePath != "/nas/Show/S01E02-new.mkv" {
		t.Fatalf("archive path not updated: %q", archivePath)
	}
}

func TestMarkReleaseCompletedRecordsEpisodeZeroSpecial(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "series.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.db.Close()
	release := ParseRelease("Example.S02E00.Recap.1080p.WEB-DL.ITA", "magnet:?xt=urn:btih:0123456789012345678901234567890123456789", "test")
	if release == nil || release.IsPack {
		t.Fatalf("release = %#v, want E00 special", release)
	}
	if err := db.MarkReleaseCompleted(release, "/nas/Example/Example - S02E00 - Speciale.mkv", 1234); err != nil {
		t.Fatal(err)
	}
	var archivePath string
	if err := db.db.QueryRow("SELECT archive_path FROM episodes WHERE series_id=(SELECT id FROM series WHERE name='Example') AND season=2 AND episode=0").Scan(&archivePath); err != nil {
		t.Fatalf("special row not created: %v", err)
	}
	if archivePath == "" {
		t.Fatal("completed E00 special has no archive path")
	}
}
