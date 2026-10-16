package main

import "testing"

// TestPprofAddress pins the security property of the opt-in profiling endpoint:
// only a loopback address is accepted, so GX_TORRENT_PPROF can never expose the
// Go profiles on the network. A bare port defaults to 127.0.0.1.
func TestPprofAddress(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"", "", false},
		{"   ", "", false},
		{"6060", "", false},
		{"127.0.0.1", "", false},
		{":6060", "127.0.0.1:6060", true},
		{"127.0.0.1:6060", "127.0.0.1:6060", true},
		{"localhost:6060", "localhost:6060", true},
		{"[::1]:6060", "[::1]:6060", true},
		{"0.0.0.0:6060", "", false},
		{"192.168.1.10:6060", "", false},
		{"example.com:6060", "", false},
		{"8.8.8.8:6060", "", false},
	}
	for _, tc := range cases {
		got, ok := pprofAddress(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("pprofAddress(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
