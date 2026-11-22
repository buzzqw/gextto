package torrent

import (
	"net"
	"testing"
)

// TestAnnounceIPv6Selection: only a global unicast address is published. A
// link-local one is never routable, a ULA is reachable only inside the LAN, and
// a temporary or deprecated address (the flags the system reports) is refused
// even when it is global: publishing it would list an endpoint that rotates
// away or that peers cannot reach.
func TestAnnounceIPv6Selection(t *testing.T) {
	global := net.ParseIP("2001:db8:1::10")
	other := net.ParseIP("2001:db8:2::20")
	temporary := net.ParseIP("2001:db8:1::ffff:10")
	skip := func(ip net.IP) bool { return ip.Equal(temporary) }

	cases := []struct {
		name       string
		candidates []net.IP
		skip       func(net.IP) bool
		want       string
	}{
		{"no candidates", nil, nil, ""},
		{"only link-local and ULA", []net.IP{
			net.ParseIP("fe80::1"), net.ParseIP("fd00::2"),
		}, nil, ""},
		{"IPv4 is not published here", []net.IP{net.ParseIP("192.0.2.7")}, nil, ""},
		{"global wins over the local ones", []net.IP{
			net.ParseIP("fe80::1"), net.ParseIP("fd00::2"), global,
		}, nil, global.String()},
		{"temporary is skipped", []net.IP{temporary, other}, skip, other.String()},
		{"all skipped", []net.IP{temporary}, skip, ""},
		{"loopback and unspecified", []net.IP{net.ParseIP("::1"), net.ParseIP("::")}, nil, ""},
	}
	for _, c := range cases {
		var got string
		if ip := announceIPv6(c.candidates, c.skip); ip != nil {
			got = ip.String()
		}
		if got != c.want {
			t.Errorf("%s: announceIPv6 = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestParseIfInet6 reads the flags column the Linux kernel exposes, so a
// privacy address (temporary, 0x01) or one on its way out (deprecated, 0x20)
// can be told apart from a permanent global one (0x80).
func TestParseIfInet6(t *testing.T) {
	data := []byte(
		"20010db8000100000000000000000010 02 40 00 80 eth0\n" +
			"20010db80001000000000000ffff0010 02 40 00 01 eth0\n" +
			"20010db8000100000000000000000020 02 40 00 20 eth0\n" +
			"fe800000000000000000000000000001 02 40 20 80 eth0\n" +
			"\n" +
			"garbage line\n")

	flags := parseIfInet6(data)
	if len(flags) != 4 {
		t.Fatalf("parsed %d entries, want 4", len(flags))
	}
	get := func(s string) uint8 { return flags[ipKey(net.ParseIP(s))] }
	if f := get("2001:db8:1::10"); f != 0x80 {
		t.Errorf("permanent global flags = %#x, want 0x80", f)
	}
	if f := get("2001:db8:1::ffff:10"); f != ifaFTemporary {
		t.Errorf("temporary flags = %#x, want %#x", f, ifaFTemporary)
	}
	if f := get("2001:db8:1::20"); f != ifaFDeprecated {
		t.Errorf("deprecated flags = %#x, want %#x", f, ifaFDeprecated)
	}
	if f := get("fe80::1"); f&(ifaFTemporary|ifaFDeprecated|ifaFTentative) != 0 {
		t.Errorf("link-local flags = %#x, want none of the unusable bits", f)
	}
	// An address the file does not mention is not judged unusable.
	if _, ok := flags[ipKey(net.ParseIP("2001:db8:9::1"))]; ok {
		t.Error("an unknown address must not appear in the map")
	}
}

// TestListensIPv6: the published address is only meaningful when the listener
// accepts IPv6 at all. A v4 literal or 0.0.0.0 says no; the wildcard and a
// scoped literal say yes.
func TestListensIPv6(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"", true},
		{"::", true},
		{"0.0.0.0", false},
		{"192.168.1.10", false},
		{"2001:db8::1", true},
		{"fe80::1%eth0", true},
	}
	for _, c := range cases {
		s := &Session{config: Config{Host: c.host}}
		if got := s.listensIPv6(); got != c.want {
			t.Errorf("listensIPv6(%q) = %v, want %v", c.host, got, c.want)
		}
	}

	// No listener at all: nothing is published even with a global address.
	off := &Session{config: Config{Host: "::", ListenPort: 0}}
	if got := off.announceIPv6Address(); got != "" {
		t.Errorf("announceIPv6Address with no listener = %q, want empty", got)
	}
}

// TestIPKeyIsTheSixteenByteForm: the address is compared with the kernel's
// 16-byte form, not with whatever shape the interface list returned (a 4-in-6
// or a 16-byte slice both key the same).
func TestIPKeyIsTheSixteenByteForm(t *testing.T) {
	a := ipKey(net.ParseIP("2001:db8::1"))
	b := ipKey(net.ParseIP("2001:db8:0:0:0:0:0:1"))
	if a != b {
		t.Fatalf("keys differ: %x vs %x", a, b)
	}
}
