package addrlist

import (
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

// Less orders candidates by dial priority; the btree pops the max first.
func (p *peerAddr) Less(than btree.Item) bool {
	return p.priority < than.(*peerAddr).priority
}
