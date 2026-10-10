package main

import (
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"
)

// newQuietDaemon builds a daemon without starting the queue loop, so tests can
// publish a synthetic snapshot without a tick overwriting it.
func newQuietDaemon(t *testing.T) *Daemon {
	t.Helper()
	data := t.TempDir()
	opts := Options{
		Listen:      "127.0.0.1:0",
		DataDir:     data,
		LinkDir:     filepath.Join(data, "links"),
		DownloadDir: filepath.Join(data, "downloads"),
		DBPath:      filepath.Join(data, "session.db"),
		StatePath:   filepath.Join(data, "state.json"),
		Network:     NetworkOptions{PortBegin: 43000, PortEnd: 43100, Encryption: 1},
		Tick:        50 * time.Millisecond,
	}
	d, err := newDaemon(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.close() })
	return d
}

// TestLSDDueHashesReadsSnapshotWithoutLock locks in the engine change: LSD
// announces are computed from the lock-free snapshot, never by taking d.mu and
// calling t.Stats() on the torrent run loops. Holding d.mu while calling
// dueHashes must therefore not deadlock.
func TestLSDDueHashesReadsSnapshotWithoutLock(t *testing.T) {
	d := newQuietDaemon(t)
	d.publishViewsLocked([]torrentInfo{
		{Hash: "aa", State: "downloading"},
		{Hash: "bb", State: "paused"},
		{Hash: "cc", State: "seeding", Private: true},
		{Hash: "dd", State: "stalled"},
		{Hash: "ee", State: "checking_files"},
		{Hash: "ff", State: "error"},
		{Hash: "gg", State: "seeding"},
	})
	s := &lsdService{d: d, announced: map[string]time.Time{}}

	d.mu.Lock()
	done := make(chan []string, 1)
	go func() { done <- s.dueHashes(time.Now()) }()
	var due []string
	select {
	case due = <-done:
	case <-time.After(5 * time.Second):
		d.mu.Unlock()
		t.Fatal("dueHashes blocked: it must not take d.mu")
	}
	d.mu.Unlock()

	sort.Strings(due)
	want := []string{"aa", "ee", "gg"}
	if !slices.Equal(due, want) {
		t.Fatalf("dueHashes = %v, want %v", due, want)
	}
}

// TestLSDDueHashesInterval verifies announced torrents are only re-announced
// after the interval, and that torrents no longer running are forgotten.
func TestLSDDueHashesInterval(t *testing.T) {
	d := newQuietDaemon(t)
	d.publishViewsLocked([]torrentInfo{{Hash: "aa", State: "seeding"}})
	s := &lsdService{d: d, announced: map[string]time.Time{}}

	now := time.Now()
	if due := s.dueHashes(now); !slices.Equal(due, []string{"aa"}) {
		t.Fatalf("first dueHashes = %v, want [aa]", due)
	}
	if due := s.dueHashes(now.Add(time.Minute)); len(due) != 0 {
		t.Fatalf("second dueHashes = %v, want empty before the interval", due)
	}
	if due := s.dueHashes(now.Add(lsdInterval + time.Second)); !slices.Equal(due, []string{"aa"}) {
		t.Fatalf("third dueHashes = %v, want [aa] after the interval", due)
	}

	// The torrent stops running: it must be forgotten, not re-announced.
	d.publishViewsLocked([]torrentInfo{{Hash: "aa", State: "paused"}})
	now = now.Add(2 * lsdInterval)
	if due := s.dueHashes(now); len(due) != 0 {
		t.Fatalf("dueHashes after stop = %v, want empty", due)
	}
	if _, ok := s.announced["aa"]; ok {
		t.Fatal("stopped torrent still tracked as announced")
	}
}
