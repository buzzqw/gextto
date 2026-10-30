package torrent

// session_listen.go: one listening port shared by every torrent, like
// libtorrent. Opening a port per torrent cannot be forwarded on a NAT router.
// With Config.ListenPort set, the session accepts
// all incoming peers, completes the BitTorrent/MSE handshake, picks the
// torrent from the info hash and hands it the ready connection.

import (
	"errors"
	"net"
	"time"

	"github.com/buzzqw/gextto/internal/dht"
	"github.com/buzzqw/gextto/internal/gxcore/internal/btconn"
	"github.com/buzzqw/gextto/internal/gxcore/internal/handshaker/incominghandshaker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/netx"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peersource"
)

// maxRoutedHandshakes bounds concurrent incoming handshakes on the shared port.
const maxRoutedHandshakes = 256

const (
	// acceptRetryInitial is the first pause after a transient Accept failure;
	// it doubles up to acceptRetryMax so a long descriptor shortage does not
	// spin the loop.
	acceptRetryInitial = 5 * time.Millisecond
	acceptRetryMax     = time.Second
)

func (s *Session) sharedPort() bool {
	return s.config.ListenPort > 0
}

func (s *Session) startSharedListener() error {
	if !s.sharedPort() {
		return nil
	}
	listener, err := listenTCPOn(s.config.Host, int(s.config.ListenPort))
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
	delay, warned := acceptRetryInitial, false
	for {
		raw, err := s.utpSocket.Accept()
		if err != nil {
			if !s.acceptRetry(err, &delay, &warned) {
				return
			}
			continue
		}
		delay, warned = acceptRetryInitial, false
		conn := netx.TrackUTP(netx.NewUTPConn(raw))
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
	delay, warned := acceptRetryInitial, false
	for {
		conn, err := listener.Accept()
		if err != nil {
			if !s.acceptRetry(err, &delay, &warned) {
				return
			}
			continue
		}
		delay, warned = acceptRetryInitial, false
		netx.IncomingTCP.Add(1)
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

// acceptRetry decides what to do after an Accept failure. A transient error
// (EMFILE/ENFILE when descriptors are briefly exhausted, EINTR, ECONNABORTED)
// must not kill the listener for the rest of the session: it waits with a
// growing backoff and lets the loop try again. A permanent error, or a closed
// session, stops the loop.
func (s *Session) acceptRetry(err error, delay *time.Duration, warned *bool) bool {
	select {
	case <-s.closeC:
		return false
	default:
	}
	var ne net.Error
	if !errors.As(err, &ne) || !ne.Temporary() {
		s.log.Errorln("incoming peer listener stopped:", err)
		return false
	}
	if !*warned {
		s.log.Warningln("cannot accept an incoming peer connection, retrying:", err)
		*warned = true
	} else {
		s.log.Debugln("cannot accept an incoming peer connection yet:", err)
	}
	select {
	case <-s.closeC:
		return false
	case <-time.After(*delay):
	}
	if *delay < acceptRetryMax {
		*delay *= 2
		if *delay > acceptRetryMax {
			*delay = acceptRetryMax
		}
	}
	return true
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
	lookup := func(ih [20]byte) ([20]byte, [8]byte, bool) {
		target = s.torrentForInfoHash(ih)
		if target == nil {
			return [20]byte{}, [8]byte{}, false
		}
		return target.peerID, target.ourExtensions(), true
	}
	encConn, cipher, extensions, peerID, _, err := btconn.AcceptRouted(
		conn, s.config.PeerHandshakeTimeout, s.sharedSKey, s.config.ForceIncomingEncryption, lookup)
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
	if t.ipBanned(ipstr) {
		conn.Close()
		return
	}
	t.connectedPeerIPs[ipstr] = struct{}{}
	t.startPeer(conn, peersource.Incoming, t.incomingPeers, h.PeerID, h.Extensions, h.Cipher)
}
