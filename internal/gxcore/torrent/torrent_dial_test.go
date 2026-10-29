package torrent

import (
	"net"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/addrlist"
	"github.com/buzzqw/gextto/internal/gxcore/internal/handshaker/incominghandshaker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/handshaker/outgoinghandshaker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peersource"
	"github.com/buzzqw/gextto/internal/gxcore/logging"
)

// dialTestTorrent is the least a dial needs: a session config, a candidate
// pool and the IP registries. MaxPeerDial 0 stops dialAddresses before any
// socket is opened.
func dialTestTorrent(completed bool, maxPeerDial int) *torrent {
	clientIP := net.ParseIP("127.0.0.1")
	t := &torrent{
		completed:           completed,
		errC:                make(chan error),
		session:             &Session{config: Config{MaxPeerDial: maxPeerDial}},
		addrList:            addrlist.New(100, nil, 0, &clientIP),
		peers:               map[*peer.Peer]struct{}{},
		incomingPeers:       map[*peer.Peer]struct{}{},
		outgoingPeers:       map[*peer.Peer]struct{}{},
		incomingHandshakers: map[*incominghandshaker.IncomingHandshaker]struct{}{},
		outgoingHandshakers: map[*outgoinghandshaker.OutgoingHandshaker]struct{}{},
		connectedPeerIPs:    map[string]struct{}{},
		seedPeerIPs:         map[string]struct{}{},
		bannedPeerIPs:       map[string]struct{}{},
		log:                 logging.NewLogger("torrent test"),
	}
	return t
}

func testAddrs(ips ...string) []*net.TCPAddr {
	out := make([]*net.TCPAddr, 0, len(ips))
	for index, ip := range ips {
		out = append(out, &net.TCPAddr{IP: net.ParseIP(ip), Port: 50000 + index})
	}
	return out
}

// TestDialBudgetWhileSeeding: a seed dials too, but only a few peers, because
// the connections exist to find leechers and every candidate that turns out to
// be a seed costs a slot.
func TestDialBudgetWhileSeeding(t *testing.T) {
	downloading := dialTestTorrent(false, 80)
	if got := downloading.dialBudget(); got != 80 {
		t.Fatalf("downloading budget = %d, want the session MaxPeerDial", got)
	}
	seeding := dialTestTorrent(true, 80)
	if got := seeding.dialBudget(); got != seedDialBudget {
		t.Fatalf("seeding budget = %d, want %d", got, seedDialBudget)
	}
	// The per-torrent connection cap still wins over the seed budget.
	seeding.maxConnections = 3
	if got := seeding.dialBudget(); got != 3 {
		t.Fatalf("seeding budget with a cap = %d, want 3", got)
	}
	// A small MaxPeerDial is not raised to the seed budget.
	small := dialTestTorrent(true, 4)
	if got := small.dialBudget(); got != 4 {
		t.Fatalf("seeding budget with MaxPeerDial 4 = %d, want 4", got)
	}
}

// TestSeedingTorrentQueuesTrackerPeers: a completed torrent keeps the peers a
// tracker gives it. Before, it dropped them and could never reach a leecher
// that could not connect to it.
func TestSeedingTorrentQueuesTrackerPeers(t *testing.T) {
	tr := dialTestTorrent(true, 0)
	tr.handleNewPeers(testAddrs("10.1.0.1", "10.1.0.2"), peersource.Tracker)
	if got := tr.addrList.Len(); got != 2 {
		t.Fatalf("queued addresses = %d, want 2", got)
	}
	// A downloading torrent keeps doing the same.
	downloading := dialTestTorrent(false, 0)
	downloading.handleNewPeers(testAddrs("10.1.0.3"), peersource.Tracker)
	if got := downloading.addrList.Len(); got != 1 {
		t.Fatalf("downloading queued addresses = %d, want 1", got)
	}
}

// TestNextDialAddressSkipsKnownSeeds: the peers that already answered "both
// seeds" are not knocked on again, and a connected IP is not dialed twice.
func TestNextDialAddressSkipsKnownSeeds(t *testing.T) {
	tr := dialTestTorrent(true, 0)
	tr.addrList.Push(testAddrs("10.2.0.1", "10.2.0.2", "10.2.0.3"), peersource.Tracker)
	tr.seedPeerIPs["10.2.0.1"] = struct{}{}
	tr.connectedPeerIPs["10.2.0.2"] = struct{}{}

	addr, _ := tr.nextDialAddress()
	if addr == nil || addr.IP.String() != "10.2.0.3" {
		t.Fatalf("next dial = %v, want the only unknown leecher", addr)
	}
	if addr, _ := tr.nextDialAddress(); addr != nil {
		t.Fatalf("next dial after the pool = %v, want nil", addr)
	}
}
