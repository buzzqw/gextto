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

func syncMapped(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	return unix.Msync(b, unix.MS_SYNC)
}

// dataSync flushes file data (not metadata) to stable storage.
func dataSync(f *os.File) error {
	return unix.Fdatasync(int(f.Fd()))
}
