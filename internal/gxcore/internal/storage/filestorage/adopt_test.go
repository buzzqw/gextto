package filestorage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile lays down a file of the given size with recognisable bytes.
func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, size)
	for i := range data {
		data[i] = 0xA5
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestOpenExtendsAShorterFile pins the harmless direction: a file left shorter
// than the torrent expects (a crash during preallocation) is extended, and the
// bytes already there survive.
func TestOpenExtendsAShorterFile(t *testing.T) {
	dest := t.TempDir()
	s, err := New(dest, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	name := "movie.mkv"
	path := filepath.Join(dest, name)
	writeFile(t, path, 100)

	f, _, err := s.Open(name, 4096)
	if err != nil {
		t.Fatalf("a shorter file was refused: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 4096 {
		t.Fatalf("the file was left at %d bytes, want 4096", info.Size())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if data[0] != 0xA5 || data[99] != 0xA5 {
		t.Fatal("extending the file overwrote the bytes already on disk")
	}
}

// TestOpenRefusesToTruncateALongerFile is the rule other engines fixed in
// 2026: bytes past the torrent's expectation are not ours, so adding a torrent
// must never cut them. The file stays exactly as it was and the add fails.
func TestOpenRefusesToTruncateALongerFile(t *testing.T) {
	dest := t.TempDir()
	s, err := New(dest, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join("folder", "movie.mkv")
	path := filepath.Join(dest, name)
	writeFile(t, path, 9000)

	_, _, err = s.Open(name, 4096)
	if err == nil {
		t.Fatal("a longer file was silently truncated")
	}
	for _, want := range []string{name, "9000", "4096"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the error does not name %q: %v", want, err)
		}
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Size() != 9000 {
		t.Fatalf("the file was changed to %d bytes; nothing on disk may change", info.Size())
	}
}

// TestOpenAdoptsAFileOfTheRightSize pins the normal case: a complete file of a
// torrent added again is opened as it is.
func TestOpenAdoptsAFileOfTheRightSize(t *testing.T) {
	dest := t.TempDir()
	s, err := New(dest, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	name := "movie.mkv"
	path := filepath.Join(dest, name)
	writeFile(t, path, 4096)

	f, _, err := s.Open(name, 4096)
	if err != nil {
		t.Fatalf("a file of the expected size was refused: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 4096 {
		t.Fatalf("the file changed to %d bytes, want 4096", info.Size())
	}
}
