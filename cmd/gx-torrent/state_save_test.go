package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newSaverDaemon builds a daemon whose state has a few torrents and starts the
// coalescing saver with a short debounce, the way newDaemon does.
func newSaverDaemon(t *testing.T, debounce time.Duration) *Daemon {
	t.Helper()
	d := benchState(t.TempDir(), 3, 0)
	d.stateDelay = debounce
	d.startStateSaver()
	t.Cleanup(d.stopStateSaver)
	return d
}

// dirtyAllLocked changes the persisted state the way a tick does and asks for
// one save, holding d.mu exactly as the queue loop does.
func dirtyAllLocked(d *Daemon, tick int) {
	d.mu.Lock()
	for _, meta := range d.state.Torrents {
		meta.Category = fmt.Sprintf("tick-%d", tick)
	}
	d.dirty = true
	d.scheduleSaveLocked()
	d.mu.Unlock()
}

// TestStateSaverCoalescesDirtyTicks is the point of the writer goroutine: a
// burst of dirty ticks — what swarm counts, rotations and progress steps
// produce — must reach the disk as one write, not one per tick.
func TestStateSaverCoalescesDirtyTicks(t *testing.T) {
	const debounce = 150 * time.Millisecond
	d := newSaverDaemon(t, debounce)

	for i := 0; i < 20; i++ {
		dirtyAllLocked(d, i)
	}
	waitFor(t, "the coalesced state write", func() bool { return d.stateWrites.Load() >= 1 })

	// Nothing changed after that write, so no further write may follow.
	time.Sleep(3 * debounce)
	if got := d.stateWrites.Load(); got != 1 {
		t.Fatalf("20 dirty ticks produced %d state writes, want 1", got)
	}
	data, err := os.ReadFile(d.opts.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"category": "tick-19"`) {
		t.Fatalf("the coalesced write did not carry the last dirty tick:\n%s", data)
	}
}

// TestCloseFlushesPendingState covers the durability rule: shutdown writes the
// last state even when the debounce window has not elapsed and nothing else
// would save it.
func TestCloseFlushesPendingState(t *testing.T) {
	d := benchState(t.TempDir(), 3, 0)
	// An hour of debounce: the coalesced write cannot happen on its own.
	d.stateDelay = time.Hour
	d.startStateSaver()

	d.mu.Lock()
	for _, meta := range d.state.Torrents {
		meta.Category = "ultimo"
	}
	d.dirty = true
	d.scheduleSaveLocked()
	d.mu.Unlock()
	if got := d.stateWrites.Load(); got != 0 {
		t.Fatalf("state was written before the debounce elapsed (%d writes)", got)
	}

	d.close()

	data, err := os.ReadFile(d.opts.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "ultimo") {
		t.Fatalf("close did not write the last state:\n%s", data)
	}
	if _, err := os.Stat(d.opts.StatePath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("close left the temporary state file behind: %v", err)
	}
}

// TestStateSaverDropsStaleSnapshot locks the ordering rule: a coalesced write
// that took its snapshot before a handler's synchronous save must be dropped,
// not put older state on disk after it.
func TestStateSaverDropsStaleSnapshot(t *testing.T) {
	d := benchState(t.TempDir(), 3, 0)

	d.mu.Lock()
	staleSeq, staleState := d.prepareSnapshotLocked()
	d.mu.Unlock()
	stale, err := encodeSnapshot(staleState)
	if err != nil {
		t.Fatal(err)
	}

	// A handler changes the state and answers only once it is saved.
	d.mu.Lock()
	for _, meta := range d.state.Torrents {
		meta.Category = "nuovo"
	}
	d.dirty = true
	d.saveLocked()
	d.mu.Unlock()

	// The slow coalesced write now tries to flush the older snapshot.
	if err := d.writeState(staleSeq, stale); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(d.opts.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "nuovo") {
		t.Fatalf("an older snapshot overwrote the newer state:\n%s", data)
	}
	if strings.Contains(string(data), `"category": "serie"`) {
		t.Fatalf("the stale snapshot reached the disk:\n%s", data)
	}
}

// TestSaveLockedKeepsDirtyAfterFailure: when the write fails, the change must
// stay marked dirty so the saver retries it instead of being lost until the
// next unrelated change.
func TestSaveLockedKeepsDirtyAfterFailure(t *testing.T) {
	d := benchState(t.TempDir(), 3, 0)
	// A state path under a missing folder: the write cannot succeed.
	d.opts.StatePath = filepath.Join(d.opts.StatePath, "missing", "state.json")

	d.mu.Lock()
	for _, meta := range d.state.Torrents {
		meta.Category = "perso"
	}
	d.dirty = true
	d.saveLocked()
	dirty := d.dirty
	d.mu.Unlock()

	if !dirty {
		t.Fatal("a failed save must leave the state dirty for the retry")
	}
	if got := d.stateWrites.Load(); got != 0 {
		t.Fatalf("%d state writes on a failing path, want 0", got)
	}
}

// TestSaveLockedStillWritesSynchronously guards the contract handlers rely on:
// the change is on disk when saveLocked returns, whatever the saver is doing.
func TestSaveLockedStillWritesSynchronously(t *testing.T) {
	d := benchState(t.TempDir(), 3, 0)
	d.stateDelay = time.Hour
	d.startStateSaver()
	t.Cleanup(d.stopStateSaver)

	d.mu.Lock()
	for _, meta := range d.state.Torrents {
		meta.Category = "subito"
	}
	d.dirty = true
	d.saveLocked()
	d.mu.Unlock()
	if got := d.stateWrites.Load(); got != 1 {
		t.Fatalf("saveLocked did not write before returning (%d writes)", got)
	}
	data, err := os.ReadFile(d.opts.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "subito") {
		t.Fatalf("saveLocked did not persist the change:\n%s", data)
	}
	if _, err := os.Stat(d.opts.StatePath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("saveLocked left the temporary state file behind: %v", err)
	}
}
