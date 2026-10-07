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
// without fallocate fall back to a sparse file.
func preallocate(f *os.File, size int64) error {
	if size <= 0 {
		return nil
	}
	if err := unix.Fallocate(int(f.Fd()), 0, 0, size); err == nil {
		return nil
	}
	return f.Truncate(size)
}
