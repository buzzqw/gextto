package piecepicker

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
	"github.com/buzzqw/gextto/internal/gxcore/internal/sliceset"
	"github.com/buzzqw/gextto/internal/gxcore/internal/webseedsource"
	"github.com/rcrowley/go-metrics"
)

/*

These are the things to consider when selecting a piece for downloading:

  * Piece is done (hash checked and written to disk)
  * Piece is writing
  * Peer has the piece
  * Peer is choking us
  * Piece is marked as allowed-fast
  * Piece is requested from another peers
  * Piece is reserved for downloading by a webseed source
  * Is endgame mode activated (all pieces are requested)
  * Are there stalled peers (snubbed or choked in the middle of download)
  * Is sequential download mode enabled

Do not forget to re-check these when making changes.

*/

// PiecePicker runs an algorithm to determine which piece to download next, from which peer or webseed source.
// PiecePicker keeps track availability of pieces among peers.
type PiecePicker struct {
	webseedSources       []*webseedsource.WebseedSource
	pieces               []myPiece
	piecesByAvailability []*myPiece
	piecesByStalled      []*myPiece
	maxDuplicateDownload int
	maxWebseedPieces     int
	available            uint32
	endgame              bool

	// Pick the next piece in sequential order instead of the rarest piece.
	sequential bool

	// Pick the pieces at both ends of every file first, then continue
	// rarest-first (gextto fork). Independent of sequential order.
	firstLast bool

	// streamBegin/streamEnd are the piece range a player is reading (gextto
	// fork): those pieces are requested before any other. Empty when equal.
	streamBegin, streamEnd uint32
}

type myPiece struct {
	*piece.Piece
	Having    sliceset.SliceSet[peer.Peer]
	Requested sliceset.SliceSet[peer.Peer]
	Snubbed   sliceset.SliceSet[peer.Peer]
	Choked    sliceset.SliceSet[peer.Peer]

	// Downloading from webseed source or marked to be downloaded later.
	RequestedWebseed *webseedsource.WebseedSource

	// The piece holds data from the first or the last fileEdgeSize bytes of a file.
	// Only set in sequential download mode.
	FileHead, FileTail bool
}

// RunningDownloads returns the number of pieces that are being downloaded actively.
// This number does not include downloads of a piece whose peers are snubbed or choked.
func (p *myPiece) RunningDownloads() int {
	return p.Requested.Len() - p.StalledDownloads()
}

// StalledDownloads returns the number of piece downloads whose peers are snubbed or choked.
func (p *myPiece) StalledDownloads() int {
	return p.Snubbed.Len() + p.Choked.Len()
}

// AvailableForWebseed returns true if the piece is allowed to be downloaded from a webseed source.
func (p *myPiece) AvailableForWebseed() bool {
	if p.Done || p.Writing || p.Skip {
		return false
	}
	return p.RequestedWebseed == nil
}

// PickableBy returns true if the piece can be requested from the peer right away.
func (p *myPiece) PickableBy(pe *peer.Peer) bool {
	if p.Done || p.Writing || p.Skip {
		return false
	}
	if p.Requested.Len() > 0 {
		return false
	}
	return p.Having.Has(pe)
}

// New returns a new PiecePicker.
// If sequential is true, pieces are picked in sequential order instead of rarest-first.
// If firstLast is true, the pieces at both ends of every file are picked first
// (gextto fork; sequential mode implies it).
func New(pieces []piece.Piece, maxDuplicateDownload int, webseedSources []*webseedsource.WebseedSource, sequential, firstLast bool) *PiecePicker {
	ps := make([]myPiece, len(pieces))
	for i := range pieces {
		ps[i] = myPiece{Piece: &pieces[i]}
	}
	sps := make([]*myPiece, len(ps))
	sps2 := make([]*myPiece, len(ps))
	for i := range sps {
		sps[i] = &ps[i]
		sps2[i] = &ps[i]
	}
	maxWebseedPieces := len(pieces) / 20 // Download 5% of pieces in a single HTTP request (see BEP19)
	if maxWebseedPieces == 0 {
		maxWebseedPieces = 1
	}
	if sequential || firstLast {
		markFileEdges(ps)
	}
	return &PiecePicker{
		pieces:               ps,
		piecesByAvailability: sps,
		piecesByStalled:      sps2,
		maxDuplicateDownload: maxDuplicateDownload,
		maxWebseedPieces:     maxWebseedPieces,
		webseedSources:       webseedSources,
		sequential:           sequential,
		firstLast:            firstLast,
	}
}

// Upper limit for the number of bytes that are downloaded first at both ends of a file.
const maxFileEdgeSize = 8 * 1024 * 1024

// fileEdgeSize returns the number of bytes at both ends of a file that are downloaded before the
// rest of the file. A single piece is not always enough: the moov atom of an MP4 file or the
// idx1 index of an AVI file is a few megabytes long on a long recording.
// qBittorrent reserves 1% of the file size for the same purpose.
func fileEdgeSize(fileSize int64) int64 {
	return max(min(fileSize/100, maxFileEdgeSize), 1)
}

// markFileEdges flags the pieces that hold data from either end of a file.
// Padding files are skipped, they don't belong to any file.
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

// SetOrder changes the piece order at runtime (gextto fork): sequential picks
// the pieces in index order, firstLast prioritizes the ends of every file.
// Enabling either recomputes the file-edge flags; disabling both stops using
// them (the flags may stay set, they are consulted only when an order is on).
func (p *PiecePicker) SetOrder(sequential, firstLast bool) {
	if (sequential || firstLast) && !p.sequential && !p.firstLast {
		markFileEdges(p.pieces)
	}
	p.sequential = sequential
	p.firstLast = firstLast
}

// SetStreamWindow prioritizes the pieces in [begin, end) (gextto fork): the
// pieces a player is reading are requested before the rest. begin == end clears
// the window.
func (p *PiecePicker) SetStreamWindow(begin, end uint32) {
	if end > uint32(len(p.pieces)) {
		end = uint32(len(p.pieces))
	}
	if begin > end {
		begin = end
	}
	p.streamBegin, p.streamEnd = begin, end
}

// pickStreaming returns the first piece in the streaming window the peer can
// download right now.
func (p *PiecePicker) pickStreaming(pe *peer.Peer) *myPiece {
	for i := p.streamBegin; i < p.streamEnd; i++ {
		if i >= uint32(len(p.pieces)) {
			return nil
		}
		mp := &p.pieces[i]
		if mp.PickableBy(pe) {
			return mp
		}
	}
	return nil
}

// PieceDownloading reports whether the piece at index i is being downloaded
// right now (gextto fork, for piece diagnostics).
func (p *PiecePicker) PieceDownloading(i uint32) bool {
	if i >= uint32(len(p.pieces)) {
		return false
	}
	mp := &p.pieces[i]
	return mp.Writing || mp.Requested.Len() > 0 || mp.RequestedWebseed != nil
}

// CloseWebseedDownloader closes the download from a webseed source.
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

// AddWebseedSource adds a web seed source at runtime (gextto fork). The list is
// rebuilt so it never aliases the caller's slice.
func (p *PiecePicker) AddWebseedSource(src *webseedsource.WebseedSource) {
	next := make([]*webseedsource.WebseedSource, 0, len(p.webseedSources)+1)
	next = append(next, p.webseedSources...)
	p.webseedSources = append(next, src)
}

// RemoveWebseedSource drops a web seed source from the picker (gextto fork).
// Close its downloader first (CloseWebseedDownloader) to release the pieces.
func (p *PiecePicker) RemoveWebseedSource(src *webseedsource.WebseedSource) {
	kept := make([]*webseedsource.WebseedSource, 0, len(p.webseedSources))
	for _, s := range p.webseedSources {
		if s != src {
			kept = append(kept, s)
		}
	}
	p.webseedSources = kept
}

// WebseedStopAt sets the webseed downloader to stop at index `i`.
func (p *PiecePicker) WebseedStopAt(src *webseedsource.WebseedSource, i uint32) (closed bool) {
	oldEnd := src.Downloader.End
	newEnd := i
	for i := newEnd; i < oldEnd; i++ {
		if p.pieces[i].RequestedWebseed != src {
			panic(fmt.Sprintf("invalid source in piece #%d: %s", i, p.pieces[i].RequestedWebseed.URL))
		}
		p.pieces[i].RequestedWebseed = nil
	}
	src.Downloader.UpdateEnd(newEnd)
	if src.Downloader.ReadCurrent() >= newEnd {
		p.CloseWebseedDownloader(src)
		return true
	}
	return false
}

// Available returns the number of available pieces among the swarm.
func (p *PiecePicker) Available() uint32 {
	return p.available
}

// RequestedPeers returns the number of peers that the piece with the index is requested from.
func (p *PiecePicker) RequestedPeers(i uint32) []*peer.Peer {
	return p.pieces[i].Requested.Items
}

// RequestedWebseedSource returns the number of webseed sources that the piece with the index is requested from.
func (p *PiecePicker) RequestedWebseedSource(i uint32) *webseedsource.WebseedSource {
	return p.pieces[i].RequestedWebseed
}

// HandleHave must be called to set the availability of the piece at the peer.
func (p *PiecePicker) HandleHave(pe *peer.Peer, i uint32) {
	pe.Bitfield.Set(i)
	p.addHavingPeer(i, pe)
}

// HandleAllowedFast must be called to set the allowed-fast status of the piece at peer.
func (p *PiecePicker) HandleAllowedFast(pe *peer.Peer, i uint32) {
	pe.ReceivedAllowedFast.Add(p.pieces[i].Piece)
}

// HandleSnubbed must be called to set the peer as snubbed when it is slow or stalled.
func (p *PiecePicker) HandleSnubbed(pe *peer.Peer, i uint32) {
	if p.pieces[i].Choked.Has(pe) {
		panic("peer snubbed while choked")
	}
	p.pieces[i].Snubbed.Add(pe)
}

// HandleChoke must be called to set choke status of the remote peer.
func (p *PiecePicker) HandleChoke(pe *peer.Peer, i uint32) {
	p.pieces[i].Snubbed.Remove(pe)
	p.pieces[i].Choked.Add(pe)
}

// HandleUnchoke must be called to unset choke status of the remote peer.
func (p *PiecePicker) HandleUnchoke(pe *peer.Peer, i uint32) {
	p.pieces[i].Choked.Remove(pe)
}

// HandleCancelDownload must be called to update indexes when a piece download is canceled from the peer.
func (p *PiecePicker) HandleCancelDownload(pe *peer.Peer, i uint32) {
	p.pieces[i].Requested.Remove(pe)
	p.pieces[i].Snubbed.Remove(pe)
	p.pieces[i].Choked.Remove(pe)
}

// HandleDisconnect must be called to remove the peer from internal indexes.
func (p *PiecePicker) HandleDisconnect(pe *peer.Peer) {
	for i := range p.pieces {
		p.HandleCancelDownload(pe, uint32(i))
		p.removeHavingPeer(i, pe)
	}
}

func (p *PiecePicker) addHavingPeer(i uint32, pe *peer.Peer) {
	ok := p.pieces[i].Having.Add(pe)
	if ok && p.pieces[i].Having.Len() == 1 {
		p.available++
	}
}

func (p *PiecePicker) removeHavingPeer(i int, pe *peer.Peer) {
	ok := p.pieces[i].Having.Remove(pe)
	if ok && p.pieces[i].Having.Len() == 0 {
		p.available--
	}
}

// PickFor selects the next piece for download from the peer.
func (p *PiecePicker) PickFor(pe *peer.Peer) (pp *piece.Piece, allowedFast bool) {
	pi, allowedFast := p.findPiece(pe)
	if pi == nil {
		return nil, false
	}
	pe.Snubbed = false
	pi.Requested.Add(pe)
	return pi.Piece, allowedFast
}

func (p *PiecePicker) findPiece(pe *peer.Peer) (mp *myPiece, allowedFast bool) {
	// Peer is allowed to download only one piece at a time
	if pe.Downloading {
		return nil, false
	}
	if p.downloadingWebseed() {
		if pe.PeerChoking {
			return nil, false
		}
		mp = p.pickLastPieceOfSmallestGap(pe)
		if mp != nil {
			return mp, pe.ReceivedAllowedFast.Has(mp.Piece)
		}
		mp = p.peerStealsFromWebseed(pe)
		if mp != nil {
			return mp, pe.ReceivedAllowedFast.Has(mp.Piece)
		}
		return nil, false
	}
	// Pieces at file edges come before the allowed-fast pieces, which are spread over the
	// torrent. While choked they are the only pieces we can request, so this applies only
	// after the peer unchokes us.
	if !pe.PeerChoking {
		// The player's streaming window comes first (gextto fork).
		if mp = p.pickStreaming(pe); mp != nil {
			return mp, pe.ReceivedAllowedFast.Has(mp.Piece)
		}
	}
	if (p.sequential || p.firstLast) && !pe.PeerChoking {
		mp = p.pickFileEdge(pe)
		if mp != nil {
			return mp, false
		}
	}
	// Pick allowed fast piece
	pi := p.pickAllowedFast(pe)
	if pi != nil {
		return pi, true
	}
	// Must be unchoked to request a peer
	if pe.PeerChoking {
		return nil, false
	}
	// Short path for endgame mode.
	if p.endgame {
		return p.pickEndgame(pe), false
	}
	// Pieck rarest piece
	if p.sequential {
		pi = p.pickSequential(pe)
	} else {
		pi = p.pickRarest(pe)
	}
	if pi != nil {
		return pi, false
	}
	// Check if endgame mode is activated
	if p.endgame {
		return p.pickEndgame(pe), false
	}
	// Re-request stalled downloads
	return p.pickStalled(pe), false
}

func (p *PiecePicker) pickAllowedFast(pe *peer.Peer) *myPiece {
	var picked *myPiece
	for _, pi := range pe.ReceivedAllowedFast.Items {
		mp := &p.pieces[pi.Index]
		if mp.Done || mp.Writing || mp.Skip {
			continue
		}
		if mp.Requested.Len() == 0 && mp.Having.Has(pe) {
			if !p.sequential {
				return mp
			}
			// The allowed-fast set is unordered, so scan it all for the lowest index.
			if picked == nil || mp.Index < picked.Index {
				picked = mp
			}
		}
	}
	return picked
}

func (p *PiecePicker) pickRarest(pe *peer.Peer) *myPiece {
	// Sort by rarity
	slices.SortFunc(p.piecesByAvailability, func(a, b *myPiece) int {
		return cmp.Compare(len(a.Having.Items), len(b.Having.Items))
	})
	var picked *myPiece
	var hasUnrequested bool
	// Select unrequested piece
	for _, mp := range p.piecesByAvailability {
		if mp.Done || mp.Writing || mp.Skip {
			continue
		}
		if mp.Requested.Len() == 0 && mp.Having.Has(pe) {
			picked = mp
			break
		}
		if mp.Requested.Len() == 0 {
			hasUnrequested = true
		}
	}
	if picked == nil && !hasUnrequested {
		p.endgame = true
	}
	return picked
}

// pickFileEdge returns a piece at either end of a file that the peer has and nobody else is
// downloading. Media files keep their index at one of the two ends, so these pieces come
// before the rest of the pieces. Players need the index before they can start playing.
func (p *PiecePicker) pickFileEdge(pe *peer.Peer) *myPiece {
	for i := range p.pieces {
		mp := &p.pieces[i]
		if (mp.FileHead || mp.FileTail) && mp.PickableBy(pe) {
			return mp
		}
	}
	return nil
}

// pickSequential returns the first piece in index order that the peer has and nobody else is
// downloading. It replaces pickRarest in sequential download mode. The pieces at file edges are
// already picked by pickFileEdge before this function runs.
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
		if mp.Having.Has(pe) {
			return mp
		}
		hasUnrequested = true
	}
	if !hasUnrequested {
		p.endgame = true
	}
	return nil
}

func (p *PiecePicker) pickEndgame(pe *peer.Peer) *myPiece {
	// Sort by request count
	slices.SortFunc(p.piecesByAvailability, func(a, b *myPiece) int {
		return cmp.Compare(a.RunningDownloads(), b.RunningDownloads())
	})
	// Select unrequested piece
	for _, mp := range p.piecesByAvailability {
		if mp.Done || mp.Writing || mp.Skip {
			continue
		}
		if mp.Requested.Len() < p.maxDuplicateDownload && mp.Having.Has(pe) {
			return mp
		}
	}
	return nil
}

func (p *PiecePicker) pickStalled(pe *peer.Peer) *myPiece {
	// Sort by request count
	slices.SortFunc(p.piecesByStalled, func(a, b *myPiece) int {
		return cmp.Compare(a.StalledDownloads(), b.StalledDownloads())
	})
	// Select unrequested piece
	for _, mp := range p.piecesByStalled {
		if mp.Done || mp.Writing || mp.Skip {
			continue
		}
		if mp.RunningDownloads() > 0 {
			continue
		}
		if mp.Requested.Len() < p.maxDuplicateDownload && mp.Having.Has(pe) {
			return mp
		}
	}
	return nil
}
