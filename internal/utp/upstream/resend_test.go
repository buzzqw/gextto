package upstream

import (
	"context"
	"net"
	"testing"
	"time"
)

// A server that accepts but never reads stalls the client's ACK clock: the
// client's RTO resends must fire and land in the socket counters.
func TestTimeoutResendsAreCounted(t *testing.T) {
	serverPC, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewSocketFromPacketConn(serverPC)
	if err != nil {
		t.Fatal(err)
	}
	defer server.CloseNow()

	clientPC, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewSocketFromPacketConn(clientPC)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseNow()

	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := server.Accept()
		if err == nil {
			accepted <- c
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dialed, err := client.DialContext(ctx, "udp", server.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer dialed.Close()

	select {
	case srvConn := <-accepted:
		defer srvConn.Close() // never Read: the stall
	case <-ctx.Done():
		t.Fatal("accept timed out")
	}

	// Past the server's 1 MiB read buffer the ACKs stall and the RTO
	// timers must fire.
	_ = dialed.SetWriteDeadline(time.Now().Add(4 * time.Second))
	_, _ = dialed.Write(make([]byte, 3<<20))

	if timeout, _ := client.ResendStats(); timeout == 0 {
		t.Fatal("expected RTO resends on a stalled connection")
	}
}

// Pushing past the 50-SYN backlog evicts one (with a reset) and counts
// the drop: under burst the eviction, not the accept loop, is the bound.
func TestBacklogEvictionIsCounted(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSocketFromPacketConn(pc)
	if err != nil {
		t.Fatal(err)
	}
	defer s.CloseNow()
	mu.Lock()
	for i := 0; i < 51; i++ {
		s.pushBacklog(syn{seq_nr: uint16(i), conn_id: uint16(i + 1), addr: "198.51.100.9:51413"})
	}
	n := len(s.backlog)
	drops := s.SynDrops()
	mu.Unlock()
	if n != 50 {
		t.Fatalf("backlog holds %d SYNs, want 50", n)
	}
	if drops != 1 {
		t.Fatalf("syn drops = %d, want 1", drops)
	}
}

// The fast-resend counter plumbing, without fabricating selective ACKs.
func TestResendStatsPlumbing(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSocketFromPacketConn(pc)
	if err != nil {
		t.Fatal(err)
	}
	defer s.CloseNow()
	if timeout, fast := s.ResendStats(); timeout != 0 || fast != 0 {
		t.Fatalf("fresh socket stats = (%d, %d), want zeros", timeout, fast)
	}
	s.addTimeoutResend()
	s.addTimeoutResend()
	s.addFastResend()
	if timeout, fast := s.ResendStats(); timeout != 2 || fast != 1 {
		t.Fatalf("socket stats = (%d, %d), want (2, 1)", timeout, fast)
	}
}

// The latency seed bounds the first RTO: a 5 ms estimate gives [10,20] ms
// instead of the fixed [800,1600] ms, out-of-range estimates clamp, and real
// samples always win over the seed.
func TestInitialLatencySeedBoundsRTO(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSocketFromPacketConn(pc)
	if err != nil {
		t.Fatal(err)
	}
	defer s.CloseNow()
	c := s.newConn(&net.UDPAddr{IP: net.ParseIP("203.0.113.9"), Port: 51413})

	if rt := c.resendTimeout(); rt < 800*time.Millisecond || rt > 1600*time.Millisecond {
		t.Fatalf("default RTO = %v, want [800ms, 1600ms]", rt)
	}
	s.SetInitialLatencyEstimator(func(string) (time.Duration, bool) {
		return 5 * time.Millisecond, true
	})
	if rt := c.resendTimeout(); rt < 10*time.Millisecond || rt > 20*time.Millisecond {
		t.Fatalf("seeded RTO = %v, want [10ms, 20ms]", rt)
	}
	s.SetInitialLatencyEstimator(func(string) (time.Duration, bool) {
		return time.Microsecond, true
	})
	if rt := c.resendTimeout(); rt < 10*time.Millisecond || rt > 20*time.Millisecond {
		t.Fatalf("clamped-low RTO = %v, want [10ms, 20ms]", rt)
	}
	s.SetInitialLatencyEstimator(func(string) (time.Duration, bool) {
		return time.Hour, true
	})
	if rt := c.resendTimeout(); rt < 800*time.Millisecond || rt > 1600*time.Millisecond {
		t.Fatalf("clamped-high RTO = %v, want [800ms, 1600ms]", rt)
	}
	c.addLatency(50 * time.Millisecond)
	if rt := c.resendTimeout(); rt < 100*time.Millisecond || rt > 200*time.Millisecond {
		t.Fatalf("sampled RTO = %v, want [100ms, 200ms]", rt)
	}
}
