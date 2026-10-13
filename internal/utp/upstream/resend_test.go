package upstream

import (
	"context"
	"net"
	"net/netip"
	"os"
	"sync/atomic"
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
		s.pushBacklog(syn{seq_nr: uint16(i), conn_id: uint16(i + 1), addr: netip.MustParseAddrPort("198.51.100.9:51413")})
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

// The loss-driven adaptive RTO: repeated timeouts double the connection's RTO
// up to maxRTOBackoff, a delivered packet halves it back, and disabling the
// adaptation leaves the base RTO untouched.
func TestAdaptiveRTOBacksOffOnTimeouts(t *testing.T) {
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
	// A 50 ms sample pins the base RTO to [100, 200] ms, so each backoff step
	// has a disjoint range to assert on.
	c.addLatency(50 * time.Millisecond)

	assertRTO := func(lo, hi time.Duration, what string) {
		t.Helper()
		if rt := c.resendTimeout(); rt < lo || rt > hi {
			t.Fatalf("%s RTO = %v, want [%v, %v]", what, rt, lo, hi)
		}
	}

	if c.rtoBackoff != 1 {
		t.Fatalf("fresh connection backoff = %d, want 1", c.rtoBackoff)
	}
	assertRTO(100*time.Millisecond, 200*time.Millisecond, "base")

	c.noteRTOTimeout()
	if c.rtoBackoff != 2 {
		t.Fatalf("backoff after one timeout = %d, want 2", c.rtoBackoff)
	}
	assertRTO(200*time.Millisecond, 400*time.Millisecond, "backoff-2")

	c.noteRTOTimeout()
	if c.rtoBackoff != 4 {
		t.Fatalf("backoff after two timeouts = %d, want 4", c.rtoBackoff)
	}
	c.noteRTOTimeout() // beyond the cap
	if c.rtoBackoff != maxRTOBackoff {
		t.Fatalf("backoff is not capped: %d, want %d", c.rtoBackoff, maxRTOBackoff)
	}
	assertRTO(400*time.Millisecond, 800*time.Millisecond, "backoff-4")

	c.noteDelivery()
	if c.rtoBackoff != 2 {
		t.Fatalf("backoff after a delivery = %d, want 2", c.rtoBackoff)
	}
	c.noteDelivery()
	if c.rtoBackoff != 1 {
		t.Fatalf("backoff after two deliveries = %d, want 1", c.rtoBackoff)
	}
	c.noteDelivery()
	if c.rtoBackoff != 1 {
		t.Fatalf("backoff fell below 1: %d", c.rtoBackoff)
	}
	assertRTO(100*time.Millisecond, 200*time.Millisecond, "relaxed")

	// With the adaptation off the timeouts must not move the RTO.
	adaptiveRTO.Store(false)
	defer adaptiveRTO.Store(true)
	c.noteRTOTimeout()
	c.noteRTOTimeout()
	if c.rtoBackoff != 1 {
		t.Fatalf("disabled adaptation moved the backoff: %d", c.rtoBackoff)
	}
	assertRTO(100*time.Millisecond, 200*time.Millisecond, "disabled")
}

// The minimum-latency floor: a sub-millisecond sampled RTT is raised to the
// floor, so the RTO cannot collapse to microseconds on a fast path; lowering
// the floor for the A/B test lets the sampled value through.
func TestMinimumLatencyFloor(t *testing.T) {
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

	if got := minLatencyDuration(); got != 5*time.Millisecond {
		t.Fatalf("default min latency = %v, want 5ms", got)
	}
	c.addLatency(10 * time.Microsecond)
	if rt := c.resendTimeout(); rt < 10*time.Millisecond || rt > 20*time.Millisecond {
		t.Fatalf("floored RTO = %v, want [10ms, 20ms]", rt)
	}
	minLatencyNanos.Store(int64(time.Microsecond))
	defer minLatencyNanos.Store(int64(5 * time.Millisecond))
	if rt := c.resendTimeout(); rt >= time.Millisecond {
		t.Fatalf("unfloored RTO = %v, want sub-millisecond", rt)
	}
}

// blackholePC drops every packet a Socket tries to write once silent is set,
// so its peer keeps timing out: the sustained-loss harness for the backoff.
type blackholePC struct {
	net.PacketConn
	silent atomic.Bool
}

func (b *blackholePC) WriteTo(p []byte, addr net.Addr) (int, error) {
	if b.silent.Load() {
		return len(p), nil
	}
	return b.PacketConn.WriteTo(p, addr)
}

// silentPeerResends completes the uTP handshake with a peer, then silences the
// peer so the client's sends keep timing out, and returns the socket's
// RTO-resend count after the write window together with the connection's final
// RTO backoff. adaptive decides whether the loss-driven backoff is active and
// floor sets the minimum RTT assumed by the RTO.
func silentPeerResends(t *testing.T, adaptive bool, floor, window time.Duration) (int64, uint32) {
	t.Helper()
	adaptiveRTO.Store(adaptive)
	minLatencyNanos.Store(int64(floor))
	defer func() {
		adaptiveRTO.Store(true)
		minLatencyNanos.Store(int64(5 * time.Millisecond))
	}()

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	bh := &blackholePC{PacketConn: pc}
	server, err := NewSocketFromPacketConn(bh)
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
		if c, err := server.Accept(); err == nil {
			accepted <- c
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), window+5*time.Second)
	defer cancel()
	dialed, err := client.DialContext(ctx, "udp", server.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer dialed.Close()

	select {
	case srvConn := <-accepted:
		defer srvConn.Close()
	case <-ctx.Done():
		t.Fatal("accept timed out")
	}

	// Handshake done: the peer goes silent, every client packet now times out.
	bh.silent.Store(true)

	_ = dialed.SetWriteDeadline(time.Now().Add(window))
	_, _ = dialed.Write(make([]byte, 8<<20))
	timeout, _ := client.ResendStats()
	mu.Lock()
	var maxBackoff uint32
	for _, cc := range client.conns {
		if cc.rtoBackoff > maxBackoff {
			maxBackoff = cc.rtoBackoff
		}
	}
	mu.Unlock()
	return timeout, maxBackoff
}

// A/B measurement of the adaptive RTO against a peer that goes silent: the
// backoff must space the retransmissions out and send fewer of them. Opt-in
// (GX_MEASURE_UTP=1), so it stays out of the normal suite.
func TestAdaptiveRTOMeasurement(t *testing.T) {
	if os.Getenv("GX_MEASURE_UTP") == "" {
		t.Skip("set GX_MEASURE_UTP=1 to run the RTO A/B measurement")
	}
	const window = 4 * time.Second
	var adaptiveTotal, fixedTotal int64
	for i := 0; i < 3; i++ {
		adaptive, adaptiveBackoff := silentPeerResends(t, true, 5*time.Millisecond, window)
		fixed, fixedBackoff := silentPeerResends(t, false, 5*time.Millisecond, window)
		adaptiveTotal += adaptive
		fixedTotal += fixed
		t.Logf("run %d, silent peer, %v: RTO resends adaptive=%d (backoff %d) fixed=%d (backoff %d)",
			i, window, adaptive, adaptiveBackoff, fixed, fixedBackoff)
	}
	if adaptiveTotal >= fixedTotal {
		t.Fatalf("adaptive RTO did not reduce resends: adaptive=%d fixed=%d", adaptiveTotal, fixedTotal)
	}
	t.Logf("backoff, total over 3 runs: adaptive=%d fixed=%d (%.0f%% fewer)", adaptiveTotal, fixedTotal,
		100*float64(fixedTotal-adaptiveTotal)/float64(fixedTotal))

	// The minimum-latency floor, with the backoff off: on a fast path the
	// sampled RTT is tens of microseconds, so a 1 µs floor is a resend storm;
	// the 5 ms floor is the fix.
	const short = time.Second
	floorOn, _ := silentPeerResends(t, false, 5*time.Millisecond, short)
	floorOff, _ := silentPeerResends(t, false, time.Microsecond, short)
	t.Logf("floor, silent peer %v, backoff off: RTO resends 5ms floor=%d 1µs floor=%d (%.1fx storm)",
		short, floorOn, floorOff, float64(floorOff)/float64(floorOn))
	if floorOn >= floorOff {
		t.Fatalf("minimum-latency floor did not reduce resends: %d vs %d", floorOn, floorOff)
	}
}
