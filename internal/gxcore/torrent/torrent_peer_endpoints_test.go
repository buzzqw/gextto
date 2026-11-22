package torrent

import (
	"net"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peersource"
)

// TestLocalEndpointClassification: the addresses a peer must not be able to
// make us dial. Private LAN addresses stay usable (BEP 40 ranks them and a
// peer on the same network gossips them), loopback, link-local, unspecified
// and multicast do not.
func TestLocalEndpointClassification(t *testing.T) {
	cases := []struct {
		ip    string
		local bool
	}{
		{"127.0.0.1", true},
		{"127.10.20.30", true},
		{"::1", true},
		{"0.0.0.0", true},
		{"::", true},
		{"169.254.1.2", true},
		{"fe80::1", true},
		{"ff02::1", true},
		{"224.0.0.1", true},
		{"", true}, // no address: nothing to dial
		{"192.168.1.10", false},
		{"10.0.0.5", false},
		{"172.16.4.4", false},
		{"fd00::42", false},
		{"8.8.8.8", false},
		{"2001:4860:4860::8888", false},
	}
	for _, c := range cases {
		var ip net.IP
		if c.ip != "" {
			ip = net.ParseIP(c.ip)
		}
		if got := localEndpoint(ip); got != c.local {
			t.Errorf("localEndpoint(%q) = %v, want %v", c.ip, got, c.local)
		}
	}
}

// TestPEXLocalEndpointsAreDropped: a peer that gossips our own host or the
// router must not turn the client into a scanner. The four sources that can
// legitimately name a local address (tracker, DHT, manual, incoming) keep
// every address they return.
func TestPEXLocalEndpointsAreDropped(t *testing.T) {
	tr := dialTestTorrent(false, 0)
	tr.handleNewPeers(testAddrs(
		"127.0.0.1",    // our own host
		"169.254.1.2",  // link-local
		"0.0.0.0",      // unspecified
		"192.168.1.20", // LAN: legitimate
		"8.8.8.8",      // public
	), peersource.PEX)

	if got := tr.addrList.LenSource(peersource.PEX); got != 2 {
		t.Fatalf("PEX candidates = %d, want the LAN and the public one", got)
	}

	tracker := dialTestTorrent(false, 0)
	// 10.9.9.9, not 127.0.0.1: the address pool drops the client's own IP
	// anyway, which is a different rule (no self-dials).
	tracker.handleNewPeers(testAddrs("10.9.9.9", "192.168.1.20"), peersource.Tracker)
	if got := tracker.addrList.LenSource(peersource.Tracker); got != 2 {
		t.Fatalf("tracker candidates = %d, want both kept", got)
	}
}

// TestHolepunchRefusesLocalEndpoint: a ut_holepunch connect naming loopback,
// link-local or the unspecified address is not dialled (libtorrent 2.1.1,
// "validate peer-supplied endpoints against local addresses").
func TestHolepunchRefusesLocalEndpoint(t *testing.T) {
	tr := dialTestTorrent(false, 0)
	for _, ip := range []string{"127.0.0.1", "169.254.10.10", "0.0.0.0", "fe80::1"} {
		tr.dialHolepunch(holepunchTCPAddr(ip, 8080))
	}
	if len(tr.outgoingHandshakers) != 0 || tr.holepunchDials != 0 {
		t.Fatalf("handshakers = %d, dials = %d, want none",
			len(tr.outgoingHandshakers), tr.holepunchDials)
	}

	// A private LAN endpoint is still reachable this way: the peer behind
	// the same NAT is exactly what BEP 55 is for.
	tr.dialHolepunch(holepunchTCPAddr("192.168.1.30", 6881))
	if tr.holepunchDials != 1 || len(tr.outgoingHandshakers) != 1 {
		t.Fatalf("dials = %d, handshakers = %d, want one LAN attempt",
			tr.holepunchDials, len(tr.outgoingHandshakers))
	}
}

// TestHolepunchRendezvousFromPeerWithoutExtension: a peer may address a
// ut_holepunch message with the id we advertised while never advertising the
// extension itself. Building a reply for it must not be attempted (the shape
// of GHSA-c5j3-3rp9-c4rv in anacrolix/torrent, where the lookup panicked and
// the read loop took the process down).
func TestHolepunchRendezvousFromPeerWithoutExtension(t *testing.T) {
	tr := dialTestTorrent(false, 0)
	tr.session.config.Holepunch = true
	if tr.holepunchAttempted == nil {
		tr.holepunchAttempted = map[string]struct{}{}
	}
	silent := &peer.Peer{}
	tr.peers[silent] = struct{}{}

	tr.tryHolepunchRendezvous(holepunchTCPAddr("8.8.8.8", 6881))

	if len(tr.holepunchAttempted) != 0 {
		t.Fatal("a rendezvous was sent to a peer that never advertised ut_holepunch")
	}
	if peerSupportsHolepunch(silent) {
		t.Fatal("a peer without an extension handshake must not support ut_holepunch")
	}

	// And the reply path is guarded the same way: no handshake, no send.
	tr.sendHolepunch(silent, peerprotocol.HolepunchMessage{
		Type: peerprotocol.HolepunchConnect,
		Addr: net.IPv4(5, 6, 7, 8),
		Port: 6881,
	})
}
