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
			if !pi.Having.Has(pe) {
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
	a := make([]Range, 0, len(p.pieces)/2)
	var inGap bool
	var begin uint32
	for _, pi := range p.pieces {
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
			a = append(a, r)
			inGap = false
		case r.Len() == p.maxWebseedPieces:
			a = append(a, r)
			begin = pi.Index
		}
	}
	if inGap {
		a = append(a, Range{Begin: begin, End: uint32(len(p.pieces))})
	}
	return a
}

// pickLastPieceOfSmallestGap offers a peer the tail-most fetchable piece of
// the smallest gap first, skipping requested pieces, ones the peer lacks,
// and choked non-allowed-fast ones.
func (p *PiecePicker) pickLastPieceOfSmallestGap(pe *peer.Peer) *myPiece {
	gaps := p.findGaps()
	if len(gaps) == 0 {
		return nil
	}
	slices.SortFunc(gaps, func(a, b Range) int { return cmp.Compare(a.Len(), b.Len()) })
	for _, gap := range gaps {
		// int: the loop runs below zero.
		for i := int(gap.End - 1); i >= int(gap.Begin); i-- {
			mp := &p.pieces[i]
			if mp.Requested.Len() > 0 {
				continue
			}
			if !mp.Having.Has(pe) {
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
