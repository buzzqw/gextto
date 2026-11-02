// Package btconn dials and accepts raw BitTorrent connections: plain or
// MSE-encrypted TCP, plus the uTP path assembled by the session layer.
package btconn

import (
	"bytes"
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
	// replay holds the bytes rw serves before the socket, if any.
	replay *bytes.Buffer
}

// HintRead forwards a reader's hint to the socket, without the bytes rw still
// holds: the socket will never see those.
func (c *rwConn) HintRead(n int) {
	if c.replay != nil {
		n -= c.replay.Len()
	}
	if n <= 0 {
		return
	}
	if h, ok := c.Conn.(interface{ HintRead(int) }); ok {
		h.HintRead(n)
	}
}

func (c *rwConn) Read(p []byte) (n int, err error)  { return c.rw.Read(p) }
func (c *rwConn) Write(p []byte) (n int, err error) { return c.rw.Write(p) }
