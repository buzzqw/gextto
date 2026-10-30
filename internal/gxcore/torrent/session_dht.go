package torrent

import (
	"errors"
	"net"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/dht"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

func (s *Session) processDHTResults() {
	dhtLimiter := time.NewTicker(time.Second)
	defer dhtLimiter.Stop()
	for {
		select {
		case <-dhtLimiter.C:
			s.handleDHTtick()
		case res := <-s.dht.PeersRequestResults:
			for ih, peers := range res {
				s.mTorrents.RLock()
				torrents, ok := s.torrentsByInfoHash[ih]
				s.mTorrents.RUnlock()
				if !ok {
					continue
				}
				addrs := parseDHTPeers(peers)
				for _, t := range torrents {
					select {
					case t.torrent.dhtPeersC <- addrs:
					case <-t.torrent.closeC:
					default:
					}
				}
			}
		case <-s.closeC:
			return
		}
	}
}

// dhtAnnounceBatch is how many queued torrents one one-second tick hands to the
// DHT. The requests only enqueue a lookup in the node, so a batch keeps a large
// session announcing on time without flooding it; one per tick would delay the
// last torrent by one second each.
const dhtAnnounceBatch = 10

// handleDHTtick hands a batch of pending DHT peer requests to the node. The
// queue is drained a batch at a time, in random map order, so no torrent is
// starved and the map lock is never held while talking to the node.
func (s *Session) handleDHTtick() {
	type request struct {
		ih   [20]byte
		port int
	}
	pending := make([]request, 0, dhtAnnounceBatch)
	s.mPeerRequests.Lock()
	for t, port := range s.dhtPeerRequests {
		if len(pending) == dhtAnnounceBatch {
			break
		}
		pending = append(pending, request{ih: t.infoHash, port: port})
		delete(s.dhtPeerRequests, t)
	}
	s.mPeerRequests.Unlock()
	for _, req := range pending {
		s.dht.PeersRequestPort(string(req.ih[:]), true, req.port)
	}
}

// DHTScrape asks the DHT for the approximate swarm size of an info hash
// (BEP 33): the number of seeders and peers. It needs a running DHT.
func (s *Session) DHTScrape(ih [20]byte, timeout time.Duration) (seeders, peers float64, err error) {
	if s.dht == nil {
		return 0, 0, errors.New("dht disabled")
	}
	est, err := s.dht.Scrape(dht.InfoHash(string(ih[:])), timeout)
	if err != nil {
		return 0, 0, err
	}
	return est.Seeders, est.Peers, nil
}

// publicIPForConfig returns the external IP the node is reached at, when it is
// known: the configured DHT host if it is a specific routable address, or the IP
// of the outgoing interface (VPN killswitch). It feeds the BEP 42 secure node
// ID; nil means "learn it from the network".
func publicIPForConfig(cfg Config) net.IP {
	if ip := net.ParseIP(strings.TrimSpace(cfg.DHTHost)); ip != nil && !ip.IsUnspecified() && !isLocalNetworkAddr(ip) {
		return ip
	}
	if cfg.OutgoingInterface != "" {
		if iface, err := net.InterfaceByName(cfg.OutgoingInterface); err == nil {
			if addrs, err := iface.Addrs(); err == nil {
				for _, a := range addrs {
					ipnet, ok := a.(*net.IPNet)
					if !ok {
						continue
					}
					if ip := ipnet.IP; ip != nil && !ip.IsUnspecified() && !isLocalNetworkAddr(ip) {
						return ip
					}
				}
			}
		}
	}
	return nil
}

func isLocalNetworkAddr(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsPrivate()
}

func parseDHTPeers(peers []string) []*net.TCPAddr {
	addrs := make([]*net.TCPAddr, 0, len(peers))
	for _, peer := range peers {
		// DHT returns one binary entry per peer: 6 bytes for IPv4 (BEP 5),
		// 18 for IPv6 (BEP 32). Anything else is skipped.
		switch len(peer) {
		case 6:
			var cp tracker.CompactPeer
			if cp.UnmarshalBinary([]byte(peer)) != nil {
				continue
			}
			addrs = append(addrs, cp.Addr())
		case 18:
			var cp tracker.CompactPeer6
			if cp.UnmarshalBinary([]byte(peer)) != nil {
				continue
			}
			addrs = append(addrs, cp.Addr())
		}
	}
	return addrs
}
