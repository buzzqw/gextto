package gextto

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// TestDownloadsShowFileCheckProgress: while the engine re-checks the files on
// disk the download percentage does not move, so the row shows the pieces
// verified so far instead: a bar that grows and the count next to it.
func TestDownloadsShowFileCheckProgress(t *testing.T) {
	state := newTestAppState(t)
	state.setActiveEngine(fakeProblemsEngine{list: []models.TorrentView{
		{
			Hash: "cccc000000000000000000000000000000000001", Name: "Show S01E01",
			State: "checking_files", Progress: 100,
			PiecesTotal: 200, PiecesChecked: 50,
		},
		{
			Hash: "cccc000000000000000000000000000000000002", Name: "Show S01E02",
			State: "downloading", Progress: 40, TotalSize: 100, TotalDone: 40,
		},
	}})

	rows := uiTorrentRows(state)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	checking := rows[0]
	if !checking.Checking || checking.State != "Verifica file" {
		t.Fatalf("checking row = %+v", checking)
	}
	if checking.PiecesChecked != 50 || checking.PiecesTotal != 200 || checking.ProgressPct != "25.0" {
		t.Fatalf("check progress = %s (%d/%d), want 25.0 (50/200)", checking.ProgressPct, checking.PiecesChecked, checking.PiecesTotal)
	}
	if checking.ProgressClass != "active" {
		t.Fatalf("check progress class = %q, want active", checking.ProgressClass)
	}
	if rows[1].Checking || rows[1].ProgressPct != "40.0" {
		t.Fatalf("downloading row changed: %+v", rows[1])
	}

	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	code, body := v2Request(t, server, http.MethodGet, "/?view=downloads", nil)
	if code != http.StatusOK {
		t.Fatalf("downloads page -> %d", code)
	}
	for _, want := range []string{"Verifica file", "25.0%", "50/200"} {
		if !strings.Contains(body, want) {
			t.Fatalf("downloads page is missing %q", want)
		}
	}
}
