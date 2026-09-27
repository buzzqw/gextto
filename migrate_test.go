package gextto

import (
	"os"
	"path/filepath"
	"testing"
)

func makeSourceDB(t *testing.T, path string, value string) {
	t.Helper()
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open source db %s: %v", path, err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE sample(id INTEGER PRIMARY KEY, value TEXT)"); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := db.Exec("INSERT INTO sample(value) VALUES(?)", value); err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func TestMigrateDataDirCopiesAndRenames(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()

	makeSourceDB(t, filepath.Join(source, "legacy_series.db"), "series-row")
	makeSourceDB(t, filepath.Join(source, "legacy_archive.db"), "archive-row")
	makeSourceDB(t, filepath.Join(source, "legacy_config.db"), "config-row")
	makeSourceDB(t, filepath.Join(source, "legacy_comics.db"), "comics-row")

	state := filepath.Join(source, "legacy_torrents_state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "abc.fastresume"), []byte("resume"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "legacy.json"), []byte(`{"listen":"0.0.0.0:5000"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := MigrateDataDir(source, target)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, suffix := range []string{"series", "archive", "config", "comics"} {
		destination, ok := report.Databases[suffix]
		if !ok {
			t.Fatalf("database %s not migrated", suffix)
		}
		if _, err := os.Stat(destination); err != nil {
			t.Fatalf("destination %s missing: %v", destination, err)
		}
	}
	// The series copy must be a valid database with the row preserved.
	db, err := OpenSQLite(report.Databases["series"])
	if err != nil {
		t.Fatalf("open migrated series: %v", err)
	}
	defer db.Close()
	var value string
	if err := db.QueryRow("SELECT value FROM sample").Scan(&value); err != nil || value != "series-row" {
		t.Fatalf("row = %q err=%v", value, err)
	}

	if report.TorrentState == "" {
		t.Fatal("torrent state not reported")
	}
	if _, err := os.Stat(filepath.Join(report.TorrentState, "abc.fastresume")); err != nil {
		t.Fatalf("fastresume not copied: %v", err)
	}
	if report.ConfigJSON == "" {
		t.Fatal("config json not reported")
	}
	if _, err := os.Stat(filepath.Join(target, "gextto.json")); err != nil {
		t.Fatalf("gextto.json not created: %v", err)
	}
}

func TestMigrateDataDirGuards(t *testing.T) {
	if _, err := MigrateDataDir("", t.TempDir()); err == nil {
		t.Fatal("empty source should fail")
	}
	dir := t.TempDir()
	if _, err := MigrateDataDir(dir, dir); err == nil {
		t.Fatal("same source and destination should fail")
	}
	if _, err := MigrateDataDir(dir, t.TempDir()); err == nil {
		t.Fatal("directory without databases should fail")
	}
}

func TestParseMigrateCommand(t *testing.T) {
	command := Parse([]string{"migrate", "--from", "/old", "--to", "/new"})
	if command.Kind != CommandMigrate || command.From != "/old" || command.To != "/new" {
		t.Fatalf("parsed = %+v", command)
	}
	command = Parse([]string{"migrate", "--from=/old2", "--to=/new2"})
	if command.From != "/old2" || command.To != "/new2" {
		t.Fatalf("parsed = %+v", command)
	}
}
