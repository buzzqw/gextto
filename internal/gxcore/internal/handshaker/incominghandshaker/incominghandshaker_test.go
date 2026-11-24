package incominghandshaker

import (
	"net"
	"testing"
	"time"
)

// TestRunClosesConnectionOnFailure pins the ownership rule: a per-torrent
// handshake that fails must not leave the accepted socket open while its result
// waits in the torrent loop.
func TestRunClosesConnectionOnFailure(t *testing.T) {
	client, remote := net.Pipe()
	h := New(client)
	results := make(chan *IncomingHandshaker, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.Run([20]byte{}, func([20]byte) []byte { return nil }, func([20]byte) bool { return false }, results, time.Second, [8]byte{}, false)
	}()
	_ = remote.Close()

	select {
	case got := <-results:
		if got.Error == nil {
			t.Fatal("handshake unexpectedly succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handshake did not report its failure")
	}
	<-done
	if _, err := client.Write([]byte("x")); err == nil {
		t.Fatal("failed handshake left its connection open")
	}
}
