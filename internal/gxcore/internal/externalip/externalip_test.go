package externalip

import (
	"net"
	"testing"
)

// TestIsPublicIPv6 covers the exclusions used when collecting this host's own
// global IPv6 addresses.
func TestIsPublicIPv6(t *testing.T) {
	cases := map[string]bool{
		"2001:4860:4860::8888": true,
		"fc00::1":              false, // ULA
		"fd12:3456::1":         false, // ULA
		"fe80::1":              false, // link-local
		"::1":                  false, // loopback
		"::":                   false, // unspecified
		"ff02::1":              false, // multicast
	}
	for ip, want := range cases {
		if got := isPublicIPv6(net.ParseIP(ip)); got != want {
			t.Errorf("isPublicIPv6(%s) = %v, want %v", ip, got, want)
		}
	}
}
