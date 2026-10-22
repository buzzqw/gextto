package dht

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"expvar"
	"fmt"
	"net"
	"strconv"
	"sync/atomic"
	"time"

	bencode "github.com/jackpal/bencode-go"
)

// searchRetryPeriod re-queries a node after this silence: answers freshness
// for both pending and past queries in wasContactedRecently.
// It is read by every DHT goroutine, hence atomic.
var searchRetryPeriodNanos atomic.Int64

func init() { searchRetryPeriodNanos.Store(int64(15 * time.Second)) }

func searchRetryPeriod() time.Duration { return time.Duration(searchRetryPeriodNanos.Load()) }

// SetSearchRetryPeriod changes how long a node stays unqueried before it is
// asked again (15 s by default) and returns the previous value. It is meant
// for tests on the loopback, where every answer is immediate; it applies to
// every DHT in the process.
func SetSearchRetryPeriod(period time.Duration) time.Duration {
	return time.Duration(searchRetryPeriodNanos.Swap(int64(period)))
}

// remoteNode is one DHT contact: address, id, in-flight and answered
// queries, reachability, downloads it announced, and the anti-Sybil
// bookkeeping (noTree/countSubnet) plus its logger.
type remoteNode struct {
	address net.UDPAddr
	// addressBinaryFormat is the compact host:port form of the address.
	addressBinaryFormat string
	id                  string
	// lastQueryID cycles 0-255 as transaction ids (two-letter by spec).
	lastQueryID int
	// TODO: key by infohash instead?
	pendingQueries   map[string]*queryType // key: transaction ID
	pastQueries      map[string]*queryType // key: transaction ID
	reachable        bool
	lastResponseTime time.Time
	lastSearchTime   time.Time
	ActiveDownloads  []string // infohashes this peer announced
	// noTree keeps an address-book node out of tree slots at the subnet cap:
	// remembered without re-discovery loops.
	noTree bool
	// countedSubnet tracks inclusion in the incremental subnet counter for
	// insert/update/kill sync.
	countedSubnet bool
	log           *DebugLogger
}

// newRemoteNode registers an address with a random first transaction id.
func newRemoteNode(addr net.UDPAddr, id string, log *DebugLogger) *remoteNode {
	return &remoteNode{
		address:             addr,
		addressBinaryFormat: encodeCompactPeerAddr(addr.String()),
		lastQueryID:         newTransactionId(),
		id:                  id,
		reachable:           false,
		pendingQueries:      map[string]*queryType{},
		pastQueries:         map[string]*queryType{},
		log:                 log,
	}
}

// queryType is one outstanding exchange: kind, subject, origin, and the BEP
// 33/44 sub-flags.
type queryType struct {
	Type    string
	ih      InfoHash
	srcNode string
	// scrape marks a BEP 33 bloom-filter get_peers.
	scrape bool
	// bep44 links the query to an outbound BEP 44 lookup (0 = none).
	bep44 uint64
}

const (
	// maxUDPPacketSize bounds a datagram; full buffers warn (truncation).
	maxUDPPacketSize = 4096
	v4nodeContactLen = 26
	v6nodeContactLen = 38 // some clients seem to send multiples of 38
	nodeIdLen        = 20
)

var (
	totalSent         = expvar.NewInt("totalSent")
	totalReadBytes    = expvar.NewInt("totalReadBytes")
	totalWrittenBytes = expvar.NewInt("totalWrittenBytes")
)

// parseNodesString splits a concatenated nodes/nodes6 reply into id->address
// by family length, rejecting misaligned blobs with a log line.
func parseNodesString(nodes string, proto string, log DebugLogger) (parsed map[string]string) {
	var nodeContactLen int
	if proto == "udp4" {
		nodeContactLen = v4nodeContactLen
	} else if proto == "udp6" {
		nodeContactLen = v6nodeContactLen
	} else {
		return
	}
	parsed = make(map[string]string)
	if len(nodes)%nodeContactLen > 0 {
		log.Debugf("DHT: len(NodeString) = %d, INVALID LENGTH, should be a multiple of %d", len(nodes), nodeContactLen)
		log.Debugf("%T %#v\n", nodes, nodes)
		return
	}
	log.Debugf("DHT: len(NodeString) = %d, had %d nodes, nodeContactLen=%d\n", len(nodes), len(nodes)/nodeContactLen, nodeContactLen)
	for i := 0; i < len(nodes); i += nodeContactLen {
		id := nodes[i : i+nodeIdLen]
		address := decodeCompactPeerAddr(nodes[i+nodeIdLen : i+nodeContactLen])
		parsed[id] = address
	}
	return
}

// newQuery opens a transaction of transType on the node, cycling the id.
// Payload details stay the caller's job.
func (r *remoteNode) newQuery(transType string) (transId string) {
	(*r.log).Debugf("newQuery for %x, lastID %v", r.id, r.lastQueryID)
	r.lastQueryID = (r.lastQueryID + 1) % 256
	transId = strconv.Itoa(r.lastQueryID)
	(*r.log).Debugf("... new id %v", r.lastQueryID)
	r.pendingQueries[transId] = &queryType{Type: transType}
	return
}

// wasContactedRecently reports a recent exchange about ih: any pending or
// past query for it within the retry period. Mixed subjects keep answering
// false.
func (r *remoteNode) wasContactedRecently(ih InfoHash) bool {
	if len(r.pendingQueries) == 0 && len(r.pastQueries) == 0 {
		return false
	}
	if !r.lastResponseTime.IsZero() && time.Since(r.lastResponseTime) > searchRetryPeriod() {
		return false
	}
	for _, q := range r.pendingQueries {
		if q.ih == ih {
			return true
		}
	}
	if !r.lastSearchTime.IsZero() && time.Since(r.lastSearchTime) > searchRetryPeriod() {
		return false
	}
	for _, q := range r.pastQueries {
		if q.ih == ih {
			return true
		}
	}
	return false
}

// getPeersResponse is the decodable shape of get_peers/find_node replies,
// extended with the BEP 42 ip and BEP 33 bloom fields.
type getPeersResponse struct {
	// TODO: values can be a string on some clients (e.g. original bittorrent).
	Values []string `bencode:"values"`
	Id     string   `bencode:"id"`
	Nodes  string   `bencode:"nodes"`
	Nodes6 string   `bencode:"nodes6"`
	Token  string   `bencode:"token"`
	// IP is our external address as the responder sees it (BEP 42).
	IP string `bencode:"ip"`
	// BFpe/BFsd are the 256-byte BEP 33 scrape filters.
	BFpe string `bencode:"BFpe"`
	BFsd string `bencode:"BFsd"`
}

// answerType is the decodable shape of incoming queries.
type answerType struct {
	Id       string   `bencode:"id"`
	Target   string   `bencode:"target"`
	InfoHash InfoHash `bencode:"info_hash"`
	Port     int      `bencode:"port"`
	Token    string   `bencode:"token"`
	// ImpliedPort (BEP 5): use the packet's source port, for NATed peers
	// unaware of their external port.
	ImpliedPort int `bencode:"implied_port"`
	// Seed/Scrape/NoSeed are the BEP 33 announce/lookup flags.
	Seed   int `bencode:"seed"`
	Scrape int `bencode:"scrape"`
	NoSeed int `bencode:"noseed"`
}

// responseType is the generic inbound message envelope.
type responseType struct {
	T string           `bencode:"t"`
	Y string           `bencode:"y"`
	Q string           `bencode:"q"`
	R getPeersResponse `bencode:"r"`
	E []string         `bencode:"e"`
	A answerType       `bencode:"a"`
	// Ro is the BEP 43 read-only flag on queries.
	Ro int `bencode:"ro"`
	// IP is the BEP 42 reflected address on replies.
	IP string `bencode:"ip"`
	// Unsupported mainline extension for client identification.
	// V string(?)	"v"
}

// packetConn is the datagram socket subset the DHT needs.
type packetConn interface {
	ReadFrom(p []byte) (int, net.Addr, error)
	WriteTo(p []byte, addr net.Addr) (int, error)
	LocalAddr() net.Addr
	Close() error
}

// sendMsg bencodes a query and datagrams it to the node, metering volume.
// Marshal failures drop silently (nothing valid to send).
func sendMsg(conn packetConn, raddr net.UDPAddr, query interface{}, log DebugLogger) {
	totalSent.Add(1)
	var b bytes.Buffer
	if err := bencode.Marshal(&b, query); err != nil {
		return
	}
	if n, err := conn.WriteTo(b.Bytes(), &raddr); err != nil {
		log.Debugf("DHT: node write failed to %+v, error=%s", raddr, err)
	} else {
		totalWrittenBytes.Add(int64(n))
	}
}

// readResponse decodes one datagram into the generic envelope. The bounded
// pre-scanner runs first: bencode-go would otherwise allocate a declared
// string length (gigabytes on "d999999999:d") before noticing the short
// buffer — an unrecoverable OOM. Decoder panics convert to errors, never to
// fake successes.
func readResponse(p packetType, log DebugLogger) (response responseType, err error) {
	if verr := validBencode(p.b); verr != nil {
		log.Debugf("DHT: rejecting malformed response %q: %v", string(p.b), verr)
		return response, verr
	}
	defer func() {
		if x := recover(); x != nil {
			log.Debugf("DHT: !!! Recovering from panic() after bencode.Unmarshal %q, %v", string(p.b), x)
			err = fmt.Errorf("bencode.Unmarshal panic: %v", x)
		}
	}()
	if e2 := bencode.Unmarshal(bytes.NewBuffer(p.b), &response); e2 == nil {
		err = nil
		return
	} else {
		log.Debugf("DHT: unmarshal error, odd or partial data during UDP read? %v, err=%s", string(p.b), e2)
		return response, e2
	}
}

// queryMessage is the outbound query shape. No extra fields may be added.
type queryMessage struct {
	T string                 `bencode:"t"`
	Y string                 `bencode:"y"`
	Q string                 `bencode:"q"`
	A map[string]interface{} `bencode:"a"`
	// Ro marks read-only nodes' queries (BEP 43).
	Ro int `bencode:"ro,omitempty"`
}

// replyMessage is the outbound reply shape with the BEP 42 reflected
// requester address.
type replyMessage struct {
	T string                 `bencode:"t"`
	Y string                 `bencode:"y"`
	R map[string]interface{} `bencode:"r"`
	// IP is the requester's compact address at top level.
	IP string `bencode:"ip,omitempty"`
}

// packetType pairs a datagram with its source.
type packetType struct {
	b     []byte
	raddr net.UDPAddr
}

// listen opens the datagram socket, logging but tolerating failure (nil
// socket with the error).
func listen(addr string, listenPort int, proto string, log DebugLogger) (socket packetConn, err error) {
	log.Debugf("DHT: Listening for peers on IP: %s port: %d Protocol=%s\n", addr, listenPort, proto)
	listener, err := net.ListenPacket(proto, net.JoinHostPort(addr, strconv.Itoa(listenPort)))
	if err != nil {
		log.Debugf("DHT: Listen failed:%s\n", err)
	}
	if listener != nil {
		socket = listener
	}
	return
}

// readFromSocket pumps datagrams to the engine, recycling arena buffers on
// every path so a burst of read errors can neither drain the arena (which
// would wedge Stop) nor spin hot (10 ms backoff while unstopped). Full-size
// datagrams warn (truncation); empty ones recycle silently.
func readFromSocket(socket packetConn, conChan chan packetType, bytesArena arena, stop chan bool, log DebugLogger) {
	for {
		select {
		case <-stop:
			return
		default:
		}
		b := bytesArena.Pop()
		n, from, err := socket.ReadFrom(b)
		addr, _ := from.(*net.UDPAddr)
		if addr == nil && from != nil {
			addr, _ = net.ResolveUDPAddr("udp", from.String())
		}
		if addr == nil && err == nil {
			bytesArena.Push(b)
			continue
		}
		if err != nil {
			bytesArena.Push(b)
			log.Debugf("DHT: readResponse error:%s\n", err)
			select {
			case <-stop:
				return
			case <-time.After(10 * time.Millisecond):
			}
			continue
		}
		b = b[0:n]
		if n == maxUDPPacketSize {
			log.Debugf("DHT: Warning. Received packet with len >= %d, some data may have been discarded.\n", maxUDPPacketSize)
		}
		totalReadBytes.Add(int64(n))
		if n > 0 {
			p := packetType{b, *addr}
			select {
			case conChan <- p:
				continue
			case <-stop:
				return
			}
		}
		bytesArena.Push(b)
	}
}

// bogusId rejects malformed node ids.
func bogusId(id string) bool {
	return len(id) != 20
}

// newTransactionId draws a random query id byte, wall-clock fallback on
// randomness failure.
func newTransactionId() int {
	n, err := rand.Read(make([]byte, 1))
	if err != nil {
		return time.Now().Second()
	}
	return n
}

// InfoHash is a binary info hash.
type InfoHash string

func (i InfoHash) String() string {
	return fmt.Sprintf("%x", string(i))
}

// DecodeInfoHash parses 40 hex characters into a binary info hash.
func DecodeInfoHash(x string) (b InfoHash, err error) {
	var h []byte
	h, err = hex.DecodeString(x)
	if len(h) != 20 {
		return "", fmt.Errorf("DecodeInfoHash: expected InfoHash len=20, got %d", len(h))
	}
	return InfoHash(h), err
}

// DecodePeerAddress renders a binary host:port contact human-readable
// ("abcdef" to 97.98.99.100:25958).
func DecodePeerAddress(x string) string {
	return decodeCompactPeerAddr(x)
}
