package gextto

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// watcherSettingValue reads one raw value from the config settings table.
func watcherSettingValue(t *testing.T, dataDir, key string) string {
	t.Helper()
	conn, err := OpenConfigDB(filepath.Join(dataDir, "gextto_config.db"))
	if err != nil {
		t.Fatalf("OpenConfigDB: %v", err)
	}
	defer conn.Close()
	var value string
	if err := conn.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&value); err != nil {
		t.Fatalf("settings[%q]: %v", key, err)
	}
	return value
}

// TestWatchedFoldersRoundTrip persists and reloads the watched folder list
// through the `watched_folders` settings key in a temp data directory.
func TestWatcherFoldersRoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	folders := []WatchedFolder{
		{Path: "/media/watch/in", Enabled: true, Recursive: true, DeleteAfter: false},
		{Path: "/media/watch/out", Enabled: false, Recursive: false, DeleteAfter: true},
	}
	if err := SaveWatchedFolders(dataDir, folders); err != nil {
		t.Fatalf("SaveWatchedFolders: %v", err)
	}

	raw := watcherSettingValue(t, dataDir, "watched_folders")
	if !strings.HasPrefix(strings.TrimSpace(raw), "[") {
		t.Fatalf("watched_folders should be a JSON array, got %q", raw)
	}

	got := LoadWatchedFolders(map[string]string{"watched_folders": raw})
	if !reflect.DeepEqual(got, folders) {
		t.Fatalf("round trip = %#v, want %#v", got, folders)
	}
}

// TestWatchedFoldersSaveNilWritesEmptyArray checks nil is persisted as `[]`.
func TestWatcherFoldersSaveNilWritesEmptyArray(t *testing.T) {
	dataDir := t.TempDir()
	if err := SaveWatchedFolders(dataDir, nil); err != nil {
		t.Fatalf("SaveWatchedFolders: %v", err)
	}
	raw := watcherSettingValue(t, dataDir, "watched_folders")
	if strings.TrimSpace(raw) != "[]" {
		t.Fatalf("nil folders should serialize to [], got %q", raw)
	}
}

// TestWatchedFolderDefaults verifies the JSON defaults: enabled and
// delete_after default to true when absent.
func TestWatcherFolderDefaults(t *testing.T) {
	if got := LoadWatchedFolders(map[string]string{}); got != nil {
		t.Fatalf("missing key should yield nil, got %#v", got)
	}
	if got := LoadWatchedFolders(map[string]string{"watched_folders": "{"}); got != nil {
		t.Fatalf("bad JSON should yield nil, got %#v", got)
	}

	folders := LoadWatchedFolders(map[string]string{
		"watched_folders": `[{"path":"/x"},{"path":"/y","enabled":false,"recursive":true,"delete_after":false}]`,
	})
	if len(folders) != 2 {
		t.Fatalf("folders = %#v", folders)
	}
	if !folders[0].Enabled || !folders[0].DeleteAfter || folders[0].Recursive {
		t.Fatalf("defaults not applied: %#v", folders[0])
	}
	if folders[1].Enabled || folders[1].DeleteAfter || !folders[1].Recursive {
		t.Fatalf("explicit values not honored: %#v", folders[1])
	}
}

// TestValidateWatchedFolders covers the validation limits.
func TestWatcherValidateFolders(t *testing.T) {
	if got := ValidateWatchedFolders(nil); got != "" {
		t.Fatalf("empty list should be valid, got %q", got)
	}
	if got := ValidateWatchedFolders([]WatchedFolder{{Path: "/media"}}); got != "" {
		t.Fatalf("valid list rejected: %q", got)
	}
	if got := ValidateWatchedFolders([]WatchedFolder{{Path: "   "}}); got != "a watched folder has an empty or oversized path" {
		t.Fatalf("blank path = %q", got)
	}
	if got := ValidateWatchedFolders([]WatchedFolder{{Path: strings.Repeat("p", 4097)}}); got != "a watched folder has an empty or oversized path" {
		t.Fatalf("oversized path = %q", got)
	}
	many := make([]WatchedFolder, 51)
	for index := range many {
		many[index] = WatchedFolder{Path: "/media"}
	}
	if got := ValidateWatchedFolders(many); got != "too many watched folders (max 50)" {
		t.Fatalf("too many = %q", got)
	}
}

// TestScanFolderFlatAndRecursiveAndNoise ports the
// `scans_only_torrent_and_magnet_files_without_noise` test.
func TestWatcherScanFolderFlatAndRecursiveAndNoise(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.torrent"), "d4:infod4:name3:fooee")
	mustWriteFile(t, filepath.Join(root, "b.magnet"), "magnet:?xt=urn:btih:abc")
	mustWriteFile(t, filepath.Join(root, "c.part"), "partial")
	mustWriteFile(t, filepath.Join(root, "d.tmp"), "partial")
	mustWriteFile(t, filepath.Join(root, ".hidden.torrent"), "hidden")
	mustWriteFile(t, filepath.Join(root, "notes.txt"), "nope")
	nested := filepath.Join(root, "sub")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	mustWriteFile(t, filepath.Join(nested, "e.torrent"), "d4:infod4:name3:baree")

	flat := WatchedFolder{Path: root, Enabled: true, Recursive: false, DeleteAfter: true}
	found := ScanFolder(flat)
	if len(found) != 2 {
		t.Fatalf("flat scan = %#v, want 2 entries", found)
	}
	// The result is sorted, so `a.torrent` precedes `b.magnet`.
	if filepath.Base(found[0]) != "a.torrent" || filepath.Base(found[1]) != "b.magnet" {
		t.Fatalf("flat scan order = %#v", found)
	}
	for _, path := range found {
		name := filepath.Base(path)
		if name == "c.part" || name == "d.tmp" || strings.HasPrefix(name, ".") {
			t.Fatalf("noise file leaked into the scan: %s", name)
		}
	}

	deep := flat
	deep.Recursive = true
	if got := ScanFolder(deep); len(got) != 3 {
		t.Fatalf("recursive scan = %#v, want 3 entries", got)
	}
}

// TestScanFolderMissingOrEmpty ports the
// `disabled_or_missing_folders_yield_nothing` test.
func TestWatcherScanFolderMissingOrEmpty(t *testing.T) {
	missing := WatchedFolder{Path: "/definitely/not/here", Enabled: true}
	if got := ScanFolder(missing); len(got) != 0 {
		t.Fatalf("missing folder = %#v, want empty", got)
	}
	if got := ScanFolder(WatchedFolder{}); len(got) != 0 {
		t.Fatalf("empty folder = %#v, want empty", got)
	}
	// A regular file is not a directory and yields nothing.
	file := filepath.Join(t.TempDir(), "file.torrent")
	mustWriteFile(t, file, "x")
	if got := ScanFolder(WatchedFolder{Path: file}); len(got) != 0 {
		t.Fatalf("file as folder = %#v, want empty", got)
	}
}

// TestWatchedFolderIsCandidate documents the candidate filter.
func TestWatcherIsCandidate(t *testing.T) {
	cases := map[string]bool{
		"a.torrent":       true,
		"a.TORRENT":       true,
		"a.magnet":        true,
		"a.MAGNET":        true,
		"a.part":          false,
		"a.torrent.part":  false,
		"a.torrent.tmp":   false,
		".hidden.torrent": false,
		"notes.txt":       false,
		"torrent":         false,
	}
	for name, want := range cases {
		if got := watchedCandidate(filepath.Join("/tmp", name)); got != want {
			t.Fatalf("watchedCandidate(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestMagnetFromFileExtractsFirstMagnet ports the
// `reads_magnet_uri_from_file` test.
func TestWatcherMagnetFromFileExtractsFirstMagnet(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "release.magnet")
	mustWriteFile(t, file, "Download it here:\nmagnet:?xt=urn:btih:deadbeef&dn=Test\n")
	got := MagnetFromFile(file)
	if got == nil || *got != "magnet:?xt=urn:btih:deadbeef&dn=Test" {
		t.Fatalf("MagnetFromFile = %v", got)
	}

	empty := filepath.Join(root, "empty.magnet")
	mustWriteFile(t, empty, "no magnet here")
	if got := MagnetFromFile(empty); got != nil {
		t.Fatalf("MagnetFromFile(no magnet) = %v, want nil", got)
	}

	missing := filepath.Join(root, "does-not-exist.magnet")
	if got := MagnetFromFile(missing); got != nil {
		t.Fatalf("MagnetFromFile(missing) = %v, want nil", got)
	}

	quoted := filepath.Join(root, "quoted.magnet")
	mustWriteFile(t, quoted, `magnet:?xt=urn:btih:cafe" trailing`)
	if got := MagnetFromFile(quoted); got == nil || *got != "magnet:?xt=urn:btih:cafe" {
		t.Fatalf("MagnetFromFile(quoted) = %v", got)
	}
}

// TestConsumeDeletesOrMarksFiles ports the
// `consume_deletes_or_marks_files` test.
func TestWatcherConsumeDeletesOrMarksFiles(t *testing.T) {
	root := t.TempDir()

	deleted := filepath.Join(root, "one.torrent")
	mustWriteFile(t, deleted, "x")
	if err := Consume(deleted, true); err != nil {
		t.Fatalf("Consume(delete): %v", err)
	}
	if _, err := os.Stat(deleted); !os.IsNotExist(err) {
		t.Fatalf("deleted file still exists: %v", err)
	}

	kept := filepath.Join(root, "two.torrent")
	mustWriteFile(t, kept, "x")
	if err := Consume(kept, false); err != nil {
		t.Fatalf("Consume(keep): %v", err)
	}
	if _, err := os.Stat(kept); !os.IsNotExist(err) {
		t.Fatalf("kept source still exists: %v", err)
	}
	imported := filepath.Join(root, "two.torrent.imported")
	if _, err := os.Stat(imported); err != nil {
		t.Fatalf("expected %s: %v", imported, err)
	}

	extensionless := filepath.Join(root, "three")
	mustWriteFile(t, extensionless, "x")
	if err := Consume(extensionless, false); err != nil {
		t.Fatalf("Consume(extensionless): %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "three.imported")); err != nil {
		t.Fatalf("expected three.imported: %v", err)
	}
}

// mustWriteFile writes data to path, failing the test on error.
func mustWriteFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestScanFolderStopsAtMaxDepth(t *testing.T) {
	root := t.TempDir()
	dir := root
	for i := 0; i <= watchedMaxDepth+1; i++ {
		dir = filepath.Join(dir, fmt.Sprintf("d%d", i))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "deep.torrent"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "d0", "shallow.torrent"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ScanFolder(WatchedFolder{Path: root, Enabled: true, Recursive: true})
	if len(got) != 1 || filepath.Base(got[0]) != "shallow.torrent" {
		t.Fatalf("ScanFolder = %v, want only shallow.torrent", got)
	}
}
