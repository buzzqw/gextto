// Package utp isolates the uTP transport (BEP 29) behind Gextto's own API.
//
// The socket implementation is Gextto's own (internal/utp/upstream, EUPL-1.2,
// written against the anacrolix/utp v0.2.0 reference) so it can be tuned and
// instrumented; the engine (internal/gxcore) only talks to the Socket type
// below. The package also exports counters (dials, accepts, errors and their
// retransmissions) behind peer-racing and the adaptive RTO.
package utp

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"time"

	aup "github.com/buzzqw/gextto/internal/utp/upstream"
)

// Stats snapshots socket activity since creation.
type Stats struct {
	Dials        uint64
	Accepts      uint64
	DialErrors   uint64
	AcceptErrors uint64
	// ResendsTimeout counts RTO-driven packet resends (loss/congestion
	// signal); ResendsFast counts selective-ACK fast resends. SynDrops
	// counts backlog SYNs evicted under burst.
	ResendsTimeout uint64
	ResendsFast    uint64
	SynDrops       uint64
}

// Socket is Gextto's uTP socket. It wraps a UDP packet connection, diverting
// uTP packets to child uTP conns, and doubles as the net.PacketConn shared
// with the DHT on the single port.
type Socket struct {
	inner        *aup.Socket
	dials        atomic.Uint64
	accepts      atomic.Uint64
	dialErrors   atomic.Uint64
	acceptErrors atomic.Uint64
}

var (
	_ net.PacketConn = (*Socket)(nil)
	_ net.Listener   = (*Socket)(nil)
)

// NewSocketFromPacketConn builds a Socket over pc, like the wrapped library.
func NewSocketFromPacketConn(pc net.PacketConn) (*Socket, error) {
	inner, err := aup.NewSocketFromPacketConn(pc)
	if err != nil {
		return nil, err
	}
	return &Socket{inner: inner}, nil
}

// DialContext opens an outbound uTP connection. A dial cancelled by the peer
// race (TCP won) or by shutdown is not a failure and does not count as a dial
// error: only a real error (no SYN-ACK within the timeout, socket error) does,
// so the counter stays a UDP-reachability diagnostic.
func (s *Socket) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	s.dials.Add(1)
	c, err := s.inner.DialContext(ctx, network, addr)
	if err != nil && !errors.Is(err, context.Canceled) {
		s.dialErrors.Add(1)
	}
	return c, err
}

// Accept waits for the next inbound uTP connection.
func (s *Socket) Accept() (net.Conn, error) {
	c, err := s.inner.Accept()
	if err != nil {
		s.acceptErrors.Add(1)
		return nil, err
	}
	s.accepts.Add(1)
	return c, nil
}

// Close closes the socket gracefully.
func (s *Socket) Close() error { return s.inner.Close() }

// CloseNow destroys the socket immediately, releasing pending connections.
func (s *Socket) CloseNow() error { return s.inner.CloseNow() }

// Addr reports the underlying packet connection address.
func (s *Socket) Addr() net.Addr { return s.inner.Addr() }

// LocalAddr implements net.PacketConn / net.Listener.
func (s *Socket) LocalAddr() net.Addr { return s.inner.LocalAddr() }

// ReadFrom implements net.PacketConn (used by the DHT on the shared socket).
func (s *Socket) ReadFrom(p []byte) (int, net.Addr, error) { return s.inner.ReadFrom(p) }

// WriteTo implements net.PacketConn (used by the DHT on the shared socket).
func (s *Socket) WriteTo(p []byte, addr net.Addr) (int, error) {
	return s.inner.WriteTo(p, addr)
}

// SetDeadline implements net.PacketConn.
func (s *Socket) SetDeadline(t time.Time) error { return s.inner.SetDeadline(t) }

// SetReadDeadline implements net.PacketConn.
func (s *Socket) SetReadDeadline(t time.Time) error { return s.inner.SetReadDeadline(t) }

// SetWriteDeadline implements net.PacketConn.
func (s *Socket) SetWriteDeadline(t time.Time) error { return s.inner.SetWriteDeadline(t) }

// SetInitialLatencyEstimator installs the per-remote RTT estimate seeding
// fresh connections' RTO. See upstream Socket.
func (s *Socket) SetInitialLatencyEstimator(f func(addr string) (time.Duration, bool)) {
	s.inner.SetInitialLatencyEstimator(f)
}

// Stats returns the socket activity counters.
func (s *Socket) Stats() Stats {
	timeout, fast := s.inner.ResendStats()
	return Stats{
		Dials:          s.dials.Load(),
		Accepts:        s.accepts.Load(),
		DialErrors:     s.dialErrors.Load(),
		AcceptErrors:   s.acceptErrors.Load(),
		ResendsTimeout: uint64(timeout),
		ResendsFast:    uint64(fast),
		SynDrops:       uint64(s.inner.SynDrops()),
	}
}
