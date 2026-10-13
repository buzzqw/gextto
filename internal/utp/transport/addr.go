package transport

// GEXTTO: our own uTP (BEP 29) code; see doc.go. addr presents a uTP connection's underlying
// socket address, tagged with the "utp/" network so it can be told apart from a
// plain UDP or TCP address.

import "net"

type addr struct {
	socket net.Addr
}

// Network returns the underlying network prefixed with "utp/".
func (a addr) Network() string { return "utp/" + a.socket.Network() }

// String returns the underlying address.
func (a addr) String() string { return a.socket.String() }
