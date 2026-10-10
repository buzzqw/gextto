package dht

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"expvar"
	"fmt"
	"net"
	"strconv"
	"time"

	bencode "github.com/jackpal/bencode-go"
	"github.com/nictuku/nettools"
)

// Search a node again after some time.
var searchRetryPeriod = 15 * time.Second

// Owned by the DHT engine.
type remoteNode struct {
	address net.UDPAddr
	// addressDotFormatted contains a binary representation of the node's host:port address.
	addressBinaryFormat string
	id                  string
	// lastQueryID should be incremented after consumed. Based on the
	// protocol, it would be two letters, but I'm using 0-255, although
	// treated as string.
	lastQueryID int
	// TODO: key by infohash instead?
	pendingQueries   map[string]*queryType // key: transaction ID
	pastQueries      map[string]*queryType // key: transaction ID
	reachable        bool
	lastResponseTime time.Time
	lastSearchTime   time.Time
	ActiveDownloads  []string // List of infohashes we know this peer is downloading.
	log              *DebugLogger
}

func newRemoteNode(addr net.UDPAddr, id string, log *DebugLogger) *remoteNode {
	return &remoteNode{
		address:             addr,
		addressBinaryFormat: nettools.DottedPortToBinary(addr.String()),
		lastQueryID:         newTransactionId(),
		id:                  id,
		reachable:           false,
		pendingQueries:      map[string]*queryType{},
		pastQueries:         map[string]*queryType{},
		log:                 log,
	}
}

type queryType struct {
	Type    string
	ih      InfoHash
	srcNode string
}

const (
	// Once in a while I get a few bigger ones, but meh.
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

// The 'nodes' response is a string with fixed length contacts concatenated arbitrarily.
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
	} else {
		log.Debugf("DHT: len(NodeString) = %d, had %d nodes, nodeContactLen=%d\n", len(nodes), len(nodes)/nodeContactLen, nodeContactLen)
	}
	for i := 0; i < len(nodes); i += nodeContactLen {
		id := nodes[i : i+nodeIdLen]
		address := nettools.BinaryToDottedPort(nodes[i+nodeIdLen : i+nodeContactLen])
		parsed[id] = address
	}
	return

}

// newQuery creates a new transaction id and adds an entry to r.pendingQueries.
// It does not set any extra information to the transaction information, so the
// caller must take care of that.
func (r *remoteNode) newQuery(transType string) (transId string) {
	(*r.log).Debugf("newQuery for %x, lastID %v", r.id, r.lastQueryID)
	r.lastQueryID = (r.lastQueryID + 1) % 256
	transId = strconv.Itoa(r.lastQueryID)
	(*r.log).Debugf("... new id %v", r.lastQueryID)
	r.pendingQueries[transId] = &queryType{Type: transType}
	return
}

// wasContactedRecently returns true if a node was contacted recently _and_
// one of the recent queries (not necessarily the last) was about the ih. If
// the ih is different at each time, it will keep returning false.
func (r *remoteNode) wasContactedRecently(ih InfoHash) bool {
	if len(r.pendingQueries) == 0 && len(r.pastQueries) == 0 {
		return false
	}
	if !r.lastResponseTime.IsZero() && time.Since(r.lastResponseTime) > searchRetryPeriod {
		return false
	}
	for _, q := range r.pendingQueries {
		if q.ih == ih {
			return true
		}
	}
	if !r.lastSearchTime.IsZero() && time.Since(r.lastSearchTime) > searchRetryPeriod {
		return false
	}
	for _, q := range r.pastQueries {
		if q.ih == ih {
			return true
		}
	}
	return false
}

type getPeersResponse struct {
	// TODO: argh, values can be a string depending on the client (e.g: original bittorrent).
	Values []string `bencode:"values"`
	Id     string   `bencode:"id"`
	Nodes  string   `bencode:"nodes"`
	Nodes6 string   `bencode:"nodes6"`
	Token  string   `bencode:"token"`
}

type answerType struct {
	Id       string   `bencode:"id"`
	Target   string   `bencode:"target"`
	InfoHash InfoHash `bencode:"info_hash"` // should probably be a string.
	Port     int      `bencode:"port"`
	Token    string   `bencode:"token"`
}

// Generic stuff we read from the wire, not knowing what it is. This is as generic as can be.
type responseType struct {
	T string           `bencode:"t"`
	Y string           `bencode:"y"`
	Q string           `bencode:"q"`
	R getPeersResponse `bencode:"r"`
	E []string         `bencode:"e"`
	A answerType       `bencode:"a"`
	// Unsupported mainline extension for client identification.
	// V string(?)	"v"
}

// sendMsg bencodes the data in 'query' and sends it to the remote node.
// packetConn is the subset of net.PacketConn the DHT uses.
type packetConn interface {
	ReadFrom(p []byte) (int, net.Addr, error)
	WriteTo(p []byte, addr net.Addr) (int, error)
	LocalAddr() net.Addr
	Close() error
}

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
	return
}

// Read responses from bencode-speaking nodes. Return the appropriate data structure.
func readResponse(p packetType, log DebugLogger) (response responseType, err error) {
	// The calls to bencode.Unmarshal() can be fragile.
	defer func() {
		if x := recover(); x != nil {
			log.Debugf("DHT: !!! Recovering from panic() after bencode.Unmarshal %q, %v", string(p.b), x)
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

// Message to be sent out in the wire. Must not have any extra fields.
type queryMessage struct {
	T string                 `bencode:"t"`
	Y string                 `bencode:"y"`
	Q string                 `bencode:"q"`
	A map[string]interface{} `bencode:"a"`
}

type replyMessage struct {
	T string                 `bencode:"t"`
	Y string                 `bencode:"y"`
	R map[string]interface{} `bencode:"r"`
}

type packetType struct {
	b     []byte
	raddr net.UDPAddr
}

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

// Read from UDP socket, writes slice of byte into channel.
func readFromSocket(socket packetConn, conChan chan packetType, bytesArena arena, stop chan bool, log DebugLogger) {
	for {
		// Check stop before blocking on a free buffer: the arena holds only a
		// few buffers, so a burst of read errors could drain it and block Pop
		// forever, leaving Stop() unable to join this goroutine.
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
			// Return the buffer so the arena never drains, then stop if asked.
			// A short wait avoids a tight loop if the read error is persistent
			// and stop has not been closed yet.
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

func bogusId(id string) bool {
	return len(id) != 20
}

func newTransactionId() int {
	n, err := rand.Read(make([]byte, 1))
	if err != nil {
		return time.Now().Second()
	}
	return n
}

type InfoHash string

func (i InfoHash) String() string {
	return fmt.Sprintf("%x", string(i))
}

// DecodeInfoHash transforms a hex-encoded 20-characters string to a binary
// infohash.
func DecodeInfoHash(x string) (b InfoHash, err error) {
	var h []byte
	h, err = hex.DecodeString(x)
	if len(h) != 20 {
		return "", fmt.Errorf("DecodeInfoHash: expected InfoHash len=20, got %d", len(h))
	}
	return InfoHash(h), err
}

// DecodePeerAddress transforms the binary-encoded host:port address into a
// human-readable format. So, "abcdef" becomes 97.98.99.100:25958.
func DecodePeerAddress(x string) string {
	return nettools.BinaryToDottedPort(x)
}
