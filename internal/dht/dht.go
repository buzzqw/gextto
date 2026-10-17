// DHT node for tracker-less peer discovery: Kademlia (BEP 5) with IPv6
// (BEP 32), swarm scrape (BEP 33), secure node IDs (BEP 42), read-only mode
// (BEP 43) and the distributed key/value store (BEP 44).
// Status: Supports all DHT operations from the specification.

package dht

// Message types: query, response, error.
//
// RPCs:
//	ping: reachability probe, feeds the routing table.
//	find_node: table upkeep, run as the table thins or every X minutes.
//	get_peers: the real lookup: iterative peer search per info hash.
//	announce_peer: publish ourselves as a peer for an info hash.
//
// Reference: http://www.bittorrent.org/beps/bep_0005.html
//

import (
	"crypto/rand"
	"crypto/sha1"
	"expvar"
	"flag"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config tunes the node. NewConfig fills the defaults; later mutations do
// not apply. Field meanings double as flag help (see RegisterFlags).
type Config struct {
	// Address binds the socket; blank auto-selects.
	Address string
	// Port binds the socket; zero auto-selects.
	Port int
	// NumTargetPeers sizes each info-hash peer search. Default value: 5.
	NumTargetPeers int
	// DHTRouters bootstraps the network, comma-separated.
	DHTRouters string
	// MaxNodes caps the in-memory routing table. Default value: 100.
	MaxNodes int
	// MaxNodesPerSubnet optionally caps how many routing-table nodes may share
	// the same /24 (IPv4) or /48 (IPv6): a cheap anti-Sybil guard against many
	// node IDs from a few prefixes. 0 (default) disables it. Turn it on only
	// after measuring with `make measure-dht`: peers behind CGNAT can share a
	// prefix, so a too-strict cap drops legitimate nodes.
	MaxNodesPerSubnet int
	// CleanupPeriod paces reachability probes. Default value: 15 min.
	CleanupPeriod time.Duration
	// SaveRoutingTable persists the table across restarts with periodic
	// snapshots. Default value: true.
	SaveRoutingTable bool
	// SavePeriod paces the snapshots. Default value: 5 minutes.
	SavePeriod time.Duration
	// RateLimit caps processed packets per second; negative disables.
	// Default value: 100.
	RateLimit int64
	// MaxInfoHashes caps tracked info hashes (~25 MB with the defaults);
	// more helps the network. Default value: 2048.
	MaxInfoHashes int
	// MaxInfoHashPeers caps peers per info hash (6 bytes each). Default value: 256.
	MaxInfoHashPeers int
	// ClientPerMinuteLimit drops over-talking clients past this packet rate.
	// Default value: 50.
	ClientPerMinuteLimit int
	// ThrottlerTrackedClients sizes the client-throttle LRU. Default value: 1000.
	ThrottlerTrackedClients int64
	// UDPProto selects the socket family: udp4 (default), udp6, or udp (dual).
	UDPProto string
	// PacketConn, when set, is used instead of opening a UDP socket: a uTP
	// socket shares its port with the DHT this way. The DHT does not close it.
	PacketConn net.PacketConn
	// PublicIP, when set, is the external IP others see (BEP 42): the node ID is
	// derived from it, so it cannot be spoofed and enforcing peers do not ignore
	// us. Left nil, the ID is random until an external IP is learned from the
	// network (see the BEP 42 "ip" field handling).
	PublicIP net.IP
	// ReadOnly runs the node in BEP 43 read-only mode: it does not answer
	// queries and asks not to be added to other nodes' routing tables. Useful
	// behind a restrictive NAT or to reduce traffic.
	ReadOnly bool
}

// Creates a *Config populated with default values.
func NewConfig() *Config {
	return &Config{
		Address:                 "",
		Port:                    0, // Picks a random port.
		NumTargetPeers:          5,
		DHTRouters:              "router.magnets.im:6881,router.bittorrent.com:6881,dht.transmissionbt.com:6881",
		MaxNodes:                500,
		CleanupPeriod:           15 * time.Minute,
		SaveRoutingTable:        true,
		SavePeriod:              5 * time.Minute,
		RateLimit:               100,
		MaxInfoHashes:           2048,
		MaxInfoHashPeers:        256,
		ClientPerMinuteLimit:    50,
		ThrottlerTrackedClients: 1000,
		UDPProto:                "udp4",
	}
}

var DefaultConfig = NewConfig()

// Registers Config fields as command line flags.  If c is nil, DefaultConfig
// is used.
func RegisterFlags(c *Config) {
	if c == nil {
		c = DefaultConfig
	}
	flag.StringVar(&c.DHTRouters, "routers", c.DHTRouters,
		"Comma separated addresses of DHT routers used to bootstrap the DHT network.")
	flag.IntVar(&c.MaxNodes, "maxNodes", c.MaxNodes,
		"Maximum number of nodes to store in the routing table, in memory. This is the primary configuration for how noisy or aggressive this node should be. When the node starts, it will try to reach d.config.MaxNodes/2 as quick as possible, to form a healthy routing table.")
	flag.DurationVar(&c.CleanupPeriod, "cleanupPeriod", c.CleanupPeriod,
		"How often to ping nodes in the network to see if they are reachable.")
	flag.DurationVar(&c.SavePeriod, "savePeriod", c.SavePeriod,
		"How often to save the routing table to disk.")
	flag.Int64Var(&c.RateLimit, "rateLimit", c.RateLimit,
		"Maximum packets per second to be processed. Beyond this limit they are silently dropped. Set to -1 to disable rate limiting.")
}

const (
	// Try to ensure that at least these many nodes are in the routing table.
	minNodes           = 16
	secretRotatePeriod = 5 * time.Minute
)

// DHT should be created by New(). It provides DHT features to a torrent
// client, such as finding new peers for torrent downloads without requiring a
// tracker.
// DHT is the node. Build it with New (nil takes DefaultConfig), run it
// with Start, end it with Stop.
type DHT struct {
	// PeersRequestResults carries found peers per info hash as binary
	// contacts (see DecodePeerAddress), buffered depth 1.
	PeersRequestResults chan map[InfoHash][]string
	// Logger hooks RPC observations for the embedding client. Renaming it
	// Hooks would break users, so the name stays.
	Logger Logger
	// DebugLogger sinks diagnostics; nullLogger silences them.
	DebugLogger DebugLogger

	nodeId                 string
	config                 Config
	routingTable           *routingTable
	peerStore              *peerStore
	conn                   packetConn
	sharedConn             bool
	exploredNeighborhood   bool
	remoteNodeAcquaintance chan string
	// The bootstrap routers resolved off the loop (routers.go): the loop
	// owns these fields, the resolution comes back on routersResolved.
	routerAddrs      []string
	routersAt        time.Time
	routersRetryAt   time.Time
	routersResolving bool
	routersResolved  chan []string
	bootstrapPending bool
	// lookupIP resolves host names (nil = the system resolver); set before
	// Start, never changed afterwards.
	lookupIP       ipLookup
	peersRequest   chan ihReq
	nodesRequest   chan ihReq
	pingRequest    chan *remoteNode
	portRequest    chan int
	removeInfoHash chan InfoHash
	stop           chan bool
	wg             sync.WaitGroup
	clientThrottle *clientThrottle
	store          *dhtStore
	tokenSecrets   []string // current + previous announce-token secret

	// publicIP is the external IP we are reached at (BEP 42), once known.
	publicIP net.IP
	// items holds the BEP 44 items this node stores.
	items *itemStore
	// BEP 33 scrape client state (run-loop owned).
	scrapeRequest chan scrapeRequest
	scrapeDone    chan InfoHash
	scrapes       map[InfoHash]*scrapeCollector
	// BEP 44 outbound client state (run-loop owned). bep44s is keyed by the
	// lookup id; bep44Seq hands out ids.
	bep44Request chan bep44LookupRequest
	bep44Done    chan uint64
	bep44s       map[uint64]*bep44Lookup
	bep44Seq     uint64
	// confirm tracks which nodes reported each peer (multi-path confirmation
	// observability, run-loop owned).
	confirm map[peerConfirmKey]*peerConfirmEntry
}

// New creates a DHT node. If config is nil, DefaultConfig will be used.
// Changing the config after calling this function has no effect.
//
// This method replaces NewDHTNode.
func New(config *Config) (node *DHT, err error) {
	if config == nil {
		config = DefaultConfig
	}
	// Copy to avoid changes.
	cfg := *config
	node = makeNode(cfg)
	c := openStore(cfg.Port, cfg.SaveRoutingTable)
	node.store = c
	if err := ensureNodeID(node, c); err != nil {
		return nil, err
	}
	secureNodeID(node, c, cfg.PublicIP)
	node.publicIP = cfg.PublicIP
	// The types don't match because JSON marshalling needs []byte.
	node.nodeId = string(c.Id)
	node.routingTable.nodeId = node.nodeId

	// Seed the stored remotes before the engine reads them.
	node.wg.Add(1)
	go func() {
		defer node.wg.Done()
		for addr := range c.Remotes {
			node.addNodeWait(addr)
		}
	}()
	return
}

// makeNode wires a DHT with its stores, channels, throttle and sub-clients.
func makeNode(cfg Config) *DHT {
	node := &DHT{
		config:               cfg,
		peerStore:            newPeerStore(cfg.MaxInfoHashes, cfg.MaxInfoHashPeers),
		PeersRequestResults:  make(chan map[InfoHash][]string, 1),
		stop:                 make(chan bool),
		DebugLogger:          &nullLogger{},
		exploredNeighborhood: false,
		// Buffered against senders that must not block.
		remoteNodeAcquaintance: make(chan string, 100),
		routersResolved:        make(chan []string, 1),
		peersRequest:           make(chan ihReq, 100),
		nodesRequest:           make(chan ihReq, 100),
		pingRequest:            make(chan *remoteNode),
		portRequest:            make(chan int),
		removeInfoHash:         make(chan InfoHash),
		clientThrottle:         newClientThrottle(cfg.ClientPerMinuteLimit, cfg.ThrottlerTrackedClients),
	}
	routingTable := newRoutingTable(&node.DebugLogger)
	routingTable.maxPerSubnet = cfg.MaxNodesPerSubnet
	node.routingTable = routingTable
	node.tokenSecrets = []string{node.newTokenSecret(), node.newTokenSecret()}
	node.items = newItemStore()
	node.scrapeRequest = make(chan scrapeRequest, 4)
	node.scrapeDone = make(chan InfoHash, 4)
	node.scrapes = make(map[InfoHash]*scrapeCollector)
	node.bep44Request = make(chan bep44LookupRequest, 4)
	node.bep44Done = make(chan uint64, 4)
	node.bep44s = make(map[uint64]*bep44Lookup)
	node.confirm = make(map[peerConfirmKey]*peerConfirmEntry)
	return node
}

// ensureNodeID mints and persists a random id when the store holds none.
func ensureNodeID(node *DHT, c *dhtStore) error {
	if len(c.Id) == 20 {
		return nil
	}
	var err error
	c.Id, err = randNodeId()
	if err != nil {
		return err
	}
	node.DebugLogger.Debugf("Using a new random node ID: %x %d", c.Id, len(c.Id))
	saveStore(*c)
	return nil
}

// secureNodeID replaces a spoofable id with a BEP 42 derivation of the
// configured external IP, so enforcing peers do not ignore us.
func secureNodeID(node *DHT, c *dhtStore, ip net.IP) {
	if ip == nil {
		return
	}
	var idArr [20]byte
	copy(idArr[:], c.Id)
	if !NodeIDSecure(idArr, ip) {
		c.Id = generateSecureNodeID(ip)
		saveStore(*c)
		node.DebugLogger.Infof("DHT: using a BEP 42 secure node ID for external IP %s", ip)
	}
}

func (d *DHT) newTokenSecret() string {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		// This would return a string with up to 5 null chars.
		d.DebugLogger.Errorf("DHT: failed to generate random newTokenSecret: %v", err)
	}
	return string(b)
}

// Logger hooks RPC observations for the embedding client.
type Logger interface {
	GetPeers(addr net.UDPAddr, queryID string, infoHash InfoHash)
}

type ihReq struct {
	ih      InfoHash
	options announceOptions
}

type announceOptions struct {
	announce bool
	port     int
}

// PeersRequest searches peers for ih, announcing as a downloader when asked
// (a pure router sets announce false). RemoveInfoHash withdraws it.
func (d *DHT) PeersRequest(ih string, announce bool) {
	d.PeersRequestPort(ih, announce, d.config.Port)
}

// PeersRequestPort is PeersRequest with the announce port spelled out.
func (d *DHT) PeersRequestPort(ih string, announce bool, port int) {
	d.peersRequest <- ihReq{InfoHash(ih), announceOptions{announce, port}}
	d.DebugLogger.Infof("DHT: torrent client asking more peers for %x.", ih)
}

// RemoveInfoHash withdraws a finished info hash from the local store.
func (d *DHT) RemoveInfoHash(ih string) {
	d.removeInfoHash <- InfoHash(ih)
	d.DebugLogger.Infof("DHT: torrent client removes info hash %x.", ih)
}

// Stop halts the node and waits for the loops to exit.
func (d *DHT) Stop() {
	close(d.stop)
	d.wg.Wait()
}

// Port reports the bound port (handy with port 0 auto-assignment).
func (d *DHT) Port() int {
	return <-d.portRequest
}

// getPeers asks the closest nodes for infoHash's peers, falling back to
// the bootstrap routers on an empty table.
func (d *DHT) getPeers(infoHash InfoHash) {
	closest := d.routingTable.lookupFiltered(infoHash)
	if len(closest) == 0 {
		for _, s := range d.routers() {
			if r, e := d.routingTable.getOrCreateNode("", s, d.config.UDPProto); e == nil {
				d.getPeersFrom(r, infoHash)
			}
		}
	}
	for _, r := range closest {
		d.getPeersFrom(r, infoHash)
	}
}

// findNode searches the table (or the routers, when empty) for id.
func (d *DHT) findNode(id string) {
	ih := InfoHash(id)
	closest := d.routingTable.lookupFiltered(ih)
	if len(closest) == 0 {
		for _, s := range d.routers() {
			if r, e := d.routingTable.getOrCreateNode("", s, d.config.UDPProto); e == nil {
				d.findNodeFrom(r, id)
			}
		}
	}
	for _, r := range closest {
		d.findNodeFrom(r, id)
	}
}

// Start listens and runs the main loop in the background, returning the
// listen error if any. Stop ends it.
func (d *DHT) Start() (err error) {
	if err = d.initSocket(); err == nil {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			d.loop()
		}()
	}
	return err
}

// Run listens and runs the main loop inline until Stop.
// DEPRECATED - use Start instead.
func (d *DHT) Run() error {
	d.DebugLogger.Infof("dht.Run() is deprecated, use dht.Start() instead")
	if err := d.initSocket(); err != nil {
		return err
	}
	d.loop()
	return nil
}

// initSocket binds the socket (shared or fresh) and records the bound port.
func (d *DHT) initSocket() (err error) {
	if d.config.PacketConn != nil {
		d.conn = d.config.PacketConn
		d.sharedConn = true
	} else {
		d.conn, err = listen(d.config.Address, d.config.Port, d.config.UDPProto, d.DebugLogger)
	}
	if err != nil {
		return err
	}
	d.config.Port = d.conn.LocalAddr().(*net.UDPAddr).Port
	return nil
}

// bootstrap pings the configured routers, looks us up through them, then
// looks us up table-wide and pulls peers for active downloads. Routers given
// by name are resolved in the background first: the router part then runs
// when they arrive (routersArrived).
func (d *DHT) bootstrap() {
	if len(d.routers()) == 0 {
		d.bootstrapPending = true
	} else {
		d.bootstrapRouters()
	}
	d.findNode(d.nodeId)
	d.getMorePeers(nil)
}

// bootstrapRouters pings the resolved routers and looks us up through them.
func (d *DHT) bootstrapRouters() {
	for _, s := range d.routerAddrs {
		d.ping(s)
		if r, e := d.routingTable.getOrCreateNode("", s, d.config.UDPProto); e == nil {
			d.findNodeFrom(r, d.nodeId)
		}
	}
}

// loop is the main working section of dht.
// It bootstraps a routing table, if necessary,
// and listens for incoming DHT requests until d.Stop()
// is called from another go routine.
// rateLimiter arms the inbound packet token bucket: disabled below zero,
// floored at 10 above it (smaller rates quantize badly at 10 Hz refill).
func (d *DHT) rateLimiter() (<-chan time.Time, int64) {
	if d.config.RateLimit < 0 {
		d.DebugLogger.Infof("rate limiting disabled")
		return nil, d.config.RateLimit
	}
	if d.config.RateLimit > 0 && d.config.RateLimit < 10 {
		d.config.RateLimit = 10
	}
	return time.Tick(time.Second / 10), d.config.RateLimit
}

// loop pumps the node until Stop: one reader goroutine feeds datagrams to
// the run loop below (a 3-buffer arena suffices while reading stays single;
// parallelizing either side must bump it). Own sockets close here; shared
// ones belong to their owner.
func (d *DHT) loop() {
	defer func() {
		if !d.sharedConn {
			d.conn.Close()
		}
	}()

	bytesArena := newArena(maxUDPPacketSize, 3)
	socketChan := make(chan packetType)
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		readFromSocket(d.conn, socketChan, bytesArena, d.stop, d.DebugLogger)
	}()

	d.bootstrap()

	cleanupTicker := time.Tick(d.config.CleanupPeriod)
	secretRotateTicker := time.Tick(secretRotatePeriod)

	saveTicker := make(<-chan time.Time)
	if d.store != nil {
		saveTicker = time.Tick(d.config.SavePeriod)
	}

	fillTokenBucket, tokenBucket := d.rateLimiter()
	d.DebugLogger.Infof("DHT: Starting DHT node %x on port %d.", d.nodeId, d.config.Port)

	for {
		select {
		case <-d.stop:
			d.DebugLogger.Infof("DHT exiting.")
			d.clientThrottle.Stop()
			return
		case addr := <-d.remoteNodeAcquaintance:
			d.helloFromPeer(addr)
		case addrs := <-d.routersResolved:
			d.routersArrived(addrs)
		case req := <-d.peersRequest:
			d.servePeersRequests(req)

		case ih := <-d.removeInfoHash:
			d.peerStore.removeLocalDownload(ih)
		case req := <-d.scrapeRequest:
			d.startScrape(req)
		case ih := <-d.scrapeDone:
			d.finishScrape(ih)
		case req := <-d.bep44Request:
			d.startBep44(req)
		case id := <-d.bep44Done:
			d.finishBep44(id)
		case req := <-d.nodesRequest:
			d.serveNodesRequests(req)

		case p := <-socketChan:
			totalRecv.Add(1)
			if d.config.RateLimit > 0 && tokenBucket <= 0 {
				// TODO In the future it might be better to avoid dropping things like ping replies.
				totalDroppedPackets.Add(1)
			} else {
				d.processPacket(p)
				if d.config.RateLimit > 0 {
					tokenBucket--
				}
			}
			bytesArena.Push(p.b)

		case <-fillTokenBucket:
			if tokenBucket < d.config.RateLimit {
				tokenBucket += d.config.RateLimit / 10
			}
		case <-cleanupTicker:
			d.runCleanupTick()
		case node := <-d.pingRequest:
			d.pingNode(node)
		case <-secretRotateTicker:
			d.tokenSecrets = []string{d.newTokenSecret(), d.tokenSecrets[0]}
		case d.portRequest <- d.config.Port:
			continue
		case <-saveTicker:
			d.persistRoutingTable()
		}
	}
}

// servePeersRequests drains the queued peer searches, de-dupes them by info
// hash, registers local downloads, and launches one lookup each. A lookup
// may already hold peers without seeds — it still runs.
func (d *DHT) servePeersRequests(first ihReq) {
	m := map[InfoHash]announceOptions{first.ih: first.options}
	for {
		select {
		case req := <-d.peersRequest:
			m[req.ih] = req.options
		default:
			goto drained
		}
	}
drained:
	for ih, options := range m {
		if options.announce {
			d.peerStore.addLocalDownload(ih, options.port)
		}
		d.getPeers(ih)
	}
}

// serveNodesRequests drains queued find-node targets, de-duped, and
// launches one search each.
func (d *DHT) serveNodesRequests(first ihReq) {
	m := map[InfoHash]bool{first.ih: true}
	for {
		select {
		case req := <-d.nodesRequest:
			m[req.ih] = true
		default:
			goto drained
		}
	}
drained:
	for ih := range m {
		d.findNode(string(ih))
	}
}

// runCleanupTick publishes the table metrics, expires peer confirmations,
// pings the stale set off-loop, and re-bootstraps a thin table.
func (d *DHT) runCleanupTick() {
	ms, ins := d.tableMetrics()
	subnetShareMax.Store(int64(ms))
	nodesInsecureID.Store(int64(ins))
	subnetCapHintValue.Store(int64(subnetCapHint(ms, d.routingTable.maxPerSubnet)))
	d.expirePeerConfirm(time.Now())
	needPing := d.routingTable.cleanup(d.config.CleanupPeriod, d.peerStore)
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		pingSlowly(d.pingRequest, needPing, d.config.CleanupPeriod, d.stop)
	}()
	if d.needMoreNodes() {
		d.bootstrap()
	}
}

// persistRoutingTable snapshots reachable nodes to disk once the table is
// worth keeping.
func (d *DHT) persistRoutingTable() {
	tbl := d.routingTable.reachableNodes()
	if len(tbl) > 5 {
		d.store.Remotes = tbl
		saveStore(*d.store)
	}
}

func (d *DHT) needMoreNodes() bool {
	n := d.routingTable.numNodes()
	return n < minNodes || n*2 < d.config.MaxNodes
}

// needMorePeers reports an under-seeded info hash.
func (d *DHT) needMorePeers(ih InfoHash) bool {
	return d.peerStore.alive(ih) < d.config.NumTargetPeers
}

// getMorePeers pulls peers for every wanting download, via r or broadly.
func (d *DHT) getMorePeers(r *remoteNode) {
	for ih := range d.peerStore.localActiveDownloads {
		if d.needMorePeers(ih) {
			if r == nil {
				d.getPeers(ih)
			} else {
				d.getPeersFrom(r, ih)
			}
		}
	}
}

// helloFromPeer pings an unknown address into reachability probing,
// skipping known ones and holding back past a full table.
func (d *DHT) helloFromPeer(addr string) {
	_, addrResolved, existed, err := d.routingTable.hostPortToNode(addr, d.config.UDPProto)
	if err != nil {
		d.DebugLogger.Debugf("helloFromPeer error: %v", err)
		return
	}
	if existed {
		return
	}
	if d.routingTable.length() < d.config.MaxNodes {
		d.ping(addrResolved)
		return
	}
}

func (d *DHT) processPacket(p packetType) {
	d.DebugLogger.Debugf("DHT processing packet from %v", p.raddr.String())
	if !d.clientThrottle.CheckBlock(p.raddr.IP.String()) {
		totalPacketsFromBlockedHosts.Add(1)
		d.DebugLogger.Debugf("Node exceeded rate limiter. Dropping packet.")
		return
	}
	if len(p.b) == 0 || p.b[0] != 'd' {
		// Malformed DHT packet. There are protocol extensions out
		// there that we don't support or understand.
		d.DebugLogger.Debugf("Malformed DHT packet.")
		return
	}
	r, err := readResponse(p, d.DebugLogger)
	if err != nil {
		d.DebugLogger.Debugf("DHT: readResponse Error: %v, %q", err, string(p.b))
		return
	}
	switch {
	case r.Y == "r":
		d.handleResponsePacket(p, r)
	case r.Y == "q":
		d.handleQueryPacket(p, r)
	default:
		d.DebugLogger.Debugf("DHT: Bogus DHT query from %v.", p.raddr)
	}
}

// handleResponsePacket routes a reply: sender bookkeeping (BEP 42 reflected
// address, id fix-ups, reachability, neighborhood), then dispatch by the
// answered query kind. Unknown senders get one ping when the table has room.
func (d *DHT) handleResponsePacket(p packetType, r responseType) {
	d.DebugLogger.Debugf("DHT processing response from %x", r.R.Id)
	if bogusId(r.R.Id) {
		d.DebugLogger.Debugf("DHT received packet with bogus node id %x", r.R.Id)
		return
	}
	if r.R.Id == d.nodeId {
		d.DebugLogger.Debugf("DHT received reply from self, id %x", r.A.Id)
		return
	}
	// BEP 42: the responder tells us our external IP; adopt a secure node
	// ID for it once, if the external IP was not configured at startup.
	d.noteExternalIP(r.R.IP)
	node, addr, existed, err := d.routingTable.hostPortToNode(p.raddr.String(), d.config.UDPProto)
	if err != nil {
		d.DebugLogger.Debugf("DHT readResponse error processing response: %v", err)
		return
	}
	if !existed {
		d.DebugLogger.Debugf("DHT: Received reply from a host we don't know: %v", p.raddr)
		if d.routingTable.length() < d.config.MaxNodes {
			d.ping(addr)
		}
		return
	}
	// Fix the node ID.
	if node.id == "" {
		node.id = r.R.Id
		d.routingTable.update(node, d.config.UDPProto)
	}
	if node.id != r.R.Id {
		d.DebugLogger.Debugf("DHT: Node changed IDs %x => %x", node.id, r.R.Id)
	}
	if query, ok := node.pendingQueries[r.T]; ok {
		d.DebugLogger.Debugf("DHT: Received reply to %v", query.Type)
		d.absorbReply(node, addr, p, r, query)
	} else {
		d.DebugLogger.Debugf("DHT: Unknown query id: %v", r.T)
	}
}

// absorbReply folds one answered query into the node: reachability,
// reflected address (BEP 42), neighborhood, table refill, then dispatch by
// query kind.
func (d *DHT) absorbReply(node *remoteNode, addr string, p packetType, r responseType, query *queryType) {
	if !node.reachable {
		node.reachable = true
		totalNodesReached.Add(1)
	}
	node.lastResponseTime = time.Now()
	reflected := r.IP
	if reflected == "" {
		reflected = r.R.IP
	}
	recordReflected(reflected, p.raddr.IP)
	node.pastQueries[r.T] = query
	d.routingTable.neighborhoodUpkeep(node, d.config.UDPProto, d.peerStore)

	// First host in the table: look ourselves up recursively to build the
	// neighborhood ASAP.
	if d.needMoreNodes() {
		d.DebugLogger.Debugf("DHT: need more nodes")
		d.findNode(d.nodeId)
	}
	d.exploredNeighborhood = true

	switch query.Type {
	case "ping":
		totalRecvPingReply.Add(1)
	case "get_peers":
		d.DebugLogger.Debugf("DHT: got get_peers response")
		d.processGetPeerResults(node, r)
	case "find_node":
		d.DebugLogger.Debugf("DHT: got find_node response")
		d.processFindNodeResults(node, r)
	case "announce_peer":
		// Nothing to do. In the future, update counters.
	case "get", "put":
		// BEP 44 outbound: a reply to our own get/put.
		d.handleBep44Reply(node, p, query)
	default:
		d.DebugLogger.Debugf("DHT: Unknown query type: %v from %v", query.Type, addr)
	}
	delete(node.pendingQueries, r.T)
}

// handleQueryPacket answers an inbound query: self-messages drop,
// read-only nodes stay silent (BEP 43), unknown senders get one reachability
// ping (unless read-only flagged), then dispatch by query kind.
func (d *DHT) handleQueryPacket(p packetType, r responseType) {
	if r.A.Id == d.nodeId {
		d.DebugLogger.Debugf("DHT received packet from self, id %x", r.A.Id)
		return
	}
	if d.config.ReadOnly {
		return
	}
	node, addr, existed, err := d.routingTable.hostPortToNode(p.raddr.String(), d.config.UDPProto)
	if err != nil {
		d.DebugLogger.Debugf("Error readResponse error processing query: %v", err)
		return
	}
	if !existed && r.Ro != 1 {
		if d.routingTable.length() < d.config.MaxNodes {
			d.ping(addr)
		}
	}
	d.DebugLogger.Debugf("DHT processing %v request", r.Q)
	switch r.Q {
	case "ping":
		totalRecvPing.Add(1)
		d.replyPing(p.raddr, r)
	case "get_peers":
		d.replyGetPeers(p.raddr, r)
	case "find_node":
		d.replyFindNode(p.raddr, r)
	case "announce_peer":
		totalRecvAnnouncePeer.Add(1)
		d.replyAnnouncePeer(p.raddr, node, r)
	case "get":
		d.replyGet(p.raddr, p.b)
	case "put":
		d.replyPut(p.raddr, p.b)
	default:
		d.DebugLogger.Debugf("DHT: non-implemented handler for type %v", r.Q)
	}
}

// ping opens a reachability probe to address.
func (d *DHT) ping(address string) {
	r, err := d.routingTable.getOrCreateNode("", address, d.config.UDPProto)
	if err != nil {
		d.DebugLogger.Debugf("ping error for address %v: %v", address, err)
		return
	}
	d.pingNode(r)
}

// pingNode queries one node for liveness.
func (d *DHT) pingNode(r *remoteNode) {
	d.DebugLogger.Debugf("DHT: ping => %+v", r.address)
	t := r.newQuery("ping")

	queryArguments := map[string]interface{}{"id": d.nodeId}
	query := queryMessage{t, "q", "ping", queryArguments, d.readOnlyFlag()}
	sendMsg(d.conn, r.address, query, d.DebugLogger)
	totalSentPing.Add(1)
}

// dualStack enables BEP 32 dual-stack on the family-agnostic "udp" proto
// only; "udp4" (default) and "udp6" stay single-family.
func (d *DHT) dualStack() bool {
	return d.config.UDPProto == "udp"
}

// readOnlyFlag marks outgoing queries so others skip us (BEP 43).
func (d *DHT) readOnlyFlag() int {
	if d.config.ReadOnly {
		return 1
	}
	return 0
}

// replyIP renders the BEP 42 reflected address (compact, top-level) so a
// NATed requester learns itself; it meters every emission.
func replyIP(addr net.UDPAddr) string {
	bin := encodeCompactPeerAddr(addr.String())
	if bin != "" {
		reflectedSent.Add(1)
	}
	return bin
}

// nodesField pairs a raw nodes blob with its parse family.
type nodesField struct {
	raw   string
	proto string
}

// nodeResponseFields selects which nodes blobs to parse per proto: both on
// dual-stack ("udp"), one on single-family. IPv6 contacts always parse as
// "udp6", never "udp4".
func (d *DHT) nodeResponseFields(nodes, nodes6 string) []nodesField {
	switch {
	case d.dualStack():
		return []nodesField{{nodes, "udp4"}, {nodes6, "udp6"}}
	case d.config.UDPProto == "udp6":
		return []nodesField{{nodes6, "udp6"}}
	default: // "udp4"
		return []nodesField{{nodes, "udp4"}}
	}
}

// getPeersFrom queries one node for ih's peers, tagging the exchange.
func (d *DHT) getPeersFrom(r *remoteNode, ih InfoHash) {
	if r == nil {
		return
	}
	totalSentGetPeers.Add(1)
	ty := "get_peers"
	transId := r.newQuery(ty)
	tagPendingQuery(r, transId, ih)
	queryArguments := map[string]interface{}{
		"id":        d.nodeId,
		"info_hash": ih,
	}
	// BEP 32: dual-stack asks for both families; single-family sends no
	// "want" (udp4 behavior unchanged).
	if d.dualStack() {
		queryArguments["want"] = []string{"n4", "n6"}
	}
	query := queryMessage{transId, "q", ty, queryArguments, d.readOnlyFlag()}
	d.DebugLogger.Debugf("DHT sending get_peers. nodeID: %x@%v, InfoHash: %x , distance: %x", r.id, r.address, ih, hashDistance(InfoHash(r.id), ih))
	r.lastSearchTime = time.Now()
	sendMsg(d.conn, r.address, query, d.DebugLogger)
}

// findNodeFrom queries one node for id's neighborhood, tagging the exchange.
func (d *DHT) findNodeFrom(r *remoteNode, id string) {
	if r == nil {
		return
	}
	totalSentFindNode.Add(1)
	ty := "find_node"
	transId := r.newQuery(ty)
	ih := InfoHash(id)
	d.DebugLogger.Debugf("findNodeFrom adding pendingQueries transId=%v ih=%x", transId, ih)
	tagPendingQuery(r, transId, ih)
	queryArguments := map[string]interface{}{
		"id":     d.nodeId,
		"target": id,
	}
	query := queryMessage{transId, "q", ty, queryArguments, d.readOnlyFlag()}
	d.DebugLogger.Debugf("DHT sending find_node. nodeID: %x@%v, target ID: %x , distance: %x", r.id, r.address, id, hashDistance(InfoHash(r.id), ih))
	r.lastSearchTime = time.Now()
	sendMsg(d.conn, r.address, query, d.DebugLogger)
}

// announcePeer advertises ourselves as a peer for ih to address, token
// in hand. tagPendingQuery records the subject on the exchange.
// tagPendingQuery subjects a fresh exchange to ih: reuse the slot when
// newQuery recycled an id still present, else open one.
func tagPendingQuery(r *remoteNode, transId string, ih InfoHash) {
	if _, ok := r.pendingQueries[transId]; ok {
		r.pendingQueries[transId].ih = ih
	} else {
		r.pendingQueries[transId] = &queryType{ih: ih}
	}
}

func (d *DHT) announcePeer(address net.UDPAddr, ih InfoHash, port int, token string) {
	r, err := d.routingTable.getOrCreateNode("", address.String(), d.config.UDPProto)
	if err != nil {
		d.DebugLogger.Debugf("announcePeer error: %v", err)
		return
	}
	ty := "announce_peer"
	d.DebugLogger.Debugf("DHT: announce_peer => address: %v, ih: %x, token: %x", address, ih, token)
	transId := r.newQuery(ty)
	queryArguments := map[string]interface{}{
		"id":        d.nodeId,
		"info_hash": ih,
		"port":      port,
		"token":     token,
	}
	query := queryMessage{transId, "q", ty, queryArguments, d.readOnlyFlag()}
	sendMsg(d.conn, address, query, d.DebugLogger)
	totalSentAnnouncePeer.Add(1)
}

// hostToken derives a per-source, per-scope announce token: a token issued
// for one swarm (or BEP 44 path) never validates on another.
func (d *DHT) hostToken(addr net.UDPAddr, scope []byte, secret string) string {
	h := sha1.New()
	io.WriteString(h, addr.String())
	h.Write(scope)
	io.WriteString(h, secret)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// checkToken validates against the current and previous rotating secrets.
func (d *DHT) checkToken(addr net.UDPAddr, scope []byte, token string) bool {
	match := false
	for _, secret := range d.tokenSecrets {
		if d.hostToken(addr, scope, secret) == token {
			match = true
			break
		}
	}
	d.DebugLogger.Debugf("checkToken for %v, %q matches? %v", addr, token, match)
	return match
}

// replyAnnouncePeer validates and stores an announce: node may be nil on a
// fresh restart. Implied port (BEP 5) substitutes the packet source port;
// out-of-range ports never store (they would hand every lookup a dud);
// token mismatch counts but still gets a positive reply (no back-tracking).
func (d *DHT) replyAnnouncePeer(addr net.UDPAddr, node *remoteNode, r responseType) {
	ih := InfoHash(r.A.InfoHash)
	d.DebugLogger.Debugf("DHT: announce_peer. Host %v, nodeID: %x, infoHash: %x, peerPort %d, distance to me %x",
		addr, r.A.Id, ih, r.A.Port, hashDistance(ih, InfoHash(d.nodeId)),
	)
	port := r.A.Port
	if r.A.ImpliedPort == 1 {
		port = addr.Port
	}
	validPort := port > 0 && port <= 65535
	if node != nil && validPort && d.checkToken(addr, []byte(ih), r.A.Token) {
		totalAnnounceTokenOK.Add(1)
		peerAddr := net.TCPAddr{IP: addr.IP, Port: port}
		d.peerStore.addContact(ih, encodeCompactPeerAddr(peerAddr.String()), r.A.Seed == 1)
		// Allow searching this node immediately, since it's telling us
		// it has an infohash. Enables faster upgrade of other nodes to
		// "peer" of an infohash, if the announcement is valid.
		node.lastResponseTime = time.Now().Add(-searchRetryPeriod)
		localPort := d.peerStore.hasLocalDownload(ih)
		if localPort != 0 {
			d.PeersRequestResults <- map[InfoHash][]string{ih: {encodeCompactPeerAddr(peerAddr.String())}}
		}
	} else {
		// Wrong, missing or mismatched-query token: not stored.
		totalAnnounceTokenBad.Add(1)
	}
	reply := replyMessage{
		T:  r.T,
		Y:  "r",
		IP: replyIP(addr),
		R:  map[string]interface{}{"id": d.nodeId},
	}
	sendMsg(d.conn, addr, reply, d.DebugLogger)
}

// replyGetPeers answers with a token plus, exclusively: scrape blooms for
// scrape asks (BEP 33), peers for known swarms (seeds filtered on noseed),
// else the closest nodes split by family (BEP 32).
func (d *DHT) replyGetPeers(addr net.UDPAddr, r responseType) {
	totalRecvGetPeers.Add(1)
	d.DebugLogger.Debugf("DHT get_peers. Host: %v , nodeID: %x , InfoHash: %x , distance to me: %x",
		addr, r.A.Id, InfoHash(r.A.InfoHash), hashDistance(r.A.InfoHash, InfoHash(d.nodeId)))

	if d.Logger != nil {
		d.Logger.GetPeers(addr, r.A.Id, r.A.InfoHash)
	}

	ih := r.A.InfoHash
	r0 := map[string]interface{}{"id": d.nodeId, "token": d.hostToken(addr, []byte(ih), d.tokenSecrets[0])}
	reply := replyMessage{
		T:  r.T,
		Y:  "r",
		IP: replyIP(addr),
		R:  r0,
	}

	if r.A.Scrape == 1 {
		if pe, sd := d.peerStore.scrapeFilters(ih); pe != nil || sd != nil {
			if pe != nil {
				reply.R["BFpe"] = string(pe[:])
			}
			if sd != nil {
				reply.R["BFsd"] = string(sd[:])
			}
		}
	} else if peerContacts := d.peersForInfoHash(ih, r.A.NoSeed == 1); len(peerContacts) > 0 {
		reply.R["values"] = peerContacts
	}
	if _, hasValues := reply.R["values"]; !hasValues {
		n4, n6 := d.nodesForInfoHash(ih)
		if n4 != "" {
			reply.R["nodes"] = n4
		}
		if n6 != "" {
			reply.R["nodes6"] = n6
		}
	}
	sendMsg(d.conn, addr, reply, d.DebugLogger)
}

// nodesForInfoHash returns the closest nodes for ih split by address family:
// nodesForInfoHash renders the closest nodes per family (BEP 32): v4
// contacts (id + 6 bytes) into n4, v6 (id + 18 bytes) into n6. Bogus
// addresses die here instead of poisoning replies.
func (d *DHT) nodesForInfoHash(ih InfoHash) (n4, n6 string) {
	v4 := make([]string, 0, kNodes)
	v6 := make([]string, 0, kNodes)
	for _, r := range d.routingTable.lookup(ih) {
		if r != nil {
			binaryHost := r.id + encodeCompactPeerAddr(r.address.String())
			if binaryHost == "" {
				d.DebugLogger.Debugf("killing node with bogus address %v", r.address.String())
				d.routingTable.kill(r, d.peerStore)
			} else if r.address.IP.To4() != nil {
				v4 = append(v4, binaryHost)
			} else {
				v6 = append(v6, binaryHost)
			}
		}
	}
	d.DebugLogger.Debugf("replyGetPeers: Nodes only. Giving %d v4, %d v6", len(v4), len(v6))
	return strings.Join(v4, ""), strings.Join(v6, "")
}

// peersForInfoHash serves known swarm contacts, logging a hit.
func (d *DHT) peersForInfoHash(ih InfoHash, noseed bool) []string {
	peerContacts := d.peerStore.peerContacts(ih, noseed)
	if len(peerContacts) > 0 {
		d.DebugLogger.Debugf("replyGetPeers: Giving peers! %x was requested, and we knew %d peers!", ih, len(peerContacts))
	}
	return peerContacts
}

// replyFindNode answers with filtered neighbors topped up unfiltered,
// each family in its own field capped at kNodes (BEP 32).
func (d *DHT) replyFindNode(addr net.UDPAddr, r responseType) {
	totalRecvFindNode.Add(1)
	d.DebugLogger.Debugf("DHT find_node. Host: %v , nodeId: %x , target ID: %x , distance to me: %x",
		addr, r.A.Id, r.A.Target, hashDistance(InfoHash(r.A.Target), InfoHash(d.nodeId)))

	node := InfoHash(r.A.Target)
	r0 := map[string]interface{}{"id": d.nodeId}
	reply := replyMessage{
		T:  r.T,
		Y:  "r",
		IP: replyIP(addr),
		R:  r0,
	}

	neighbors := d.routingTable.lookupFiltered(node)
	if len(neighbors) < kNodes {
		neighbors = append(neighbors, d.routingTable.lookup(node)...)
	}
	v4 := make([]string, 0, kNodes)
	v6 := make([]string, 0, kNodes)
	for _, r := range neighbors {
		contact := r.id + r.addressBinaryFormat
		if r.address.IP.To4() != nil {
			if len(v4) < kNodes {
				v4 = append(v4, contact)
			}
		} else if len(v6) < kNodes {
			v6 = append(v6, contact)
		}
	}
	d.DebugLogger.Debugf("replyFindNode: Nodes only. Giving %d v4, %d v6", len(v4), len(v6))
	if len(v4) > 0 {
		reply.R["nodes"] = strings.Join(v4, "")
	}
	if len(v6) > 0 {
		reply.R["nodes6"] = strings.Join(v6, "")
	}
	sendMsg(d.conn, addr, reply, d.DebugLogger)
}

// replyPing answers a liveness probe with our id.
func (d *DHT) replyPing(addr net.UDPAddr, response responseType) {
	d.DebugLogger.Debugf("DHT: reply ping => %v", addr)
	reply := replyMessage{
		T:  response.T,
		Y:  "r",
		IP: replyIP(addr),
		R:  map[string]interface{}{"id": d.nodeId},
	}
	sendMsg(d.conn, addr, reply, d.DebugLogger)
}

// Process another node's response to a get_peers query. If the response
// contains peers, send them to the Torrent engine, our client, using the
// DHT.PeersRequestResults channel. If it contains closest nodes, query
// them if we still need it. Also announce ourselves as a peer for that node,
// unless we are in supernode mode.
func (d *DHT) processGetPeerResults(node *remoteNode, resp responseType) {
	totalRecvGetPeersReply.Add(1)

	query, _ := node.pendingQueries[resp.T]
	if query.scrape {
		d.collectScrape(query.ih, resp.R)
	}
	port := d.peerStore.hasLocalDownload(query.ih)
	if port != 0 {
		d.announcePeer(node.address, query.ih, port, resp.R.Token)
	}
	if resp.R.Values != nil {
		d.publishPeers(query.ih, node, resp.R.Values)
	}
	// Read the "nodes" (IPv4) and/or "nodes6" (IPv6) fields according to the
	// configured protocol. Each field is parsed and resolved with its own
	// family, so an IPv6 contact is never resolved with "udp4".
	// Read the "nodes" (IPv4) and/or "nodes6" (IPv6) fields according to the
	// configured protocol. Each field is parsed and resolved with its own
	// family, so an IPv6 contact is never resolved with "udp4".
	for _, field := range d.nodeResponseFields(resp.R.Nodes, resp.R.Nodes6) {
		d.absorbDiscoveredNodes(node, query, field)
	}
}

// publishPeers stores the reply's contacts (dupes included: the client
// dedupes) and publishes them, unless stopping with nobody listening.
func (d *DHT) publishPeers(ih InfoHash, node *remoteNode, values []string) {
	peers := make([]string, 0, len(values))
	reporter := reporterKey(node)
	for _, peerContact := range values {
		d.peerStore.addContact(ih, peerContact, false)
		d.notePeerReport(ih, peerContact, reporter)
		peers = append(peers, peerContact)
	}
	if len(peers) == 0 {
		return
	}
	result := map[InfoHash][]string{ih: peers}
	totalPeers.Add(int64(len(peers)))
	d.DebugLogger.Debugf("DHT: processGetPeerResults, totalPeers: %v", totalPeers.String())
	select {
	case d.PeersRequestResults <- result:
	case <-d.stop:
	}
}

// absorbDiscoveredNodes folds one nodes-field of a get_peers reply into the
// table: self references and self-promotions drop (counted), dupes count,
// and genuinely new close nodes requeue the lookup while peers are wanted.
func (d *DHT) absorbDiscoveredNodes(node *remoteNode, query *queryType, field nodesField) {
	d.DebugLogger.Debugf("DHT: handling get_peers results proto=%s len(nodelist)=%d", field.proto, len(field.raw))
	if field.raw == "" {
		return
	}
	for id, address := range parseNodesString(field.raw, field.proto, d.DebugLogger) {
		if id == d.nodeId {
			d.DebugLogger.Debugf("DHT got reference of self for get_peers, id %x", id)
			continue
		}
		_, addr, existed, err := d.routingTable.hostPortToNode(address, field.proto)
		if err != nil {
			d.DebugLogger.Debugf("DHT error parsing get peers node: %v", err)
			continue
		}
		if addr == node.address.String() {
			// A node vouching for its own address is likely sniffing or
			// attracting traffic: ignore the whole reference.
			totalSelfPromotions.Add(1)
			continue
		}
		if existed {
			d.DebugLogger.Debugf("DHT: processGetPeerResults DUPE node reference: %x@%v from %x@%v. Distance: %x.",
				id, address, node.id, node.address, hashDistance(query.ih, InfoHash(node.id)))
			totalGetPeersDupes.Add(1)
			continue
		}
		d.DebugLogger.Debugf("DHT: Got new node reference: %x@%v from %x@%v. Distance: %x.",
			id, address, node.id, node.address, hashDistance(query.ih, InfoHash(node.id)))
		if _, err := d.routingTable.getOrCreateNode(id, addr, field.proto); err == nil && d.needMorePeers(query.ih) {
			// Requeue while peers are wanted (announce stays false: local
			// downloads already recorded it, and announcing happens on
			// reply processing, not on send). A full channel drops the
			// requeue; the node is tabled anyway for the next getPeers.
			select {
			case d.peersRequest <- ihReq{ih: query.ih}:
			default:
			}
		}
	}
}

// Process another node's response to a find_node query.
func (d *DHT) processFindNodeResults(node *remoteNode, resp responseType) {
	totalRecvFindNodeReply.Add(1)

	query, _ := node.pendingQueries[resp.T]
	// Like processGetPeerResults, read "nodes" (IPv4) and/or "nodes6" (IPv6)
	// depending on the configured protocol, each parsed with its own family.
	for _, field := range d.nodeResponseFields(resp.R.Nodes, resp.R.Nodes6) {
		d.absorbFoundNodes(node, query, field)
	}
}

// absorbFoundNodes folds one nodes-field of a find_node reply into the
// table: unparsables, self references and self-promotions (common here,
// even from public routers) drop counted; dupes count; new nodes table,
// requeue the search while the table is thin, and pull peers through them.
func (d *DHT) absorbFoundNodes(node *remoteNode, query *queryType, field nodesField) {
	d.DebugLogger.Debugf("processFindNodeResults find_node = %s proto=%s len(nodelist)=%d", decodeCompactPeerAddr(node.addressBinaryFormat), field.proto, len(field.raw))
	if field.raw == "" {
		return
	}
	for id, address := range parseNodesString(field.raw, field.proto, d.DebugLogger) {
		_, addr, existed, err := d.routingTable.hostPortToNode(address, field.proto)
		if err != nil {
			d.DebugLogger.Debugf("DHT error parsing node from find_find response: %v", err)
			continue
		}
		if id == d.nodeId {
			d.DebugLogger.Debugf("DHT got reference of self for find_node, id %x", id)
			continue
		}
		if addr == node.address.String() {
			totalSelfPromotions.Add(1)
			continue
		}
		if existed {
			d.DebugLogger.Debugf("DHT: processFindNodeResults DUPE node reference, query %x: %x@%v from %x@%v. Distance: %x.",
				query.ih, id, address, node.id, node.address, hashDistance(query.ih, InfoHash(node.id)))
			totalFindNodeDupes.Add(1)
			continue
		}
		d.DebugLogger.Debugf("DHT: Got new node reference, query %x: %x@%v from %x@%v. Distance: %x.",
			query.ih, id, address, node.id, node.address, hashDistance(query.ih, InfoHash(node.id)))
		r, err := d.routingTable.getOrCreateNode(id, addr, field.proto)
		if err != nil {
			d.DebugLogger.Debugf("processFindNodeResults calling getOrCreateNode: %v. Id=%x, Address=%q", err, id, addr)
			continue
		}
		if d.needMoreNodes() {
			// Requeue while the table is thin; a full channel drops it
			// (the node is tabled anyway).
			select {
			case d.nodesRequest <- ihReq{ih: query.ih}:
			default:
			}
		}
		d.getMorePeers(r)
	}
}

// randNodeId draws a 20-byte node id.
func randNodeId() ([]byte, error) {
	b := make([]byte, 20)
	_, err := io.ReadFull(rand.Reader, b)
	return b, err
}

var (
	totalNodesReached            = expvar.NewInt("totalNodesReached")
	totalGetPeersDupes           = expvar.NewInt("totalGetPeersDupes")
	totalFindNodeDupes           = expvar.NewInt("totalFindNodeDupes")
	totalSelfPromotions          = expvar.NewInt("totalSelfPromotions")
	totalPeers                   = expvar.NewInt("totalPeers")
	totalSentPing                = expvar.NewInt("totalSentPing")
	totalSentGetPeers            = expvar.NewInt("totalSentGetPeers")
	totalSentFindNode            = expvar.NewInt("totalSentFindNode")
	totalRecvGetPeers            = expvar.NewInt("totalRecvGetPeers")
	totalRecvGetPeersReply       = expvar.NewInt("totalRecvGetPeersReply")
	totalRecvPing                = expvar.NewInt("totalRecvPing")
	totalRecvPingReply           = expvar.NewInt("totalRecvPingReply")
	totalRecvFindNode            = expvar.NewInt("totalRecvFindNode")
	totalRecvFindNodeReply       = expvar.NewInt("totalRecvFindNodeReply")
	totalRecvAnnouncePeer        = expvar.NewInt("totalRecvAnnouncePeer")
	totalSentAnnouncePeer        = expvar.NewInt("totalSentAnnouncePeer")
	totalAnnounceTokenOK         = expvar.NewInt("totalAnnounceTokenOK")
	totalAnnounceTokenBad        = expvar.NewInt("totalAnnounceTokenBad")
	totalPacketsFromBlockedHosts = expvar.NewInt("totalPacketsFromBlockedHosts")
	totalDroppedPackets          = expvar.NewInt("totalDroppedPackets")
	totalRecv                    = expvar.NewInt("totalRecv")
)

// knownNodes is the routing table size, readable from any goroutine.
var knownNodes atomic.Int64

// subnetCapHintValue is the suggested Config.MaxNodesPerSubnet from the live
// routing-table concentration (0 = nothing to suggest), published by the run
// loop and exposed in Stats().
var subnetCapHintValue atomic.Int64

// Stats snapshots the DHT counters for SessionStats.DHTStats: the daemon
// exposes each key as "dht_<key>" (API and web page), so names are frozen.
func Stats() map[string]int64 {
	return map[string]int64{
		"nodes":         knownNodes.Load(),
		"nodes_reached": totalNodesReached.Value(),
		"peers_found":   totalPeers.Value(),
		"packets_in":    totalRecv.Value(),
		"packets_out":   totalSent.Value(),
		"bytes_in":      totalReadBytes.Value(),
		"bytes_out":     totalWrittenBytes.Value(),
		"dropped":       totalDroppedPackets.Value(),

		// Per-RPC traffic over the four core calls; BEP 44 get/put excluded.
		"queries_in": totalRecvPing.Value() + totalRecvGetPeers.Value() +
			totalRecvFindNode.Value() + totalRecvAnnouncePeer.Value(),
		"queries_out": totalSentPing.Value() + totalSentGetPeers.Value() +
			totalSentFindNode.Value() + totalSentAnnouncePeer.Value(),
		"replies_in": totalRecvPingReply.Value() + totalRecvGetPeersReply.Value() +
			totalRecvFindNodeReply.Value(),

		// Screening and health: rejected and ignored noise, measurable across
		// hardening changes.
		"blocked_hosts":   totalPacketsFromBlockedHosts.Value(),
		"self_promotions": totalSelfPromotions.Value(),
		"dupe_references": totalGetPeersDupes.Value() + totalFindNodeDupes.Value(),
		"nodes_killed":    totalKilledNodes.Value(),

		// Token hygiene: accepted vs rejected (wrong, missing, foreign-scope).
		"announce_ok_token":  totalAnnounceTokenOK.Value(),
		"announce_bad_token": totalAnnounceTokenBad.Value(),

		// Reflected self-view (BEP 42 "ip") and inferred NAT mapping (0
		// unknown, 1 cone, 2 symmetric); see recordReflected.
		"reflected_port":         reflectedPort.Load(),
		"reflected_observations": reflectedObs.Load(),
		"reflected_sent":         reflectedSent.Load(),
		"nat_mapping":            natMappingType.Load(),

		// Table composition (Sybil visibility): max nodes per subnet, insecure
		// ids, capped nodes, and the cap hint (0 = none suggested; the engine
		// never self-enables it).
		"subnet_share_max":      subnetShareMax.Load(),
		"nodes_insecure_id":     nodesInsecureID.Load(),
		"nodes_capped":          totalNodesCapped.Value(),
		"subnet_cap_hint":       subnetCapHintValue.Load(),
		"peers_confirmed_multi": peersConfirmedMulti.Value(),
	}
}
