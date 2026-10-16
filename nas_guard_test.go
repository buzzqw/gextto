package gextto

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A destination that never answers (hung network share) must be reported as an
// error instead of blocking the torrent worker forever.
func TestValidateDestinationTimesOut(t *testing.T) {
	oldProbe := destinationWritable
	oldTimeout := destinationProbeTimeout
	destinationWritable = func(string) error {
		time.Sleep(time.Minute)
		return nil
	}
	destinationProbeTimeout = 20 * time.Millisecond
	t.Cleanup(func() {
		destinationWritable = oldProbe
		destinationProbeTimeout = oldTimeout
	})

	start := time.Now()
	if err := ValidateDestination(t.TempDir()); err == nil {
		t.Fatal("a destination that never answers must time out")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("ValidateDestination blocked for %s, want a bounded wait", elapsed)
	}
}

func TestValidateDestinationAcceptsWritableDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new", "library")
	if err := ValidateDestination(dir); err != nil {
		t.Fatalf("a writable destination must validate: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("destination was not created: %v", err)
	}
}

func TestArchiveResetBurstIsCapped(t *testing.T) {
	db := newTestDB(t)
	for i := 0; i < archiveResetBurstLimit; i++ {
		if !db.allowArchiveReset() {
			t.Fatalf("reset %d refused before the cap", i+1)
		}
	}
	if db.allowArchiveReset() {
		t.Fatalf("a reset beyond the %d-reset cap was allowed", archiveResetBurstLimit)
	}
}

// A vanished library volume makes every archived file look missing at once.
// Only a bounded burst may be reset; the rest of the rows are preserved.
func TestMassArchiveResetIsBlockedAtTheCap(t *testing.T) {
	db := newTestDB(t)
	// A populated folder proves the volume is mounted, so a vanished file inside
	// it is "confirmed missing" and would normally be reset.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keep.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	total := archiveResetBurstLimit + 3
	for i := int64(1); i <= int64(total); i++ {
		release := testRelease()
		release.Episode = int64Ptr(i)
		release.EpisodeRange = []int64{i}
		release.Title = fmt.Sprintf("Example.S01E%02d.1080p", i)
		if _, _, err := db.CheckSeries(&release); err != nil {
			t.Fatalf("episode %d: create row: %v", i, err)
		}
		missing := filepath.Join(dir, fmt.Sprintf("gone-e%02d.mkv", i))
		if _, err := db.db.Exec("UPDATE episodes SET downloaded_at=?1,archive_path=?2,size_bytes=9 WHERE series_id=(SELECT id FROM series WHERE name='Example') AND season=1 AND episode=?3", nowSQLite(), missing, i); err != nil {
			t.Fatal(err)
		}
		if _, _, err := db.CheckSeries(&release); err != nil {
			t.Fatalf("episode %d: reset check: %v", i, err)
		}
	}

	var reset, preserved int
	if err := db.db.QueryRow("SELECT COUNT(*) FROM episodes WHERE series_id=(SELECT id FROM series WHERE name='Example') AND archive_path IS NULL").Scan(&reset); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRow("SELECT COUNT(*) FROM episodes WHERE series_id=(SELECT id FROM series WHERE name='Example') AND COALESCE(archive_path,'') <> ''").Scan(&preserved); err != nil {
		t.Fatal(err)
	}
	if reset != archiveResetBurstLimit {
		t.Fatalf("reset rows = %d, want %d (the cap)", reset, archiveResetBurstLimit)
	}
	if preserved != total-archiveResetBurstLimit {
		t.Fatalf("preserved rows = %d, want %d", preserved, total-archiveResetBurstLimit)
	}
}
