package torrent

import (
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// v2OnlyTwoFiles is a v2-only torrent with two files of 15 bytes and pieces of
// 10: each file gets its own pieces, so the layout is [10 5][10 5] =
// pieces 0,1 for "a" and 2,3 for "b". Read as one stream (v1) it would be
// three pieces [10 10 10] with "b" starting inside piece 1.
func v2OnlyTwoFiles() *metainfo.Info {
	return &metainfo.Info{
		V2:          true,
		PieceLength: 10,
		NumPieces:   4,
		Length:      30,
		Files:       []metainfo.File{{Path: "a", Length: 15}, {Path: "b", Length: 15}},
	}
}

func TestV2OnlySelectionSkipsTheRightPieces(t *testing.T) {
	tr := torrentWithInfo(v2OnlyTwoFiles())
	// Keep "b", drop "a". Read as one stream, "b" would be pieces 1 and 2.
	skip := tr.skippedPieces([]bool{true, false})
	want := []bool{true, true, false, false}
	if len(skip) != len(want) {
		t.Fatalf("skip = %v, want %v", skip, want)
	}
	for i := range want {
		if skip[i] != want[i] {
			t.Fatalf("skip = %v, want %v: the pieces of file b are 2 and 3", skip, want)
		}
	}
}

func TestV2OnlySelectedBytesCountShortTails(t *testing.T) {
	tr := torrentWithInfo(v2OnlyTwoFiles())
	tr.selectionMaskSet = true // every file wanted
	tr.bitfield = bitfield.New(4)
	for i := uint32(0); i < 4; i++ {
		tr.bitfield.Set(i)
	}
	total, done := tr.selectedBytes()
	if total != 30 || done != 30 {
		t.Fatalf("selected bytes = %d/%d, want 30/30 (two 5-byte tails, not full pieces)", done, total)
	}

	// Only the tail of "a" missing: 25 of 30.
	tr.bitfield = bitfield.New(4)
	for _, i := range []uint32{0, 2, 3} {
		tr.bitfield.Set(i)
	}
	if total, done = tr.selectedBytes(); total != 30 || done != 25 {
		t.Fatalf("selected bytes = %d/%d, want 25/30", done, total)
	}
}

func TestV2OnlyBytesCompleteUsesThePieceLengths(t *testing.T) {
	info := v2OnlyTwoFiles()
	tr := torrentWithInfo(info)
	tr.pieces = []piece.Piece{{Index: 0, Length: 10}, {Index: 1, Length: 5}, {Index: 2, Length: 10}, {Index: 3, Length: 5}}
	tr.bitfield = bitfield.New(4)
	for i := uint32(0); i < 4; i++ {
		tr.bitfield.Set(i)
	}
	if got := tr.bytesComplete(); got != 30 {
		t.Fatalf("bytes complete = %d, want 30 (the report measured 35)", got)
	}
}

func TestV2OnlyFileStatsAttributeBytesToTheirFile(t *testing.T) {
	have := map[uint32]bool{0: true, 1: true} // all of "a", nothing of "b"
	stats := fileStatsFromLayout(v2OnlyTwoFiles(), func(i uint32) bool { return have[i] })
	got := map[string]int64{}
	for _, s := range stats {
		got[s.Path()] = s.BytesCompleted
	}
	if got["a"] != 15 || got["b"] != 0 {
		t.Fatalf("file stats = %v, want a=15 b=0", got)
	}
}

func TestV2OnlyWantedCompleteNeedsTheTailOfAWantedFile(t *testing.T) {
	tr := torrentWithInfo(v2OnlyTwoFiles())
	// Only "b" wanted. Pieces 1 and 2 are what a one-stream reading would
	// call "b": having them must not complete the selection while the tail
	// of "b" (piece 3) is missing.
	tr.selectionMask, tr.selectionMaskSet = tr.skippedPieces([]bool{true, false}), true
	tr.bitfield = bitfield.New(4)
	tr.bitfield.Set(1)
	tr.bitfield.Set(2)
	if tr.wantedComplete() {
		t.Fatal("a wanted file whose tail piece is missing was declared complete")
	}
	tr.bitfield.Set(3)
	if !tr.wantedComplete() {
		t.Fatal("every piece of the wanted file is there: the selection is complete")
	}
}

func TestV2OnlyFilePieceRangeStartsOnTheFilesOwnPiece(t *testing.T) {
	tr := torrentWithInfo(v2OnlyTwoFiles())
	begin, end, ok := tr.filePieceRange(1, 0, 15)
	if !ok || begin != 2 || end != 4 {
		t.Fatalf("pieces of file b = [%d,%d) ok=%v, want [2,4)", begin, end, ok)
	}
	begin, end, ok = tr.filePieceRange(1, 12, 3)
	if !ok || begin != 3 || end != 4 {
		t.Fatalf("tail of file b = [%d,%d) ok=%v, want [3,4)", begin, end, ok)
	}
}

// The v1 layout is unchanged: files end to end, one short piece at the end.
func TestV1LayoutStillSpansFiles(t *testing.T) {
	info := &metainfo.Info{
		PieceLength: 10, NumPieces: 3, Length: 30,
		Files: []metainfo.File{{Path: "a", Length: 15}, {Path: "b", Length: 15}},
	}
	tr := torrentWithInfo(info)
	skip := tr.skippedPieces([]bool{false, true})
	if want := []bool{false, false, true}; len(skip) != 3 || skip[0] != want[0] || skip[1] != want[1] || skip[2] != want[2] {
		t.Fatalf("v1 skip = %v, want %v (piece 1 holds bytes of both files)", skip, want)
	}
	begin, end, ok := tr.filePieceRange(1, 0, 15)
	if !ok || begin != 1 || end != 3 {
		t.Fatalf("v1 pieces of file b = [%d,%d) ok=%v, want [1,3)", begin, end, ok)
	}
	var lengths []int64
	forEachPieceLength(info, func(_ uint32, n int64) { lengths = append(lengths, n) })
	if len(lengths) != 3 || lengths[0] != 10 || lengths[2] != 10 {
		t.Fatalf("v1 piece lengths = %v", lengths)
	}
}

func TestStoppedBytesCompleteCountsFromTheLayout(t *testing.T) {
	tr := torrentWithInfo(v2OnlyTwoFiles()) // stopped: no pieces in memory
	tr.bitfield = bitfield.New(4)
	for _, i := range []uint32{0, 2, 3} { // tail of "a" missing
		tr.bitfield.Set(i)
	}
	if got := tr.bytesComplete(); got != 25 {
		t.Fatalf("stopped bytes complete = %d, want 25 (it used to be 0, and the estimate 30)", got)
	}
}
