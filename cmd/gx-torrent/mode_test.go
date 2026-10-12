package main

import (
	"net"
	"testing"
)

// TestDefaultListen pins the exposure default: standalone listens on every
// interface so another PC on the LAN can reach the page and API, while managed
// stays on loopback because Gextto runs beside the daemon.
func TestDefaultListen(t *testing.T) {
	standalone := defaultListen(ModeStandalone)
	if loopback(standalone) {
		t.Fatalf("standalone default %q is loopback, LAN clients cannot reach it", standalone)
	}
	if _, port, err := net.SplitHostPort(standalone); err != nil || port != "8890" {
		t.Fatalf("standalone default %q is not host:8890", standalone)
	}
	if managed := defaultListen(ModeManaged); !loopback(managed) {
		t.Fatalf("managed default %q is not loopback", managed)
	}
}

func TestResolveMode(t *testing.T) {
	cases := []struct {
		flag, fingerprint string
		want              Mode
		wantErr           bool
	}{
		{"", "abc123", ModeManaged, false},  // started by Gextto
		{"", "", ModeStandalone, false},     // started by hand
		{"managed", "", ModeManaged, false}, // explicit
		{"standalone", "abc123", ModeStandalone, false},
		{"STANDALONE", "", ModeStandalone, false},
		{" managed ", "", ModeManaged, false},
		{"bogus", "", "", true},
	}
	for _, c := range cases {
		got, err := resolveMode(c.flag, c.fingerprint)
		if c.wantErr {
			if err == nil {
				t.Errorf("resolveMode(%q,%q): expected error", c.flag, c.fingerprint)
			}
			continue
		}
		if err != nil {
			t.Errorf("resolveMode(%q,%q): %v", c.flag, c.fingerprint, err)
			continue
		}
		if got != c.want {
			t.Errorf("resolveMode(%q,%q) = %q, want %q", c.flag, c.fingerprint, got, c.want)
		}
	}
}
