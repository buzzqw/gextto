package gextto

import (
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func TestUpgradeReasonUntilStopsAtCutoffButKeepsRepacks(t *testing.T) {
	existing := models.Quality{Resolution: "1080p", Source: "webdl", Codec: "h264"}
	better := models.Quality{Resolution: "2160p", Source: "webdl", Codec: "h265"}
	if got := upgradeReasonUntil(&better, &existing, 2480, 1280, 200, 0); got == "" {
		t.Fatal("without a cutoff the 2160p release is an upgrade")
	}
	if got := upgradeReasonUntil(&better, &existing, 2480, 1280, 200, 1200); got != "" {
		t.Fatalf("cutoff reached: expected no upgrade, got %q", got)
	}
	if got := upgradeReasonUntil(&better, &existing, 2480, 1280, 200, 1500); got == "" {
		t.Fatal("cutoff not reached yet: the upgrade must still be taken")
	}
	repack := existing
	repack.IsRepack = true
	if got := upgradeReasonUntil(&repack, &existing, 1300, 1280, 200, 1200); got != "repack" {
		t.Fatalf("a REPACK must pass the cutoff, got %q", got)
	}
}

func TestUpgradeUntilBlocksSeriesReplacement(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	var seriesID int64
	if err := db.db.QueryRow("SELECT id FROM series WHERE name='Show'").Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,quality_score,downloaded_at,archive_path) VALUES (?1,1,1,'Show.S01E01.1080p.WEB-DL',1280,'2024-01-01T00:00:00+00:00','/nas/e1.mkv')", seriesID); err != nil {
		t.Fatal(err)
	}
	candidate := testRelease()
	candidate.Series = stringPtr("Show")
	candidate.Season = int64Ptr(1)
	candidate.Episode = int64Ptr(1)
	candidate.IsPack = false
	candidate.Quality = models.Quality{Resolution: "2160p", Source: "webdl"}
	score := candidate.Quality.Score()
	index := &models.ArchiveQualityIndex{Best: map[[2]int64]models.ArchiveQuality{
		{1, 1}: {Quality: models.Quality{Resolution: "1080p", Source: "webdl"}, Score: 1280},
	}}
	context := &models.ApprovalContext{Archive: index, Live: &models.LiveDownloads{}, DryRun: true}
	approved, reason, err := db.CheckSeriesScored(&candidate, score, 200, context)
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, approved, "without a cutoff expected an upgrade, got "+reason)

	context.UpgradeUntilScore = 1200
	approved, reason, err = db.CheckSeriesScored(&candidate, score, 200, context)
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, !approved, "cutoff reached: expected a refusal, got "+reason)
}

func TestUpgradeUntilBlocksMovieReplacement(t *testing.T) {
	db := newTestDB(t)
	old := models.Release{
		Title:   "Example Movie",
		Magnet:  "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Source:  "rss",
		Quality: models.Quality{Resolution: "1080p", Source: "webdl"},
		Kind:    "movie",
		Year:    int64Ptr(2024),
		Seeders: -1,
		Peers:   -1,
	}
	if approved, reason, err := db.CheckMovie(&old); err != nil || !approved {
		t.Fatalf("first download refused: %v %s", err, reason)
	}
	if err := db.RegisterTorrent(&old); err != nil {
		t.Fatal(err)
	}
	upgrade := old
	upgrade.Magnet = "magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	upgrade.Quality.Resolution = "2160p"
	approved, _, err := db.checkMovieScoredWith(&upgrade, upgrade.Quality.Score(), 200, 0, false, true)
	if err != nil || !approved {
		t.Fatalf("without a cutoff the 2160p movie is an upgrade: %v", err)
	}
	approved, reason, err := db.checkMovieScoredWith(&upgrade, upgrade.Quality.Score(), 200, old.Quality.Score(), false, true)
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, !approved, "cutoff reached: expected a refusal, got "+reason)
}
