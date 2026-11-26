package torrent

import (
	"fmt"
	"testing"
	"time"
)

// Bans of peers never seen again expire only when their own address is
// checked: the list must still shrink, or corrupt peers with unique
// addresses grow it for the whole session.
func TestExpiredBansAreSweptAsTheListGrows(t *testing.T) {
	s := &Session{}
	for i := 0; i < 10000; i++ {
		s.BanIP(fmt.Sprintf("192.0.2.%d:%d", i%250, i), -time.Second) // already expired
	}
	s.BanIP("198.51.100.1", time.Hour)
	s.mBannedIPs.Lock()
	n := len(s.bannedIPs)
	s.mBannedIPs.Unlock()
	if n >= 2*minBannedSweep {
		t.Fatalf("%d bans kept, want the expired ones swept (bound %d)", n, 2*minBannedSweep)
	}
	if !s.IsBannedIP("198.51.100.1") {
		t.Fatal("a ban still in force was swept")
	}
}
