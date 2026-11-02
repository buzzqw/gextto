package addrlist

import (
	"bytes"
	"net"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peerpriority"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peersource"
	"github.com/google/btree"
)

// peerAddr is one candidate peer address with the metadata the AddrList
// indexes need: discovery time, source, dial priority, and its position in
// the by-time slice. The btree orders by priority (see Less).
type peerAddr struct {
	addr      *net.TCPAddr
	timestamp time.Time
	source    peersource.Source
	priority  peerpriority.Priority

	// index locates this entry in AddrList.peerByTime for O(1) removal.
	index int
}

var _ btree.Item = (*peerAddr)(nil)

// Less orders candidates by dial priority; the btree pops the max first. The
// address breaks a tie: BEP 40 masks part of the IP, so distinct peers can share
// a priority, and the btree treats equal items as one and would drop a peer. Only
// the same address is equal, which is what makes a repeated Push a refresh.
func (p *peerAddr) Less(than btree.Item) bool {
	o := than.(*peerAddr)
	if p.priority != o.priority {
		return p.priority < o.priority
	}
	if c := bytes.Compare(p.addr.IP.To16(), o.addr.IP.To16()); c != 0 {
		return c < 0
	}
	return p.addr.Port < o.addr.Port
}
