// Package filestorage stores torrent files on disk: plain OS files with
// fdatasync durability, or shared memory mappings on Linux, under one root.
package filestorage

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/buzzqw/gextto/internal/gxcore/internal/storage"
)

// FileStorage opens torrent files under a root directory.
type FileStorage struct {
	// Preallocate reserves the full size of new files.
	Preallocate bool
	// Mmap serves files from a shared mapping (Linux, with plain-file
	// fallback elsewhere or when the mapping fails).
	Mmap bool
	dest string
	perm fs.FileMode
}

// New roots a storage at dest.
func New(dest string, perm fs.FileMode) (*FileStorage, error) {
	abs, err := filepath.Abs(dest)
	if err != nil {
		return nil, err
	}
	return &FileStorage{dest: abs, perm: perm}, nil
}

var _ storage.Storage = (*FileStorage)(nil)

// Open opens name under the root, creating parents and the file as needed.
// New files are preallocated or sparsely truncated per configuration;
// existing files of the wrong size are resized. Writes stay un-synced: the
// engine batches durability into storage.File.Sync at resume-bitfield time.
func (s *FileStorage) Open(name string, size int64) (f storage.File, exists bool, err error) {
	name = filepath.Join(s.dest, filepath.Clean(name))
	if err = os.MkdirAll(filepath.Dir(name), os.ModeDir|s.perm); err != nil {
		return nil, false, err
	}
	of, opened, err := s.openFile(name, size)
	if err != nil {
		return nil, false, err
	}
	defer func() {
		if err != nil {
			_ = of.Close()
			f = nil
			return
		}
		if rerr := disableReadAhead(of); rerr != nil {
			_ = of.Close()
			err = rerr
			f = nil
			return
		}
		f = s.wrap(of, size)
	}()
	return nil, opened, nil
}

// openFile opens the OS file, creating and sizing it on first sight.
func (s *FileStorage) openFile(name string, size int64) (*os.File, bool, error) {
	mode := s.perm &^ 0111
	flags := applyNoAtimeFlag(os.O_RDWR)
	of, err := os.OpenFile(name, flags, mode)
	if err == nil {
		return s.adoptExisting(of, size)
	}
	if !os.IsNotExist(err) {
		return nil, false, err
	}
	of, err = os.OpenFile(name, flags|os.O_CREATE, mode)
	if err != nil {
		return nil, false, err
	}
	if s.Preallocate {
		err = preallocate(of, size)
	} else {
		err = of.Truncate(size)
	}
	if err != nil {
		_ = of.Close()
		return nil, false, err
	}
	return of, false, nil
}

// adoptExisting resizes an existing file that drifted from the expected size.
func (s *FileStorage) adoptExisting(of *os.File, size int64) (*os.File, bool, error) {
	fi, err := of.Stat()
	if err != nil {
		_ = of.Close()
		return nil, false, err
	}
	if fi.Size() != size {
		err = of.Truncate(size)
	}
	return of, true, err
}

// RootDir returns the storage root.
func (s *FileStorage) RootDir() string {
	return s.dest
}

// wrap backs an opened file with a memory mapping when configured (Linux,
// sized mappings only), else with a plain fdatasync file.
func (s *FileStorage) wrap(of *os.File, size int64) storage.File {
	if s.Mmap && size > 0 && size <= int64(maxInt()) {
		if data, err := mapFile(of, int(size)); err == nil {
			return &mmapFile{f: of, data: data}
		}
	}
	return osFile{of}
}

func maxInt() int { return int(^uint(0) >> 1) }
