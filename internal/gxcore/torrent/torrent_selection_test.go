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
