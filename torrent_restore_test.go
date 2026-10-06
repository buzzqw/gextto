package gextto

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMissingActiveTorrentsListsOnlyLostActiveDownloads(t *testing.T) {
	db := newTestDB(t)
	lost := testRelease()
	lost.Magnet = "magnet:?xt=urn:btih:0123456789012345678901234567890123456789&dn=Example.S01E01.1080p"
	if err := db.RegisterTorrent(&lost); err != nil {
		t.Fatal(err)
	}
	live := testRelease()
	live.Magnet = "magnet:?xt=urn:btih:abcdefabcdefabcdefabcdefabcdefabcdefabcd&dn=Live"
	if err := db.RegisterTorrent(&live); err != nil {
		t.Fatal(err)
	}
	done := testRelease()
	done.Magnet = "magnet:?xt=urn:btih:fedcba9876543210fedcba9876543210fedcba98&dn=Done"
	if err := db.RegisterTorrent(&done); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("UPDATE torrent_meta SET status='completed' WHERE hash='fedcba9876543210fedcba9876543210fedcba98'"); err != nil {
		t.Fatal(err)
	}
	missing, err := db.missingActiveTorrents(map[string]struct{}{"abcdefabcdefabcdefabcdefabcdefabcdefabcd": {}})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0].Hash != "0123456789012345678901234567890123456789" {
		t.Fatalf("missing = %+v, want only the lost active download", missing)
	}
	if got := magnetDisplayName(missing[0].Release.Magnet); got != "Example.S01E01.1080p" {
		t.Fatalf("display name = %q", got)
	}
}

func TestExistingDataFolderFindsThePreviousDownload(t *testing.T) {
	empty := t.TempDir()
	withData := t.TempDir()
	if err := os.Mkdir(filepath.Join(withData, "Show.S01.1080p"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := existingDataFolder("Show.S01.1080p", []string{empty, withData}); got != withData {
		t.Fatalf("folder = %q, want %q", got, withData)
	}
	if got := existingDataFolder("Other", []string{empty, withData}); got != "" {
		t.Fatalf("folder = %q, want none", got)
	}
	// A crafted name must never escape the candidate folders.
	if got := existingDataFolder("../etc", []string{withData}); got != "" {
		t.Fatalf("path traversal accepted: %q", got)
	}
}
