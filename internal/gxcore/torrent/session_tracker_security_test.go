package torrent

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/trackermanager"
	"github.com/buzzqw/gextto/internal/gxcore/logging"
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
	var logs bytes.Buffer
	log := logger.New("session-tracker-opt-in")
	log.SetHandler(logging.NewWriterHandler(&logs))
	s := &Session{config: Config{WebSourcesAllowPrivate: true}, trackerManager: manager, log: log}
	got := s.parseTrackers([][]string{{"http://127.0.0.1:8080/announce"}}, false)
	manager.Close()
	if len(got) != 1 {
		t.Fatalf("opted-in metainfo trackers = %v, want one private tracker", trackerURLs(got))
	}
	if strings.Contains(logs.String(), "private or local address") {
		t.Fatalf("an allowed tracker was reported as skipped: %s", logs.String())
	}
}

func TestParseTrackersSaysWhyAPrivateTrackerIsSkipped(t *testing.T) {
	manager := trackermanager.New(nil, time.Second, false)
	var logs bytes.Buffer
	log := logger.New("session-tracker-refused")
	log.SetHandler(logging.NewWriterHandler(&logs))
	s := &Session{config: Config{}, trackerManager: manager, log: log}
	got := s.parseTrackers([][]string{{
		"http://192.168.1.10:6969/announce",
		"http://10.0.0.5:6969/announce",
		"http://8.8.8.8:8080/announce",
	}}, false)
	manager.Close()
	if len(got) != 1 {
		t.Fatalf("metainfo trackers = %v, want only the public tracker", trackerURLs(got))
	}
	out := logs.String()
	if !strings.Contains(out, "2 trackers are skipped") {
		t.Fatalf("the skipped trackers are not in the log: %s", out)
	}
	for _, want := range []string{"192.168.1.10", "private or local address", "-web-sources-allow-private"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the log does not say %q: %s", want, out)
		}
	}
}

func trackerURLs(trackers []tracker.Tracker) []string {
	urls := make([]string, len(trackers))
	for i, tr := range trackers {
		urls[i] = tr.URL()
	}
	return urls
}
