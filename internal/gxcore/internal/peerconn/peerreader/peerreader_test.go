package peerreader

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bandwidth"
	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
)

// newTestReader wires a reader over one end of an in-memory pipe.
func newTestReader(t *testing.T) (*PeerReader, net.Conn) {
	t.Helper()
	ours, theirs := net.Pipe()
	t.Cleanup(func() {
		_ = ours.Close()
		_ = theirs.Close()
	})
	return New(ours, logger.New("peerreader-test"), time.Minute, 30<<20, bandwidth.New(0)), theirs
}

// writeMessage frames one message: length prefix, id, payload.
func writeMessage(conn io.Writer, id peerprotocol.MessageID, payload []byte) {
	header := make([]byte, 5)
	binary.BigEndian.PutUint32(header[:4], uint32(1+len(payload)))
	header[4] = byte(id)
	_, _ = conn.Write(header)
	if len(payload) > 0 {
		_, _ = conn.Write(payload)
	}
}

// TestBitfieldMessageIsCapped pins the per-type ceiling: the session cap is
// MaxMetadataSize (30 MiB) because of the metadata size, but a bitfield is
// never that large, and allocating it on a peer's say-so is a memory
// amplification. A legal bitfield still goes through.
func TestBitfieldMessageIsCapped(t *testing.T) {
	r, peer := newTestReader(t)
	go r.Run()

	go func() {
		writeMessage(peer, peerprotocol.Bitfield, make([]byte, maxBitfieldMessage+1))
	}()
	select {
	case <-r.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the reader accepted a bitfield above the per-message cap")
	}
}

// TestExtensionMessageIsCapped covers the BEP 10 message path, which used to
// allocate up to the session cap before the bencode screen ran.
func TestExtensionMessageIsCapped(t *testing.T) {
	r, peer := newTestReader(t)
	go r.Run()

	go func() {
		writeMessage(peer, peerprotocol.Extension, make([]byte, maxExtensionMessage+1))
	}()
	select {
	case <-r.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the reader accepted an extension message above the cap")
	}
}

// TestShortPieceMessageIsRejected covers a declared piece length shorter than
// the index+begin header: reading it would consume bytes of the next message.
func TestShortPieceMessageIsRejected(t *testing.T) {
	r, peer := newTestReader(t)
	go r.Run()

	go func() {
		writeMessage(peer, peerprotocol.Piece, nil)
	}()
	select {
	case <-r.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the reader accepted a piece message shorter than its header")
	}
}

// TestLegalBitfieldIsDelivered keeps a normal connection working.
func TestLegalBitfieldIsDelivered(t *testing.T) {
	r, peer := newTestReader(t)
	go r.Run()

	go func() {
		writeMessage(peer, peerprotocol.Bitfield, []byte{0xff, 0x00})
	}()
	select {
	case msg := <-r.Messages():
		bf, ok := msg.(peerprotocol.BitfieldMessage)
		if !ok || len(bf.Data) != 2 {
			t.Fatalf("unexpected message %#v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a legal bitfield was not delivered")
	}
}

// benchConn is a net.Conn over an in-memory buffer, for the read path only.
type benchConn struct{ r *bytes.Reader }

func (c *benchConn) Read(p []byte) (int, error)       { return c.r.Read(p) }
func (c *benchConn) Write(p []byte) (int, error)      { return len(p), nil }
func (c *benchConn) Close() error                     { return nil }
func (c *benchConn) LocalAddr() net.Addr              { return benchAddr{} }
func (c *benchConn) RemoteAddr() net.Addr             { return benchAddr{} }
func (c *benchConn) SetDeadline(time.Time) error      { return nil }
func (c *benchConn) SetReadDeadline(time.Time) error  { return nil }
func (c *benchConn) SetWriteDeadline(time.Time) error { return nil }

type benchAddr struct{}

func (benchAddr) Network() string { return "tcp" }
func (benchAddr) String() string  { return "127.0.0.1:1" }

// BenchmarkPeerReaderRequests measures the parse-and-hand-off path for the
// message that dominates an upload-heavy connection: the block requests of a
// leecher. The per-type size caps added to parseMessage are three integer
// comparisons on messages that carry none, so the figure must stay in the
// nanoseconds per message.
func BenchmarkPeerReaderRequests(b *testing.B) {
	const count = 4096
	payload := make([]byte, 12) // index, begin, length
	var frame bytes.Buffer
	for i := 0; i < count; i++ {
		binary.BigEndian.PutUint32(payload[0:4], uint32(i))
		binary.BigEndian.PutUint32(payload[8:12], 16<<10)
		writeMessage(&frame, peerprotocol.Request, payload)
	}
	data := frame.Bytes()
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := New(&benchConn{r: bytes.NewReader(data)}, logger.New("peerreader-bench"), time.Minute, 30<<20, bandwidth.New(0))
		done := make(chan struct{})
		go func() { r.Run(); close(done) }()
		for j := 0; j < count; j++ {
			<-r.Messages()
		}
		r.Stop()
		<-done
	}
	b.StopTimer()
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*count), "ns/message")
}
