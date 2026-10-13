package torrent

import (
	"net"
	"strconv"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/ctxutil"
	"github.com/buzzqw/gextto/internal/gxcore/internal/handshaker/outgoinghandshaker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/mse"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peersource"
	"github.com/buzzqw/gextto/internal/gxcore/internal/resolver"
)

// setNeedMorePeers tells the announce machinery whether the swarm still
// wants peers.
func (t *torrent) setNeedMorePeers(val bool) {
	for _, an := range t.announcers {
		an.NeedMorePeers(val)
	}
	if t.dhtAnnouncer != nil {
		t.dhtAnnouncer.NeedMorePeers(val)
	}
}

// addPeerString queues a "host:port" peer: literals go straight to the dial
// pool, names resolve in the background.
func (t *torrent) addPeerString(addr string) error {
	hoststr, portstr, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	port64, err := strconv.ParseUint(portstr, 10, 16)
	if err != nil {
		return err
	}
	port := int(port64)
	ip := net.ParseIP(hoststr)
	if ip == nil {
		go t.resolveAndAddPeer(hoststr, port)
		return nil
	}
	taddr := &net.TCPAddr{IP: ip, Port: port}
	go t.AddPeers([]*net.TCPAddr{taddr})
	return nil
}

// resolveAndAddPeer resolves a manual peer without the blocklist (operator
// overrides) and feeds it to the swarm as a manual source.
func (t *torrent) resolveAndAddPeer(host string, port int) {
	ctx, cancel := ctxutil.FromChan(t.closeC)
	defer cancel()
	ip, resolvedPort, err := resolver.Resolve(ctx, net.JoinHostPort(host, strconv.Itoa(port)), t.session.config.DNSResolveTimeout, nil)
	if err != nil {
		return
	}
	t.handleNewPeers([]*net.TCPAddr{{IP: ip, Port: resolvedPort}}, peersource.Manual)
}

// handleNewPeers absorbs discovered addresses: eagerness off (we have
// candidates again), unless stopped; unfinished torrents filter bans, queue
// and dial.
func (t *torrent) handleNewPeers(addrs []*net.TCPAddr, source peersource.Source) {
	t.log.Debugf("received %d peers from %s", len(addrs), source)
	t.setNeedMorePeers(false)
	if status := t.status(); status == Stopped || status == Stopping {
		return
	}
	if !t.completed {
		addrs = t.filterBannedIPs(addrs)
		t.addrList.Push(addrs, source)
		t.dialAddresses()
	}
}

// ipBanned checks the per-torrent ban plus the session-wide time-limited ban
// set on corrupt data.
func (t *torrent) ipBanned(ip string) bool {
	if _, ok := t.bannedPeerIPs[ip]; ok {
		return true
	}
	return t.session != nil && t.session.IsBannedIP(ip)
}

// filterBannedIPs drops banned addresses in place.
func (t *torrent) filterBannedIPs(a []*net.TCPAddr) []*net.TCPAddr {
	b := a[:0]
	for _, x := range a {
		if !t.ipBanned(x.IP.String()) {
			b = append(b, x)
		}
	}
	return b
}

// dialAddresses fills the outgoing dial budget from the candidate pool,
// capped by the per-torrent limit when set. An emptied pool marks the swarm
// hungry again; already-connected IPs are skipped without consuming budget.
func (t *torrent) dialAddresses() {
	if t.completed {
		return
	}
	peersConnected := func() int {
		return len(t.outgoingPeers) + len(t.outgoingHandshakers)
	}
	maxDial := t.session.config.MaxPeerDial
	if t.maxConnections > 0 {
		if allowed := t.maxConnections - len(t.peers) - len(t.incomingHandshakers); allowed < maxDial {
			maxDial = allowed
		}
	}
	for peersConnected() < maxDial {
		addr, src := t.addrList.Pop()
		if addr == nil {
			t.setNeedMorePeers(true)
			return
		}
		ip := addr.IP.String()
		if _, ok := t.connectedPeerIPs[ip]; ok {
			continue
		}
		h := outgoinghandshaker.New(addr, src)
		t.outgoingHandshakers[h] = struct{}{}
		t.connectedPeerIPs[ip] = struct{}{}
		go h.Run(
			t.session.config.PeerConnectTimeout,
			t.session.config.PeerHandshakeTimeout,
			t.peerID,
			t.infoHash,
			t.outgoingHandshakerResultC,
			t.ourExtensions(),
			t.session.config.DisableOutgoingEncryption,
			t.session.config.ForceOutgoingEncryption,
		)
	}
}

// startPeer adopts a handshaked connection: PEX seeding, duplicate-id
// rejection (same id twice means a crossed or replayed connection), peer
// registration and pump startup, metrics and greeting messages, and a kick
// for the v2 layer fetch on magnet torrents.
func (t *torrent) startPeer(
	conn net.Conn,
	source peersource.Source,
	peers map[*peer.Peer]struct{},
	peerID [20]byte,
	extensions [8]byte,
	cipher mse.CryptoMethod,
) {
	addr := conn.RemoteAddr().(*net.TCPAddr)
	t.pexAddPeer(addr)
	if _, ok := t.peerIDs[peerID]; ok {
		t.log.Debugf("peer with same id already connected. addr: %s id: %s", addr, peerID)
		conn.Close()
		delete(t.connectedPeerIPs, addr.IP.String())
		t.pexDropPeer(addr)
		t.dialAddresses()
		return
	}
	t.peerIDs[peerID] = struct{}{}

	pe := peer.New(conn, source, peerID, extensions, cipher, t.session.config.PieceReadTimeout, t.session.config.RequestTimeout, t.session.config.MaxRequestsIn, int(t.session.config.MaxMetadataSize), t.bucketDownload, t.bucketUpload)
	t.peers[pe] = struct{}{}
	peers[pe] = struct{}{}
	if t.info() != nil {
		pe.Bitfield = bitfield.New(t.info().NumPieces)
	}
	go pe.Run(t.messages, t.pieceMessagesC.SendC(), t.peerSnubbedC, t.peerDisconnectedC)
	t.session.metrics.Peers.Inc(1)
	t.emitPeer(EventPeerConnected, pe.Addr().String())
	t.sendFirstMessage(pe)
	t.recentlySeen.Add(pe.Addr())
	t.startV2LayerDownload()
}

// sendFirstMessage greets a peer: availability first (super-seed offer or
// bitfield), then the extension handshake, the DHT port, and the
// allowed-fast set — skipped while super-seeding, where unadvertised pieces
// must stay ungrabbable (libtorrent parity).
func (t *torrent) sendFirstMessage(p *peer.Peer) {
	if t.superSeedActive() {
		t.superSeedStartPeer(p, true)
	} else {
		t.sendBitfield(p)
	}
	if p.ExtensionsEnabled {
		t.sendExtensionHandshake(p)
	}
	if p.DHTEnabled {
		p.SendMessage(peerprotocol.PortMessage{Port: t.session.config.DHTPort})
	}
	if p.FastEnabled && t.pieces != nil && !t.superSeedActive() {
		p.GenerateAndSendAllowedFastMessages(t.session.config.AllowedFastSet, t.info().NumPieces, t.infoHash, t.pieces)
	}
}

// sendExtensionHandshake sends our BEP 10 handshake, flagging upload_only
// while a (partial) seed per BEP 21.
func (t *torrent) sendExtensionHandshake(p *peer.Peer) {
	var metadataSize uint32
	if t.info() != nil {
		metadataSize = uint32(len(t.info().Bytes))
	}
	p.SendMessage(peerprotocol.ExtensionMessage{
		ExtendedMessageID: peerprotocol.ExtensionIDHandshake,
		Payload: peerprotocol.NewExtensionHandshake(
			metadataSize,
			t.getClientVersion(),
			p.Addr().IP,
			t.session.config.MaxRequestsIn,
			t.session.config.Holepunch,
			t.uploadOnly(),
		),
	})
}

// sendBitfield advertises our pieces compactly for fast peers
// (have-all/have-none) and verbatim otherwise.
func (t *torrent) sendBitfield(p *peer.Peer) {
	bf := t.bitfield
	switch {
	case p.FastEnabled && bf != nil && bf.All():
		p.SendMessage(peerprotocol.HaveAllMessage{})
	case p.FastEnabled && (bf == nil || bf.Count() == 0):
		p.SendMessage(peerprotocol.HaveNoneMessage{})
	case bf != nil:
		bitfieldData := make([]byte, len(bf.Bytes()))
		copy(bitfieldData, bf.Bytes())
		p.SendMessage(&peerprotocol.BitfieldMessage{Data: bitfieldData})
	}
}

// getClientVersion reports the extension-handshake client string, masked for
// private torrents.
func (t *torrent) getClientVersion() string {
	if t.info() != nil && t.info().Private {
		return t.session.config.PrivateExtensionHandshakeClientVersion
	}
	return publicExtensionHandshakeClientVersion
}

// processQueuedMessages replays the messages peers sent before metadata
// arrived, now that the torrent can interpret them.
func (t *torrent) processQueuedMessages() {
	for pe := range t.peers {
		for _, msg := range pe.Messages {
			t.handlePeerMessage(peer.Message{Peer: pe, Message: msg})
		}
		pe.Messages = nil
	}
}

// handlePeerSnubbed slows a stalled peer down to the base request queue (as
// after a choke) and tells the picker, or parks a stalled metadata fetch.
func (t *torrent) handlePeerSnubbed(pe *peer.Peer) {
	if m := t.pieceDownloaders[pe]; len(m) > 0 {
		// A stopped snub timer may still fire after a choke.
		if pe.PeerChoking {
			return
		}
		pe.Snubbed = true
		t.resetRequestQueue(pe)
		if t.piecePicker != nil {
			for _, pd := range m {
				t.piecePicker.HandleSnubbed(pe, pd.Piece.Index)
			}
		}
		t.startPieceDownloaders()
	} else if id, ok := t.infoDownloaders[pe]; ok {
		pe.Snubbed = true
		t.infoDownloadersSnubbed[pe] = id
		t.startInfoDownloaders()
	}
}
