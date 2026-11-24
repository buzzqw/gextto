package gextto

import (
	"path/filepath"
	"testing"
)

func TestPartialLibrarySavesPreserveTheOtherListAndMovieIDs(t *testing.T) {
	dir := t.TempDir()
	if err := SaveLibrary(dir,
		[]SeriesConfig{{Name: "Old Series", Enabled: true}},
		[]MovieConfig{{ID: 7, Name: "Movie A", Year: "2026", Quality: "1080p", Enabled: true}},
	); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errors := make(chan error, 2)
	go func() {
		<-start
		errors <- SaveSeriesLibrary(dir, []SeriesConfig{{Name: "Series A", Enabled: true}})
	}()
	go func() {
		<-start
		errors <- SaveMoviesLibrary(dir, []MovieConfig{{Name: "Movie A", Year: "2026", Quality: "2160p", Enabled: true}})
	}()
	close(start)
	for range 2 {
		if err := <-errors; err != nil {
			t.Fatalf("partial library save: %v", err)
		}
	}

	cfg := DefaultConfig()
	cfg.DataDir = dir
	if err := cfg.loadConfigDB(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Series) != 1 || cfg.Series[0].Name != "Series A" {
		t.Fatalf("series = %+v, want only Series A", cfg.Series)
	}
	if len(cfg.Movies) != 1 || cfg.Movies[0].Name != "Movie A" || cfg.Movies[0].Quality != "2160p" {
		t.Fatalf("movies = %+v, want the independently saved Movie A", cfg.Movies)
	}
	if cfg.Movies[0].ID != 7 {
		t.Fatalf("movie id = %d, want the preserved id 7", cfg.Movies[0].ID)
	}
}

func TestSaveMoviesLibraryFailureRollsBackTheMovieList(t *testing.T) {
	dir := t.TempDir()
	if err := SaveLibrary(dir, nil, []MovieConfig{{ID: 1, Name: "Movie A"}}); err != nil {
		t.Fatal(err)
	}
	// Duplicate explicit primary keys make the insert fail after DELETE. The
	// transaction must restore the previous movie rather than leave an empty list.
	if err := SaveMoviesLibrary(dir, []MovieConfig{{ID: 2, Name: "Movie B"}, {ID: 2, Name: "Movie C"}}); err == nil {
		t.Fatal("duplicate movie ids should fail")
	}
	cfg := DefaultConfig()
	cfg.DataDir = dir
	if err := cfg.loadConfigDB(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Movies) != 1 || cfg.Movies[0].ID != 1 || cfg.Movies[0].Name != "Movie A" {
		t.Fatalf("movies after rollback = %+v, want the original Movie A", cfg.Movies)
	}
}

func TestSaveSeriesLibraryDoesNotRequireAMoviesTable(t *testing.T) {
	dir := t.TempDir()
	// Simulate a database created by the legacy settings writer, before the
	// movies table existed. A series-only save must not depend on that table.
	db, err := OpenConfigDB(filepath.Join(dir, "gextto_config.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE settings(key TEXT PRIMARY KEY, value TEXT NOT NULL)"); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := SaveSeriesLibrary(dir, []SeriesConfig{{Name: "Series A"}}); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.DataDir = dir
	if err := cfg.loadConfigDB(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Series) != 1 || cfg.Series[0].Name != "Series A" {
		t.Fatalf("series = %+v, want Series A", cfg.Series)
	}
}
