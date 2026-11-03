package torrent

import (
	"net"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerconn"
)

// loopbackPeer builds a peer over a real loopback TCP connection (peer.IP
// needs a TCP address).
func loopbackPeer(t *testing.T, id byte) *peer.Peer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept()
		accepted <- c
	}()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	other := <-accepted
	t.Cleanup(func() { conn.Close(); other.Close() })
	var peerID [20]byte
	peerID[0] = id
	return peer.New(conn, &peerconn.WireStats{}, 0, peerID, [8]byte{}, 0, time.Minute, time.Minute, 1, 1<<20, nil, nil)
}

// TestSilentPeerOnlyAfterTheThreshold: a reconnect replaces an established
// peer only when that peer has been silent for staleConnectionAfter.
func TestSilentPeerOnlyAfterTheThreshold(t *testing.T) {
	old := staleConnectionAfter
	t.Cleanup(func() { staleConnectionAfter = old })
	a := loopbackPeer(t, 1)
	tr := &torrent{peers: map[*peer.Peer]struct{}{a: {}}}
	sameID := func(pe *peer.Peer) bool { return pe.ID == a.ID }

	staleConnectionAfter = time.Hour
	if got := tr.silentPeer(sameID); got != nil {
		t.Fatal("a peer that just connected was treated as silent")
	}
	staleConnectionAfter = 0
	if got := tr.silentPeer(sameID); got != a {
		t.Fatalf("silentPeer = %v, want the silent peer", got)
	}
	if got := tr.silentPeer(func(pe *peer.Peer) bool { return pe.ID[0] == 9 }); got != nil {
		t.Fatal("silentPeer returned a peer that does not match")
	}
}
