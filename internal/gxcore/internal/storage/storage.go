// Package storage contains an interface for reading and writing files in a torrent.
package storage

import "io"

// Storage is an interface for reading/writing torrent files.
type Storage interface {
	// Open a file. If the file does not exist, it will be created.
	Open(name string, size int64) (f File, exists bool, err error)
	// RootDir is the absolute path of the storage root.
	RootDir() string
}

// File interface for reading/writing torrent data.
type File interface {
	io.ReaderAt
	io.WriterAt
	io.Closer
	// Sync flushes the file's data to stable storage. The engine calls it
	// before persisting the resume bitfield so a bit that survives a crash
	// always refers to data already on disk.
	Sync() error
}

// Provider is a torrent storage provider.
type Provider interface {
	// GetStorage returns a storage for a torrent.
	GetStorage(torrentID string) (Storage, error)
}
