package peerreader

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bandwidth"
	"github.com/buzzqw/gextto/internal/gxcore/internal/bufferpool"
	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

const (
	// MaxBlockSize caps a single request's block.
	MaxBlockSize = 16 * 1024
	// readTimeout bounds idleness: peers must keepalive within it.
	readTimeout = 2 * time.Minute
	// readBufferSize frames the common fixed head: length + id + request.
	readBufferSize = 4 + 1 + 12

	// The session cap (maxMsgSize, from MaxMetadataSize) bounds every message
	// at 30 MiB, which is far too large to allocate on a peer's say-so: a
	// single variable-length message would hand a remote peer a 30 MiB buffer
	// and, before the metadata, a place to keep it. Each variable-length type
	// gets its own ceiling, generous for the protocol and small in memory.
	//
	// maxBitfieldMessage is 1 MiB: 8.4 million pieces, beyond any real torrent.
	maxBitfieldMessage = 1 << 20
	// maxExtensionMessage is 256 KiB: BEP 10 handshake, a 16 KiB metadata
	// block and a PEX gossip batch all fit with room to spare.
	maxExtensionMessage = 256 << 10
	// maxHashesMessage is 64 KiB: BEP 52 permits 512 hashes of 32 bytes, plus
	// the header.
	maxHashesMessage = 64 << 10
)

// blockPool recycles piece-block buffers across connections.
var blockPool = bufferpool.New(piece.BlockSize)

// PeerReader parses peer wire messages off a connection, enforcing size caps
// before allocating and throttling block intake through the read limiter.
type PeerReader struct {
	conn         net.Conn
	r            io.Reader
	log          logger.Logger
	pieceTimeout time.Duration
	maxMsgSize   int
	bucket       *bandwidth.Limiter // rate changes at runtime
	messages     chan any
	stopC        chan struct{}
	doneC        chan struct{}
	dr           *deadlineReader
}

// New wraps conn; maxMsgSize bounds any single message before its buffer is
// allocated, closing the hostile-allocation hole on variable-length messages.
func New(conn net.Conn, l logger.Logger, pieceTimeout time.Duration, maxMsgSize int, b *bandwidth.Limiter) *PeerReader {
	dr := &deadlineReader{conn: conn, window: readTimeout}
	return &PeerReader{
		conn:         conn,
		r:            bufio.NewReaderSize(dr, readBufferSize),
		dr:           dr,
		log:          l,
		pieceTimeout: pieceTimeout,
		maxMsgSize:   maxMsgSize,
		bucket:       b,
		messages:     make(chan any),
		stopC:        make(chan struct{}),
		doneC:        make(chan struct{}),
	}
}

// Messages streams the parsed messages.
func (p *PeerReader) Messages() <-chan any {
	return p.messages
}

// Stop ends the read loop; Done closes when it exited.
func (p *PeerReader) Stop() {
	close(p.stopC)
}

// Done closes when the read loop exits.
func (p *PeerReader) Done() chan struct{} {
	return p.doneC
}

// deadlineReader puts a progress-based read deadline under the message
// parser. A read fails only when the peer sent nothing for a whole window, or
// when the transport reports a timeout before the armed deadline. The second
// case is a dead connection that calls itself a timeout: uTP reports its ack
// timeout that way. The deadline is re-armed only when it passes, not per
// message or block. On uTP SetReadDeadline takes the transport-wide lock and
// resets a timer. Arming it per block was a large share of the CPU under load.
// Retrying a dead connection's "timeout" made the reader spin on that lock.
type deadlineReader struct {
	conn       net.Conn
	window     time.Duration // silence tolerated before a read fails
	deadline   time.Time     // the armed socket read deadline
	progressed bool          // bytes arrived since the deadline was armed
}

// arm sets the socket read deadline window from now.
func (d *deadlineReader) arm(window time.Duration) error {
	t := time.Now().Add(window)
	if err := d.conn.SetReadDeadline(t); err != nil {
		return err
	}
	d.deadline = t
	d.progressed = false
	return nil
}

func (d *deadlineReader) Read(b []byte) (int, error) {
	for {
		n, err := d.conn.Read(b)
		if n > 0 {
			d.progressed = true
		}
		if err == nil {
			return n, nil
		}
		nerr, ok := err.(net.Error)
		if !ok || !nerr.Timeout() || time.Now().Before(d.deadline) {
			return n, err // real error, or a dead connection posing as a timeout
		}
		if n > 0 {
			return n, nil // the next Read re-arms
		}
		if !d.progressed {
			return 0, err // silent for a whole window
		}
		if err = d.arm(d.window); err != nil {
			return 0, err
		}
	}
}

// Run reads length-prefixed messages until error or Stop. Routine endings
// (peer hangup, truncation, throttled stop, network errors, or a stop while
// an error is pending) stay quiet; block-size violations log at debug;
// anything else logs as an error.
func (p *PeerReader) Run() {
	defer close(p.doneC)

	var err error
	defer func() {
		if err == nil {
			return
		}
		switch {
		case err == io.EOF:
			return
		case err == io.ErrUnexpectedEOF:
			return
		case err == errStoppedWhileWaitingBucket:
			return
		}
		if _, ok := err.(*net.OpError); ok {
			return
		}
		select {
		case <-p.stopC:
		default:
			if _, ok := err.(*blockSizeError); ok {
				p.log.Debug(err)
			} else {
				p.log.Error(err)
			}
		}
	}()

	if err = p.dr.arm(readTimeout); err != nil {
		return
	}
	// The fixed-size head is read into stack arrays: binary.Read would allocate
	// a buffer and go through reflection for every message of every peer.
	var head [4]byte
	var idb [1]byte
	for {
		if err = p.readFull(head[:]); err != nil {
			return
		}
		length := binary.BigEndian.Uint32(head[:])
		if length == 0 {
			p.log.Debug("Received message of type \"keep alive\"")
			continue
		}
		if err = p.readFull(idb[:]); err != nil {
			return
		}
		id := peerprotocol.MessageID(idb[0])
		length--
		if int64(length) > int64(p.maxMsgSize) {
			err = fmt.Errorf("received message larger than allowed (%d > %d)", length, p.maxMsgSize)
			return
		}
		var msg any
		if msg, err = p.parseMessage(id, length); err != nil {
			return
		}
		if msg == nil {
			continue // unknown type, already discarded
		}
		select {
		case p.messages <- msg:
		case <-p.stopC:
			return
		}
	}
}

// readFull fills b without allocating, for the fixed-size message bodies.
func (p *PeerReader) readFull(b []byte) error {
	_, err := io.ReadFull(p.r, b)
	return err
}

// readRequest decodes index, begin and length, the body shared by request,
// reject and cancel.
func (p *PeerReader) readRequest() (peerprotocol.RequestMessage, error) {
	var b [12]byte
	if err := p.readFull(b[:]); err != nil {
		return peerprotocol.RequestMessage{}, err
	}
	return peerprotocol.RequestMessage{
		Index:  binary.BigEndian.Uint32(b[0:4]),
		Begin:  binary.BigEndian.Uint32(b[4:8]),
		Length: binary.BigEndian.Uint32(b[8:12]),
	}, nil
}

// parseMessage decodes one message body of the given id and remaining
// length. Unknown ids are discarded inline and reported as (nil, nil).
func (p *PeerReader) parseMessage(id peerprotocol.MessageID, length uint32) (any, error) {
	var err error
	switch id {
	case peerprotocol.Choke:
		p.log.Debug("Received Choke")
		return peerprotocol.ChokeMessage{}, nil
	case peerprotocol.Unchoke:
		p.log.Debug("Received Unchoke")
		return peerprotocol.UnchokeMessage{}, nil
	case peerprotocol.Interested:
		p.log.Debug("Received Interested")
		return peerprotocol.InterestedMessage{}, nil
	case peerprotocol.NotInterested:
		p.log.Debug("Received NotInterested")
		return peerprotocol.NotInterestedMessage{}, nil
	case peerprotocol.Have:
		var b [4]byte
		if err = p.readFull(b[:]); err != nil {
			return nil, err
		}
		return peerprotocol.HaveMessage{Index: binary.BigEndian.Uint32(b[:])}, nil
	case peerprotocol.Bitfield:
		if length > maxBitfieldMessage {
			return nil, &blockSizeError{messageID: id, got: length, allowedMax: maxBitfieldMessage}
		}
		var bm peerprotocol.BitfieldMessage
		bm.Data = make([]byte, length)
		if _, err = io.ReadFull(p.r, bm.Data); err != nil {
			return nil, err
		}
		return bm, nil
	case peerprotocol.Request:
		rm, err := p.readRequest()
		if err != nil {
			return nil, err
		}
		if rm.Length > MaxBlockSize {
			return nil, &blockSizeError{messageID: id, got: rm.Length, allowedMax: MaxBlockSize}
		}
		return rm, nil
	case peerprotocol.Reject:
		rm, err := p.readRequest()
		if err != nil {
			return nil, err
		}
		p.log.Debugf("Received Reject: %+v", rm)
		return peerprotocol.RejectMessage{RequestMessage: rm}, nil
	case peerprotocol.Cancel:
		cm, err := p.readRequest()
		if err != nil {
			return nil, err
		}
		return peerprotocol.CancelMessage{RequestMessage: cm}, nil
	case peerprotocol.Piece:
		return p.parsePiece(length)
	case peerprotocol.HaveAll:
		return peerprotocol.HaveAllMessage{}, nil
	case peerprotocol.HaveNone:
		return peerprotocol.HaveNoneMessage{}, nil
	case peerprotocol.AllowedFast:
		var b [4]byte
		if err = p.readFull(b[:]); err != nil {
			return nil, err
		}
		return peerprotocol.AllowedFastMessage{HaveMessage: peerprotocol.HaveMessage{Index: binary.BigEndian.Uint32(b[:])}}, nil
	case peerprotocol.Port:
		var b [2]byte
		if err = p.readFull(b[:]); err != nil {
			return nil, err
		}
		return peerprotocol.PortMessage{Port: binary.BigEndian.Uint16(b[:])}, nil
	case peerprotocol.Extension:
		if length > maxExtensionMessage {
			return nil, &blockSizeError{messageID: id, got: length, allowedMax: maxExtensionMessage}
		}
		buf := make([]byte, length)
		if _, err = io.ReadFull(p.r, buf); err != nil {
			return nil, err
		}
		var em peerprotocol.ExtensionMessage
		if err = em.UnmarshalBinary(buf); err != nil {
			return nil, err
		}
		return em.Payload, nil
	case peerprotocol.HashRequest:
		return p.parseHashMessage(length, false, false)
	case peerprotocol.HashReject:
		return p.parseHashMessage(length, true, false)
	case peerprotocol.Hashes:
		return p.parseHashMessage(length, false, true)
	default:
		p.log.Debugf("unhandled message type: %s", id)
		p.log.Debugln("Discarding", length, "bytes...")
		if _, err = io.CopyN(io.Discard, p.r, int64(length)); err != nil {
			return nil, err
		}
		return nil, nil
	}
}

// parsePiece decodes a piece message: 8-byte header plus the block payload,
// capped at the wire block size and read through the throttle.
func (p *PeerReader) parsePiece(length uint32) (any, error) {
	if length < 8 {
		// Shorter than the index+begin header: reading it would consume bytes
		// of the next message and leave the stream out of step.
		return nil, fmt.Errorf("invalid piece message size: %d", length)
	}
	var head [8]byte
	if err := p.readFull(head[:]); err != nil {
		return nil, err
	}
	pm := peerprotocol.PieceMessage{
		Index: binary.BigEndian.Uint32(head[0:4]),
		Begin: binary.BigEndian.Uint32(head[4:8]),
	}
	length -= 8
	if length > piece.BlockSize {
		return nil, &blockSizeError{messageID: peerprotocol.Piece, got: length, allowedMax: piece.BlockSize}
	}
	buf, err := p.readPiece(length)
	if err != nil {
		return nil, err
	}
	return Piece{PieceMessage: pm, Buffer: buf}, nil
}

// parseHashMessage decodes the v2 hash-transfer messages (BEP 52): a fixed
// header for request/reject, header plus a whole number of hashes for
// hashes. Malformed sizes are protocol errors.
func (p *PeerReader) parseHashMessage(length uint32, reject, withHashes bool) (any, error) {
	if !withHashes {
		if length != peerprotocol.HashRequestHeaderSize {
			kind := "invalid hash request size: %d"
			if reject {
				kind = "invalid hash reject size: %d"
			}
			return nil, fmt.Errorf(kind, length)
		}
		buf := make([]byte, length)
		if _, err := io.ReadFull(p.r, buf); err != nil {
			return nil, err
		}
		if reject {
			var hm peerprotocol.HashRejectMessage
			if err := hm.UnmarshalBinary(buf); err != nil {
				return nil, err
			}
			return hm, nil
		}
		var hm peerprotocol.HashRequestMessage
		if err := hm.UnmarshalBinary(buf); err != nil {
			return nil, err
		}
		return hm, nil
	}
	if length < peerprotocol.HashRequestHeaderSize ||
		(length-peerprotocol.HashRequestHeaderSize)%peerprotocol.HashSize != 0 {
		return nil, fmt.Errorf("invalid hashes size: %d", length)
	}
	if length > maxHashesMessage {
		return nil, &blockSizeError{messageID: peerprotocol.Hashes, got: length, allowedMax: maxHashesMessage}
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(p.r, buf); err != nil {
		return nil, err
	}
	var hm peerprotocol.HashesMessage
	if err := hm.HashRequestHeader.UnmarshalBinary(buf[:peerprotocol.HashRequestHeaderSize]); err != nil {
		return nil, err
	}
	tail := buf[peerprotocol.HashRequestHeaderSize:]
	hm.Hashes = make([][peerprotocol.HashSize]byte, len(tail)/peerprotocol.HashSize)
	for i := range hm.Hashes {
		copy(hm.Hashes[i][:], tail[i*peerprotocol.HashSize:])
	}
	return hm, nil
}

// readPiece streams length payload bytes into a pooled buffer under the read
// throttle. While the block is read, the reader tolerates pieceTimeout of
// silence instead of readTimeout. A slow peer that keeps sending bytes is
// kept. Stops release the buffer instead of leaking it.
func (p *PeerReader) readPiece(length uint32) (buf bufferpool.Buffer, err error) {
	// ReadFull fills the whole block before it is used; on an error it is
	// released unread.
	buf = blockPool.GetUncleared(int(length))
	defer func() {
		if err != nil {
			buf.Release()
		}
	}()

	if d := p.bucket.Take(int64(length)); d > 0 {
		select {
		case <-time.After(d):
		case <-p.stopC:
			err = errStoppedWhileWaitingBucket
			return
		}
		// The throttle wait is not the peer's silence: start a fresh window.
		if err = p.dr.arm(p.pieceTimeout); err != nil {
			return
		}
	} else if time.Until(p.dr.deadline) > p.pieceTimeout {
		// Tighten the idle deadline to the piece window. While blocks stream in
		// the deadline is already that short, so this re-arms once per window.
		if err = p.dr.arm(p.pieceTimeout); err != nil {
			return
		}
	}
	p.dr.window = p.pieceTimeout
	_, err = io.ReadFull(p.r, buf.Data)
	p.dr.window = readTimeout
	if err == nil {
		// A whole block arrived. It may have come from the bufio buffer without
		// a socket read, so record the progress for the idle wait that follows.
		p.dr.progressed = true
	}
	return
}

var errStoppedWhileWaitingBucket = errors.New("peer reader stopped while waiting for bucket")

// blockSizeError rejects an overlong block or request.
type blockSizeError struct {
	messageID  peerprotocol.MessageID
	got        uint32
	allowedMax uint32
}

func (e *blockSizeError) Error() string {
	return fmt.Sprintf("received %s message with block size larger than allowed (%d > %d)", e.messageID, e.got, e.allowedMax)
}
