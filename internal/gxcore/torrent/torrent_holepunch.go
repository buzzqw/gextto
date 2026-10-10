package torrent

// gextto fork: BEP 55 "Holepunch extension" (ut_holepunch).
//
// A peer behind a NAT that cannot be reached with an inbound connection asks a
// connected peer (the relay) to introduce it to a target. The relay is already
// connected to the target and sends a "connect" message to both sides, each
// carrying the other's endpoint; both then dial each other over uTP, and the
// simultaneous outgoing packets open the NAT mappings in between.
//
// The relay is a regular peer (this is what makes BEP 55 interoperable with
// µTorrent, BitComet, qBittorrent/libtorrent and Transmission); it is not the
// libtorrent-proprietary DHT relay.

import (
	"net"

	"github.com/buzzqw/gextto/internal/gxcore/internal/handshaker/outgoinghandshaker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peersource"
)

// maxHolepunchRelays bounds how many peers a single rendezvous is sent to, so
// a failed dial never turns into a broadcast.
const maxHolepunchRelays = 8

// peerSupportsHolepunch reports whether the peer advertised ut_holepunch in its
// BEP 10 extension handshake.
func peerSupportsHolepunch(pe *peer.Peer) bool {
	if pe.ExtensionHandshake == nil {
		return false
	}
	_, ok := pe.ExtensionHandshake.M[peerprotocol.ExtensionKeyHolepunch]
	return ok
}

// sendHolepunch writes a ut_holepunch message using the id the peer advertised
// for the extension.
func (t *torrent) sendHolepunch(pe *peer.Peer, msg peerprotocol.HolepunchMessage) {
	if !peerSupportsHolepunch(pe) {
		return
	}
	pe.SendMessage(peerprotocol.ExtensionMessage{
		ExtendedMessageID: pe.ExtensionHandshake.M[peerprotocol.ExtensionKeyHolepunch],
		Payload:           msg,
	})
}

// handleHolepunchMessage processes an incoming ut_holepunch message.
func (t *torrent) handleHolepunchMessage(pe *peer.Peer, msg peerprotocol.HolepunchMessage) {
	if !t.session.config.Holepunch {
		return
	}
	switch msg.Type {
	case peerprotocol.HolepunchRendezvous:
		t.handleHolepunchRendezvous(pe, msg)
	case peerprotocol.HolepunchConnect:
		t.handleHolepunchConnect(pe, msg)
	case peerprotocol.HolepunchError:
		t.log.Debugf("holepunch error from %s: %s", pe.Addr(), msg.ErrCode)
	}
}

// holepunchRelay is the minimal view of a connected peer used to plan a
// rendezvous.
type holepunchRelay struct {
	Addr     *net.TCPAddr
	Supports bool
}

// holepunchRelayMsg is one message the relay must deliver: Target < 0 means the
// initiator, otherwise the index of the target peer in the relay slice.
type holepunchRelayMsg struct {
	Target int
	Msg    peerprotocol.HolepunchMessage
}

// planHolepunchRendezvous decides what a relaying peer answers to a rendezvous,
// following BEP 55. It is pure, so the relay logic is unit tested without real
// peers.
func planHolepunchRendezvous(sender *net.TCPAddr, msg peerprotocol.HolepunchMessage, relays []holepunchRelay) []holepunchRelayMsg {
	target := msg.AddrPort()
	errTo := func(code peerprotocol.HolepunchErrCode) []holepunchRelayMsg {
		return []holepunchRelayMsg{{
			Target: -1,
			Msg:    peerprotocol.HolepunchMessage{Type: peerprotocol.HolepunchError, Addr: msg.Addr, Port: msg.Port, ErrCode: code},
		}}
	}
	if sender == nil || target == nil {
		return errTo(peerprotocol.HolepunchNoSuchPeer)
	}
	if target.IP.Equal(sender.IP) && target.Port == sender.Port {
		return errTo(peerprotocol.HolepunchNoSelf)
	}
	var out []holepunchRelayMsg
	connected := false
	for i, relay := range relays {
		if relay.Addr == nil || relay.Addr.Port != target.Port || !relay.Addr.IP.Equal(target.IP) {
			continue
		}
		connected = true
		if !relay.Supports {
			out = append(out, holepunchRelayMsg{Target: -1, Msg: peerprotocol.HolepunchMessage{
				Type: peerprotocol.HolepunchError, Addr: msg.Addr, Port: msg.Port, ErrCode: peerprotocol.HolepunchNoSupport,
			}})
			continue
		}
		// Each side learns the other's endpoint and dials it.
		out = append(out, holepunchRelayMsg{Target: -1, Msg: peerprotocol.HolepunchMessage{
			Type: peerprotocol.HolepunchConnect, Addr: target.IP, Port: uint16(target.Port),
		}})
		out = append(out, holepunchRelayMsg{Target: i, Msg: peerprotocol.HolepunchMessage{
			Type: peerprotocol.HolepunchConnect, Addr: sender.IP, Port: uint16(sender.Port),
		}})
	}
	if !connected {
		return errTo(peerprotocol.HolepunchNotConnected)
	}
	return out
}

// handleHolepunchRendezvous relays an introduction between the initiator and a
// peer we are connected to, per BEP 55.
func (t *torrent) handleHolepunchRendezvous(sender *peer.Peer, msg peerprotocol.HolepunchMessage) {
	t.log.Debugf("holepunch rendezvous from %s: target %s", sender.Addr(), msg.AddrPort())
	peers := make([]*peer.Peer, 0, len(t.peers))
	relays := make([]holepunchRelay, 0, len(t.peers))
	for pe := range t.peers {
		peers = append(peers, pe)
		relays = append(relays, holepunchRelay{Addr: pe.Addr(), Supports: peerSupportsHolepunch(pe)})
	}
	for _, action := range planHolepunchRendezvous(sender.Addr(), msg, relays) {
		if action.Target < 0 {
			t.sendHolepunch(sender, action.Msg)
			continue
		}
		t.sendHolepunch(peers[action.Target], action.Msg)
	}
}

// handleHolepunchConnect dials the endpoint a relay told us to reach.
func (t *torrent) handleHolepunchConnect(sender *peer.Peer, msg peerprotocol.HolepunchMessage) {
	endpoint := msg.AddrPort()
	if endpoint == nil {
		return
	}
	t.log.Debugf("holepunch connect from %s: dialing %s", sender.Addr(), endpoint)
	t.dialHolepunch(endpoint)
}

// dialHolepunch starts an outgoing handshake to an endpoint reached through a
// holepunch, even when the torrent is seeding (a peer behind NAT can only be
// reached this way).
func (t *torrent) dialHolepunch(endpoint *net.TCPAddr) {
	ip := endpoint.IP.String()
	if _, ok := t.connectedPeerIPs[ip]; ok {
		return
	}
	if t.ipBanned(ip) {
		return
	}
	h := outgoinghandshaker.New(endpoint, peersource.Holepunch)
	t.outgoingHandshakers[h] = struct{}{}
	t.connectedPeerIPs[ip] = struct{}{}
	go h.Run(
		t.session.config.PeerConnectTimeout,
		t.session.config.PeerHandshakeTimeout,
		t.peerID,
		t.infoHash,
		t.outgoingHandshakerResultC,
		t.session.extensions,
		t.session.config.DisableOutgoingEncryption,
		t.session.config.ForceOutgoingEncryption,
	)
}

// tryHolepunchRendezvous asks connected peers to introduce us to an endpoint we
// could not reach directly. One attempt per endpoint.
func (t *torrent) tryHolepunchRendezvous(target *net.TCPAddr) {
	if !t.session.config.Holepunch || target == nil {
		return
	}
	key := target.String()
	if _, ok := t.holepunchAttempted[key]; ok {
		return
	}
	sent := 0
	for pe := range t.peers {
		if !peerSupportsHolepunch(pe) {
			continue
		}
		if addr := pe.Addr(); addr != nil && addr.IP.Equal(target.IP) && addr.Port == target.Port {
			continue // the target itself is not a useful relay
		}
		t.sendHolepunch(pe, peerprotocol.HolepunchMessage{
			Type: peerprotocol.HolepunchRendezvous,
			Addr: target.IP,
			Port: uint16(target.Port),
		})
		sent++
		if sent >= maxHolepunchRelays {
			break
		}
	}
	if sent > 0 {
		t.holepunchAttempted[key] = struct{}{}
	}
	t.log.Debugf("holepunch rendezvous for %s sent to %d peer(s)", target, sent)
}
