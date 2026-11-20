package main

import (
	"bytes"
	"fmt"
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
	go func() { done <- s.dueHashes(time.Now(), 0) }()
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
	if due := s.dueHashes(now, 0); !slices.Equal(due, []string{"aa"}) {
		t.Fatalf("first dueHashes = %v, want [aa]", due)
	}
	if due := s.dueHashes(now.Add(time.Minute), 0); len(due) != 0 {
		t.Fatalf("second dueHashes = %v, want empty before the interval", due)
	}
	if due := s.dueHashes(now.Add(lsdInterval+time.Second), 0); !slices.Equal(due, []string{"aa"}) {
		t.Fatalf("third dueHashes = %v, want [aa] after the interval", due)
	}

	// The torrent stops running: it must be forgotten, not re-announced.
	d.publishViewsLocked([]torrentInfo{{Hash: "aa", State: "paused"}})
	now = now.Add(2 * lsdInterval)
	if due := s.dueHashes(now, 0); len(due) != 0 {
		t.Fatalf("dueHashes after stop = %v, want empty", due)
	}
	if _, ok := s.announced["aa"]; ok {
		t.Fatal("stopped torrent still tracked as announced")
	}
}

// TestLSDMessageIPv6 checks the announce message carries the IPv6 group as
// Host and round-trips through parseLSD (BEP 14 over ff15::efc0:988f).
func TestLSDMessageIPv6(t *testing.T) {
	s := &lsdService{port: 6881, cookie: "abcd"}
	hash := "0123456789abcdef0123456789abcdef01234567"
	raw := s.message(lsdGroup6, []string{hash})

	if !bytes.Contains(raw, []byte("Host: "+lsdGroup6)) {
		t.Fatalf("message missing IPv6 host header:\n%s", raw)
	}
	port, hashes, cookie, ok := parseLSD(raw)
	if !ok || port != 6881 || cookie != "abcd" || !slices.Equal(hashes, []string{hash}) {
		t.Fatalf("parseLSD = %d %v %q %v", port, hashes, cookie, ok)
	}
}

// TestLSDDueHashesBatchAndPriority locks in the BEP 14 round-robin: one slot
// carries a bounded batch, downloads go before checks and seeds, and a torrent
// left out stays due for the next slot.
func TestLSDDueHashesBatchAndPriority(t *testing.T) {
	d := newQuietDaemon(t)
	d.publishViewsLocked([]torrentInfo{
		{Hash: "seed", State: "seeding"},
		{Hash: "check", State: "checking_files"},
		{Hash: "down", State: "downloading"},
	})
	s := &lsdService{d: d, announced: map[string]time.Time{}}
	now := time.Now()
	for _, want := range []string{"down", "check", "seed"} {
		if got := s.dueHashes(now, 1); !slices.Equal(got, []string{want}) {
			t.Fatalf("dueHashes(limit 1) = %v, want [%s]", got, want)
		}
	}
	if got := s.dueHashes(now, 1); len(got) != 0 {
		t.Fatalf("dueHashes after every torrent was announced = %v, want empty", got)
	}
}

// TestLSDDueHashesRoundRobin checks that a full batch leaves the rest due: with
// more torrents than one packet holds, the next slot takes the remainder.
func TestLSDDueHashesRoundRobin(t *testing.T) {
	d := newQuietDaemon(t)
	views := make([]torrentInfo, 0, lsdMaxPerMessage+6)
	for i := 0; i < lsdMaxPerMessage+6; i++ {
		views = append(views, torrentInfo{Hash: fmt.Sprintf("%040x", i), State: "seeding"})
	}
	d.publishViewsLocked(views)
	s := &lsdService{d: d, announced: map[string]time.Time{}}
	now := time.Now()
	if got := s.dueHashes(now, lsdMaxPerMessage); len(got) != lsdMaxPerMessage {
		t.Fatalf("first batch = %d torrents, want %d", len(got), lsdMaxPerMessage)
	}
	if got := s.dueHashes(now, lsdMaxPerMessage); len(got) != 6 {
		t.Fatalf("second batch = %d torrents, want the 6 left out", len(got))
	}
}

// TestLSDMessageFitsMTU holds the packet under the 1400 bytes BEP 14 asks for.
func TestLSDMessageFitsMTU(t *testing.T) {
	hashes := make([]string, lsdMaxPerMessage)
	for i := range hashes {
		hashes[i] = fmt.Sprintf("%040x", i)
	}
	s := &lsdService{port: 6881, cookie: "0123456789abcdef"}
	raw := s.message(lsdGroup, hashes)
	if len(raw) > 1400 {
		t.Fatalf("announce of %d hashes is %d bytes, over the 1400-byte limit", len(hashes), len(raw))
	}
	if _, got, _, ok := parseLSD(raw); !ok || len(got) != len(hashes) {
		t.Fatalf("parseLSD = %d hashes, ok=%v, want %d", len(got), ok, len(hashes))
	}
}
