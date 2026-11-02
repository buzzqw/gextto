package btconn

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/netx"
)

// TestDialRetriesOverUTPAfterTCPReset reproduces a uTP-only peer: its TCP
// listener accepts the raced TCP leg and resets it, while the uTP leg would
// work. The first attempt must fail on the reset and Dial must retry over uTP
// alone, instead of losing the peer until the next announce.
func TestDialRetriesOverUTPAfterTCPReset(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var accepted atomic.Int64
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			if tc, ok := c.(*net.TCPConn); ok {
				_ = tc.SetLinger(0) // close with RST, like the uTP-only peer
			}
			_ = c.Close()
		}
	}()

	ih := [20]byte{1, 2, 3}
	ourID := [20]byte{4, 5, 6}
	peerID := [20]byte{7, 8, 9}

	// The uTP leg is slower than TCP so the race picks TCP first. The first
	// call is cancelled by the race; the retry gets a live pipe and a peer
	// that completes the handshake.
	var utpDials atomic.Int64
	netx.SetUTPDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		utpDials.Add(1)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			var got [68]byte
			if _, err := io.ReadFull(b, got[:]); err != nil {
				return
			}
			_ = binary.Write(b, binary.BigEndian, handshake{
				Pstr:       pstr,
				Extensions: [8]byte{},
				InfoHash:   ih,
				PeerID:     peerID,
			})
		}()
		return a, nil
	}, false)
	defer netx.SetUTPDialer(nil, false)

	stopC := make(chan struct{})
	defer close(stopC)
	conn, _, _, gotID, err := Dial(ln.Addr(), 2*time.Second, 2*time.Second, false, false, [8]byte{}, ih, ourID, stopC)
	if err != nil {
		t.Fatalf("Dial did not recover over uTP: %v", err)
	}
	defer conn.Close()
	if !netx.IsUTP(conn) {
		t.Fatal("expected the retry to return a uTP connection")
	}
	if gotID != peerID {
		t.Fatalf("peer id = %x, want %x", gotID, peerID)
	}
	if accepted.Load() != 1 {
		t.Fatalf("TCP accepted %d times, want 1", accepted.Load())
	}
	if utpDials.Load() == 0 {
		t.Fatal("uTP was never dialed")
	}
}

func TestRetryableDialError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"reset", syscall.ECONNRESET, true},
		{"pipe", syscall.EPIPE, true},
		{"eof", io.EOF, true},
		{"unexpected eof", io.ErrUnexpectedEOF, true},
		{"timeout", context.DeadlineExceeded, false},
		{"own connection", errOwnConnection, false},
		{"wrong infohash", errInvalidInfoHash, false},
	}
	for _, c := range cases {
		if got := retryableDialError(c.err); got != c.want {
			t.Errorf("%s: retryableDialError(%v) = %v, want %v", c.name, c.err, got, c.want)
		}
	}
}
