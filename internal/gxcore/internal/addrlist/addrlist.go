package addrlist

import (
	"net"
	"slices"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/blocklist"
	"github.com/buzzqw/gextto/internal/gxcore/internal/externalip"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerpriority"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peersource"
	"github.com/google/btree"
)

// AddrList is the pool of known-but-not-yet-connected peer addresses. It is
// indexed twice: a btree by dial priority for Pop, and a slice by discovery
// time for age-based eviction. Both indexes always describe the same set;
// Push panics when they drift apart, because a silent desync would either
// leak entries or dial garbage.
type AddrList struct {
	peerByTime     []*peerAddr
	peerByPriority *btree.BTree

	maxItems   int
	listenPort int
	clientIP   *net.IP
	blocklist  *blocklist.Blocklist

	countBySource map[peersource.Source]int
}

// New builds an address list holding at most maxItems candidates.
func New(maxItems int, blocklist *blocklist.Blocklist, listenPort int, clientIP *net.IP) *AddrList {
	return &AddrList{
		peerByPriority: btree.New(2),

		maxItems:      maxItems,
		listenPort:    listenPort,
		clientIP:      clientIP,
		blocklist:     blocklist,
		countBySource: make(map[peersource.Source]int),
	}
}

// Reset drops every candidate.
func (d *AddrList) Reset() {
	d.peerByTime = nil
	d.peerByPriority.Clear(false)
	d.countBySource = make(map[peersource.Source]int)
}

// Len returns the number of candidate addresses.
func (d *AddrList) Len() int {
	return d.peerByPriority.Len()
}

// LenSource returns the number of candidates learned from one source.
func (d *AddrList) LenSource(s peersource.Source) int {
	return d.countBySource[s]
}

// Pop removes and returns the highest-priority candidate, or nil when empty.
// Popping also clears the by-time slot, keeping both indexes in sync.
func (d *AddrList) Pop() (*net.TCPAddr, peersource.Source) {
	item := d.peerByPriority.DeleteMax()
	if item == nil {
		return nil, 0
	}
	p := item.(*peerAddr)
	d.peerByTime[p.index] = nil
	d.countBySource[p.source]--
	return p.addr, p.source
}

// Push admits new candidates from one source. Duplicates refresh the stored
// entry (timestamp, priority) instead of doubling it. Afterwards the list is
// re-sorted by time, re-indexed, and trimmed down to maxItems by evicting
// the oldest entries first.
func (d *AddrList) Push(addrs []*net.TCPAddr, source peersource.Source) {
	now := time.Now()
	admitted := 0
	for _, addr := range addrs {
		if !d.admissible(addr) {
			continue
		}
		p := &peerAddr{
			addr:      addr,
			timestamp: now,
			source:    source,
			priority:  peerpriority.Calculate(addr, d.clientAddr()),
		}
		if prev := d.peerByPriority.ReplaceOrInsert(p); prev != nil {
			old := prev.(*peerAddr)
			d.peerByTime[old.index] = p
			p.index = old.index
			d.countBySource[old.source]--
		} else {
			d.peerByTime = append(d.peerByTime, p)
			p.index = len(d.peerByTime) - 1
		}
		admitted++
	}
	if admitted == 0 {
		// Nothing changed: the two indexes already agree, so the O(n log n)
		// re-sort below would be pure waste (PEX can push often).
		return
	}
	d.compact()
	slices.SortFunc(d.peerByTime, func(a, b *peerAddr) int { return a.timestamp.Compare(b.timestamp) })
	for i, p := range d.peerByTime {
		p.index = i
	}
	d.countBySource[source] += admitted

	if excess := d.peerByPriority.Len() - d.maxItems; excess > 0 {
		d.evictOldest(excess)
		d.compact()
	}
	if len(d.peerByTime) != d.peerByPriority.Len() {
		panic("addr list data structures not in sync")
	}
}

// admissible filters out addresses that must never be dialed: port-less,
// our own listener, our own interface IPs, and blocklisted ones.
func (d *AddrList) admissible(addr *net.TCPAddr) bool {
	// 0 port is invalid
	if addr.Port == 0 {
		return false
	}
	// Discard own client
	if addr.IP.IsLoopback() && addr.Port == d.listenPort {
		return false
	} else if d.clientIP.Equal(addr.IP) {
		return false
	}
	if externalip.IsExternal(addr.IP) {
		return false
	}
	return d.blocklist == nil || !d.blocklist.Blocked(addr.IP)
}

// compact drops the nilled by-time slots in place and re-indexes the rest.
func (d *AddrList) compact() {
	kept := d.peerByTime[:0]
	for _, p := range d.peerByTime {
		if p == nil {
			continue
		}
		kept = append(kept, p)
		p.index = len(kept) - 1
	}
	d.peerByTime = kept
}

// evictOldest drops the delta longest-known candidates, from both indexes.
func (d *AddrList) evictOldest(delta int) {
	for i := range delta {
		d.countBySource[d.peerByTime[i].source]--
		d.peerByPriority.Delete(d.peerByTime[i])
		d.peerByTime[i] = nil
	}
}

func (d *AddrList) clientAddr() *net.TCPAddr {
	ip := *d.clientIP
	if ip == nil {
		ip = net.IPv4(0, 0, 0, 0)
	}
	return &net.TCPAddr{
		IP:   ip,
		Port: d.listenPort,
	}
}
