package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
)

func torrentTrackerURLs(t *testing.T, d *Daemon, hash string) []string {
	t.Helper()
	d.mu.Lock()
	tr, _ := d.findLocked(hash)
	d.mu.Unlock()
	if tr == nil {
		t.Fatal("torrent not found")
	}
	var out []string
	for _, tracker := range tr.Trackers() {
		out = append(out, tracker.URL)
	}
	return out
}

func trackerOff(t *testing.T, d *Daemon, hash string) []string {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	_, meta := d.findLocked(hash)
	if meta == nil {
		t.Fatal("torrent not found")
	}
	return append([]string(nil), meta.TrackerOff...)
}

// TestTrackerToggleKeepsTheURL switches a tracker off and on again without
// removing it: the engine stops announcing, the state remembers the URL, the
// view lists it as disabled, and a restart does not bring it back.
func TestTrackerToggleKeepsTheURL(t *testing.T) {
	d := newTestDaemon(t)
	src := t.TempDir()
	hash, _, err := d.add(addRequest{
		TorrentData: makeTorrent(t, t.TempDir(), "movie.bin", 10_000),
		Destination: src, Paused: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	offURL := "http://127.0.0.1:1/announce"
	onURL := "http://127.0.0.1:2/announce"
	if err := d.addTrackers(hash, []string{offURL, onURL}); err != nil {
		t.Fatal(err)
	}
	if got := torrentTrackerURLs(t, d, hash); len(got) != 2 {
		t.Fatalf("trackers after add = %v, want two", got)
	}

	if err := d.setTrackerEnabled(hash, offURL, false); err != nil {
		t.Fatal(err)
	}
	if got := torrentTrackerURLs(t, d, hash); !slices.Equal(got, []string{onURL}) {
		t.Fatalf("trackers after disable = %v, want [%s]", got, onURL)
	}
	if got := trackerOff(t, d, hash); !slices.Equal(got, []string{offURL}) {
		t.Fatalf("tracker off list = %v, want [%s]", got, offURL)
	}

	// The inspect view lists both: the enabled one and the switched-off one.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/torrents/"+hash+"/trackers", nil)
	req.SetPathValue("hash", hash)
	req.SetPathValue("what", "trackers")
	rec := httptest.NewRecorder()
	d.handleInspect(rec, req)
	var view []trackerInfo
	if err := json.NewDecoder(rec.Body).Decode(&view); err != nil {
		t.Fatalf("trackers view: %v", err)
	}
	if len(view) != 2 {
		t.Fatalf("trackers view = %+v, want two entries", view)
	}
	disabled := false
	for _, item := range view {
		if item.URL == offURL {
			disabled = !item.Enabled && item.Status == "disabled"
		}
	}
	if !disabled {
		t.Fatalf("switched-off tracker is not shown as disabled: %+v", view)
	}

	// A resume written by an older version can bring the tracker back: the
	// reconcile pass removes it again.
	d.mu.Lock()
	tr, _ := d.findLocked(hash)
	d.mu.Unlock()
	if err := tr.AddTracker(offURL); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	d.reconcileLocked()
	d.mu.Unlock()
	if got := torrentTrackerURLs(t, d, hash); !slices.Equal(got, []string{onURL}) {
		t.Fatalf("trackers after reconcile = %v, want [%s]", got, onURL)
	}

	// The state file carries the switched-off URL across restarts.
	data, err := readFileString(d.opts.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(data, "tracker_off") || !strings.Contains(data, offURL) {
		t.Fatalf("state does not carry the switched-off tracker:\n%s", data)
	}

	if err := d.setTrackerEnabled(hash, offURL, true); err != nil {
		t.Fatal(err)
	}
	if got := torrentTrackerURLs(t, d, hash); !slices.Equal(got, []string{onURL, offURL}) {
		t.Fatalf("trackers after enable = %v, want both", got)
	}
	if got := trackerOff(t, d, hash); len(got) != 0 {
		t.Fatalf("tracker off list after enable = %v, want empty", got)
	}

	// Removing a switched-off tracker forgets the URL instead of leaving it
	// waiting in the off list.
	if err := d.setTrackerEnabled(hash, offURL, false); err != nil {
		t.Fatal(err)
	}
	if err := d.removeTrackers(hash, []string{offURL}); err != nil {
		t.Fatal(err)
	}
	if got := trackerOff(t, d, hash); len(got) != 0 {
		t.Fatalf("removed tracker is still in the off list: %v", got)
	}
}

// TestTrackerToggleFromThePage covers the button the tracker tab posts.
func TestTrackerToggleFromThePage(t *testing.T) {
	d := newTestDaemon(t)
	hash, _, err := d.add(addRequest{
		TorrentData: makeTorrent(t, t.TempDir(), "movie.bin", 10_000),
		Destination: t.TempDir(), Paused: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	tracker := "http://127.0.0.1:1/announce"
	if err := d.addTrackers(hash, []string{tracker}); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"hash": {hash}, "tab": {"trackers"}, "op": {"toggle"}, "url": {tracker}, "enabled": {"0"}}
	req := httptest.NewRequest(http.MethodPost, "/ui/trackers", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-GX-Fetch", "1")
	rec := httptest.NewRecorder()
	d.handleUITrackers(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle = %d: %s", rec.Code, rec.Body.String())
	}
	if got := trackerOff(t, d, hash); !slices.Equal(got, []string{tracker}) {
		t.Fatalf("tracker off list = %v, want [%s]", got, tracker)
	}
	if got := torrentTrackerURLs(t, d, hash); len(got) != 0 {
		t.Fatalf("trackers after page toggle = %v, want none", got)
	}
}

// readFileString is a tiny helper so the test states what it is reading.
func readFileString(path string) (string, error) {
	data, err := os.ReadFile(path)
	return string(data), err
}

// TestTrackerEnabledAction covers the REST action a client calls.
func TestTrackerEnabledAction(t *testing.T) {
	d := newTestDaemon(t)
	hash, _, err := d.add(addRequest{
		TorrentData: makeTorrent(t, t.TempDir(), "movie.bin", 10_000),
		Destination: t.TempDir(), Paused: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	tracker := "http://127.0.0.1:1/announce"
	if err := d.addTrackers(hash, []string{tracker}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	resp, err := http.PostForm(srv.URL+"/api/v1/torrents/"+hash+"/tracker-enabled", url.Values{
		"url": {tracker}, "enabled": {"0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tracker-enabled = %d", resp.StatusCode)
	}
	if got := trackerOff(t, d, hash); !slices.Equal(got, []string{tracker}) {
		t.Fatalf("tracker off list = %v, want [%s]", got, tracker)
	}
}

// TestAddTrackersPartialFailureKeepsTheGoodOne covers a mixed add: a URL that
// joins must not stay marked as switched off because a later one is refused.
func TestAddTrackersPartialFailureKeepsTheGoodOne(t *testing.T) {
	d := newTestDaemon(t)
	hash, _, err := d.add(addRequest{
		TorrentData: makeTorrent(t, t.TempDir(), "movie.bin", 10_000),
		Destination: t.TempDir(), Paused: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	good := "http://127.0.0.1:1/announce"
	if err := d.addTrackers(hash, []string{good}); err != nil {
		t.Fatal(err)
	}
	if err := d.setTrackerEnabled(hash, good, false); err != nil {
		t.Fatal(err)
	}
	if err := d.addTrackers(hash, []string{good, "not-a-tracker-url"}); err == nil {
		t.Fatal("an invalid tracker URL was accepted")
	}
	if got := torrentTrackerURLs(t, d, hash); !slices.Equal(got, []string{good}) {
		t.Fatalf("trackers after a mixed add = %v, want [%s]", got, good)
	}
	if got := trackerOff(t, d, hash); len(got) != 0 {
		t.Fatalf("the good tracker is still switched off: %v", got)
	}
}
