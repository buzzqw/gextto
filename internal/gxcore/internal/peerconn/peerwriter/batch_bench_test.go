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

	"github.com/buzzqw/gextto/internal/gxcore/internal/bandwidth"
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

// runWriter starts w and collects the payload it reports as uploaded.
func runWriter(t *testing.T, w *PeerWriter) *atomic.Int64 {
	t.Helper()
	var uploaded atomic.Int64
	go w.Run()
	t.Cleanup(w.Stop)
	go func() {
		for {
			select {
			case m := <-w.Messages():
				if u, ok := m.(BlockUploaded); ok {
					uploaded.Add(int64(u.Length))
				}
			case <-w.Done():
				return
			}
		}
	}()
	return &uploaded
}

func waitBytes(t *testing.T, conn *countingConn, uploaded *atomic.Int64, wantBytes, wantPayload int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for conn.bytes.Load() < wantBytes || uploaded.Load() < wantPayload {
		if time.Now().After(deadline) {
			t.Fatalf("wrote %d of %d bytes, reported %d of %d", conn.bytes.Load(), wantBytes, uploaded.Load(), wantPayload)
		}
		time.Sleep(time.Millisecond)
	}
}

// Queued blocks go out in fewer writes, intact and in order, with a second request
// for a served block answered by a reject inside the same run, and only the blocks
// counted as uploaded.
func TestPeerWriterBatchesBlocks(t *testing.T) {
	const blocks = 40
	conn := &countingConn{keep: true}
	w := New(conn, logger.New("test"), 250, true, nil)
	data := make([]byte, blocks*16384)
	for i := range data {
		data[i] = byte(i * 31)
	}
	source := bytes.NewReader(data)
	go func() {
		for j := uint32(0); j < blocks; j++ {
			w.SendPiece(peerprotocol.RequestMessage{Index: 0, Begin: j * 16384, Length: 16384}, source)
		}
		w.SendPiece(peerprotocol.RequestMessage{Index: 0, Begin: 0, Length: 16384}, source) // already served
	}()
	uploaded := runWriter(t, w)
	waitBytes(t, conn, uploaded, blocks*(13+16384)+17, blocks*16384)

	conn.mu.Lock()
	got := append([]byte(nil), conn.got...)
	conn.mu.Unlock()
	for j := uint32(0); j < blocks; j++ {
		if n := binary.BigEndian.Uint32(got); n != 9+16384 || peerprotocol.MessageID(got[4]) != peerprotocol.Piece {
			t.Fatalf("block %d: frame length %d, id %d", j, n, got[4])
		}
		if begin := binary.BigEndian.Uint32(got[9:]); begin != j*16384 {
			t.Fatalf("block %d carries begin %d", j, begin)
		}
		if !bytes.Equal(got[13:13+16384], data[j*16384:(j+1)*16384]) {
			t.Fatalf("block %d is not intact", j)
		}
		got = got[13+16384:]
	}
	if len(got) != 17 || peerprotocol.MessageID(got[4]) != peerprotocol.Reject {
		t.Fatalf("after the blocks: %d bytes, want the 17 of a reject", len(got))
	}
	if n := uploaded.Load(); n != blocks*16384 {
		t.Fatalf("reported %d bytes uploaded, want %d", n, blocks*16384)
	}
}

// A rate limit paces single blocks: a batch would turn it into bursts.
func TestPeerWriterDoesNotBatchUnderARateLimit(t *testing.T) {
	const blocks = 6
	conn := &countingConn{}
	w := New(conn, logger.New("test"), 250, true, bandwidth.New(1<<30))
	data := make([]byte, blocks*16384)
	source := bytes.NewReader(data)
	go func() {
		for j := uint32(0); j < blocks; j++ {
			w.SendPiece(peerprotocol.RequestMessage{Index: 0, Begin: j * 16384, Length: 16384}, source)
		}
	}()
	uploaded := runWriter(t, w)
	waitBytes(t, conn, uploaded, blocks*(13+16384), blocks*16384)
	if n := conn.writes.Load(); n != blocks {
		t.Fatalf("%d writes for %d blocks under a rate limit, want one each", n, blocks)
	}
}

// BenchmarkPeerWriterBlocks is the upload path: blocks queued faster than they go.
func BenchmarkPeerWriterBlocks(b *testing.B) {
	conn := &countingConn{}
	w := New(conn, logger.New("bench"), 1<<30, true, nil)
	go w.Run()
	defer w.Stop()
	go func() {
		for {
			select {
			case <-w.Messages():
			case <-w.Done():
				return
			}
		}
	}()
	source := bytes.NewReader(make([]byte, 1<<24))
	b.SetBytes(16384)
	b.ReportAllocs()
	var want int64
	for i := 0; b.Loop(); i++ {
		w.SendPiece(peerprotocol.RequestMessage{Index: uint32(i >> 10), Begin: uint32(i%1024) * 16384, Length: 16384}, source)
		want += 13 + 16384
	}
	for conn.bytes.Load() < want {
		runtime.Gosched()
	}
	b.ReportMetric(float64(conn.writes.Load())/float64(b.N), "writes/block")
}
