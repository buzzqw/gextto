package utp

import (
	"context"
	"net"
	"testing"
	"time"
)

// Loopback dial/accept/echo through the wrapper, plus the activity counters.
func TestSocketLoopbackEcho(t *testing.T) {
	serverPC, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewSocketFromPacketConn(serverPC)
	if err != nil {
		t.Fatal(err)
	}
	defer server.CloseNow()
	if server.Addr() == nil || server.LocalAddr() == nil {
		t.Fatal("nil socket address")
	}

	clientPC, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewSocketFromPacketConn(clientPC)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	accepted := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		c, err := server.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- c
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dialed, err := client.DialContext(ctx, "udp", server.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer dialed.Close()

	var srvConn net.Conn
	select {
	case srvConn = <-accepted:
	case err := <-acceptErr:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal("accept timed out")
	}
	defer srvConn.Close()

	msg := []byte("utp-wrapper-ping")
	if err := dialed.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := dialed.Write(msg); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	if err := srvConn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	n, err := srvConn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != string(msg) {
		t.Fatalf("echo mismatch: %q", buf[:n])
	}

	st := client.Stats()
	if st.Dials != 1 || st.DialErrors != 0 {
		t.Fatalf("client stats: %+v", st)
	}
	st = server.Stats()
	if st.Accepts != 1 || st.AcceptErrors != 0 {
		t.Fatalf("server stats: %+v", st)
	}
	if st.ResendsTimeout != 0 || st.ResendsFast != 0 {
		t.Fatalf("fresh socket resend stats: %+v", st)
	}
}
