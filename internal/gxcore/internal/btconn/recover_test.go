package btconn

import (
	"net"
	"testing"
	"time"
)

// panickingConn is a socket whose every operation panics: the shape of a bug
// in the handshake reached by a stranger's bytes.
type panickingConn struct{}

func (panickingConn) Read([]byte) (int, error)         { panic("boom") }
func (panickingConn) Write([]byte) (int, error)        { panic("boom") }
func (panickingConn) Close() error                     { return nil }
func (panickingConn) LocalAddr() net.Addr              { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }
func (panickingConn) RemoteAddr() net.Addr             { return &net.TCPAddr{IP: net.IPv4(203, 0, 113, 5)} }
func (panickingConn) SetDeadline(time.Time) error      { return nil }
func (panickingConn) SetReadDeadline(time.Time) error  { return nil }
func (panickingConn) SetWriteDeadline(time.Time) error { return nil }

// TestAcceptRoutedTurnsAPanicIntoAnError pins that a handshake fault is an
// error for the connection, not the death of the daemon: the caller closes the
// socket and everything else keeps running.
func TestAcceptRoutedTurnsAPanicIntoAnError(t *testing.T) {
	lookup := func([20]byte) (ourID [20]byte, ourExtensions [8]byte, ok bool) { return [20]byte{}, [8]byte{}, true }
	conn, _, _, _, _, err := AcceptRouted(panickingConn{}, time.Second, nil, false, lookup)
	if err == nil {
		t.Fatal("a panicking handshake returned no error")
	}
	if conn != nil {
		t.Fatal("a panicking handshake returned a connection")
	}
}

// TestDialTurnsAPanicIntoAnError is the outgoing side of the same rule.
func TestDialTurnsAPanicIntoAnError(t *testing.T) {
	conn, _, _, _, err := Dial(panickingAddr{}, time.Second, time.Second, false, false, [8]byte{}, [20]byte{}, [20]byte{}, make(chan struct{}))
	if err == nil {
		t.Fatal("a panicking dial returned no error")
	}
	if conn != nil {
		t.Fatal("a panicking dial returned a connection")
	}
}

// panickingAddr is a dial target whose String panics, which fails the dial
// before any socket is opened.
type panickingAddr struct{}

func (panickingAddr) Network() string { return "tcp" }
func (panickingAddr) String() string  { panic("boom") }
