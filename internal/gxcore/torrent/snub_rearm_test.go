package torrent

import (
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecedownloader"
)

// A finished piece restarts the stall detector while the peer still has other
// pieces in flight, and stops it only when none is left: a peer that goes
// quiet with pieces outstanding must still be noticed.
func TestSnubTimerFollowsPiecesInFlight(t *testing.T) {
	pe := loopbackPeer(t, 7)
	tr := &torrent{pieceDownloaders: map[*peer.Peer]map[uint32]*piecedownloader.PieceDownloader{
		pe: {4: nil},
	}}
	tr.rearmSnubAfterPiece(pe)
	if !pe.SnubArmed() {
		t.Fatal("a piece still in flight left the stall detector stopped")
	}
	delete(tr.pieceDownloaders, pe)
	tr.rearmSnubAfterPiece(pe)
	if pe.SnubArmed() {
		t.Fatal("the stall detector kept running with no piece in flight")
	}
}
