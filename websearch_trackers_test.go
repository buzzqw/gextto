package gextto

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resetWebsearchTrackers empties the downloaded list for one test and puts the
// previous one back afterwards.
func resetWebsearchTrackers(t *testing.T) {
	t.Helper()
	websearchTrackerState.Lock()
	previous := websearchTrackerState.trackers
	websearchTrackerState.trackers = nil
	websearchTrackerState.Unlock()
	t.Cleanup(func() {
		websearchTrackerState.Lock()
		websearchTrackerState.trackers = previous
		websearchTrackerState.Unlock()
	})
}

func TestParseWebsearchTrackersKeepsOnlyValidUniqueURLs(t *testing.T) {
	list := parseWebsearchTrackers(strings.NewReader(
		"# comment\nudp://tracker.example:1337/announce\n\nhttps://t.example/announce\n" +
			"udp://tracker.example:1337/announce\nnot a url\nftp://x.example/\nwss://w.example/\n"))
	want := []string{"udp://tracker.example:1337/announce", "https://t.example/announce", "wss://w.example/"}
	if len(list) != len(want) {
		t.Fatalf("trackers = %v, want %v", list, want)
	}
	for i := range want {
		if list[i] != want[i] {
			t.Errorf("tracker %d = %q, want %q", i, list[i], want[i])
		}
	}
}

func TestParseWebsearchTrackersStopsAtTheCap(t *testing.T) {
	var body strings.Builder
	for i := 0; i < websearchTrackerListMax*2; i++ {
		fmt.Fprintf(&body, "udp://tracker%d.example:1337/announce\n", i)
	}
	if got := len(parseWebsearchTrackers(strings.NewReader(body.String()))); got != websearchTrackerListMax {
		t.Fatalf("trackers taken = %d, want the cap %d", got, websearchTrackerListMax)
	}
}

// TestWebsearchTrackersFallBackUntilTheListArrives: a magnet must carry
// trackers even before the first download, and the downloaded list replaces the
// built-in one once it is there.
func TestWebsearchTrackersFallBackUntilTheListArrives(t *testing.T) {
	resetWebsearchTrackers(t)
	if got := currentWebsearchTrackers(); len(got) != len(fallbackWebsearchTrackers) {
		t.Fatalf("trackers before the download = %v, want the fallback list", got)
	}
	magnet := buildMagnet("0123456789012345678901234567890123456789", "Example Show")
	if !strings.Contains(magnet, "&tr="+url.QueryEscape(fallbackWebsearchTrackers[0])) {
		t.Fatalf("magnet without the fallback trackers: %q", magnet)
	}
	setWebsearchTrackers([]string{"udp://only.example:6969/announce"})
	got := currentWebsearchTrackers()
	if len(got) != 1 || got[0] != "udp://only.example:6969/announce" {
		t.Fatalf("trackers after the download = %v", got)
	}
	setWebsearchTrackers(nil)
	if len(currentWebsearchTrackers()) != 1 {
		t.Fatal("an empty list must keep the previous one")
	}
}

func TestWebsearchTrackerCacheSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	trackers := []string{"udp://a.example:1337/announce", "https://b.example/announce"}
	if err := saveWebsearchTrackerCache(dir, trackers); err != nil {
		t.Fatal(err)
	}
	loaded := loadWebsearchTrackerCache(dir)
	if len(loaded) != len(trackers) {
		t.Fatalf("loaded = %v, want %v", loaded, trackers)
	}
	for i := range trackers {
		if loaded[i] != trackers[i] {
			t.Errorf("tracker %d = %q, want %q", i, loaded[i], trackers[i])
		}
	}
	if missing := loadWebsearchTrackerCache(t.TempDir()); missing != nil {
		t.Fatalf("a folder without the file = %v, want nothing", missing)
	}
	if err := saveWebsearchTrackerCache(dir, nil); err != nil {
		t.Fatalf("saving an empty list = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, websearchTrackerListFile+".tmp")); !os.IsNotExist(err) {
		t.Fatal("the temporary file must not be left behind")
	}
}

func TestRefreshWebsearchTrackersStoresTheList(t *testing.T) {
	resetWebsearchTrackers(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "udp://fresh.example:1337/announce")
		fmt.Fprintln(w, "https://fresh2.example/announce")
	}))
	defer server.Close()
	dir := t.TempDir()
	count, err := refreshWebsearchTrackers(context.Background(), dir, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("trackers taken = %d, want 2", count)
	}
	if got := currentWebsearchTrackers(); len(got) != 2 || got[0] != "udp://fresh.example:1337/announce" {
		t.Fatalf("trackers in use = %v", got)
	}
	if cached := loadWebsearchTrackerCache(dir); len(cached) != 2 {
		t.Fatalf("cached = %v, want the two downloaded trackers", cached)
	}
}

// TestRefreshWebsearchTrackersKeepsThePreviousListOnFailure: a failed download
// must not empty the list in use, and a reply without trackers is a failure.
func TestRefreshWebsearchTrackersKeepsThePreviousListOnFailure(t *testing.T) {
	resetWebsearchTrackers(t)
	setWebsearchTrackers([]string{"udp://kept.example:1337/announce"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "<html><body>down for maintenance</body></html>")
	}))
	defer server.Close()
	if _, err := refreshWebsearchTrackers(context.Background(), t.TempDir(), server.URL); err == nil {
		t.Fatal("a page without trackers must be an error")
	}
	if got := currentWebsearchTrackers(); len(got) != 1 || got[0] != "udp://kept.example:1337/announce" {
		t.Fatalf("trackers in use after a failed download = %v", got)
	}
}
