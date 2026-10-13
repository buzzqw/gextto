package torrent

import (
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/suspendchan"
	"github.com/buzzqw/gextto/internal/gxcore/internal/urldownloader"
)

// TestPieceWriteRingBufferBoundsAndResumes: the piece channels stay open while
// fewer than maxPendingWrites pieces are being written, are suspended at the
// cap, and resume as soon as the ring buffer has room; draining never
// underflows.
func TestPieceWriteRingBufferBoundsAndResumes(t *testing.T) {
	tr := &torrent{
		pieceMessagesC:      suspendchan.New[peer.PieceMessage](0),
		webseedPieceResultC: suspendchan.New[*urldownloader.PieceResult](0),
	}
	for i := 0; i < maxPendingWrites-1; i++ {
		tr.beginPieceWrite()
	}
	if tr.pieceMessagesC.ReceiveC() == nil || tr.webseedPieceResultC.ReceiveC() == nil {
		t.Fatal("channels suspended below the cap")
	}
	tr.beginPieceWrite()
	if tr.pieceMessagesC.ReceiveC() != nil || tr.webseedPieceResultC.ReceiveC() != nil {
		t.Fatal("channels must be suspended at the cap")
	}
	if tr.pendingWrites != maxPendingWrites {
		t.Fatalf("pendingWrites = %d, want %d", tr.pendingWrites, maxPendingWrites)
	}
	tr.endPieceWrite()
	if tr.pieceMessagesC.ReceiveC() == nil || tr.webseedPieceResultC.ReceiveC() == nil {
		t.Fatal("channels must resume below the cap")
	}
	for i := 0; i < maxPendingWrites+2; i++ {
		tr.endPieceWrite()
	}
	if tr.pendingWrites != 0 {
		t.Fatalf("pendingWrites = %d, want 0", tr.pendingWrites)
	}
}
