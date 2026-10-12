package filestorage

import (
	"io"
	"os"
	"sync"

	"github.com/buzzqw/gextto/internal/gxcore/internal/storage"
)

// osFile wraps the OS file so Sync uses fdatasync (piece data needs no metadata
// flush) instead of fsync. ReadAt/WriteAt/Close come from the embedded file.
type osFile struct{ *os.File }

var _ storage.File = osFile{}

func (f osFile) Sync() error { return dataSync(f.File) }

// dirtyRange is a half-open byte interval [begin, end) written into the
// mapping since the last Sync.
type dirtyRange struct{ begin, end int64 }

// maxDirtyRanges bounds the tracked intervals. Piece writes are large and
// mostly sequential, so the list stays tiny; scattered writes that fill it are
// coalesced into a single interval (a wider msync is still correct, just less
// selective), so the bookkeeping can never grow without bound.
const maxDirtyRanges = 64

// mmapFile is an mmap-backed storage.File on Linux: ReadAt/WriteAt copy into the
// mapped memory and the kernel writes the dirty pages back asynchronously, so a
// piece write is a memcpy instead of a pwrite syscall.
//
// Sync flushes only the byte ranges written since the previous Sync, not the
// whole mapping. The session persists the resume bitfield every
// ResumeWriteInterval (30 s) and Syncs for durability beforehand, so a
// whole-file msync would rewrite every dirty page of a multi-GB file on every
// tick; a ranged msync keeps the cost proportional to what was downloaded.
type mmapFile struct {
	f    *os.File
	data []byte
	// dirty holds the ranges written since the last Sync, in write order and
	// non-overlapping. A piece writer goroutine appends to it while the torrent
	// run loop calls Sync, so it is guarded by mu.
	mu    sync.Mutex
	dirty []dirtyRange
}

var _ storage.File = (*mmapFile)(nil)

func (m *mmapFile) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off > int64(len(m.data)) {
		return 0, io.EOF
	}
	n := copy(p, m.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (m *mmapFile) WriteAt(p []byte, off int64) (int, error) {
	if off < 0 || off+int64(len(p)) > int64(len(m.data)) {
		return 0, os.ErrInvalid
	}
	n := copy(m.data[off:], p)
	m.markDirty(off, off+int64(n))
	return n, nil
}

// markDirty records [begin, end) as written. The fast path extends the last
// interval, which covers the sequential piece writes that dominate; anything
// else is appended and, once the list is full, coalesced into one interval.
func (m *mmapFile) markDirty(begin, end int64) {
	if begin >= end {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if n := len(m.dirty); n > 0 {
		last := &m.dirty[n-1]
		if begin <= last.end {
			if end > last.end {
				last.end = end
			}
			return
		}
	}
	if len(m.dirty) >= maxDirtyRanges {
		lo, hi := m.dirty[0].begin, m.dirty[0].end
		for _, r := range m.dirty[1:] {
			if r.begin < lo {
				lo = r.begin
			}
			if r.end > hi {
				hi = r.end
			}
		}
		lo, hi = min(lo, begin), max(hi, end)
		m.dirty = append(m.dirty[:0], dirtyRange{lo, hi})
		return
	}
	m.dirty = append(m.dirty, dirtyRange{begin, end})
}

func (m *mmapFile) Sync() error {
	// Take ownership of the pending ranges so a concurrent write marks its own
	// range independently while the flush runs.
	m.mu.Lock()
	ranges := m.dirty
	m.dirty = nil
	m.mu.Unlock()
	for i, r := range ranges {
		if err := syncMappedRange(m.data, r.begin, r.end); err != nil {
			// Put the unflushed ranges back so the next Sync retries them.
			for _, pending := range ranges[i:] {
				m.markDirty(pending.begin, pending.end)
			}
			return err
		}
	}
	return nil
}

func (m *mmapFile) Close() error {
	err := unmapFile(m.data)
	if e := m.f.Close(); err == nil {
		err = e
	}
	return err
}
