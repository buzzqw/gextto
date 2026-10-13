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
}

// New wraps conn; maxMsgSize bounds any single message before its buffer is
// allocated, closing the hostile-allocation hole on variable-length messages.
func New(conn net.Conn, l logger.Logger, pieceTimeout time.Duration, maxMsgSize int, b *bandwidth.Limiter) *PeerReader {
	return &PeerReader{
		conn:         conn,
		r:            bufio.NewReaderSize(conn, readBufferSize),
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

	for {
		if err = p.conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			return
		}
		var length uint32
		if err = binary.Read(p.r, binary.BigEndian, &length); err != nil {
			return
		}
		if length == 0 {
			p.log.Debug("Received message of type \"keep alive\"")
			continue
		}
		var id peerprotocol.MessageID
		if err = binary.Read(p.r, binary.BigEndian, &id); err != nil {
			return
		}
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
		var hm peerprotocol.HaveMessage
		if err = binary.Read(p.r, binary.BigEndian, &hm); err != nil {
			return nil, err
		}
		return hm, nil
	case peerprotocol.Bitfield:
		var bm peerprotocol.BitfieldMessage
		bm.Data = make([]byte, length)
		if _, err = io.ReadFull(p.r, bm.Data); err != nil {
			return nil, err
		}
		return bm, nil
	case peerprotocol.Request:
		var rm peerprotocol.RequestMessage
		if err = binary.Read(p.r, binary.BigEndian, &rm); err != nil {
			return nil, err
		}
		if rm.Length > MaxBlockSize {
			return nil, &blockSizeError{messageID: id, got: rm.Length, allowedMax: MaxBlockSize}
		}
		return rm, nil
	case peerprotocol.Reject:
		var rm peerprotocol.RejectMessage
		if err = binary.Read(p.r, binary.BigEndian, &rm); err != nil {
			return nil, err
		}
		p.log.Debugf("Received Reject: %+v", rm)
		return rm, nil
	case peerprotocol.Cancel:
		var cm peerprotocol.CancelMessage
		if err = binary.Read(p.r, binary.BigEndian, &cm); err != nil {
			return nil, err
		}
		return cm, nil
	case peerprotocol.Piece:
		return p.parsePiece(length)
	case peerprotocol.HaveAll:
		return peerprotocol.HaveAllMessage{}, nil
	case peerprotocol.HaveNone:
		return peerprotocol.HaveNoneMessage{}, nil
	case peerprotocol.AllowedFast:
		var am peerprotocol.AllowedFastMessage
		if err = binary.Read(p.r, binary.BigEndian, &am); err != nil {
			return nil, err
		}
		return am, nil
	case peerprotocol.Port:
		var pm peerprotocol.PortMessage
		if err = binary.Read(p.r, binary.BigEndian, &pm); err != nil {
			return nil, err
		}
		return pm, nil
	case peerprotocol.Extension:
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
	var pm peerprotocol.PieceMessage
	if err := binary.Read(p.r, binary.BigEndian, &pm); err != nil {
		return nil, err
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
// throttle and the piece deadline. A timed-out partial block keeps the bytes
// and continues; a timed-out empty read disconnects. Stops release the
// buffer instead of leaking it.
func (p *PeerReader) readPiece(length uint32) (buf bufferpool.Buffer, err error) {
	buf = blockPool.Get(int(length))
	defer func() {
		if err != nil {
			buf.Release()
		}
	}()

	var n int
	for {
		if d := p.bucket.Take(int64(length)); d > 0 {
			select {
			case <-time.After(d):
			case <-p.stopC:
				err = errStoppedWhileWaitingBucket
				return
			}
		}
		if err = p.conn.SetReadDeadline(time.Now().Add(p.pieceTimeout)); err != nil {
			return
		}
		var m int
		m, err = io.ReadFull(p.r, buf.Data[n:])
		n += m
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				if n > 0 {
					continue // slow peer: keep the bytes, read the rest
				}
				return // silent peer: disconnect
			}
			return
		}
		return
	}
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
