//go:build linux

package filestorage

import (
	"os"

	"golang.org/x/sys/unix"
)

// mapFile maps the whole file for shared read/write access.
func mapFile(f *os.File, size int) ([]byte, error) {
	return unix.Mmap(int(f.Fd()), 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
}

func unmapFile(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	return unix.Munmap(b)
}

// syncMappedRange flushes [begin, end) of the mapping to disk. msync requires
// the address to be page-aligned, so the range is widened to page boundaries
// (the mapping base is page-aligned, so the shifted slice stays aligned) and
// clamped to the mapping.
func syncMappedRange(data []byte, begin, end int64) error {
	if len(data) == 0 || begin >= end {
		return nil
	}
	page := int64(os.Getpagesize())
	begin = max(begin/page*page, 0)
	end = min((end+page-1)/page*page, int64(len(data)))
	if begin >= end {
		return nil
	}
	return unix.Msync(data[begin:end], unix.MS_SYNC)
}

// dataSync flushes file data (not metadata) to stable storage.
func dataSync(f *os.File) error {
	return unix.Fdatasync(int(f.Fd()))
}
