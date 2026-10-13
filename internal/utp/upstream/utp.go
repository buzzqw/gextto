// Package utp implements uTP, the micro transport protocol as used with
// Bittorrent. It opts for simplicity and reliability over strict adherence to
// the (poor) spec. It allows using the underlying OS-level transport despite
// dispatching uTP on top to allow for example, shared socket use with DHT.
// Additionally, multiple uTP connections can share the same OS socket, to
// truly realize uTP's claim to be light on system and network switching
// resources.
//
// Socket is a wrapper of net.UDPConn, and performs dispatching of uTP packets
// to attached uTP Conns. Dial and Accept is done via Socket. Conn implements
// net.Conn over uTP, via aforementioned Socket.
package upstream

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// Maximum received SYNs that haven't been accepted. If more SYNs are
	// received, a pseudo randomly selected SYN is replied to with a reset to
	// make room.
	backlog = 50

	// IPv6 min MTU is 1280, -40 for IPv6 header, and ~8 for fragment header?
	minMTU = 1438 // Why?
	// uTP header of 20, +2 for the next extension, and an optional selective
	// ACK.
	maxHeaderSize  = 20 + 2 + (((maxUnackedInbound+7)/8)+3)/4*4
	maxPayloadSize = minMTU - maxHeaderSize
	maxRecvSize    = 0x2000

	// Maximum out-of-order packets to buffer.
	maxUnackedInbound = 256
	maxUnackedSends   = 256

	readBufferLen = 1 << 20 // ~1MiB

	// How long to wait before sending a state packet, after one is required.
	// This prevents spamming a state packet for every packet received, and
	// non-state packets that are being sent also fill the role.
	pendingSendStateDelay = 500 * time.Microsecond

	// GEXTTO: adaptive RTO. A connection whose packets keep timing out backs
	// its RTO off (doubling up to this cap) so it stops hammering a congested
	// or stalled peer; a delivered packet relaxes it back toward the measured
	// RTT. See Conn.resendTimeout/noteRTOTimeout/noteDelivery.
	maxRTOBackoff = 4
)

var (
	sendBufferPool = sync.Pool{
		New: func() interface{} { return make([]byte, minMTU) },
	}
	// This is the latency we assume on new connections. It should be higher
	// than the latency we expect on most connections to prevent excessive
	// resending to peers that take a long time to respond, before we've got a
	// better idea of their actual latency.
	initialLatency time.Duration
	// If a write isn't acked within this period, destroy the connection.
	writeTimeout time.Duration
	// Assume the connection has been closed by the peer getting no packets of
	// any kind for this time.
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
}

// Strongly-type guarantee of resolved network address.
type resolvedAddrStr string

type read struct {
	data []byte
	from net.Addr
}

type syn struct {
	seq_nr, conn_id uint16
	// net.Addr.String() of a Socket's real net.PacketConn.
	addr string
}

var (
	mu                         sync.RWMutex
	sockets                    = map[*Socket]struct{}{}
	logLevel                   = 0
	artificialPacketDropChance = 0.0

	// GEXTTO: adaptiveRTO gates the loss-driven RTO backoff (see Conn.
	// resendTimeout). It is a variable so tests and the measurement harness
	// can A/B the change; production leaves it true. Atomic because the
	// harness flips it while socket goroutines run.
	adaptiveRTO atomic.Bool

	// GEXTTO: minimum RTT assumed for the RTO, in nanoseconds, resolving the
	// vendored "TODO: introduce a minimum latency". Fast paths (loopback, LAN)
	// measure tens of microseconds; without the floor the RTO goes
	// sub-millisecond and a briefly stalled peer triggers a resend storm.
	// Matches the seed's 5 ms floor. Atomic so the harness can A/B it.
	minLatencyNanos atomic.Int64
)

// GEXTTO: minLatencyDuration is the minimum RTT assumed when computing the RTO.
func minLatencyDuration() time.Duration { return time.Duration(minLatencyNanos.Load()) }

func init() {
	logLevel, _ = strconv.Atoi(os.Getenv("GO_UTP_LOGGING"))
	fmt.Sscanf(os.Getenv("GO_UTP_PACKET_DROP"), "%f", &artificialPacketDropChance)
	// GEXTTO: the adaptive RTO is on by default; the measurement harness and
	// the A/B test turn it off explicitly.
	adaptiveRTO.Store(true)
	// GEXTTO: the RTO never assumes an RTT below 5 ms.
	minLatencyNanos.Store(int64(5 * time.Millisecond))
}

var (
	errClosed               = errors.New("closed")
	errTimeout    net.Error = timeoutError{"i/o timeout"}
	errAckTimeout           = timeoutError{"timed out waiting for ack"}
)

type timeoutError struct {
	msg string
}

func (me timeoutError) Timeout() bool   { return true }
func (me timeoutError) Error() string   { return me.msg }
func (me timeoutError) Temporary() bool { return false }

type st int

func (me st) String() string {
	switch me {
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
		panic(fmt.Sprintf("%d", me))
	}
}

const (
	stData  st = 0
	stFin      = 1
	stState    = 2
	stReset    = 3
	stSyn      = 4

	// Used for validating packet headers.
	stMax = stSyn
)

type recv struct {
	seen bool
	data []byte
	Type st
}

// Attempt to connect to a remote uTP listener, creating a Socket just for
// this connection.
func Dial(addr string) (net.Conn, error) {
	return DialContext(context.Background(), addr)
}

// Same as Dial with a timeout parameter. Creates a Socket just for the
// connection, which will be closed with the Conn is. To reuse another Socket,
// see Socket.Dial.
func DialContext(ctx context.Context, addr string) (nc net.Conn, err error) {
	s, err := NewSocket("udp", ":0")
	if err != nil {
		return
	}
	defer s.Close()
	return s.DialContext(ctx, "", addr)
}

// Listen creates listener Socket to accept incoming connections.
func Listen(laddr string) (net.Listener, error) {
	return NewSocket("udp", laddr)
}

func nowTimestamp() uint32 {
	return uint32(time.Now().UnixNano() / int64(time.Microsecond))
}

func seqLess(a, b uint16) bool {
	if b < 0x8000 {
		return a < b || a >= b-0x8000
	} else {
		return a < b && a >= b-0x8000
	}
}
