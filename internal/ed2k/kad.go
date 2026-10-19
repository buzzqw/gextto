package ed2k

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config configures the search client.
type Config struct {
	// DataDir keeps the node ID, the cached nodes.dat and the server list.
	DataDir string
	// UDPListen is the Kad bind address, e.g. "0.0.0.0:4672". Empty binds an
	// ephemeral port on all interfaces.
	UDPListen string
	// TCPPort is advertised in HELLO packets. This client does not accept
	// eMule client connections yet, so it is informational.
	TCPPort int
	// NodesURL downloads a fresh nodes.dat when the cache is missing or old.
	NodesURL string
	// NodesFile overrides the cached nodes.dat path.
	NodesFile string
	// ServerListURL downloads a server.met when no explicit server list is set.
	ServerListURL string
	// Servers is an explicit `host:port` list; it wins over ServerListURL.
	Servers []string
	// SearchTimeout bounds a whole Search call, bootstrap included.
	SearchTimeout time.Duration
	// QueryTimeout bounds the result-collection window. Zero uses
	// SearchTimeout minus a reserve.
	QueryTimeout time.Duration
	// NodesPerRequest is how many contacts a Kad lookup asks for (eD2k uses
	// 2 for FIND_VALUE, 8 for FIND_NODE; 8 is a good middle ground).
	NodesPerRequest int
	// MaxContacts caps the routing table.
	MaxContacts int
	// MaxResults caps the results kept from one query.
	MaxResults int
	// UPnP asks the router to forward the Kad UDP port.
	UPnP bool
	// UPnPDescription is the port-mapping description shown by the router.
	UPnPDescription string
	// ClientHandler receives incoming eMule client connections on TCPPort.
	// Nil keeps the search-only behaviour: the callback is accepted and the
	// connection is closed. The upload plane sets it.
	ClientHandler func(net.Conn)
	// UserHash is the eMule client identity used for server login and peer
	// credits. Zero loads (or creates) it in DataDir.
	UserHash ID
}

func (c *Config) applyDefaults() {
	if c.SearchTimeout <= 0 {
		c.SearchTimeout = 20 * time.Second
	}
	if c.QueryTimeout <= 0 {
		c.QueryTimeout = c.SearchTimeout - 6*time.Second
		if c.QueryTimeout < 3*time.Second {
			c.QueryTimeout = 3 * time.Second
		}
	}
	if c.NodesPerRequest <= 0 {
		c.NodesPerRequest = 8
	}
	if c.MaxContacts <= 0 {
		c.MaxContacts = 512
	}
	if c.MaxResults <= 0 {
		c.MaxResults = 500
	}
	if c.TCPPort <= 0 {
		c.TCPPort = 4662
	}
	if c.UPnPDescription == "" {
		c.UPnPDescription = "gx-ed2k Kad"
	}
	if c.NodesFile == "" && c.DataDir != "" {
		c.NodesFile = filepath.Join(c.DataDir, "nodes.dat")
	}
}

// Stats is the live state reported by /api/v1/status and /api/v1/health.
type Stats struct {
	Nodes         int    `json:"nodes"`
	Bootstrapped  bool   `json:"bootstrapped"`
	Servers       int    `json:"servers"`
	ServerSearch  bool   `json:"server_search"`
	Queries       int64  `json:"queries"`
	Results       int64  `json:"results"`
	LastError     string `json:"last_error,omitempty"`
	LastQueryAt   string `json:"last_query_at,omitempty"`
	LastQueryName string `json:"last_query,omitempty"`
	// ServerID is the client ID the last eD2k server assigned: a value below
	// 0x01000000 is a LowID, i.e. the server could not call us back.
	ServerID     uint32     `json:"server_id,omitempty"`
	ServerIDSet  bool       `json:"server_id_known"`
	LowID        bool       `json:"low_id"`
	PeerPackets  int64      `json:"peer_packets"`
	Reachability string     `json:"reachability"`
	ReachDetail  string     `json:"reachability_detail,omitempty"`
	UPnP         UPnPResult `json:"upnp"`
}

// Client is a Kad search node plus an eD2k server search client.
type Client struct {
	cfg Config

	conn *net.UDPConn
	self ID
	port int

	tcpListener net.Listener
	tcpPort     int

	table *routingTable

	mu       sync.Mutex
	pending  map[ID]chan []Contact
	searches map[ID][]*collector
	closed   bool
	closeCh  chan struct{}
	wg       sync.WaitGroup

	bootstrapMu   sync.Mutex
	bootstrapped  bool
	lastBootstrap time.Time
	lastErr       string
	queries       int64
	results       int64
	lastQueryAt   time.Time
	lastQueryName string

	upnpMu   sync.Mutex
	upnp     UPnPResult
	upnpDone bool

	serverMu    sync.Mutex
	serverID    uint32
	serverIDSet bool

	peerPackets int64

	servers       []string
	userHashValue ID
}

type collector struct {
	files   chan SearchFile
	sources chan PeerSource
}

// New creates a client. Start binds the socket and bootstraps; Search does
// both lazily when it is called on a fresh client.
func New(cfg Config) (*Client, error) {
	cfg.applyDefaults()
	if cfg.DataDir == "" {
		return nil, errors.New("ed2k: DataDir is required")
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("ed2k: data dir: %w", err)
	}
	self, err := loadOrCreateNodeID(filepath.Join(cfg.DataDir, "kad.id"))
	if err != nil {
		return nil, err
	}
	c := &Client{
		cfg:           cfg,
		self:          self,
		userHashValue: loadOrCreateUserID(cfg.DataDir, cfg.UserHash),
		table:         newRoutingTable(cfg.MaxContacts),
		pending:       map[ID]chan []Contact{},
		searches:      map[ID][]*collector{},
		closeCh:       make(chan struct{}),
	}
	c.servers = NormalizeServerList(strings.Join(cfg.Servers, ","))
	return c, nil
}

// SelfID is our Kad identifier.
func (c *Client) SelfID() ID { return c.self }

// Nodes snapshots the routing table, closest first from our own ID.
func (c *Client) Nodes() []Contact { return c.table.closest(c.self, 0) }

func loadOrCreateNodeID(path string) (ID, error) {
	if raw, err := os.ReadFile(path); err == nil {
		if id, err := ParseID(strings.TrimSpace(string(raw))); err == nil {
			return id, nil
		}
	}
	id, err := RandomID()
	if err != nil {
		return id, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return id, fmt.Errorf("ed2k: node id: %w", err)
	}
	if err := os.WriteFile(path, []byte(id.Hex()+"\n"), 0o644); err != nil {
		return id, fmt.Errorf("ed2k: node id: %w", err)
	}
	return id, nil
}

// Start binds the UDP socket and starts the packet loop. It is idempotent.
func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.conn != nil {
		c.mu.Unlock()
		return nil
	}
	listen := c.cfg.UDPListen
	if listen == "" {
		listen = "0.0.0.0:0"
	}
	addr, err := net.ResolveUDPAddr("udp4", listen)
	if err != nil {
		c.mu.Unlock()
		return fmt.Errorf("ed2k: Kad listen address: %w", err)
	}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		c.mu.Unlock()
		return fmt.Errorf("ed2k: Kad socket: %w", err)
	}
	c.conn = conn
	c.port = conn.LocalAddr().(*net.UDPAddr).Port
	// eD2k assigns a HighID only when it can open a TCP connection to the
	// advertised port. This daemon does not transfer files, so it accepts the
	// callback and closes the connection; the port must still be listening.
	tcpListener, tcpErr := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", c.cfg.TCPPort))
	if tcpErr == nil {
		c.tcpListener = tcpListener
		c.tcpPort = c.cfg.TCPPort
	} else {
		log.Printf("gx-ed2k: TCP port %d unavailable: %v (eD2k may assign a LowID)", c.cfg.TCPPort, tcpErr)
	}
	c.mu.Unlock()

	c.wg.Add(1)
	go c.readLoop()
	if c.tcpListener != nil {
		c.wg.Add(1)
		go c.acceptLoop()
	}
	if c.cfg.UPnP {
		go c.openKadPort()
	}
	_ = c.Bootstrap(ctx)
	return nil
}

// acceptLoop accepts the eD2k callback (and any client handshake) and closes
// it. The search client does not speak the client-to-client protocol; being
// connectable is what turns a LowID into a HighID.
func (c *Client) acceptLoop() {
	defer c.wg.Done()
	for {
		conn, err := c.tcpListener.Accept()
		if err != nil {
			return
		}
		if c.cfg.ClientHandler != nil {
			go c.cfg.ClientHandler(conn)
			continue
		}
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		buffer := make([]byte, 64)
		_, _ = conn.Read(buffer)
		_ = conn.Close()
	}
}

// openKadPort asks the router for a UDP mapping. It never blocks a search: the
// result is stored for /api/v1/status, and a router without UPnP simply leaves
// it empty with an explanatory error.
func (c *Client) openKadPort() {
	result := mapKadPort(context.Background(), c.UDPPort(), c.cfg.TCPPort, c.cfg.UPnPDescription)
	c.upnpMu.Lock()
	c.upnp = result
	c.upnpDone = true
	c.upnpMu.Unlock()
	if result.Error != "" {
		log.Printf("gx-ed2k: UPnP: %s", result.Error)
		return
	}
	log.Printf("gx-ed2k: UPnP mapped UDP %d and TCP %d (external IP %s)", c.UDPPort(), c.cfg.TCPPort, result.ExternalIP)
}

// Close stops the packet loop.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	close(c.closeCh)
	conn := c.conn
	listener := c.tcpListener
	c.mu.Unlock()
	// Keep what we learned before the socket goes away: the next start reuses
	// these contacts instead of bootstrapping from scratch.
	c.saveNodes()
	var err error
	if listener != nil {
		_ = listener.Close()
	}
	if conn != nil {
		err = conn.Close()
	}
	c.wg.Wait()
	return err
}

// UDPPort is the bound Kad port (0 before Start).
func (c *Client) UDPPort() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.port
}

// Stats snapshots the counters.
func (c *Client) Stats() Stats {
	c.mu.Lock()
	stats := Stats{
		Nodes:         c.table.len(),
		Bootstrapped:  c.bootstrapped,
		Servers:       len(c.servers),
		Queries:       c.queries,
		Results:       c.results,
		LastError:     c.lastErr,
		LastQueryName: c.lastQueryName,
	}
	if !c.lastQueryAt.IsZero() {
		stats.LastQueryAt = c.lastQueryAt.UTC().Format(time.RFC3339)
	}
	c.mu.Unlock()
	c.upnpMu.Lock()
	stats.UPnP = c.upnp
	upnpDone := c.upnpDone
	c.upnpMu.Unlock()
	c.serverMu.Lock()
	serverID := c.serverID
	serverIDSet := c.serverIDSet
	c.serverMu.Unlock()
	stats.ServerID = serverID
	stats.ServerIDSet = serverIDSet
	stats.LowID = serverIDSet && serverID < 0x01000000
	stats.PeerPackets = atomic.LoadInt64(&c.peerPackets)
	stats.Reachability, stats.ReachDetail = reachability(upnpDone, stats.UPnP, stats.LowID, serverIDSet, stats.PeerPackets > 0)
	return stats
}

// reachability turns UPnP, the eD2k client ID and actual Kad traffic into the
// "sensor" shown by the page: open, limited (firewall/NAT) or unknown. UPnP
// alone is not enough: a mapping can succeed while every outgoing UDP packet
// is dropped, and the sensor must not call that "open".
func reachability(upnpDone bool, upnp UPnPResult, lowID, serverIDKnown, peerAnswered bool) (string, string) {
	if lowID {
		return "limited", "eD2k assigned a LowID: the server cannot call us back (NAT/firewall)"
	}
	if peerAnswered {
		return "open", "Kad peers answered"
	}
	if upnpDone {
		if upnp.Error == "" && upnp.Available {
			return "limited", "UPnP mapped the port, but no Kad peer answered: outbound UDP may be blocked"
		}
		return "limited", "UPnP is unavailable: " + upnp.Error
	}
	if serverIDKnown {
		return "unknown", "the eD2k server assigned a HighID, but UPnP was not checked"
	}
	return "unknown", "no reachability check completed yet"
}

func (c *Client) setError(err error) {
	if err == nil {
		return
	}
	c.mu.Lock()
	c.lastErr = err.Error()
	c.mu.Unlock()
}

// Bootstrap loads the cached node list (downloading it when stale) and asks
// the seeds for contacts. It is safe to call repeatedly; a successful
// bootstrap within the last five minutes is reused.
func (c *Client) Bootstrap(ctx context.Context) error {
	c.bootstrapMu.Lock()
	defer c.bootstrapMu.Unlock()
	c.mu.Lock()
	already := c.bootstrapped
	c.mu.Unlock()
	if already && time.Since(c.lastBootstrap) < 5*time.Minute {
		return nil
	}
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return errors.New("ed2k: Kad socket is not open")
	}
	seeds := c.loadSeeds(ctx)
	if len(seeds) == 0 {
		err := errors.New("ed2k: no Kad bootstrap nodes: configure a nodes.dat URL or a cached file")
		c.setError(err)
		return err
	}

	// Keep every contact from nodes.dat: the first few are asked to bootstrap,
	// but the rest are valid routing entries and make the first search useful
	// even when those seeds are stale.
	for _, contact := range seeds {
		c.table.add(contact, c.self)
	}
	for index, contact := range seeds {
		if index >= 12 {
			break
		}
		c.send(contact, KadBootstrapReq, nil)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) && c.table.len() == 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	if c.table.len() == 0 {
		// The seeds are still valid contacts even when they did not answer the
		// bootstrap request: talking to them directly is the next best thing.
		for _, seed := range seeds {
			c.table.add(seed, c.self)
		}
	}
	for _, contact := range c.table.closest(c.self, 16) {
		c.send(contact, KadHelloReq, encodeHello(c.self, c.cfg.TCPPort))
	}
	c.lastBootstrap = time.Now()
	c.mu.Lock()
	c.bootstrapped = c.table.len() > 0
	ok := c.bootstrapped
	c.mu.Unlock()
	if !ok {
		err := errors.New("ed2k: Kad bootstrap produced no contacts")
		c.setError(err)
		return err
	}
	c.saveNodes()
	return nil
}

// saveNodes writes the contacts learned so far to the local nodes.dat, so the
// next start does not begin from an empty routing table or depend on the
// download being available.
func (c *Client) saveNodes() {
	if c.cfg.NodesFile == "" {
		return
	}
	contacts := c.table.all()
	if len(contacts) == 0 {
		return
	}
	if err := WriteNodesFile(c.cfg.NodesFile, contacts); err != nil {
		c.setError(fmt.Errorf("ed2k: save nodes.dat: %w", err))
	}
}

// loadSeeds returns cached contacts, refreshing the file from NodesURL when it
// is missing or older than a week. A network error falls back to the stale
// cache; a client with no cache starts empty.
func (c *Client) loadSeeds(ctx context.Context) []Contact {
	var seeds []Contact
	if c.cfg.NodesFile != "" {
		seeds = LoadNodesFile(c.cfg.NodesFile)
	}
	fresh := c.cfg.NodesFile == "" || nodesFileFresh(c.cfg.NodesFile, 7*24*time.Hour)
	if len(seeds) == 0 {
		// An empty or unreadable cache must not block the download: this also
		// recovers from a nodes.dat written by an older parser.
		fresh = false
	}
	if fresh || strings.TrimSpace(c.cfg.NodesURL) == "" {
		return seeds
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.NodesURL, nil)
	if err != nil {
		return seeds
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.setError(fmt.Errorf("ed2k: nodes.dat download: %w", err))
		return seeds
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.setError(fmt.Errorf("ed2k: nodes.dat download: HTTP %d", resp.StatusCode))
		return seeds
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return seeds
	}
	downloaded, err := ParseNodesFile(data)
	if err != nil || len(downloaded) == 0 {
		if err != nil {
			c.setError(fmt.Errorf("ed2k: nodes.dat: %w", err))
		}
		return seeds
	}
	if c.cfg.NodesFile != "" {
		_ = WriteNodesFile(c.cfg.NodesFile, downloaded)
	}
	return downloaded
}

// Search looks a keyword up on Kad and, in parallel, on the configured eD2k
// servers. Results are deduplicated by MD4 hash. A failure of one network is
// not fatal while the other produced results.
func (c *Client) Search(ctx context.Context, query string) ([]SearchFile, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("ed2k: empty query")
	}
	if err := c.Start(ctx); err != nil {
		return nil, err
	}
	searchCtx, cancel := context.WithTimeout(ctx, c.cfg.SearchTimeout)
	defer cancel()

	c.mu.Lock()
	c.queries++
	c.lastQueryAt = time.Now()
	c.lastQueryName = query
	c.mu.Unlock()

	keyword := BestKeyword(query)
	if keyword == "" {
		return nil, fmt.Errorf("ed2k: query %q has no searchable keyword", query)
	}
	target := KeywordID(keyword)
	// Send every meaningful token as a restrictive AND tree: the peer then
	// filters the keyword bucket, instead of returning every file indexed
	// under the primary keyword (for example all shows with S04E01).
	terms := SearchTerms(query)

	type networkResult struct {
		files []SearchFile
		err   error
	}
	kadCh := make(chan networkResult, 1)
	serverCh := make(chan networkResult, 1)
	go func() {
		files, err := c.searchKad(searchCtx, keyword, target, terms)
		kadCh <- networkResult{files: files, err: err}
	}()
	go func() {
		files, err := c.searchServers(searchCtx, keyword)
		serverCh <- networkResult{files: files, err: err}
	}()

	kad := <-kadCh
	servers := <-serverCh
	merged := mergeFiles(c.cfg.MaxResults, kad.files, servers.files)
	// An episode query is specific: never fall back to the unfiltered title
	// results when the broad retry returned other episodes. A free-text query
	// keeps the old fuzzy fallback.
	if filtered := filterQueryMatches(merged, query); len(filtered) > 0 || queryHasEpisode(query) {
		merged = filtered
	}
	c.mu.Lock()
	c.results += int64(len(merged))
	if len(merged) == 0 {
		if kad.err != nil {
			c.lastErr = kad.err.Error()
		} else if servers.err != nil {
			c.lastErr = servers.err.Error()
		}
	}
	c.mu.Unlock()
	c.saveNodes()
	if len(merged) == 0 {
		if kad.err != nil && servers.err != nil {
			return nil, fmt.Errorf("ed2k: search failed: Kad: %v; servers: %v", kad.err, servers.err)
		}
		return nil, nil
	}
	return merged, nil
}

func (c *Client) searchKad(ctx context.Context, keyword string, target ID, terms []string) ([]SearchFile, error) {
	if err := c.Bootstrap(ctx); err != nil {
		// With an empty routing table there is nothing to ask. This is a
		// normal, reportable condition (no nodes.dat), not a crash.
		return nil, err
	}
	c.lookup(ctx, target, 5*time.Second)

	collector := &collector{files: make(chan SearchFile, 256)}
	c.addCollector(target, collector)
	defer c.removeCollector(target, collector)

	contacts := c.table.closest(target, 16)
	if len(contacts) == 0 {
		return nil, errors.New("ed2k: no Kad contacts")
	}
	c.sendSearch(ctx, contacts, target, terms)

	deadline := time.Now().Add(c.cfg.QueryTimeout)
	// A Kad keyword search normally answers within a few seconds. Waiting the
	// whole budget when the network is silent (UDP blocked, no responder) only
	// delays the server results, which arrive in parallel.
	if max := time.Now().Add(8 * time.Second); deadline.After(max) {
		deadline = max
	}
	files := c.collectFiles(ctx, collector, deadline, nil)
	if len(files) > 0 || ctx.Err() != nil {
		return files, nil
	}

	// One retry: a lost datagram or a temporarily stale closest set must not
	// turn a real result into a gap. The Datagram is cheap and the extra
	// window is short, while the caller's context still bounds the total.
	c.lookup(ctx, target, 1200*time.Millisecond)
	retry := c.table.closest(target, 32)
	if len(retry) > 16 {
		retry = retry[:16]
	}
	if len(retry) > 0 {
		// A silent round with the exact episode token is often a bucket
		// problem, not a missing file: ask for the title only and let the
		// local filter pick the episode.
		retryTerms := broadTerms(terms)
		if len(retryTerms) == 0 {
			retryTerms = terms
		}
		c.sendSearch(ctx, retry, target, retryTerms)
		retryUntil := time.Now().Add(2 * time.Second)
		if ctxDeadline, ok := ctx.Deadline(); ok && retryUntil.After(ctxDeadline) {
			retryUntil = ctxDeadline
		}
		files = c.collectFiles(ctx, collector, retryUntil, files)
	}
	return files, nil
}

// kadSendGap paces the search datagrams a little: eMule peers are more likely
// to answer a handful of packets spread over a few milliseconds than a burst.
const kadSendGap = 15 * time.Millisecond

func (c *Client) sendSearch(ctx context.Context, contacts []Contact, target ID, terms []string) {
	payload := encodeSearchRequest(target, terms)
	for index, contact := range contacts {
		if index > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(kadSendGap):
			}
		}
		c.send(contact, KadSearchKeyReq, payload)
	}
}

func (c *Client) collectFiles(ctx context.Context, collector *collector, until time.Time, files []SearchFile) []SearchFile {
	for time.Now().Before(until) && len(files) < c.cfg.MaxResults {
		select {
		case <-ctx.Done():
			return files
		case file := <-collector.files:
			files = append(files, file)
		case <-time.After(150 * time.Millisecond):
		}
	}
	return files
}

// lookup walks the Kad network towards target, learning closer contacts. It
// stops as soon as a round finds no closer node or the budget expires.
func (c *Client) lookup(ctx context.Context, target ID, budget time.Duration) {
	deadline := time.Now().Add(budget)
	candidates := c.table.closest(target, 32)
	tried := map[ID]struct{}{}
	for round := 0; round < 4 && time.Now().Before(deadline); round++ {
		channel := c.expectNodes(target)
		sent := 0
		for _, contact := range candidates {
			if sent >= 5 || time.Now().After(deadline) {
				break
			}
			if _, ok := tried[contact.ID]; ok {
				continue
			}
			if sent > 0 {
				select {
				case <-ctx.Done():
					c.dropNodeWaiter(target, channel)
					return
				case <-time.After(kadSendGap):
				}
			}
			tried[contact.ID] = struct{}{}
			c.send(contact, KadReq, encodeNodeRequest(target, contact.ID, byte(c.cfg.NodesPerRequest)))
			sent++
		}
		if sent == 0 {
			break
		}
		roundDeadline := time.Now().Add(1200 * time.Millisecond)
		added := 0
		for time.Now().Before(roundDeadline) && time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				c.dropNodeWaiter(target, channel)
				return
			case contacts := <-channel:
				for _, contact := range contacts {
					if c.table.add(contact, c.self) {
						added++
					}
					candidates = append(candidates, contact)
				}
				sort.Slice(candidates, func(i, j int) bool {
					return Less(Distance(target, candidates[i].ID), Distance(target, candidates[j].ID))
				})
			case <-time.After(1200 * time.Millisecond):
			}
			if len(candidates) > 64 {
				candidates = candidates[:64]
			}
		}
		c.dropNodeWaiter(target, channel)
		if added == 0 && round > 0 {
			break
		}
	}
}

func (c *Client) expectNodes(target ID) chan []Contact {
	channel := make(chan []Contact, 32)
	c.mu.Lock()
	if existing, ok := c.pending[target]; ok {
		c.mu.Unlock()
		return existing
	}
	c.pending[target] = channel
	c.mu.Unlock()
	return channel
}

func (c *Client) dropNodeWaiter(target ID, channel chan []Contact) {
	c.mu.Lock()
	if c.pending[target] == channel {
		delete(c.pending, target)
	}
	c.mu.Unlock()
}

func (c *Client) addCollector(target ID, collector *collector) {
	c.mu.Lock()
	c.searches[target] = append(c.searches[target], collector)
	c.mu.Unlock()
}

func (c *Client) removeCollector(target ID, collector *collector) {
	c.mu.Lock()
	list := c.searches[target]
	for index, item := range list {
		if item == collector {
			list = append(list[:index], list[index+1:]...)
			break
		}
	}
	if len(list) == 0 {
		delete(c.searches, target)
	} else {
		c.searches[target] = list
	}
	c.mu.Unlock()
}

func (c *Client) send(contact Contact, opcode byte, payload []byte) {
	if !contact.valid() {
		return
	}
	c.writeTo(contact.UDPAddr(), opcode, payload)
}

// writeTo sends a packet to an address even when the peer is not (yet) a
// routing-table contact. PING/PONG and the answer to a bootstrap request need
// this: the sender may have no ID we know.
func (c *Client) writeTo(addr *net.UDPAddr, opcode byte, payload []byte) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil || addr == nil {
		return
	}
	_, _ = conn.WriteToUDP(kadPacket(opcode, payload), addr)
}

func (c *Client) readLoop() {
	defer c.wg.Done()
	buffer := make([]byte, 65536)
	for {
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		size, addr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			select {
			case <-c.closeCh:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		if size < 2 || size > 65535 {
			continue
		}
		packet := append([]byte(nil), buffer[:size]...)
		c.handlePacket(packet, addr)
	}
}

func (c *Client) handlePacket(data []byte, addr *net.UDPAddr) {
	opcode, payload, err := splitKadPacket(data)
	if err != nil {
		return
	}
	atomic.AddInt64(&c.peerPackets, 1)
	switch opcode {
	case KadPing:
		c.writeTo(addr, KadPong, encodePong())
	case KadPong:
		return
	case KadHelloReq:
		contact, err := decodeHello(payload)
		if err != nil {
			return
		}
		contact.IP = addr.IP
		contact.UDPPort = uint16(addr.Port)
		c.table.add(contact, c.self)
		c.send(contact, KadHelloRes, encodeHello(c.self, c.cfg.TCPPort))
	case KadHelloRes:
		contact, err := decodeHello(payload)
		if err != nil {
			return
		}
		contact.IP = addr.IP
		contact.UDPPort = uint16(addr.Port)
		c.table.add(contact, c.self)
	case KadBootstrapReq:
		c.writeTo(addr, KadBootstrapRes, encodeBootstrapResponse(c.self, c.cfg.TCPPort, c.table.closest(c.self, 20), 20))
	case KadBootstrapRes:
		responder, contacts, err := decodeBootstrapResponse(payload)
		if err != nil {
			return
		}
		responder.IP = addr.IP
		responder.UDPPort = uint16(addr.Port)
		c.table.add(responder, c.self)
		for _, contact := range contacts {
			c.table.add(contact, c.self)
		}
	case KadReq:
		_, target, receiver, err := decodeNodeRequest(payload)
		if err != nil || receiver != c.self {
			return
		}
		contacts := c.table.closest(target, c.cfg.NodesPerRequest)
		c.writeTo(addr, KadRes, encodeNodeResponse(target, contacts))
	case KadRes:
		target, contacts, err := decodeNodeResponse(payload)
		if err != nil {
			return
		}
		c.mu.Lock()
		channel := c.pending[target]
		c.mu.Unlock()
		if channel != nil {
			select {
			case channel <- contacts:
			default:
			}
		}
	case KadSearchKeyReq:
		target, _, _, err := decodeSearchRequest(payload)
		if err != nil {
			return
		}
		// We do not index files: answer with an empty result so the peer does
		// not keep retrying. The packet still carries our ID and the target,
		// which is what the protocol requires.
		c.writeTo(addr, KadSearchRes, emptySearchResponse(c.self, target))
	case KadSearchSource:
		target, _, _, err := decodeSearchRequest(payload)
		if err != nil {
			return
		}
		c.writeTo(addr, KadSearchRes, emptySearchResponse(c.self, target))
	case KadSearchRes:
		_, target, files, err := decodeSearchResponse(payload)
		if err != nil {
			return
		}
		_, sources, sourceErr := decodeSourceResponse(payload)
		if sourceErr != nil {
			sources = nil
		}
		c.mu.Lock()
		collectors := append([]*collector(nil), c.searches[target]...)
		c.mu.Unlock()
		for _, file := range files {
			for _, collector := range collectors {
				if collector.files == nil {
					continue
				}
				select {
				case collector.files <- file:
				default:
				}
			}
		}
		for _, source := range sources {
			for _, collector := range collectors {
				if collector.sources == nil {
					continue
				}
				select {
				case collector.sources <- source:
				default:
				}
			}
		}
	case KadHelloResAck:
		return
	}
}

func emptySearchResponse(self, target ID) []byte {
	w := newWriter(binary.LittleEndian)
	w.kadID(self)
	w.kadID(target)
	w.u16(0)
	return w.buf
}

// mergeFiles deduplicates by hash, keeping the entry with the higher source
// count. Kad and server results for the same file are the same bytes, so this
// is safe and gives a better source estimate.
func mergeFiles(limit int, groups ...[]SearchFile) []SearchFile {
	byHash := map[string]SearchFile{}
	var order []string
	for _, group := range groups {
		for _, file := range group {
			if file.Name == "" || file.Size <= 0 || !ValidHash(file.Hash) {
				continue
			}
			existing, ok := byHash[file.Hash]
			if !ok {
				byHash[file.Hash] = file
				order = append(order, file.Hash)
				continue
			}
			if file.Sources > existing.Sources {
				existing.Sources = file.Sources
			}
			if existing.Origin != file.Origin {
				existing.Origin = "kad+server"
			}
			byHash[file.Hash] = existing
		}
	}
	out := make([]SearchFile, 0, len(order))
	for _, hash := range order {
		out = append(out, byHash[hash])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Sources != out[j].Sources {
			return out[i].Sources > out[j].Sources
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// filterQueryMatches keeps the results whose name contains every query token.
// Kad returns everything indexed under the keyword, including unrelated files
// that merely share one word; this cuts the visible noise. Callers fall back to
// the unfiltered list when the strict filter would hide everything (a real
// release may spell an episode number differently from a Kad keyword).
func filterQueryMatches(files []SearchFile, query string) []SearchFile {
	out := make([]SearchFile, 0, len(files))
	for _, file := range files {
		if MatchesQuery(file.Name, query) {
			out = append(out, file)
		}
	}
	return out
}

// BestKeyword returns the primary token of a Kad lookup. eMule indexes every
// word of a file name and derives the lookup target from the first keyword;
// we prefer the first real title word, skipping episode markers, quality tags
// and years, so "Silo S04E01" looks up "silo" and not "s04e01".
func BestKeyword(query string) string {
	terms := SearchTerms(query)
	if len(terms) == 0 {
		return ""
	}
	for _, term := range terms {
		if !lookupNoise(term) {
			return term
		}
	}
	return terms[0]
}

// SearchTerms is the token list sent in the restrictive expression tree.
// Stop words are dropped: names often omit them (The.Matrix.1999.mkv has no
// "The"), and requiring one would hide valid results.
func SearchTerms(query string) []string {
	all := QueryTokens(query)
	out := make([]string, 0, len(all))
	for _, token := range all {
		if _, stop := kadStopWords[token]; stop {
			continue
		}
		out = append(out, token)
	}
	return out
}

// broadTerms drops episode, quality and year tokens for the retry: a silent
// round can mean the album/series is indexed under the title but the exact
// "S04E01" token is not in the peer's keyword bucket. The caller still filters
// the result against the full query.
func broadTerms(terms []string) []string {
	out := make([]string, 0, len(terms))
	for _, term := range terms {
		if lookupNoise(term) {
			continue
		}
		out = append(out, term)
	}
	return out
}

var kadStopWords = map[string]struct{}{
	"the": {}, "and": {}, "of": {}, "for": {}, "with": {},
	"di": {}, "da": {}, "il": {}, "lo": {}, "la": {}, "le": {}, "les": {},
	"el": {}, "los": {}, "las": {}, "der": {}, "die": {}, "das": {},
	"und": {}, "et": {}, "de": {}, "del": {}, "della": {}, "dei": {},
	"degli": {}, "un": {}, "una": {}, "uno": {}, "per": {}, "con": {},
	"von": {}, "van": {},
}

var (
	kadEpisodeToken = regexp.MustCompile(`^s\d{1,2}[ ._-]?e\d{1,3}$|^\d{1,2}x\d{1,3}$`)
	kadNoiseToken   = regexp.MustCompile(`^(19|20)\d{2}$|^(2160p|1080p|1080i|720p|576p|480p|4k|bluray|bdrip|brrip|webrip|webdl|web|hdtv|dvdrip|xvid|divx|hevc|x265|x264|h264|h265|aac|ac3|dts|atmos)$`)
)

func lookupNoise(term string) bool {
	return kadEpisodeToken.MatchString(term) || kadNoiseToken.MatchString(term)
}

// QueryTokens returns the distinct usable tokens of a query, used to make
// sure a Kad result actually matches what the caller asked for (Kad returns
// everything indexed under the keyword, which may include unrelated names).
func QueryTokens(query string) []string {
	invalid := " ()[]{}<>,._-!?:;\\/\"'"
	words := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return strings.ContainsRune(invalid, r)
	})
	seen := map[string]struct{}{}
	out := make([]string, 0, len(words))
	for _, word := range words {
		if len([]byte(word)) < 3 {
			continue
		}
		if _, ok := seen[word]; ok {
			continue
		}
		seen[word] = struct{}{}
		out = append(out, word)
	}
	return out
}

func queryHasEpisode(query string) bool {
	for _, token := range SearchTerms(query) {
		if kadEpisodeToken.MatchString(token) {
			return true
		}
	}
	return false
}

// MatchesQuery reports whether a file name contains every searchable token of
// the query. Kad keyword results are indexed per word, not by phrase, so a
// client-side filter is required to avoid showing unrelated files. The token
// list matches SearchTerms, so a name without "The" still matches "The Matrix".
func MatchesQuery(name, query string) bool {
	lower := strings.ToLower(name)
	for _, token := range SearchTerms(query) {
		if kadEpisodeToken.MatchString(token) {
			if !episodeInName(lower, token) {
				return false
			}
			continue
		}
		if !strings.Contains(lower, token) {
			return false
		}
	}
	return true
}

var kadEpisodeInName = regexp.MustCompile(`(?i)s(\d{1,2})[ ._-]?e(\d{1,3})`)
var kadEpisodeXInName = regexp.MustCompile(`(?i)(?:^|[^0-9])(\d{1,2})x(\d{1,3})(?:[^0-9]|$)`)

// episodeInName accepts the common episode spellings: S04E01, S04.E01,
// S04 E01 and 4x01/04x01. It is used both for the query token and for the
// file name.
func episodeInName(name, token string) bool {
	season, episode, ok := parseEpisodeToken(token)
	if !ok {
		return false
	}
	for _, match := range kadEpisodeInName.FindAllStringSubmatch(name, -1) {
		if atoiSmall(match[1]) == season && atoiSmall(match[2]) == episode {
			return true
		}
	}
	for _, match := range kadEpisodeXInName.FindAllStringSubmatch(name, -1) {
		if atoiSmall(match[1]) == season && atoiSmall(match[2]) == episode {
			return true
		}
	}
	return false
}

func parseEpisodeToken(token string) (int, int, bool) {
	if match := regexp.MustCompile(`^s(\d{1,2})[ ._-]?e(\d{1,3})$`).FindStringSubmatch(strings.ToLower(token)); match != nil {
		return atoiSmall(match[1]), atoiSmall(match[2]), true
	}
	if match := regexp.MustCompile(`^(\d{1,2})x(\d{1,3})$`).FindStringSubmatch(strings.ToLower(token)); match != nil {
		return atoiSmall(match[1]), atoiSmall(match[2]), true
	}
	return 0, 0, false
}

func atoiSmall(value string) int {
	n := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}
