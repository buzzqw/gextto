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

// WireStats tallies the raw peer-socket bytes below the message layer, so
// the session can report protocol overhead as wire minus payload. One
// instance is owned by a Session and shared by all its peers: keeping it
// per-session (not process-global) makes both sides of the subtraction the
// same scope, also across engine session restarts.
type WireStats struct {
	Read    atomic.Int64
	Written atomic.Int64
}

// countingConn wraps a peer socket with wire accounting into stats, and
// remembers when the peer last sent a byte.
type countingConn struct {
	net.Conn
	stats *WireStats
	// lastRead is the UnixNano time of the last read that returned bytes.
	lastRead atomic.Int64
}

func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.lastRead.Store(time.Now().UnixNano())
		if c.stats != nil {
			c.stats.Read.Add(int64(n))
		}
	}
	return n, err
}

// HintRead forwards the reader's hint to a socket that can use it (uTP).
func (c *countingConn) HintRead(n int) {
	if h, ok := c.Conn.(interface{ HintRead(int) }); ok {
		h.HintRead(n)
	}
}

func (c *countingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if c.stats != nil {
		c.stats.Written.Add(int64(n))
	}
	return n, err
}

// Conn couples a socket with its reader and writer goroutines, merging both
// message streams into one channel for the owning peer.
type Conn struct {
	conn     net.Conn
	counting *countingConn
	reader   *peerreader.PeerReader
	writer   *peerwriter.PeerWriter
	messages chan any
	log      logger.Logger
	closeC   chan struct{}
	doneC    chan struct{}
}

// New wraps conn with rate-limited reading and writing. Wire bytes are
// accumulated into stats (nil disables the accounting).
func New(conn net.Conn, stats *WireStats, l logger.Logger, pieceTimeout time.Duration, maxRequestsIn, maxMsgSize int, fastEnabled bool, br, bw *bandwidth.Limiter) *Conn {
	counting := &countingConn{Conn: conn, stats: stats}
	counting.lastRead.Store(time.Now().UnixNano())
	conn = counting
	return &Conn{
		conn:     conn,
		counting: counting,
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

// SilentFor returns how long the peer has sent nothing (since the connection
// was adopted when it never sent a byte).
func (p *Conn) SilentFor() time.Duration {
	return time.Since(time.Unix(0, p.counting.lastRead.Load()))
}

// String returns the peer's remote address.
func (p *Conn) String() string {
	return p.conn.RemoteAddr().String()
}

// SetUploadClasses attaches the session's trading/seeding upload split.
func (p *Conn) SetUploadClasses(classes *bandwidth.UploadClasses) {
	p.writer.SetUploadClasses(classes)
}

// SetUploadClass sets the class this peer's uploads count in.
func (p *Conn) SetUploadClass(class int32) { p.writer.SetUploadClass(class) }

// UploadClass reports the class this peer's uploads count in.
func (p *Conn) UploadClass() int32 { return p.writer.UploadClass() }

// SetUploadWeight sets this peer's own share of a limited upload in percent.
func (p *Conn) SetUploadWeight(percent int32) { p.writer.SetUploadWeight(percent) }

// UploadWeight reports this peer's own share of a limited upload in percent.
func (p *Conn) UploadWeight() int32 { return p.writer.UploadWeight() }

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
