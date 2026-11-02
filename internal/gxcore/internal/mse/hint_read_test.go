package mse

import (
	"bytes"
	"io"
	"net"
	"testing"
)

// hintRecorder is a socket that remembers the hints it receives.
type hintRecorder struct {
	net.Conn
	hints []int
}

func (c *hintRecorder) HintRead(n int) { c.hints = append(c.hints, n) }

// encryptedPair completes a real handshake in which the initiator sends extra
// bytes after the payload the responder consumes.
func encryptedPair(t *testing.T, initialPayload []byte) (out *Conn, in *Conn, inSocket *hintRecorder) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	sKey := bytes.Repeat([]byte{7}, 20)
	out = WrapConn(a)
	inSocket = &hintRecorder{Conn: b}
	in = WrapConn(inSocket)
	done := make(chan error, 1)
	go func() {
		_, err := out.HandshakeOutgoing(sKey, RC4, initialPayload)
		done <- err
	}()
	err := in.HandshakeIncoming(
		func([20]byte) []byte { return sKey },
		func(CryptoMethod) CryptoMethod { return RC4 })
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	return out, in, inSocket
}

// A hint must not count the bytes the stream already holds from the handshake: the
// peer will not send them again, and the reader would wait until its deadline.
func TestHintReadLeavesOutTheInitialPayload(t *testing.T) {
	_, in, socket := encryptedPair(t, make([]byte, 100))

	in.HintRead(1000)
	in.HintRead(100) // all of it is already here: nothing to ask the socket
	if len(socket.hints) != 1 || socket.hints[0] != 900 {
		t.Fatalf("hints to the socket = %v, want [900]", socket.hints)
	}

	if _, err := io.ReadFull(in, make([]byte, 40)); err != nil {
		t.Fatal(err)
	}
	in.HintRead(1000)
	if got := socket.hints[len(socket.hints)-1]; got != 940 {
		t.Fatalf("after reading 40 bytes the hint is %d, want 940", got)
	}

	if _, err := io.ReadFull(in, make([]byte, 60)); err != nil {
		t.Fatal(err)
	}
	in.HintRead(1000)
	if got := socket.hints[len(socket.hints)-1]; got != 1000 {
		t.Fatalf("with the payload consumed the hint is %d, want 1000", got)
	}
}

// The initiator holds no payload, so its hint reaches the socket whole.
func TestHintReadOnTheInitiator(t *testing.T) {
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	socket := &hintRecorder{Conn: a}
	s := WrapConn(socket)
	s.HintRead(16384)
	if len(socket.hints) != 1 || socket.hints[0] != 16384 {
		t.Fatalf("hints to the socket = %v, want [16384]", socket.hints)
	}
}
