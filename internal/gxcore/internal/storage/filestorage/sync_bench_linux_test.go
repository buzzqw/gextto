//go:build linux

package filestorage

import (
	"testing"

	"golang.org/x/sys/unix"
)

// benchMmapSync compares flushing a small dirty region against flushing the
// whole mapping, which is what the engine used to do on every resume tick.
// whole=true flushes the mapping directly; whole=false uses the ranged Sync.
func benchMmapSync(b *testing.B, whole bool) {
	s, err := New(b.TempDir(), 0o644)
	if err != nil {
		b.Fatal(err)
	}
	s.Mmap = true
	const size = 1 << 30
	f, _, err := s.Open("bench.bin", size)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	mf := f.(*mmapFile)
	piece := make([]byte, 256<<10)
	off := int64(size - len(piece))
	b.SetBytes(int64(len(piece)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := f.WriteAt(piece, off); err != nil {
			b.Fatal(err)
		}
		if whole {
			if err := unix.Msync(mf.data, unix.MS_SYNC); err != nil {
				b.Fatal(err)
			}
			mf.mu.Lock()
			mf.dirty = mf.dirty[:0]
			mf.mu.Unlock()
		} else if err := f.Sync(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMmapSyncRanged(b *testing.B) { benchMmapSync(b, false) }
func BenchmarkMmapSyncWhole(b *testing.B)  { benchMmapSync(b, true) }

// benchMmapSyncClean isolates the page-walk cost that the engine paid on every
// resume tick for files with no writes since the previous tick: the whole-file
// msync still walks the mapping, while the ranged Sync is a no-op.
func benchMmapSyncClean(b *testing.B, whole bool) {
	s, err := New(b.TempDir(), 0o644)
	if err != nil {
		b.Fatal(err)
	}
	s.Mmap = true
	const size = 512 << 20
	f, _, err := s.Open("bench.bin", size)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	mf := f.(*mmapFile)
	// Populate the whole mapping so its pages are present in the page cache,
	// then flush, leaving a large clean mapping, as a finished or paused file.
	page := make([]byte, 1<<20)
	for off := int64(0); off < size; off += int64(len(page)) {
		if _, err := f.WriteAt(page, off); err != nil {
			b.Fatal(err)
		}
	}
	if err := f.Sync(); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(size)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if whole {
			if err := unix.Msync(mf.data, unix.MS_SYNC); err != nil {
				b.Fatal(err)
			}
		} else if err := f.Sync(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMmapSyncRangedClean(b *testing.B) { benchMmapSyncClean(b, false) }
func BenchmarkMmapSyncWholeClean(b *testing.B)  { benchMmapSyncClean(b, true) }
