package gextto

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestBackupPrivacyIsIndependentOfUmask(t *testing.T) {
	previous := syscall.Umask(0)
	defer syscall.Umask(previous)

	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	backupRoot := filepath.Join(root, "backups")
	cloudDir := filepath.Join(root, "cloud", "nested")
	for _, dir := range []string{dataDir, backupRoot, cloudDir} {
		if err := os.MkdirAll(dir, 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o777); err != nil {
			t.Fatal(err)
		}
	}
	writeBackupFile(t, filepath.Join(dataDir, "gextto.json"), "{}\n")

	database, err := OpenSQLite(filepath.Join(dataDir, "gextto_series.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("CREATE TABLE privacy_probe(value TEXT)"); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	archive, err := CreateSnapshot(dataDir, backupRoot, 3)
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	assertOwnerOnlyDirectory(t, backupRoot)
	assertOwnerOnlyFile(t, archive)

	// The database snapshot is private even before it is folded into the zip.
	scratch := filepath.Join(dataDir, ".gextto-backup-privacy-probe")
	if err := backupPrivateDir(scratch); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(scratch)
	snapshot := filepath.Join(scratch, "gextto_series.db")
	if err := snapshotDatabase(filepath.Join(dataDir, "gextto_series.db"), snapshot); err != nil {
		t.Fatal(err)
	}
	assertOwnerOnlyDirectory(t, scratch)
	assertOwnerOnlyFile(t, snapshot)

	copied, err := CopySnapshot(archive, cloudDir)
	if err != nil {
		t.Fatalf("CopySnapshot: %v", err)
	}
	assertOwnerOnlyDirectory(t, cloudDir)
	assertOwnerOnlyFile(t, copied)
	entries, err := os.ReadDir(cloudDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".partial") {
			t.Fatalf("cloud copy left a partial file: %s", entry.Name())
		}
	}
}

func assertOwnerOnlyDirectory(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("directory %s mode = %04o, want 0700", path, got)
	}
}

func assertOwnerOnlyFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("file %s mode = %04o, want 0600", path, got)
	}
}
