package pexlist

import (
	"net"
	"strings"

	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

const (
	// BEP 11: Except for the initial PEX message the combined amount of added v4/v6 contacts should not exceed 50 entries.
	// The same applies to dropped entries.
	maxPeers = 50
)

// PEXList contains the list of peer address for sending them to a peer at certain interval.
// List contains 2 separate lists for added and dropped addresses.
type PEXList struct {
	added    map[tracker.CompactPeer]struct{}
	dropped  map[tracker.CompactPeer]struct{}
	added6   map[tracker.CompactPeer6]struct{}
	dropped6 map[tracker.CompactPeer6]struct{}
	flushed  bool
}

// New returns a new empty PEXList.
func New() *PEXList {
	return &PEXList{
		added:    make(map[tracker.CompactPeer]struct{}),
		dropped:  make(map[tracker.CompactPeer]struct{}),
		added6:   make(map[tracker.CompactPeer6]struct{}),
		dropped6: make(map[tracker.CompactPeer6]struct{}),
	}
}

// NewWithRecentlySeen returns a new PEXList with given peers added to the dropped part.
func NewWithRecentlySeen(rs []tracker.CompactPeer) *PEXList {
	l := New()
	for _, cp := range rs {
		l.dropped[cp] = struct{}{}
	}
	return l
}

// Add adds the address to the added part and removes from dropped part.
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

// Drop adds the address to the dropped part and removes from added part.
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

// Flush returns the added and dropped parts for IPv4 and IPv6 and empties the
// list. The IPv6 parts are encoded as compact 18-byte records (BEP 11).
func (l *PEXList) Flush() (added, added6, dropped, dropped6 string) {
	added = l.flush(l.added, l.flushed)
	added6 = l.flush6(l.added6, l.flushed)
	dropped = l.flush(l.dropped, l.flushed)
	dropped6 = l.flush6(l.dropped6, l.flushed)
	l.flushed = true
	return
}

func (l *PEXList) flush(m map[tracker.CompactPeer]struct{}, limit bool) string {
	count := len(m)
	if limit && count > maxPeers {
		count = maxPeers
	}

	var s strings.Builder
	s.Grow(count * 6)
	for p := range m {
		if count == 0 {
			break
		}
		count--

		b, err := p.MarshalBinary()
		if err != nil {
			panic(err)
		}
		s.Write(b)
		delete(m, p)
	}
	return s.String()
}

func (l *PEXList) flush6(m map[tracker.CompactPeer6]struct{}, limit bool) string {
	count := len(m)
	if limit && count > maxPeers {
		count = maxPeers
	}

	var s strings.Builder
	s.Grow(count * 18)
	for p := range m {
		if count == 0 {
			break
		}
		count--

		b, err := p.MarshalBinary()
		if err != nil {
			panic(err)
		}
		s.Write(b)
		delete(m, p)
	}
	return s.String()
}
