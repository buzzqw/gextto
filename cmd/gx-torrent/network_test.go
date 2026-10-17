package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

func TestIPFilterRulesCache(t *testing.T) {
	d := newTestDaemon(t)
	path := filepath.Join(t.TempDir(), "filter.dat")
	if err := os.WriteFile(path, []byte("1.2.3.0/24\n5.6.7.0/24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if n, err := d.loadIPFilterLocked(path); err != nil || n != 2 {
		t.Fatalf("first load = %d/%v, want 2 rules", n, err)
	}
	if d.ipFilterSet.Len() != 2 {
		t.Fatalf("cached set holds %d rules, want 2", d.ipFilterSet.Len())
	}

	// Same path, size and mtime: the parsed rules are reused and the file is
	// not read. Garbage of the same length proves it.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not a filter at all!!\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if n, err := d.loadIPFilterLocked(path); err != nil || n != 2 {
		t.Fatalf("cached load = %d/%v, want the 2 cached rules", n, err)
	}

	// A changed file is parsed again.
	if err := os.WriteFile(path, []byte("9.9.9.0/24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if n, err := d.loadIPFilterLocked(path); err != nil || n != 1 {
		t.Fatalf("reload = %d/%v, want 1 rule", n, err)
	}
}

// Holepunching punches UDP mappings, so it follows uTP: enabling it without uTP
// must still leave it off in the engine session.
func TestApplyNetworkHolepunchFollowsUTP(t *testing.T) {
	d := &Daemon{}
	d.peerPort = 51413

	cfg := &torrent.Config{}
	d.opts.Network = NetworkOptions{UTP: true, Holepunch: true}
	d.applyNetwork(cfg)
	if !cfg.UTP || !cfg.Holepunch {
		t.Fatalf("utp=%v holepunch=%v, want both on", cfg.UTP, cfg.Holepunch)
	}

	cfg = &torrent.Config{}
	d.opts.Network = NetworkOptions{UTP: false, Holepunch: true}
	d.applyNetwork(cfg)
	if cfg.UTP || cfg.Holepunch {
		t.Fatalf("utp=%v holepunch=%v, want both off", cfg.UTP, cfg.Holepunch)
	}
}
