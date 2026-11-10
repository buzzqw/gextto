package gextto

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

// An idle torrent gets no history; a busy one keeps one until the whole window
// is idle again or it leaves the session.
func TestTorrentRateHistoryKeepsOnlyBusyTorrents(t *testing.T) {
	h := newTorrentRateHistory()
	now := time.Now()
	h.record(now, []models.TorrentView{{Hash: "AA", DownloadRate: 0}, {Hash: "BB", DownloadRate: 50 << 10}})
	if got := len(h.samples("aa")); got != 0 {
		t.Fatalf("idle torrent kept %d samples", got)
	}
	if got := len(h.samples("bb")); got != 1 {
		t.Fatalf("busy torrent kept %d samples, want 1", got)
	}
	// Idle but inside the window: the history stays and grows.
	h.record(now.Add(rateSampleInterval), []models.TorrentView{{Hash: "BB"}})
	if got := len(h.samples("BB")); got != 2 {
		t.Fatalf("history after an idle tick = %d, want 2", got)
	}
	// Past the window with only idle samples left: dropped.
	h.record(now.Add(rateHistoryWindow+2*rateSampleInterval), []models.TorrentView{{Hash: "BB"}})
	if got := len(h.samples("BB")); got != 0 {
		t.Fatalf("an idle window kept %d samples", got)
	}
	// Leaving the session drops the history at once.
	h.record(now, []models.TorrentView{{Hash: "CC", UploadRate: 1 << 20}})
	h.record(now.Add(rateSampleInterval), nil)
	if got := len(h.samples("CC")); got != 0 {
		t.Fatalf("removed torrent kept %d samples", got)
	}
}

func TestTorrentRateHistoryIsBounded(t *testing.T) {
	h := newTorrentRateHistory()
	start := time.Now()
	view := []models.TorrentView{{Hash: "AA", DownloadRate: 1 << 20}}
	for i := 0; i < rateHistoryLen*2; i++ {
		h.record(start.Add(time.Duration(i)*time.Second), view)
	}
	if got := len(h.samples("AA")); got != rateHistoryLen {
		t.Fatalf("ring holds %d samples, want %d", got, rateHistoryLen)
	}
	views := make([]models.TorrentView, torrentRateHistoryMax+10)
	for i := range views {
		views[i] = models.TorrentView{Hash: strings.Repeat("x", i+1), DownloadRate: 1 << 20}
	}
	h.record(start, views)
	h.mu.RLock()
	n := len(h.rings)
	h.mu.RUnlock()
	if n > torrentRateHistoryMax {
		t.Fatalf("%d torrents with a history, cap %d", n, torrentRateHistoryMax)
	}
}

func TestTorrentSpeedViewFrom(t *testing.T) {
	now := time.Now()
	if v := v2TorrentSpeedViewFrom(nil, now); v.HasData || !v.Idle {
		t.Fatalf("no samples: %+v", v)
	}
	samples := []torrentRate{
		{At: now.Add(-2 * time.Minute), Down: 2 << 20, Up: 0},
		{At: now.Add(-time.Minute), Down: 4 << 20, Up: 1 << 20},
	}
	v := v2TorrentSpeedViewFrom(samples, now)
	if !v.HasData || v.DownPoints == "" || v.UpPoints == "" || v.DownArea == "" {
		t.Fatalf("chart not drawn: %+v", v)
	}
	if v.PeakDown != "4.0 MB/s" && !strings.Contains(v.PeakDown, "4") {
		t.Fatalf("peak down %q", v.PeakDown)
	}
	if !strings.Contains(v.AvgDown, "3") {
		t.Fatalf("average down %q, want about 3 MiB/s", v.AvgDown)
	}
}

func TestQbitPieceRuns(t *testing.T) {
	runs := qbitPieceRuns([]int{2, 2, 1, 0, 0, 2})
	want := []TorrentPieceRun{{0, 1, "have"}, {2, 2, "downloading"}, {3, 4, ""}, {5, 5, "have"}}
	if len(runs) != len(want) {
		t.Fatalf("runs = %+v", runs)
	}
	for i := range want {
		if runs[i] != want[i] {
			t.Fatalf("run %d = %+v, want %+v", i, runs[i], want[i])
		}
	}
	if got := qbitPieceRuns(nil); len(got) != 0 {
		t.Fatalf("no metadata gave %+v", got)
	}
}

func TestV2AvailCells(t *testing.T) {
	cells := v2AvailCells(&TorrentPieceAvailability{Cells: []int{-1, 0, 2, 4}, Missing: 3, Most: 4})
	if cells[0].Class != "done" || cells[1].Class != "none" || cells[2].Class != "avail" || cells[3].Class != "avail" {
		t.Fatalf("classes %+v", cells)
	}
	if cells[3].Op != "1.00" || cells[2].Op >= cells[3].Op {
		t.Fatalf("shading %q %q: more holders must read fuller", cells[2].Op, cells[3].Op)
	}
	if cells[2].Holders != 2 {
		t.Fatalf("holders %d", cells[2].Holders)
	}
}

func TestSessionTotalsView(t *testing.T) {
	state := &AppState{rate_history: newRateHistory(4)}
	if v := v2SessionTotalsFrom(state); v.Known {
		t.Fatal("totals known before any reading")
	}
	state.rate_history.totals.set(3<<30, 1<<30, true)
	v := v2SessionTotalsFrom(state)
	if !v.Known || v.Down == "" || v.Up == "" {
		t.Fatalf("totals %+v", v)
	}
	if v := v2SessionTotalsFrom(&AppState{}); v.Known {
		t.Fatal("a hand-built state reported totals")
	}
}

// fakePieceMapEngine adds the availability the gx-torrent daemon reports.
type fakePieceMapEngine struct{ fakePieceEngine }

func (f fakePieceMapEngine) PieceMap(hash string) ([]TorrentPieceRun, *TorrentPieceAvailability, bool, error) {
	runs, _, _ := f.PieceRuns(hash)
	return runs, &TorrentPieceAvailability{Cells: []int{-1, 0, 3}, Missing: 2, Most: 3}, true, nil
}

// The Pieces tab draws the availability bar under the map, and the Speed tab
// draws the torrent's own history.
func TestV2DetailAvailabilityAndSpeedTabs(t *testing.T) {
	state := newTestAppState(t)
	hash := strings.Repeat("a", 40)
	state.setActiveEngine(fakePieceMapEngine{fakePieceEngine{hash: hash}})
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/downloads/detail/panel?hash="+hash+"&tab=pieces", nil)
	if code != http.StatusOK {
		t.Fatalf("pieces tab -> %d", code)
	}
	for _, marker := range []string{`class="avail-bar"`, "avail-none", "avail-done", "avail-avail"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("pieces tab missing %q:\n%s", marker, body)
		}
	}

	code, body = v2Request(t, server, http.MethodGet, "/downloads/detail/panel?hash="+hash+"&tab=speed", nil)
	if code != http.StatusOK || !strings.Contains(body, "detail-traffic") || strings.Contains(body, "traffic-chart") {
		t.Fatalf("speed tab without history -> %d:\n%s", code, body)
	}
	now := time.Now()
	state.rate_history.torrents.record(now.Add(-time.Minute), []models.TorrentView{{Hash: hash, DownloadRate: 1 << 20}})
	state.rate_history.torrents.record(now, []models.TorrentView{{Hash: hash, DownloadRate: 2 << 20}})
	_, body = v2Request(t, server, http.MethodGet, "/downloads/detail/panel?hash="+hash+"&tab=speed", nil)
	if !strings.Contains(body, "traffic-chart") || !strings.Contains(body, "polyline") {
		t.Fatalf("speed tab with history did not draw the chart:\n%s", body)
	}
}
