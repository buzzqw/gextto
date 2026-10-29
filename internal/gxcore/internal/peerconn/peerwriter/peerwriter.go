package peerwriter

import (
	"bytes"
	"container/list"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bandwidth"
	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerconn/peerreader"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
)

// keepAlivePeriod spaces the keepalive probes; the ticker runs at half that.
const keepAlivePeriod = 2 * time.Minute

// maxBatch caps the small messages handed to the writer in one go (the
// in-flight request cap is 250 by default, so one refill fits).
const maxBatch = 256

// servedWindowSize caps the remembered served blocks: an unbounded set grew
// forever and rejected legitimate re-requests (e.g. after a corrupt
// transfer) for the rest of the connection.
const servedWindowSize = 1024

// servedWindow remembers the most recent served requests for duplicate
// suppression, evicting oldest-first past capacity.
type servedWindow struct {
	m   map[peerprotocol.RequestMessage]struct{}
	q   []peerprotocol.RequestMessage
	max int
}

func newServedWindow(n int) *servedWindow {
	return &servedWindow{
		m:   make(map[peerprotocol.RequestMessage]struct{}),
		q:   make([]peerprotocol.RequestMessage, 0, n),
		max: n,
	}
}

// seen records r, reporting whether it was already inside the window.
func (w *servedWindow) seen(r peerprotocol.RequestMessage) bool {
	if _, ok := w.m[r]; ok {
		return true
	}
	w.m[r] = struct{}{}
	w.q = append(w.q, r)
	if len(w.q) > w.max {
		delete(w.m, w.q[0])
		w.q = w.q[1:]
	}
	return false
}

// PeerWriter serializes queued protocol messages onto the connection: a
// queue loop stages them, a writer loop frames and sends them.
//
// The writer asks for work on readyC and the queue loop answers on batchC
// with either one block upload or the run of small messages at the head of
// the queue (requests, haves, …), which the writer frames into one buffer and
// sends with one Write: a refill of 250 requests is one syscall and a few
// segments instead of 250 of each. Uploads still travel alone, so
// cancellation, the choke purge and the throttle see them as before.
type PeerWriter struct {
	conn                  net.Conn
	queueC                chan peerprotocol.Message
	cancelC               chan peerprotocol.CancelMessage
	writeQueue            *list.List
	maxQueuedRequests     int
	fastEnabled           bool
	currentQueuedRequests int
	readyC                chan struct{}
	batchC                chan []peerprotocol.Message
	// batches are the two hand-off slices, used in turn: the writer asks for
	// the next batch only after it has sent the previous one, so the slice
	// filled now is never the one being written.
	batches        [2][]peerprotocol.Message
	nextBatch      int
	frameBuf       bytes.Buffer
	messages       chan any
	servedRequests *servedWindow
	bucket         *bandwidth.Limiter // rate changes at runtime
	log            logger.Logger
	stopC          chan struct{}
	doneC          chan struct{}
}

// New stages a writer over conn.
func New(conn net.Conn, l logger.Logger, maxQueuedRequests int, fastEnabled bool, b *bandwidth.Limiter) *PeerWriter {
	return &PeerWriter{
		conn:              conn,
		queueC:            make(chan peerprotocol.Message),
		cancelC:           make(chan peerprotocol.CancelMessage),
		writeQueue:        list.New(),
		maxQueuedRequests: maxQueuedRequests,
		fastEnabled:       fastEnabled,
		readyC:            make(chan struct{}),
		batchC:            make(chan []peerprotocol.Message, 1),
		messages:          make(chan any),
		servedRequests:    newServedWindow(servedWindowSize),
		bucket:            b,
		log:               l,
		stopC:             make(chan struct{}),
		doneC:             make(chan struct{}),
	}
}

// Messages reports upload completions. See BlockUploaded.
func (p *PeerWriter) Messages() <-chan any {
	return p.messages
}

// SendMessage queues a protocol message, dropping it when stopped.
func (p *PeerWriter) SendMessage(msg peerprotocol.Message) {
	select {
	case p.queueC <- msg:
	case <-p.doneC:
	}
}

// SendPiece queues one block upload; the bytes are read from storage at
// send time, not now.
func (p *PeerWriter) SendPiece(msg peerprotocol.RequestMessage, pi io.ReaderAt) {
	m := Piece{Data: pi, RequestMessage: msg}
	select {
	case p.queueC <- m:
	case <-p.doneC:
	}
}

// CancelRequest withdraws a queued piece request.
func (p *PeerWriter) CancelRequest(msg peerprotocol.CancelMessage) {
	select {
	case p.cancelC <- msg:
	case <-p.doneC:
	}
}

// Stop ends the loops; Done closes when they exited.
func (p *PeerWriter) Stop() {
	close(p.stopC)
}

// Done closes when the run loop exits.
func (p *PeerWriter) Done() chan struct{} {
	return p.doneC
}

// Run stages queued messages and hands the head to the writer when ready.
// A choke purges queued uploads; an overfull piece queue rejects (or drops)
// the newest.
func (p *PeerWriter) Run() {
	defer close(p.doneC)

	go p.messageWriter()

	for {
		var readyC chan struct{}
		if p.writeQueue.Len() > 0 {
			readyC = p.readyC
		}
		select {
		case msg := <-p.queueC:
			p.queueMessage(msg)
		case <-readyC:
			p.batchC <- p.takeBatch()
		case cm := <-p.cancelC:
			p.cancelRequest(cm)
		case <-p.stopC:
			return
		}
	}
}

// takeBatch removes the head of the queue for the writer: a block upload on
// its own, or the run of small messages up to the next upload (at most
// maxBatch). Small messages are never cancelled or purged, so handing them
// over together changes nothing but the number of writes.
func (p *PeerWriter) takeBatch() []peerprotocol.Message {
	batch := p.batches[p.nextBatch][:0]
	for e := p.writeQueue.Front(); e != nil && len(batch) < maxBatch; e = p.writeQueue.Front() {
		msg := e.Value.(peerprotocol.Message)
		if _, ok := msg.(Piece); ok {
			if len(batch) > 0 {
				break
			}
			p.writeQueue.Remove(e)
			p.currentQueuedRequests--
			batch = append(batch, msg)
			break
		}
		p.writeQueue.Remove(e)
		batch = append(batch, msg)
	}
	p.batches[p.nextBatch] = batch
	p.nextBatch ^= 1
	return batch
}

// queueMessage stages msg, enforcing the piece-queue bound: silent drop by
// default, reject message when the fast extension is on.
func (p *PeerWriter) queueMessage(msg peerprotocol.Message) {
	if req, ok := msg.(Piece); ok {
		if p.currentQueuedRequests >= p.maxQueuedRequests {
			if p.fastEnabled {
				p.writeQueue.PushBack(peerprotocol.RejectMessage{RequestMessage: req.RequestMessage})
			}
			return
		}
		p.currentQueuedRequests++
	} else if _, ok := msg.(peerprotocol.ChokeMessage); ok {
		p.cancelQueuedPieceMessages()
	}
	p.writeQueue.PushBack(msg)
}

// cancelQueuedPieceMessages purges every staged upload (on choke).
func (p *PeerWriter) cancelQueuedPieceMessages() {
	var next *list.Element
	for e := p.writeQueue.Front(); e != nil; e = next {
		next = e.Next()
		if _, ok := e.Value.(Piece); ok {
			p.writeQueue.Remove(e)
			p.currentQueuedRequests--
		}
	}
}

// cancelRequest withdraws the first staged upload matching cm.
func (p *PeerWriter) cancelRequest(cm peerprotocol.CancelMessage) {
	for e := p.writeQueue.Front(); e != nil; e = e.Next() {
		if pi, ok := e.Value.(Piece); ok && pi.Index == cm.Index && pi.Begin == cm.Begin && pi.Length == cm.Length {
			p.writeQueue.Remove(e)
			p.currentQueuedRequests--
			break
		}
	}
}

// messageWriter frames staged messages (length + id + body) into a reused
// buffer and sends them, throttling uploads through the bucket, counting
// payload bytes, and probing keepalives. Duplicate served requests are
// rejected instead of re-served.
func (p *PeerWriter) messageWriter() {
	defer p.conn.Close()

	// The handshaker's write deadline must not linger into the session.
	if err := p.conn.SetWriteDeadline(time.Time{}); err != nil {
		if _, ok := err.(*net.OpError); ok {
			p.log.Debugln("cannot set deadline:", err)
		} else {
			p.log.Error(err)
		}
		return
	}

	keepAliveTicker := time.NewTicker(keepAlivePeriod / 2)
	defer keepAliveTicker.Stop()

	// Reused frame buffer, sized for a max-block piece message:
	// 4 length + 1 id + 8 piece header + block.
	var frame [4 + 1 + 8 + peerreader.MaxBlockSize]byte
	p.frameBuf = *bytes.NewBuffer(frame[:0])

	for {
		select {
		case p.readyC <- struct{}{}:
			batch := <-p.batchC
			if pi, ok := batch[0].(Piece); ok {
				var msg peerprotocol.Message = pi
				if p.servedRequests.seen(pi.RequestMessage) {
					msg = peerprotocol.RejectMessage{RequestMessage: pi.RequestMessage}
				}
				if !p.sendFramed(msg) {
					return
				}
				continue
			}
			if !p.sendBatch(batch) {
				return
			}
		case <-keepAliveTicker.C:
			if !p.sendKeepAlive() {
				return
			}
		case <-p.stopC:
			return
		}
	}
}

// appendFrame serializes msg (length + id + body) at the end of buf. False
// means the writer loop must exit.
func (p *PeerWriter) appendFrame(buf *bytes.Buffer, msg peerprotocol.Message) bool {
	start := buf.Len()
	buf.Write([]byte{0, 0, 0, 0, 0}) // length + id placeholder

	var m int64
	var err error
	if wt, ok := msg.(io.WriterTo); ok {
		m, err = wt.WriteTo(buf)
	} else {
		m, err = buf.ReadFrom(msg)
	}
	if err != nil {
		select {
		case <-p.stopC:
			return false
		default:
		}
		p.log.Errorf("cannot serialize message [%v]: %s", msg.ID(), err.Error())
		return false
	}
	frame := buf.Bytes()[start:]
	binary.BigEndian.PutUint32(frame[:4], uint32(1+m))
	frame[4] = uint8(msg.ID())
	return true
}

// resetFrameBuf empties the reused frame buffer. It starts on the writer's
// scratch array, so a frame that fits never allocates; an oversized one (a
// bitfield of a huge torrent) grows it once and the larger buffer is kept.
func (p *PeerWriter) resetFrameBuf() *bytes.Buffer {
	p.frameBuf.Reset()
	return &p.frameBuf
}

// sendBatch frames a run of small messages back to back and writes them
// together, flushing early only when the next frame would not fit scratch.
// False means the writer loop must exit.
func (p *PeerWriter) sendBatch(batch []peerprotocol.Message) bool {
	buf := p.resetFrameBuf()
	for _, msg := range batch {
		if buf.Len() > peerreader.MaxBlockSize/2 {
			if !p.write(buf.Bytes(), msg) {
				return false
			}
			buf = p.resetFrameBuf()
		}
		if !p.appendFrame(buf, msg) {
			return false
		}
	}
	return p.write(buf.Bytes(), batch[len(batch)-1])
}

// write sends b, logging a failure under msg's id. False means the writer
// loop must exit.
func (p *PeerWriter) write(b []byte, msg peerprotocol.Message) bool {
	_, err := p.conn.Write(b)
	return p.writeOK(err, msg)
}

// writeOK classifies a write error: a network error ends the connection
// quietly (the peer went away), anything else is reported. False means the
// writer loop must exit.
func (p *PeerWriter) writeOK(err error, msg peerprotocol.Message) bool {
	if err == nil {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		p.log.Debugf("cannot write message [%v]: %s", msg.ID(), err.Error())
	} else {
		p.log.Errorf("cannot write message [%v]: %s", msg.ID(), err.Error())
	}
	return false
}

// sendFramed serializes one message into scratch and writes it, honoring
// the upload throttle for pieces and reporting payload bytes. False means
// the writer loop must exit.
func (p *PeerWriter) sendFramed(msg peerprotocol.Message) bool {
	buf := p.resetFrameBuf()
	if !p.appendFrame(buf, msg) {
		return false
	}

	if _, ok := msg.(Piece); ok {
		if d := p.bucket.Take(int64(buf.Len())); d > 0 {
			select {
			case <-time.After(d):
			case <-p.stopC:
				return false
			}
		}
	}
	n, err := p.conn.Write(buf.Bytes())
	if _, ok := msg.(Piece); ok {
		p.countUploadBytes(n)
	}
	return p.writeOK(err, msg)
}

// sendKeepAlive probes the connection. False means the loop must exit.
func (p *PeerWriter) sendKeepAlive() bool {
	_, err := p.conn.Write([]byte{0, 0, 0, 0})
	if err == nil {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		p.log.Debugf("cannot write keepalive message: %s", err.Error())
	} else {
		p.log.Errorf("cannot write keepalive message: %s", err.Error())
	}
	return false
}

// countUploadBytes reports the payload bytes of a written piece (frame and
// header excluded, never negative).
func (p *PeerWriter) countUploadBytes(n int) {
	n -= 13 // message + piece header
	if n < 0 {
		n = 0
	}
	if uploaded := uint32(n); uploaded > 0 {
		select {
		case p.messages <- BlockUploaded{Length: uploaded}:
		case <-p.stopC:
		}
	}
}
