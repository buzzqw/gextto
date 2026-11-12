package dht

import (
	"encoding/hex"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// A read-only view of the routing table for diagnostics pages: published by
// the run loop every tableViewPeriod, read from any goroutine.

// tableViewPeriod paces the snapshot of the routing table.
const tableViewPeriod = 30 * time.Second

// goodNodeAge is how recent an answer must be for a node to count as good.
const goodNodeAge = 15 * time.Minute

// TableBucket is one row of the table view: the nodes sharing Depth leading
// bits with our own id.
type TableBucket struct {
	Depth int `json:"depth"`
	Nodes int `json:"nodes"`
	Good  int `json:"good"`
}

// TableSearch is an info hash the node is looking peers up for.
type TableSearch struct {
	InfoHash string `json:"info_hash"`
	Queries  int    `json:"queries"`
}

// TableView summarises the routing table and the lookups in flight.
type TableView struct {
	NodeID    string        `json:"node_id"`
	Nodes     int           `json:"nodes"`
	Good      int           `json:"good"`
	IPv4      int           `json:"ipv4"`
	IPv6      int           `json:"ipv6"`
	Buckets   []TableBucket `json:"buckets"`
	Searches  []TableSearch `json:"searches"`
	TakenAt   time.Time     `json:"taken_at"`
	LastQuery *time.Time    `json:"last_incoming_query,omitempty"`
	QueriesIn int64         `json:"queries_in"`
}

// tableView is the last published view; lastIncomingQuery the Unix time of
// the last query a remote node sent us (proof the port receives packets).
var (
	tableViewMu       sync.Mutex
	tableView         *TableView
	lastIncomingQuery atomic.Int64
)

// maxViewSearches bounds the lookups listed in the view.
const maxViewSearches = 50

// View returns the last routing-table view, or nil before the first one.
func View() *TableView {
	tableViewMu.Lock()
	v := tableView
	tableViewMu.Unlock()
	if v == nil {
		return nil
	}
	out := *v
	out.Buckets = append([]TableBucket(nil), v.Buckets...)
	out.Searches = append([]TableSearch(nil), v.Searches...)
	if at := lastIncomingQuery.Load(); at > 0 {
		last := time.Unix(at, 0)
		out.LastQuery = &last
	}
	out.QueriesIn = totalRecvPing.Value() + totalRecvGetPeers.Value() + totalRecvFindNode.Value() + totalRecvAnnouncePeer.Value()
	return &out
}

// noteIncomingQuery records that a remote node reached us.
func noteIncomingQuery(now time.Time) {
	lastIncomingQuery.Store(now.Unix())
}

// publishTableView builds the view on the run loop, the only goroutine that
// may read the table.
func (d *DHT) publishTableView(now time.Time) {
	view := &TableView{NodeID: hex.EncodeToString([]byte(d.nodeId)), TakenAt: now}
	depths := map[int]*TableBucket{}
	searches := map[InfoHash]int{}
	for _, node := range d.routingTable.addresses {
		if node == nil {
			continue
		}
		view.Nodes++
		good := node.reachable && now.Sub(node.lastResponseTime) < goodNodeAge
		if good {
			view.Good++
		}
		if node.address.IP.To4() != nil {
			view.IPv4++
		} else {
			view.IPv6++
		}
		if len(node.id) == len(d.nodeId) && node.id != "" {
			depth := commonBits(node.id, d.nodeId)
			b := depths[depth]
			if b == nil {
				b = &TableBucket{Depth: depth}
				depths[depth] = b
			}
			b.Nodes++
			if good {
				b.Good++
			}
		}
		for _, q := range node.pendingQueries {
			if q != nil && q.Type == "get_peers" && q.ih != "" {
				searches[q.ih]++
			}
		}
	}
	for _, b := range depths {
		view.Buckets = append(view.Buckets, *b)
	}
	sort.Slice(view.Buckets, func(i, j int) bool { return view.Buckets[i].Depth < view.Buckets[j].Depth })
	for ih, n := range searches {
		view.Searches = append(view.Searches, TableSearch{InfoHash: hex.EncodeToString([]byte(ih)), Queries: n})
	}
	sort.Slice(view.Searches, func(i, j int) bool {
		if view.Searches[i].Queries != view.Searches[j].Queries {
			return view.Searches[i].Queries > view.Searches[j].Queries
		}
		return view.Searches[i].InfoHash < view.Searches[j].InfoHash
	})
	if len(view.Searches) > maxViewSearches {
		view.Searches = view.Searches[:maxViewSearches]
	}
	tableViewMu.Lock()
	tableView = view
	tableViewMu.Unlock()
}
