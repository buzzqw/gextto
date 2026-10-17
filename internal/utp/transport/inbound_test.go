package transport

import (
	"net"
	"testing"
)

// The socket hands payloads that point into its reused receive buffer. An
// out-of-order packet waits in inbound, so it must keep its own copy: the next
// datagram overwrites the buffer before the gap is filled.
func TestOutOfOrderPayloadSurvivesBufferReuse(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSocketFromPacketConn(pc)
	if err != nil {
		t.Fatal(err)
	}
	defer s.CloseNow()

	mu.Lock()
	defer mu.Unlock()
	c := s.newConn(pc.LocalAddr())
	defer c.packetReadTimeoutTimer.Stop()
	c.recv_id = 7
	c.synAcked = true
	c.ack_nr = 10

	buf := []byte("BBBB")
	h := header{Type: stData, Version: 1, ConnID: c.recv_id, SeqNr: 12, AckNr: c.seq_nr - 1}
	c.processDelivery(h, buf) // seq 12 before 11: queued
	copy(buf, "AAAA")         // the socket reuses its buffer for the next datagram
	h.SeqNr = 11
	c.processDelivery(h, buf)
	c.unpendSendState()

	if got := string(c.readBuf[c.readPos:]); got != "AAAABBBB" {
		t.Fatalf("read buffer = %q, want %q", got, "AAAABBBB")
	}
}

// A consumed inbound slot must not keep its payload reachable through the
// backing array, which outlives the resliced window.
func TestConsumedInboundSlotReleasesPayload(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSocketFromPacketConn(pc)
	if err != nil {
		t.Fatal(err)
	}
	defer s.CloseNow()

	mu.Lock()
	defer mu.Unlock()
	c := s.newConn(pc.LocalAddr())
	defer c.packetReadTimeoutTimer.Stop()
	c.recv_id = 7
	c.synAcked = true
	c.ack_nr = 10

	h := header{Type: stData, Version: 1, ConnID: c.recv_id, SeqNr: 12, AckNr: c.seq_nr - 1}
	c.processDelivery(h, []byte("BBBB")) // queued: inbound[1]
	backing := c.inbound[:cap(c.inbound)]
	h.SeqNr = 11
	c.processDelivery(h, []byte("AAAA")) // fills the gap, both consumed
	c.unpendSendState()

	for i, r := range backing {
		if r.data != nil {
			t.Fatalf("backing slot %d still holds %q", i, r.data)
		}
	}
}
