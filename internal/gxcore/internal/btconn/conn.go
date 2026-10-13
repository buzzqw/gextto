// Package btconn dials and accepts raw BitTorrent connections: plain or
// MSE-encrypted TCP, plus the uTP path assembled by the session layer.
package btconn

import (
	"io"
	"net"
)

// readWriter glues a separately-owned reader and writer into one stream,
// used when the read side (e.g. an MSE decryptor) differs from the write side.
type readWriter struct {
	io.Reader
	io.Writer
}

// rwConn presents a split readWriter stream as a net.Conn by borrowing the
// metadata (addresses, deadlines) of an underlying connection.
type rwConn struct {
	rw io.ReadWriter
	net.Conn
}

func (c *rwConn) Read(p []byte) (n int, err error)  { return c.rw.Read(p) }
func (c *rwConn) Write(p []byte) (n int, err error) { return c.rw.Write(p) }
