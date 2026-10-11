package announcer

import (
	"context"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

// fakeTracker records the events of the announces it receives.
type fakeTracker struct {
	events []tracker.Event
}

func (f *fakeTracker) Announce(ctx context.Context, req tracker.AnnounceRequest) (*tracker.AnnounceResponse, error) {
	f.events = append(f.events, req.Event)
	return &tracker.AnnounceResponse{Interval: time.Minute}, nil
}

func (f *fakeTracker) Scrape(ctx context.Context, req tracker.ScrapeRequest) (*tracker.ScrapeResponse, error) {
	return nil, tracker.ErrScrapeNotSupported
}

func (f *fakeTracker) URL() string { return "http://tracker.test/announce" }

func announceEvent(t *testing.T, torrent tracker.Torrent, event tracker.Event) tracker.Event {
	t.Helper()
	ft := &fakeTracker{}
	respC := make(chan *tracker.AnnounceResponse, 1)
	errC := make(chan error, 1)
	announce(context.Background(), ft, event, 50, torrent, respC, errC)
	select {
	case <-respC:
	case err := <-errC:
		t.Fatalf("announce: %v", err)
	case <-time.After(time.Second):
		t.Fatal("announce did not complete")
	}
	if len(ft.events) != 1 {
		t.Fatalf("recorded %d events, want 1", len(ft.events))
	}
	return ft.events[0]
}

func TestAnnouncePartialSeedUsesPaused(t *testing.T) {
	partial := tracker.Torrent{PartialSeed: true}
	if got := announceEvent(t, partial, tracker.EventNone); got != tracker.EventPaused {
		t.Errorf("periodic announce of a partial seed = %v, want paused", got)
	}
	if got := announceEvent(t, partial, tracker.EventStarted); got != tracker.EventPaused {
		t.Errorf("started announce of a partial seed = %v, want paused", got)
	}
	if got := announceEvent(t, partial, tracker.EventCompleted); got != tracker.EventPaused {
		t.Errorf("completed announce of a partial seed = %v, want paused", got)
	}
	// A "stopped" event keeps its meaning: the peer is leaving the swarm.
	if got := announceEvent(t, partial, tracker.EventStopped); got != tracker.EventStopped {
		t.Errorf("stopped announce of a partial seed = %v, want stopped", got)
	}
}

func TestAnnounceFullSeedKeepsEvent(t *testing.T) {
	full := tracker.Torrent{PartialSeed: false}
	if got := announceEvent(t, full, tracker.EventNone); got != tracker.EventNone {
		t.Errorf("periodic announce of a downloader = %v, want empty", got)
	}
	if got := announceEvent(t, full, tracker.EventCompleted); got != tracker.EventCompleted {
		t.Errorf("completed announce = %v, want completed", got)
	}
}
