package transport

// GEXTTO: our own uTP (BEP 29) code; see doc.go.
// Package-level constants, sentinel errors and the tiny helpers that do not
// belong to a single type.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Protocol and buffer limits. uTP segments a stream into packets of at most
// minMTU bytes; the base header is 20 bytes, plus 2 per extension.
const (
	// backlog is the maximum number of SYNs held before the oldest is reset.
	backlog = 50

	// minMTU is the smallest datagram uTP will emit (IPv6 min MTU minus the
	// IPv6 and fragment headers).
	minMTU = 1438
	// maxHeaderSize is the base header plus the largest selective-ACK block.
	maxHeaderSize  = 20 + 2 + (((maxUnackedInbound+7)/8)+3)/4*4
	maxPayloadSize = minMTU - maxHeaderSize
	maxRecvSize    = 0x2000

	// maxUnackedInbound bounds out-of-order packets buffered per connection;
	// maxUnackedSends bounds unacknowledged packets in flight.
	maxUnackedInbound = 256
	maxUnackedSends   = 256

	// readBufferLen is the receive window each connection offers.
	readBufferLen = 1 << 20 // ~1 MiB

	// readBufKeep is the largest drained read buffer a connection keeps for
	// reuse; a larger one (grown by a burst) is released.
	readBufKeep = 256 << 10

	// pendingSendStateDelay batches state packets: a state packet is not sent
	// for every inbound packet, and an outgoing data packet fills the role.
	pendingSendStateDelay = 500 * time.Microsecond

	// maxRTOBackoff caps the loss-driven adaptive RTO backoff (see
	// Conn.resendTimeout).
	maxRTOBackoff = 4
)

var (
	sendBufferPool = sync.Pool{
		New: func() any { return make([]byte, minMTU) },
	}
	// initialLatency is the RTT assumed for a connection before any sample or
	// seed is available. Deliberately generous so a new peer is not hammered
	// with resends before its real latency is known.
	initialLatency time.Duration
	// writeTimeout destroys a connection whose packet is never acknowledged.
	writeTimeout time.Duration
	// packetReadTimeout destroys a connection that hears nothing at all.
	packetReadTimeout time.Duration
)

func setDefaultDurations() {
	// An approximate upper bound for most connections across the world.
	initialLatency = 400 * time.Millisecond
	// Getting no reply for this period for a packet, we can probably rule out
	// latency and client lag.
	writeTimeout = 15 * time.Second
	// Somewhere longer than the BitTorrent grace period (90-120s), and less
	// than default TCP reset (4min).
	packetReadTimeout = 2 * time.Minute
}

func init() {
	setDefaultDurations()
	logLevel, _ = strconv.Atoi(os.Getenv("GO_UTP_LOGGING"))
	fmt.Sscanf(os.Getenv("GO_UTP_PACKET_DROP"), "%f", &artificialPacketDropChance)
	// GEXTTO: the adaptive RTO is on by default; the measurement harness and
	// the A/B test turn it off explicitly.
	adaptiveRTO.Store(true)
	// GEXTTO: the RTO never assumes an RTT below 5 ms.
	minLatencyNanos.Store(int64(5 * time.Millisecond))
}

var (
	mu                         sync.RWMutex
	sockets                    = map[*Socket]struct{}{}
	logLevel                   = 0
	artificialPacketDropChance = 0.0

	// GEXTTO: adaptiveRTO gates the loss-driven RTO backoff (see
	// Conn.resendTimeout). It is a variable so tests and the measurement
	// harness can A/B the change; production leaves it true. Atomic because
	// the harness flips it while socket goroutines run.
	adaptiveRTO atomic.Bool

	// GEXTTO: minimum RTT assumed for the RTO, in nanoseconds, resolving the
	// reference implementation's "TODO: introduce a minimum latency". Fast
	// paths (loopback, LAN) measure tens of microseconds; without the floor the
	// RTO goes sub-millisecond and a briefly stalled peer triggers a resend
	// storm. Matches the seed's 5 ms floor. Atomic so the harness can A/B it.
	minLatencyNanos atomic.Int64
)

// GEXTTO: minLatencyDuration is the minimum RTT assumed when computing the RTO.
func minLatencyDuration() time.Duration { return time.Duration(minLatencyNanos.Load()) }

var (
	errClosed               = errors.New("closed")
	errTimeout    net.Error = timeoutError{"i/o timeout"}
	errAckTimeout           = timeoutError{"timed out waiting for ack"}
)

// timeoutError is a net.Error that always reports Timeout.
type timeoutError struct {
	msg string
}

func (e timeoutError) Timeout() bool   { return true }
func (e timeoutError) Error() string   { return e.msg }
func (e timeoutError) Temporary() bool { return false }

// st is the uTP packet type. The four-bit type field in the header.
type st int

const (
	stData  st = 0
	stFin      = 1
	stState    = 2
	stReset    = 3
	stSyn      = 4

	// stMax is the highest valid packet type, used to validate headers.
	stMax = stSyn
)

func (t st) String() string {
	switch t {
	case stData:
		return "stData"
	case stFin:
		return "stFin"
	case stState:
		return "stState"
	case stReset:
		return "stReset"
	case stSyn:
		return "stSyn"
	default:
		panic(fmt.Sprintf("%d", t))
	}
}

// recv is one buffered inbound packet in a connection's out-of-order window.
type recv struct {
	seen bool
	data []byte
	Type st
}

// syn is an unaccepted connection request held in the backlog.
type syn struct {
	seq_nr, conn_id uint16
	// addr is the resolved remote address the SYN came from.
	addr netip.AddrPort
}

// read is a datagram read from the underlying packet connection.
type read struct {
	data []byte
	from net.Addr
}

// Dial connects to a remote uTP listener, creating a Socket used only for this
// connection (and closed with it). To share a Socket, use Socket.DialContext.
func Dial(addr string) (net.Conn, error) {
	return DialContext(context.Background(), addr)
}

// DialContext is Dial with a context; the Socket is closed with the Conn.
func DialContext(ctx context.Context, addr string) (net.Conn, error) {
	s, err := NewSocket("udp", ":0")
	if err != nil {
		return nil, err
	}
	defer s.Close()
	return s.DialContext(ctx, "", addr)
}

// Listen returns a Socket accepting incoming connections on laddr.
func Listen(laddr string) (net.Listener, error) {
	return NewSocket("udp", laddr)
}

// nowTimestamp is the uTP microsecond clock (wraps every ~71 minutes, as the
// wire format expects).
func nowTimestamp() uint32 {
	return uint32(time.Now().UnixNano() / int64(time.Microsecond))
}

// seqLess reports whether a comes before b in uTP's wrapping 16-bit sequence
// space. The expression is kept exactly as the reference implementation: the
// two halves (b below or above 0x8000) need different comparisons around the
// half-way point, so it is not just a single signed subtraction.
func seqLess(a, b uint16) bool {
	const half = uint16(0x8000)
	if b < half {
		return a < b || a >= b-half
	}
	return a < b && a >= b-half
}
