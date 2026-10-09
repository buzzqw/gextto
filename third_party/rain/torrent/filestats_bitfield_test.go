package torrent

import (
	"testing"

	"github.com/cenkalti/rain/v2/internal/metainfo"
)

// gextto fork: completed bytes per file from the bitfield of a stopped torrent.
func TestFileStatsFromPieces(t *testing.T) {
	// Pieces of 10 bytes: a=0..25, padding 25..30, b=30..50, empty c, d=50..55.
	files := []metainfo.File{
		{Path: "a", Length: 25},
		{Path: ".pad/5", Length: 5, Padding: true},
		{Path: "b", Length: 20},
		{Path: "c", Length: 0},
		{Path: "d", Length: 5},
	}
	have := map[uint32]bool{0: true, 2: true, 3: true, 4: true}
	stats := fileStatsFromPieces(files, 10, func(index uint32) bool { return have[index] })
	want := map[string]int64{"a": 15, "b": 20, "c": 0, "d": 0}
	if len(stats) != len(want) {
		t.Fatalf("got %d files, want %d (padding excluded)", len(stats), len(want))
	}
	for _, stat := range stats {
		if stat.BytesCompleted != want[stat.Path()] {
			t.Errorf("%s: completed %d, want %d", stat.Path(), stat.BytesCompleted, want[stat.Path()])
		}
	}
	have[5] = true
	for _, stat := range fileStatsFromPieces(files, 10, func(index uint32) bool { return have[index] }) {
		if stat.Path() == "d" && stat.BytesCompleted != 5 {
			t.Errorf("d: completed %d, want 5", stat.BytesCompleted)
		}
	}
}
