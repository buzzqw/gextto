package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/settings"
)

func savePathOf(t *testing.T, d *Daemon, hash string) string {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	_, meta := d.findLocked(hash)
	if meta == nil {
		t.Fatalf("meta %s not found", hash)
	}
	return meta.SavePath
}

// TestAutoTMM covers automatic torrent management: a managed torrent follows its
// category's save path (on enable, on category change and on path change).
func TestAutoTMM(t *testing.T) {
	d := newTestDaemon(t)
	catPath := t.TempDir()
	d.createCategory("movies", catPath)
	src := t.TempDir()
	data := makeTorrent(t, src, "movie.bin", 20_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hash)

	// Without autoTMM, assigning a category does not relocate.
	if err := d.setCategory(hash, "movies"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if got := savePathOf(t, d, hash); got != src {
		t.Fatalf("a non-managed torrent moved: %s", got)
	}

	// Enabling autoTMM relocates it to the category's path.
	if err := d.setAutoManagement(hash, true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "moved to the category path", func() bool { return savePathOf(t, d, hash) == catPath })

	// Changing the category's path relocates it again.
	newPath := t.TempDir()
	d.editCategory("movies", newPath)
	waitFor(t, "moved to the new category path", func() bool { return savePathOf(t, d, hash) == newPath })

	// Disabling autoTMM keeps the current path.
	if err := d.setAutoManagement(hash, false); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	_, meta := d.findLocked(hash)
	auto := meta.AutoTMM
	d.mu.Unlock()
	if auto {
		t.Fatal("autoTMM was not disabled")
	}
	if got := savePathOf(t, d, hash); got != newPath {
		t.Fatalf("path changed after disabling autoTMM: %s", got)
	}
}

func TestAutoTMMForAdd(t *testing.T) {
	cases := []struct {
		req    addRequest
		global bool
		want   bool
	}{
		{addRequest{}, false, false},                                // default off
		{addRequest{}, true, true},                                  // global on
		{addRequest{AutoTMM: false, AutoTMMSet: true}, true, false}, // explicit wins
		{addRequest{AutoTMM: true, AutoTMMSet: true}, false, true},  // explicit wins
	}
	for _, c := range cases {
		if got := autoTMMForAdd(c.req, c.global); got != c.want {
			t.Errorf("autoTMMForAdd(%+v, %v) = %v, want %v", c.req, c.global, got, c.want)
		}
	}
}

// TestAutoTMMGlobalDefault checks the global default is applied to new torrents
// that do not state the flag, and that an explicit value wins.
func TestAutoTMMGlobalDefault(t *testing.T) {
	store, err := settings.Load(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	store.Set(autoTMMEnabledKey, "true")
	d := newTestDaemonFull(t, NetworkOptions{PortBegin: 42300, PortEnd: 42400, Encryption: 1, PEX: true}, store)
	src := t.TempDir()

	autoOf := func(hash string) bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		_, meta := d.findLocked(hash)
		return meta != nil && meta.AutoTMM
	}

	hash, _, err := d.add(addRequest{TorrentData: makeTorrent(t, src, "a.bin", 10_000), Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hash)
	if !autoOf(hash) {
		t.Fatal("the global autoTMM default was not applied")
	}

	hash2, _, err := d.add(addRequest{TorrentData: makeTorrent(t, src, "b.bin", 10_000), Destination: src, AutoTMM: false, AutoTMMSet: true})
	if err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hash2)
	if autoOf(hash2) {
		t.Fatal("an explicit autoTMM=false must win over the global default")
	}
}
