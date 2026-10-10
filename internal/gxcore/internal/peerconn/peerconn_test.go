package peerconn

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
)

// TestCountingConnTracksRawBytes proves the gextto fork counts the raw peer
// wire bytes (framing and encryption included) separately from the payload,
// which is what the session reports as protocol overhead.
func TestCountingConnTracksRawBytes(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		buf := make([]byte, 4)
		if _, err := io.ReadFull(server, buf); err != nil {
			return
		}
		_, _ = server.Write([]byte("pong"))
	}()

	c := New(client, logger.New("test"), time.Second, 1, 1<<20, false, nil, nil)

	writeBefore := WireBytesWritten.Load()
	readBefore := WireBytesRead.Load()

	if _, err := c.conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(c.conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}

	if got := WireBytesWritten.Load() - writeBefore; got != 4 {
		t.Fatalf("wire written delta = %d, want 4", got)
	}
	if got := WireBytesRead.Load() - readBefore; got != 4 {
		t.Fatalf("wire read delta = %d, want 4", got)
	}
}
