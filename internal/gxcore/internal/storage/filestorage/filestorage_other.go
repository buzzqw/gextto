//go:build !linux

package filestorage

import "os"

// applyNoAtimeFlag is a no-op off Linux: O_NOATIME is Linux-only.
func applyNoAtimeFlag(f int) int {
	return f
}

// withoutNoAtime reports that there is nothing to drop: O_NOATIME is Linux-only.
func withoutNoAtime(f int) (int, bool) {
	return f, false
}

// preallocate falls back to a sparse file: only Linux fallocate can reserve
// blocks up front.
func preallocate(f *os.File, size int64) error {
	return f.Truncate(size)
}
