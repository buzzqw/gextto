package peer

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
)

// MaxPacketSize bounds one client packet. A compressed part carries at most
// one 9,728,000-byte part (plus its zlib overhead and the 24-byte header), so
// 16 MiB leaves room without letting a peer exhaust memory.
const MaxPacketSize = 16 << 20

// Conn is a framed eMule client connection. It owns the underlying net.Conn;
// callers set deadlines through the context of Handshake or directly on the
// connection for the rest of the session.
type Conn struct {
	conn   net.Conn
	reader *bufio.Reader
	max    int
}

// NewConn wraps an established TCP connection.
func NewConn(conn net.Conn) *Conn {
	return &Conn{conn: conn, reader: bufio.NewReaderSize(conn, 32<<10), max: MaxPacketSize}
}

// RemoteAddr exposes the peer address for logs and source bookkeeping.
func (c *Conn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }

// SetMaxPacketSize lowers or raises the payload cap. Invalid values are
// rejected so a caller cannot accidentally disable the guard.
func (c *Conn) SetMaxPacketSize(size int) error {
	if size <= 0 || size > MaxPacketSize {
		return fmt.Errorf("peer: invalid packet size %d", size)
	}
	c.max = size
	return nil
}

// Send writes one framed packet. It writes the whole frame or returns an
// error, so a partial packet is never left on the wire.
func (c *Conn) Send(opcode byte, payload []byte) error {
	if len(payload) > c.max {
		return fmt.Errorf("peer: packet of %d bytes exceeds the limit", len(payload))
	}
	frame := Frame(opcode, payload)
	for len(frame) > 0 {
		written, err := c.conn.Write(frame)
		if err != nil {
			return err
		}
		frame = frame[written:]
	}
	return nil
}

// Receive reads one framed packet. The payload is capped before allocation;
// a peer that announces a larger packet is disconnected by the caller.
func (c *Conn) Receive() (byte, []byte, error) {
	var header [6]byte
	if _, err := io.ReadFull(c.reader, header[:]); err != nil {
		return 0, nil, err
	}
	if header[0] != ClientProtocol {
		return 0, nil, errors.New("peer: not an eMule client packet")
	}
	size := int(uint32(header[2]) | uint32(header[3])<<8 | uint32(header[4])<<16 | uint32(header[5])<<24)
	if size > c.max {
		return 0, nil, fmt.Errorf("peer: packet of %d bytes exceeds the limit", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		return 0, nil, err
	}
	return header[1], payload, nil
}

// Handshake sends our hello and returns the peer's answer. The context
// deadline (when present) bounds the exchange; the previous deadline is
// restored afterwards so the download loop controls its own timeouts.
func (c *Conn) Handshake(ctx context.Context, hello Hello) (Hello, error) {
	if hello.UserHash == (ed2k.ID{}) {
		return Hello{}, errors.New("peer: hello without a user hash")
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := c.conn.SetDeadline(deadline); err != nil {
			return Hello{}, err
		}
		defer c.conn.SetDeadline(time.Time{})
	}
	if err := c.Send(OpHello, EncodeHello(hello)); err != nil {
		return Hello{}, fmt.Errorf("peer: send hello: %w", err)
	}
	opcode, payload, err := c.Receive()
	if err != nil {
		return Hello{}, fmt.Errorf("peer: read hello answer: %w", err)
	}
	if opcode != OpHelloAnswer {
		return Hello{}, fmt.Errorf("peer: expected hello answer, got opcode 0x%02X", opcode)
	}
	answer, err := DecodeHello(payload)
	if err != nil {
		return Hello{}, err
	}
	if answer.UserHash == hello.UserHash {
		return Hello{}, errors.New("peer: hello answered by ourselves")
	}
	if answer.UserHash == (ed2k.ID{}) {
		return Hello{}, errors.New("peer: hello answer without a user hash")
	}
	return answer, nil
}

// Close closes the underlying connection.
func (c *Conn) Close() error { return c.conn.Close() }
