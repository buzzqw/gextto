package mse

import (
	"net"
)

// Conn layers message-stream encryption over a peer connection.
// Reads and writes pass through the negotiated Stream cipher.
type Conn struct {
	net.Conn
	*Stream
}

// WrapConn attaches a fresh, pre-handshake Stream to conn. Complete
// HandshakeIncoming or HandshakeOutgoing before exchanging data.
func WrapConn(conn net.Conn) *Conn {
	return &Conn{Conn: conn, Stream: NewStream(conn)}
}

func (c *Conn) Read(p []byte) (n int, err error) {
	return c.Stream.Read(p)
}

func (c *Conn) Write(p []byte) (n int, err error) {
	return c.Stream.Write(p)
}
