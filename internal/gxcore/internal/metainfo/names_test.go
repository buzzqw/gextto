package metainfo

import (
	"path/filepath"
	"testing"

	"github.com/zeebo/bencode"
)

func infoPaths(t *testing.T, info *Info) []string {
	t.Helper()
	out := make([]string, len(info.Files))
	for i, f := range info.Files {
		out[i] = f.Path
	}
	return out
}

func assertPaths(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d paths, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestDuplicateNamesAreDeduped: names that differ only in case are two files on
// Linux but one on Windows. The later entry gets a -N suffix, before the
// extension, and a folder keeps it for every file under it.
func TestDuplicateNamesAreDeduped(t *testing.T) {
	entries := []any{
		map[string]any{"length": int64(10), "path": []string{"a.txt"}},
		map[string]any{"length": int64(10), "path": []string{"A.txt"}},
		map[string]any{"length": int64(10), "path": []string{"Docs", "x.bin"}},
		map[string]any{"length": int64(10), "path": []string{"docs", "y.bin"}},
	}
	info, err := NewInfo(craftedInfo(t, "root", entries), true, false)
	if err != nil {
		t.Fatalf("NewInfo: %v", err)
	}
	assertPaths(t, infoPaths(t, info), []string{
		filepath.Join("root", "a.txt"),
		filepath.Join("root", "A-1.txt"),
		filepath.Join("root", "Docs", "x.bin"),
		filepath.Join("root", "docs-1", "y.bin"),
	})
}

// TestExactDuplicateNamesAreDeduped: two identical paths in a torrent used to
// be refused; they are now renamed instead, so one piece cannot overwrite the
// other on disk.
func TestExactDuplicateNamesAreDeduped(t *testing.T) {
	entries := []any{
		map[string]any{"length": int64(10), "path": []string{"a.txt"}},
		map[string]any{"length": int64(20), "path": []string{"a.txt"}},
	}
	info, err := NewInfo(craftedInfo(t, "root", entries), true, false)
	if err != nil {
		t.Fatalf("NewInfo: %v", err)
	}
	assertPaths(t, infoPaths(t, info), []string{
		filepath.Join("root", "a.txt"),
		filepath.Join("root", "a-1.txt"),
	})
}

// TestDedupAvoidsAnExistingSuffix: the generated name must not collide with a
// literal "a-1.txt" already in the torrent.
func TestDedupAvoidsAnExistingSuffix(t *testing.T) {
	entries := []any{
		map[string]any{"length": int64(10), "path": []string{"a.txt"}},
		map[string]any{"length": int64(10), "path": []string{"A.txt"}},
		map[string]any{"length": int64(10), "path": []string{"a-1.txt"}},
	}
	info, err := NewInfo(craftedInfo(t, "root", entries), true, false)
	if err != nil {
		t.Fatalf("NewInfo: %v", err)
	}
	assertPaths(t, infoPaths(t, info), []string{
		filepath.Join("root", "a.txt"),
		filepath.Join("root", "A-1.txt"),
		filepath.Join("root", "a-1-1.txt"),
	})
}

// TestFileAndFolderCollidingByCase: "A" and "a/b" cannot coexist on a
// case-insensitive filesystem; the folder is the later entry and is renamed.
func TestFileAndFolderCollidingByCase(t *testing.T) {
	entries := []any{
		map[string]any{"length": int64(10), "path": []string{"A"}},
		map[string]any{"length": int64(10), "path": []string{"a", "b.bin"}},
	}
	info, err := NewInfo(craftedInfo(t, "root", entries), true, false)
	if err != nil {
		t.Fatalf("NewInfo: %v", err)
	}
	assertPaths(t, infoPaths(t, info), []string{
		filepath.Join("root", "A"),
		filepath.Join("root", "a-1", "b.bin"),
	})
}

// TestPaddingPathsAreNotDeduped: BEP 47 gives every pad file the same
// ".pad/<length>" path, and they must stay that way.
func TestPaddingPathsAreNotDeduped(t *testing.T) {
	entries := []any{
		map[string]any{"length": int64(10), "path": []string{"file.bin"}},
		map[string]any{"length": int64(6), "path": []string{".pad", "16384"}, "attr": "p"},
		map[string]any{"length": int64(6), "path": []string{".pad", "16384"}, "attr": "p"},
	}
	info, err := NewInfo(craftedInfo(t, "root", entries), true, true)
	if err != nil {
		t.Fatalf("NewInfo: %v", err)
	}
	assertPaths(t, infoPaths(t, info), []string{
		filepath.Join("root", "file.bin"),
		filepath.Join("root", ".pad", "16384"),
		filepath.Join("root", ".pad", "16384"),
	})
}

// TestV2DuplicateNamesAreDeduped: the same rule applies to a BEP 52 file tree.
func TestV2DuplicateNamesAreDeduped(t *testing.T) {
	info := map[string]any{
		"name":         "root",
		"piece length": 16384,
		"meta version": 2,
		"file tree": map[string]any{
			"root": map[string]any{
				"a.txt": map[string]any{"": map[string]any{"length": 10}},
				"A.txt": map[string]any{"": map[string]any{"length": 10}},
			},
		},
	}
	b, err := bencode.EncodeBytes(info)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	mi, err := NewV2Info(b)
	if err != nil {
		t.Fatalf("NewV2Info: %v", err)
	}
	assertPaths(t, infoPaths(t, mi), []string{
		filepath.Join("root", "A.txt"),
		filepath.Join("root", "a-1.txt"),
	})
}
