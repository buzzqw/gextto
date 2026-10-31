package netx

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// resetDialPrefs clears the per-host memory for test isolation.
func resetDialPrefs() {
	prefMu.Lock()
	dialPrefs = map[string]*transportPref{}
	prefMu.Unlock()
}

// stubAddrConn returns a conn with UDP addresses, like a uTP conn.
func stubAddrConn() net.Conn {
	a, b := net.Pipe()
	go func() {
		_ = b.Close()
	}()
	return &udpAddrConn{Conn: a}
}

type udpAddrConn struct {
	net.Conn
}

func (c *udpAddrConn) RemoteAddr() net.Addr {
	return &net.UDPAddr{IP: net.ParseIP("203.0.113.7"), Port: 51413}
}

func (c *udpAddrConn) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.ParseIP("192.0.2.1"), Port: 6881}
}

func BenchmarkUTPConnRemoteAddr(b *testing.B) {
	c := TrackUTP(NewUTPConn(stubAddrConn()))
	defer c.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = c.RemoteAddr().String()
	}
}

// pipeDialer is an instant-success uTP dialer counting its invocations.
func pipeDialer(calls *atomic.Int64) func(context.Context, string) (net.Conn, error) {
	return func(_ context.Context, _ string) (net.Conn, error) {
		calls.Add(1)
		a, b := net.Pipe()
		go func() {
			_ = b.Close()
		}()
		return a, nil
	}
}

func TestPreferredDialTransportNeedsSamplesAndShare(t *testing.T) {
	resetDialPrefs()
	host := "198.51.100.7"
	if got := preferredDialTransport(host); got != "" {
		t.Fatalf("empty memory prefers %q", got)
	}
	for i := 0; i < preferMinSamples-1; i++ {
		noteDialWinner(host, "utp", time.Millisecond)
	}
	if got := preferredDialTransport(host); got != "" {
		t.Fatalf("below min samples prefers %q", got)
	}
	noteDialWinner(host, "tcp", time.Millisecond)
	noteDialWinner(host, "tcp", time.Millisecond)
	// 3 utp vs 2 tcp: no 75% share.
	if got := preferredDialTransport(host); got != "" {
		t.Fatalf("without share prefers %q", got)
	}
	noteDialWinner(host, "utp", time.Millisecond)
	noteDialWinner(host, "utp", time.Millisecond)
	noteDialWinner(host, "utp", time.Millisecond)
	// 6 utp vs 2 tcp = 75%.
	if got := preferredDialTransport(host); got != "utp" {
		t.Fatalf("expected utp preference, got %q", got)
	}
}

func TestNoteDialWinnerDecaysStaleHistory(t *testing.T) {
	resetDialPrefs()
	host := "198.51.100.8"
	for i := 0; i < preferMaxSamples; i++ {
		noteDialWinner(host, "tcp", time.Millisecond)
	}
	prefMu.Lock()
	p := dialPrefs[host]
	prefMu.Unlock()
	if p.utpWins+p.tcpWins > preferMaxSamples {
		t.Fatalf("history exceeds cap: %+v", p)
	}
	if got := preferredDialTransport(host); got != "tcp" {
		t.Fatalf("expected tcp preference after decay, got %q", got)
	}
	// New evidence can still flip the preference.
	for i := 0; i < preferMaxSamples; i++ {
		noteDialWinner(host, "utp", time.Millisecond)
	}
	if got := preferredDialTransport(host); got != "utp" {
		t.Fatalf("expected flipped utp preference, got %q", got)
	}
}

// The recent winner must get its head start: with uTP preferred and winning
// instantly, the TCP leg is cancelled before dialling — no wasted connection.
func TestDialPeerSkipsLoserWhenPreferredWins(t *testing.T) {
	old := preferHeadStart
	preferHeadStart = 300 * time.Millisecond
	defer func() { preferHeadStart = old }()
	resetDialPrefs()

	var tcpAccepted atomic.Int64
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			tcpAccepted.Add(1)
			_ = c.Close()
		}
	}()

	var utpDials atomic.Int64
	SetUTPDialer(pipeDialer(&utpDials), false)
	defer SetUTPDialer(nil, false)

	closed := "127.0.0.1:1" // refused: TCP always loses while seeding
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := 0; i < preferMinSamples; i++ {
		c, err := DialPeer(ctx, net.Dialer{}, "tcp", closed)
		if err != nil {
			t.Fatal(err)
		}
		_ = c.Close()
	}
	if got := preferredDialTransport("127.0.0.1"); got != "utp" {
		t.Fatalf("expected utp preference, got %q", got)
	}

	before := tcpAccepted.Load()
	c, err := DialPeer(ctx, net.Dialer{}, "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !IsUTP(c) {
		t.Fatal("expected the preferred uTP leg to win")
	}
	// The 300ms head start must have cancelled TCP before it dialled.
	time.Sleep(100 * time.Millisecond)
	if got := tcpAccepted.Load() - before; got != 0 {
		t.Fatalf("loser TCP leg dialled %d times", got)
	}
}

// The delayed leg is a fallback, not a requirement: with TCP preferred but
// unreachable, the dial still succeeds over uTP.
func TestDialPeerFallsBackToDelayedLeg(t *testing.T) {
	old := preferHeadStart
	preferHeadStart = 300 * time.Millisecond
	defer func() { preferHeadStart = old }()
	resetDialPrefs()

	// The seeding listener stays up: it collects the real races that earn
	// TCP the head start.
	seed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer seed.Close()
	seedAddr := seed.Addr().String()

	// The fallback dials a port with nothing behind it. It is a second port
	// of the same host (the preference is kept per host) so that a uTP leg
	// of a seeding race landing after DialPeer returned cannot be counted
	// as the fallback's dial.
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadAddr := dead.Addr().String()
	_ = dead.Close()

	var utpDials atomic.Int64
	SetUTPDialer(func(_ context.Context, addr string) (net.Conn, error) {
		// uTP is unreachable at the seeding port (so TCP earns the head
		// start) and works at the fallback port.
		if addr != deadAddr {
			return nil, context.DeadlineExceeded
		}
		utpDials.Add(1)
		a, b := net.Pipe()
		go func() {
			_ = b.Close()
		}()
		return a, nil
	}, false)
	defer SetUTPDialer(nil, false)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Seed a TCP preference: uTP fails, TCP to the open listener wins.
	for i := 0; i < preferMinSamples; i++ {
		c, err := DialPeer(ctx, net.Dialer{}, "tcp", seedAddr)
		if err != nil {
			t.Fatal(err)
		}
		_ = c.Close()
	}
	if got := preferredDialTransport("127.0.0.1"); got != "tcp" {
		t.Fatalf("expected tcp preference, got %q", got)
	}

	// Now TCP is dead while uTP works: the delayed leg must still connect.
	start := time.Now()
	c, err := DialPeer(ctx, net.Dialer{}, "tcp", deadAddr)
	if err != nil {
		t.Fatalf("delayed uTP leg did not fall back: %v", err)
	}
	defer c.Close()
	if !IsUTP(c) {
		t.Fatal("expected the fallback uTP leg")
	}
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Fatalf("fallback used no head start (elapsed %v)", elapsed)
	}
	if utpDials.Load() != 1 {
		t.Fatalf("expected exactly one uTP dial, got %d", utpDials.Load())
	}
}

// DialLatencyEMA serves the uTP initial-RTO seed: unknown hosts report
// nothing, known hosts the winning leg's moving average (uTP preferred).
func TestDialLatencyEMA(t *testing.T) {
	resetDialPrefs()
	if _, ok := DialLatencyEMA("198.51.100.9:51413"); ok {
		t.Fatal("unknown host must report no estimate")
	}
	noteDialWinner("198.51.100.9", "tcp", 120*time.Millisecond)
	noteDialWinner("198.51.100.9", "tcp", 120*time.Millisecond)
	if ema, ok := DialLatencyEMA("198.51.100.9:51413"); !ok || ema != 120*time.Millisecond {
		t.Fatalf("tcp fallback EMA = %v, %v", ema, ok)
	}
	noteDialWinner("198.51.100.9", "utp", 30*time.Millisecond)
	if ema, ok := DialLatencyEMA("198.51.100.9:51413"); !ok || ema != 30*time.Millisecond {
		t.Fatalf("utp EMA = %v, %v, want 30ms", ema, ok)
	}
}
