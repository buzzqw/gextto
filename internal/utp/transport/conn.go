package transport

// GEXTTO: our own uTP (BEP 29) code; see doc.go.
// Conn is one uTP stream. A Socket owns its Conns and dispatches packets to and
// from them. Every Conn and Socket state change is serialised by the package
// mutex mu, so the code below assumes mu is held (methods document the rare
// exceptions).

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"time"
)

// Conn is a uTP stream and implements net.Conn. Create it through a Socket
// (newConn); the zero value is not usable.
type Conn struct {
	// recv_id is the id the peer writes in packets to us; send_id the one we
	// write in packets to it. seq_nr counts our data packets, ack_nr the
	// highest inbound sequence number consumed, lastAck the highest one the
	// peer acknowledged.
	recv_id, send_id uint16
	seq_nr, ack_nr   uint16
	lastAck          uint16
	// lastTimeDiff echoes the peer's clock skew, for RTT accounting.
	lastTimeDiff uint32
	// peerWndSize is the receive window the peer advertises; cur_window is the
	// unacknowledged payload we have sent into it.
	peerWndSize uint32
	cur_window  uint32
	connKey     connKey

	// Data waiting to be read by the application. readBuf holds the received
	// bytes and readPos is how far the application has consumed; the unread
	// window is readBuf[readPos:]. Keeping the offset (rather than reslicing
	// from the front) preserves the backing array's capacity, so a refilled
	// window does not reallocate.
	readBuf         []byte
	readPos         int
	readBufNotEmpty Event

	socket           *Socket
	remoteSocketAddr net.Addr
	// startTimestamp anchors the uTP microsecond clock (see timestamp).
	startTimestamp uint32
	created        time.Time

	synAcked  bool // the peer echoed our SYN (acceptors set it directly)
	gotFin    Event
	wroteFin  Event
	err       error
	closed    Event
	destroyed Event
	canWrite  Event

	// unackedSends holds the packets awaiting acknowledgement, oldest first.
	unackedSends []*send
	// inbound holds out-of-order packets; inbound[0] is ack_nr+1.
	inbound    []recv
	inboundWnd int
	connDeadlines
	// latencies are the recent RTT samples the moving average uses.
	latencies []time.Duration

	// GEXTTO: adaptive RTO backoff (1..maxRTOBackoff). It doubles on each
	// RTO-driven resend and halves on each delivered packet, so a stalled or
	// congested peer is retried with growing patience while a link that
	// recovers is probed at the measured RTT again quickly. See resendTimeout.
	rtoBackoff uint32

	// resendTimer is the single RTO timer of the connection: it fires for the
	// earliest outstanding packet. One timer per packet cost an allocation and a
	// runtime timer heap entry for every segment sent (measured 99 allocs per
	// 16 KiB block in the loopback benchmark).
	resendTimer    *time.Timer
	resendTimerDue time.Time

	// A state packet is owed to the peer, possibly batched by a short timer.
	pendingSendState                bool
	sendPendingSendSendStateTimer   *time.Timer
	sendPendingSendStateTimerActive bool

	// packetReadTimeoutTimer destroys the connection when nothing arrives for
	// packetReadTimeout.
	packetReadTimeoutTimer *time.Timer
}

var _ net.Conn = (*Conn)(nil)

// age is how long ago the connection was created.
func (c *Conn) age() time.Duration { return time.Since(c.created) }

// timestamp is the connection's uTP clock in microseconds, since creation.
func (c *Conn) timestamp() uint32 { return nowTimestamp() - c.startTimestamp }

// sendPendingSendStateTimerCallback flushes a batched state packet.
func (c *Conn) sendPendingSendStateTimerCallback() {
	mu.Lock()
	defer mu.Unlock()
	c.sendPendingSendStateTimerActive = false
	c.sendPendingSendSendStateTimer.Stop()
	c.sendPendingState()
}

// sendPendingState sends the state packet the connection owes: a reset once
// destroyed, otherwise a state (ACK) packet.
func (c *Conn) sendPendingState() {
	if c.destroyed.IsSet() {
		c.sendReset()
	} else {
		c.sendState()
	}
}

// bufferedInbound is how many received bytes wait to be read.
func (c *Conn) bufferedInbound() int { return len(c.readBuf) - c.readPos }

// wndSize is how many more payload bytes fit in our receive buffers. That is
// our advertised window.
func (c *Conn) wndSize() uint32 {
	buffered := c.bufferedInbound()
	if buffered+c.inboundWnd > readBufferLen {
		return 0
	}
	return uint32(readBufferLen - buffered - c.inboundWnd)
}

// makePacket builds a packet of _type with an up-to-date header, including a
// selective ACK for the out-of-order packets currently held. The buffer comes
// from sendBufferPool and must be returned there once sent.
func (c *Conn) makePacket(_type st, connID, seqNr uint16, payload []byte) (p []byte) {
	var selAck selectiveAckBitmask
	for i := 1; i < len(c.inbound); i++ {
		if c.inbound[i].seen {
			selAck.SetBit(i - 1)
		}
	}
	h := header{
		Type:          _type,
		Version:       1,
		ConnID:        connID,
		SeqNr:         seqNr,
		AckNr:         c.ack_nr,
		WndSize:       c.wndSize(),
		Timestamp:     c.timestamp(),
		TimestampDiff: c.lastTimeDiff,
	}
	if len(selAck.Bytes) != 0 {
		// BEP 29 requires the selective-ACK payload to be at least 4 bytes and
		// a multiple of 4.
		if len(selAck.Bytes)%4 != 0 {
			panic(len(selAck.Bytes))
		}
		h.Extensions = append(h.Extensions, extensionField{
			Type:  extensionTypeSelectiveAck,
			Bytes: selAck.Bytes,
		})
	}
	p = sendBufferPool.Get().([]byte)[:0:minMTU]
	n := h.Marshal(p)
	p = p[:n]
	// Extension headers are currently fixed in size.
	if n > maxHeaderSize {
		panic("header has unexpected size")
	}
	p = append(p, payload...)
	return
}

// send writes one packet to the peer. Callers hold mu.
func (c *Conn) send(_type st, connID uint16, payload []byte, seqNr uint16) (err error) {
	p := c.makePacket(_type, connID, seqNr, payload)
	n1, err := c.socket.writeTo(p, c.remoteSocketAddr)
	sendBufferPool.Put(p[:0:minMTU])
	if err != nil {
		return
	}
	if n1 != len(p) {
		panic(n1)
	}
	if c.unpendSendState() && _type != stState {
		// A data packet just covered the state packet we owed.
		unsentStatePackets.Add(1)
	}
	return
}

// unpendSendState cancels a batched state packet and reports whether one was
// pending.
func (c *Conn) unpendSendState() (wasPending bool) {
	wasPending = c.pendingSendState
	c.pendingSendState = false
	c.sendPendingSendSendStateTimer.Stop()
	c.sendPendingSendStateTimerActive = false
	return
}

// pendSendState schedules a state packet, batched by pendingSendStateDelay so a
// burst of inbound packets triggers one ACK, not one each.
func (c *Conn) pendSendState() {
	if c.pendingSendState {
		// A state packet is already owed and was deferred again.
		unsentStatePackets.Add(1)
	}
	c.pendingSendState = true
	if !c.sendPendingSendStateTimerActive {
		c.sendPendingSendSendStateTimer.Reset(pendingSendStateDelay)
		c.sendPendingSendStateTimerActive = true
	}
}

// writeSyn introduces us to the peer (an initiator's first packet).
func (c *Conn) writeSyn() {
	c.write(stSyn, c.recv_id, nil, c.seq_nr)
}

// write sends a data, SYN or FIN packet and records it for acknowledgement.
// Data packets are truncated to maxPayloadSize. A write after FIN panics, which
// is an internal invariant, not peer-reachable input.
func (c *Conn) write(_type st, connID uint16, payload []byte, seqNr uint16) (n int, err error) {
	switch _type {
	case stSyn, stFin, stData:
	default:
		panic(_type)
	}
	if c.wroteFin.IsSet() {
		panic("can't write after fin")
	}
	if len(payload) > maxPayloadSize {
		payload = payload[:maxPayloadSize]
	}
	if err := c.send(_type, connID, payload, seqNr); err != nil {
		c.destroy(fmt.Errorf("error sending packet: %s", err))
		return 0, err
	}
	n = len(payload)
	// Copy the payload: the caller keeps its buffer, and the copy outlives
	// this call for a possible resend.
	if payload != nil {
		payload = append(sendBufferPool.Get().([]byte)[:0:minMTU], payload...)
	}
	s := &send{
		payloadSize: uint32(len(payload)),
		started:     time.Now(),
		_type:       _type,
		connID:      connID,
		payload:     payload,
		seqNr:       seqNr,
		conn:        c,
	}
	c.unackedSends = append(c.unackedSends, s)
	c.scheduleResendLocked()
	c.cur_window += s.payloadSize
	c.updateCanWrite()
	c.seq_nr++
	return
}

// latency estimates the path RTT: the mean of the recent samples, floored at
// minLatency so a fast path cannot collapse the RTO; before any sample it uses
// the per-host seed (see Socket.initialLatencyFor) or the fixed initialLatency.
//
// GEXTTO: the floor resolves the reference "minimum latency" TODO; the seed
// comes from the dial race.
func (c *Conn) latency() (ret time.Duration) {
	if len(c.latencies) != 0 {
		for _, l := range c.latencies {
			ret += l
		}
		ret = (ret + time.Duration(len(c.latencies)) - 1) / time.Duration(len(c.latencies))
		if ret < minLatencyDuration() {
			ret = minLatencyDuration()
		}
		return ret
	}
	if est, ok := c.socket.initialLatencyFor(c.remoteSocketAddr.String()); ok {
		return est
	}
	return initialLatency
}

// sendState sends an (empty) state packet carrying our ACKs and window.
func (c *Conn) sendState() {
	c.send(stState, c.send_id, nil, c.seq_nr)
	sentStatePackets.Add(1)
}

// sendReset sends an empty reset packet.
func (c *Conn) sendReset() {
	c.send(stReset, c.send_id, nil, c.seq_nr)
}

// addLatency records one RTT sample, keeping the last ten.
func (c *Conn) addLatency(l time.Duration) {
	c.latencies = append(c.latencies, l)
	if len(c.latencies) > 10 {
		c.latencies = c.latencies[len(c.latencies)-10:]
	}
}

// ack acknowledges our send with the given sequence number and trims the
// acknowledged prefix of unackedSends.
func (c *Conn) ack(nr uint16) {
	if !seqLess(c.lastAck, nr) {
		return // already acknowledged
	}
	i := nr - c.lastAck - 1
	if int(i) >= len(c.unackedSends) {
		// The peer acknowledged packets we have not even sent.
		acksReceivedAheadOfSyn.Add(1)
		return
	}
	s := c.unackedSends[i]
	latency, first := s.Ack()
	if first {
		c.cur_window -= s.payloadSize
		c.updateCanWrite()
		c.addLatency(latency)
		c.noteDelivery()
	}
	for len(c.unackedSends) != 0 {
		if !c.unackedSends[0].acked.IsSet() {
			return // the front packet is still outstanding
		}
		c.unackedSends[0] = nil // release the acked send from the backing array
		c.unackedSends = c.unackedSends[1:]
		c.updateCanWrite()
		c.lastAck++
	}
	// Nothing is outstanding: the timer can stop.
	c.scheduleResendLocked()
}

// ackTo acknowledges every packet up to nr.
func (c *Conn) ackTo(nr uint16) {
	if !seqLess(nr, c.seq_nr) {
		return
	}
	for seqLess(c.lastAck, nr) {
		c.ack(c.lastAck + 1)
	}
}

// seqSend returns the send for a sequence number, or nil when there is none.
func (c *Conn) seqSend(seqNr uint16) *send {
	if !seqLess(c.lastAck, seqNr) {
		return nil // already acknowledged
	}
	i := int(seqNr - c.lastAck - 1)
	if i >= len(c.unackedSends) {
		return nil
	}
	return c.unackedSends[i]
}

// resendTimeout is the retransmission timeout: an RTT-derived base with jitter,
// scaled by the adaptive loss-driven backoff.
func (c *Conn) resendTimeout() time.Duration {
	l := c.latency()
	ret := jitterDuration(3*l, l)
	// GEXTTO: loss-driven backoff. Each RTO timeout is the congestion signal
	// the socket counters expose; it makes this connection more patient, up to
	// maxRTOBackoff, which cuts retransmission volume to a stalled or
	// congested peer. A delivered packet halves it (noteDelivery). A healthy
	// connection never timeout-resends, so it stays at 1: today's behaviour.
	if c.rtoBackoff > 1 {
		ret *= time.Duration(c.rtoBackoff)
	}
	return ret
}

// noteRTOTimeout is the feedback for the adaptive RTO: it doubles the backoff,
// capped at maxRTOBackoff. Called from send.timeoutResend under mu.
func (c *Conn) noteRTOTimeout() {
	if !adaptiveRTO.Load() {
		return
	}
	switch {
	case c.rtoBackoff < 1:
		c.rtoBackoff = 1
	case c.rtoBackoff < maxRTOBackoff:
		c.rtoBackoff *= 2
		if c.rtoBackoff > maxRTOBackoff {
			c.rtoBackoff = maxRTOBackoff
		}
	}
}

// noteDelivery relaxes the adaptive RTO backoff by half (floor 1) when a packet
// is acknowledged, so a link that recovers stops paying for a past stall.
// Called from Conn.ack under mu.
func (c *Conn) noteDelivery() {
	if !adaptiveRTO.Load() {
		return
	}
	if c.rtoBackoff > 1 {
		c.rtoBackoff /= 2
	}
}

// ackSkipped is called for a packet the peer did not acknowledge: a run of
// skipped packets triggers a fast resend. Called from applyAcks under mu.
func (c *Conn) ackSkipped(seqNr uint16) {
	s := c.seqSend(seqNr)
	if s == nil {
		return
	}
	s.acksSkipped++
	if s.acked.IsSet() {
		return
	}
	switch s.acksSkipped {
	case 3, 60:
		ackSkippedResends.Add(1)
		s.resend()
		// GEXTTO: congestion signal for the socket counters.
		c.socket.addFastResend()
		c.scheduleResendLocked()
	}
}

// scheduleResendLocked arms the connection's single RTO timer for the oldest
// outstanding packet, or stops it when nothing is waiting. The caller holds mu.
//
// The front of unackedSends is the oldest packet still unacknowledged, so its
// deadline is the earliest one in the normal case; the timer callback scans the
// whole list, so a later packet that became due meanwhile is not missed. Reading
// the front keeps the cost of a write to one timeout computation, as before.
func (c *Conn) scheduleResendLocked() {
	if c.destroyed.IsSet() || len(c.unackedSends) == 0 {
		if c.resendTimer != nil {
			c.resendTimer.Stop()
			c.resendTimer = nil
			c.resendTimerDue = time.Time{}
		}
		return
	}
	front := c.unackedSends[0]
	if front == nil || front.acked.IsSet() {
		return
	}
	due := front.dueAt()
	if c.resendTimer != nil && !c.resendTimerDue.IsZero() && !c.resendTimerDue.After(due) {
		return // already armed, and not later than this packet
	}
	delay := time.Until(due)
	if delay < 0 {
		delay = 0
	}
	if c.resendTimer != nil {
		c.resendTimer.Stop()
	}
	c.resendTimerDue = due
	c.resendTimer = time.AfterFunc(delay, c.resendDue)
}

// resendDue is the RTO timer callback: it retransmits every packet whose time
// has come, destroys the connection when a packet waited past writeTimeout, and
// re-arms the timer for the next one.
func (c *Conn) resendDue() {
	mu.Lock()
	defer mu.Unlock()
	c.resendTimer = nil
	c.resendTimerDue = time.Time{}
	if c.destroyed.IsSet() {
		return
	}
	now := time.Now()
	rto := c.resendTimeout()
	for _, s := range c.unackedSends {
		if s == nil || s.acked.IsSet() {
			continue
		}
		if now.Sub(s.started) >= writeTimeout {
			s.timedOut()
			return
		}
		multiplier := s.numResends
		if multiplier < 1 {
			multiplier = 1
		}
		if now.Before(s.started.Add(rto * time.Duration(multiplier))) {
			continue
		}
		s.resend()
		s.noteRTOResend()
	}
	c.scheduleResendLocked()
}

// receivePacket hands one inbound packet to the connection state machine.
func (c *Conn) receivePacket(h header, payload []byte) {
	c.packetReadTimeoutTimer.Reset(packetReadTimeout)
	c.processDelivery(h, payload)
}

// receivePacketTimeoutCallback destroys a connection that heard nothing for
// packetReadTimeout.
func (c *Conn) receivePacketTimeoutCallback() {
	mu.Lock()
	c.destroy(errors.New("no packet read timeout"))
	mu.Unlock()
}

// lazyDestroy tears the connection down once it has said goodbye to the peer:
// FIN written and acknowledged, FIN received (or the app closed).
func (c *Conn) lazyDestroy() {
	if c.wroteFin.IsSet() && len(c.unackedSends) <= 1 && (c.gotFin.IsSet() || c.closed.IsSet()) {
		c.destroy(errors.New("lazily destroyed"))
	}
}

// processDelivery applies one inbound packet: its acknowledgements, then its
// payload (buffering out-of-order data and surfacing in-order data).
func (c *Conn) processDelivery(h header, payload []byte) {
	deliveriesProcessed.Add(1)
	defer c.lazyDestroy()
	c.assertHeader(h)
	c.peerWndSize = h.WndSize
	c.applyAcks(h)
	if h.Timestamp == 0 {
		c.lastTimeDiff = 0
	} else {
		c.lastTimeDiff = c.timestamp() - h.Timestamp
	}

	if h.Type == stReset {
		c.destroy(errors.New("peer reset"))
		return
	}
	if !c.synAcked {
		// The first packet must complete the handshake (a state packet).
		if h.Type != stState {
			return
		}
		c.synAcked = true
		c.updateCanWrite()
		c.ack_nr = h.SeqNr - 1
		return
	}
	if h.Type == stState {
		return
	}
	// Even if we do not want this packet, tell the peer our state in case it
	// missed something.
	c.pendSendState()
	if !seqLess(c.ack_nr, h.SeqNr) {
		return // already received
	}
	inboundIndex := int(h.SeqNr - c.ack_nr - 1)
	if inboundIndex < len(c.inbound) && c.inbound[inboundIndex].seen {
		return // already received
	}
	// 64 packets corresponds to 8 bytes of selective ACK, the useful limit
	// derived from production logs.
	if inboundIndex >= maxUnackedInbound {
		// Too far ahead: discard.
		if logLevel >= 1 {
			log.Printf("received packet from %s %d ahead of next seqnr (%x > %x)",
				c.remoteSocketAddr, inboundIndex, h.SeqNr, c.ack_nr+1)
		}
		return
	}
	// Extend inbound so the new packet has a slot.
	for inboundIndex >= len(c.inbound) {
		c.inbound = append(c.inbound, recv{})
	}
	c.inbound[inboundIndex] = recv{true, payload, h.Type}
	c.inboundWnd += len(payload)
	before := len(c.inbound)
	c.processInbound()
	// payload points into the socket's receive buffer, reused for the next
	// datagram. In-order data was just copied into readBuf; a packet still
	// queued (out of order, or the read buffer full) must own its bytes.
	if i := inboundIndex - (before - len(c.inbound)); i >= 0 {
		c.inbound[i].data = append([]byte(nil), payload...)
	}
}

// applyAcks acknowledges our sends from the packet's AckNr and selective-ACK
// extension, marking the gaps so a fast resend can fire.
func (c *Conn) applyAcks(h header) {
	c.ackTo(h.AckNr)
	for _, ext := range h.Extensions {
		if ext.Type != extensionTypeSelectiveAck {
			continue
		}
		c.ackSkipped(h.AckNr + 1)
		bitmask := selectiveAckBitmask{Bytes: ext.Bytes}
		for i := 0; i < bitmask.NumBits(); i++ {
			nr := h.AckNr + 2 + uint16(i)
			if bitmask.BitIsSet(i) {
				c.ack(nr)
			} else {
				c.ackSkipped(nr)
			}
		}
	}
}

// assertHeader guards the connection-id invariant: only a SYN carries the
// peer's send id, every other packet ours. A mismatch is an internal bug, so it
// panics rather than dropping the packet silently.
func (c *Conn) assertHeader(h header) {
	if h.Type == stSyn {
		if h.ConnID != c.send_id {
			panic(fmt.Sprintf("%d != %d", h.ConnID, c.send_id))
		}
		return
	}
	if h.ConnID != c.recv_id {
		panic("erroneous delivery")
	}
}

func (c *Conn) updateReadBufNotEmpty() {
	c.readBufNotEmpty.SetBool(c.bufferedInbound() != 0)
}

// appendReadBuf appends received bytes to the read window. When the backing
// array is full and some of it is already consumed, it first moves the unread
// tail to the front, so the append reuses the capacity instead of reallocating
// the whole window.
func (c *Conn) appendReadBuf(p []byte) {
	if c.readPos > 0 && len(c.readBuf) == cap(c.readBuf) {
		n := copy(c.readBuf, c.readBuf[c.readPos:])
		c.readBuf = c.readBuf[:n]
		c.readPos = 0
	}
	c.readBuf = append(c.readBuf, p...)
}

// processInbound moves consecutive buffered packets into the read buffer.
func (c *Conn) processInbound() {
	for !c.gotFin.IsSet() && len(c.inbound) > 0 && c.inbound[0].seen && c.bufferedInbound() < readBufferLen {
		c.ack_nr++
		p := c.inbound[0]
		// Clear the slot before reslicing: the backing array outlives the
		// slice window, and a stale entry would keep its payload alive.
		c.inbound[0] = recv{}
		c.inbound = c.inbound[1:]
		c.inboundWnd -= len(p.data)
		c.appendReadBuf(p.data)
		c.updateReadBufNotEmpty()
		if p.Type == stFin {
			c.gotFin.Set()
		}
	}
}

// waitAck blocks until the send with the given sequence number is acknowledged
// or the connection is destroyed. Callers hold mu.
func (c *Conn) waitAck(seq uint16) {
	s := c.seqSend(seq)
	if s == nil {
		return
	}
	waitEvents(&mu, &s.acked, &c.destroyed)
}

// recvSynAck waits for our SYN to be acknowledged and returns the connection
// error, if any. Callers hold no lock.
func (c *Conn) recvSynAck() (err error) {
	mu.Lock()
	defer mu.Unlock()
	c.waitAck(1)
	if c.err != nil {
		err = c.err
	}
	c.synAcked = true
	c.updateCanWrite()
	return err
}

// writeFin sends our FIN once.
func (c *Conn) writeFin() {
	if c.wroteFin.IsSet() {
		return
	}
	c.write(stFin, c.send_id, nil, c.seq_nr)
	c.wroteFin.Set()
}

// destroy marks the connection torn down with reason, detaching it from the
// socket. Idempotent: the first reason wins.
func (c *Conn) destroy(reason error) {
	c.destroyed.Set()
	if c.err == nil {
		c.err = reason
	}
	c.detach()
}

// closeNow closes the connection immediately, without waiting for the peer.
func (c *Conn) closeNow() (err error) {
	c.closed.Set()
	c.writeFin()
	c.destroy(errors.New("destroyed"))
	return nil
}

// Close closes the stream gracefully: FIN is sent and the connection tears down
// once the peer has also finished.
func (c *Conn) Close() (err error) {
	mu.Lock()
	defer mu.Unlock()
	c.closed.Set()
	c.writeFin()
	c.lazyDestroy()
	return nil
}

func (c *Conn) LocalAddr() net.Addr { return addr{c.socket.Addr()} }

// Read copies buffered inbound data, blocking until some is available, the peer
// closes, an error occurs or the read deadline passes.
func (c *Conn) Read(b []byte) (n int, err error) {
	mu.Lock()
	defer mu.Unlock()
	for {
		n = copy(b, c.readBuf[c.readPos:])
		c.readPos += n
		if c.readPos == len(c.readBuf) {
			// The window is fully consumed: keep the backing array for the
			// next fill, unless a burst grew it past readBufKeep. Keeping
			// those across idle connections held hundreds of MB under load.
			if cap(c.readBuf) > readBufKeep {
				c.readBuf = nil
			} else {
				c.readBuf = c.readBuf[:0]
			}
			c.readPos = 0
		}
		c.updateReadBufNotEmpty()
		if n != 0 {
			// Refill the read buffer from any packets that were waiting.
			c.processInbound()
			return n, nil
		}
		switch {
		case c.gotFin.IsSet() || c.closed.IsSet():
			return 0, io.EOF
		case c.destroyed.IsSet():
			if c.err == nil {
				panic("closed without receiving fin, and no error")
			}
			return 0, c.err
		case c.connDeadlines.read.passed.IsSet():
			return 0, errTimeout
		}
		waitEvents(&mu,
			&c.gotFin,
			&c.closed,
			&c.destroyed,
			&c.connDeadlines.read.passed,
			&c.readBufNotEmpty)
	}
}

func (c *Conn) RemoteAddr() net.Addr { return addr{c.remoteSocketAddr} }

func (c *Conn) String() string {
	return fmt.Sprintf("<UTPConn %s-%s (%d)>", c.LocalAddr(), c.RemoteAddr(), c.recv_id)
}

// updateCanWrite recomputes whether a data packet may be sent now.
func (c *Conn) updateCanWrite() {
	c.canWrite.SetBool(c.synAcked &&
		len(c.unackedSends) < maxUnackedSends &&
		c.cur_window <= c.peerWndSize)
}

// Write sends payload to the peer, blocking while the window is full, the
// stream is closed, an error occurs or the write deadline passes.
func (c *Conn) Write(p []byte) (n int, err error) {
	mu.Lock()
	defer mu.Unlock()
	for len(p) != 0 {
		switch {
		case c.wroteFin.IsSet() || c.closed.IsSet():
			return n, errClosed
		case c.destroyed.IsSet():
			return n, c.err
		case c.connDeadlines.write.passed.IsSet():
			return n, errTimeout
		}
		// With a zero peer window we still send one packet, so a stalled
		// window does not deadlock the connection.
		if c.canWrite.IsSet() {
			n1, werr := c.write(stData, c.send_id, p, c.seq_nr)
			n += n1
			if werr != nil {
				return n, werr
			}
			if n1 == 0 {
				panic(len(p))
			}
			p = p[n1:]
			continue
		}
		waitEvents(&mu,
			&c.wroteFin,
			&c.closed,
			&c.destroyed,
			&c.connDeadlines.write.passed,
			&c.canWrite)
	}
	return n, nil
}

// detach removes the connection from its socket's map and lets the socket tear
// down if it was waiting on it.
func (c *Conn) detach() {
	s := c.socket
	if _, ok := s.conns[c.connKey]; !ok {
		return
	}
	delete(s.conns, c.connKey)
	s.lazyDestroy()
}
