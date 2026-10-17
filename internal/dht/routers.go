package dht

// routers.go keeps every DNS lookup off the main loop. The bootstrap routers
// (DHTRouters) and the nodes handed to AddNode may be host names; resolving one
// can hang for minutes when the resolver is down. Done on the loop, that stalls
// the whole node and every caller waiting on it — a torrent's run loop feeding
// AddNode from a peer's PORT message blocked there until the engine's watchdog
// killed the daemon. The loop only ever sees literal IP:port addresses.

import (
	"context"
	"expvar"
	"net"
	"strings"
	"time"
)

const (
	// routerRefresh re-resolves the routers now and then: their addresses
	// change rarely, but they do.
	routerRefresh = 30 * time.Minute
	// routerRetry spaces the attempts while no router resolves (no network).
	routerRetry = time.Minute
	// lookupTimeout bounds one host-name resolution.
	lookupTimeout = 15 * time.Second
)

// totalDroppedNodeHints counts AddNode hints dropped because the loop's queue
// was full: a hint is not worth blocking the caller for.
var totalDroppedNodeHints = expvar.NewInt("totalDroppedNodeHints")

// ipLookup resolves a host name. A node keeps its own (DHT.lookupIP) so a
// test can give one node a resolver without touching the others.
type ipLookup func(ctx context.Context, host string) ([]net.IPAddr, error)

// systemLookup is the system resolver.
func systemLookup(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

// AddNode feeds a "host:port" node to the routing table without ever blocking
// the caller: the node is a hint (a peer's PORT message, a magnet's dht=
// node). A host name is resolved in the background; when the loop's queue is
// full the hint is dropped.
func (d *DHT) AddNode(addr string) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return
	}
	if net.ParseIP(host) != nil {
		d.offerNode(addr)
		return
	}
	lookup, proto := d.lookup(), d.config.UDPProto
	go func() {
		if ip, ok := resolveHost(lookup, host, proto); ok {
			d.offerNode(net.JoinHostPort(ip, port))
		}
	}()
}

// offerNode queues a literal address for the loop, dropping it when full.
func (d *DHT) offerNode(addr string) {
	select {
	case d.remoteNodeAcquaintance <- addr:
	default:
		totalDroppedNodeHints.Add(1)
	}
}

// addNodeWait queues a literal address and waits for room: the stored remotes
// loaded at start, which are worth keeping and have no caller to block.
func (d *DHT) addNodeWait(addr string) {
	select {
	case d.remoteNodeAcquaintance <- addr:
	case <-d.stop:
	}
}

// routers returns the bootstrap routers as literal addresses. Host names are
// resolved in the background (resolveRoutersAsync); until then the list may
// be empty and the callers simply have no router to fall back on. Loop-only.
func (d *DHT) routers() []string {
	if literal, ok := literalRouters(d.config.DHTRouters); ok {
		return literal
	}
	now := time.Now()
	stale := len(d.routerAddrs) == 0 && now.After(d.routersRetryAt) ||
		len(d.routerAddrs) > 0 && now.Sub(d.routersAt) > routerRefresh
	if stale && !d.routersResolving {
		d.resolveRoutersAsync()
	}
	return d.routerAddrs
}

// resolveRoutersAsync resolves the routers away from the loop; the result
// comes back through routersResolved.
func (d *DHT) resolveRoutersAsync() {
	d.routersResolving = true
	spec, proto, lookup := d.config.DHTRouters, d.config.UDPProto, d.lookup()
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		addrs := resolveRouters(lookup, spec, proto)
		select {
		case d.routersResolved <- addrs:
		case <-d.stop:
		}
	}()
}

// routersArrived installs a resolution and runs the bootstrap that was waiting
// for it. Loop-only.
func (d *DHT) routersArrived(addrs []string) {
	d.routersResolving = false
	now := time.Now()
	if len(addrs) == 0 {
		d.routersRetryAt = now.Add(routerRetry)
		return
	}
	d.routerAddrs = addrs
	d.routersAt = now
	if d.bootstrapPending {
		d.bootstrapPending = false
		d.bootstrapRouters()
		d.findNode(d.nodeId)
		d.getMorePeers(nil)
	}
}

// literalRouters reports whether every router is already an IP:port, and
// returns them: those need no resolution.
func literalRouters(spec string) ([]string, bool) {
	var out []string
	for _, s := range strings.Split(spec, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		host, _, err := net.SplitHostPort(s)
		if err != nil || net.ParseIP(host) == nil {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

// resolveRouters turns "host:port,..." into literal "ip:port" addresses of the
// family proto asks for; entries that do not resolve are skipped.
func resolveRouters(lookup ipLookup, spec, proto string) []string {
	var out []string
	for _, s := range strings.Split(spec, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		host, port, err := net.SplitHostPort(s)
		if err != nil {
			continue
		}
		if net.ParseIP(host) != nil {
			out = append(out, s)
			continue
		}
		if ip, ok := resolveHost(lookup, host, proto); ok {
			out = append(out, net.JoinHostPort(ip, port))
		}
	}
	return out
}

// resolveHost resolves one host name to an address of proto's family ("udp4",
// "udp6" or "udp" for either), bounded by lookupTimeout.
func resolveHost(lookup ipLookup, host, proto string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
	defer cancel()
	addrs, err := lookup(ctx, host)
	if err != nil {
		return "", false
	}
	for _, a := range addrs {
		v4 := a.IP.To4() != nil
		if proto == "udp4" && !v4 || proto == "udp6" && v4 {
			continue
		}
		return a.IP.String(), true
	}
	return "", false
}

// lookup is the node's resolver: the system one unless a test set another.
func (d *DHT) lookup() ipLookup {
	if d.lookupIP != nil {
		return d.lookupIP
	}
	return systemLookup
}
