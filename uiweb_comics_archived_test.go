package gextto

import (
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// TestTorrentRowShowsACompletedComicAsArchived pins the weekly-pack case: a
// comic completed by its own pipeline, paused after seeding and re-checked at
// 0 % once its files went to the library, shows as completed and archived at
// 100 %, not as a paused download at 0 %.
func TestTorrentRowShowsACompletedComicAsArchived(t *testing.T) {
	state := newTestAppState(t)
	const hash = "b91a4e4a0ae94cfbf628b1406f231d81ed873af4"
	if err := state.comics.AddTorrent(hash, "https://example.invalid/pack", "Weekly Pack 2026-10-07", "/downloads"); err != nil {
		t.Fatal(err)
	}
	if err := state.comics.CompleteTorrent(hash, "/downloads"); err != nil {
		t.Fatal(err)
	}
	state.setActiveEngine(fakeProblemsEngine{list: []models.TorrentView{
		{Hash: hash, Name: "2026.10.07 Weekly Pack", State: "paused", Progress: 0, TotalSize: 6 << 30},
	}})
	rows := uiTorrentRows(state)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if !row.Archived || row.State != "Completato · archiviato" || row.ProgressPct != "100.0" || row.TotalDone != row.TotalSize {
		t.Fatalf("row = archived %v, state %q, progress %s, done %d/%d", row.Archived, row.State, row.ProgressPct, row.TotalDone, row.TotalSize)
	}
}
