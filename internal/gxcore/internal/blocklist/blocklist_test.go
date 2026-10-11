package blocklist

import (
	"fmt"
	"net"
	"strings"
	"testing"
)

// TestFormats covers the extra blocklist formats added by the engine:
// name:range (P2P), "a.b.c.d - a.b.c.d , level , label" (eMule) and plain
// dotted ranges, on top of CIDR.
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
// separate interval list.
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

// TestIPv4MergeAndBoundaries covers the merged-range storage: overlapping and
// adjacent ranges collapse, and the 0.0.0.0 / 255.255.255.255 edges do not
// overflow the merge.
func TestIPv4MergeAndBoundaries(t *testing.T) {
	rules := `10.0.0.0/8
10.1.0.0-10.1.255.255
11.0.0.0-11.0.0.255
11.0.1.0-11.0.1.255
203.0.113.7-203.0.113.7
255.255.255.255-255.255.255.255
`
	b := New()
	if _, err := b.Reload(strings.NewReader(rules)); err != nil {
		t.Fatalf("reload: %v", err)
	}
	for ip, blocked := range map[string]bool{
		"9.255.255.255":   false,
		"10.0.0.0":        true,
		"10.1.2.3":        true,
		"10.255.255.255":  true,
		"11.0.0.0":        true,
		"11.0.1.255":      true,
		"11.0.2.0":        false,
		"203.0.113.6":     false,
		"203.0.113.7":     true,
		"203.0.113.8":     false,
		"255.255.255.254": false,
		"255.255.255.255": true,
		"0.0.0.0":         false,
	} {
		if got := b.Blocked(net.ParseIP(ip)); got != blocked {
			t.Errorf("%s blocked=%v want %v", ip, got, blocked)
		}
	}
}

// TestBlockedNoAllocation fixes that a lookup is just a lock plus a binary
// search: the blocklist is checked on every incoming/outgoing connection and
// tracker request, so it must not allocate.
func TestBlockedNoAllocation(t *testing.T) {
	b := New()
	if _, err := b.Reload(strings.NewReader("10.0.0.0/8\n203.0.113.0/24\n")); err != nil {
		t.Fatal(err)
	}
	blocked := net.ParseIP("10.1.2.3")
	missing := net.ParseIP("8.8.8.8")
	var wrong bool
	allocs := testing.AllocsPerRun(1000, func() {
		if !b.Blocked(blocked) || b.Blocked(missing) {
			wrong = true
		}
	})
	if wrong {
		t.Fatal("unexpected lookup result")
	}
	if allocs != 0 {
		t.Fatalf("Blocked allocated %.1f times per run, want 0", allocs)
	}
}

// BenchmarkBlocked measures the lookup cost on a blocklist built from many
// non-overlapping ranges.
func BenchmarkBlocked(b *testing.B) {
	var sb strings.Builder
	for i := 0; i < 100000; i++ {
		v := uint32(i * 2)
		fmt.Fprintf(&sb, "%d.%d.%d.%d-%d.%d.%d.%d\n",
			byte(v>>24), byte(v>>16), byte(v>>8), byte(v),
			byte(v>>24), byte(v>>16), byte(v>>8), byte(v|1))
	}
	bl := New()
	if _, err := bl.Reload(strings.NewReader(sb.String())); err != nil {
		b.Fatal(err)
	}
	ip := net.ParseIP("10.0.0.1")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bl.Blocked(ip)
	}
}
