package piecepicker

import (
	"cmp"
	"math/rand/v2"
	"slices"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/webseedsource"
)

// WebseedDownloadSpec assigns a piece range to one webseed source.
type WebseedDownloadSpec struct {
	Source *webseedsource.WebseedSource
	Begin  uint32 // piece index
	End    uint32 // piece index
}

// PickWebseed claims the next range for src and marks its pieces, so no
// other source picks them again.
func (p *PiecePicker) PickWebseed(src *webseedsource.WebseedSource) *WebseedDownloadSpec {
	r := p.findPieceRangeForWebseed()
	if r == nil {
		return nil
	}
	for i := r.Begin; i < r.End; i++ {
		if p.pieces[i].RequestedWebseed != nil {
			panic("already downloading from webseed url")
		}
		p.pieces[i].RequestedWebseed = src
	}
	return &WebseedDownloadSpec{
		Source: src,
		Begin:  r.Begin,
		End:    r.End,
	}
}

func (p *PiecePicker) downloadingWebseed() bool {
	for _, src := range p.webseedSources {
		if src.Downloading() {
			return true
		}
	}
	return false
}

// findPieceRangeForWebseed prefers file tails under sequential/first-last
// (single-piece requests that finish fast), then the first gap in order,
// else the largest gap at random.
func (p *PiecePicker) findPieceRangeForWebseed() *Range {
	if gaps := p.findGaps(); len(gaps) > 0 {
		if p.sequential || p.firstLast {
			for i := range p.pieces {
				if mp := &p.pieces[i]; mp.FileTail && mp.AvailableForWebseed() {
					return &Range{Begin: mp.Index, End: mp.Index + 1}
				}
			}
			if p.sequential {
				return &gaps[0]
			}
		}
		return selectRandomLargestGap(gaps)
	}
	return p.webseedStealsFromAnotherWebseed()
}

// selectRandomLargestGap picks uniformly among the longest gaps.
func selectRandomLargestGap(gaps []Range) *Range {
	slices.SortFunc(gaps, func(a, b Range) int { return cmp.Compare(b.Len(), a.Len()) })
	longest := gaps[0].Len()
	n := len(gaps)
	for i := range gaps {
		if gaps[i].Len() != longest {
			n = i
			break
		}
	}
	pick := gaps[rand.IntN(n)]
	return &pick
}

func (p *PiecePicker) getDownloadingSources() []*webseedsource.WebseedSource {
	var ret []*webseedsource.WebseedSource
	for _, src := range p.webseedSources {
		if src.Downloading() {
			ret = append(ret, src)
		}
	}
	return ret
}

// webseedStealsFromAnotherWebseed splits the fullest active download in
// half, handing the back half to an idle source.
func (p *PiecePicker) webseedStealsFromAnotherWebseed() *Range {
	downloading := p.getDownloadingSources()
	if len(downloading) == 0 {
		return nil
	}
	slices.SortFunc(downloading, func(a, b *webseedsource.WebseedSource) int { return cmp.Compare(b.Remaining(), a.Remaining()) })
	src := downloading[0]
	r := &Range{
		Begin: (src.Downloader.ReadCurrent() + src.Downloader.End + 1) / 2,
		End:   src.Downloader.End,
	}
	if r.Begin >= r.End {
		return nil
	}
	p.WebseedStopAt(src, r.Begin)
	return r
}

// peerStealsFromWebseed hands a peer the tail-most piece an active webseed
// has not reached yet, provided the peer has it, wants allow, and nobody
// fetches it already.
func (p *PiecePicker) peerStealsFromWebseed(pe *peer.Peer) *myPiece {
	for _, src := range p.getDownloadingSources() {
		if src.Remaining() == 0 {
			continue
		}
		for i := src.Downloader.End - 1; i > src.Downloader.ReadCurrent(); i-- {
			pi := &p.pieces[i]
			if pi.Done || pi.Writing || pi.Skip {
				continue
			}
			if !pi.heldBy(pe) {
				continue
			}
			if pi.Requested.Len() > 0 {
				continue
			}
			p.WebseedStopAt(src, i)
			return pi
		}
	}
	return nil
}

// findGaps lists the BEP 19 gaps — runs of webseed-fetchable pieces —
// capped at maxWebseedPieces per gap.
func (p *PiecePicker) findGaps() []Range {
	return p.appendGaps(make([]Range, 0, len(p.pieces)/2))
}

// appendGaps appends the webseed gaps to dst. The pieces are read in place: a
// copy of each one per call was most of the cost of a pick.
func (p *PiecePicker) appendGaps(dst []Range) []Range {
	var inGap bool
	var begin uint32
	for i := range p.pieces {
		pi := &p.pieces[i]
		if !inGap {
			if pi.AvailableForWebseed() {
				begin = pi.Index
				inGap = true
			}
			continue
		}
		r := Range{Begin: begin, End: pi.Index}
		switch {
		case !pi.AvailableForWebseed():
			dst = append(dst, r)
			inGap = false
		case r.Len() == p.maxWebseedPieces:
			dst = append(dst, r)
			begin = pi.Index
		}
	}
	if inGap {
		dst = append(dst, Range{Begin: begin, End: uint32(len(p.pieces))})
	}
	return dst
}

// pickLastPieceOfSmallestGap offers a peer the tail-most fetchable piece of
// the smallest gap first, skipping requested pieces, ones the peer lacks,
// and choked non-allowed-fast ones. It runs at every pick of every peer
// while a webseed downloads, so the gaps go to reused buffers and are ordered
// by a counting sort on their length (bounded by maxWebseedPieces).
func (p *PiecePicker) pickLastPieceOfSmallestGap(pe *peer.Peer) *myPiece {
	p.gapScratch = p.appendGaps(p.gapScratch[:0])
	gaps := p.gapScratch
	if len(gaps) == 0 {
		return nil
	}
	longest := 0
	for _, gap := range gaps {
		longest = max(longest, gap.Len())
	}
	if cap(p.gapCounts) < longest+2 {
		p.gapCounts = make([]int, longest+2)
	}
	counts := p.gapCounts[:longest+2]
	clear(counts)
	for _, gap := range gaps {
		counts[gap.Len()+1]++
	}
	for i := 1; i < len(counts); i++ {
		counts[i] += counts[i-1]
	}
	if cap(p.gapOrder) < len(gaps) {
		p.gapOrder = make([]Range, len(gaps))
	}
	order := p.gapOrder[:len(gaps)]
	for _, gap := range gaps {
		order[counts[gap.Len()]] = gap
		counts[gap.Len()]++
	}
	for _, gap := range order {
		// int: the loop runs below zero.
		for i := int(gap.End - 1); i >= int(gap.Begin); i-- {
			mp := &p.pieces[i]
			if mp.Requested.Len() > 0 {
				continue
			}
			if !mp.heldBy(pe) {
				continue
			}
			if pe.PeerChoking && !pe.ReceivedAllowedFast.Has(mp.Piece) {
				continue
			}
			return mp
		}
	}
	return nil
}

// Range is an inclusive-begin, exclusive-end piece span.
type Range struct {
	Begin, End uint32
}

// Len counts the pieces in the range.
func (r Range) Len() int {
	return int(r.End) - int(r.Begin)
}
