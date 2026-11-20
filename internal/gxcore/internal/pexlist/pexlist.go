package pexlist

import (
	"net"
	"strings"

	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

// maxPeers caps the combined v4+v6 entries of one PEX message (BEP 11): gossip
// must stay small, and there is no reason for the first message to carry more
// than the ones after it. The peer keeps the addresses we send it and we send
// the rest on the next flush.
const maxPeers = 50

// PEXList stages peer addresses for the next PEX message, split into added
// and dropped sets per address family. Entries move between the sets as
// peers appear and vanish; Flush emits and clears them.
type PEXList struct {
	added    map[tracker.CompactPeer]struct{}
	dropped  map[tracker.CompactPeer]struct{}
	added6   map[tracker.CompactPeer6]struct{}
	dropped6 map[tracker.CompactPeer6]struct{}
}

// New returns an empty PEXList.
func New() *PEXList {
	return &PEXList{
		added:    make(map[tracker.CompactPeer]struct{}),
		dropped:  make(map[tracker.CompactPeer]struct{}),
		added6:   make(map[tracker.CompactPeer6]struct{}),
		dropped6: make(map[tracker.CompactPeer6]struct{}),
	}
}

// NewWithRecentlySeen seeds the dropped set with recently seen peers, so the
// first message already tells the remote side what we know.
func NewWithRecentlySeen(rs []tracker.CompactPeer) *PEXList {
	l := New()
	for _, cp := range rs {
		l.dropped[cp] = struct{}{}
	}
	return l
}

// Add stages addr as appeared, un-dropping it when it was staged before.
func (l *PEXList) Add(addr *net.TCPAddr) {
	if tracker.IsIPv6(addr) {
		p := tracker.NewCompactPeer6(addr)
		l.added6[p] = struct{}{}
		delete(l.dropped6, p)
		return
	}
	p := tracker.NewCompactPeer(addr)
	l.added[p] = struct{}{}
	delete(l.dropped, p)
}

// Drop stages addr as gone, un-adding it when it was staged before.
func (l *PEXList) Drop(addr *net.TCPAddr) {
	if tracker.IsIPv6(addr) {
		p := tracker.NewCompactPeer6(addr)
		l.dropped6[p] = struct{}{}
		delete(l.added6, p)
		return
	}
	peer := tracker.NewCompactPeer(addr)
	l.dropped[peer] = struct{}{}
	delete(l.added, peer)
}

// Flush encodes the staged sets as compact strings and resets them. Every
// message shares the maxPeers budget between the families, IPv4 first: what
// does not fit stays staged for the next one.
func (l *PEXList) Flush() (added, added6, dropped, dropped6 string) {
	added, added6 = flushFamily(l.added, l.added6, maxPeers)
	dropped, dropped6 = flushFamily(l.dropped, l.dropped6, maxPeers)
	return
}

// flushFamily emits one added/dropped family pair, v4 first with the v6
// remainder of the shared budget.
func flushFamily(m4 map[tracker.CompactPeer]struct{}, m6 map[tracker.CompactPeer6]struct{}, limit int) (string, string) {
	s4 := flush4(m4, limit)
	remaining := limit
	if remaining >= 0 {
		remaining -= len(s4) / 6
		if remaining < 0 {
			remaining = 0
		}
	}
	return s4, flush6(m6, remaining)
}

func flush4(m map[tracker.CompactPeer]struct{}, limit int) string {
	var s strings.Builder
	if len(m) < limit {
		limit = len(m)
	}
	s.Grow(limit * 6)
	sent := 0
	for p := range m {
		if sent == limit {
			break
		}
		b, err := p.MarshalBinary()
		delete(m, p)
		if err != nil {
			// A peer can hand us an address we cannot encode back (the
			// tracker and PEX decoders accept shapes we do not send): drop
			// it instead of panicking on our own gossip.
			continue
		}
		s.Write(b)
		sent++
	}
	return s.String()
}

func flush6(m map[tracker.CompactPeer6]struct{}, limit int) string {
	var s strings.Builder
	if len(m) < limit {
		limit = len(m)
	}
	s.Grow(limit * 18)
	sent := 0
	for p := range m {
		if sent == limit {
			break
		}
		b, err := p.MarshalBinary()
		delete(m, p)
		if err != nil {
			continue
		}
		s.Write(b)
		sent++
	}
	return s.String()
}
