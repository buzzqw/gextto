package gextto

import (
	"path/filepath"
	"testing"
)

// TestMigrateLibtorrentKeys pins the settings rename: the generic knobs become
// `torrent_*`, the settings read only by the embedded engine keep their name
// (they go away with it), a value already present under the new name wins, and
// the old rows are removed from the database.
func TestMigrateLibtorrentKeys(t *testing.T) {
	dir := t.TempDir()
	conn, err := OpenConfigDB(filepath.Join(dir, "gextto_config.db"))
	if err != nil {
		t.Fatalf("OpenConfigDB: %v", err)
	}
	defer conn.Close()
	if err := sqlExec(conn, "CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);"); err != nil {
		t.Fatal(err)
	}
	seed := map[string]string{
		"libtorrent_dir":               "/downloads",
		"libtorrent_ramdisk_dir":       "/mnt/ram",
		"libtorrent_dht":               "false",
		"libtorrent_connections_limit": "150",  // still used by gx-torrent: must be renamed
		"libtorrent_enabled":           "true", // engine-only: must stay untouched
		"libtorrent_sequential":        "false",
		"torrent_sequential":           "true", // already renamed: this value wins
	}
	for key, value := range seed {
		if err := sqlExec(conn, "INSERT INTO settings(key,value) VALUES (?1,?2)", key, value); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &Config{Settings: map[string]string{}}
	for key, value := range seed {
		cfg.Settings[key] = value
	}

	if err := migrateLibtorrentKeys(conn, cfg); err != nil {
		t.Fatalf("migrateLibtorrentKeys: %v", err)
	}

	for old, want := range map[string]string{
		"torrent_dir":               "/downloads",
		"torrent_ramdisk_dir":       "/mnt/ram",
		"torrent_dht":               "false",
		"torrent_connections_limit": "150",
	} {
		if got := cfg.Settings[old]; got != want {
			t.Fatalf("%s = %q, want %q", old, got, want)
		}
	}
	if _, ok := cfg.Settings["libtorrent_enabled"]; !ok {
		t.Fatal("the libtorrent-only setting must not be migrated")
	}
	if cfg.Settings["torrent_sequential"] != "true" {
		t.Fatalf("torrent_sequential = %q, want the already-present value to win", cfg.Settings["torrent_sequential"])
	}
	for _, gone := range []string{"libtorrent_dir", "libtorrent_ramdisk_dir", "libtorrent_dht", "libtorrent_sequential", "libtorrent_connections_limit"} {
		if _, ok := cfg.Settings[gone]; ok {
			t.Fatalf("%s must be gone from the settings", gone)
		}
	}

	var value string
	if err := conn.QueryRow("SELECT value FROM settings WHERE key = 'torrent_dir'").Scan(&value); err != nil || value != "/downloads" {
		t.Fatalf("database not updated: value=%q err=%v", value, err)
	}
	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM settings WHERE key = 'libtorrent_dir'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("the old row must be removed: count=%d err=%v", count, err)
	}
	if err := conn.QueryRow("SELECT COUNT(*) FROM settings WHERE key = 'libtorrent_enabled'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("the libtorrent-only row must stay: count=%d err=%v", count, err)
	}
}
