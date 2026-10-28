package piecepicker

import (
	"fmt"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
	"github.com/buzzqw/gextto/internal/gxcore/internal/sliceset"
	"github.com/buzzqw/gextto/internal/gxcore/internal/webseedsource"
	"github.com/rcrowley/go-metrics"
)

/*

Piece selection weighs, in order: completion, in-flight writes, peer
availability, choke state, allowed-fast grants, existing requests, webseed
reservations, endgame (everything requested), stalled peers, and the
sequential/streaming orders. Keep this list current: any new stage in
findPiece belongs here.

*/

// PiecePicker decides which piece a peer (or webseed) downloads next and
// tracks who has, wants and stalls what.
type PiecePicker struct {
	webseedSources       []*webseedsource.WebseedSource
	pieces               []myPiece
	piecesByAvailability []*myPiece
	piecesByStalled      []*myPiece
	maxDuplicateDownload int
	maxWebseedPieces     int
	available            uint32

	// gapScratch, gapOrder and gapCounts are reused by every peer pick while
	// a webseed downloads (pickLastPieceOfSmallestGap), which used to allocate
	// and sort a gap list per pick.
	gapScratch []Range
	gapOrder   []Range
	gapCounts  []int
	endgame    bool

	// highPriority counts the high-priority pieces (RefreshPriorities), so
	// pickHigh skips its full scan when there are none, the usual case.
	highPriority int

	// sequential picks index order over rarest-first.
	sequential bool

	// firstLast picks both file ends first, then rarest-first. Independent
	// of sequential.
	firstLast bool

	// streamBegin/streamEnd is the player's current window: first pick.
	// Equal bounds clear it.
	streamBegin, streamEnd uint32

	// readaheadBegin/readaheadEnd is the likely-next window: second pick,
	// independent of the stream window.
	readaheadBegin, readaheadEnd uint32
}

// myPiece is a piece plus its swarm bookkeeping.
type myPiece struct {
	*piece.Piece
	// holders counts the connected peers that have the piece. Who they are
	// is in each peer's bitfield (heldBy); a holder list cost one pointer per
	// piece and peer (35 MB of heap with 150 peers on three ISOs).
	holders   uint32
	Requested sliceset.SliceSet[peer.Peer]
	Snubbed   sliceset.SliceSet[peer.Peer]
	Choked    sliceset.SliceSet[peer.Peer]

	// RequestedWebseed reserves the piece for a webseed source.
	RequestedWebseed *webseedsource.WebseedSource

	// FileHead/FileTail mark pieces holding a file's first or last edge
	// bytes. Set in sequential-ish orders only.
	FileHead, FileTail bool
}

// RunningDownloads counts the actively served fetches, excluding stalled ones.
func (p *myPiece) RunningDownloads() int {
	return p.Requested.Len() - p.StalledDownloads()
}

// StalledDownloads counts fetches stuck on snubbed or choked peers.
func (p *myPiece) StalledDownloads() int {
	return p.Snubbed.Len() + p.Choked.Len()
}

// AvailableForWebseed allows a webseed fetch: unfinished, unreserved,
// wanted pieces only.
func (p *myPiece) AvailableForWebseed() bool {
	if p.Done || p.Writing || p.Skip {
		return false
	}
	return p.RequestedWebseed == nil
}

// PickableBy allows an immediate peer request: wanted, unrequested, and
// held by the peer.
func (p *myPiece) PickableBy(pe *peer.Peer) bool {
	if p.Done || p.Writing || p.Skip {
		return false
	}
	if p.Requested.Len() > 0 {
		return false
	}
	return p.heldBy(pe)
}

// heldBy reports whether pe has the piece, in O(1) from the peer's bitfield:
// HandleHave sets the bit and counts the holder, HandleDontHave and
// HandleDisconnect clear and uncount it, and the bitfields start empty,
// sized to the torrent, when the picker is built.
func (p *myPiece) heldBy(pe *peer.Peer) bool {
	return pe.Bitfield != nil && pe.Bitfield.Has(p.Index)
}

// HighPriority marks pieces of high-priority files (5..7): fetched ahead
// of the rest, right after the player windows.
func (p *myPiece) HighPriority() bool {
	return !p.Skip && p.Priority >= piece.PriorityHigh
}

// New builds a picker over pieces. Sequential/firstLast arm the file-edge
// flags up front (SetOrder maintains them later).
func New(pieces []piece.Piece, maxDuplicateDownload int, webseedSources []*webseedsource.WebseedSource, sequential, firstLast bool) *PiecePicker {
	ps := make([]myPiece, len(pieces))
	for i := range pieces {
		ps[i] = myPiece{Piece: &pieces[i]}
	}
	byAvailability := make([]*myPiece, len(ps))
	byStalled := make([]*myPiece, len(ps))
	for i := range byAvailability {
		byAvailability[i] = &ps[i]
		byStalled[i] = &ps[i]
	}
	// One HTTP range covers ~5% of the pieces (BEP 19 sizing).
	maxWebseedPieces := len(pieces) / 20
	if maxWebseedPieces == 0 {
		maxWebseedPieces = 1
	}
	if sequential || firstLast {
		markFileEdges(ps)
	}
	picker := &PiecePicker{
		pieces:               ps,
		piecesByAvailability: byAvailability,
		piecesByStalled:      byStalled,
		maxDuplicateDownload: maxDuplicateDownload,
		maxWebseedPieces:     maxWebseedPieces,
		webseedSources:       webseedSources,
		sequential:           sequential,
		firstLast:            firstLast,
	}
	picker.RefreshPriorities()
	return picker
}

// maxFileEdgeSize caps the file-end span fetched ahead: enough for an MP4
// moov atom or AVI index on long recordings (qBittorrent reserves 1% of the
// file for the same purpose).
const maxFileEdgeSize = 8 * 1024 * 1024

// fileEdgeSize spans 1% of the file, capped, minimum one byte.
func fileEdgeSize(fileSize int64) int64 {
	return max(min(fileSize/100, maxFileEdgeSize), 1)
}

// markFileEdges flags pieces touching either end of a real (non-padding)
// file.
func markFileEdges(pieces []myPiece) {
	sizes := make(map[string]int64)
	for i := range pieces {
		for _, sec := range pieces[i].Data {
			if !sec.Padding {
				sizes[sec.Name] = max(sizes[sec.Name], sec.Offset+sec.Length)
			}
		}
	}
	for i := range pieces {
		for _, sec := range pieces[i].Data {
			if sec.Padding {
				continue
			}
			n := fileEdgeSize(sizes[sec.Name])
			if sec.Offset < n {
				pieces[i].FileHead = true
			}
			if sec.Offset+sec.Length > sizes[sec.Name]-n {
				pieces[i].FileTail = true
			}
		}
	}
}

// SetOrder switches the piece order live, (re)computing edge flags when an
// order turns on. Flags may linger after both turn off; nothing consults
// them then.
func (p *PiecePicker) SetOrder(sequential, firstLast bool) {
	if (sequential || firstLast) && !p.sequential && !p.firstLast {
		markFileEdges(p.pieces)
	}
	p.sequential = sequential
	p.firstLast = firstLast
}

// clampWindow bounds a player window to the piece list; inverted bounds
// clear it.
func clampWindow(begin, end uint32, total int) (uint32, uint32) {
	if end > uint32(total) {
		end = uint32(total)
	}
	if begin > end {
		begin = end
	}
	return begin, end
}

// SetStreamWindow prioritizes [begin, end): the player's current reads.
func (p *PiecePicker) SetStreamWindow(begin, end uint32) {
	p.streamBegin, p.streamEnd = clampWindow(begin, end, len(p.pieces))
}

// pickStreaming takes the first pickable piece in the stream window.
func (p *PiecePicker) pickStreaming(pe *peer.Peer) *myPiece {
	return firstPickable(p.pieces, p.streamBegin, p.streamEnd, pe)
}

// SetReadaheadWindow prioritizes [begin, end) after the stream window: the
// player's likely-next reads, independent of it.
func (p *PiecePicker) SetReadaheadWindow(begin, end uint32) {
	p.readaheadBegin, p.readaheadEnd = clampWindow(begin, end, len(p.pieces))
}

// pickReadahead takes the first pickable piece in the readahead window.
func (p *PiecePicker) pickReadahead(pe *peer.Peer) *myPiece {
	return firstPickable(p.pieces, p.readaheadBegin, p.readaheadEnd, pe)
}

// firstPickable scans [begin, end) for a piece the peer can take now.
func firstPickable(pieces []myPiece, begin, end uint32, pe *peer.Peer) *myPiece {
	for i := begin; i < end; i++ {
		if i >= uint32(len(pieces)) {
			return nil
		}
		if mp := &pieces[i]; mp.PickableBy(pe) {
			return mp
		}
	}
	return nil
}

// pickHigh takes the rarest high-priority piece the peer can take.
func (p *PiecePicker) pickHigh(pe *peer.Peer) *myPiece {
	if p.highPriority == 0 {
		return nil
	}
	var picked *myPiece
	for i := range p.pieces {
		mp := &p.pieces[i]
		if !mp.HighPriority() || !mp.PickableBy(pe) {
			continue
		}
		if picked == nil || mp.holders < picked.holders {
			picked = mp
		}
	}
	return picked
}

// PieceDownloading reports an in-flight piece (for diagnostics).
func (p *PiecePicker) PieceDownloading(i uint32) bool {
	if i >= uint32(len(p.pieces)) {
		return false
	}
	mp := &p.pieces[i]
	return mp.Writing || mp.Requested.Len() > 0 || mp.RequestedWebseed != nil
}

// CloseWebseedDownloader releases a source's pieces and stops its meter and
// fetcher. A piece claimed by another source is a bug and panics.
func (p *PiecePicker) CloseWebseedDownloader(src *webseedsource.WebseedSource) {
	src.DownloadSpeed.Stop()
	src.DownloadSpeed = metrics.NilMeter{}
	if src.Downloader == nil {
		return
	}
	for i := src.Downloader.Begin; i < src.Downloader.End; i++ {
		if p.pieces[i].RequestedWebseed != src {
			panic(fmt.Sprintf("invalid source in piece: %d", i))
		}
		p.pieces[i].RequestedWebseed = nil
	}
	src.Downloader.Close()
	src.Downloader = nil
}

// AddWebseedSource registers a source live, without aliasing the caller's
// slice.
func (p *PiecePicker) AddWebseedSource(src *webseedsource.WebseedSource) {
	next := make([]*webseedsource.WebseedSource, 0, len(p.webseedSources)+1)
	next = append(next, p.webseedSources...)
	p.webseedSources = append(next, src)
}

// RemoveWebseedSource unregisters a source. Close its downloader first so
// its pieces release.
func (p *PiecePicker) RemoveWebseedSource(src *webseedsource.WebseedSource) {
	kept := make([]*webseedsource.WebseedSource, 0, len(p.webseedSources))
	for _, s := range p.webseedSources {
		if s != src {
			kept = append(kept, s)
		}
	}
	p.webseedSources = kept
}

// WebseedStopAt truncates a source's range at i, releasing the tail pieces.
// Truncating past the read cursor closes the downloader and reports it. A
// foreign-claimed tail piece is a bug and panics.
func (p *PiecePicker) WebseedStopAt(src *webseedsource.WebseedSource, i uint32) (closed bool) {
	oldEnd := src.Downloader.End
	for j := i; j < oldEnd; j++ {
		if p.pieces[j].RequestedWebseed != src {
			panic(fmt.Sprintf("invalid source in piece #%d: %s", j, p.pieces[j].RequestedWebseed.URL))
		}
		p.pieces[j].RequestedWebseed = nil
	}
	src.Downloader.UpdateEnd(i)
	if src.Downloader.ReadCurrent() >= i {
		p.CloseWebseedDownloader(src)
		return true
	}
	return false
}

// Available counts distinct swarm pieces.
func (p *PiecePicker) Available() uint32 {
	return p.available
}

// RequestedPeers lists the peers fetching piece i.
func (p *PiecePicker) RequestedPeers(i uint32) []*peer.Peer {
	return p.pieces[i].Requested.Items
}

// RequestedWebseedSource names the webseed fetching piece i, if any.
func (p *PiecePicker) RequestedWebseedSource(i uint32) *webseedsource.WebseedSource {
	return p.pieces[i].RequestedWebseed
}

// HandleHave records peer availability of piece i.
func (p *PiecePicker) HandleHave(pe *peer.Peer, i uint32) {
	if pe.Bitfield.Test(i) {
		return // already counted: the bit and the count move together
	}
	pe.Bitfield.Set(i)
	p.addHolder(i)
}

// HandleDontHave clears BEP 54-reneged availability of piece i.
func (p *PiecePicker) HandleDontHave(pe *peer.Peer, i uint32) {
	if pe.Bitfield == nil || !pe.Bitfield.Test(i) {
		return
	}
	pe.Bitfield.Clear(i)
	p.removeHolder(int(i))
}

// RefreshPriorities recounts the high-priority pieces after the file
// priorities or the skipped files change.
func (p *PiecePicker) RefreshPriorities() {
	p.highPriority = 0
	for i := range p.pieces {
		if p.pieces[i].HighPriority() {
			p.highPriority++
		}
	}
}

// HandleAllowedFast records a fast-exception grant for piece i.
func (p *PiecePicker) HandleAllowedFast(pe *peer.Peer, i uint32) {
	pe.ReceivedAllowedFast.Add(p.pieces[i].Piece)
}

// HandleSnubbed marks a slow peer on piece i; snubbing a choked peer is a
// bug and panics.
func (p *PiecePicker) HandleSnubbed(pe *peer.Peer, i uint32) {
	if p.pieces[i].Choked.Has(pe) {
		panic("peer snubbed while choked")
	}
	p.pieces[i].Snubbed.Add(pe)
}

// HandleChoke marks the peer choked on piece i.
func (p *PiecePicker) HandleChoke(pe *peer.Peer, i uint32) {
	p.pieces[i].Choked.Add(pe)
}

// HandleUnchoke clears the choke mark on piece i.
func (p *PiecePicker) HandleUnchoke(pe *peer.Peer, i uint32) {
	p.pieces[i].Choked.Remove(pe)
}

// HandleCancelDownload forgets a cancelled fetch on piece i.
func (p *PiecePicker) HandleCancelDownload(pe *peer.Peer, i uint32) {
	p.pieces[i].Requested.Remove(pe)
	p.pieces[i].Snubbed.Remove(pe)
	p.pieces[i].Choked.Remove(pe)
}

// HandleDisconnect forgets a peer everywhere.
func (p *PiecePicker) HandleDisconnect(pe *peer.Peer) {
	for i := range p.pieces {
		p.HandleCancelDownload(pe, uint32(i))
		// Only the pieces it had count it. The bit is cleared with the count,
		// so the books stay balanced even if a disconnect came twice.
		if p.pieces[i].heldBy(pe) {
			pe.Bitfield.Clear(uint32(i))
			p.removeHolder(i)
		}
	}
}

// addHolder counts a new holder, ticking availability on the first.
func (p *PiecePicker) addHolder(i uint32) {
	p.pieces[i].holders++
	if p.pieces[i].holders == 1 {
		p.available++
	}
}

// removeHolder uncounts a holder, ticking availability at zero.
func (p *PiecePicker) removeHolder(i int) {
	p.pieces[i].holders--
	if p.pieces[i].holders == 0 {
		p.available--
	}
}

// PickFor selects the peer's next piece and marks it requested, clearing
// the snub flag; the bool reports an allowed-fast grant.
func (p *PiecePicker) PickFor(pe *peer.Peer) (pp *piece.Piece, allowedFast bool) {
	pi, allowedFast := p.findPiece(pe)
	if pi == nil {
		return nil, false
	}
	pe.Snubbed = false
	pi.Requested.Add(pe)
	return pi.Piece, allowedFast
}

// findPiece runs the selection stages in priority order: concurrency gate,
// webseed-stealing mode, player windows and high priority (unchoked only),
// file edges, allowed-fast grants, then — unchoked only — endgame, rarest
// or sequential, and stalled re-requests. The bool tags allowed-fast picks.
func (p *PiecePicker) findPiece(pe *peer.Peer) (mp *myPiece, allowedFast bool) {
	if !roomFor(pe) {
		return nil, false
	}
	if p.downloadingWebseed() {
		return p.findPieceAmongWebseeds(pe)
	}
	if !pe.PeerChoking {
		if mp = p.pickStreaming(pe); mp != nil {
			return mp, pe.ReceivedAllowedFast.Has(mp.Piece)
		}
		if mp = p.pickReadahead(pe); mp != nil {
			return mp, pe.ReceivedAllowedFast.Has(mp.Piece)
		}
		if mp = p.pickHigh(pe); mp != nil {
			return mp, pe.ReceivedAllowedFast.Has(mp.Piece)
		}
	}
	// File edges precede the spread-out allowed-fast set and apply only
	// unchoked (choked peers can only take fast grants).
	if (p.sequential || p.firstLast) && !pe.PeerChoking {
		if mp = p.pickFileEdge(pe); mp != nil {
			return mp, false
		}
	}
	if mp = p.pickAllowedFast(pe); mp != nil {
		return mp, true
	}
	if pe.PeerChoking {
		return nil, false
	}
	if p.endgame {
		return p.pickEndgame(pe), false
	}
	if p.sequential {
		mp = p.pickSequential(pe)
	} else {
		mp = p.pickRarest(pe)
	}
	if mp != nil {
		return mp, false
	}
	if p.endgame {
		return p.pickEndgame(pe), false
	}
	return p.pickStalled(pe), false
}

// roomFor reports whether the peer may open another piece: multi-piece
// peers up to their cap, single-piece peers only when idle.
func roomFor(pe *peer.Peer) bool {
	if pe.MaxConcurrentPieces > 1 {
		return pe.ConcurrentPieces < pe.MaxConcurrentPieces
	}
	return !pe.Downloading
}

// findPieceAmongWebseeds serves a choked-by-webseeds swarm: choked peers
// wait, others take gap tails or steal from webseeds.
func (p *PiecePicker) findPieceAmongWebseeds(pe *peer.Peer) (*myPiece, bool) {
	if pe.PeerChoking {
		return nil, false
	}
	if mp := p.pickLastPieceOfSmallestGap(pe); mp != nil {
		return mp, pe.ReceivedAllowedFast.Has(mp.Piece)
	}
	if mp := p.peerStealsFromWebseed(pe); mp != nil {
		return mp, pe.ReceivedAllowedFast.Has(mp.Piece)
	}
	return nil, false
}

// pickAllowedFast takes a granted piece: first found normally, lowest index
// in sequential mode (the grant set is unordered).
func (p *PiecePicker) pickAllowedFast(pe *peer.Peer) *myPiece {
	var picked *myPiece
	for _, pi := range pe.ReceivedAllowedFast.Items {
		mp := &p.pieces[pi.Index]
		if mp.Done || mp.Writing || mp.Skip {
			continue
		}
		if mp.Requested.Len() == 0 && mp.heldBy(pe) {
			if !p.sequential {
				return mp
			}
			if picked == nil || mp.Index < picked.Index {
				picked = mp
			}
		}
	}
	return picked
}

// pickRarest takes the least-held unrequested piece the peer has, flipping
// to endgame when nothing is unrequested anywhere.
//
// It scans for the minimum instead of sorting the whole table first: the order
// was only ever used to read the first eligible piece, and the sort ran on every
// pick of every peer (the pick is 9 % of the profile under load).
func (p *PiecePicker) pickRarest(pe *peer.Peer) *myPiece {
	var picked *myPiece
	var hasUnrequested bool
	for _, mp := range p.piecesByAvailability {
		if mp.Done || mp.Writing || mp.Skip {
			continue
		}
		if mp.Requested.Len() != 0 {
			continue
		}
		hasUnrequested = true
		// Compare the rarity before asking the peer's bitfield: a piece that
		// cannot beat the current best is skipped with two field reads.
		if picked != nil && mp.holders >= picked.holders {
			continue
		}
		if !mp.heldBy(pe) {
			continue
		}
		picked = mp
		if mp.holders <= 1 {
			break // nobody can hold a piece fewer times
		}
	}
	if picked == nil && !hasUnrequested {
		p.endgame = true
	}
	return picked
}

// pickFileEdge takes the first edge piece the peer holds that nobody
// fetches: players need file ends (indexes) before anything else.
func (p *PiecePicker) pickFileEdge(pe *peer.Peer) *myPiece {
	for i := range p.pieces {
		if mp := &p.pieces[i]; (mp.FileHead || mp.FileTail) && mp.PickableBy(pe) {
			return mp
		}
	}
	return nil
}

// pickSequential takes the lowest unrequested index the peer holds,
// flipping to endgame when nothing is unrequested anywhere.
func (p *PiecePicker) pickSequential(pe *peer.Peer) *myPiece {
	var hasUnrequested bool
	for i := range p.pieces {
		mp := &p.pieces[i]
		if mp.Done || mp.Writing || mp.Skip {
			continue
		}
		if mp.Requested.Len() > 0 {
			continue
		}
		if mp.heldBy(pe) {
			return mp
		}
		hasUnrequested = true
	}
	if !hasUnrequested {
		p.endgame = true
	}
	return nil
}

// pickEndgame duplicates the least-requested piece the peer holds and lacks,
// under the duplicate cap. The minimum is found by scanning, not by sorting:
// during the endgame this runs for every peer on every pick.
func (p *PiecePicker) pickEndgame(pe *peer.Peer) *myPiece {
	var picked *myPiece
	pickedRunning := 0
	for _, mp := range p.piecesByAvailability {
		if mp.Done || mp.Writing || mp.Skip {
			continue
		}
		running := mp.RunningDownloads()
		if picked != nil && running >= pickedRunning {
			continue // cannot beat the current best
		}
		if mp.Requested.Len() >= p.maxDuplicateDownload || !mp.heldBy(pe) || mp.Requested.Has(pe) {
			continue
		}
		picked, pickedRunning = mp, running
	}
	return picked
}

// pickStalled re-requests a stalled piece's idle copy: running pieces stay,
// and the least-stalled duplicate under the cap wins.
func (p *PiecePicker) pickStalled(pe *peer.Peer) *myPiece {
	var picked *myPiece
	pickedStalled := 0
	for _, mp := range p.piecesByStalled {
		if mp.Done || mp.Writing || mp.Skip {
			continue
		}
		stalled := mp.StalledDownloads()
		if picked != nil && stalled >= pickedStalled {
			continue // cannot beat the current best
		}
		if mp.RunningDownloads() > 0 {
			continue
		}
		if mp.Requested.Len() >= p.maxDuplicateDownload || !mp.heldBy(pe) || mp.Requested.Has(pe) {
			continue
		}
		picked, pickedStalled = mp, stalled
	}
	return picked
}
