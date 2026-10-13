package peerconn

import (
	"io"
	"net"
	"sync/atomic"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bandwidth"
	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerconn/peerreader"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerconn/peerwriter"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
)

// Wire byte counters, tallied below the message layer so the session can
// report protocol and encryption overhead as wire minus payload.
var (
	WireBytesRead    atomic.Int64
	WireBytesWritten atomic.Int64
)

// countingConn wraps a peer socket with wire accounting.
type countingConn struct {
	net.Conn
}

func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	WireBytesRead.Add(int64(n))
	return n, err
}

func (c *countingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	WireBytesWritten.Add(int64(n))
	return n, err
}

// Conn couples a socket with its reader and writer goroutines, merging both
// message streams into one channel for the owning peer.
type Conn struct {
	conn     net.Conn
	reader   *peerreader.PeerReader
	writer   *peerwriter.PeerWriter
	messages chan any
	log      logger.Logger
	closeC   chan struct{}
	doneC    chan struct{}
}

// New wraps conn with rate-limited reading and writing.
func New(conn net.Conn, l logger.Logger, pieceTimeout time.Duration, maxRequestsIn, maxMsgSize int, fastEnabled bool, br, bw *bandwidth.Limiter) *Conn {
	conn = &countingConn{Conn: conn}
	return &Conn{
		conn:     conn,
		reader:   peerreader.New(conn, l, pieceTimeout, maxMsgSize, br),
		writer:   peerwriter.New(conn, l, maxRequestsIn, fastEnabled, bw),
		messages: make(chan any),
		log:      l,
		closeC:   make(chan struct{}),
		doneC:    make(chan struct{}),
	}
}

// Addr returns the peer's address.
func (p *Conn) Addr() *net.TCPAddr {
	return p.conn.RemoteAddr().(*net.TCPAddr)
}

// IP returns the peer's IP in string form.
func (p *Conn) IP() string {
	return p.conn.RemoteAddr().(*net.TCPAddr).IP.String()
}

// String returns the peer's remote address.
func (p *Conn) String() string {
	return p.conn.RemoteAddr().String()
}

// Close stops the reader and writer and waits for the run loop to exit.
func (p *Conn) Close() {
	close(p.closeC)
	<-p.doneC
}

// Logger prefixes peer logs with the connection identity.
func (p *Conn) Logger() logger.Logger {
	return p.log
}

// Messages streams everything received from or reported about the peer. It
// closes when the run loop exits; any reader or writer error ends the loop
// and the socket with it.
func (p *Conn) Messages() <-chan any {
	return p.messages
}

// SendMessage queues a protocol message without blocking.
func (p *Conn) SendMessage(msg peerprotocol.Message) {
	p.writer.SendMessage(msg)
}

// SendPiece queues one block upload without blocking. The bytes are read
// from storage just before sending, so large pieces never fully sit in
// memory. Past maxRequestsIn queued messages the newest is dropped.
func (p *Conn) SendPiece(msg peerprotocol.RequestMessage, pi io.ReaderAt) {
	p.writer.SendPiece(msg, pi)
}

// CancelRequest drops a queued piece message matching msg.
func (p *Conn) CancelRequest(msg peerprotocol.CancelMessage) {
	p.writer.CancelRequest(msg)
}

// Run pumps reader and writer streams into Messages until Close, an error on
// either side, or a peer going silent — then it stops the other side, closes
// the socket, and returns.
func (p *Conn) Run() {
	defer close(p.doneC)
	defer close(p.messages)

	p.log.Debugln("Communicating peer", p.conn.RemoteAddr())

	go p.reader.Run()
	defer func() { <-p.reader.Done() }()

	go p.writer.Run()
	defer func() { <-p.writer.Done() }()

	defer p.conn.Close()
	for {
		select {
		case msg := <-p.reader.Messages():
			p.relay(msg)
		case msg := <-p.writer.Messages():
			p.relay(msg)
		case <-p.closeC:
			p.reader.Stop()
			p.writer.Stop()
			return
		case <-p.reader.Done():
			p.writer.Stop()
			return
		case <-p.writer.Done():
			p.reader.Stop()
			return
		}
	}
}

// relay forwards one message unless the connection is closing.
func (p *Conn) relay(msg any) {
	select {
	case p.messages <- msg:
	case <-p.closeC:
	}
}
