package pexlist

import (
	"net"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

func TestPEXListSeparatesFamilies(t *testing.T) {
	l := New()
	l.Add(&net.TCPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 6881})
	l.Add(&net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 6882})

	added, added6, dropped, dropped6 := l.Flush()
	if len(dropped) != 0 || len(dropped6) != 0 {
		t.Fatalf("nothing dropped expected, got %d/%d", len(dropped), len(dropped6))
	}
	// IPv4 part: one 6-byte compact record.
	if len(added) != 6 {
		t.Fatalf("added = %d bytes, want 6", len(added))
	}
	a4, err := tracker.DecodePeersCompact([]byte(added))
	if err != nil || len(a4) != 1 || !a4[0].IP.Equal(net.IPv4(1, 2, 3, 4)) || a4[0].Port != 6881 {
		t.Fatalf("ipv4 part: %v %v", a4, err)
	}
	// IPv6 part: one 18-byte compact record.
	if len(added6) != 18 {
		t.Fatalf("added6 = %d bytes, want 18", len(added6))
	}
	a6, err := tracker.DecodePeersCompact6([]byte(added6))
	if err != nil || len(a6) != 1 || !a6[0].IP.Equal(net.ParseIP("2001:db8::1")) || a6[0].Port != 6882 {
		t.Fatalf("ipv6 part: %v %v", a6, err)
	}

	// A flush empties the list.
	added, added6, _, _ = l.Flush()
	if added != "" || added6 != "" {
		t.Fatalf("list not emptied: %q %q", added, added6)
	}
}

func TestPEXListDropRoutesByFamily(t *testing.T) {
	l := New()
	l.Drop(&net.TCPAddr{IP: net.ParseIP("2001:db8::2"), Port: 1})
	_, _, dropped, dropped6 := l.Flush()
	if dropped != "" || len(dropped6) != 18 {
		t.Fatalf("dropped=%q dropped6=%d", dropped, len(dropped6))
	}
}
