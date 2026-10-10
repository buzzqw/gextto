package main

import "testing"

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
