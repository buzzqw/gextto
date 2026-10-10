package pexlist

import (
	"net"
	"slices"

	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

// MaxLength is the maximum number of items to keep in the RecentlySeen list.
const MaxLength = 25

// RecentlySeen is a peer address list that keeps the last `MaxLength` items.
type RecentlySeen struct {
	peers  []tracker.CompactPeer
	offset int
	length int
}

// Add a new address to the list. IPv6 peers are not tracked here (this list
// feeds the IPv4 "dropped" part of a PEX message); they are ignored so a v6
// address is never stored as 0.0.0.0.
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

// Peers returns the addresses in the list.
func (l *RecentlySeen) Peers() []tracker.CompactPeer {
	return l.peers
}

// Len returns the number of addresses in the list.
func (l *RecentlySeen) Len() int {
	return len(l.peers)
}
