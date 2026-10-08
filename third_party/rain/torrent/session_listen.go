package torrent

// session_listen.go (gextto fork): one listening port shared by every torrent,
// like libtorrent. Upstream rain opens a port per torrent, which cannot be
// forwarded on a NAT router. With Config.ListenPort set, the session accepts
// all incoming peers, completes the BitTorrent/MSE handshake, picks the
// torrent from the info hash and hands it the ready connection.

import (
	"github.com/cenkalti/rain/v2/internal/netx"
	"net"
	"time"

	"github.com/cenkalti/rain/v2/internal/btconn"
	"github.com/cenkalti/rain/v2/internal/handshaker/incominghandshaker"
	"github.com/cenkalti/rain/v2/internal/peersource"
	"github.com/nictuku/dht"
)

// maxRoutedHandshakes bounds concurrent incoming handshakes on the shared port.
const maxRoutedHandshakes = 256

func (s *Session) sharedPort() bool {
	return s.config.ListenPort > 0
}

func (s *Session) startSharedListener() error {
	if !s.sharedPort() {
		return nil
	}
	ip := net.ParseIP(s.config.Host)
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: ip, Port: int(s.config.ListenPort)})
	if err != nil {
		return err
	}
	s.listener = listener
	s.log.Info("Listening peers on tcp://" + listener.Addr().String())
	go s.acceptShared(listener)
	if s.utpSocket != nil {
		s.log.Info("Listening peers on utp://" + s.utpSocket.Addr().String())
		go s.acceptUTP()
	}
	return nil
}

// acceptUTP routes incoming uTP peers like the TCP ones.
func (s *Session) acceptUTP() {
	slots := make(chan struct{}, maxRoutedHandshakes)
	for {
		raw, err := s.utpSocket.Accept()
		if err != nil {
			select {
			case <-s.closeC:
			default:
				s.log.Errorln("uTP listener stopped:", err)
			}
			return
		}
		conn := netx.TrackUTP(&netx.UTPConn{Conn: raw})
		netx.IncomingUTP.Add(1)
		addr, ok := conn.RemoteAddr().(*net.TCPAddr)
		if !ok || (s.config.BlocklistEnabledForIncomingConnections && s.blocklist != nil && s.blocklist.Blocked(addr.IP)) {
			conn.Close()
			continue
		}
		select {
		case slots <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		go func() {
			defer func() { <-slots }()
			s.routeIncoming(conn)
		}()
	}
}

func (s *Session) acceptShared(listener *net.TCPListener) {
	slots := make(chan struct{}, maxRoutedHandshakes)
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-s.closeC:
			default:
				s.log.Errorln("shared listener stopped:", err)
			}
			return
		}
		addr, ok := conn.RemoteAddr().(*net.TCPAddr)
		if !ok {
			conn.Close()
			continue
		}
		if s.config.BlocklistEnabledForIncomingConnections && s.blocklist != nil && s.blocklist.Blocked(addr.IP) {
			conn.Close()
			continue
		}
		select {
		case slots <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		go func() {
			defer func() { <-slots }()
			s.routeIncoming(conn)
		}()
	}
}

// sharedSKey finds the MSE secret (info hash) among all torrents.
func (s *Session) sharedSKey(sKeyHash [20]byte) []byte {
	s.mTorrents.RLock()
	defer s.mTorrents.RUnlock()
	for _, t := range s.torrents {
		if t.torrent.sKeyHash == sKeyHash {
			ih := t.torrent.infoHash
			return ih[:]
		}
	}
	return nil
}

// runningTorrent returns the torrent for an info hash, if it is in the session.
func (s *Session) torrentForInfoHash(ih [20]byte) *torrent {
	s.mTorrents.RLock()
	defer s.mTorrents.RUnlock()
	list := s.torrentsByInfoHash[dht.InfoHash(ih[:])]
	if len(list) == 0 {
		return nil
	}
	return list[0].torrent
}

func (s *Session) routeIncoming(conn net.Conn) {
	var target *torrent
	lookup := func(ih [20]byte) ([20]byte, bool) {
		target = s.torrentForInfoHash(ih)
		if target == nil {
			return [20]byte{}, false
		}
		return target.peerID, true
	}
	encConn, cipher, extensions, peerID, _, err := btconn.AcceptRouted(
		conn, s.config.PeerHandshakeTimeout, s.sharedSKey, s.config.ForceIncomingEncryption, lookup, s.extensions)
	if err != nil || target == nil {
		conn.Close()
		return
	}
	h := incominghandshaker.New(encConn)
	h.PeerID = peerID
	h.Extensions = extensions
	h.Cipher = cipher
	select {
	case target.routedConnC <- h:
	case <-target.closeC:
		encConn.Close()
	case <-time.After(10 * time.Second):
		encConn.Close()
	}
}

// handleRoutedConnection runs in the torrent loop and applies the same checks
// as a connection accepted on the torrent's own port.
func (t *torrent) handleRoutedConnection(h *incominghandshaker.IncomingHandshaker) {
	conn := h.Conn
	if t.status() == Stopped || t.status() == Stopping {
		conn.Close()
		return
	}
	if len(t.incomingHandshakers)+len(t.incomingPeers) >= t.session.config.MaxPeerAccept ||
		(t.maxConnections > 0 && len(t.peers) >= t.maxConnections) {
		conn.Close()
		return
	}
	addr, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		conn.Close()
		return
	}
	ipstr := addr.IP.String()
	if _, ok := t.connectedPeerIPs[ipstr]; ok {
		conn.Close()
		return
	}
	if _, ok := t.bannedPeerIPs[ipstr]; ok {
		conn.Close()
		return
	}
	t.connectedPeerIPs[ipstr] = struct{}{}
	t.startPeer(conn, peersource.Incoming, t.incomingPeers, h.PeerID, h.Extensions, h.Cipher)
}
