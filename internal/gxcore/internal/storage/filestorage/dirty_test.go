package filestorage

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestMarkDirtyMergesAndCoalesces guards the ranged-flush bookkeeping: adjacent
// writes extend the last interval, gaps start a new one, and a full list is
// coalesced into a single interval instead of growing without bound.
func TestMarkDirtyMergesAndCoalesces(t *testing.T) {
	m := &mmapFile{}

	// Sequential writes (the common piece-write pattern) extend one interval.
	m.markDirty(0, 100)
	m.markDirty(100, 200)
	m.markDirty(150, 250) // overlapping write, still merged
	want := []dirtyRange{{0, 250}}
	if !reflect.DeepEqual(m.dirty, want) {
		t.Fatalf("after sequential writes dirty = %v, want %v", m.dirty, want)
	}

	// A gap starts a new interval.
	m.markDirty(400, 500)
	want = []dirtyRange{{0, 250}, {400, 500}}
	if !reflect.DeepEqual(m.dirty, want) {
		t.Fatalf("after a gap dirty = %v, want %v", m.dirty, want)
	}

	// A zero-length write is ignored.
	m.markDirty(600, 600)
	if !reflect.DeepEqual(m.dirty, want) {
		t.Fatalf("zero-length write changed dirty = %v", m.dirty)
	}

	// Fill past the cap: the list must stay bounded and still cover the writes.
	lastBegin, lastEnd := int64(0), int64(0)
	for i := 0; i < maxDirtyRanges+10; i++ {
		begin := int64(1000 + i*100)
		m.markDirty(begin, begin+10)
		lastBegin, lastEnd = begin, begin+10
	}
	if len(m.dirty) > maxDirtyRanges {
		t.Fatalf("dirty has %d ranges after overflow, want <= %d", len(m.dirty), maxDirtyRanges)
	}
	covered := false
	for _, r := range m.dirty {
		if r.begin <= lastBegin && r.end >= lastEnd {
			covered = true
		}
	}
	if !covered {
		t.Fatalf("dirty ranges %v do not cover the last write [%d,%d)", m.dirty, lastBegin, lastEnd)
	}
}

// TestMmapRangedSyncPersists checks the ranged msync path end to end: a small
// write near the end of a multi-page file is flushed and readable after reopen,
// and a second round of writes is flushed too (the ranges are cleared on Sync).
func TestMmapRangedSyncPersists(t *testing.T) {
	dir := t.TempDir()
	const size = 1 << 20
	s, err := New(dir, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	s.Mmap = true
	f, _, err := s.Open("ranged.bin", size)
	if err != nil {
		t.Fatal(err)
	}
	mf, ok := f.(*mmapFile)
	if !ok {
		t.Fatalf("expected an mmapFile, got %T", f)
	}

	if _, err := f.WriteAt([]byte("first"), size-64); err != nil {
		t.Fatal(err)
	}
	if len(mf.dirty) == 0 {
		t.Fatal("WriteAt did not record a dirty range")
	}
	if err := f.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(mf.dirty) != 0 {
		t.Fatalf("Sync left %d dirty ranges", len(mf.dirty))
	}

	// A second round dirtied and flushed independently.
	if _, err := f.WriteAt([]byte("second"), 0); err != nil {
		t.Fatal(err)
	}
	if err := f.Sync(); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "ranged.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != size {
		t.Fatalf("file size = %d, want %d", len(got), size)
	}
	if string(got[:6]) != "second" {
		t.Fatalf("head = %q, want second", got[:6])
	}
	if string(got[size-64:size-59]) != "first" {
		t.Fatalf("tail = %q, want first", got[size-64:size-59])
	}
}

// TestMarkDirtyOutOfOrderWrite: with concurrent piece writes a range can land
// just before the last tracked one. The merged interval must widen its start
// too, or those bytes would never be msync'd.
func TestMarkDirtyOutOfOrderWrite(t *testing.T) {
	m := &mmapFile{}
	m.markDirty(1000, 2000)
	m.markDirty(500, 1000) // adjacent, before
	m.markDirty(300, 600)  // overlapping, before
	want := []dirtyRange{{300, 2000}}
	if !reflect.DeepEqual(m.dirty, want) {
		t.Fatalf("dirty = %v, want %v", m.dirty, want)
	}
	m.markDirty(0, 100) // disjoint, before: a new interval
	want = []dirtyRange{{300, 2000}, {0, 100}}
	if !reflect.DeepEqual(m.dirty, want) {
		t.Fatalf("dirty = %v, want %v", m.dirty, want)
	}
}
