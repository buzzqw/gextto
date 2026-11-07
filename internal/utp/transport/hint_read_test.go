package transport

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// hintPair is an established loopback connection, seen from both ends.
func hintPair(t *testing.T) (client, server *Conn) {
	t.Helper()
	listen := func() *Socket {
		pc, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		s, err := NewSocketFromPacketConn(pc)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.CloseNow() })
		return s
	}
	a, b := listen(), listen()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := b.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := a.DialContext(ctx, "", b.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	// The acceptor sees the connection only when the first data arrives.
	if _, err := c.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-accepted:
		one := make([]byte, 1)
		if _, err := s.Read(one); err != nil {
			t.Fatal(err)
		}
		return c.(*Conn), s.(*Conn)
	case <-time.After(5 * time.Second):
		t.Fatal("no connection accepted")
	}
	return nil, nil
}

type readResult struct {
	n   int
	err error
}

func readAsync(c *Conn, size int) <-chan readResult {
	done := make(chan readResult, 1)
	go func() {
		n, err := c.Read(make([]byte, size))
		done <- readResult{n, err}
	}()
	return done
}

// A hinted Read waits for the whole amount, across several separate writes, and
// the hint covers only that Read.
func TestHintReadWaitsForTheWholeAmount(t *testing.T) {
	client, server := hintPair(t)
	server.HintRead(3000)
	done := readAsync(server, 16384)
	if _, err := client.Write(make([]byte, 1000)); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		t.Fatalf("read returned %d bytes (%v) before the amount it waits for", r.n, r.err)
	case <-time.After(200 * time.Millisecond):
	}
	if _, err := client.Write(make([]byte, 2000)); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if r.err != nil || r.n != 3000 {
			t.Fatalf("read = %d, %v; want 3000 bytes", r.n, r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("read still waiting with the whole amount sent")
	}
	// The next Read is not held back.
	done = readAsync(server, 16384)
	if _, err := client.Write(make([]byte, 10)); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if r.err != nil || r.n != 10 {
			t.Fatalf("second read = %d, %v; want 10 bytes", r.n, r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the hint held back a second read")
	}
}

// A peer that stalls is noticed as before: the deadline releases the bytes it did
// send, and with nothing buffered it reports the timeout.
func TestHintReadYieldsToTheDeadline(t *testing.T) {
	client, server := hintPair(t)
	if _, err := client.Write(make([]byte, 1000)); err != nil {
		t.Fatal(err)
	}
	waitBuffered(t, server, 1000)
	if err := server.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	server.HintRead(16384)
	r := <-readAsync(server, 16384)
	if r.err != nil || r.n != 1000 {
		t.Fatalf("read at the deadline = %d, %v; want the 1000 bytes sent", r.n, r.err)
	}
	server.HintRead(16384)
	r = <-readAsync(server, 16384)
	var timeout net.Error
	if !errors.As(r.err, &timeout) || !timeout.Timeout() {
		t.Fatalf("read with nothing buffered = %d, %v; want a timeout", r.n, r.err)
	}
}

// A peer that closes mid-block hands over what it sent, then EOF.
func TestHintReadYieldsToTheClose(t *testing.T) {
	client, server := hintPair(t)
	server.HintRead(16384)
	done := readAsync(server, 16384)
	if _, err := client.Write(make([]byte, 1000)); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if r.n != 1000 {
			t.Fatalf("read after close = %d, %v; want the 1000 bytes sent", r.n, r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("read still waiting after the peer closed")
	}
}

// A hint larger than half the window is capped, so the peer can always send the
// amount the reader waits for.
func TestHintReadStaysBelowTheWindow(t *testing.T) {
	_, server := hintPair(t)
	server.HintRead(10 * readBufferLen)
	mu.Lock()
	defer mu.Unlock()
	if server.readWant != readBufferLen/2 {
		t.Fatalf("readWant = %d, want %d", server.readWant, readBufferLen/2)
	}
}

func waitBuffered(t *testing.T, c *Conn, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		got := c.bufferedInbound()
		mu.Unlock()
		if got >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d bytes buffered, want %d", got, want)
		}
		time.Sleep(time.Millisecond)
	}
}

// A reader that always leaves bytes behind must not make the read window grow
// or allocate: drained chunks go back to the pool, whatever the size of the
// datagrams.
func TestReadBufferReusesTheConsumedHead(t *testing.T) {
	var q readQueue
	datagram := make([]byte, 1400)
	out := make([]byte, len(datagram))
	for range 8 {
		q.Write(datagram)
	}
	for i := range 20000 {
		q.Write(datagram)
		if n := q.Read(out); n != len(datagram) {
			t.Fatalf("read %d bytes, want %d", n, len(datagram))
		}
		if len(q.chunks) > 2 {
			t.Fatalf("after %d datagrams %d chunks hold %d unread bytes", i, len(q.chunks), q.Len())
		}
	}
	allocs := testing.AllocsPerRun(1000, func() {
		q.Write(datagram)
		q.Read(out)
	})
	if allocs > 0.1 {
		t.Fatalf("%.2f allocations per datagram, want none in the steady state", allocs)
	}
}
