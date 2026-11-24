package torrent

import (
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/trackermanager"
)

func TestParseTrackersTreatsMetainfoAsUntrusted(t *testing.T) {
	manager := trackermanager.New(nil, time.Second, false)
	s := &Session{config: Config{}, trackerManager: manager}
	got := s.parseTrackers([][]string{{
		"http://127.0.0.1:8080/announce",
		"http://8.8.8.8:8080/announce",
	}}, false)
	manager.Close()
	if len(got) != 1 || got[0].URL() != "http://8.8.8.8:8080/announce" {
		t.Fatalf("metainfo trackers = %v, want only the public tracker", trackerURLs(got))
	}
}

func TestParseTrackersHonorsPrivateSourceOptIn(t *testing.T) {
	manager := trackermanager.NewWithPolicy(nil, time.Second, false, true)
	s := &Session{config: Config{WebSourcesAllowPrivate: true}, trackerManager: manager}
	got := s.parseTrackers([][]string{{"http://127.0.0.1:8080/announce"}}, false)
	manager.Close()
	if len(got) != 1 {
		t.Fatalf("opted-in metainfo trackers = %v, want one private tracker", trackerURLs(got))
	}
}

func trackerURLs(trackers []tracker.Tracker) []string {
	urls := make([]string, len(trackers))
	for i, tr := range trackers {
		urls[i] = tr.URL()
	}
	return urls
}
