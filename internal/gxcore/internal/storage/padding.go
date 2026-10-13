package storage

// PaddingFile stands in for the zero-filled alignment gaps between files.
// It serves endless zeroes on read, refuses writes (padding must never reach
// storage), and syncs trivially.
type PaddingFile struct{}

func NewPaddingFile(length int64) File {
	return PaddingFile{}
}

var _ File = PaddingFile{}

func (f PaddingFile) ReadAt(p []byte, off int64) (n int, err error) {
	// Zero the span: old clients without padding support still verify
	// piece hashes computed over zero-filled gaps.
	clear(p)
	return len(p), nil
}

func (f PaddingFile) WriteAt(p []byte, off int64) (n int, err error) {
	panic("attempt to write padding file")
}

func (f PaddingFile) Close() error {
	return nil
}

// Sync is a no-op: padding is never written.
func (f PaddingFile) Sync() error {
	return nil
}
