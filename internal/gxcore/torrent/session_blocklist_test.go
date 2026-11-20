package torrent

import "testing"

// TestNoteBlockedCountsEveryConnection pins the two figures the operator reads:
// the running total, and how many are waiting for the next aggregated line.
func TestNoteBlockedCountsEveryConnection(t *testing.T) {
	s := &Session{}
	for i := 0; i < 3; i++ {
		s.noteBlocked()
	}
	if got := s.blockedConnections.Load(); got != 3 {
		t.Fatalf("the total is %d, want 3", got)
	}
	// The first drop is reported at once and opens a window; the next two
	// accumulate for the following line.
	if got := s.blockedSince.Load(); got != 2 {
		t.Fatalf("the pending count is %d, want 2", got)
	}
}
