package torrent

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/handshaker/incominghandshaker"
)

// TestIncomingHandshakeFailureClosesSocket checks the torrent-loop half of the
// ownership handoff: even if a handshaker reports an error without closing, the
// per-torrent result handler must close the socket and release the pinned IP.
func TestIncomingHandshakeFailureClosesSocket(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	remote := <-accepted
	defer remote.Close()

	ip := conn.RemoteAddr().(*net.TCPAddr).IP.String()
	h := incominghandshaker.New(conn)
	h.Error = errors.New("handshake failed")
	tt := &torrent{
		incomingHandshakers: map[*incominghandshaker.IncomingHandshaker]struct{}{h: {}},
		connectedPeerIPs:    map[string]struct{}{ip: {}},
	}
	tt.handleIncomingHandshakeDone(h)
	if _, ok := tt.incomingHandshakers[h]; ok {
		t.Fatal("failed handshaker was not retired")
	}
	if _, ok := tt.connectedPeerIPs[ip]; ok {
		t.Fatal("failed handshake left its IP pinned")
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("failed incoming handshake left its socket open")
	}
}
