package metainfo

import (
	"crypto/sha256"
	"testing"

	"github.com/zeebo/bencode"
)

func mustInfo(t *testing.T, value map[string]any) []byte {
	t.Helper()
	b, err := bencode.EncodeBytes(value)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return b
}

// TestMultiFileLayout pins the difference between "a folder with one file" and
// "a single file": the count of files is not enough, because both have one.
// A web seed uses this flag to decide whether its URL is the file or a folder.
func TestMultiFileLayout(t *testing.T) {
	pieces := make([]byte, 20) // one SHA-1 piece
	root := sha256.Sum256(make([]byte, 100))

	v1Single := mustInfo(t, map[string]any{
		"name": "solo.mkv", "piece length": 16384, "pieces": pieces, "length": 100,
	})
	v1Folder := mustInfo(t, map[string]any{
		"name": "pack", "piece length": 16384, "pieces": pieces,
		"files": []any{map[string]any{"length": 100, "path": []string{"only.mkv"}}},
	})
	v2Single := mustInfo(t, map[string]any{
		"name": "solo.mkv", "piece length": 16384, "meta version": 2,
		"file tree": map[string]any{"solo.mkv": map[string]any{"": map[string]any{
			"length": 100, "pieces root": string(root[:]),
		}}},
	})
	v2Folder := mustInfo(t, map[string]any{
		"name": "pack", "piece length": 16384, "meta version": 2,
		"file tree": map[string]any{"pack": map[string]any{"only.mkv": map[string]any{"": map[string]any{
			"length": 100, "pieces root": string(root[:]),
		}}}},
	})
	hybridFolder := mustInfo(t, map[string]any{
		"name": "pack", "piece length": 16384, "pieces": pieces, "meta version": 2,
		"files": []any{map[string]any{"length": 100, "path": []string{"only.mkv"}}},
		"file tree": map[string]any{"pack": map[string]any{"only.mkv": map[string]any{"": map[string]any{
			"length": 100, "pieces root": string(root[:]),
		}}}},
	})

	cases := []struct {
		name string
		raw  []byte
		v2   bool
		want bool
	}{
		{"v1 single-file", v1Single, false, false},
		{"v1 folder with one file", v1Folder, false, true},
		{"v2 single-file", v2Single, true, false},
		{"v2 folder with one file", v2Folder, true, true},
		{"hybrid folder with one file", hybridFolder, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var (
				info *Info
				err  error
			)
			if tc.v2 {
				info, err = NewV2Info(tc.raw)
			} else {
				info, err = NewInfo(tc.raw, true, true)
			}
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if info.MultiFile != tc.want {
				t.Fatalf("MultiFile = %v, want %v (files=%v)", info.MultiFile, tc.want, info.Files)
			}
			if len(info.Files) != 1 {
				t.Fatalf("files = %v, want one", info.Files)
			}
		})
	}
}
