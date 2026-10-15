package gextto

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

type fakeProblemsEngine struct {
	TorrentEngine
	list []models.TorrentView
}

func (fakeProblemsEngine) Name() string                                  { return BackendGxTorrent }
func (f fakeProblemsEngine) List() []models.TorrentView                  { return f.list }
func (fakeProblemsEngine) Files(string) ([]models.FileView, bool, error) { return nil, true, nil }

// TestDashboardProblemsHidesCompletedCycles: a warning around a download whose
// cycle ended well (its file was imported into the library, or it superseded an
// existing copy) is noise and does not belong in "Da controllare"; a salvage
// (🛟) still does, even though its message also says "added to the library".
func TestDashboardProblemsHidesCompletedCycles(t *testing.T) {
	state := newTestAppState(t)
	state.setActiveEngine(fakeProblemsEngine{})
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	imported := "bbbb000000000000000000000000000000000001"
	upgraded := "bbbb000000000000000000000000000000000002"
	unfinished := "bbbb000000000000000000000000000000000003"
	salvaged := "bbbb000000000000000000000000000000000004"
	now := time.Now()
	for _, event := range []logging.Event{
		// Cycle ended well, then a spurious finalization warning.
		{At: now.Add(-6 * time.Hour), Level: logging.LevelInfo, Hash: imported, Message: "📁 Silo S02E09 added to the library (5.0 GB): /lib/Silo.mkv"},
		{At: now.Add(-1 * time.Hour), Level: logging.LevelWarn, Hash: imported, Message: "completed torrent finalization failed"},
		// Superseded an existing copy (♻️), then a warning.
		{At: now.Add(-5 * time.Hour), Level: logging.LevelInfo, Hash: upgraded, Message: "♻️ Silo updated (11.0 GB): /lib/Silo.mkv · 1 previous version «old.mkv» moved to the trash"},
		{At: now.Add(-1 * time.Hour), Level: logging.LevelWarn, Hash: upgraded, Message: "completed torrent finalization failed"},
		// Never imported: the warning stays.
		{At: now.Add(-1 * time.Hour), Level: logging.LevelWarn, Hash: unfinished, Message: "completed torrent finalization failed"},
		// A salvage is still something to check.
		{At: now.Add(-1 * time.Hour), Level: logging.LevelInfo, Hash: salvaged, Message: "🛟 «Pack»: 3 episodes already complete, added to the library before giving up (partial)"},
	} {
		if err := state.db.RecordAcquisitionEvent(event); err != nil {
			t.Fatal(err)
		}
	}

	code, body := v2Request(t, server, http.MethodGet, "/dashboard/problems", nil)
	if code != http.StatusOK {
		t.Fatalf("panel -> %d", code)
	}
	for _, hash := range []string{imported, upgraded} {
		if strings.Contains(body, "hash="+hash) {
			t.Fatalf("a completed cycle must not be listed (%s): %s", hash, body)
		}
	}
	for _, hash := range []string{unfinished, salvaged} {
		if !strings.Contains(body, "hash="+hash) {
			t.Fatalf("problem %s must be listed: %s", hash, body)
		}
	}
}

// TestDashboardProblemsPanel: il riquadro "Da controllare" mostra il torrent
// fermo con l'abbandono previsto, lo spostamento in sospeso e l'avviso recente
// di un altro download, senza ripetere quelli già elencati sopra.
func TestDashboardProblemsPanel(t *testing.T) {
	state := newTestAppState(t)
	stuck := "aaaa000000000000000000000000000000000001"
	moving := "aaaa000000000000000000000000000000000002"
	gone := "aaaa000000000000000000000000000000000003"
	state.setActiveEngine(fakeProblemsEngine{list: []models.TorrentView{
		{Hash: stuck, Name: "Morgane.S04", State: "stalled", Stalled: true, HasMetadata: true, NumComplete: 0},
		{Hash: moving, Name: "Marshals.S01E10", State: "seeding", Progress: 100},
	}})
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	// Nothing yet: the panel says so.
	code, body := v2Request(t, server, http.MethodGet, "/dashboard/problems", nil)
	if code != http.StatusOK || !strings.Contains(body, "Niente da controllare") {
		t.Fatalf("empty panel -> %d: %s", code, body)
	}

	since := time.Now().Add(-2 * time.Hour)
	if err := state.db.SaveStallWatch(stuck, StallWatch{lastProgressAt: since, stalledSince: &since, nextRetryAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := state.db.SaveTorrentMove(moving, TorrentMoveState{Retry: &StorageMoveRetry{destination: "/nas/Marshals", attempts: 3}}); err != nil {
		t.Fatal(err)
	}
	for _, event := range []logging.Event{
		{At: time.Now(), Level: logging.LevelWarn, Hash: stuck, Message: "⏸️ Morgane is stuck"},
		{At: time.Now(), Level: logging.LevelWarn, Hash: gone, Message: "❌ Gave up on FBI"},
		{At: time.Now().Add(-48 * time.Hour), Level: logging.LevelError, Hash: gone, Message: "old error"},
		{At: time.Now(), Level: logging.LevelInfo, Hash: gone, Message: "📥 Downloading FBI"},
	} {
		if err := state.db.RecordAcquisitionEvent(event); err != nil {
			t.Fatal(err)
		}
	}

	code, body = v2Request(t, server, http.MethodGet, "/dashboard/problems", nil)
	if code != http.StatusOK {
		t.Fatalf("panel -> %d", code)
	}
	giveUp := since.Add(72 * time.Hour).Local().Format("02/01 15:04")
	for _, want := range []string{"Morgane.S04", "Nessun seeder nello swarm.", giveUp, "Marshals.S01E10", "/nas/Marshals", "Gave up on FBI", "/series/history?hash=" + gone} {
		if !strings.Contains(body, want) {
			t.Fatalf("panel lacks %q: %s", want, body)
		}
	}
	for _, unwanted := range []string{"Morgane is stuck", "old error", "Downloading FBI", "Niente da controllare"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("panel must not show %q: %s", unwanted, body)
		}
	}

	// The Storia button opens the story of that download.
	code, body = v2Request(t, server, http.MethodGet, "/series/history?hash="+gone+"&modal=1", nil)
	if code != http.StatusOK || !strings.Contains(body, "v2-history-title") || !strings.Contains(body, "Downloading FBI") {
		t.Fatalf("history by hash -> %d: %s", code, body)
	}
	// The dashboard loads the panel.
	code, body = v2Request(t, server, http.MethodGet, "/?view=dashboard", nil)
	if code != http.StatusOK || !strings.Contains(body, `hx-get="/dashboard/problems"`) || !strings.Contains(body, `id="v2-modal"`) {
		t.Fatalf("dashboard lacks the problems panel -> %d", code)
	}
}
