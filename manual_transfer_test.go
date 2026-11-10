package gextto

// manual_transfer_test.go pins where a download added by hand ends: a free
// search result stays in the download folder, outside the library, while a
// release chosen for the library (an episode searched from the series page)
// is imported like any other.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

// The free search route records the transfer reason, the library route the
// plain manual one.
func TestManualAddsRecordWhereTheyEnd(t *testing.T) {
	state := newTestAppState(t)
	free := models.Release{Title: "Series.A.S01E05.1080p.WEB.H264-GROUP", Magnet: "magnet:?xt=urn:btih:" + strings.Repeat("5", 40), Kind: "series"}
	chosen := models.Release{Title: "Series.A.S01E06.1080p.WEB.H264-GROUP", Magnet: "magnet:?xt=urn:btih:" + strings.Repeat("6", 40), Kind: "series"}
	if status, value := addParsedReleaseWithReason(state, free, manualTransferReason); status != http.StatusAccepted {
		t.Fatalf("free search add -> %d %v", status, value)
	}
	if status, value := AddParsedRelease(state, chosen); status != http.StatusAccepted {
		t.Fatalf("library add -> %d %v", status, value)
	}
	if reason, _ := state.db.TorrentReason(strings.Repeat("5", 40)); reason == nil || *reason != manualTransferReason {
		t.Fatalf("free search reason = %v, want %q", reason, manualTransferReason)
	}
	if reason, _ := state.db.TorrentReason(strings.Repeat("6", 40)); reason == nil || *reason != manualReason {
		t.Fatalf("library reason = %v, want %q", reason, manualReason)
	}

	// The HTTP route of the search results is the free one.
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	third := `{"release":{"title":"Series.A.S01E07.1080p.WEB.H264-GROUP","magnet":"magnet:?xt=urn:btih:` + strings.Repeat("7", 40) + `","kind":"series"}}`
	response, err := http.Post(server.URL+"/api/search/add", "application/json", strings.NewReader(third))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode == http.StatusAccepted {
		if reason, _ := state.db.TorrentReason(strings.Repeat("7", 40)); reason == nil || *reason != manualTransferReason {
			t.Fatalf("/api/search/add reason = %v, want %q", reason, manualTransferReason)
		}
	} else if response.StatusCode != http.StatusConflict {
		// A test state without the initial setup answers 409 before queuing.
		t.Fatalf("/api/search/add -> %d", response.StatusCode)
	}
}

// A free search result of a followed series completes where it was
// downloaded: no move, no rename, nothing recorded in the library, and the
// seed policy never moves it there afterwards.
func TestFreeSearchDownloadStaysInTransfer(t *testing.T) {
	p := newEpisodeEnv(t)
	p.cfg.RenameEpisodes = true
	if err := p.db.SetTorrentReason(episodeHash, manualTransferReason); err != nil {
		t.Fatal(err)
	}
	if p.finish(t) {
		t.Fatal("a free search download was processed as a library import")
	}
	source := filepath.Join(p.downloads, p.name)
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("the download left the transfer folder: %v", err)
	}
	if got := libraryVideos(t, p.library); len(got) != 0 {
		t.Fatalf("library holds %v, want nothing", got)
	}
	status, _ := p.db.TorrentStatus(episodeHash)
	if status == nil || *status != "completed" {
		t.Fatalf("status = %v, want completed", status)
	}
	if archived, _ := p.db.TorrentProcessed(episodeHash); archived != nil && strings.TrimSpace(*archived) != "" {
		t.Fatalf("a library path was recorded: %q", *archived)
	}
	if p.session.moves != 0 {
		t.Fatal("a free search download was moved")
	}

	// A later storage move (the engine relocating it) imports nothing either.
	event := models.TorrentEvent{Kind: "storage_moved", Hash: episodeHash, Name: p.name, SavePath: p.downloads}
	if processed, err := HandleTorrentEvent(p.cfg, p.session, p.db, p.moves, p.postSeed, p.retries, event,
		NewTmdbClientWithLanguage(nil, "it-IT"), FromConfig(p.cfg)); processed || err != nil {
		t.Fatalf("storage_moved: processed=%v err=%v", processed, err)
	}

	// At the end of its seed it is stopped, not moved into the library.
	view := p.session.list[0]
	view.AllTimeDownload, view.AllTimeUpload, view.SeedingSeconds, view.AutoManaged = 100, 1000, 60, true
	view.SeedRatio, view.SeedDays = -1, -1
	p.cfg.Transfer.SeedRatio = 0.5
	p.cfg.Transfer.SeedTimeDays = 365
	seeding := &pauseCountingSession{stubTorrentSession: stubTorrentSession{list: []models.TorrentView{view}}}
	EnforceSeedPolicy(p.cfg, seeding, p.db, map[string]struct{}{}, map[string]StorageMoveRetry{}, map[string]time.Time{})
	if len(seeding.paused) != 1 {
		t.Fatalf("paused %v, want the seed stopped at its ratio", seeding.paused)
	}
	if len(seeding.moved) != 0 {
		t.Fatalf("moved %v after the seed, want it left in the transfer folder", seeding.moved)
	}
}

// The same download chosen for the library goes there, as before.
func TestLibraryManualDownloadIsImported(t *testing.T) {
	p := newEpisodeEnv(t)
	if err := p.db.SetTorrentReason(episodeHash, manualReason); err != nil {
		t.Fatal(err)
	}
	if !p.finish(t) {
		t.Fatal("a release chosen for the library was not imported")
	}
	if got := libraryVideos(t, p.library); len(got) != 1 {
		t.Fatalf("library holds %v, want the episode", got)
	}
}
