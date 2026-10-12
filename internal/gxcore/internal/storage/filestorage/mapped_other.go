//go:build !linux

package filestorage

import "os"

// mmap storage is a Linux optimization; elsewhere the storage falls back to
// plain OS files and Sync uses fsync.

func mapFile(f *os.File, size int) ([]byte, error) {
	return nil, os.ErrInvalid
}

func unmapFile(b []byte) error { return nil }

func syncMapped(b []byte) error { return nil }

func dataSync(f *os.File) error { return f.Sync() }
