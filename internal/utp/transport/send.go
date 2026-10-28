package transport

// GEXTTO: our own uTP (BEP 29) code; see doc.go. send is one outstanding data packet: its payload,
// resend timer and RTO backoff state.

import (
	"log"
	"time"
)

// send is an unacknowledged packet.
type send struct {
	acked       Event
	payloadSize uint32
	started     time.Time
	_type       st
	connID      uint16
	payload     []byte
	seqNr       uint16
	conn        *Conn

	acksSkipped int
	numResends  int
}

// dueAt is when this packet must be sent again: the RTO of the connection
// scaled by its own backoff. The connection keeps one timer for all of its
// outstanding packets, so the timer itself is not stored here.
func (s *send) dueAt() time.Time {
	multiplier := s.numResends
	if multiplier < 1 {
		multiplier = 1
	}
	return s.started.Add(s.conn.resendTimeout() * time.Duration(multiplier))
}

// Ack marks the packet acknowledged. first is true only the first time, when it
// also returns the measured RTT; a resend does not refresh the sample.
func (s *send) Ack() (latency time.Duration, first bool) {
	first = !s.acked.IsSet()
	if first {
		latency = time.Since(s.started)
	}
	if s.payload != nil {
		sendBufferPool.Put(s.payload[:0:minMTU])
		s.payload = nil
	}
	s.acked.Set()
	return
}

// timedOut destroys the connection: the peer never acknowledged within
// writeTimeout.
func (s *send) timedOut() {
	s.conn.destroy(errAckTimeout)
}

// noteRTOResend feeds the loss signal of one retransmission to the socket
// counters and the connection's adaptive backoff.
func (s *send) noteRTOResend() {
	s.conn.socket.addTimeoutResend()
	s.conn.noteRTOTimeout()
	s.numResends++
}

// resend retransmits the packet unless it was acknowledged meanwhile.
func (s *send) resend() {
	if s.acked.IsSet() {
		return
	}
	if err := s.conn.send(s._type, s.connID, s.payload, s.seqNr); err != nil {
		log.Printf("error resending packet: %s", err)
	}
}
