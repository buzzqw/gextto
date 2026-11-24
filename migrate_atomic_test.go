package gextto

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyDatabaseAtomicallyReplacesExistingDestination(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source ?#% ' ü.db")
	destination := filepath.Join(dir, "destination ?#% ' ü.db")
	makeSourceDB(t, source, "new-row")
	makeSourceDB(t, destination, "old-row")

	if err := copyDatabase(source, destination); err != nil {
		t.Fatalf("copyDatabase: %v", err)
	}
	assertNoMigrationStaging(t, dir, destination)
	db, err := OpenSQLite(destination)
	if err != nil {
		t.Fatalf("open destination: %v", err)
	}
	defer db.Close()
	var value string
	if err := db.QueryRow("SELECT value FROM sample").Scan(&value); err != nil || value != "new-row" {
		t.Fatalf("copied row = %q, err=%v; want new-row", value, err)
	}
}

func TestCopyDatabaseFailurePreservesDestinationAndCleansStaging(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "invalid.db")
	destination := filepath.Join(dir, "destination.db")
	if err := os.WriteFile(source, []byte("not a SQLite database"), 0o644); err != nil {
		t.Fatal(err)
	}
	makeSourceDB(t, destination, "old-row")
	before, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}

	if err := copyDatabase(source, destination); err == nil {
		t.Fatal("copying an invalid database should fail")
	}
	after, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("the destination changed after a failed copy")
	}
	assertNoMigrationStaging(t, dir, destination)
}

func TestCopyDatabaseBytesRecoversACopiedWAL(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.db")
	db, err := OpenSQLite(source)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE sample(id INTEGER PRIMARY KEY, value TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO sample(value) VALUES('main-row')"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO sample(value) VALUES('wal-row')"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(source + "-wal"); err != nil {
		t.Fatalf("the source WAL was not kept for the fallback test: %v", err)
	}

	staging := filepath.Join(t.TempDir(), "fallback.db")
	if err := copyDatabaseBytes(source, staging); err != nil {
		t.Fatalf("copyDatabaseBytes: %v", err)
	}
	if err := sealSQLiteCopy(staging); err != nil {
		t.Fatalf("sealSQLiteCopy: %v", err)
	}
	copied, err := OpenSQLite(staging)
	if err != nil {
		t.Fatalf("open fallback copy: %v", err)
	}
	defer copied.Close()
	var count int
	if err := copied.QueryRow("SELECT COUNT(*) FROM sample").Scan(&count); err != nil || count != 2 {
		t.Fatalf("fallback rows = %d, err=%v; want the main row and the WAL row", count, err)
	}
}

func TestMigrateDataDirRejectsDestinationInsideSource(t *testing.T) {
	source := t.TempDir()
	makeSourceDB(t, filepath.Join(source, "legacy_series.db"), "series-row")
	destination := filepath.Join(source, "nested", "data")
	if _, err := MigrateDataDir(source, destination); err == nil {
		t.Fatal("a destination inside the source should be rejected")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("the rejected destination was created: %v", err)
	}
}

func TestCopyDatabaseRejectsTheSameSourceAndDestination(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.db")
	makeSourceDB(t, source, "source-row")
	before, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := copyDatabase(source, source); err == nil {
		t.Fatal("copying a database onto itself should fail")
	}
	after, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("the source changed after a rejected self-copy")
	}
}

func assertNoMigrationStaging(t *testing.T, dir, destination string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "." + filepath.Base(destination) + ".migrate-"
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			t.Fatalf("migration staging entry was left behind: %s", entry.Name())
		}
	}
}
