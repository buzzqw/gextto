package torrent

import (
	"slices"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
)

func threeFileInfo() *metainfo.Info {
	return &metainfo.Info{Files: []metainfo.File{
		{Length: 1, Path: "a"},
		{Length: 1, Path: "b"},
		{Length: 1, Path: "c"},
	}}
}

func TestMaskFromSelectOnly(t *testing.T) {
	info := threeFileInfo()
	cases := []struct {
		name   string
		wanted []int
		want   []bool
	}{
		{"only file 0", []int{0}, []bool{false, true, true}},
		{"files 0 and 2", []int{0, 2}, []bool{false, true, false}},
		{"every file", []int{0, 1, 2}, nil},
		{"no valid index", []int{9}, nil},
		{"empty", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := maskFromSelectOnly(info, tc.wanted)
			if !slices.Equal(got, tc.want) {
				t.Errorf("maskFromSelectOnly(%v) = %v, want %v", tc.wanted, got, tc.want)
			}
		})
	}
}

func TestMaskFromSelectOnlySkipsPadding(t *testing.T) {
	info := &metainfo.Info{Files: []metainfo.File{
		{Length: 1, Path: "a"},
		{Length: 1, Path: "b", Padding: true},
		{Length: 1, Path: "c"},
	}}
	// The `so` index addresses non-padding files, so 0 is "a" and 1 is "c".
	got := maskFromSelectOnly(info, []int{0})
	want := []bool{false, false, true}
	if !slices.Equal(got, want) {
		t.Errorf("maskFromSelectOnly = %v, want %v", got, want)
	}
}

func TestMaskFromFlagsSkipsPadding(t *testing.T) {
	info := &metainfo.Info{Files: []metainfo.File{
		{Length: 1, Path: "a"},
		{Length: 1, Path: "b", Padding: true},
		{Length: 1, Path: "c"},
	}}
	// Flags address non-padding files: skip "a", want "c".
	got := maskFromFlags(info, []bool{true, false})
	want := []bool{true, false, false}
	if !slices.Equal(got, want) {
		t.Errorf("maskFromFlags = %v, want %v", got, want)
	}
	// Everything wanted → nil.
	if got := maskFromFlags(info, []bool{false, false}); got != nil {
		t.Errorf("all-wanted flags = %v, want nil", got)
	}
}

func TestPrioritiesToInfoFiles(t *testing.T) {
	info := &metainfo.Info{Files: []metainfo.File{
		{Length: 1, Path: "a"},
		{Length: 1, Path: "b", Padding: true},
		{Length: 1, Path: "c"},
	}}
	// Skip "a", high for "c"; padding keeps the normal priority.
	got := prioritiesToInfoFiles(info, []int{0, 7})
	want := []int{0, 4, 7}
	if !slices.Equal(got, want) {
		t.Fatalf("prioritiesToInfoFiles = %v, want %v", got, want)
	}
	// A short list (or one that is all normal) means "no explicit selection".
	if got := prioritiesToInfoFiles(info, []int{4, 4}); got != nil {
		t.Fatalf("all-normal = %v, want nil", got)
	}
	if got := prioritiesToInfoFiles(info, []int{}); got != nil {
		t.Fatalf("empty = %v, want nil", got)
	}
}

// TestSelectionPrioritiesSkipAndHigh checks the whole per-file → per-piece
// mapping through a torrent: file "a" is skipped (0), file "b" is high (7), so
// piece 0 is skipped and piece 1 carries the high priority.
func TestSelectionPrioritiesSkipAndHigh(t *testing.T) {
	info := &metainfo.Info{
		PieceLength: 2,
		Length:      4,
		NumPieces:   2,
		Files: []metainfo.File{
			{Length: 2, Path: "a"},
			{Length: 2, Path: "b"},
		},
	}
	tr := &torrent{
		id:      "t1",
		info:    info,
		session: &Session{config: Config{FilePriorities: func(string) []int { return []int{0, 7} }}},
	}
	if got := tr.skippedInfoFiles(); !slices.Equal(got, []bool{true, false}) {
		t.Fatalf("skippedInfoFiles = %v, want [true false]", got)
	}
	if got := tr.piecePriorities(); !slices.Equal(got, []uint8{0, 7}) {
		t.Fatalf("piecePriorities = %v, want [0 7]", got)
	}
	// No callback → every piece is normal and nothing is skipped.
	plain := &torrent{id: "t2", info: info, session: &Session{}}
	if got := plain.skippedInfoFiles(); got != nil {
		t.Fatalf("no selection skippedInfoFiles = %v, want nil", got)
	}
	if got := plain.piecePriorities(); got != nil {
		t.Fatalf("no selection piecePriorities = %v, want nil", got)
	}
}

// TestLegacyFileSelectionStillWorks keeps the old skip/download contract alive:
// FileSelection is honoured when FilePriorities is not set, and the wanted files
// become normal-priority pieces.
func TestLegacyFileSelectionStillWorks(t *testing.T) {
	info := &metainfo.Info{
		PieceLength: 2,
		Length:      4,
		NumPieces:   2,
		Files: []metainfo.File{
			{Length: 2, Path: "a"},
			{Length: 2, Path: "b"},
		},
	}
	tr := &torrent{
		id:      "t1",
		info:    info,
		session: &Session{config: Config{FileSelection: func(string) []bool { return []bool{true, false} }}},
	}
	if got := tr.skippedInfoFiles(); !slices.Equal(got, []bool{true, false}) {
		t.Fatalf("skippedInfoFiles = %v, want [true false]", got)
	}
	if got := tr.piecePriorities(); !slices.Equal(got, []uint8{0, 4}) {
		t.Fatalf("piecePriorities = %v, want [0 4]", got)
	}
}
