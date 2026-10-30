package metainfo

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zeebo/bencode"
)

// craftedInfo bencodes one info dict with the given name and file entries.
func craftedInfo(t *testing.T, name string, entries []any) []byte {
	t.Helper()
	info := map[string]any{
		"name":         name,
		"piece length": int64(16384),
		"pieces":       make([]byte, 20),
		"files":        entries,
	}
	b, err := bencode.EncodeBytes(info)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return b
}

// TestNewInfoRefusesEscapingName pins the fix for the download-folder escape:
// the info name is the first component of every storage path and is not
// screened like the path entries, so a name of ".." used to yield files above
// the download folder ("../evil.bin"). Other odd names are sanitized by
// cleanName and stay inside: they must still be accepted.
func TestNewInfoRefusesEscapingName(t *testing.T) {
	entries := []any{map[string]any{"length": int64(10), "path": []string{"evil.bin"}}}
	if _, err := NewInfo(craftedInfo(t, "..", entries), true, true); err == nil {
		t.Errorf("name %q was accepted, expected a refusal", "..")
	}
	for _, name := range []string{".", "/", "../.."} {
		info, err := NewInfo(craftedInfo(t, name, entries), true, true)
		if err != nil {
			t.Errorf("name %q was refused: %v", name, err)
			continue
		}
		if !filepath.IsLocal(info.Files[0].Path) {
			t.Errorf("name %q escaped to %q", name, info.Files[0].Path)
		}
	}
}

// TestNewInfoRefusesEscapingSingleFile covers the single-file layout, where the
// name is the whole storage path.
func TestNewInfoRefusesEscapingSingleFile(t *testing.T) {
	info := map[string]any{
		"name":         "..",
		"piece length": int64(16384),
		"pieces":       make([]byte, 20),
		"length":       int64(10),
	}
	b, err := bencode.EncodeBytes(info)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := NewInfo(b, true, true); err == nil {
		t.Fatal("a single-file torrent named \"..\" was accepted")
	}
}

// TestNewInfoRefusesEscapingPathEntry keeps the per-entry screen honest: a
// ".." component in the file list is refused as before.
func TestNewInfoRefusesEscapingPathEntry(t *testing.T) {
	entries := []any{map[string]any{"length": int64(10), "path": []string{"..", "evil.bin"}}}
	if _, err := NewInfo(craftedInfo(t, "pack", entries), true, true); err == nil {
		t.Fatal("a \"..\" path entry was accepted")
	}
}

// TestNewInfoKeepsOrdinaryNames makes sure the new screen does not refuse the
// names a real torrent carries, including a backslash in a file name on the
// platforms where it is an ordinary character.
func TestNewInfoKeepsOrdinaryNames(t *testing.T) {
	names := []string{"Ubuntu 24.04 amd64", "a.b.c", "...", "name with spaces"}
	if runtime.GOOS != "windows" {
		names = append(names, `back\slash`)
	}
	entries := []any{map[string]any{"length": int64(10), "path": []string{"file.bin"}}}
	for _, name := range names {
		info, err := NewInfo(craftedInfo(t, name, entries), true, true)
		if err != nil {
			t.Errorf("name %q was refused: %v", name, err)
			continue
		}
		if len(info.Files) != 1 || !strings.HasPrefix(info.Files[0].Path, name) {
			t.Errorf("name %q: unexpected path %q", name, info.Files[0].Path)
		}
	}
}

// TestSetFilePathsRefusesEscapingPaths covers the resume override (a rename
// written by an older version, a hand-edited database).
func TestSetFilePathsRefusesEscapingPaths(t *testing.T) {
	entries := []any{map[string]any{"length": int64(10), "path": []string{"a.bin"}}}
	info, err := NewInfo(craftedInfo(t, "pack", entries), true, true)
	if err != nil {
		t.Fatalf("NewInfo: %v", err)
	}
	if info.SetFilePaths([]string{"../outside.bin"}) {
		t.Fatal("SetFilePaths applied a path above the storage root")
	}
	if info.SetFilePaths([]string{"/abs.bin"}) {
		t.Fatal("SetFilePaths applied an absolute path")
	}
	if info.SetFilePaths([]string{"folder/a.bin"}) != true {
		t.Fatal("SetFilePaths refused a correct relative path")
	}
}
