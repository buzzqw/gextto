// Package torrent implements a BitTorrent session: torrents, DHT, caches
// and the transports they share.
package torrent

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anacrolix/utp"
	"github.com/buzzqw/gextto/internal/dht"
	"github.com/buzzqw/gextto/internal/gxcore/internal/bandwidth"
	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/blocklist"
	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/netx"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piececache"
	"github.com/buzzqw/gextto/internal/gxcore/internal/resolver"
	"github.com/buzzqw/gextto/internal/gxcore/internal/resourcemanager"
	"github.com/buzzqw/gextto/internal/gxcore/internal/resumer/boltdbresumer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/semaphore"
	"github.com/buzzqw/gextto/internal/gxcore/internal/storage"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/trackermanager"
	"go.etcd.io/bbolt"
	berrors "go.etcd.io/bbolt/errors"
)

// Resume database layout: a session bucket for globals (blocklist cache)
// and one sub-bucket per torrent id under torrents.
var (
	sessionBucket         = []byte("session")
	torrentsBucket        = []byte("torrents")
	blocklistKey          = []byte("blocklist")
	blocklistTimestampKey = []byte("blocklist-timestamp")
	blocklistURLHashKey   = []byte("blocklist-url-hash")
)

// Session shares one DHT node, tracker transports, caches, budgets and
// listeners across its torrents.
type Session struct {
	config         Config
	db             *bbolt.DB
	storage        storage.Provider
	resumer        *boltdbresumer.Resumer
	log            logger.Logger
	extensions     [8]byte
	dht            *dht.DHT
	rpc            *rpcServer
	trackerManager *trackermanager.TrackerManager
	ram            *resourcemanager.ResourceManager[*peer.Peer]
	pieceCache     *piececache.Cache
	webseedClient  http.Client
	createdAt      time.Time
	semWrite       *semaphore.Semaphore
	metrics        *sessionMetrics
	// bucketDownload/bucketUpload are retuned live by SetSpeedLimits.
	bucketDownload *bandwidth.Limiter
	bucketUpload   *bandwidth.Limiter
	closeC         chan struct{}

	// bannedIPs quarantines corrupt-data peers session-wide with a TTL
	// (libtorrent-style smart ban), consulted by every torrent.
	mBannedIPs sync.Mutex
	bannedIPs  map[string]time.Time

	// storageErrors counts storage-layer piece-write failures (full disk,
	// unwritable file, broken mount) since start.
	storageErrors atomic.Int64

	// Event subscribers: the mutex guards the map only.
	subMu   sync.Mutex
	subs    map[int]chan Event
	subNext int

	// detachedAnnouncers tracks "stopped" announcers of closed torrents
	// still finishing in the background.
	detachedAnnouncers sync.WaitGroup

	mPeerRequests   sync.Mutex
	dhtPeerRequests map[*torrent]struct{}

	mTorrents          sync.RWMutex
	torrents           map[string]*Torrent
	torrentsByInfoHash map[dht.InfoHash][]*Torrent
	invalidTorrentIDs  []string

	mPorts         sync.RWMutex
	availablePorts map[int]struct{}
	listener       *net.TCPListener
	utpSocket      *utp.Socket

	mBlocklist         sync.RWMutex
	blocklist          *blocklist.Blocklist
	blocklistTimestamp time.Time
}

// NewSession builds a session from cfg: validated egress, resume database,
// shared UDP socket, DHT node, transports, caches, blocklist, listener,
// stored torrents, RPC and background loops, in that dependency order. Any
// failure unwinds what was opened and reports it.
func NewSession(cfg Config) (*Session, error) {
	outbound, err := egressSettings(cfg)
	if err != nil {
		return nil, err
	}
	netx.Configure(outbound)
	if cfg.MaxOpenFiles > 0 {
		if err := setNoFile(cfg.MaxOpenFiles); err != nil {
			return nil, errors.New("cannot change max open files limit: " + err.Error())
		}
	}
	if cfg.CustomLogHandler != nil {
		logger.SetHandler(cfg.CustomLogHandler)
	}
	if cfg.Debug {
		logger.SetDebug()
	}

	cfg.Database = os.ExpandEnv(cfg.Database)
	cfg.DataDir = os.ExpandEnv(cfg.DataDir)
	if err := os.MkdirAll(filepath.Dir(cfg.Database), os.ModeDir|cfg.FilePermissions); err != nil {
		return nil, err
	}
	l := logger.New("session")
	db, err := openResumeDB(cfg)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			db.Close()
		}
	}()
	ids, err := listStoredTorrents(db)
	if err != nil {
		return nil, err
	}
	res, err := boltdbresumer.New(db, torrentsBucket)
	if err != nil {
		return nil, err
	}
	utpSocket, err := openSharedSocket(cfg)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil && utpSocket != nil {
			netx.SetUTPDialer(nil, false)
			utpSocket.Close()
		}
	}()
	var dhtNode *dht.DHT
	if cfg.DHTEnabled {
		if dhtNode, err = startDHTNode(cfg, utpSocket); err != nil {
			return nil, err
		}
		defer func() {
			if err != nil {
				dhtNode.Stop()
			}
		}()
	}
	c := assembleSession(cfg, l, db, res, utpSocket, dhtNode)
	if err = c.startBlocklistReloader(); err != nil {
		return nil, err
	}
	c.advertiseExtensions()
	c.initMetrics()
	if err = c.startSharedListener(); err != nil {
		return nil, err
	}
	c.loadExistingTorrents(ids)
	if c.config.RPCEnabled {
		c.rpc = newRPCServer(c)
		if err = c.rpc.Start(c.config.RPCHost, c.config.RPCPort); err != nil {
			return nil, err
		}
	}
	if cfg.DHTEnabled {
		go c.processDHTResults()
	}
	go c.updateStatsLoop()
	return c, nil
}

// egressSettings validates the port range and the proxy URL (scheme and
// host) into dialer settings.
func egressSettings(cfg Config) (netx.Settings, error) {
	if cfg.ListenPort == 0 && cfg.PortBegin >= cfg.PortEnd {
		return netx.Settings{}, errors.New("invalid port range")
	}
	outbound := netx.Settings{Interface: strings.TrimSpace(cfg.OutgoingInterface)}
	raw := strings.TrimSpace(cfg.Proxy)
	if raw == "" {
		return outbound, nil
	}
	proxyURL, err := url.Parse(raw)
	if err != nil || proxyURL.Host == "" {
		return outbound, errors.New("invalid proxy URL")
	}
	switch proxyURL.Scheme {
	case "socks5", "socks5h", "http", "https":
	default:
		return outbound, errors.New("proxy must be socks5:// or http://")
	}
	outbound.Proxy = proxyURL
	return outbound, nil
}

// openResumeDB opens the resume database, translating a lock timeout into
// the "another process" error.
func openResumeDB(cfg Config) (*bbolt.DB, error) {
	db, err := bbolt.Open(cfg.Database, cfg.FilePermissions&^0111, &bbolt.Options{Timeout: time.Second})
	if err == berrors.ErrTimeout {
		return nil, errors.New("resume database is locked by another process")
	} else if err != nil {
		return nil, err
	}
	return db, nil
}

// listStoredTorrents ensures the buckets and returns every stored torrent id.
func listStoredTorrents(db *bbolt.DB) ([]string, error) {
	var ids []string
	err := db.Update(func(tx *bbolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(sessionBucket); err != nil {
			return err
		}
		b, err := tx.CreateBucketIfNotExists(torrentsBucket)
		if err != nil {
			return err
		}
		return b.ForEach(func(k, _ []byte) error {
			ids = append(ids, string(k))
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// openSharedSocket opens the single UDP socket for uTP and the DHT (like
// libtorrent), wiring the uTP dialer to it. Without UTP (or with a proxy,
// which cannot speak UDP) there is no socket and the dialer stays direct.
func openSharedSocket(cfg Config) (*utp.Socket, error) {
	netx.SetUTPDialer(nil, false)
	if !(cfg.UTP && cfg.ListenPort > 0 && strings.TrimSpace(cfg.Proxy) == "") {
		return nil, nil
	}
	host := cfg.DHTHost
	if host == "" {
		host = cfg.Host
	}
	pc, err := listenUDPOn(host, int(cfg.ListenPort))
	if err != nil {
		return nil, err
	}
	utpSocket, err := utp.NewSocketFromPacketConn(pc)
	if err != nil {
		pc.Close()
		return nil, err
	}
	netx.SetUTPDialer(func(ctx context.Context, addr string) (net.Conn, error) {
		return utpSocket.DialContext(ctx, "udp", addr)
	}, cfg.UTPOnly)
	return utpSocket, nil
}

// startDHTNode configures and starts the DHT: socket family follows the
// shared socket when there is one (BEP 32 dual-stack mapping included),
// else the configured host (blank for a self-bound dual-stack socket);
// bootstrap routers, no routing-table persistence, no target peers; secure
// node id from the public IP when known (BEP 42); read-only on request
// (BEP 43).
func startDHTNode(cfg Config, utpSocket *utp.Socket) (*dht.DHT, error) {
	dhtConfig := dht.NewConfig()
	dhtConfig.Address = cfg.DHTHost
	dhtConfig.Port = int(cfg.DHTPort)
	dhtConfig.UDPProto = udpProtoForHost(cfg.DHTHost)
	if utpSocket != nil {
		dhtConfig.PacketConn = utpSocket
		dhtConfig.UDPProto = udpProtoForAddr(utpSocket.LocalAddr())
	} else if dhtConfig.UDPProto == "udp" {
		dhtConfig.Address = ""
	}
	dhtConfig.DHTRouters = strings.Join(cfg.DHTBootstrapNodes, ",")
	dhtConfig.SaveRoutingTable = false
	dhtConfig.NumTargetPeers = 0
	dhtConfig.PublicIP = publicIPForConfig(cfg)
	dhtConfig.ReadOnly = cfg.DHTReadOnly
	dhtNode, err := dht.New(dhtConfig)
	if err != nil {
		return nil, err
	}
	if err := dhtNode.Start(); err != nil {
		return nil, err
	}
	return dhtNode, nil
}

// assembleSession builds the Session with its transports, caches, budgets
// and registries; the storage defaults to files, the limiters to the
// configured KiB/s floors.
func assembleSession(cfg Config, l logger.Logger, db *bbolt.DB, res *boltdbresumer.Resumer, utpSocket *utp.Socket, dhtNode *dht.DHT) *Session {
	ports := make(map[int]struct{})
	for p := cfg.PortBegin; p < cfg.PortEnd; p++ {
		ports[int(p)] = struct{}{}
	}
	bl := blocklist.NewLogger(l.Errorf)
	var blTracker *blocklist.Blocklist
	if cfg.BlocklistEnabledForTrackers {
		blTracker = bl
	}
	c := &Session{
		config:             cfg,
		db:                 db,
		resumer:            res,
		blocklist:          bl,
		trackerManager:     trackermanager.New(blTracker, cfg.DNSResolveTimeout, !cfg.TrackerHTTPVerifyTLS),
		log:                l,
		torrents:           make(map[string]*Torrent),
		torrentsByInfoHash: make(map[dht.InfoHash][]*Torrent),
		availablePorts:     ports,
		utpSocket:          utpSocket,
		dht:                dhtNode,
		pieceCache:         piececache.New(cfg.ReadCacheSize, cfg.ReadCacheTTL, cfg.ParallelReads),
		ram:                resourcemanager.New[*peer.Peer](cfg.WriteCacheSize),
		createdAt:          time.Now(),
		semWrite:           semaphore.New(int(cfg.ParallelWrites)),
		closeC:             make(chan struct{}),
		bannedIPs:          make(map[string]time.Time),
		webseedClient:      webseedHTTPClient(cfg, bl),
	}
	if cfg.CustomStorage != nil {
		c.storage = cfg.CustomStorage
	} else {
		c.storage = newFileStorageProvider(&cfg)
	}
	c.bucketDownload = bandwidth.New(max(cfg.SpeedLimitDownload, 0) * 1024)
	c.bucketUpload = bandwidth.New(max(cfg.SpeedLimitUpload, 0) * 1024)
	if cfg.DHTEnabled {
		c.dhtPeerRequests = make(map[*torrent]struct{})
	}
	return c
}

// webseedHTTPClient fetches through the session resolver with the SSRF
// guard (no LAN unless allowed) and the configured timeouts and TLS policy.
func webseedHTTPClient(cfg Config, bl *blocklist.Blocklist) http.Client {
	return http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				ip, port, err := resolver.Resolve(ctx, addr, cfg.DNSResolveTimeout, bl)
				if err != nil {
					return nil, err
				}
				if !cfg.WebSourcesAllowPrivate {
					if err := netx.CheckPublicTCPAddr(ip, port); err != nil {
						return nil, err
					}
				}
				var d net.Dialer
				taddr := &net.TCPAddr{IP: ip, Port: port}
				dctx, cancel := context.WithTimeout(ctx, cfg.WebseedDialTimeout)
				defer cancel()
				return netx.DialContext(dctx, d, network, taddr.String())
			},
			TLSHandshakeTimeout:   cfg.WebseedTLSHandshakeTimeout,
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: !cfg.WebseedVerifyTLS}, // nolint: gosec
			ResponseHeaderTimeout: cfg.WebseedResponseHeaderTimeout,
		},
	}
}

// advertiseExtensions sets our handshake reserved bits: fast (BEP 6) and
// extension protocol (BEP 10) always, DHT (BEP 5) when it runs.
func (c *Session) advertiseExtensions() {
	ext, err := bitfield.NewBytes(c.extensions[:], 64)
	if err != nil {
		panic(err)
	}
	ext.Set(61)
	ext.Set(43)
	if c.config.DHTEnabled {
		ext.Set(63)
	}
}

// parseTrackers builds the tracker clients tier by tier, skipping
// unparsable URLs.
func (s *Session) parseTrackers(tiers [][]string, private bool) []tracker.Tracker {
	ret := make([]tracker.Tracker, 0, len(tiers))
	for _, tier := range tiers {
		trackers := make([]tracker.Tracker, 0, len(tier))
		for _, tr := range tier {
			t, err := s.trackerManager.Get(tr, s.config.TrackerHTTPTimeout, s.getTrackerUserAgent(private), int64(s.config.TrackerHTTPMaxResponseSize))
			if err != nil {
				continue
			}
			trackers = append(trackers, t)
		}
		if len(trackers) > 0 {
			ret = append(ret, tracker.NewTier(trackers))
		}
	}
	return ret
}

// getTrackerUserAgent masks the client string on private torrents.
func (s *Session) getTrackerUserAgent(private bool) string {
	if private {
		return s.config.TrackerHTTPPrivateUserAgent
	}
	return trackerHTTPPublicUserAgent
}

// Close stops everything in dependency order: torrents in parallel (each
// announces stopped within its timeout), RPC, budgets, caches, detached
// announcers, transports, meters, and the database last.
func (s *Session) Close() error {
	close(s.closeC)
	if s.listener != nil {
		s.listener.Close()
	}

	// The DHT reads the shared uTP socket: close it first so its reader
	// returns and Stop can finish. CloseNow (not Close) because open uTP
	// connections end only later with their torrents; the DHT reader itself
	// is released via a read deadline.
	if s.utpSocket != nil {
		netx.SetUTPDialer(nil, false)
		_ = s.utpSocket.SetReadDeadline(time.Now())
		s.utpSocket.CloseNow()
	}
	if s.config.DHTEnabled {
		s.dht.Stop()
	}

	s.updateStats()

	var wg sync.WaitGroup
	s.mTorrents.Lock()
	wg.Add(len(s.torrents))
	for _, t := range s.torrents {
		go func(t *Torrent) {
			t.torrent.Close()
			wg.Done()
		}(t)
	}
	s.torrents = nil
	s.mTorrents.Unlock()
	wg.Wait()

	if s.rpc != nil {
		if err := s.rpc.Stop(s.config.RPCShutdownTimeout); err != nil {
			s.log.Errorln("cannot stop RPC server:", err.Error())
		}
	}

	s.ram.Close()
	s.pieceCache.Close()
	// Announcers share trackerManager's transports; their timeout bounds this.
	s.detachedAnnouncers.Wait()
	s.trackerManager.Close()
	s.metrics.Close()
	return s.db.Close()
}

// ListTorrents snapshots the registered torrents, in random order.
func (s *Session) ListTorrents() []*Torrent {
	s.mTorrents.RLock()
	defer s.mTorrents.RUnlock()
	torrents := make([]*Torrent, 0, len(s.torrents))
	for _, t := range s.torrents {
		torrents = append(torrents, t)
	}
	return torrents
}

// BanIP quarantines a peer IP session-wide for ttl.
func (s *Session) BanIP(ip string, ttl time.Duration) {
	if ip == "" {
		return
	}
	s.mBannedIPs.Lock()
	if s.bannedIPs == nil {
		s.bannedIPs = make(map[string]time.Time)
	}
	s.bannedIPs[ip] = time.Now().Add(ttl)
	s.mBannedIPs.Unlock()
}

// IsBannedIP reports a live ban, lazily expiring stale ones on read.
func (s *Session) IsBannedIP(ip string) bool {
	s.mBannedIPs.Lock()
	defer s.mBannedIPs.Unlock()
	expiry, ok := s.bannedIPs[ip]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		delete(s.bannedIPs, ip)
		return false
	}
	return true
}

// getPort hands out a torrent port: the shared one when configured, else
// the next free port from the range.
func (s *Session) getPort() (int, error) {
	if s.sharedPort() {
		return int(s.config.ListenPort), nil
	}
	s.mPorts.Lock()
	defer s.mPorts.Unlock()
	for p := range s.availablePorts {
		delete(s.availablePorts, p)
		return p, nil
	}
	return 0, errors.New("no free port")
}

// releasePort returns a port to the pool (a no-op on the shared port).
func (s *Session) releasePort(port int) {
	if s.sharedPort() {
		return
	}
	s.mPorts.Lock()
	defer s.mPorts.Unlock()
	s.availablePorts[port] = struct{}{}
}

// GetTorrent finds a torrent by id, or nil.
func (s *Session) GetTorrent(id string) *Torrent {
	s.mTorrents.RLock()
	defer s.mTorrents.RUnlock()
	return s.torrents[id]
}

// RemoveTorrent drops the torrent and, unless keepData, its files. The
// stopped announce goes out in the background; removal does not wait for it.
func (s *Session) RemoveTorrent(id string, keepData bool) error {
	t, err := s.removeTorrentFromClient(id)
	if t != nil {
		err = s.stopAndRemoveData(t, keepData)
	}
	return err
}

// removeTorrentFromClient unregisters the torrent and deletes its resume
// record. The DHT un-announce runs lock-free: it messages the DHT loop,
// which itself takes mTorrents on replies.
func (s *Session) removeTorrentFromClient(id string) (*Torrent, error) {
	s.mTorrents.Lock()
	t, ok := s.torrents[id]
	if !ok {
		s.mTorrents.Unlock()
		return nil, nil
	}
	t.torrent.log.Info("removing torrent")
	delete(s.torrents, id)

	ih := dht.InfoHash(t.torrent.InfoHash())
	a := s.torrentsByInfoHash[ih]
	for i, it := range a {
		if it == t {
			a[i] = a[len(a)-1]
			s.torrentsByInfoHash[ih] = a[:len(a)-1]
			break
		}
	}
	lastWithInfoHash := len(s.torrentsByInfoHash[ih]) == 0
	s.mTorrents.Unlock()

	if s.config.DHTEnabled && lastWithInfoHash {
		s.dht.RemoveInfoHash(string(ih))
	}
	return t, s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(torrentsBucket).DeleteBucket([]byte(id))
	})
}

// stopAndRemoveData closes the torrent, frees its port and, unless
// keepData, deletes its data dir (namespaced by id, or by info name
// otherwise).
func (s *Session) stopAndRemoveData(t *Torrent, keepData bool) error {
	t.torrent.Close()
	s.releasePort(t.torrent.port)
	if keepData {
		return nil
	}
	var err error
	var dest string
	if s.config.DataDirIncludesTorrentID {
		dest = filepath.Join(s.config.DataDir, t.torrent.id)
	} else if t.torrent.info() != nil {
		dest = filepath.Join(s.config.DataDir, t.torrent.info().Name)
	}
	if dest != "" {
		if err = os.RemoveAll(dest); err != nil {
			s.log.Errorf("cannot remove torrent data. err: %s dest: %s", err, dest)
		}
	}
	return err
}

// StartAll persists the started flag on every torrent, then starts them off
// the registry lock (Start blocks until the run loop receives).
func (s *Session) StartAll() error {
	err := s.db.Update(func(tx *bbolt.Tx) error {
		tb := tx.Bucket(torrentsBucket)
		s.mTorrents.RLock()
		for _, t := range s.torrents {
			b := tb.Bucket([]byte(t.torrent.id))
			_ = b.Put([]byte("started"), []byte("true"))
		}
		defer s.mTorrents.RUnlock()
		return nil
	})
	if err != nil {
		return err
	}
	s.mTorrents.RLock()
	torrents := make([]*Torrent, 0, len(s.torrents))
	for _, t := range s.torrents {
		torrents = append(torrents, t)
	}
	s.mTorrents.RUnlock()
	for _, t := range torrents {
		t.torrent.Start()
	}
	return nil
}

// StopAll persists the cleared flag on every torrent, then stops them off
// the registry lock.
func (s *Session) StopAll() error {
	err := s.db.Update(func(tx *bbolt.Tx) error {
		tb := tx.Bucket(torrentsBucket)
		s.mTorrents.RLock()
		for _, t := range s.torrents {
			b := tb.Bucket([]byte(t.torrent.id))
			_ = b.Put([]byte("started"), []byte("false"))
		}
		defer s.mTorrents.RUnlock()
		return nil
	})
	if err != nil {
		return err
	}
	s.mTorrents.RLock()
	torrents := make([]*Torrent, 0, len(s.torrents))
	for _, t := range s.torrents {
		torrents = append(torrents, t)
	}
	s.mTorrents.RUnlock()
	for _, t := range torrents {
		t.torrent.Stop()
	}
	return nil
}
