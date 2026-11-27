package transport

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

func testSocket(t *testing.T) *Socket {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSocketFromPacketConn(pc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.CloseNow() })
	return s
}

// A destroyed connection must not stay reachable from its timers: a pending
// timer keeps the callback, the callback keeps the Conn with its buffers.
func TestDestroyedConnStopsItsTimers(t *testing.T) {
	server, client := testSocket(t), testSocket(t)
	go func() {
		if c, err := server.Accept(); err == nil {
			_ = c.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	nc, err := client.DialContext(ctx, "udp", server.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	c := nc.(*Conn)
	_ = c.SetReadDeadline(time.Now().Add(time.Hour))
	if _, err := c.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}

	c.mu.Lock()
	c.closeNow()
	readTimerArmed := c.packetReadTimeoutTimer.Stop()
	deadlineArmed := c.connDeadlines.read.timer != nil && c.connDeadlines.read.timer.Stop()
	resendArmed := c.resendTimer != nil
	c.mu.Unlock()
	if readTimerArmed || deadlineArmed || resendArmed {
		t.Fatalf("timers still armed after destroy: read timeout %v, deadline %v, resend %v",
			readTimerArmed, deadlineArmed, resendArmed)
	}
}

// blockingPacketConn never completes a write until released, like a socket
// whose send buffer is full.
type blockingPacketConn struct {
	net.PacketConn
	release chan struct{}
	once    sync.Once
}

func (b *blockingPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	<-b.release
	return len(p), nil
}

func (b *blockingPacketConn) Close() error {
	b.once.Do(func() { close(b.release) })
	return b.PacketConn.Close()
}

// Every unknown packet asks for three resets. With writes blocked, a flood of
// them must not leave three goroutines per packet waiting.
func TestUnknownPacketFloodBoundsResetWriters(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	blocking := &blockingPacketConn{PacketConn: pc, release: make(chan struct{})}
	s, err := NewSocketFromPacketConn(blocking)
	if err != nil {
		t.Fatal(err)
	}
	defer s.CloseNow()

	from := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9}
	h := header{Type: stData, Version: 1, ConnID: 4242, SeqNr: 1}
	buf := make([]byte, maxHeaderSize)
	packet := buf[:h.Marshal(buf)]
	for i := 0; i < 200; i++ {
		socketMu.Lock()
		s.handleReceivedPacket(read{data: packet, from: from})
		socketMu.Unlock()
	}
	if got := s.resetsInFlight.Load(); got > maxResetsInFlight {
		t.Fatalf("%d reset writers in flight, want at most %d", got, maxResetsInFlight)
	}
	close(blocking.release)
	blocking.once.Do(func() {})
	deadline := time.Now().Add(5 * time.Second)
	for s.resetsInFlight.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := s.resetsInFlight.Load(); got != 0 {
		t.Fatalf("%d reset writers still running after the socket unblocked", got)
	}
}
