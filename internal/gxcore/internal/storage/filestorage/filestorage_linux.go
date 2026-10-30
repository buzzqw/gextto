package filestorage

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// disableReadAhead hints random access so the page cache does not prefetch
// sequentially across multi-gigabyte torrent files.
func disableReadAhead(f *os.File) error {
	return unix.Fadvise(int(f.Fd()), 0, 0, unix.FADV_RANDOM)
}

// applyNoAtimeFlag opens without atime updates: torrent workloads rewrite
// metadata otherwise, pointlessly dirtying inodes on every read.
func applyNoAtimeFlag(f int) int {
	return f | syscall.O_NOATIME
}

// withoutNoAtime drops O_NOATIME (ok is false when the flag is absent): used to
// retry an open the kernel refused with EPERM on a file we do not own.
func withoutNoAtime(f int) (int, bool) {
	if f&syscall.O_NOATIME == 0 {
		return f, false
	}
	return f &^ syscall.O_NOATIME, true
}

// preallocate reserves the file up front so a nearly-full disk fails fast
// at add time instead of mid-download. Filesystems without fallocate — and
// tmpfs, where reserving would pin the whole size in RAM at once even for a
// torrent about to be relocated for not fitting — fall back to sparse.
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
