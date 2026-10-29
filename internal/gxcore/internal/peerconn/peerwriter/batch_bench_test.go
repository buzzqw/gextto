package peerwriter

import (
	"bytes"
	"encoding/binary"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
)

// countingConn is a net.Conn that discards what it is given and counts the
// Write calls and bytes: each call is one syscall (and, with TCP_NODELAY, at
// least one segment) on a real socket.
type countingConn struct {
	writes atomic.Int64
	bytes  atomic.Int64
	mu     sync.Mutex
	got    []byte
	keep   bool
}

func (c *countingConn) Write(b []byte) (int, error) {
	c.writes.Add(1)
	c.bytes.Add(int64(len(b)))
	if c.keep {
		c.mu.Lock()
		c.got = append(c.got, b...)
		c.mu.Unlock()
	}
	return len(b), nil
}

func (c *countingConn) Read([]byte) (int, error)         { select {} }
func (c *countingConn) Close() error                     { return nil }
func (c *countingConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (c *countingConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (c *countingConn) SetDeadline(time.Time) error      { return nil }
func (c *countingConn) SetReadDeadline(time.Time) error  { return nil }
func (c *countingConn) SetWriteDeadline(time.Time) error { return nil }

// BenchmarkPeerWriterRequests sends the requests of one refill (250, the
// in-flight cap) the way the torrent loop does, one SendMessage each, and
// reports the Write calls the connection saw per message.
func BenchmarkPeerWriterRequests(b *testing.B) {
	const perRefill = 250
	conn := &countingConn{}
	w := New(conn, logger.New("bench"), 250, true, nil)
	go w.Run()
	defer w.Stop()
	want := int64(0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := uint32(0); j < perRefill; j++ {
			w.SendMessage(peerprotocol.RequestMessage{Index: uint32(i), Begin: j * 16384, Length: 16384})
		}
		want += perRefill * 17
		for conn.bytes.Load() < want {
			time.Sleep(10 * time.Microsecond)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(conn.writes.Load())/float64(int64(b.N)*perRefill), "writes/msg")
}

// TestPeerWriterBatchKeepsOrderAndFraming queues small messages around a block
// upload and checks the bytes on the wire: every message framed, in queue
// order, the block intact, and the small run before the block sent in fewer
// Write calls than messages.
func TestPeerWriterBatchKeepsOrderAndFraming(t *testing.T) {
	conn := &countingConn{keep: true}
	w := New(conn, logger.New("test"), 250, true, nil)
	block := make([]byte, 16384)
	for i := range block {
		block[i] = byte(i * 7)
	}
	// Queue everything before the loops run, so the small run is waiting
	// together when the writer first asks for work.
	go func() {
		w.SendMessage(peerprotocol.HaveMessage{Index: 1})
		for j := uint32(0); j < 40; j++ {
			w.SendMessage(peerprotocol.RequestMessage{Index: 2, Begin: j * 16384, Length: 16384})
		}
		w.SendPiece(peerprotocol.RequestMessage{Index: 3, Begin: 0, Length: 16384}, bytes.NewReader(block))
		w.SendMessage(peerprotocol.HaveMessage{Index: 4})
	}()
	go w.Run()
	defer w.Stop()
	// The torrent always reads the upload notices; an unread one would hold
	// the writer after the block.
	go func() {
		for {
			select {
			case <-w.Messages():
			case <-w.Done():
				return
			}
		}
	}()

	want := int64(9 + 40*17 + (13 + 16384) + 9)
	deadline := time.Now().Add(5 * time.Second)
	for conn.bytes.Load() < want {
		if time.Now().After(deadline) {
			t.Fatalf("wrote %d bytes, want %d", conn.bytes.Load(), want)
		}
		time.Sleep(time.Millisecond)
	}
	conn.mu.Lock()
	got := append([]byte(nil), conn.got...)
	conn.mu.Unlock()

	type frame struct {
		id   peerprotocol.MessageID
		body []byte
	}
	var frames []frame
	for len(got) > 0 {
		n := binary.BigEndian.Uint32(got)
		frames = append(frames, frame{peerprotocol.MessageID(got[4]), got[5 : 4+n]})
		got = got[4+n:]
	}
	if len(frames) != 43 {
		t.Fatalf("got %d frames, want 43", len(frames))
	}
	if frames[0].id != peerprotocol.Have || binary.BigEndian.Uint32(frames[0].body) != 1 {
		t.Fatalf("first frame %+v, want have 1", frames[0])
	}
	for j := 0; j < 40; j++ {
		f := frames[1+j]
		if f.id != peerprotocol.Request || binary.BigEndian.Uint32(f.body[4:]) != uint32(j)*16384 {
			t.Fatalf("frame %d: %v begin %d, want request begin %d", 1+j, f.id, binary.BigEndian.Uint32(f.body[4:]), j*16384)
		}
	}
	if f := frames[41]; f.id != peerprotocol.Piece || !bytes.Equal(f.body[8:], block) {
		t.Fatalf("frame 41 is not the intact block")
	}
	if f := frames[42]; f.id != peerprotocol.Have || binary.BigEndian.Uint32(f.body) != 4 {
		t.Fatalf("last frame %+v, want have 4", f)
	}
	if n := conn.writes.Load(); n >= 43 {
		t.Fatalf("%d Write calls for 43 messages: the small run was not batched", n)
	}
}

// BenchmarkPeerWriterRequestsTCP is BenchmarkPeerWriterRequests over a real
// loopback TCP connection (TCP_NODELAY, as Go sets it), where every Write is a
// syscall and a segment.
func BenchmarkPeerWriterRequestsTCP(b *testing.B) {
	const perRefill = 250
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer ln.Close()
	var received atomic.Int64
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 64<<10)
		for {
			n, err := c.Read(buf)
			received.Add(int64(n))
			if err != nil {
				return
			}
		}
	}()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		b.Fatal(err)
	}
	w := New(conn, logger.New("bench"), 250, true, nil)
	go w.Run()
	defer w.Stop()
	want := int64(0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := uint32(0); j < perRefill; j++ {
			w.SendMessage(peerprotocol.RequestMessage{Index: uint32(i), Begin: j * 16384, Length: 16384})
		}
		want += perRefill * 17
		for received.Load() < want {
			runtime.Gosched()
		}
	}
}
