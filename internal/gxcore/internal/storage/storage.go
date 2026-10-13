// Package storage defines how the engine reads and writes torrent files:
// a Storage opens named files, a Provider hands out one Storage per torrent.
package storage

import "io"

// Storage opens and locates the files of one torrent.
type Storage interface {
	// Open returns the file, creating it when missing. exists reports
	// whether the file was already there.
	Open(name string, size int64) (f File, exists bool, err error)
	// RootDir is the absolute storage root.
	RootDir() string
}

// File reads, writes and persists torrent bytes.
type File interface {
	io.ReaderAt
	io.WriterAt
	io.Closer
	// Sync flushes data to stable storage. The engine syncs before
	// persisting the resume bitfield, so a bit that survives a crash always
	// refers to bytes already on disk.
	Sync() error
}

// Provider issues one Storage per torrent id.
type Provider interface {
	// GetStorage returns the storage for a torrent.
	GetStorage(torrentID string) (Storage, error)
}
