package dht

import (
	"expvar"
	"fmt"
	"net"
	"time"
)

// newRoutingTable starts an empty table: tree, address book and subnet
// counters.
func newRoutingTable(log *DebugLogger) *routingTable {
	return &routingTable{
		nTree:       &nTree{},
		addresses:   make(map[string]*remoteNode),
		subnetNodes: make(map[string]int),
		log:         log,
	}
}

// routingTable pairs the Kademlia id tree with an address book, the
// neighborhood boundary, and the anti-Sybil per-subnet accounting.
type routingTable struct {
	*nTree
	// addresses maps "host:port" strings to nodes (net.UDPAddr is no map key).
	addresses map[string]*remoteNode

	// Neighborhood tracking.
	nodeId       string
	boundaryNode *remoteNode
	// proximity counts the prefix bits shared by boundaryNode and nodeId.
	proximity int

	// maxPerSubnet caps tree nodes per /24 (v4) or /48 (v6); 0 disables.
	maxPerSubnet int
	// subnetNodes counts tree occupants per subnet key, maintained
	// incrementally so cap checks stay O(1).
	subnetNodes map[string]int

	log *DebugLogger
}

// hasSubnetSlot tells whether node holds a tree slot (the invariant the
// counter mirrors): live, identified, addressed, and subnetted.
func (r *routingTable) hasSubnetSlot(n *remoteNode) bool {
	return n != nil && !n.noTree && !bogusId(n.id) && n.address.IP != nil && subnetKey(n.address.IP) != ""
}

// trackSubnet moves node into (add) or out of the subnet counter.
// Idempotent via countedSubnet; call exactly on slot gain/loss.
func (r *routingTable) trackSubnet(n *remoteNode, add bool) {
	key := subnetKey(n.address.IP)
	if key == "" {
		n.countedSubnet = false
		return
	}
	if add {
		if !n.countedSubnet {
			r.subnetNodes[key]++
			n.countedSubnet = true
		}
		return
	}
	if !n.countedSubnet {
		return
	}
	n.countedSubnet = false
	if r.subnetNodes[key] > 0 {
		r.subnetNodes[key]--
		if r.subnetNodes[key] == 0 {
			delete(r.subnetNodes, key)
		}
	}
}

// subnetAtCap reports a full subnet for ip. counted marks an already-slotted
// node under update, whose own slot must not count against itself. False
// with the cap off.
func (r *routingTable) subnetAtCap(ip net.IP, counted bool) bool {
	if r.maxPerSubnet <= 0 {
		return false
	}
	key := subnetKey(ip)
	if key == "" {
		return false
	}
	n := r.subnetNodes[key]
	if counted {
		n--
	}
	return n >= r.maxPerSubnet
}

// hostPortToNode resolves hostPort ("host:port", DNS allowed) into the known
// node, address string and presence. Empty input is a programming bug and
// panics; nil table entries and empty resolutions error.
func (r *routingTable) hostPortToNode(hostPort string, port string) (node *remoteNode, addr string, existed bool, err error) {
	if hostPort == "" {
		panic("programming error: hostPortToNode received a nil hostPort")
	}
	address, err := net.ResolveUDPAddr(port, hostPort)
	if err != nil {
		return nil, "", false, err
	}
	if address.String() == "" {
		return nil, "", false, fmt.Errorf("programming error: address resolution for hostPortToNode returned an empty string")
	}
	n, existed := r.addresses[address.String()]
	if existed && n == nil {
		return nil, "", false, fmt.Errorf("programming error: hostPortToNode found nil node in address table")
	}
	return n, address.String(), existed, nil
}

// length counts address-book entries.
func (r *routingTable) length() int {
	return len(r.addresses)
}

// reachableNodes exports the reachable, identified nodes for persistence,
// publishing the count per local id as an expvar gauge.
func (r *routingTable) reachableNodes() (tbl map[string][]byte) {
	tbl = make(map[string][]byte)
	for addr, r := range r.addresses {
		if addr == "" {
			(*r.log).Debugf("reachableNodes: found empty address for node %x.", r.id)
			continue
		}
		if r.reachable && len(r.id) == 20 {
			tbl[addr] = []byte(r.id)
		}
	}

	hexId := fmt.Sprintf("%x", r.nodeId)
	// A fresh expvar per call: simpler than fetching, casting and ensuring
	// the published value, and deliberately not a NewInt gauge.
	v := new(expvar.Int)
	v.Set(int64(len(tbl)))
	reachableNodes.Set(hexId, v)
	return
}

// numNodes counts address-book entries.
func (r *routingTable) numNodes() int {
	return len(r.addresses)
}

// isValidAddr requires a non-empty host:port.
func isValidAddr(addr string) bool {
	if addr == "" {
		return false
	}
	if h, p, err := net.SplitHostPort(addr); h == "" || p == "" || err != nil {
		return false
	}
	return true
}

// update re-identifies a known node: capped subnets keep the address-book
// entry id-labelled but slotless (noTree), so it is neither re-discovered
// in a loop nor counted; others enter the tree and the counter.
func (r *routingTable) update(node *remoteNode, proto string) error {
	_, addr, existed, err := r.hostPortToNode(node.address.String(), proto)
	if err != nil {
		return err
	}
	if !isValidAddr(addr) {
		return fmt.Errorf("routingTable.update received an invalid address %v", addr)
	}
	if !existed {
		return fmt.Errorf("node missing from the routing table: %v", node.address.String())
	}
	if node.id != "" {
		if r.subnetAtCap(node.address.IP, node.countedSubnet) {
			totalNodesCapped.Add(1)
			r.trackSubnet(node, false)
			node.noTree = true
			r.addresses[addr].id = node.id
			return nil
		}
		node.noTree = false
		r.nTree.insert(node)
		r.trackSubnet(node, true)
		totalNodes.Add(1)
		r.addresses[addr].id = node.id
	}
	return nil
}

// insert admits a node: port-less and unspecified addresses refuse, known
// addresses report success without duplicating, capped subnets stay
// slotless in the book, and identified, uncapped nodes join the tree and
// the counters.
func (r *routingTable) insert(node *remoteNode, proto string) error {
	if node.address.Port == 0 {
		return fmt.Errorf("routingTable.insert() got a node with Port=0")
	}
	if node.address.IP.IsUnspecified() {
		return fmt.Errorf("routingTable.insert() got a node with a non-specified IP address")
	}
	_, addr, existed, err := r.hostPortToNode(node.address.String(), proto)
	if err != nil {
		return err
	}
	if !isValidAddr(addr) {
		return fmt.Errorf("routingTable.insert received an invalid address %v", addr)

	}
	if existed {
		return nil
	}
	if !bogusId(node.id) && r.subnetAtCap(node.address.IP, node.countedSubnet) {
		totalNodesCapped.Add(1)
		node.noTree = true
	}
	r.addresses[addr] = node
	knownNodes.Store(int64(len(r.addresses)))
	if !bogusId(node.id) && !node.noTree {
		r.nTree.insert(node)
		totalNodes.Add(1)
		r.trackSubnet(node, true)
	}
	return nil
}

// getOrCreateNode returns the known node for hostPort (IP:port or DNS,
// resolved when new), creating and inserting it when absent: idempotent.
func (r *routingTable) getOrCreateNode(id string, hostPort string, proto string) (node *remoteNode, err error) {
	node, addr, existed, err := r.hostPortToNode(hostPort, proto)
	if err != nil {
		return nil, err
	}
	if existed {
		return node, nil
	}
	udpAddr, err := net.ResolveUDPAddr(proto, addr)
	if err != nil {
		return nil, err
	}
	node = newRemoteNode(*udpAddr, id, r.log)
	return node, r.insert(node, proto)
}

// kill drops a node from book, counters and tree, resets the neighborhood
// boundary when it dies, and marks its contacts dead in the peer store.
func (r *routingTable) kill(n *remoteNode, p *peerStore) {
	r.trackSubnet(n, false)
	delete(r.addresses, n.address.String())
	knownNodes.Store(int64(len(r.addresses)))
	r.nTree.cut(InfoHash(n.id), 0)
	totalKilledNodes.Add(1)

	if r.boundaryNode != nil && n.id == r.boundaryNode.id {
		r.resetNeighborhoodBoundary()
	}
	p.killContact(decodeCompactPeerAddr(n.addressBinaryFormat))
}

// resetNeighborhoodBoundary promotes the most distant known neighbor to
// boundary, recomputing proximity (zeroed when neighborless).
func (r *routingTable) resetNeighborhoodBoundary() {
	r.proximity = 0
	neighbors := r.lookup(InfoHash(r.nodeId))
	if len(neighbors) > 0 {
		r.boundaryNode = neighbors[len(neighbors)-1]
		r.proximity = commonBits(r.nodeId, r.boundaryNode.id)
	}
}

// cleanup sweeps the address book: mismatched or empty addresses die;
// reachable nodes with no queries queue a ping, stale ones (2 missed
// cycles) die, fresh ones rest; unreachable nodes past the pending cap die.
// Survivors needing probes return in needPing. Timed in debug (a few
// thousand nodes sweep in tens of ms).
func (r *routingTable) cleanup(cleanupPeriod time.Duration, p *peerStore) (needPing []*remoteNode) {
	needPing = make([]*remoteNode, 0, 10)
	t0 := time.Now()
	for addr, n := range r.addresses {
		switch {
		case addr != n.address.String():
			(*r.log).Debugf("cleanup: node address mismatches: %v != %v. Deleting node", addr, n.address.String())
			r.kill(n, p)
			continue
		case addr == "":
			(*r.log).Debugf("cleanup: found empty address for node %x. Deleting node", n.id)
			r.kill(n, p)
			continue
		case n.reachable:
			if len(n.pendingQueries) == 0 {
				goto PING
			}
			if time.Since(n.lastResponseTime) > cleanupPeriod*2+(cleanupPeriod/15) {
				(*r.log).Debugf("DHT: Old node seen %v ago. Deleting", time.Since(n.lastResponseTime))
				r.kill(n, p)
				continue
			}
			if time.Since(n.lastResponseTime).Nanoseconds() < cleanupPeriod.Nanoseconds()/2 {
				continue
			}
		default:
			if len(n.pendingQueries) > maxNodePendingQueries {
				(*r.log).Debugf("DHT: Node never replied to ping. Deleting. %v", n.address)
				r.kill(n, p)
				continue
			}
		}
	PING:
		needPing = append(needPing, n)
	}
	(*r.log).Debugf("DHT: Routing table cleanup took %v\n", time.Since(t0))
	return needPing
}

// neighborhoodUpkeep absorbs node n when neighborless, underpopulated, or
// strictly closer than the boundary (zero shared bits do not qualify).
func (r *routingTable) neighborhoodUpkeep(n *remoteNode, proto string, p *peerStore) {
	if r.boundaryNode == nil {
		r.addNewNeighbor(n, false, proto, p)
		return
	}
	if r.length() < kNodes {
		r.addNewNeighbor(n, false, proto, p)
		return
	}
	cmp := commonBits(r.nodeId, n.id)
	if cmp == 0 {
		return
	}
	if cmp > r.proximity {
		r.addNewNeighbor(n, true, proto, p)
		return
	}
}

// addNewNeighbor inserts n, displacing the boundary (which recomputes it)
// or recomputing it directly; insert failures only log.
func (r *routingTable) addNewNeighbor(n *remoteNode, displaceBoundary bool, proto string, p *peerStore) {
	if err := r.insert(n, proto); err != nil {
		(*r.log).Debugf("addNewNeighbor error: %v", err)
		return
	}
	if displaceBoundary && r.boundaryNode != nil {
		r.kill(r.boundaryNode, p)
	} else {
		r.resetNeighborhoodBoundary()
	}
	(*r.log).Debugf("New neighbor added %s with proximity %d", decodeCompactPeerAddr(n.addressBinaryFormat), r.proximity)
}

// pingSlowly spaces probe signals over the cleanup period (minus a minute)
// so pings do not burst. Sends honor stop: after Run exits nobody reads
// pingRequest, and a bare send would wedge Stop forever.
func pingSlowly(pingRequest chan *remoteNode, needPing []*remoteNode, cleanupPeriod time.Duration, stop chan bool) {
	if len(needPing) == 0 {
		return
	}
	duration := cleanupPeriod - (1 * time.Minute)
	perPingWait := duration / time.Duration(len(needPing))
	for _, r := range needPing {
		select {
		case pingRequest <- r:
		case <-stop:
			return
		}
		select {
		case <-time.After(perPingWait):
		case <-stop:
			return
		}
	}
}

var (
	// totalKilledNodes counts routing-table kills, repeats included.
	totalKilledNodes = expvar.NewInt("totalKilledNodes")
	// totalNodes counts routing-table admissions, repeats included.
	totalNodes = expvar.NewInt("totalNodes")
	// totalNodesCapped counts nodes denied a tree slot by the subnet cap
	// (never fires with the cap off).
	totalNodesCapped = expvar.NewInt("totalNodesCapped")
	// reachableNodes gauges reachable nodes per local id at last persist.
	reachableNodes = expvar.NewMap("reachableNodes")
)
