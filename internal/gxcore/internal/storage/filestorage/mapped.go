package filestorage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/storage"
)

// osFile wraps the OS file so Sync uses fdatasync (piece data needs no metadata
// flush) instead of fsync.
//
// dirty records a write since the last Sync: the resume tick syncs every open
// file of every torrent, and most of them are untouched between two ticks. A
// clean file needs no fdatasync. With thousands of files per torrent (or a
// network share) that is the difference between a handful of syscalls and tens
// of thousands every interval, on the torrent's run loop.
//
// A read that fails with a transient I/O error (EIO, or a stale handle on NFS)
// reopens the file and tries again for about a second before giving up: a NAS,
// SMB or FUSE mount often serves the same bytes through a fresh open, and the
// error would otherwise stop the torrent as if the file were damaged.
type osFile struct {
	// mu guards file: reads and writes share it, a reopen swaps it.
	mu    sync.RWMutex
	file  *os.File
	name  string
	flags int
	dirty atomic.Bool
}

var _ storage.File = (*osFile)(nil)

// readRetryDelays are the waits before each reopen-and-retry of a read that
// failed with a transient error: about a second in all.
var readRetryDelays = []time.Duration{100 * time.Millisecond, 300 * time.Millisecond, 600 * time.Millisecond}

func newOSFile(of *os.File, flags int) *osFile {
	return &osFile{file: of, name: of.Name(), flags: flags}
}

// ReadAt reads from the file, reopening it after a transient error.
func (f *osFile) ReadAt(p []byte, off int64) (int, error) {
	n, err := f.readAt(p, off)
	if err == nil || !isTransientReadError(err) || f.name == "" {
		return n, err
	}
	for _, delay := range readRetryDelays {
		time.Sleep(delay)
		if reopenErr := f.reopen(); reopenErr != nil {
			continue
		}
		if n, err = f.readAt(p, off); err == nil || !isTransientReadError(err) {
			return n, err
		}
	}
	return n, err
}

// DropCache drops the OS cache pages of a range just checked, so a recheck of
// a large torrent does not push the rest of the machine out of memory.
func (f *osFile) DropCache(off, length int64) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	dropFileCache(f.file, off, length)
}

func (f *osFile) readAt(p []byte, off int64) (int, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return readFileAt(f.file, p, off)
}

// readFileAt is the plain positional read; tests replace it to inject errors.
var readFileAt = func(file *os.File, p []byte, off int64) (int, error) {
	return file.ReadAt(p, off)
}

// reopen replaces the handle with a fresh open of the same path. Unflushed
// writes are flushed first, so swapping the handle loses nothing.
func (f *osFile) reopen() error {
	fresh, err := openForWrite(f.name, f.flags, 0)
	if err != nil {
		return err
	}
	f.mu.Lock()
	old := f.file
	if f.dirty.Load() {
		_ = dataSync(old)
	}
	f.file = fresh
	f.mu.Unlock()
	return old.Close()
}

// WriteAt records that the file has bytes to flush.
func (f *osFile) WriteAt(p []byte, off int64) (int, error) {
	f.mu.RLock()
	n, err := f.file.WriteAt(p, off)
	f.mu.RUnlock()
	if n > 0 {
		f.dirty.Store(true)
	}
	return n, err
}

// Sync flushes only when something was written; a failed flush stays dirty so
// the next tick retries it instead of persisting the bitfield over unflushed
// data.
func (f *osFile) Sync() error {
	if !f.dirty.Swap(false) {
		return nil
	}
	f.mu.RLock()
	err := dataSync(f.file)
	f.mu.RUnlock()
	if err != nil {
		f.dirty.Store(true)
		return err
	}
	return nil
}

// Close closes the current handle.
func (f *osFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.file.Close()
}

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

// errMappedFault is returned when touching the mapping faults: the kernel
// raises SIGBUS when a page cannot be backed (disk full on a sparse file, or
// the file truncated by another process). Without the guard the fault would
// crash the whole daemon; with it only the torrent sees an I/O error.
var errMappedFault = errors.New("mmap access fault (disk full or file truncated)")

// recoverFault turns a memory fault raised in the calling function into an
// error and restores the previous SetPanicOnFault setting. It must be the
// deferred function itself (recover only works there):
//
//	defer recoverFault(debug.SetPanicOnFault(true), &err)
func recoverFault(old bool, err *error) {
	debug.SetPanicOnFault(old)
	if r := recover(); r != nil {
		if _, ok := r.(interface{ Addr() uintptr }); !ok {
			panic(r)
		}
		*err = fmt.Errorf("%w: %v", errMappedFault, r)
	}
}

func (m *mmapFile) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 || off > int64(len(m.data)) {
		return 0, io.EOF
	}
	defer recoverFault(debug.SetPanicOnFault(true), &err)
	n = copy(p, m.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (m *mmapFile) WriteAt(p []byte, off int64) (n int, err error) {
	if off < 0 || off+int64(len(p)) > int64(len(m.data)) {
		return 0, os.ErrInvalid
	}
	defer recoverFault(debug.SetPanicOnFault(true), &err)
	n = copy(m.data[off:], p)
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
		// Overlapping or adjacent on either side: widen the last interval.
		// Concurrent piece writes (write ring buffer) land out of order, so a
		// write just before the last interval must widen its start too, or
		// its bytes would never be flushed.
		if begin <= last.end && end >= last.begin {
			last.begin = min(last.begin, begin)
			last.end = max(last.end, end)
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
