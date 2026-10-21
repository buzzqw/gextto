package gextto

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/buzzqw/gextto/internal/amule/amuletest"
)

// notifiedEvent is one webhook delivery seen by the test server.
type notifiedEvent struct {
	Event string         `json:"event"`
	Data  map[string]any `json:"data"`
}

// webhookRecorder starts a webhook that records every event it receives and
// returns a notifier that delivers to it.
func webhookRecorder(t *testing.T) (*Notifier, func() []notifiedEvent) {
	t.Helper()
	var mu sync.Mutex
	var events []notifiedEvent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var event notifiedEvent
		if err := json.Unmarshal(body, &event); err == nil {
			mu.Lock()
			events = append(events, event)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	cfg := DefaultConfig()
	cfg.NotifyWebhookURL = &server.URL
	return FromConfig(&cfg), func() []notifiedEvent {
		mu.Lock()
		defer mu.Unlock()
		return append([]notifiedEvent(nil), events...)
	}
}

func findNotified(events []notifiedEvent, name string) *notifiedEvent {
	for index := range events {
		if events[index].Event == name {
			return &events[index]
		}
	}
	return nil
}

// TestGapFillED2KDownloadSendsStartNotification: an episode found only on
// eD2k by the automatic gap filling is queued in aMule and announced like a
// torrent, with the download_started notification.
func TestGapFillED2KDownloadSendsStartNotification(t *testing.T) {
	core, coreCfg := ed2kTestCore(t, map[string]string{"ed2k_fallback": "true"})
	core.SetResults([]amuletest.Result{{
		Hash: "9a9a9a9a9a9a9a9a9a9a9a9a9a9a9a9a", Name: "Example S01E02 1080p WEB-DL ITA.mkv", Size: 900000000, Sources: 8,
	}}, "")
	state := newCycleState(t)
	cfg := *state.cfg
	cfg.Settings = map[string]string{}
	for key, value := range state.cfg.Settings {
		cfg.Settings[key] = value
	}
	for key, value := range coreCfg.Settings {
		cfg.Settings[key] = value
	}
	cfg.Series = []SeriesConfig{{Name: "Example", Enabled: true}}
	cfg.FeedURLs = nil
	cfg.Indexers = nil
	cfg.WebsearchEngines = nil
	cfg.DryRun = false

	// Episodes 1 and 3 are in the library, so episode 2 is a gap.
	for _, episode := range []int64{1, 3} {
		release := testRelease()
		release.Episode = int64Ptr(episode)
		release.EpisodeRange = []int64{episode}
		release.Title = strings.Replace(release.Title, "E01", "E0"+string(rune('0'+episode)), 1)
		release.Magnet = "magnet:?xt=urn:btih:" + strings.Repeat(string(rune('0'+episode)), 40)
		if _, _, err := state.db.CheckSeries(&release); err != nil {
			t.Fatal(err)
		}
	}
	notifier, events := webhookRecorder(t)

	domain := "series"
	stats, err := RunCycleDomain(context.Background(), &cfg, state.engine, state.db, state.archive, state.comics,
		notifier, state.activeEngine(), &domain)
	if err != nil {
		t.Fatalf("RunCycleDomain: %v", err)
	}
	if stats == nil || stats.DownloadsStarted != 1 {
		t.Fatalf("stats = %+v, want one download started", stats)
	}
	if queued := core.Downloads(); len(queued) != 1 || !strings.EqualFold(queued[0].Hash, "9a9a9a9a9a9a9a9a9a9a9a9a9a9a9a9a") {
		t.Fatalf("files queued in aMule = %+v, want the eD2k episode", queued)
	}
	started := findNotified(events(), "download_started")
	if started == nil {
		t.Fatalf("no download_started notification: %+v", events())
	}
	if title, _ := started.Data["title"].(string); !strings.Contains(title, "S01E02") {
		t.Fatalf("download_started title = %q, want the eD2k episode", title)
	}
}

// TestCompletedED2KDownloadSendsCompletionNotification: an eD2k file that
// completes is imported like a torrent and then announced with the
// torrent_completed notification, only once.
func TestCompletedED2KDownloadSendsCompletionNotification(t *testing.T) {
	f := newMovieImportFixture(t)
	session := &movieCopySession{}
	processed, err := HandleTorrentEvent(&f.cfg, session, f.db, map[string]struct{}{}, map[string]struct{}{},
		map[string]StorageMoveRetry{}, f.event, NewTmdbClient(nil), NewNotifier())
	if err != nil || !processed {
		t.Fatalf("HandleTorrentEvent: processed=%v err=%v", processed, err)
	}
	notifier, events := webhookRecorder(t)
	meta, err := f.db.TorrentMeta(f.hash)
	if err != nil || meta == nil {
		t.Fatalf("TorrentMeta: %v %v", meta, err)
	}
	announced := map[string]struct{}{}
	path := bg_announceCompletion(f.db, notifier, f.event, meta, false, announced)
	if !strings.HasPrefix(path, f.archive) {
		t.Fatalf("announced path = %q, want the library copy under %q", path, f.archive)
	}
	completed := findNotified(events(), "torrent_completed")
	if completed == nil {
		t.Fatalf("no torrent_completed notification: %+v", events())
	}
	if title, _ := completed.Data["title"].(string); title != f.release.Title {
		t.Fatalf("torrent_completed title = %q, want %q", title, f.release.Title)
	}
	if kind, _ := completed.Data["kind"].(string); kind != "movie" {
		t.Fatalf("torrent_completed kind = %q, want movie", kind)
	}

	// A repeated completion event must not notify a second time.
	bg_announceCompletion(f.db, notifier, f.event, meta, true, announced)
	count := 0
	for _, event := range events() {
		if event.Event == "torrent_completed" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("torrent_completed sent %d times, want 1", count)
	}
}
