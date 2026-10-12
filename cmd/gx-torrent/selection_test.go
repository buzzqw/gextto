package main

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestSkipMaskAndAllNormal(t *testing.T) {
	if got := skipMask([]int{4, 0, 7}); !slices.Equal(got, []bool{false, true, false}) {
		t.Fatalf("skipMask = %v, want [false true false]", got)
	}
	if got := skipMask([]int{4, 4}); got != nil {
		t.Fatalf("skipMask all wanted = %v, want nil", got)
	}
	if !isAllNormal([]int{4, 4, 4}) {
		t.Fatal("all 4 must be all-normal")
	}
	for _, prio := range [][]int{{0, 4}, {7, 4}, {1}} {
		if isAllNormal(prio) {
			t.Fatalf("%v must not be all-normal", prio)
		}
	}
}

func TestQbitFilePriorityMapping(t *testing.T) {
	in := map[int]int{0: 0, 1: 4, 6: 6, 7: 7}
	for from, want := range in {
		if got := qbitFilePriorityIn(from); got != want {
			t.Errorf("qbitFilePriorityIn(%d) = %d, want %d", from, got, want)
		}
	}
	out := map[int]int{0: 0, 1: 1, 4: 1, 5: 6, 6: 6, 7: 7}
	for from, want := range out {
		if got := qbitFilePriorityOut(from); got != want {
			t.Errorf("qbitFilePriorityOut(%d) = %d, want %d", from, got, want)
		}
	}
}

// TestFilePriorityLevelsDoNotRestart checks that changing only the priority
// levels (the set of skipped files is unchanged) is applied to the running
// torrent without stopping it or moving any file.
func TestFilePriorityLevelsDoNotRestart(t *testing.T) {
	d := newTestDaemonWith(t, NetworkOptions{PortBegin: 44600, PortEnd: 44699, Encryption: 1})
	src := filepath.Join(t.TempDir(), "src")
	data := makeMultiTorrent(t, src, "Season", []int{50_000, 70_000, 40_000})
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src, Paused: true})
	if err != nil {
		t.Fatal(err)
	}
	// All normal: nothing is stored, the engine keeps every file wanted.
	if err := d.setFilePriorities(hash, []int{4, 4, 4}); err != nil {
		t.Fatal(err)
	}
	id := torrentID(d, hash)
	if got := d.prioritiesFor(id); got != nil {
		t.Fatalf("all-normal selection = %v, want none", got)
	}
	status := stateOf(d, hash)
	// Raise file 0 to high: only the levels change, nothing may move.
	if err := d.setFilePriorities(hash, []int{7, 4, 4}); err != nil {
		t.Fatal(err)
	}
	if got := d.prioritiesFor(id); !slices.Equal(got, []int{7, 4, 4}) {
		t.Fatalf("prioritiesFor = %v, want [7 4 4]", got)
	}
	d.mu.Lock()
	moving := len(d.moving)
	_, meta := d.findLocked(hash)
	d.mu.Unlock()
	if moving != 0 {
		t.Fatalf("a level-only change started a move")
	}
	if stateOf(d, hash) != status {
		t.Fatalf("a level-only change changed the torrent status")
	}
	if meta == nil || !slices.Equal(meta.FilePriorities, []int{7, 4, 4}) {
		t.Fatalf("meta.FilePriorities = %v, want [7 4 4]", meta)
	}
	// Back to all normal: the selection is dropped again.
	if err := d.setFilePriorities(hash, []int{4, 4, 4}); err != nil {
		t.Fatal(err)
	}
	if got := d.prioritiesFor(id); got != nil {
		t.Fatalf("after reset prioritiesFor = %v, want none", got)
	}
}

// torrentID returns the engine id of a torrent (the key the selection map uses).
func torrentID(d *Daemon, hash string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	t, _ := d.findLocked(hash)
	if t == nil {
		return ""
	}
	return t.ID()
}
