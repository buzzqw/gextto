package upstream

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
	resendTimer *time.Timer
	numResends  int
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
	if s.resendTimer != nil {
		s.resendTimer.Stop()
		s.resendTimer = nil
	}
	return
}

// timedOut destroys the connection: the peer never acknowledged within
// writeTimeout.
func (s *send) timedOut() {
	s.conn.destroy(errAckTimeout)
}

// timeoutResend is the RTO timer: retransmit the packet and schedule the next
// attempt, feeding the loss signal to the socket counters and this connection's
// adaptive backoff.
func (s *send) timeoutResend() {
	mu.Lock()
	defer mu.Unlock()
	if time.Since(s.started) >= writeTimeout {
		s.timedOut()
		return
	}
	if s.acked.IsSet() || s.conn.destroyed.IsSet() {
		return
	}
	rt := s.conn.resendTimeout()
	s.resend()
	s.conn.socket.addTimeoutResend()
	s.conn.noteRTOTimeout()
	s.numResends++
	s.resendTimer.Reset(rt * time.Duration(s.numResends))
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
