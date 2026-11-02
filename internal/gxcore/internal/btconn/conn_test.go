package btconn

import (
	"bytes"
	"net"
	"testing"
)

type hintRecorder struct {
	net.Conn
	hints []int
}

func (c *hintRecorder) HintRead(n int) { c.hints = append(c.hints, n) }

// The bytes replayed from the plain handshake attempt never reach the socket again,
// so a hint must not ask the socket for them.
func TestRWConnHintLeavesOutTheReplay(t *testing.T) {
	socket := &hintRecorder{}
	seen := bytes.NewBuffer(make([]byte, 20))
	c := &rwConn{nil, socket, seen}

	c.HintRead(100)
	c.HintRead(20)
	if len(socket.hints) != 1 || socket.hints[0] != 80 {
		t.Fatalf("hints to the socket = %v, want [80]", socket.hints)
	}
	seen.Reset()
	c.HintRead(100)
	if socket.hints[1] != 100 {
		t.Fatalf("with the replay consumed the hint is %d, want 100", socket.hints[1])
	}
}
