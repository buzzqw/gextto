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

func (t *torrent) start() {
	// Do not start if already started.
	if t.errC != nil {
		return
	}

	// Stop announcing Stopped event if in "Stopping" state.
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
	// a selection that grew reopens a completed torrent before
	// peers arrive (they are ignored while it counts as completed).
	t.resetCompletionIfWantedMissing()

	if t.info() != nil {
		if t.info().NeedsV2Layers() && !t.info().HasPieceHashes() {
			// A v2 torrent (usually a magnet reloaded from resume) whose piece
			// layers are still missing: fetch them before allocating files.
			t.addFixedPeers()
			t.startAcceptor()
			t.startAnnouncers()
			t.startV2LayerDownload()
		} else if t.pieces != nil {
			if t.bitfield != nil {
				t.addFixedPeers()
				t.startAcceptor()
				t.startAnnouncers()
				t.startPieceDownloaders()
			} else {
				t.startVerifier()
			}
		} else {
			t.startAllocator()
		}
	} else {
		t.addFixedPeers()
		t.startAcceptor()
		t.startAnnouncers()
		t.startInfoDownloaders()
	}
}

func (t *torrent) startVerifier() {
	t.startMergeVerifier(nil, false)
}

// startMergeVerifier starts the verifier and, with candidates, runs the swarm
// merge first inside the verifier goroutine: the run loop keeps answering
// Stats/Stop/peers while pieces are read, hashed and copied. The merged pieces
// are then not hashed again (Skip); with onlyMerged (storage created empty in
// this run) nothing else is read either (Only), since nothing else can be valid.
func (t *torrent) startMergeVerifier(candidates []*Torrent, onlyMerged bool) {
	if t.verifier != nil {
		t.crash("verifier exists")
	}
	if len(t.pieces) == 0 {
		t.crash("zero length pieces")
	}
	t.verifier = verifier.New()
	// Pieces the swarm merge already re-verified need no second read+hash: hand
	// the set to the verifier and clear it, so a later manual re-check verifies
	// everything.
	if len(t.mergedPieces) > 0 {
		merged := t.mergedPieces
		t.mergedPieces = nil
		t.verifier.Skip = func(i uint32) bool { return merged[i] }
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

func (t *torrent) addFixedPeers() {
	for _, pe := range t.fixedPeers {
		_ = t.addPeerString(pe)
	}
}

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

func (t *torrent) startAcceptor() {
	if t.session.sharedPort() {
		// The session listens for everyone on the shared port.
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

func (t *torrent) startPieceDownloaders() {
	if t.status() != Downloading {
		return
	}
	for _, src := range t.webseedSources {
		if !src.Downloading() && !src.Disabled {
			started := t.startPieceDownloaderForWebseed(src)
			if !started {
				break
			}
		}
	}
	for pe := range t.peers {
		t.fillPeerDownloads(pe)
	}
}

// fillPeerDownloads starts pieces for the peer until it reaches the
// multi-piece cap (maxPiecesPerPeer) or no piece is available. A peer always
// gets a first piece; an extra one only when the pipe has room (pipeHasRoom),
// so a piece large enough to keep the request window full does not reserve a
// second piece buffer for nothing.
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

// pipeHasRoom reports whether the peer's request window has room that its
// current pieces cannot fill: every block is already requested and fewer than
// maxAllowedRequests are in flight. Only then is a further piece worth its RAM.
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

// maxPiecesPerPeer is how many pieces of one torrent a peer may download at the
// same time. Above one, a peer whose current piece has fewer blocks than its
// request budget keeps the pipe full on a second piece instead of idling; the
// extra piece is opened only when the pipe really has room (pipeHasRoom), and
// the service stays bounded by the shared request budget and by the RAM
// reserved per piece.
const maxPiecesPerPeer = 2

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
		// A request is already waiting for this peer: its grant arrives on
		// ramNotifyC and starts the piece then.
		return false
	}
	ok := t.session.ram.Request(t.id, pe, int64(t.info().PieceLength), t.ramNotifyC, pe.Done())
	if !ok {
		pe.RAMQueued = true
		return false
	}
	return t.startSinglePieceDownloader(pe)
}

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

// requestBlocks refills the peer's in-flight block requests across all of its
// pieces, sharing one budget (maxAllowedRequests) between them so multi-piece
// pipelining does not multiply the in-flight window: the budget is what is
// left after the requests already pending on every piece. It reports whether
// the window still has room that no current piece can fill (pipeHasRoom).
func (t *torrent) requestBlocks(pe *peer.Peer) (room bool) {
	m := t.pieceDownloaders[pe]
	budget := t.maxAllowedRequests(pe)
	for _, pd := range m {
		budget -= pd.Pending()
	}
	remaining := 0
	for _, pd := range m {
		if budget > 0 {
			// RequestBlocks caps the piece's own pending count, so the
			// shared budget is added on top of what it already has in flight.
			budget -= pd.RequestBlocks(pd.Pending() + budget)
		}
		remaining += pd.Remaining()
	}
	return budget > 0 && remaining == 0
}

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

// requestQueueCap is the hard cap on in-flight requests for the peer: its
// advertised reqq (if any) or MaxRequestsOut, never above MaxRequestsOut.
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

// boostRequestQueue raises the peer's in-flight target by one, up to the cap, as
// the peer keeps delivering blocks. This is the adaptive part of the request
// queue depth: a fast peer slides towards MaxRequestsOut instead of staying at
// the fixed DefaultRequestsOut.
func (t *torrent) boostRequestQueue(pe *peer.Peer) {
	cap := t.requestQueueCap(pe)
	if pe.RequestQueue <= 0 {
		pe.RequestQueue = min(t.session.config.DefaultRequestsOut, cap)
	}
	if pe.RequestQueue < cap {
		pe.RequestQueue++
	}
}

// resetRequestQueue lowers the target back to the base after a choke (or a
// stall), so a peer that stops delivering is not kept with a deep queue.
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
