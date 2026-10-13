// Package trackertest serves minimal in-memory trackers for the engine's own
// tests: just enough HTTP (BEP 3) and UDP (BEP 15) announce protocol to run
// the tracker clients and end-to-end downloads, with no external tracker.
package trackertest

import (
	"encoding/binary"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
	"github.com/zeebo/bencode"
)

// announceInterval is the interval advertised in announce replies.
const announceInterval = time.Minute

// peerStore remembers one swarm per info hash, keyed by compact peer so a
// re-announce refreshes the same entry. Mutex-guarded for the serving
// goroutines.
type peerStore struct {
	mu    sync.Mutex
	peers map[[20]byte]map[tracker.CompactPeer]struct{}
}

func newPeerStore() *peerStore {
	return &peerStore{peers: make(map[[20]byte]map[tracker.CompactPeer]struct{})}
}

// announce upserts the announcer (or removes it on stopped) and returns up
// to numWant other swarm members, never the announcer itself. numWant <= 0
// means uncapped.
func (s *peerStore) announce(infoHash [20]byte, self tracker.CompactPeer, stopped bool, numWant int) []tracker.CompactPeer {
	s.mu.Lock()
	defer s.mu.Unlock()

	swarm := s.peers[infoHash]
	if swarm == nil {
		swarm = make(map[tracker.CompactPeer]struct{})
		s.peers[infoHash] = swarm
	}
	if stopped {
		delete(swarm, self)
	} else {
		swarm[self] = struct{}{}
	}
	var result []tracker.CompactPeer
	for p := range swarm {
		if p == self {
			continue
		}
		if numWant > 0 && len(result) >= numWant {
			break
		}
		result = append(result, p)
	}
	return result
}

// compactPeer folds an address into compact form; false means non-IPv4,
// which the callers reject.
func compactPeer(ip net.IP, port int) (tracker.CompactPeer, bool) {
	v4 := ip.To4()
	if v4 == nil {
		return tracker.CompactPeer{}, false
	}
	return tracker.NewCompactPeer(&net.TCPAddr{IP: v4, Port: port}), true
}

// encodeCompactPeers concatenates compact peers into one blob.
func encodeCompactPeers(peers []tracker.CompactPeer) []byte {
	b := make([]byte, 0, len(peers)*6)
	for _, p := range peers {
		encoded, _ := p.MarshalBinary()
		b = append(b, encoded...)
	}
	return b
}

// HTTPTracker is a BEP 3 test tracker serving /announce. Stop it with Close.
type HTTPTracker struct {
	store  *peerStore
	server *http.Server
	doneC  chan struct{}
}

// NewHTTP listens on addr (e.g. "127.0.0.1:0" for an ephemeral port).
func NewHTTP(addr string) (*HTTPTracker, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	t := &HTTPTracker{
		store: newPeerStore(),
		doneC: make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/announce", t.handleAnnounce)
	t.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
	}
	go func() {
		_ = t.server.Serve(l)
		close(t.doneC)
	}()
	return t, nil
}

// Close shuts the tracker down and waits for the serving goroutine.
func (t *HTTPTracker) Close() error {
	err := t.server.Close()
	<-t.doneC
	return err
}

type httpAnnounceResponse struct {
	Interval int    `bencode:"interval"`
	Peers    []byte `bencode:"peers"`
}

func (t *HTTPTracker) handleAnnounce(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	var infoHash [20]byte
	copy(infoHash[:], q.Get("info_hash"))

	port64, err := strconv.ParseUint(q.Get("port"), 10, 16)
	if err != nil {
		http.Error(w, "invalid port", http.StatusBadRequest)
		return
	}
	numWant, _ := strconv.Atoi(q.Get("numwant"))
	stopped := q.Get("event") == "stopped"

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	self, ok := compactPeer(net.ParseIP(host), int(port64))
	if !ok {
		http.Error(w, "non-IPv4 peer", http.StatusBadRequest)
		return
	}
	peers := t.store.announce(infoHash, self, stopped, numWant)
	w.Header().Set("Content-Type", "text/plain")
	_ = bencode.NewEncoder(w).Encode(httpAnnounceResponse{
		Interval: int(announceInterval / time.Second),
		Peers:    encodeCompactPeers(peers),
	})
}

// UDP tracker protocol numbers (BEP 15).
const (
	udpProtocolID  = 0x41727101980
	actionConnect  = 0
	actionAnnounce = 1
	actionError    = 3
	eventStopped   = 3
	// udpConnectionID is the arbitrary session id handed out in connect
	// replies and echoed in announces.
	udpConnectionID = 0x27101980
)

// UDPTracker is a BEP 15 test tracker. Stop it with Close.
type UDPTracker struct {
	store *peerStore
	conn  *net.UDPConn
	doneC chan struct{}
}

// NewUDP listens on addr (e.g. "127.0.0.1:0" for an ephemeral port).
func NewUDP(addr string) (*UDPTracker, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}
	t := &UDPTracker{
		store: newPeerStore(),
		conn:  conn,
		doneC: make(chan struct{}),
	}
	go t.serve()
	return t, nil
}

// Close shuts the tracker down and waits for the serving goroutine.
func (t *UDPTracker) Close() error {
	err := t.conn.Close()
	<-t.doneC
	return err
}

func (t *UDPTracker) serve() {
	defer close(t.doneC)
	buf := make([]byte, 2048)
	for {
		n, addr, err := t.conn.ReadFromUDP(buf)
		if err != nil {
			return // connection closed
		}
		t.handle(buf[:n], addr)
	}
}

// handle dispatches one datagram: connection id (8), action (4),
// transaction id (4), then the action body.
func (t *UDPTracker) handle(req []byte, addr *net.UDPAddr) {
	if len(req) < 16 {
		return
	}
	action := binary.BigEndian.Uint32(req[8:12])
	transactionID := binary.BigEndian.Uint32(req[12:16])
	switch action {
	case actionConnect:
		if binary.BigEndian.Uint64(req[0:8]) != udpProtocolID {
			t.writeError(addr, transactionID, "invalid protocol id")
			return
		}
		resp := make([]byte, 16)
		binary.BigEndian.PutUint32(resp[0:4], actionConnect)
		binary.BigEndian.PutUint32(resp[4:8], transactionID)
		binary.BigEndian.PutUint64(resp[8:16], udpConnectionID)
		_, _ = t.conn.WriteToUDP(resp, addr)
	case actionAnnounce:
		t.handleAnnounce(req, addr, transactionID)
	default:
		t.writeError(addr, transactionID, "unknown action")
	}
}

// handleAnnounce serves a 98-byte announce body (info_hash at [16:36],
// event at [80:84], num_want at [92:96], port at [96:98]). The request IP
// field is zero in engine requests, so the source IP identifies the peer.
func (t *UDPTracker) handleAnnounce(req []byte, addr *net.UDPAddr, transactionID uint32) {
	if len(req) < 98 {
		t.writeError(addr, transactionID, "short announce")
		return
	}
	var infoHash [20]byte
	copy(infoHash[:], req[16:36])
	event := binary.BigEndian.Uint32(req[80:84])
	numWant := int(binary.BigEndian.Uint32(req[92:96]))
	port := int(binary.BigEndian.Uint16(req[96:98]))

	self, ok := compactPeer(addr.IP, port)
	if !ok {
		t.writeError(addr, transactionID, "non-IPv4 peer")
		return
	}
	peers := t.store.announce(infoHash, self, event == eventStopped, numWant)
	compact := encodeCompactPeers(peers)
	resp := make([]byte, 20+len(compact))
	binary.BigEndian.PutUint32(resp[0:4], actionAnnounce)
	binary.BigEndian.PutUint32(resp[4:8], transactionID)
	binary.BigEndian.PutUint32(resp[8:12], uint32(announceInterval/time.Second))
	binary.BigEndian.PutUint32(resp[12:16], 0)                  // leechers
	binary.BigEndian.PutUint32(resp[16:20], uint32(len(peers))) // seeders
	copy(resp[20:], compact)
	_, _ = t.conn.WriteToUDP(resp, addr)
}

func (t *UDPTracker) writeError(addr *net.UDPAddr, transactionID uint32, msg string) {
	resp := make([]byte, 8, 8+len(msg))
	binary.BigEndian.PutUint32(resp[0:4], actionError)
	binary.BigEndian.PutUint32(resp[4:8], transactionID)
	resp = append(resp, msg...)
	_, _ = t.conn.WriteToUDP(resp, addr)
}
