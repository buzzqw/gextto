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

// The row and the page carry the class: a finished movie paused in the list
// shows a full green bar, not the grey one of a paused download.
func TestDownloadsShowAGreenBarForAFinishedMovie(t *testing.T) {
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
	if rows[0].State != "In pausa" {
		t.Fatalf("the badge of the finished movie is %q, want the pause to still be said", rows[0].State)
	}
	if rows[1].ProgressClass != "paused" {
		t.Fatalf("paused download class = %q, want paused", rows[1].ProgressClass)
	}

	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	code, body := v2Request(t, server, http.MethodGet, "/?view=downloads", nil)
	if code != http.StatusOK {
		t.Fatalf("downloads page -> %d", code)
	}
	if !strings.Contains(body, `class="progress seed"`) {
		t.Fatal("the downloads page does not draw the finished movie with the green bar")
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
