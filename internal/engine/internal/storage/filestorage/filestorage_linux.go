package filestorage

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func disableReadAhead(f *os.File) error {
	return unix.Fadvise(int(f.Fd()), 0, 0, unix.FADV_RANDOM)
}

func applyNoAtimeFlag(f int) int {
	return f | syscall.O_NOATIME
}

// preallocate reserves the whole file on disk (gextto fork); filesystems
// without fallocate fall back to a sparse file. On tmpfs (a RAM disk) the file
// stays sparse: reserving it would take the whole size from RAM at once, even
// for a torrent that is about to be relocated because it does not fit.
func preallocate(f *os.File, size int64) error {
	if size <= 0 {
		return nil
	}
	var st unix.Statfs_t
	if err := unix.Fstatfs(int(f.Fd()), &st); err == nil && st.Type == unix.TMPFS_MAGIC {
		return f.Truncate(size)
	}
	if err := unix.Fallocate(int(f.Fd()), 0, 0, size); err == nil {
		return nil
	}
	return f.Truncate(size)
}
