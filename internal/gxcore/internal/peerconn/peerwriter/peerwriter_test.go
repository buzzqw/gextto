package peerwriter

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
)

// TestServedWindowBounded verifies the engine change: the dedup window is
// bounded, evicts the oldest, and stops rejecting blocks re-requested later.
func TestServedWindowBounded(t *testing.T) {
	w := newServedWindow(3)
	r := func(i uint32) peerprotocol.RequestMessage {
		return peerprotocol.RequestMessage{Index: i}
	}

	if w.seen(r(0)) {
		t.Fatal("first sight of block 0 reported as duplicate")
	}
	if !w.seen(r(0)) {
		t.Fatal("immediate re-request of block 0 not reported as duplicate")
	}
	w.seen(r(1))
	w.seen(r(2))

	// Adding a fourth entry evicts the oldest (block 0).
	w.seen(r(3))
	if w.seen(r(0)) {
		t.Fatal("block 0 still within the window after eviction")
	}
	if !w.seen(r(2)) {
		t.Fatal("block 2 was evicted too early")
	}

	// It never grows without bound.
	for i := uint32(0); i < 5000; i++ {
		w.seen(r(i))
	}
	if len(w.m) > 3 || len(w.q) > 3 {
		t.Fatalf("window grew to map=%d queue=%d, want <= 3", len(w.m), len(w.q))
	}
}

// queuedKinds lists the staged messages: "piece:<index>", "reject:<index>",
// or the message name.
func queuedKinds(w *PeerWriter) []string {
	var out []string
	for e := w.writeQueue.Front(); e != nil; e = e.Next() {
		switch m := e.Value.(type) {
		case Piece:
			out = append(out, "piece:"+strconv.Itoa(int(m.Index)))
		case peerprotocol.RejectMessage:
			out = append(out, "reject:"+strconv.Itoa(int(m.Index)))
		default:
			out = append(out, m.(peerprotocol.Message).ID().String())
		}
	}
	return out
}

// With the fast extension a choke must not drop requests in silence (BEP 6):
// each one not served is rejected, and allowed-fast pieces stay queued.
func TestChokeRejectsQueuedUploadsWithFast(t *testing.T) {
	w := New(nil, logger.New("test"), 250, true, nil)
	src := bytes.NewReader(make([]byte, 16384))
	w.queueMessage(peerprotocol.AllowedFastMessage{HaveMessage: peerprotocol.HaveMessage{Index: 7}})
	w.queueMessage(Piece{Data: src, RequestMessage: peerprotocol.RequestMessage{Index: 3, Length: 16384}})
	w.queueMessage(Piece{Data: src, RequestMessage: peerprotocol.RequestMessage{Index: 7, Length: 16384}})
	w.queueMessage(Piece{Data: src, RequestMessage: peerprotocol.RequestMessage{Index: 5, Length: 16384}})
	w.queueMessage(peerprotocol.ChokeMessage{})
	got := strings.Join(queuedKinds(w), " ")
	want := "allowed fast reject:3 piece:7 reject:5 choke"
	if got != want {
		t.Fatalf("queue after choke: %q, want %q", got, want)
	}
	if w.currentQueuedRequests != 1 {
		t.Fatalf("%d uploads counted, want 1", w.currentQueuedRequests)
	}
}

// Without the fast extension the choke itself drops the requests.
func TestChokeDropsQueuedUploadsWithoutFast(t *testing.T) {
	w := New(nil, logger.New("test"), 250, false, nil)
	src := bytes.NewReader(make([]byte, 16384))
	w.queueMessage(Piece{Data: src, RequestMessage: peerprotocol.RequestMessage{Index: 3, Length: 16384}})
	w.queueMessage(peerprotocol.ChokeMessage{})
	if got := strings.Join(queuedKinds(w), " "); got != "choke" {
		t.Fatalf("queue after choke: %q, want only the choke", got)
	}
	if w.currentQueuedRequests != 0 {
		t.Fatalf("%d uploads counted, want 0", w.currentQueuedRequests)
	}
}
