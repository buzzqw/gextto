//go:build !linux

package filestorage

import "os"

// disableReadAhead is a no-op off Linux: only fadvise knows how to hint the
// page cache against readahead.
func disableReadAhead(f *os.File) error {
	return nil
}

// applyNoAtimeFlag is a no-op off Linux: O_NOATIME is Linux-only.
func applyNoAtimeFlag(f int) int {
	return f
}

// preallocate falls back to a sparse file: only Linux fallocate can reserve
// blocks up front.
func preallocate(f *os.File, size int64) error {
	return f.Truncate(size)
}
