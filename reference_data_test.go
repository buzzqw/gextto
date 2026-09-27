package gextto

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReferenceDatabaseReadback verifies gextto against a consistent copy of a
// real gextto data directory. It is opt-in: set
//
//	GEXTTO_REFERENCE_DATA_DIR=/path/to/copy
//
// where the copy contains gextto_series.db, gextto_archive.db,
// gextto_config.db and gextto_comics.db (produced with `sqlite3 <db> ".backup"`).
// It only reads; every database is opened read-only.
func TestReferenceDatabaseReadback(t *testing.T) {
	dir := os.Getenv("GEXTTO_REFERENCE_DATA_DIR")
	if dir == "" {
		t.Skip("set GEXTTO_REFERENCE_DATA_DIR to a gextto data copy to run this test")
	}
	seriesPath := filepath.Join(dir, "gextto_series.db")
	archivePath := filepath.Join(dir, "gextto_archive.db")
	if _, err := os.Stat(seriesPath); err != nil {
		t.Skipf("reference databases not found in %s", dir)
	}

	db, err := OpenDatabase(seriesPath)
	if err != nil {
		t.Fatalf("open series db: %v", err)
	}
	defer db.db.Close()
	if rows, err := db.QuickCheck(); err != nil {
		t.Fatalf("quick check: %v", err)
	} else if len(rows) != 1 || rows[0] != "ok" {
		t.Fatalf("quick check rows = %v", rows)
	}

	// The series feed seen tables must be readable through the implemented queries.
	movies, series, err := db.SeenCounts()
	if err != nil {
		t.Fatalf("seen counts: %v", err)
	}
	t.Logf("seen: movies=%d series=%d", movies, series)
	if movies < 0 || series < 0 {
		t.Fatalf("negative seen counts: %d/%d", movies, series)
	}

	// The archive must answer a browse query and a search.
	archive, err := OpenArchive(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer archive.db.Close()
	total, err := archive.Count()
	if err != nil {
		t.Fatalf("archive count: %v", err)
	}
	t.Logf("archive entries: %d", total)
	if total <= 0 {
		t.Fatal("expected a non-empty archive")
	}
	page, err := archive.BrowsePage("", 1, 5)
	if err != nil {
		t.Fatalf("archive browse: %v", err)
	}
	if len(page.Items) == 0 {
		t.Fatal("expected archive items")
	}
}
