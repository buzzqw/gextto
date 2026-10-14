package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/buzzqw/gextto/internal/settings"
)

// TestExportDir covers the export_dir/export_dir_fin copies: the .torrent is
// written when the metadata is known and again on completion. The settings store
// is set before the daemon's run loop starts (newTestDaemonFull), so no
// goroutine reads the field concurrently.
func TestExportDir(t *testing.T) {
	store, err := settings.Load(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	exportDir := t.TempDir()
	finDir := t.TempDir()
	store.Set(exportDirKey, exportDir)
	store.Set(exportFinDirKey, finDir)
	d := newTestDaemonFull(t, NetworkOptions{PortBegin: 42100, PortEnd: 42200, Encryption: 1, PEX: true}, store)

	src := t.TempDir()
	data := makeTorrent(t, src, "movie.bin", 40_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hash)

	waitFor(t, "the .torrent exported on add", func() bool {
		_, err := os.Stat(filepath.Join(exportDir, "movie.bin.torrent"))
		return err == nil
	})
	// The payload is already at the destination, so the torrent completes at its
	// first check and the finished copy appears.
	waitFor(t, "the .torrent exported on completion", func() bool {
		_, err := os.Stat(filepath.Join(finDir, "movie.bin.torrent"))
		return err == nil
	})
}
