package gextto

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// uiProgressClass decides the colour of the bar: green when the payload is
// complete, grey for a paused download that still has data to fetch.
func TestUIProgressClass(t *testing.T) {
	cases := []struct {
		name     string
		state    string
		progress float64
		checking bool
		want     string
	}{
		{"a paused download with data to fetch stays grey", "paused", 40, false, "paused"},
		{"a finished download stays green when paused", "paused", 100, false, "seed"},
		{"the same when it reports 99.99 %", "paused", 99.99, false, "seed"},
		{"a queued complete download is green too", "queued", 100, false, "seed"},
		{"a paused download just under 100 % stays grey", "paused", 99.5, false, "paused"},
		{"seeding is green", "seeding", 100, false, "seed"},
		{"a stalled download is grey", "stalled", 12, false, "paused"},
		{"a downloading torrent is the accent colour", "downloading", 40, false, "active"},
		{"a failed download never looks finished", "error", 100, false, "active"},
		{"a file check keeps the bar blue while it runs", "checking_files", 100, true, "active"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := uiProgressClass(tc.state, tc.progress, tc.checking); got != tc.want {
				t.Fatalf("uiProgressClass(%q, %.2f, %v) = %q, want %q",
					tc.state, tc.progress, tc.checking, got, tc.want)
			}
		})
	}
}

// A finished movie paused after the seed limit has nothing left to fetch: its
// row is settled and draws no bar. What it keeps is the green class — not the
// grey one of a paused download — so wherever the bar is drawn again (the
// drawer, a check in progress) it is right. A paused download with data left
// keeps the grey bar and its percentage.
func TestDownloadsKeepTheGreenClassOfAFinishedMovie(t *testing.T) {
	state := newTestAppState(t)
	state.setActiveEngine(fakeProblemsEngine{list: []models.TorrentView{
		{
			Hash: "dddd000000000000000000000000000000000001", Name: "Movie.2026.1080p.mkv",
			State: "paused", Progress: 100, TotalSize: 100, TotalDone: 100,
		},
		{
			Hash: "dddd000000000000000000000000000000000002", Name: "Movie.2027.1080p.mkv",
			State: "paused", Progress: 40, TotalSize: 100, TotalDone: 40,
		},
	}})

	rows := uiTorrentRows(state)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].ProgressClass != "seed" {
		t.Fatalf("finished movie class = %q, want seed", rows[0].ProgressClass)
	}
	if !rows[0].Settled {
		t.Fatal("the finished movie is not settled: it would keep a full green bar and 100.0 %")
	}
	if rows[0].State != "In pausa" {
		t.Fatalf("the badge of the finished movie is %q, want the pause to still be said", rows[0].State)
	}
	if rows[1].ProgressClass != "paused" {
		t.Fatalf("paused download class = %q, want paused", rows[1].ProgressClass)
	}
	if rows[1].Settled {
		t.Fatal("the paused download with data left is settled: its percentage would disappear")
	}

	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	code, body := v2Request(t, server, http.MethodGet, "/?view=downloads", nil)
	if code != http.StatusOK {
		t.Fatalf("downloads page -> %d", code)
	}
	if !strings.Contains(body, `class="progress paused"`) {
		t.Fatal("the downloads page does not draw the paused download with the grey bar")
	}
	if strings.Contains(body, `class="progress seed"`) {
		t.Fatal("the downloads page still draws a bar for the finished movie")
	}
}

// An errored torrent is named, not left "Unknown": the detail page already
// promised the state in words.
func TestUIStateLabelNamesAnError(t *testing.T) {
	if got := uiStateLabel("error"); got != "In errore" {
		t.Fatalf("uiStateLabel(error) = %q, want In errore", got)
	}
	if got := uiStateClass("error"); got != "err" {
		t.Fatalf("uiStateClass(error) = %q, want err", got)
	}
}

// The engine's disk guard holds a download when its folder has no free space:
// the list says why, while the row stays a pause for the filters and buttons.
func TestDownloadsShowTheDiskGuardReason(t *testing.T) {
	state := newTestAppState(t)
	state.setActiveEngine(fakeProblemsEngine{list: []models.TorrentView{
		{
			Hash: "eeee000000000000000000000000000000000001", Name: "Movie.2028.1080p.mkv",
			State: "paused", Progress: 40, TotalSize: 100, TotalDone: 40, DiskWait: true,
		},
	}})

	rows := uiTorrentRows(state)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if row.State != "In attesa di spazio" {
		t.Fatalf("state = %q, want the disk guard reason", row.State)
	}
	if row.StateKey != "paused" || row.StateClass != "warn" {
		t.Fatalf("state key/class = %q/%q, want paused/warn", row.StateKey, row.StateClass)
	}
	if row.ProgressClass != "paused" {
		t.Fatalf("progress class = %q, want paused", row.ProgressClass)
	}

	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	code, body := v2Request(t, server, http.MethodGet, "/?view=downloads", nil)
	if code != http.StatusOK {
		t.Fatalf("downloads page -> %d", code)
	}
	if !strings.Contains(body, "In attesa di spazio") {
		t.Fatal("the downloads page does not name the disk guard pause")
	}
}

// The ratio is the last fact of a settled row: on desktop the facts keep their
// width and stay on one line even when a long name carries a «Perché» line.
func TestSettledRowFactsDoNotWrapOnDesktop(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	code, css := v2Request(t, server, http.MethodGet, "/static/gextto-ui.css", nil)
	if code != http.StatusOK {
		t.Fatalf("gextto-ui.css -> %d", code)
	}
	if !strings.Contains(css, ".torrent-card.is-settled > .torrent-body { flex: 0 0 auto;") {
		t.Fatal("the settled row's facts still shrink when the name is long")
	}
	if !strings.Contains(css, ".torrent-facts { display: flex; flex-wrap: nowrap;") {
		t.Fatal("the settled row's facts can still wrap onto a second line")
	}
}

// A torrent Gextto stopped at the end of its seed does not look like one the
// operator paused: the badge names the seed limit, while the state key and the
// play button keep working as before.
func TestDownloadsShowTheSeedLimitReason(t *testing.T) {
	state := newTestAppState(t)
	state.cfg.Transfer.SeedRatio = 0.1
	state.cfg.Transfer.SeedTimeMinutes = 5
	state.cfg.Transfer.SeedTimeDays = 0
	state.cfg.Transfer.SeedLimitsAll = true
	pinStateConfig(t, state)

	stopped := "dddd000000000000000000000000000000000003"
	manual := "dddd000000000000000000000000000000000004"
	state.setActiveEngine(fakeProblemsEngine{list: []models.TorrentView{
		{
			Hash: stopped, Name: "Movie.2029.1080p.mkv", State: "paused",
			Progress: 100, TotalSize: 100, TotalDone: 100,
			AllTimeDownload: 100, AllTimeUpload: 20, SeedingSeconds: 600,
			SeedRatio: -1, SeedDays: -1,
		},
		{
			Hash: manual, Name: "Movie.2030.1080p.mkv", State: "paused",
			Progress: 100, TotalSize: 100, TotalDone: 100,
			SeedRatio: -1, SeedDays: -1,
		},
	}})

	rows := uiTorrentRows(state)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	for _, row := range rows {
		switch row.Hash {
		case stopped:
			if !row.SeedLimitStop || row.State != "In pausa · limite seed" {
				t.Fatalf("seed-limit state = %q (flag %v), want the reason on the badge", row.State, row.SeedLimitStop)
			}
			if row.StateKey != "paused" || row.StateClass != "warn" {
				t.Fatalf("state key/class = %q/%q, want paused/warn", row.StateKey, row.StateClass)
			}
		case manual:
			if row.SeedLimitStop || row.State != "In pausa" {
				t.Fatalf("a torrent with no limit reached = %q (flag %v), want In pausa", row.State, row.SeedLimitStop)
			}
		}
	}

	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	code, body := v2Request(t, server, http.MethodGet, "/?view=downloads", nil)
	if code != http.StatusOK {
		t.Fatalf("downloads page -> %d", code)
	}
	if !strings.Contains(body, "In pausa · limite seed") {
		t.Fatal("the downloads page does not name the seed-limit pause")
	}
	if !strings.Contains(body, "ha raggiunto il limite di seed impostato") {
		t.Fatal("the seed-limit badge has no tooltip explaining the pause")
	}
}

// uiTorrentSettled decides which rows lose the bar. A finished torrent says
// "100 %" on every row of the list, while a file check has a bar worth
// watching and a failed torrent must not look finished.
func TestUITorrentSettled(t *testing.T) {
	cases := []struct {
		name     string
		state    string
		progress float64
		checking bool
		want     bool
	}{
		{"a seeding torrent has nothing left to fetch", "seeding", 100, false, true},
		{"a finished torrent too", "finished", 100, false, true},
		{"the 99.99 % the bar already draws as full counts", "seeding", 99.99, false, true},
		{"a completed download paused after the seed limit", "paused", 100, false, true},
		{"a paused download with data to fetch keeps its bar", "paused", 40, false, false},
		{"a downloading torrent keeps its bar", "downloading", 40, false, false},
		{"a file check is never settled: there the bar moves", "checking_files", 100, true, false},
		{"a failed torrent keeps the bar like it keeps its colour", "error", 100, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := uiTorrentSettled(tc.state, tc.progress, tc.checking); got != tc.want {
				t.Fatalf("uiTorrentSettled(%q, %.2f, %v) = %v, want %v",
					tc.state, tc.progress, tc.checking, got, tc.want)
			}
		})
	}
}

// The row of a settled torrent takes one line: the class says it, the size and
// the numbers that still move sit beside the name and the progress bar is not
// drawn. A download keeps the bar and the metrics line, unchanged.
func TestDownloadsPutASettledTorrentOnOneLine(t *testing.T) {
	seeded := "ffff000000000000000000000000000000000001"
	active := "ffff000000000000000000000000000000000002"
	state := newTestAppState(t)
	state.setActiveEngine(fakeProblemsEngine{list: []models.TorrentView{
		{
			Hash: seeded, Name: "Seeded.Movie.2026.mkv",
			State: "seeding", Progress: 100, TotalSize: 2048, TotalDone: 2048,
			UploadRate: 1024, NumPeers: 2, NumSeeds: 5, NumComplete: 12, NumIncomplete: 3,
		},
		{
			Hash: active, Name: "Downloading.Movie.2027.mkv",
			State: "downloading", Progress: 40, TotalSize: 100, TotalDone: 40,
			DownloadRate: 1024,
		},
	}})

	rows := uiTorrentRows(state)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	for _, row := range rows {
		switch row.Hash {
		case seeded:
			if !row.Settled {
				t.Fatal("the seeded torrent is not settled: its row would keep a full green bar and 100.0 %")
			}
		case active:
			if row.Settled {
				t.Fatal("the downloading torrent is settled: its bar would disappear while it moves")
			}
		}
	}

	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	code, body := v2Request(t, server, http.MethodGet, "/?view=downloads", nil)
	if code != http.StatusOK {
		t.Fatalf("downloads page -> %d", code)
	}
	rowMarkup := func(hash string) string {
		start := strings.Index(body, `id="v2-torrent-`+hash+`"`)
		if start < 0 {
			t.Fatalf("the page does not draw the row %s", hash)
		}
		end := strings.Index(body[start:], "</tr>")
		if end < 0 {
			t.Fatalf("the row %s is not closed", hash)
		}
		return body[start : start+end]
	}

	settledRow := rowMarkup(seeded)
	if !strings.Contains(settledRow, "torrent-card is-settled") {
		t.Fatal("the settled row does not carry the is-settled class")
	}
	if strings.Contains(settledRow, "torrent-progress") {
		t.Fatal("the settled row still draws the progress bar")
	}
	if !strings.Contains(settledRow, "torrent-facts") {
		t.Fatal("the settled row does not carry the size and the live numbers beside the name")
	}
	if strings.Contains(settledRow, "100.0%") {
		t.Fatal("the settled row still prints the percentage")
	}

	activeRow := rowMarkup(active)
	if strings.Contains(activeRow, "is-settled") {
		t.Fatal("the downloading row lost its bar")
	}
	if !strings.Contains(activeRow, "torrent-progress") || !strings.Contains(activeRow, "40.0%") {
		t.Fatal("the downloading row no longer shows the bar and the percentage")
	}
	if !strings.Contains(activeRow, "torrent-metrics") {
		t.Fatal("the downloading row no longer shows the metrics line")
	}
}
