package torrent

// tracker_latency_test.go locks the announce latency figures on their way out
// of the engine: milliseconds, the median, and the announce in flight.

import (
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/rpctypes"
)

func TestTrackerToRPCCarriesTheAnnounceLatency(t *testing.T) {
	status := Tracker{
		URL:           "http://tracker.test/announce",
		Status:        Working,
		Seeders:       12,
		Leechers:      4,
		InFlight:      true,
		LastLatency:   1500 * time.Millisecond,
		MedianLatency: 480 * time.Millisecond,
	}
	out := trackerToRPC(status)
	if out.InFlight != true {
		t.Fatal("an announce in flight must be reported waiting")
	}
	if out.LastAnnounceMs != 1500 || out.MedianAnnounceMs != 480 {
		t.Fatalf("latencies = %d/%d ms, want 1500/480", out.LastAnnounceMs, out.MedianAnnounceMs)
	}
	if out.URL != status.URL || out.Seeders != 12 || out.Leechers != 4 {
		t.Fatalf("tracker fields lost: %+v", out)
	}
	var zero rpctypes.Tracker
	if got := trackerToRPC(Tracker{}); got.LastAnnounceMs != 0 || got.MedianAnnounceMs != 0 || got.InFlight != zero.InFlight {
		t.Fatalf("an announce never answered must report zero, got %+v", got)
	}
}
