package pexlist

import (
	"net"
	"slices"

	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

// MaxLength bounds how many recent peers are remembered for PEX seeding.
const MaxLength = 25

// RecentlySeen is a small ring of recently met IPv4 peers. It seeds the
// dropped set of a fresh PEX exchange. IPv6 peers are ignored here — the
// list feeds the IPv4 dropped part, and a v6 address would collapse to
// 0.0.0.0 in it.
type RecentlySeen struct {
	peers  []tracker.CompactPeer
	offset int
	length int
}

// Add remembers addr unless already known. Past capacity the oldest entry is
// overwritten in ring order.
func (l *RecentlySeen) Add(addr *net.TCPAddr) {
	if addr == nil || addr.IP.To4() == nil {
		return
	}
	cp := tracker.NewCompactPeer(addr)
	if l.has(cp) {
		return
	}
	if l.length >= MaxLength {
		l.peers[l.offset] = cp
	} else {
		l.peers = append(l.peers, cp)
		l.length++
	}
	l.offset = (l.offset + 1) % MaxLength
}

func (l *RecentlySeen) has(cp tracker.CompactPeer) bool {
	return slices.Contains(l.peers, cp)
}

// Peers returns the remembered addresses.
func (l *RecentlySeen) Peers() []tracker.CompactPeer {
	return l.peers
}

// Len returns how many addresses are remembered.
func (l *RecentlySeen) Len() int {
	return len(l.peers)
}
