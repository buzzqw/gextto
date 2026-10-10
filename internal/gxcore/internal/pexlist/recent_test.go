package pexlist

import (
	"net"
	"testing"
)

// TestRecentlySeenIgnoresIPv6 locks in that an IPv6 peer is not stored as
// 0.0.0.0: the list only tracks IPv4 contacts (it feeds the IPv4 dropped part
// of a PEX message).
func TestRecentlySeenIgnoresIPv6(t *testing.T) {
	l := &RecentlySeen{}
	l.Add(&net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 1})
	l.Add(&net.TCPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 2})
	if l.Len() != 1 {
		t.Fatalf("len = %d, want 1 (only the IPv4 peer)", l.Len())
	}
	if got := l.Peers()[0].IP; got != [4]byte{1, 2, 3, 4} {
		t.Fatalf("stored %v, want 1.2.3.4", got)
	}
}
