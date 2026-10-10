package torrent

import (
	"net"
	"time"

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

func (s *Session) handleDHTtick() {
	s.mPeerRequests.Lock()
	defer s.mPeerRequests.Unlock()
	for t := range s.dhtPeerRequests {
		s.dht.PeersRequestPort(string(t.infoHash[:]), true, t.port)
		delete(s.dhtPeerRequests, t)
		return
	}
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
