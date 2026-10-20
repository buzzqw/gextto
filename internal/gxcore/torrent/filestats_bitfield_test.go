package torrent

import (
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
)

// completed bytes per file from the bitfield of a stopped torrent.
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

func TestPieceStatesFromBitfield(t *testing.T) {
	info := &metainfo.Info{NumPieces: 5}
	bits := bitfield.New(5)
	for _, index := range []uint32{0, 1, 3} {
		bits.Set(index)
	}
	skip := bitfield.New(5)
	skip.Set(4)

	states := pieceStatesFromBitfield(info, bits, skip)
	want := []string{"have", "have", "", "have", "skipped"}
	if len(states) != len(want) {
		t.Fatalf("states = %v, want %v", states, want)
	}
	for index := range want {
		if states[index] != want[index] {
			t.Fatalf("states = %v, want %v", states, want)
		}
	}
	if got := pieceStatesFromBitfield(nil, bits, nil); got != nil {
		t.Fatalf("without info = %v, want nil", got)
	}
	if got := pieceStatesFromBitfield(info, nil, nil); got != nil {
		t.Fatalf("without bitfield = %v, want nil", got)
	}
	// No skip bitfield at all: everything not have/missing.
	if got := pieceStatesFromBitfield(info, bits, nil); len(got) != 5 || got[4] != "" {
		t.Fatalf("without skip = %v", got)
	}
}
