package transport

// GEXTTO: our own uTP (BEP 29) code; see doc.go.
// Socket multiplexes a net.PacketConn: it reads datagrams once and dispatches
// each uTP packet to the Conn it belongs to. Conns and Sockets all share the
// package mutex mu, so their state transitions are serialised (the reader loop
// releases mu around the blocking read).

import (
	"context"
	"errors"
	"io"
	"log"
	"math/rand"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
)

var (
	_ net.Listener   = &Socket{}
	_ net.PacketConn = &Socket{}
)

// connKey identifies a connection on the underlying packet stream: the remote
// address plus the connection id. A netip.AddrPort is comparable and
// allocation-free, unlike net.Addr.String(), which the reader would otherwise
// compute for every inbound packet.
type connKey struct {
	remoteAddr netip.AddrPort
	connID     uint16
}

// addrPortOf turns a UDP net.Addr into the comparable key form, normalising
// IPv4-in-IPv6 and keeping a zone if present. It does not allocate.
func addrPortOf(addr net.Addr) netip.AddrPort {
	ua, ok := addr.(*net.UDPAddr)
	if !ok {
		ap, _ := netip.ParseAddrPort(addr.String())
		return ap
	}
	ip, _ := netip.AddrFromSlice(ua.IP)
	ip = ip.Unmap()
	if ua.Zone != "" {
		ip = ip.WithZone(ua.Zone)
	}
	return netip.AddrPortFrom(ip, uint16(ua.Port))
}

// Socket wraps a net.PacketConn and diverts uTP packets to its child Conns. It
// also implements net.PacketConn itself: packets that belong to no uTP
// connection (for example DHT traffic on the shared port) are handed to
// ReadFrom.
type Socket struct {
	pc    net.PacketConn
	conns map[connKey]*Conn

	backlogNotEmpty Event
	backlog         map[syn]struct{}

	closed    Event
	destroyed Event

	// wgReadWrite tracks in-flight reads and writes on the packet connection,
	// so CloseNow can wait for them.
	wgReadWrite sync.WaitGroup

	// unusedReads carries datagrams that are not uTP to ReadFrom.
	unusedReads chan read
	connDeadlines
	// ReadErr is the error that stopped the read loop, if any.
	ReadErr error

	// GEXTTO: per-socket resend counters, the congestion signal exported to
	// SessionStats. timeoutResends counts RTO-driven resends (send.go),
	// fastResends selective-ACK fast resends (conn.go ackSkipped), synDrops
	// backlog SYNs evicted under burst (pushBacklog).
	timeoutResends atomic.Int64
	fastResends    atomic.Int64
	synDrops       atomic.Int64

	// GEXTTO: optional per-remote RTT estimate (for example from recent dial
	// races) seeding the initial RTO of fresh connections. Nil uses the fixed
	// initialLatency. Consulted with "ip:port".
	initialLatencyEstimator func(addr string) (time.Duration, bool)
}

func listenPacket(network, addr string) (net.PacketConn, error) {
	return net.ListenPacket(network, addr)
}

// NewSocket creates a Socket over a new packet connection of the given network
// (empty means "udp") and address ("host:port").
func NewSocket(network, addr string) (*Socket, error) {
	if network == "" {
		network = "udp"
	}
	pc, err := listenPacket(network, addr)
	if err != nil {
		return nil, err
	}
	return NewSocketFromPacketConn(pc)
}

// NewSocketFromPacketConn creates a Socket over pc, which the Socket then owns
// and closes. To keep pc after the Socket closes, use
// NewSocketFromPacketConnNoClose.
func NewSocketFromPacketConn(pc net.PacketConn) (*Socket, error) {
	s := &Socket{
		backlog:     make(map[syn]struct{}, backlog),
		pc:          pc,
		unusedReads: make(chan read, 100),
	}
	mu.Lock()
	sockets[s] = struct{}{}
	mu.Unlock()
	go s.reader()
	return s, nil
}

// NewSocketFromPacketConnNoClose is NewSocketFromPacketConn for a PacketConn
// that outlives the Socket.
func NewSocketFromPacketConnNoClose(pc net.PacketConn) (*Socket, error) {
	return NewSocketFromPacketConn(packetConnNopCloser{pc})
}

// unusedRead hands a non-uTP datagram to ReadFrom, or drops it when nobody is
// reading (the shared socket is best-effort).
func (s *Socket) unusedRead(r read) {
	unusedReads.Add(1)
	r.data = append([]byte(nil), r.data...)
	select {
	case s.unusedReads <- r:
	default:
		unusedReadsDropped.Add(1)
	}
}

// pushBacklog queues an unaccepted SYN, resetting an old one when the backlog is
// full. The eviction is counted (burst signal for SessionStats).
func (s *Socket) pushBacklog(sy syn) {
	if _, ok := s.backlog[sy]; ok {
		return
	}
	// Make room by dropping an arbitrary (map order) old SYN.
	for k := range s.backlog {
		if len(s.backlog) < backlog {
			break
		}
		delete(s.backlog, k)
		// GEXTTO: count the eviction (burst signal for SessionStats).
		s.synDrops.Add(1)
		// A SYN is addressed to the remote's recv_id, so that is where the
		// reset goes.
		s.reset(net.UDPAddrFromAddrPort(k.addr), k.seq_nr, k.conn_id)
	}
	s.backlog[sy] = struct{}{}
	s.backlogChanged()
}

// reader reads the packet connection and dispatches datagrams until it is
// closed. It holds mu for the state it touches and releases it around the
// blocking read.
func (s *Socket) reader() {
	mu.Lock()
	defer mu.Unlock()
	defer s.destroy()
	var b [maxRecvSize]byte
	for {
		s.wgReadWrite.Add(1)
		mu.Unlock()
		n, addr, err := s.pc.ReadFrom(b[:])
		s.wgReadWrite.Done()
		mu.Lock()
		if s.destroyed.IsSet() {
			return
		}
		if err != nil {
			log.Printf("error reading Socket PacketConn: %s", err)
			s.ReadErr = err
			return
		}
		// The datagram is handled in place: b is reused for the next one, so
		// whatever keeps packet bytes past this call copies them (an
		// out-of-order payload in processDelivery, an unused read here).
		s.handleReceivedPacket(read{data: b[:n], from: addr})
	}
}

func receivedUTPPacketSize(n int) {
	if n > largestReceivedUTPPacket {
		largestReceivedUTPPacket = n
		largestReceivedUTPPacketExpvar.Set(int64(n))
	}
}

// connForRead looks up the connection a packet belongs to. A SYN is indexed by
// the id one below the eventual recv_id, so the lookup uses ConnID+1 for it.
func (s *Socket) connForRead(h header, from net.Addr) (*Conn, bool) {
	id := h.ConnID
	if h.Type == stSyn {
		id++
	}
	c, ok := s.conns[connKey{addrPortOf(from), id}]
	return c, ok
}

// handlePacketReceivedForEstablishedConn delivers a packet to its connection,
// first guarding the SYN collision case: a SYN for an id that a locally dialled
// connection already owns (send_id is one higher) is a conflict, and the peer
// gets a reset.
func (s *Socket) handlePacketReceivedForEstablishedConn(h header, from net.Addr, payload []byte, c *Conn) {
	if h.Type == stSyn {
		if h.ConnID == c.send_id-2 {
			log.Print("resetting conflicting syn")
			s.reset(from, h.SeqNr, h.ConnID)
			return
		}
		if h.ConnID != c.send_id {
			panic("bad assumption")
		}
	}
	c.receivePacket(h, payload)
}

// handleReceivedPacket decodes a datagram and routes it: to its connection, to
// the SYN backlog, or to ReadFrom (a reset or an unknown packet).
func (s *Socket) handleReceivedPacket(p read) {
	if len(p.data) < headerLen {
		s.unusedRead(p)
		return
	}
	var h header
	hEnd, err := h.Unmarshal(p.data)
	if err != nil || h.Type > stMax || h.Version != 1 {
		s.unusedRead(p)
		return
	}
	if c, ok := s.connForRead(h, p.from); ok {
		receivedUTPPacketSize(len(p.data))
		s.handlePacketReceivedForEstablishedConn(h, p.from, p.data[hEnd:], c)
		return
	}
	// No existing connection for this packet.
	switch h.Type {
	case stSyn:
		s.pushBacklog(syn{
			seq_nr:  h.SeqNr,
			conn_id: h.ConnID,
			addr:    addrPortOf(p.from),
		})
		return
	case stReset:
		// A late reset for a connection we are already done with.
	default:
		unexpectedPacketsRead.Add(1)
		// Send a reset for each possible interpretation of the connection id:
		// the packet itself and the two neighbouring ids, since we cannot tell
		// whether the connection would have been inbound or outbound.
		s.reset(p.from, h.SeqNr, h.ConnID)
		s.reset(p.from, h.SeqNr, h.ConnID-1)
		s.reset(p.from, h.SeqNr, h.ConnID+1)
	}
	s.unusedRead(p)
}

// reset sends a reset packet to addr, acknowledging ackNr under connID.
func (s *Socket) reset(addr net.Addr, ackNr, connId uint16) {
	b := make([]byte, 0, maxHeaderSize)
	h := header{Type: stReset, Version: 1, ConnID: connId, AckNr: ackNr}
	b = b[:h.Marshal(b)]
	go s.writeTo(b, addr)
}

// connIDFree reports whether id is usable as a recv_id for remoteAddr: it must
// not collide with an existing connection's recv_id, nor with its neighbours
// (the remote would use id-1 as its send_id when dialling our own Socket).
func (s *Socket) connIDFree(remoteAddr netip.AddrPort, id uint16) bool {
	if _, used := s.conns[connKey{remoteAddr, id}]; used {
		return false
	}
	if _, used := s.conns[connKey{remoteAddr, id + 1}]; used {
		return false
	}
	if _, used := s.conns[connKey{remoteAddr, id - 1}]; used {
		return false
	}
	return true
}

// newConnID returns a recv_id free for remoteAddr. It probes from a random
// point and stops at the first free id, which is O(1) with the handful of
// connections per address a socket really has, and allocates nothing. The
// previous implementation built a 64K-entry table per dial (half a megabyte)
// to pick a random free id.
func (s *Socket) newConnID(remoteAddr netip.AddrPort) (id uint16) {
	const range16 = 1 << 16
	start := uint16(rand.Intn(range16))
	for i := 0; i < range16; i++ {
		id = start + uint16(i)
		if s.connIDFree(remoteAddr, id) {
			return id
		}
	}
	return 0
}

var (
	zeroipv4 = net.ParseIP("0.0.0.0")
	zeroipv6 = net.ParseIP("::")

	ipv4lo = mustResolveIP("127.0.0.1")
	ipv6lo = mustResolveIP("::1")
)

func mustResolveIP(addr string) net.IP {
	u, err := net.ResolveIPAddr("ip", addr)
	if err != nil {
		panic(err)
	}
	return u.IP
}

// realRemoteAddr maps the unspecified addresses net gives for a wildcard
// listener to loopback, so a loopback peer is not recorded as 0.0.0.0.
func realRemoteAddr(addr net.Addr) net.Addr {
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		return addr
	}
	switch {
	case udpAddr.IP.Equal(zeroipv4):
		udpAddr.IP = ipv4lo
	case udpAddr.IP.Equal(zeroipv6):
		udpAddr.IP = ipv6lo
	}
	return addr
}

// newConn allocates a Conn attached to this socket. Callers hold mu.
func (s *Socket) newConn(addr net.Addr) *Conn {
	c := &Conn{
		socket:           s,
		remoteSocketAddr: realRemoteAddr(addr),
		created:          time.Now(),
		// GEXTTO: adaptive RTO starts at the measured-behaviour multiplier.
		rtoBackoff: 1,
	}
	c.sendPendingSendSendStateTimer = stoppedFuncTimer(c.sendPendingSendStateTimerCallback)
	c.packetReadTimeoutTimer = time.AfterFunc(packetReadTimeout, c.receivePacketTimeoutCallback)
	return c
}

// Dial connects to a remote uTP peer using this socket.
func (s *Socket) Dial(addr string) (net.Conn, error) {
	return s.DialContext(context.Background(), "", addr)
}

func (s *Socket) resolveAddr(network, addr string) (net.Addr, error) {
	n := s.network()
	if network != "" {
		n = network
	}
	return net.ResolveUDPAddr(n, addr)
}

func (s *Socket) network() string { return s.pc.LocalAddr().Network() }

// startOutboundConn registers a new outbound connection and sends its SYN.
func (s *Socket) startOutboundConn(addr net.Addr) (*Conn, error) {
	mu.Lock()
	defer mu.Unlock()
	c := s.newConn(addr)
	key := addrPortOf(c.remoteSocketAddr)
	c.recv_id = s.newConnID(key)
	c.send_id = c.recv_id + 1
	if logLevel >= 1 {
		log.Printf("dial registering addr: %s", c.RemoteAddr().String())
	}
	if !s.registerConn(c.recv_id, key, c) {
		log.Println(c.recv_id, c.RemoteAddr().String())
		for k, cc := range s.conns {
			log.Println(k, cc, cc.age())
		}
		log.Printf("that's %d connections", len(s.conns))
		return nil, errors.New("couldn't register new connection")
	}
	c.seq_nr = 1
	c.writeSyn()
	return c, nil
}

// DialContext dials addr and waits for the handshake, honouring ctx.
func (s *Socket) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	netAddr, err := s.resolveAddr(network, addr)
	if err != nil {
		return nil, err
	}
	c, err := s.startOutboundConn(netAddr)
	if err != nil {
		return nil, err
	}
	connErr := make(chan error, 1)
	go func() { connErr <- c.recvSynAck() }()
	select {
	case err = <-connErr:
	case <-ctx.Done():
		err = ctx.Err()
	}
	if err != nil {
		mu.Lock()
		c.destroy(errors.New("dial timeout"))
		mu.Unlock()
		return nil, err
	}
	mu.Lock()
	c.updateCanWrite()
	mu.Unlock()
	return c, nil
}

// writeTo writes a datagram to the underlying connection, applying the test
// packet-drop knob. It is the connection's send path for every packet.
func (s *Socket) writeTo(b []byte, addr net.Addr) (int, error) {
	if drop := artificialPacketDropChance; drop != 0 && rand.Float64() < drop {
		return len(b), nil
	}
	return s.pc.WriteTo(b, addr)
}

// registerConn indexes a connection under remoteAddr+recvID. It reports
// whether the id was free; a duplicate keeps the existing connection.
func (s *Socket) registerConn(recvID uint16, remoteAddr netip.AddrPort, c *Conn) bool {
	if s.conns == nil {
		s.conns = make(map[connKey]*Conn)
	}
	key := connKey{remoteAddr, recvID}
	if _, ok := s.conns[key]; ok {
		return false
	}
	c.connKey = key
	s.conns[key] = c
	return true
}

func (s *Socket) backlogChanged() {
	s.backlogNotEmpty.SetBool(len(s.backlog) != 0)
}

// nextSyn returns the next pending SYN, blocking until one arrives or the
// socket closes. Callers hold mu.
func (s *Socket) nextSyn() (sy syn, err error) {
	for {
		waitEvents(&mu, &s.closed, &s.backlogNotEmpty, &s.destroyed)
		if s.closed.IsSet() {
			return sy, errClosed
		}
		if s.destroyed.IsSet() {
			return sy, s.ReadErr
		}
		for k := range s.backlog {
			delete(s.backlog, k)
			s.backlogChanged()
			return k, nil
		}
	}
}

// ackSyn accepts a SYN: it creates the connection, sends the state packet (the
// implicit handshake ACK) and reports ok false when the SYN duplicates an
// existing connection (a resend, which is ACKed again).
func (s *Socket) ackSyn(sy syn) (c *Conn, ok bool) {
	c = s.newConn(net.UDPAddrFromAddrPort(sy.addr))
	c.send_id = sy.conn_id
	c.recv_id = c.send_id + 1
	c.seq_nr = uint16(rand.Int())
	c.lastAck = c.seq_nr - 1
	c.ack_nr = sy.seq_nr
	c.synAcked = true
	c.updateCanWrite()
	if !s.registerConn(c.recv_id, sy.addr, c) {
		// A SYN resend for a connection already accepted: ACK it again.
		c = s.conns[connKey{sy.addr, c.recv_id}]
		if c.send_id != sy.conn_id {
			panic(":|")
		}
		c.sendState()
		return c, false
	}
	c.sendState()
	return c, true
}

// Accept blocks for the next inbound connection.
func (s *Socket) Accept() (net.Conn, error) {
	mu.Lock()
	defer mu.Unlock()
	for {
		sy, err := s.nextSyn()
		if err != nil {
			return nil, err
		}
		if c, ok := s.ackSyn(sy); ok {
			c.updateCanWrite()
			return c, nil
		}
	}
}

// GEXTTO: SetInitialLatencyEstimator installs the per-remote RTT estimate used
// to seed fresh connections' RTO (see Conn.latency). Nil disables it. Call once
// before dialling; it is only read afterwards.
func (s *Socket) SetInitialLatencyEstimator(f func(addr string) (time.Duration, bool)) {
	s.initialLatencyEstimator = f
}

// GEXTTO: initialLatencyFor returns the seed RTT for addr clamped to
// [5 ms, initialLatency], or false when no estimate applies.
func (s *Socket) initialLatencyFor(addr string) (time.Duration, bool) {
	if s == nil || s.initialLatencyEstimator == nil {
		return 0, false
	}
	est, ok := s.initialLatencyEstimator(addr)
	if !ok || est <= 0 {
		return 0, false
	}
	const minSeedLatency = 5 * time.Millisecond
	if est < minSeedLatency {
		est = minSeedLatency
	}
	if est > initialLatency {
		est = initialLatency
	}
	return est, true
}

// GEXTTO: ResendStats reports, over the socket's lifetime, the RTO-driven and
// selective-ACK fast resends: the loss/congestion signal behind SessionStats.
func (s *Socket) ResendStats() (timeout, fast int64) {
	return s.timeoutResends.Load(), s.fastResends.Load()
}

// GEXTTO: addTimeoutResend counts one RTO-driven resend.
func (s *Socket) addTimeoutResend() { s.timeoutResends.Add(1) }

// GEXTTO: addFastResend counts one selective-ACK fast resend.
func (s *Socket) addFastResend() { s.fastResends.Add(1) }

// GEXTTO: SynDrops reports the backlog SYNs evicted under burst.
func (s *Socket) SynDrops() int64 { return s.synDrops.Load() }

// Addr is the local address the socket listens on.
func (s *Socket) Addr() net.Addr { return s.pc.LocalAddr() }

// CloseNow closes the socket and every connection at once, without the
// graceful FIN handshake.
func (s *Socket) CloseNow() error {
	mu.Lock()
	defer mu.Unlock()
	s.closed.Set()
	for _, c := range s.conns {
		c.closeNow()
	}
	s.destroy()
	s.wgReadWrite.Wait()
	return nil
}

// Close starts a graceful shutdown: the socket tears down once every
// connection has finished.
func (s *Socket) Close() error {
	mu.Lock()
	defer mu.Unlock()
	s.closed.Set()
	s.lazyDestroy()
	return nil
}

// lazyDestroy destroys a closed socket once no connection remains.
func (s *Socket) lazyDestroy() {
	if len(s.conns) != 0 {
		return
	}
	if !s.closed.IsSet() {
		return
	}
	s.destroy()
}

// destroy tears the socket down: unregisters it, closes the packet connection
// and destroys its connections. Callers hold mu.
func (s *Socket) destroy() {
	delete(sockets, s)
	s.destroyed.Set()
	s.pc.Close()
	for _, c := range s.conns {
		c.destroy(errors.New("Socket destroyed"))
	}
}

func (s *Socket) LocalAddr() net.Addr { return s.pc.LocalAddr() }

// ReadFrom returns a datagram that was not uTP (shared DHT traffic), blocking
// until one arrives or the read deadline passes.
func (s *Socket) ReadFrom(p []byte) (int, net.Addr, error) {
	select {
	case r, ok := <-s.unusedReads:
		if !ok {
			return 0, nil, io.EOF
		}
		return copy(p, r.data), r.from, nil
	case <-s.connDeadlines.read.passed.LockedChan(&mu):
		return 0, nil, errTimeout
	}
}

// WriteTo writes a datagram through the shared socket (DHT traffic).
func (s *Socket) WriteTo(b []byte, addr net.Addr) (int, error) {
	mu.Lock()
	timedOut := s.connDeadlines.write.passed.IsSet()
	s.wgReadWrite.Add(1)
	defer s.wgReadWrite.Done()
	mu.Unlock()
	if timedOut {
		return 0, errTimeout
	}
	return s.pc.WriteTo(b, addr)
}
