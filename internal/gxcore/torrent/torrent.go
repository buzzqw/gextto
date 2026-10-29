package torrent

import (
	"crypto/rand"
	"errors"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/acceptor"
	"github.com/buzzqw/gextto/internal/gxcore/internal/addrlist"
	"github.com/buzzqw/gextto/internal/gxcore/internal/allocator"
	"github.com/buzzqw/gextto/internal/gxcore/internal/announcer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/bandwidth"
	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/blocklist"
	"github.com/buzzqw/gextto/internal/gxcore/internal/bufferpool"
	"github.com/buzzqw/gextto/internal/gxcore/internal/externalip"
	"github.com/buzzqw/gextto/internal/gxcore/internal/handshaker/incominghandshaker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/handshaker/outgoinghandshaker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/infodownloader"
	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/merkle"
	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
	"github.com/buzzqw/gextto/internal/gxcore/internal/mse"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/pexlist"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecedownloader"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecepicker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecewriter"
	"github.com/buzzqw/gextto/internal/gxcore/internal/resumer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/storage"
	"github.com/buzzqw/gextto/internal/gxcore/internal/suspendchan"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/unchoker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/urldownloader"
	"github.com/buzzqw/gextto/internal/gxcore/internal/verifier"
	"github.com/buzzqw/gextto/internal/gxcore/internal/webseedsource"
	"github.com/rcrowley/go-metrics"
)

// torrent is one download: its metadata, files, pieces, peers and every
// worker driving them. All mutable state belongs to the run() goroutine;
// other goroutines reach it through commands, queries, or the documented
// locks/atomics on individual fields.
type torrent struct {
	session *Session
	id      string
	addedAt time.Time

	// Identity: info hash, trackers, magnet extras, display name.
	infoHash    [20]byte
	trackers    []tracker.Tracker
	rawTrackers [][]string
	fixedPeers  []string
	name        string
	// displayName is the rename override (qBittorrent torrents/rename), empty
	// when the torrent was never renamed. nameMu guards it: it is read from
	// several goroutines.
	displayName string
	nameMu      sync.RWMutex

	// hasV2Hint remembers a btmh-identified magnet while its info dict is
	// unknown; Info.HasV2 rules once known.
	hasV2Hint bool

	// selectOnly is the engine-side BEP 53 "so" seed (non-padding indices),
	// authoritative until Config.FileSelection speaks. The daemon may clear
	// it live, hence the mutex.
	selectOnly   []int
	selectOnlyMu sync.Mutex

	// Storage, port and metadata. infoPtr is atomic: published once by the
	// run loop, read lock-free from anywhere — never touch the field.
	storage storage.Storage
	port    int
	infoPtr atomic.Pointer[metainfo.Info]

	// pieceLayers carries the v2 top-level layers for export and serving.
	pieceLayers map[string][]byte

	// BEP 52 hash-fetch state: v2 magnets learn per-file layers from peers
	// before any piece builds or verifies.
	v2LayerFiles  []*v2LayerFile
	v2LayerByRoot map[string]*v2LayerFile
	v2Trees       map[string]*merkle.LayerTree
	// v2BlockTrees caches leaf-layer trees built off-loop from verified
	// data, for answering block-level hash requests; builds in flight
	// refuse, failures cool down. v2BlockMu guards bookkeeping only.
	v2BlockTrees  map[string]*v2BlockTreeEntry
	v2BlockBytes  int64
	v2BlockMu     sync.Mutex
	v2BlockBusy   map[string]bool
	v2BlockFailed map[string]time.Time
	v2Pending     map[*peer.Peer]*v2HashRequest
	v2NoHashPeers map[*peer.Peer]struct{}

	// Verified pieces, set only after bytes hit storage. mBitfield guards
	// announcer-side reads against the run loop.
	bitfield  *bitfield.Bitfield
	mBitfield sync.RWMutex

	// Per-torrent peer id.
	peerID [20]byte

	// Files, pieces and the picker.
	files       []allocator.File
	pieces      []piece.Piece
	piecePicker *piecepicker.PiecePicker
	// filePaths is the persisted override of the storage paths (nil = none),
	// written to the resume on the full-spec writes and applied to the info
	// before allocation. Set by the resume loader and by the rename methods.
	filePaths []string
	// contentLayout is the qBittorrent content layout chosen at add time
	// (content_layout.go); a magnet applies it when its metadata arrives.
	contentLayout string

	// Seed mode (torrent_seedmode.go): the data is trusted without the full
	// check and each piece is hashed the first time it is served.
	// seedVerified holds the pieces already hashed, seedPending the requests
	// waiting for a hash, seedHashing the hashes in flight; seedGen discards
	// results from an earlier seed-mode session, and seedRecheck restarts the
	// torrent through the verification after a failed piece.
	seedMode     bool
	seedVerified *bitfield.Bitfield
	seedPending  map[uint32][]seedModeRequest
	seedHashing  map[uint32]struct{}
	seedHashC    chan seedHashResult
	seedGen      uint64
	seedRecheck  bool

	// Peer pumps and registries.
	peerDisconnectedC chan *peer.Peer
	pieceMessagesC    *suspendchan.Chan[peer.PieceMessage]
	messages          chan peer.Message
	peers             map[*peer.Peer]struct{}
	incomingPeers     map[*peer.Peer]struct{}
	outgoingPeers     map[*peer.Peer]struct{}

	// recentlySeen seeds underpopulated PEX lists.
	recentlySeen pexlist.RecentlySeen

	unchoker *unchoker.Unchoker

	// pieceDownloaders maps each peer to its active piece fetches by index.
	// Multi-piece pipelining allows several per peer (see maxPiecesPerPeer).
	// Run loop only.
	pieceDownloaders map[*peer.Peer]map[uint32]*piecedownloader.PieceDownloader

	peerSnubbedC chan *peer.Peer

	// Metadata (BEP 9) fetches, plus the snubbed subset.
	infoDownloaders        map[*peer.Peer]*infodownloader.InfoDownloader
	infoDownloadersSnubbed map[*peer.Peer]*infodownloader.InfoDownloader

	pieceWriterResultC chan *piecewriter.PieceWriter

	// completeC closes on verified completion; completeMetadataC on info
	// arrival.
	completeC         chan struct{}
	completeMetadataC chan struct{}

	// completed latches once every wanted piece verifies.
	completed bool

	// errC carries the fatal error out; lastError remembers it.
	errC      chan error
	lastError error

	// portC reports the listening port after start.
	portC chan int

	// Run-loop lifecycle: closeC stops, doneC confirms exit, commandC feeds
	// API closures (see torrent_commands.go for the senders).
	closeC   chan struct{}
	doneC    chan struct{}
	commandC chan func()

	// Peer supply: tracker and DHT addresses, candidate pool, raw and routed
	// sockets, duplicate-id registry, listener.
	addrsFromTrackers chan []*net.TCPAddr
	addrList          *addrlist.AddrList
	incomingConnC     chan net.Conn
	routedConnC       chan *incominghandshaker.IncomingHandshaker
	peerIDs           map[[20]byte]struct{}
	acceptor          *acceptor.Acceptor

	// sKeyHash feeds the MSE stream-key lookup.
	sKeyHash [20]byte

	// Tracker and DHT announcers plus their result channels.
	announcers            []*announcer.PeriodicalAnnouncer
	stoppedEventAnnouncer *announcer.StopAnnouncer
	dhtAnnouncer          *announcer.DHTAnnouncer
	dhtPeersC             chan []*net.TCPAddr

	// Handshake tracking: live handshakers and their result channels.
	incomingHandshakers       map[*incominghandshaker.IncomingHandshaker]struct{}
	outgoingHandshakers       map[*outgoinghandshaker.OutgoingHandshaker]struct{}
	incomingHandshakerResultC chan *incominghandshaker.IncomingHandshaker
	outgoingHandshakerResultC chan *outgoinghandshaker.OutgoingHandshaker

	// infoDownloaderResultC is unused by the current metadata flow (kept for
	// the channel inventory).
	infoDownloaderResultC chan *infodownloader.InfoDownloader

	// unchokeTicker paces the 10 s unchoke round.
	unchokeTicker *time.Ticker

	// Allocation and verification workers with progress/result channels and
	// their counters.
	allocator          *allocator.Allocator
	allocatorProgressC chan allocator.Progress
	allocatorResultC   chan *allocator.Allocator
	bytesAllocated     int64
	verifier           *verifier.Verifier
	verifierProgressC  chan verifier.Progress
	verifierResultC    chan *verifier.Verifier
	checkedPieces      uint32

	// localData snapshots pieces+storage for sibling-torrent Swarm Merging
	// reads off the run loop (phase 1). See localmerge.go.
	localData atomic.Pointer[localDataSnapshot]

	// syncingSince is when the running data flush started (UnixNano), 0 when
	// none is running. The health check reads it off the run loop to tell a
	// slow disk from a stuck loop.
	syncingSince atomic.Int64

	// mergedPieces holds merge re-verified pieces for the verifier to skip
	// (consumed by startVerifier). See localmerge.go.
	mergedPieces map[uint32]bool
	// partialBlocks are, per piece, the 16 KiB blocks the swarm merge wrote
	// from a partially verified v2 source (Phase 3, localmerge.go): the piece
	// downloader takes them from disk. Dropped when the piece completes or
	// fails its hash. Run loop only.
	partialBlocks map[uint32][]uint32
	// mergeOut receives the merge result from the verifier goroutine; the run
	// loop reads it once the verifier is done (handleVerificationDone).
	mergeOut *mergeResult
	// laterMergeC is a coalescing wake-up: a sibling that has just published
	// new local data pings it so this torrent can merge it too, when it is
	// already downloading (the allocation-time merge only saw the siblings
	// that existed then). Run loop only. See localmerge.go.
	laterMergeC chan struct{}
	// mergeWaiting/mergePending hold a later merge that is draining the
	// in-flight piece writes before it can run (a piece adopting mid-merge
	// would be lost by the verifier's bitfield swap). mergeRecheck re-runs the
	// candidate scan after a verification/merge that a request arrived during.
	// Run loop only.
	mergeWaiting bool
	mergePending []*Torrent
	mergeRecheck bool
	// notifyCount is the verified-piece count at the last sibling wake-up: a
	// torrent only notifies when it has gained verified pieces since, so two
	// torrents cannot ping each other forever with no-op merges. Run loop only.
	notifyCount uint32

	// skipBits marks deselected-file pieces (nil when none): interest and
	// usefulness ignore them. Run loop only.
	skipBits *bitfield.Bitfield

	// pendingWrites bounds the write ring (maxPendingWrites): the network
	// flows during writes instead of stopping the world. Run loop only.
	pendingWrites int

	// Meters and counters.
	downloadSpeed   metrics.Meter
	uploadSpeed     metrics.Meter
	bytesDownloaded metrics.Counter
	bytesUploaded   metrics.Counter
	bytesWasted     metrics.Counter
	seededFor       metrics.Counter

	seedDurationUpdatedAt time.Time
	seedDurationTicker    *time.Ticker

	// connectedPeerIPs prevents duplicate connections per IP.
	connectedPeerIPs map[string]struct{}

	// seedPeerIPs remembers the peers that answered "both seeds" while
	// seeding: they have nothing to ask for, so a seeding torrent that dials
	// candidates to find leechers must not knock on them twice.
	seedPeerIPs map[string]struct{}

	// bannedPeerIPs holds corrupt-data peers.
	bannedPeerIPs map[string]struct{}

	// holepunchAttempted allows one BEP 55 rendezvous per endpoint, so dead
	// dials cannot flood relays.
	holepunchAttempted map[string]struct{}

	// announcersStoppedC fires when the stopped announcer finishes; buffered
	// so a detached one never blocks.
	announcersStoppedC chan struct{}

	// piecePool recycles piece buffers off the GC.
	piecePool *bufferpool.Pool

	// externalIP seeds BEP 40 peer priority from local interfaces, refreshed
	// from BEP 10 "yourip" fields.
	externalIP net.IP

	// ramNotifyC wakes RAM-granted piece starts.
	ramNotifyC chan *peer.Peer

	// Webseeds: shared client, sources, results, retries and the active
	// download count.
	webseedClient          *http.Client
	webseedSources         []*webseedsource.WebseedSource
	rawWebseedSources      []string
	webseedPieceResultC    *suspendchan.Chan[*urldownloader.PieceResult]
	webseedRetryC          chan *webseedsource.WebseedSource
	webseedActiveDownloads int

	// doVerify requests a manual re-verification on next stop.
	doVerify bool

	// stopAfterDownload/stopAfterMetadata auto-stop at those milestones.
	stopAfterDownload bool
	stopAfterMetadata bool

	// sequential/firstLast order the picks (streaming-friendly).
	sequential bool
	firstLast  bool

	// superSeeding arms BEP 16: while seeding, one advertised piece at a
	// time, served only then, so the swarm spreads data. Opt-in seeding
	// strategy; never touches queue, seed policy or bandwidth.
	superSeeding bool

	// superSeedPeers tracks per-peer super-seeding, populated only while
	// seeding with the flag on. Run goroutine owned.
	superSeedPeers map[*peer.Peer]*superSeedPeer

	// Per-torrent limiters inherit the session ones until explicit limits
	// land; peers hold these, so changes apply without reconnecting.
	bucketDownload *bandwidth.Limiter
	bucketUpload   *bandwidth.Limiter
	// bucketLocalDownload/bucketLocalUpload are held by the peers on the local
	// network: unlimited, or forwarding to the torrent limiters when the
	// session applies the limits to LAN peers (applyLocalPeerLimits).
	bucketLocalDownload *bandwidth.Limiter
	bucketLocalUpload   *bandwidth.Limiter
	// downloadLimitKib/uploadLimitKib are explicit KiB/s caps (-1 inherits).
	downloadLimitKib int64
	uploadLimitKib   int64
	// maxConnections/maxUploads cap peers and upload slots (0 = session
	// budgets only).
	maxConnections int
	maxUploads     int

	// completeCmdRun remembers the fired completion hook.
	completeCmdRun bool

	log logger.Logger
}

// newTorrent builds a torrent: the add paths (resume load, .torrent add,
// magnet add) converge here. Channels, maps, meters and registries start
// empty; info, bitfield, stats and options arrive as arguments.
func newTorrent(
	s *Session,
	id string,
	addedAt time.Time,
	infoHash []byte,
	sto storage.Storage,
	name string,
	port int,
	trackers []tracker.Tracker,
	fixedPeers []string,
	info *metainfo.Info,
	bf *bitfield.Bitfield,
	stats resumer.Stats,
	ws []*webseedsource.WebseedSource,
	stopAfterDownload bool,
	stopAfterMetadata bool,
	completeCmdRun bool,
	sequential bool,
	firstLast bool,
	superSeeding bool,
) (*torrent, error) {
	if len(infoHash) != 20 {
		return nil, errors.New("invalid infoHash (must be 20 bytes)")
	}
	cfg := s.config
	var ih [20]byte
	copy(ih[:], infoHash)
	t := &torrent{
		session:                   s,
		id:                        id,
		addedAt:                   addedAt,
		infoHash:                  ih,
		trackers:                  trackers,
		fixedPeers:                fixedPeers,
		name:                      name,
		storage:                   sto,
		port:                      port,
		bitfield:                  bf,
		log:                       logger.New("torrent " + id),
		peerDisconnectedC:         make(chan *peer.Peer),
		messages:                  make(chan peer.Message),
		pieceMessagesC:            suspendchan.New[peer.PieceMessage](0),
		peers:                     make(map[*peer.Peer]struct{}),
		incomingPeers:             make(map[*peer.Peer]struct{}),
		outgoingPeers:             make(map[*peer.Peer]struct{}),
		pieceDownloaders:          make(map[*peer.Peer]map[uint32]*piecedownloader.PieceDownloader),
		peerSnubbedC:              make(chan *peer.Peer),
		infoDownloaders:           make(map[*peer.Peer]*infodownloader.InfoDownloader),
		infoDownloadersSnubbed:    make(map[*peer.Peer]*infodownloader.InfoDownloader),
		pieceWriterResultC:        make(chan *piecewriter.PieceWriter),
		seedHashC:                 make(chan seedHashResult),
		completeC:                 make(chan struct{}),
		completeMetadataC:         make(chan struct{}),
		closeC:                    make(chan struct{}),
		commandC:                  make(chan func()),
		addrsFromTrackers:         make(chan []*net.TCPAddr),
		peerIDs:                   make(map[[20]byte]struct{}),
		incomingConnC:             make(chan net.Conn),
		routedConnC:               make(chan *incominghandshaker.IncomingHandshaker),
		sKeyHash:                  mse.HashSKey(ih[:]),
		infoDownloaderResultC:     make(chan *infodownloader.InfoDownloader),
		incomingHandshakers:       make(map[*incominghandshaker.IncomingHandshaker]struct{}),
		outgoingHandshakers:       make(map[*outgoinghandshaker.OutgoingHandshaker]struct{}),
		incomingHandshakerResultC: make(chan *incominghandshaker.IncomingHandshaker),
		outgoingHandshakerResultC: make(chan *outgoinghandshaker.OutgoingHandshaker),
		allocatorProgressC:        make(chan allocator.Progress),
		allocatorResultC:          make(chan *allocator.Allocator),
		verifierProgressC:         make(chan verifier.Progress),
		verifierResultC:           make(chan *verifier.Verifier),
		connectedPeerIPs:          make(map[string]struct{}),
		seedPeerIPs:               make(map[string]struct{}),
		bannedPeerIPs:             make(map[string]struct{}),
		holepunchAttempted:        make(map[string]struct{}),
		announcersStoppedC:        make(chan struct{}, 1),
		dhtPeersC:                 make(chan []*net.TCPAddr, 1),
		externalIP:                externalip.FirstExternalIP(),
		downloadSpeed:             metrics.NilMeter{},
		uploadSpeed:               metrics.NilMeter{},
		bytesDownloaded:           metrics.NewCounter(),
		bytesUploaded:             metrics.NewCounter(),
		bytesWasted:               metrics.NewCounter(),
		seededFor:                 metrics.NewCounter(),
		ramNotifyC:                make(chan *peer.Peer),
		laterMergeC:               make(chan struct{}, 1),
		webseedClient:             &s.webseedClient,
		webseedSources:            ws,
		webseedPieceResultC:       suspendchan.New[*urldownloader.PieceResult](0),
		webseedRetryC:             make(chan *webseedsource.WebseedSource),
		doneC:                     make(chan struct{}),
		stopAfterDownload:         stopAfterDownload,
		stopAfterMetadata:         stopAfterMetadata,
		completeCmdRun:            completeCmdRun,
		sequential:                sequential,
		firstLast:                 firstLast,
		superSeeding:              superSeeding,
		superSeedPeers:            make(map[*peer.Peer]*superSeedPeer),
	}
	t.finishConstruction(s, cfg, info, stats)
	n := t.copyPeerIDPrefix()
	if _, err := rand.Read(t.peerID[n:]); err != nil {
		return nil, err
	}
	t.unchoker = unchoker.New(cfg.UnchokedPeers, cfg.OptimisticUnchokedPeers)
	go t.run()
	return t, nil
}

// finishConstruction publishes info, inherits the session limiters, caps an
// overlong webseed list, restores counters, and wires the address pool and
// piece pool.
func (t *torrent) finishConstruction(s *Session, cfg Config, info *metainfo.Info, stats resumer.Stats) {
	t.setInfo(info)
	t.bucketDownload = bandwidth.New(0)
	t.bucketDownload.SetParent(s.bucketDownload)
	t.bucketUpload = bandwidth.New(0)
	t.bucketUpload.SetParent(s.bucketUpload)
	t.bucketLocalDownload = bandwidth.New(0)
	t.bucketLocalUpload = bandwidth.New(0)
	t.applyLocalPeerLimits(s.limitLocalPeers.Load())
	t.downloadLimitKib = -1
	t.uploadLimitKib = -1
	if len(t.webseedSources) > s.config.WebseedMaxSources {
		t.webseedSources = t.webseedSources[:10]
	}
	t.bytesDownloaded.Inc(stats.BytesDownloaded)
	t.bytesUploaded.Inc(stats.BytesUploaded)
	t.bytesWasted.Inc(stats.BytesWasted)
	t.seededFor.Inc(stats.SeededFor)
	var blocklistForOutgoingConns *blocklist.Blocklist
	if cfg.BlocklistEnabledForOutgoingConnections {
		blocklistForOutgoingConns = s.blocklist
	}
	t.addrList = addrlist.New(cfg.MaxPeerAddresses, blocklistForOutgoingConns, t.port, &t.externalIP)
	if t.info() != nil {
		t.piecePool = bufferpool.New(int(t.info().PieceLength))
	}
}

// copyPeerIDPrefix stamps the peer id prefix (masked on private torrents),
// returning the random tail's offset.
func (t *torrent) copyPeerIDPrefix() int {
	if t.info() != nil && t.info().Private {
		return copy(t.peerID[:], t.session.config.PrivatePeerIDPrefix)
	}
	prefix := t.session.config.PublicPeerIDPrefix
	if prefix == "" {
		prefix = publicPeerIDPrefix
	}
	return copy(t.peerID[:], prefix)
}

// getPeersForUnchoker snapshots the peers for the unchoke round.
func (t *torrent) getPeersForUnchoker() []unchoker.Peer {
	peers := make([]unchoker.Peer, 0, len(t.peers))
	for pe := range t.peers {
		peers = append(peers, pe)
	}
	return peers
}

// Name is the display name: the rename override when there is one, otherwise
// the name the torrent was added with.
func (t *torrent) Name() string {
	if n := t.renamedName(); n != "" {
		return n
	}
	return t.name
}

// renamedName is the rename override, "" when the torrent was never renamed.
func (t *torrent) renamedName() string {
	t.nameMu.RLock()
	defer t.nameMu.RUnlock()
	return t.displayName
}

// info reads the published metadata (nil for magnets in flight). Every
// reader — run loop, daemon, announcers, Status — goes through the atomic
// pointer: never the field.
func (t *torrent) info() *metainfo.Info {
	return t.infoPtr.Load()
}

// setInfo publishes parsed metadata from the run loop.
func (t *torrent) setInfo(info *metainfo.Info) {
	t.infoPtr.Store(info)
}

// Collections exposes the BEP 38 collection hints (nil-safe copy).
func (t *torrent) Collections() []string {
	if t.info() == nil {
		return nil
	}
	return append([]string(nil), t.info().Collections...)
}

// Similar exposes the BEP 38 similar hashes (nil-safe copy).
func (t *torrent) Similar() [][20]byte {
	if t.info() == nil {
		return nil
	}
	return append([][20]byte(nil), t.info().Similar...)
}

// InfoHash copies the 20-byte identity out.
func (t *torrent) InfoHash() []byte {
	b := make([]byte, 20)
	copy(b, t.infoHash[:])
	return b
}

// Dir is the storage root.
func (t *torrent) Dir() string {
	return t.storage.RootDir()
}

// Files lists non-padding files; it fails before metadata arrives.
func (t *torrent) Files() ([]File, error) {
	if t.info() == nil {
		return nil, errors.New("torrent metadata not ready")
	}
	files := make([]File, 0, len(t.info().Files))
	for _, f := range t.info().Files {
		if !f.Padding {
			files = append(files, File{
				path:   f.Path,
				length: f.Length,
			})
		}
	}
	return files, nil
}

// FileStats reports per-file completion: from live pieces while running,
// from the verified bitfield while stopped (parked or abandoned packs stay
// inspectable without restarting).
func (t *torrent) FileStats() ([]FileStats, error) {
	if len(t.pieces) == 0 {
		return t.fileStatsFromBitfield()
	}

	files, err := t.Files()
	if err != nil {
		return nil, err
	}
	fileComp := make(map[string]int64)
	for _, p := range t.pieces {
		if p.Done {
			for _, d := range p.Data {
				if !d.Padding {
					fileComp[d.Name] += d.Length
				}
			}
		}
	}
	stats := make([]FileStats, 0, len(files))
	for _, f := range files {
		stats = append(stats,
			FileStats{
				File:           f,
				BytesCompleted: fileComp[f.path],
			})
	}
	return stats, nil
}

// FileStats pairs a file with its completed bytes.
type FileStats struct {
	File
	BytesCompleted int64
}

// File is one torrent file: storage path and length.
type File struct {
	path   string
	length int64
}

// Path is the storage-relative path.
func (f File) Path() string {
	return f.path
}

// Length is the file size.
func (f File) Length() int64 {
	return f.length
}

// announceDHT registers the torrent for DHT peer duty.
func (t *torrent) announceDHT() {
	t.session.mPeerRequests.Lock()
	t.session.dhtPeerRequests[t] = struct{}{}
	t.session.mPeerRequests.Unlock()
}

// privateSwarm disables non-tracker discovery (DHT, PEX, LSD) on private
// torrents (BEP 27). Unknown metadata reads as public; a private magnet
// stops at info arrival.
func (t *torrent) privateSwarm() bool {
	return t.info() != nil && t.info().Private
}

// DisableLogging silences console logging; call before NewSession.
func DisableLogging() {
	logger.Disable()
}

// fileStatsFromBitfield derives per-file completion from the verified
// bitfield for pieces-less (stopped) torrents.
func (t *torrent) fileStatsFromBitfield() ([]FileStats, error) {
	t.mBitfield.RLock()
	defer t.mBitfield.RUnlock()
	if t.info() == nil || t.bitfield == nil || t.info().PieceLength == 0 {
		return nil, errors.New("torrent not running so file stats unavailable")
	}
	return fileStatsFromPieces(t.info().Files, int64(t.info().PieceLength), t.bitfield.Test), nil
}

// fileStatsFromPieces lays files end to end (padding included in the piece
// stream) and counts completed bytes per non-padding file.
func fileStatsFromPieces(files []metainfo.File, pieceLength int64, done func(uint32) bool) []FileStats {
	stats := make([]FileStats, 0, len(files))
	var offset int64
	for _, f := range files {
		start, end := offset, offset+f.Length
		offset = end
		if f.Padding {
			continue
		}
		var completed int64
		if f.Length > 0 {
			for index := start / pieceLength; index*pieceLength < end; index++ {
				if !done(uint32(index)) {
					continue
				}
				pieceStart, pieceEnd := index*pieceLength, (index+1)*pieceLength
				completed += min(pieceEnd, end) - max(pieceStart, start)
			}
		}
		stats = append(stats, FileStats{File: File{path: f.Path, length: f.Length}, BytesCompleted: completed})
	}
	return stats
}
