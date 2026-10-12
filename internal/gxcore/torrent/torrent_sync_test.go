package torrent

import (
	"io"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/allocator"
)

// syncRecorder is a storage.File that counts Sync calls.
type syncRecorder struct {
	syncs int
}

func (r *syncRecorder) ReadAt([]byte, int64) (int, error)      { return 0, io.EOF }
func (r *syncRecorder) WriteAt(p []byte, _ int64) (int, error) { return len(p), nil }
func (r *syncRecorder) Close() error                           { return nil }
func (r *syncRecorder) Sync() error                            { r.syncs++; return nil }

// syncPieces must flush every open data file, so the bitfield persisted right
// after it never refers to data that is still in the page cache.
func TestSyncPiecesFlushesEveryFile(t *testing.T) {
	a := &syncRecorder{}
	b := &syncRecorder{}
	tr := &torrent{files: []allocator.File{
		{Storage: a, Name: "a"},
		{Storage: b, Name: "b"},
	}}
	tr.syncPieces()
	if a.syncs != 1 || b.syncs != 1 {
		t.Fatalf("syncs = %d/%d, want 1/1", a.syncs, b.syncs)
	}
	// A torrent without open files must not panic.
	(&torrent{}).syncPieces()
}
