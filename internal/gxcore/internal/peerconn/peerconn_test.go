package peerconn

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
)

// TestCountingConnTracksRawBytes proves the engine counts the raw peer
// wire bytes (framing and encryption included) separately from the payload,
// which is what the session reports as protocol overhead. The counters are
// per-connection/session, so a fresh WireStats starts at zero.
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

	stats := &WireStats{}
	c := New(client, stats, logger.New("test"), time.Second, 1, 1<<20, false, nil, nil)

	if _, err := c.conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(c.conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}

	if got := stats.Written.Load(); got != 4 {
		t.Fatalf("wire written = %d, want 4", got)
	}
	if got := stats.Read.Load(); got != 4 {
		t.Fatalf("wire read = %d, want 4", got)
	}
}
