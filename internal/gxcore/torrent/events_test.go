package torrent

import (
	"strings"
	"testing"
	"time"
)

func TestEventBusSubscribeAndCancel(t *testing.T) {
	s := &Session{}
	ch, cancel := s.Subscribe()

	s.emit(Event{Type: EventTorrentAdded, Torrent: "abc"})
	select {
	case ev := <-ch:
		if ev.Type != EventTorrentAdded || ev.Torrent != "abc" {
			t.Fatalf("got %+v", ev)
		}
		if ev.At.IsZero() {
			t.Error("event timestamp not set")
		}
	case <-time.After(time.Second):
		t.Fatal("no event received")
	}

	cancel()
	if _, ok := <-ch; ok {
		t.Fatal("channel not closed after cancel")
	}
}

// TestEventBusDoesNotBlock: a full subscriber channel must drop events instead
// of stalling the engine.
func TestEventBusDoesNotBlock(t *testing.T) {
	s := &Session{}
	_, cancel := s.Subscribe()
	defer cancel()

	done := make(chan struct{})
	go func() {
		for i := 0; i < subscribeBuffer*3; i++ {
			s.emit(Event{Type: EventTorrentAdded})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("emit blocked on a full subscriber")
	}
}

func TestTorrentEmitInfoHash(t *testing.T) {
	s := &Session{}
	ch, cancel := s.Subscribe()
	defer cancel()

	tr := &torrent{session: s, infoHash: [20]byte{0xab, 0x01}}
	tr.emit(EventTorrentStopped, nil)

	ev := <-ch
	if ev.Type != EventTorrentStopped {
		t.Fatalf("type = %q", ev.Type)
	}
	if ev.Torrent != strings.Repeat("ab", 1)+"01"+strings.Repeat("00", 18) {
		t.Fatalf("hash = %q", ev.Torrent)
	}
}

func TestTorrentEmitPieceAndPeer(t *testing.T) {
	s := &Session{}
	ch, cancel := s.Subscribe()
	defer cancel()

	tr := &torrent{session: s, infoHash: [20]byte{0x01}}
	tr.emitPiece(42)
	if ev := <-ch; ev.Type != EventTorrentPiece || ev.Piece != 42 {
		t.Fatalf("piece event = %+v", ev)
	}
	tr.emitPeer(EventPeerConnected, "10.0.0.1:6881")
	if ev := <-ch; ev.Type != EventPeerConnected || ev.Peer != "10.0.0.1:6881" {
		t.Fatalf("peer event = %+v", ev)
	}
}
