package tracker

import "testing"

func TestEventNames(t *testing.T) {
	cases := []struct {
		event Event
		name  string
	}{
		{EventNone, "empty"},
		{EventCompleted, "completed"},
		{EventStarted, "started"},
		{EventStopped, "stopped"},
		{EventPaused, "paused"},
	}
	for _, c := range cases {
		if got := c.event.String(); got != c.name {
			t.Errorf("Event(%d).String() = %q, want %q", int32(c.event), got, c.name)
		}
	}
	// BEP 21 adds "paused" after the BEP 15 values (0..3).
	if int32(EventPaused) != 4 {
		t.Errorf("EventPaused = %d, want 4", int32(EventPaused))
	}
}
