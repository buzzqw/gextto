package gextto

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func writeBackupFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// readBackupZip returns every entry of a zip archive keyed by name.
func readBackupZip(t *testing.T, path string) map[string][]byte {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open zip %s: %v", path, err)
	}
	defer reader.Close()
	files := map[string][]byte{}
	for _, file := range reader.File {
		entry, err := file.Open()
		if err != nil {
			t.Fatalf("open zip entry %s: %v", file.Name, err)
		}
		data, err := io.ReadAll(entry)
		_ = entry.Close()
		if err != nil {
			t.Fatalf("read zip entry %s: %v", file.Name, err)
		}
		files[file.Name] = data
	}
	return files
}

func TestBackupCreateSnapshotIncludesDatabasesAndExcludesContent(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	backupRoot := filepath.Join(root, "backups")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}

	// A live series database, a config database and a comics database.
	series, err := OpenDatabase(filepath.Join(dataDir, "gextto_series.db"))
	if err != nil {
		t.Fatalf("open series db: %v", err)
	}
	defer series.db.Close()
	if _, err := series.db.Exec("INSERT INTO series(name,seasons) VALUES ('Alpha','1+')"); err != nil {
		t.Fatalf("insert series: %v", err)
	}

	i18n, err := OpenI18nDb(filepath.Join(dataDir, "gextto_config.db"))
	if err != nil {
		t.Fatalf("open config db: %v", err)
	}
	defer i18n.db.Close()
	if err := i18n.SetLanguage("en"); err != nil {
		t.Fatalf("set language: %v", err)
	}
	if err := i18n.Set("en", "dashboard.title", "Dashboard"); err != nil {
		t.Fatalf("set translation: %v", err)
	}

	comics, err := OpenComicsDb(filepath.Join(dataDir, "gextto_comics.db"))
	if err != nil {
		t.Fatalf("open comics db: %v", err)
	}
	defer comics.db.Close()

	// Configuration and content/caches.
	writeBackupFile(t, filepath.Join(dataDir, "gextto.json"), "{}\n")
	writeBackupFile(t, filepath.Join(dataDir, "custom", "extra.txt"), "extra\n")
	writeBackupFile(t, filepath.Join(dataDir, "gextto_torrents_state", "abc.fastresume"), "state")
	writeBackupFile(t, filepath.Join(dataDir, "ipfilter.dat"), "blocklist")
	writeBackupFile(t, filepath.Join(dataDir, "gextto_magnet_cache.json"), "{}")
	writeBackupFile(t, filepath.Join(dataDir, "gextto_magnet_feed.xml"), "<rss/>")
	writeBackupFile(t, filepath.Join(dataDir, "gextto.log"), "log line\n")
	writeBackupFile(t, filepath.Join(dataDir, "gextto.log.1"), "old log line\n")

	archive, err := CreateSnapshot(dataDir, backupRoot, 3)
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	if !strings.HasPrefix(filepath.Base(archive), "gextto-backup-") || filepath.Ext(archive) != ".zip" {
		t.Fatalf("unexpected archive name %q", archive)
	}
	// The name is the readable timestamp only (a numeric suffix appears just on
	// a same-second collision), no random hash suffix.
	validName := regexp.MustCompile(`^gextto-backup-\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}(-\d+)?\.zip$`)
	if !validName.MatchString(filepath.Base(archive)) {
		t.Fatalf("archive name not readable/simple: %q", filepath.Base(archive))
	}
	if _, err := os.Stat(archive); err != nil {
		t.Fatalf("archive not created: %v", err)
	}

	files := readBackupZip(t, archive)
	for _, required := range []string{"gextto_series.db", "gextto_config.db", "gextto_comics.db", "gextto.json", "custom/extra.txt"} {
		if _, ok := files[required]; !ok {
			t.Errorf("archive is missing %q; entries: %v", required, backupZipNames(files))
		}
	}
	for _, excluded := range []string{
		"ipfilter.dat",
		"gextto_magnet_cache.json",
		"gextto_magnet_feed.xml",
		"gextto_torrents_state/abc.fastresume",
		"gextto.log",
		"gextto.log.1",
	} {
		if _, ok := files[excluded]; ok {
			t.Errorf("archive must not contain %q; entries: %v", excluded, backupZipNames(files))
		}
	}
	for name := range files {
		if strings.HasSuffix(name, "-wal") || strings.HasSuffix(name, "-shm") ||
			strings.HasSuffix(name, ".log") || strings.Contains(name, ".log.") {
			t.Errorf("archive contains transient artifact %q", name)
		}
		if name == "gextto_torrents_state" || strings.HasPrefix(name, "gextto_torrents_state/") {
			t.Errorf("archive contains excluded directory %q", name)
		}
	}

	// The series snapshot must be a self-contained, consistent database with the
	// committed row and no -wal sidecar.
	restored := filepath.Join(root, "restored.db")
	if err := os.WriteFile(restored, files["gextto_series.db"], 0o644); err != nil {
		t.Fatalf("write restored db: %v", err)
	}
	restoredDB, err := OpenSQLite(restored)
	if err != nil {
		t.Fatalf("open restored db: %v", err)
	}
	defer restoredDB.Close()
	check, err := QuickCheck(restoredDB)
	if err != nil {
		t.Fatalf("restored quick check: %v", err)
	}
	if len(check) != 1 || check[0] != "ok" {
		t.Fatalf("restored quick check = %v, want [ok]", check)
	}
	var name string
	if err := restoredDB.QueryRow("SELECT name FROM series LIMIT 1").Scan(&name); err != nil {
		t.Fatalf("read restored series: %v", err)
	}
	if name != "Alpha" {
		t.Errorf("restored series name = %q, want Alpha", name)
	}

	// The temporary snapshot directory must be cleaned up.
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatalf("read data dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".gextto-backup-") {
			t.Errorf("temporary snapshot folder left behind: %q", entry.Name())
		}
	}
}

func TestBackupRetentionKeepsNewestN(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	backupRoot := filepath.Join(root, "backups")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	if err := os.MkdirAll(backupRoot, 0o755); err != nil {
		t.Fatalf("mkdir backups: %v", err)
	}
	writeBackupFile(t, filepath.Join(dataDir, "gextto.json"), "{}\n")

	now := time.Now()
	old := []string{
		"gextto-backup-2020-01-01_00-00-01.zip",
		"gextto-backup-2020-01-01_00-00-02.zip",
		"gextto-backup-2020-01-01_00-00-03.zip",
	}
	for index, name := range old {
		path := filepath.Join(backupRoot, name)
		if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
			t.Fatalf("write old backup: %v", err)
		}
		mod := now.Add(-time.Duration(len(old)-index) * time.Hour)
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}

	created, err := CreateSnapshot(dataDir, backupRoot, 2)
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	zips := backupZipFiles(t, backupRoot)
	if len(zips) != 2 {
		t.Fatalf("retention kept %d archives, want 2: %v", len(zips), zips)
	}
	if _, ok := zips[filepath.Base(created)]; !ok {
		t.Errorf("newest snapshot %q was pruned: %v", filepath.Base(created), zips)
	}
	if _, ok := zips[old[2]]; !ok {
		t.Errorf("newest pre-existing snapshot %q should be kept: %v", old[2], zips)
	}
	for _, name := range old[:2] {
		if _, ok := zips[name]; ok {
			t.Errorf("old snapshot %q should have been pruned: %v", name, zips)
		}
	}

	// retain < 1 is clamped to one archive.
	singleRoot := t.TempDir()
	singleData := filepath.Join(singleRoot, "data")
	singleBackups := filepath.Join(singleRoot, "backups")
	if err := os.MkdirAll(singleData, 0o755); err != nil {
		t.Fatalf("mkdir single data: %v", err)
	}
	if err := os.MkdirAll(singleBackups, 0o755); err != nil {
		t.Fatalf("mkdir single backups: %v", err)
	}
	writeBackupFile(t, filepath.Join(singleData, "gextto.json"), "{}\n")
	writeBackupFile(t, filepath.Join(singleBackups, "gextto-backup-2020-01-01_00-00-01.zip"), "old")
	createdSingle, err := CreateSnapshot(singleData, singleBackups, 0)
	if err != nil {
		t.Fatalf("CreateSnapshot retain 0: %v", err)
	}
	singleZips := backupZipFiles(t, singleBackups)
	if len(singleZips) != 1 {
		t.Fatalf("retain 0 kept %d archives, want 1: %v", len(singleZips), singleZips)
	}
	if _, ok := singleZips[filepath.Base(createdSingle)]; !ok {
		t.Errorf("retain 0 kept the wrong archive: %v", singleZips)
	}
}

func TestBackupCopySnapshotCopiesArchive(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "gextto-backup-2026-01-01_00-00-00.zip")
	payload := []byte("snapshot payload")
	if err := os.WriteFile(source, payload, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	targetDir := filepath.Join(root, "cloud", "nested")
	copied, err := CopySnapshot(source, targetDir)
	if err != nil {
		t.Fatalf("CopySnapshot: %v", err)
	}
	want := filepath.Join(targetDir, filepath.Base(source))
	if copied != want {
		t.Errorf("copied path = %q, want %q", copied, want)
	}
	got, err := os.ReadFile(copied)
	if err != nil {
		t.Fatalf("read copy: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("copied content = %q, want %q", got, payload)
	}

	if _, err := CopySnapshot("", targetDir); err == nil {
		t.Error("CopySnapshot with empty source should fail")
	}
}

func TestBackupFtpEndpointDefaultsToPort21(t *testing.T) {
	cases := []struct {
		host string
		want string
	}{
		{"10.0.0.5", "10.0.0.5:21"},
		{"ftp.example.com", "ftp.example.com:21"},
		{"ftp.example.com:2121", "ftp.example.com:2121"},
		{" 10.0.0.5 ", "10.0.0.5:21"},
		{"[::1]:21", "[::1]:21"},
		{"[::1]", "[::1]:21"},
		{"", ""},
	}
	for _, testCase := range cases {
		if got := ftpEndpoint(testCase.host); got != testCase.want {
			t.Errorf("ftpEndpoint(%q) = %q, want %q", testCase.host, got, testCase.want)
		}
	}
}

// backupZipNames returns the sorted names of the archive entries.
func backupZipNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	return names
}

// backupZipFiles returns the set of regular .zip files in a directory.
func backupZipFiles(t *testing.T, dir string) map[string]struct{} {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read backup dir: %v", err)
	}
	files := map[string]struct{}{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".zip" {
			continue
		}
		files[entry.Name()] = struct{}{}
	}
	return files
}

// TestBackupSnapshotsInSameSecondGetDistinctNames covers the collision bug: two
// backups started in the same wall-clock second must not write the same file.
func TestBackupSnapshotsInSameSecondGetDistinctNames(t *testing.T) {
	dataDir := t.TempDir()
	backupRoot := filepath.Join(t.TempDir(), "backups")
	writeBackupFile(t, filepath.Join(dataDir, "gextto.json"), "{}\n")

	first, err := CreateSnapshot(dataDir, backupRoot, 10)
	if err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	second, err := CreateSnapshot(dataDir, backupRoot, 10)
	if err != nil {
		t.Fatalf("second snapshot: %v", err)
	}
	if first == second {
		t.Fatalf("two snapshots used the same name %q", first)
	}
	// Both archives must be present and valid.
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("first archive missing: %v", err)
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatalf("second archive missing: %v", err)
	}
	readBackupZip(t, first)
	readBackupZip(t, second)

	// No published .tmp leftovers.
	entries, err := os.ReadDir(backupRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("leftover temporary archive: %s", entry.Name())
		}
	}
}

// TestBackupLeavesNoTempDirectoryOnSuccess ensures the scratch folder is always
// removed (it used to be removed only on the happy path, at the very end).
func TestBackupLeavesNoTempDirectoryOnSuccess(t *testing.T) {
	dataDir := t.TempDir()
	backupRoot := filepath.Join(t.TempDir(), "backups")
	writeBackupFile(t, filepath.Join(dataDir, "gextto.json"), "{}\n")
	if _, err := CreateSnapshot(dataDir, backupRoot, 5); err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".gextto-backup-") {
			t.Fatalf("leftover scratch folder: %s", entry.Name())
		}
	}
}

// TestBackupConcurrentSnapshotsAreSafe runs two snapshots at once: both must
// publish only complete archives and leave no scratch folder behind.
func TestBackupConcurrentSnapshotsAreSafe(t *testing.T) {
	dataDir := t.TempDir()
	backupRoot := filepath.Join(t.TempDir(), "backups")
	writeBackupFile(t, filepath.Join(dataDir, "gextto.json"), "{}\n")

	var wg sync.WaitGroup
	results := make([]string, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = CreateSnapshot(dataDir, backupRoot, 10)
		}(i)
	}
	wg.Wait()

	successes := 0
	for i := 0; i < 2; i++ {
		if errs[i] == nil {
			successes++
			readBackupZip(t, results[i])
			continue
		}
		if !strings.Contains(errs[i].Error(), "in corso") {
			t.Fatalf("unexpected concurrent error: %v", errs[i])
		}
	}
	if successes == 0 {
		t.Fatal("no snapshot succeeded")
	}
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".gextto-backup-") {
			t.Fatalf("leftover scratch folder: %s", entry.Name())
		}
	}
}
