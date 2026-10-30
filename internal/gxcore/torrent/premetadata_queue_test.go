package torrent

import (
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
)

// TestPreMetadataQueueIsBounded pins the remote memory bound: before the
// metadata a peer's have/bitfield traffic is queued and replayed, and the size
// of a message is the peer's choice (a bitfield may declare up to the session
// message cap). Without a budget one connection could retain hundreds of MiB.
func TestPreMetadataQueueIsBounded(t *testing.T) {
	p := &peer.Peer{}
	big := peerprotocol.BitfieldMessage{Data: make([]byte, maxPreMetadataBytes/2)}
	if queuedMessageSize(big) != len(big.Data) {
		t.Fatalf("a bitfield must be sized by its payload, got %d", queuedMessageSize(big))
	}
	bigSize := queuedMessageSize(big)
	for i := 0; i < maxPreMetadataBytes/bigSize; i++ {
		if !preMetadataRoom(p, bigSize) {
			t.Fatalf("message %d was refused before the budget was full", i)
		}
		p.QueuedBytes += bigSize
		p.Messages = append(p.Messages, big)
	}
	if preMetadataRoom(p, bigSize) {
		t.Fatal("a message beyond the byte budget was accepted")
	}

	// The item count is bounded too, so a stream of tiny messages cannot grow
	// the slice without limit.
	small := &peer.Peer{}
	for i := 0; i < maxPreMetadataMessages; i++ {
		if !preMetadataRoom(small, 4) {
			t.Fatalf("message %d was refused before the count was full", i)
		}
		small.QueuedBytes += 4
		small.Messages = append(small.Messages, peerprotocol.HaveMessage{Index: uint32(i)})
	}
	if preMetadataRoom(small, 4) {
		t.Fatal("a message beyond the count budget was accepted")
	}
}

// TestQueuedMessageSizeKeepsHaveSmall keeps the accounting honest: a have costs
// four bytes, not the interface box.
func TestQueuedMessageSizeKeepsHaveSmall(t *testing.T) {
	if got := queuedMessageSize(peerprotocol.HaveMessage{}); got != 4 {
		t.Fatalf("have sized %d, want 4", got)
	}
	if got := queuedMessageSize(peerprotocol.HaveAllMessage{}); got != 4 {
		t.Fatalf("have-all sized %d, want 4", got)
	}
	if got := queuedMessageSize(peerprotocol.AllowedFastMessage{}); got != 4 {
		t.Fatalf("allowed-fast sized %d, want 4", got)
	}
}
