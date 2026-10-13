package udptracker

// http://bittorrent.org/beps/bep_0015.html
// http://xbtt.sourceforge.net/udp_tracker_protocol.html

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/backoff"
	"github.com/buzzqw/gextto/internal/gxcore/internal/bencodecheck"
	"github.com/buzzqw/gextto/internal/gxcore/internal/blocklist"
	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/netx"
	"github.com/buzzqw/gextto/internal/gxcore/internal/resolver"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

const (
	connectionIDMagic    = 0x41727101980
	connectionIDInterval = time.Minute
)

// Transport multiplexes every UDP tracker exchange over one socket: it owns
// the connect sessions per destination, the transaction table, the retry
// timers and the read loop. Do submits requests; Run serves them.
type Transport struct {
	blocklist  *blocklist.Blocklist
	log        logger.Logger
	dnsTimeout time.Duration

	// Do hands requests to the run loop here.
	requestC chan udpRequest

	// The read loop delivers datagrams here.
	readC chan []byte

	// Close ends the run loop...
	closeC chan struct{}

	// ...and the loop closes doneC on exit.
	doneC chan struct{}
}

// NewTransport prepares the engine; run it with Run.
func NewTransport(bl *blocklist.Blocklist, dnsTimeout time.Duration) *Transport {
	return &Transport{
		blocklist:  bl,
		log:        logger.New("udp tracker transport"),
		dnsTimeout: dnsTimeout,
		requestC:   make(chan udpRequest),
		readC:      make(chan []byte),
		closeC:     make(chan struct{}),
		doneC:      make(chan struct{}),
	}
}

// Close stops the transport and waits for the run loop to exit.
func (t *Transport) Close() {
	close(t.closeC)
	<-t.doneC
}

// Do runs one exchange to completion with retries, or fails on context
// cancel or transport close.
func (t *Transport) Do(req udpRequest) ([]byte, error) {
	var errTransportClosed = errors.New("udp transport closed")

	select {
	case t.requestC <- req:
	case <-req.GetContext().Done():
		return nil, req.GetContext().Err()
	case <-t.closeC:
		return nil, errTransportClosed
	}
	select {
	case <-req.Done():
		return req.GetResponse()
	case <-req.GetContext().Done():
		return nil, req.GetContext().Err()
	case <-t.closeC:
		return nil, errTransportClosed
	}
}

// runState is the Run loop's mutable world: socket, sessions and exchanges.
type runState struct {
	conn         *net.UDPConn
	listening    bool
	listenErr    error
	transactions map[int32]*transaction
	connections  map[string]*connection
	connectDone  chan *connectionResult
	expired      chan string
}

// Run serves the transport until Close: socket setup, then requests,
// connects, expiries, replies and shutdown.
func (t *Transport) Run() {
	t.log.Debugln("Starting transport run loop")
	st := &runState{
		transactions: make(map[int32]*transaction),
		connections:  make(map[string]*connection),
		connectDone:  make(chan *connectionResult),
		expired:      make(chan string),
	}
	if laddr, err := netx.UDPLocalAddr(); err == nil {
		if st.conn, err = net.ListenUDP(udpNetwork(laddr), laddr); err == nil {
			st.listening = true
			t.log.Debugln("Starting transport read loop")
			go t.readLoop(st.conn)
		} else {
			st.listenErr = err
		}
	} else {
		st.listenErr = err
	}
	if !st.listening {
		t.log.Error(st.listenErr)
	}

	for {
		select {
		case req := <-t.requestC:
			t.handleRequest(st, req)
		case res := <-st.connectDone:
			t.handleConnectDone(st, res)
		case dest := <-st.expired:
			delete(st.connections, dest)
		case buf := <-t.readC:
			t.handleReply(st, buf)
		case <-t.closeC:
			t.shutdown(st)
			return
		}
	}
}

// beginTransaction registers an exchange under a fresh random id, refusing
// on the (astronomically unlikely) collision.
func (t *Transport) beginTransaction(st *runState, i udpRequest) (*transaction, error) {
	trx := newTransaction(i)
	if _, ok := st.transactions[trx.id]; ok {
		return nil, errors.New("transaction id collision")
	}
	st.transactions[trx.id] = trx
	return trx, nil
}

// handleRequest routes an exchange: without a socket everything fails with
// the listen error; unknown destinations open a session (connect first,
// queue behind it); known connected ones go out immediately; known
// connecting ones queue behind the handshake.
func (t *Transport) handleRequest(st *runState, req udpRequest) {
	if !st.listening {
		req.SetResponse(nil, st.listenErr)
		return
	}
	conn, ok := st.connections[req.GetDest()]
	if !ok {
		conn = newConnection(req)
		st.connections[req.GetDest()] = conn
		if trx, err := t.beginTransaction(st, conn); err != nil {
			conn.SetResponse(nil, err)
		} else {
			go resolveDestinationAndConnect(trx, req.GetDest(), st.conn, t.dnsTimeout, t.blocklist, st.connectDone, t.closeC)
		}
		return
	}
	if !conn.connectedAt.IsZero() {
		req.SetConnectionID(conn.id)
		if trx, err := t.beginTransaction(st, req); err != nil {
			req.SetResponse(nil, err)
		} else {
			go retryTransaction(trx, st.conn, conn.addr)
		}
		return
	}
	conn.requests = append(conn.requests, req)
}

// handleConnectDone settles a session handshake: failures evict the session
// and fail its queue; success arms the session expiry and launches every
// queued exchange.
func (t *Transport) handleConnectDone(st *runState, res *connectionResult) {
	conn := res.trx.request.(*connection)
	delete(st.transactions, res.trx.id)

	if res.err != nil {
		delete(st.connections, res.dest)
		for _, req := range conn.requests {
			req.SetResponse(nil, res.err)
		}
		return
	}
	conn.addr = res.addr
	conn.id = res.id
	conn.connectedAt = res.connectedAt

	go func(dest string) {
		select {
		case <-time.After(connectionIDInterval):
		case <-t.closeC:
			return
		}
		select {
		case st.expired <- dest:
		case <-t.closeC:
		}
	}(res.dest)

	for _, req := range conn.requests {
		req.SetConnectionID(conn.id)
		if trx, err := t.beginTransaction(st, req); err != nil {
			req.SetResponse(nil, err)
		} else {
			go retryTransaction(trx, st.conn, conn.addr)
		}
	}
	conn.requests = nil
}

// handleReply matches one datagram to its exchange by transaction id.
// Unknown ids are stray (a late duplicate after expiry) and only logged.
// Error replies decode the tracker's failure reason; every other reply is
// delivered raw for the request-specific parser.
func (t *Transport) handleReply(st *runState, buf []byte) {
	var header udpMessageHeader
	if err := binary.Read(bytes.NewReader(buf), binary.BigEndian, &header); err != nil {
		t.log.Error(err)
		return
	}
	trx, ok := st.transactions[header.TransactionID]
	if !ok {
		t.log.Debugln("Unexpected transaction ID:", header.TransactionID)
		return
	}
	delete(st.transactions, header.TransactionID)
	t.log.Debugln("Received response for transaction ID:", header.TransactionID)

	var err error
	if header.Action == actionError {
		var terr struct {
			FailureReason string `bencode:"failure reason"`
			RetryIn       string `bencode:"retry in"`
		}
		if derr := bencodecheck.DecodeBytes(buf[binary.Size(header):], &terr); derr != nil {
			err = tracker.ErrDecode
		} else {
			err = tracker.NewError(terr.FailureReason, terr.RetryIn)
		}
	}
	trx.request.SetResponse(buf, err)
	trx.cancel()
}

// shutdown fails everything outstanding, closes the socket and releases
// Close.
func (t *Transport) shutdown(st *runState) {
	for _, conn := range st.connections {
		for _, req := range conn.requests {
			req.SetResponse(nil, errors.New("transport closing"))
		}
	}
	for _, trx := range st.transactions {
		trx.cancel()
	}
	if st.listening {
		st.conn.Close()
	}
	close(t.doneC)
}

// udpNetwork picks the socket family: an unspecified local address opens a
// dual-stack socket so IPv4 and IPv6 trackers both work.
func udpNetwork(laddr *net.UDPAddr) string {
	switch {
	case laddr == nil || laddr.IP == nil || laddr.IP.IsUnspecified():
		return "udp"
	case laddr.IP.To4() != nil:
		return "udp4"
	default:
		return "udp6"
	}
}

// readLoop copies datagrams off the socket to the run loop. Oversized
// packets are truncated by the fixed buffer (20 bytes head + room for a
// 1000-peer compact list).
func (t *Transport) readLoop(conn net.Conn) {
	const maxNumWant = 1000
	bigBuf := make([]byte, 20+6*maxNumWant)
	for {
		n, err := conn.Read(bigBuf)
		if err != nil {
			select {
			case <-t.closeC:
			default:
				t.log.Error(err)
			}
			return
		}
		t.log.Debug("Read ", n, " bytes")
		buf := make([]byte, n)
		copy(buf, bigBuf)
		select {
		case t.readC <- buf:
		case <-t.closeC:
			return
		}
	}
}

// connectionResult carries a finished session handshake to the run loop.
type connectionResult struct {
	trx         *transaction
	dest        string
	addr        *net.UDPAddr
	id          int64
	err         error
	connectedAt time.Time
}

// resolveDestinationAndConnect resolves the tracker, runs the connect
// handshake and reports the session (or its error), unless stopped first.
func resolveDestinationAndConnect(trx *transaction, dest string, udpConn *net.UDPConn, dnsTimeout time.Duration, blocklist *blocklist.Blocklist, resultC chan *connectionResult, stopC chan struct{}) {
	res := &connectionResult{
		trx:  trx,
		dest: dest,
	}
	ip, port, err := resolver.Resolve(trx.ctx, dest, dnsTimeout, blocklist)
	if err != nil {
		res.err = err
		select {
		case resultC <- res:
		case <-stopC:
		}
		return
	}
	res.addr = &net.UDPAddr{IP: ip, Port: port}
	res.id, res.err = sendAndReceiveConnect(trx, udpConn, res.addr)
	if res.err == nil {
		res.connectedAt = time.Now()
	}
	select {
	case resultC <- res:
	case <-stopC:
	}
}

// sendAndReceiveConnect handshakes one session: retries in the background
// until the reply lands or the transaction context ends, then validates the
// connect response.
func sendAndReceiveConnect(trx *transaction, conn *net.UDPConn, addr net.Addr) (connectionID int64, err error) {
	go retryTransaction(trx, conn, addr)
	select {
	case <-trx.request.(*connection).done:
	case <-trx.ctx.Done():
		return 0, trx.ctx.Err()
	}
	data, err := trx.request.GetResponse()
	if err != nil {
		return 0, err
	}
	var response connectResponse
	if err := binary.Read(bytes.NewReader(data), binary.BigEndian, &response); err != nil {
		return 0, err
	}
	if response.Action != actionConnect {
		return 0, errors.New("invalid action in connect response")
	}
	return response.ConnectionID, nil
}

// retryTransaction retransmits until the transaction context ends (reply
// received or caller gone), backing off per BEP 15.
func retryTransaction(trx *transaction, conn *net.UDPConn, addr net.Addr) {
	var b bytes.Buffer
	_, _ = trx.request.WriteTo(&b)
	data := b.Bytes()

	ticker := backoff.NewTicker(new(udpBackOff))
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_, _ = conn.WriteTo(data, addr)
		case <-trx.ctx.Done():
			return
		}
	}
}
