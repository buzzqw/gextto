package torrent

import (
	"testing"
	"time"
)

func TestSessionBanIP(t *testing.T) {
	s := &Session{bannedIPs: map[string]time.Time{}}
	if s.IsBannedIP("1.2.3.4") {
		t.Fatal("an unknown IP must not be banned")
	}
	s.BanIP("1.2.3.4", time.Hour)
	if !s.IsBannedIP("1.2.3.4") {
		t.Fatal("a banned IP must be reported")
	}
	s.BanIP("", time.Hour) // empty IP: no-op, must not panic

	s.bannedIPs["5.6.7.8"] = time.Now().Add(-time.Second)
	if s.IsBannedIP("5.6.7.8") {
		t.Fatal("an expired ban must not apply")
	}
	if _, ok := s.bannedIPs["5.6.7.8"]; ok {
		t.Fatal("an expired entry must be pruned")
	}
}
