package torrent

import (
	"net"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bandwidth"
)

// limitedTorrent is a bare torrent whose own limiters run at 1 KiB/s, with the
// local limiters wired as finishConstruction does.
func limitedTorrent(limitLocal bool) *torrent {
	t := &torrent{
		bucketDownload:      bandwidth.New(1024),
		bucketUpload:        bandwidth.New(1024),
		bucketLocalDownload: bandwidth.New(0),
		bucketLocalUpload:   bandwidth.New(0),
	}
	t.applyLocalPeerLimits(limitLocal)
	return t
}

// throttled reports whether taking a megabyte from l makes the caller wait.
func throttled(l *bandwidth.Limiter) bool {
	return l.Take(1<<20) > 0
}

func TestIsLocalPeerAddr(t *testing.T) {
	cases := []struct {
		addr  net.Addr
		local bool
	}{
		{&net.TCPAddr{IP: net.ParseIP("192.168.1.20"), Port: 6881}, true},
		{&net.TCPAddr{IP: net.ParseIP("10.0.0.7"), Port: 6881}, true},
		{&net.TCPAddr{IP: net.ParseIP("172.16.3.4"), Port: 6881}, true},
		{&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 6881}, true},
		{&net.TCPAddr{IP: net.ParseIP("169.254.10.1"), Port: 6881}, true},
		{&net.TCPAddr{IP: net.ParseIP("fd12:3456::1"), Port: 6881}, true}, // IPv6 ULA
		{&net.TCPAddr{IP: net.ParseIP("fe80::1"), Port: 6881}, true},
		{&net.UDPAddr{IP: net.ParseIP("192.168.1.20"), Port: 6881}, true}, // uTP
		{&net.TCPAddr{IP: net.ParseIP("8.8.8.8"), Port: 6881}, false},
		{&net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 6881}, false},
		{&net.UDPAddr{IP: net.ParseIP("1.1.1.1"), Port: 6881}, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := isLocalPeerAddr(c.addr); got != c.local {
			t.Errorf("isLocalPeerAddr(%v) = %v, want %v", c.addr, got, c.local)
		}
	}
}

// TestLocalPeersIgnoreLimitsByDefault: a LAN peer gets limiters that never
// make it wait, a public peer gets the torrent's limited ones.
func TestLocalPeersIgnoreLimitsByDefault(t *testing.T) {
	tr := limitedTorrent(false)
	down, up := tr.peerLimiters(&net.TCPAddr{IP: net.ParseIP("192.168.1.20"), Port: 6881})
	if throttled(down) || throttled(up) {
		t.Fatal("a LAN peer must not be throttled when the limits skip the local network")
	}
	down, up = tr.peerLimiters(&net.TCPAddr{IP: net.ParseIP("8.8.8.8"), Port: 6881})
	if down != tr.bucketDownload || up != tr.bucketUpload {
		t.Fatal("a public peer must hold the torrent limiters")
	}
	if !throttled(down) || !throttled(up) {
		t.Fatal("a public peer must be throttled by a 1 KiB/s limit")
	}
}

// TestLocalPeersLimitedWhenAsked: with the option on, a LAN peer is throttled
// like any other, and switching it reaches the limiters the peer already holds.
func TestLocalPeersLimitedWhenAsked(t *testing.T) {
	tr := limitedTorrent(true)
	down, up := tr.peerLimiters(&net.TCPAddr{IP: net.ParseIP("10.0.0.7"), Port: 6881})
	if !throttled(down) || !throttled(up) {
		t.Fatal("a LAN peer must be throttled when the limits apply to the local network")
	}
	tr.applyLocalPeerLimits(false)
	if throttled(down) || throttled(up) {
		t.Fatal("lifting the LAN limits must reach the limiters a connected peer holds")
	}
	tr.applyLocalPeerLimits(true)
	if !throttled(down) {
		t.Fatal("applying the LAN limits again must reach a connected peer")
	}
}

// TestSessionSetLimitLocalPeersReachesTorrents: the session switch updates
// every registered torrent.
func TestSessionSetLimitLocalPeersReachesTorrents(t *testing.T) {
	tr := limitedTorrent(false)
	s := &Session{torrents: map[string]*Torrent{"a": {torrent: tr}}}
	down, _ := tr.peerLimiters(&net.TCPAddr{IP: net.ParseIP("192.168.0.2"), Port: 1})
	s.SetLimitLocalPeers(true)
	if !s.LimitLocalPeers() || !throttled(down) {
		t.Fatal("SetLimitLocalPeers(true) must limit the LAN peers of every torrent")
	}
	s.SetLimitLocalPeers(false)
	if s.LimitLocalPeers() || throttled(down) {
		t.Fatal("SetLimitLocalPeers(false) must lift the LAN limits of every torrent")
	}
}

// TestProxiedPeersAreNeverLocal: behind a proxy a connection reports the
// proxy's address, so a proxy on the LAN must not lift the limits of every peer.
func TestProxiedPeersAreNeverLocal(t *testing.T) {
	tr := limitedTorrent(false)
	tr.session = &Session{config: Config{Proxy: "socks5://192.168.1.2:1080"}}
	down, up := tr.peerLimiters(&net.TCPAddr{IP: net.ParseIP("192.168.1.2"), Port: 1080})
	if !throttled(down) || !throttled(up) {
		t.Fatal("a peer reached through a proxy must keep the torrent limits")
	}
}
