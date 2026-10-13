package torrent

import (
	"net"

	"github.com/buzzqw/gextto/internal/gxcore/internal/acceptor"
	"github.com/buzzqw/gextto/internal/gxcore/internal/allocator"
	"github.com/buzzqw/gextto/internal/gxcore/internal/announcer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecedownloader"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piecepicker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/urldownloader"
	"github.com/buzzqw/gextto/internal/gxcore/internal/verifier"
	"github.com/buzzqw/gextto/internal/gxcore/internal/webseedsource"
	"github.com/rcrowley/go-metrics"
)

// start boots a stopped torrent: fresh channels and meters, completion
// reopened for grown selections, then the pipeline matching the torrent's
// state — metadata fetch, v2 layer fetch, direct download, verification or
// allocation. Already-started torrents ignore the call.
func (t *torrent) start() {
	if t.errC != nil {
		return
	}
	if t.stoppedEventAnnouncer != nil {
		t.stoppedEventAnnouncer.Close()
		t.stoppedEventAnnouncer = nil
	}

	t.log.Info("starting torrent")
	t.errC = make(chan error, 1)
	t.portC = make(chan int, 1)
	t.lastError = nil
	t.downloadSpeed = metrics.NewMeter()
	t.uploadSpeed = metrics.NewMeter()
	// A grown selection reopens a completed torrent before peers arrive
	// (they are ignored while it reads as completed).
	t.resetCompletionIfWantedMissing()

	if t.info() == nil {
		t.addFixedPeers()
		t.startAcceptor()
		t.startAnnouncers()
		t.startInfoDownloaders()
		return
	}
	if t.info().NeedsV2Layers() && !t.info().HasPieceHashes() {
		// A v2 torrent (often a resume reloaded magnet) still missing its
		// piece layers: fetch those before touching files.
		t.addFixedPeers()
		t.startAcceptor()
		t.startAnnouncers()
		t.startV2LayerDownload()
		return
	}
	if t.pieces == nil {
		t.startAllocator()
		return
	}
	if t.bitfield == nil {
		t.startVerifier()
		return
	}
	t.addFixedPeers()
	t.startAcceptor()
	t.startAnnouncers()
	t.startPieceDownloaders()
}

// startVerifier verifies with no merge.
func (t *torrent) startVerifier() {
	t.startMergeVerifier(nil, false, nil)
}

// startMergeVerifier runs the verifier, fronted by the swarm merge when
// candidates exist: the merge's reads, hashes and writes run in the verifier
// goroutine off the run loop, while Stats/Stop/peers keep answering here.
// Merged pieces skip re-hashing (Skip, cleared after so a later manual check
// verifies everything); with onlyMerged (storage created empty this run)
// nothing else is even read (Only).
//
// resume carries the pieces a reloaded bitfield already marks verified (nil on
// a fresh add): with it the verifier touches no disk at all — those pieces keep
// their bit (Skip) and, together with the merged ones, are the only accepted
// pieces (Only) — so the resume path can still run the merge and re-derive the
// Phase 3 partial blocks without hashing the whole torrent again.
func (t *torrent) startMergeVerifier(candidates []*Torrent, onlyMerged bool, resume []byte) {
	if t.verifier != nil {
		t.crash("verifier exists")
	}
	if len(t.pieces) == 0 {
		t.crash("zero length pieces")
	}
	// resumed reports the pieces the reloaded bitfield already verified.
	resumed := func(i uint32) bool { return bitSet(resume, int(i)) }
	t.verifier = verifier.New()
	v := t.verifier
	if resume != nil {
		v.Skip = resumed
		v.Only = resumed
	}
	if len(t.mergedPieces) > 0 {
		merged := t.mergedPieces
		t.mergedPieces = nil
		base := v.Skip
		if base == nil {
			base = func(uint32) bool { return false }
		}
		v.Skip = func(i uint32) bool { return base(i) || merged[i] }
		if resume != nil {
			v.Only = v.Skip
		}
	}
	if len(candidates) > 0 {
		v, skip := t.verifier, t.mergeSkips()
		out := &mergeResult{}
		t.mergeOut = out
		v.Prepare = func(stop <-chan struct{}) {
			// The run loop reads *out only after it receives this verifier's
			// result, which happens after Prepare returns.
			*out = t.mergeFrom(candidates, skip, stop)
			merged := out.pieces
			if resume != nil {
				// Resume: keep the reloaded pieces, add the merged ones and
				// still read nothing from disk (Only == Skip).
				v.Skip = func(i uint32) bool { return resumed(i) || merged[i] }
				v.Only = v.Skip
				return
			}
			if len(merged) > 0 {
				v.Skip = func(i uint32) bool { return merged[i] }
			}
			if onlyMerged {
				v.Only = func(i uint32) bool { return merged[i] }
			}
		}
	}
	go t.verifier.Run(t.pieces, t.verifierProgressC, t.verifierResultC)
}

// startAllocator opens the files, routing deselected ones to parts storage
// (no parts storage means no skipping).
func (t *torrent) startAllocator() {
	if t.allocator != nil {
		t.crash("allocator exists")
	}
	t.allocator = allocator.New()
	skip := t.skippedInfoFiles()
	parts := t.partsStorage(skip)
	if parts == nil {
		skip = nil
	}
	go t.allocator.RunSelected(t.info(), t.storage, skip, parts, t.allocatorProgressC, t.allocatorResultC)
}

// addFixedPeers queues the configured static peers.
func (t *torrent) addFixedPeers() {
	for _, pe := range t.fixedPeers {
		_ = t.addPeerString(pe)
	}
}

// startAnnouncers launches tracker announcers once, plus the DHT announcer
// for public swarms when the DHT runs.
func (t *torrent) startAnnouncers() {
	if len(t.announcers) == 0 {
		for _, tr := range t.trackers {
			t.startNewAnnouncer(tr)
		}
	}
	if t.dhtAnnouncer == nil && t.session.config.DHTEnabled && !t.privateSwarm() {
		t.dhtAnnouncer = announcer.NewDHTAnnouncer()
		go t.dhtAnnouncer.Run(t.announceDHT, t.session.config.DHTAnnounceInterval, t.session.config.DHTMinAnnounceInterval, t.log)
	}
}

// startNewAnnouncer attaches one tracker with the session's announce
// telemetry hook (dropped silently once the session is gone).
func (t *torrent) startNewAnnouncer(tr tracker.Tracker) {
	an := announcer.NewPeriodicalAnnouncer(
		tr,
		t.session.config.TrackerNumWant,
		t.session.config.TrackerMinAnnounceInterval,
		t.announcerFields,
		t.completeC,
		t.addrsFromTrackers,
		t.log,
	)
	an.SetResultHook(func(resp *tracker.AnnounceResponse, err error) {
		if t.session == nil {
			return
		}
		ev := t.baseEvent(EventTrackerAnnounce)
		if err != nil {
			ev.Error = err.Error()
		} else {
			ev.Seeders = int(resp.Seeders)
			ev.Leechers = int(resp.Leechers)
		}
		t.session.emit(ev)
	})
	t.announcers = append(t.announcers, an)
	go an.Run()
}

// startAcceptor listens for peers: the shared session port when configured
// (announcing it without blocking), else one socket per torrent.
func (t *torrent) startAcceptor() {
	if t.session.sharedPort() {
		t.port = int(t.session.config.ListenPort)
		select {
		case t.portC <- t.port:
		default:
		}
		return
	}
	if t.acceptor != nil {
		return
	}
	listener, err := listenTCPOn(t.session.config.Host, t.port)
	if err != nil {
		t.log.Warningf("cannot listen port %d: %s", t.port, err)
	} else {
		t.log.Info("Listening peers on tcp://" + listener.Addr().String())
		t.port = listener.Addr().(*net.TCPAddr).Port
		t.portC <- t.port
		t.acceptor = acceptor.New(listener, t.incomingConnC, t.log)
		go t.acceptor.Run()
	}
}

// startInfoDownloaders tops the metadata fetch window, skipping snubbed
// peers.
func (t *torrent) startInfoDownloaders() {
	if t.info() != nil {
		return
	}
	for len(t.infoDownloaders)-len(t.infoDownloadersSnubbed) < t.session.config.ParallelMetadataDownloads {
		id := t.nextInfoDownload()
		if id == nil {
			break
		}
		pe := id.Peer.(*peer.Peer)
		t.infoDownloaders[pe] = id
		id.RequestBlocks(t.maxAllowedRequests(pe))
		id.Peer.(*peer.Peer).ResetSnubTimer()
	}
}

// startPieceDownloaders feeds webseeds first, then every peer. Only
// Downloading torrents fetch.
func (t *torrent) startPieceDownloaders() {
	if t.status() != Downloading {
		return
	}
	for _, src := range t.webseedSources {
		if !src.Downloading() && !src.Disabled {
			if started := t.startPieceDownloaderForWebseed(src); !started {
				break
			}
		}
	}
	for pe := range t.peers {
		t.fillPeerDownloads(pe)
	}
}

// fillPeerDownloads opens pieces for pe up to the multi-piece cap. The
// first piece always opens; further ones only with pipe room, so a piece
// wide enough to fill the request window never reserves a second buffer for
// nothing.
func (t *torrent) fillPeerDownloads(pe *peer.Peer) {
	for n := len(t.pieceDownloaders[pe]); n < maxPiecesPerPeer; n = len(t.pieceDownloaders[pe]) {
		if n > 0 && !t.pipeHasRoom(pe) {
			return
		}
		if !t.startPieceDownloaderFor(pe) {
			return
		}
	}
}

// pipeHasRoom reports a request window with room no current piece can fill:
// every block already requested, fewer in flight than allowed. Only then is
// another piece worth its RAM.
func (t *torrent) pipeHasRoom(pe *peer.Peer) bool {
	pending := 0
	for _, pd := range t.pieceDownloaders[pe] {
		if pd.Remaining() > 0 {
			return false
		}
		pending += pd.Pending()
	}
	return pending < t.maxAllowedRequests(pe)
}

// startPieceDownloaderForWebseed opens one webseed range within the active
// download budget and only while downloading.
func (t *torrent) startPieceDownloaderForWebseed(src *webseedsource.WebseedSource) (started bool) {
	if t.webseedActiveDownloads >= t.session.config.WebseedMaxDownloads {
		return false
	}
	if t.status() != Downloading {
		return false
	}
	sp := t.piecePicker.PickWebseed(src)
	if sp == nil {
		return false
	}
	t.startWebseedDownloader(sp)
	t.webseedActiveDownloads++
	return true
}

// startWebseedDownloader runs one range fetch, binding it to its source
// (fresh meter, enabled) unless already bound.
func (t *torrent) startWebseedDownloader(sp *piecepicker.WebseedDownloadSpec) {
	t.log.Debugf("downloading pieces %d-%d from webseed %s", sp.Begin, sp.End, sp.Source.URL)
	ud := urldownloader.New(sp.Source.URL, sp.Begin, sp.End, t.bucketDownload)
	for _, src := range t.webseedSources {
		if src != sp.Source {
			continue
		}
		if src.Downloader != nil {
			return
		}
		src.Downloader = ud
		src.Disabled = false
		src.LastError = nil
		src.DownloadSpeed = metrics.NewMeter()
		break
	}
	go ud.Run(t.webseedClient, t.pieces, len(t.info().Files) > 1, t.webseedPieceResultC.SendC(), t.piecePool, t.session.config.WebseedResponseBodyReadTimeout)
}

// maxPiecesPerPeer caps one peer's concurrent pieces: past the first, a
// peer whose piece underfills its request budget keeps the pipe full on a
// second piece instead of idling. The extra piece opens only with real pipe
// room, and everything stays bounded by the shared request budget and the
// per-piece RAM reservation.
const maxPiecesPerPeer = 2

// startPieceDownloaderFor opens one more piece for pe: never past the cap,
// never while not downloading, through the RAM budget when one exists (an
// already-queued request waits for its grant on ramNotifyC instead).
func (t *torrent) startPieceDownloaderFor(pe *peer.Peer) bool {
	if t.status() != Downloading {
		return false
	}
	if len(t.pieceDownloaders[pe]) >= maxPiecesPerPeer {
		return false
	}
	if pe.MaxConcurrentPieces == 0 {
		pe.MaxConcurrentPieces = maxPiecesPerPeer
	}
	if t.session.ram == nil {
		return t.startSinglePieceDownloader(pe)
	}
	if pe.RAMQueued {
		return false
	}
	if ok := t.session.ram.Request(t.id, pe, int64(t.info().PieceLength), t.ramNotifyC, pe.Done()); !ok {
		pe.RAMQueued = true
		return false
	}
	return t.startSinglePieceDownloader(pe)
}

// startSinglePieceDownloader picks and opens one piece, refunding the RAM
// reservation when nothing starts. A duplicate opener for the same piece is
// a bug and crashes.
func (t *torrent) startSinglePieceDownloader(pe *peer.Peer) (started bool) {
	defer func() {
		if !started && t.session.ram != nil {
			t.session.ram.Release(int64(t.info().PieceLength))
		}
	}()
	if t.status() != Downloading {
		return false
	}
	pi, allowedFast := t.piecePicker.PickFor(pe)
	if pi == nil {
		return false
	}
	pd := piecedownloader.New(pi, pe, allowedFast, t.piecePool.Get(int(pi.Length)))
	t.prefillBlocks(pd)
	if m := t.pieceDownloaders[pe]; m != nil {
		if _, ok := m[pi.Index]; ok {
			t.crash("peer already has a piece downloader for this piece")
		}
	}
	t.log.Debugf("requesting piece #%d from peer %s", pi.Index, pe.IP())
	if t.pieceDownloaders[pe] == nil {
		t.pieceDownloaders[pe] = make(map[uint32]*piecedownloader.PieceDownloader)
	}
	t.pieceDownloaders[pe][pi.Index] = pd
	pe.ConcurrentPieces++
	pe.Downloading = true
	pe.PipeRefillTried = false
	t.requestBlocks(pe)
	pe.ResetSnubTimer()
	return true
}

// requestBlocks shares one in-flight budget (maxAllowedRequests minus
// everything already pending across the peer's pieces) between those
// pieces, topping each up in turn. It reports pipe room the current pieces
// cannot fill.
func (t *torrent) requestBlocks(pe *peer.Peer) (room bool) {
	m := t.pieceDownloaders[pe]
	budget := t.maxAllowedRequests(pe)
	for _, pd := range m {
		budget -= pd.Pending()
	}
	remaining := 0
	for _, pd := range m {
		if budget > 0 {
			// RequestBlocks caps at the piece's own pending count, so the
			// shared remainder stacks on top of its in-flight requests.
			budget -= pd.RequestBlocks(pd.Pending() + budget)
		}
		remaining += pd.Remaining()
	}
	return budget > 0 && remaining == 0
}

// maxAllowedRequests is the peer's in-flight target: initialized from
// DefaultRequestsOut, clamped into [1, cap].
func (t *torrent) maxAllowedRequests(pe *peer.Peer) int {
	cap := t.requestQueueCap(pe)
	if pe.RequestQueue <= 0 {
		pe.RequestQueue = min(t.session.config.DefaultRequestsOut, cap)
	}
	if pe.RequestQueue > cap {
		pe.RequestQueue = cap
	}
	if pe.RequestQueue < 1 {
		pe.RequestQueue = 1
	}
	return pe.RequestQueue
}

// requestQueueCap hard-caps in-flight requests: the peer's advertised reqq
// when smaller, else MaxRequestsOut, at least 1.
func (t *torrent) requestQueueCap(pe *peer.Peer) int {
	cap := t.session.config.MaxRequestsOut
	if pe.ExtensionHandshake != nil && pe.ExtensionHandshake.RequestQueue > 0 && pe.ExtensionHandshake.RequestQueue < cap {
		cap = pe.ExtensionHandshake.RequestQueue
	}
	if cap < 1 {
		cap = 1
	}
	return cap
}

// boostRequestQueue grows the in-flight target by one toward the cap as the
// peer delivers: the adaptive depth that lets fast peers slide to
// MaxRequestsOut instead of sitting at DefaultRequestsOut.
func (t *torrent) boostRequestQueue(pe *peer.Peer) {
	cap := t.requestQueueCap(pe)
	if pe.RequestQueue <= 0 {
		pe.RequestQueue = min(t.session.config.DefaultRequestsOut, cap)
	}
	if pe.RequestQueue < cap {
		pe.RequestQueue++
	}
}

// resetRequestQueue drops the target to the base after a choke or stall, so
// a quiet peer keeps no deep queue.
func (t *torrent) resetRequestQueue(pe *peer.Peer) {
	pe.RequestQueue = min(t.session.config.DefaultRequestsOut, t.requestQueueCap(pe))
}

// prefillBlocks hands the downloader the blocks of its piece that the swarm
// merge already wrote from a local source (Phase 3): they are read back from
// disk and never requested. A block that cannot be read is simply requested.
// The entry stays until the piece completes, so endgame duplicates reuse it
// too.
func (t *torrent) prefillBlocks(pd *piecedownloader.PieceDownloader) {
	begins := t.partialBlocks[pd.Piece.Index]
	if len(begins) == 0 {
		return
	}
	lengths := make(map[uint32]uint32, len(begins))
	for _, b := range pd.Piece.CalculateBlocks() {
		lengths[b.Begin] = b.Length
	}
	for _, begin := range begins {
		length, ok := lengths[begin]
		if !ok {
			continue
		}
		data := pd.Buffer.Data[begin : begin+length]
		if _, err := pd.Piece.Data.ReadAt(data, int64(begin)); err != nil {
			continue
		}
		pd.Prefill(begin, data)
	}
}
