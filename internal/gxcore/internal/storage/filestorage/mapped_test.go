package filestorage

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// TestMmapRoundTrip checks the mmap-backed storage: writes land in the mapping,
// reads see them, out-of-range writes are rejected and the data is on disk after
// Sync and Close.
func TestMmapRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	s.Mmap = true
	f, _, err := s.Open("m.bin", 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("abcdefgh"), 2); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 10)
	if n, err := f.ReadAt(buf, 0); err != nil && err != io.EOF {
		t.Fatalf("ReadAt: %v (n=%d)", err, n)
	}
	if string(buf) != "\x00\x00abcdefgh" {
		t.Fatalf("mapped read = %q, want 00 00 abcdefgh", buf)
	}
	if _, err := f.WriteAt([]byte("xx"), 9); err == nil {
		t.Fatal("a write past the end must fail")
	}
	if err := f.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "m.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "\x00\x00abcdefgh" {
		t.Fatalf("persisted = %q, want 00 00 abcdefgh", got)
	}
}

// TestMmapFallsBackOnZeroSize keeps a zero-length file working (mmap cannot map
// zero bytes).
func TestMmapFallsBackOnZeroSize(t *testing.T) {
	s, err := New(t.TempDir(), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	s.Mmap = true
	f, _, err := s.Open("empty.bin", 0)
	if err != nil {
		t.Fatalf("Open zero-size with mmap: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkStorageWrite(b *testing.B)     { benchStorageWrite(b, false) }
func BenchmarkStorageWriteMmap(b *testing.B) { benchStorageWrite(b, true) }

func benchStorageWrite(b *testing.B, mmap bool) {
	s, err := New(b.TempDir(), 0o644)
	if err != nil {
		b.Fatal(err)
	}
	s.Mmap = mmap
	const size = 64 << 20
	f, _, err := s.Open("bench.bin", size)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, 256<<10)
	b.SetBytes(int64(len(buf)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		off := int64((i * len(buf)) % (size - len(buf)))
		if _, err := f.WriteAt(buf, off); err != nil {
			b.Fatal(err)
		}
	}
}
