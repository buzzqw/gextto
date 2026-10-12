package filestorage

import (
	"io"
	"os"

	"github.com/buzzqw/gextto/internal/gxcore/internal/storage"
)

// osFile wraps the OS file so Sync uses fdatasync (piece data needs no metadata
// flush) instead of fsync. ReadAt/WriteAt/Close come from the embedded file.
type osFile struct{ *os.File }

var _ storage.File = osFile{}

func (f osFile) Sync() error { return dataSync(f.File) }

// mmapFile is an mmap-backed storage.File on Linux: ReadAt/WriteAt copy into the
// mapped memory and the kernel writes the dirty pages back asynchronously, so a
// piece write is a memcpy instead of a pwrite syscall. Sync uses msync.
type mmapFile struct {
	f    *os.File
	data []byte
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
	return copy(m.data[off:], p), nil
}

func (m *mmapFile) Sync() error { return syncMapped(m.data) }

func (m *mmapFile) Close() error {
	err := unmapFile(m.data)
	if e := m.f.Close(); err == nil {
		err = e
	}
	return err
}
