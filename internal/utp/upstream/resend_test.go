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
