package blocklist

import (
	"net"
	"strings"
	"testing"
)

// TestFormats covers the extra blocklist formats added by the engine:
// name:range (P2P), "a.b.c.d - a.b.c.d , level , label" (eMule) and plain
// dotted ranges, on top of upstream CIDR.
func TestFormats(t *testing.T) {
	rules := `# comment
10.0.0.0/8
Bad people:1.2.3.0-1.2.3.255
005.006.007.000 - 005.006.007.010 , 000 , emule
009.009.009.000 - 009.009.009.255 , 200 , allowed
2001:db8::-2001:db8::ff
`
	b := New()
	n, err := b.Reload(strings.NewReader(rules))
	if err != nil || n != 3 {
		t.Fatalf("loaded %d rules: %v", n, err)
	}
	for ip, blocked := range map[string]bool{
		"10.1.2.3": true, "1.2.3.200": true, "5.6.7.9": true, "5.6.7.11": false,
		"9.9.9.9": false, "8.8.8.8": false,
	} {
		if got := b.Blocked(net.ParseIP(ip)); got != blocked {
			t.Errorf("%s blocked=%v want %v", ip, got, blocked)
		}
	}
}

// TestIPv6CIDR covers IPv6 CIDR rules, which the engine keeps in a
// separate interval list (the upstream engine skipped them).
func TestIPv6CIDR(t *testing.T) {
	rules := `2001:db8::/32
fd00::/8
# IPv6 ranges are not supported, only the CIDR form
2001:db8:1::-2001:db8:1::ff
10.0.0.0/8
`
	b := New()
	n, err := b.Reload(strings.NewReader(rules))
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if n != 3 {
		t.Fatalf("loaded %d rules, want 3 (2 IPv6 CIDR + 1 IPv4)", n)
	}
	for ip, blocked := range map[string]bool{
		"2001:db8::1": true, "2001:db8:ffff::1": true, "2001:db9::1": false,
		"fd00::1": true, "fe80::1": false, "fc00::1": false,
		"10.1.1.1": true, "11.1.1.1": false,
	} {
		if got := b.Blocked(net.ParseIP(ip)); got != blocked {
			t.Errorf("%s blocked=%v want %v", ip, got, blocked)
		}
	}
}
