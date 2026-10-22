package gextto

import (
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func TestUpgradeReasonUntilStopsAtCutoffButKeepsRepacks(t *testing.T) {
	existing := models.Quality{Resolution: "1080p", Source: "webdl", Codec: "h264"}
	better := models.Quality{Resolution: "2160p", Source: "webdl", Codec: "h265"}
	if got := upgradeReasonUntil(&better, &existing, 2480, 1280, 200, 0, nil); got == "" {
		t.Fatal("without a cutoff the 2160p release is an upgrade")
	}
	if got := upgradeReasonUntil(&better, &existing, 2480, 1280, 200, 1200, nil); got != "" {
		t.Fatalf("cutoff reached: expected no upgrade, got %q", got)
	}
	if got := upgradeReasonUntil(&better, &existing, 2480, 1280, 200, 1500, nil); got == "" {
		t.Fatal("cutoff not reached yet: the upgrade must still be taken")
	}
	repack := existing
	repack.IsRepack = true
	if got := upgradeReasonUntil(&repack, &existing, 1300, 1280, 200, 1200, nil); got != "repack" {
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
	approved, _, err := db.checkMovieScoredWith(&upgrade, upgrade.Quality.Score(), 200, 0, false, true, nil)
	if err != nil || !approved {
		t.Fatalf("without a cutoff the 2160p movie is an upgrade: %v", err)
	}
	approved, reason, err := db.checkMovieScoredWith(&upgrade, upgrade.Quality.Score(), 200, old.Quality.Score(), false, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, !approved, "cutoff reached: expected a refusal, got "+reason)
}

// A legacy or renamed copy may have lost its source and group tags. A
// same-size re-release must not look like an upgrade just because the
// candidate still carries them; a real resolution/codec gain still must.
func TestUnknownArchivedSourceAndGroupAreNotCredited(t *testing.T) {
	existing := models.Quality{Resolution: "1080p", Codec: "h264", Audio: "eac3", AudioChannels: "5.1", Source: "unknown", Group: "unknown"}
	candidate := existing
	candidate.Source = "webdl"
	candidate.Group = "MeM.GP"
	if got := upgradeReasonUntil(&candidate, &existing, 1360, 1150, 200, 0, nil); got != "" {
		t.Fatalf("a richer name on the same quality must not upgrade, got %q", got)
	}
	// Even when another field differs, the unknown source is not credited as a
	// 200-point gain: a 5.1 -> 7.1 step is real but below the minimum.
	channels := candidate
	channels.AudioChannels = "7.1"
	if got := upgradeReasonUntil(&channels, &existing, 1370, 1150, 200, 0, nil); got != "" {
		t.Fatalf("an unknown source must not be credited as a technical gain, got %q", got)
	}
	// A known source still counts: WEB-DL over an archived HDTV is a real
	// source upgrade.
	hdtv := existing
	hdtv.Source = "hdtv"
	if got := upgradeReasonUntil(&candidate, &hdtv, 1360, 1050, 200, 0, nil); got != "source" {
		t.Fatalf("WEB-DL over HDTV must remain a source upgrade, got %q", got)
	}
	// And a real resolution gain is untouched.
	better := candidate
	better.Resolution = "2160p"
	if got := upgradeReasonUntil(&better, &existing, 2560, 1150, 200, 0, nil); got != "resolution" {
		t.Fatalf("a resolution gain must still upgrade, got %q", got)
	}
}

// The real case: a library file renamed with the standard format keeps only
// resolution, codec and audio, and its name is also the stored title. A release
// of the same episode, same size and same streams, but with the full name
// (WEB-DL, channels, group), must not look like a score upgrade.
func TestRenamedLegacyCopyIsNotReDownloaded(t *testing.T) {
	oldName := "Only Murders in the Building - S05E09 - LESTR - [1080p][h264][EAC3][IT+EN]"
	newName := "Only.Murders.in.the.Building.S05E09.LESTR.ITA.ENG.1080p.DSNP.WEB-DL.DDP5.1.H.264-MeM.GP.mkv"
	oldQuality := ParseQuality(oldName)
	MediaInfo{VideoCodec: "h264", AudioCodec: "eac3", AudioChannels: 6,
		Width: 1920, Height: 1080}.ApplyToQuality(&oldQuality)
	newQuality := ParseQuality(newName)
	if got := upgradeReasonUntil(&newQuality, &oldQuality, 1361, 1151, 200, 0, nil); got != "" {
		t.Fatalf("the same file with a richer name must not be re-downloaded, got %q", got)
	}
}

func TestScoreUpgradeNeedsATechnicalGain(t *testing.T) {
	existing := models.Quality{Resolution: "1080p", Source: "webdl", Codec: "h264", Group: "nogrp"}
	same := existing
	same.Group = "mircrew"
	settings := map[string]string{"score_group_mircrew": "30"}
	// Same quality: a bigger file, subtitles and a healthy swarm can push the
	// release score well past the minimum difference, but that is no upgrade.
	if got := upgradeReasonUntil(&same, &existing, 1580, 1280, 200, 0, settings); got != "" {
		t.Fatalf("same quality with only size/health bonuses must not upgrade, got %q", got)
	}
	// A real codec + audio gain of at least the minimum difference still is.
	better := existing
	better.Codec = "h265"
	better.Audio = "truehd"
	if got := upgradeReasonUntil(&better, &existing, 1650, 1280, 200, 0, settings); got != "score" {
		t.Fatalf("technical gain >= min diff must upgrade, got %q", got)
	}
	// A gain below the minimum difference does not, whatever the total says.
	small := existing
	small.Codec = "h265"
	if got := upgradeReasonUntil(&small, &existing, 1700, 1280, 200, 0, settings); got != "" {
		t.Fatalf("technical gain below min diff must not upgrade, got %q", got)
	}
}

func TestSeriesSameQualityIsNotAnUpgradeBecauseOfSize(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	var seriesID int64
	if err := db.db.QueryRow("SELECT id FROM series WHERE name='Show'").Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,quality_score,downloaded_at,archive_path) VALUES (?1,1,1,'Show.S01E01.1080p.WEB-DL.H264-NOGRP',1250,'2024-01-01T00:00:00+00:00','/nas/Show.S01E01.1080p.WEB-DL.H264-NOGRP.mkv')", seriesID); err != nil {
		t.Fatal(err)
	}
	candidate := testRelease()
	candidate.Title = "Show.S01E01.1080p.WEB-DL.H264-OTHER"
	candidate.Series = stringPtr("Show")
	candidate.Season = int64Ptr(1)
	candidate.Episode = int64Ptr(1)
	candidate.IsPack = false
	candidate.Quality = ParseQuality(candidate.Title)
	// 1250 of quality + 100 of size + 150 of swarm health.
	score := int64(1500)
	context := &models.ApprovalContext{Live: &models.LiveDownloads{}, DryRun: true, ScoreSettings: map[string]string{}}
	approved, reason, err := db.CheckSeriesScored(&candidate, score, 200, context)
	if err != nil {
		t.Fatal(err)
	}
	if approved {
		t.Fatalf("a same-quality release must not replace the archived episode (reason %q)", reason)
	}
}

func TestSourceRuleFollowsTheConfiguredSourceWeights(t *testing.T) {
	webrip := models.Quality{Resolution: "1080p", Source: "webrip", Codec: "h265", Audio: "aac"}
	webdl := models.Quality{Resolution: "1080p", Source: "webdl", Codec: "h264", Audio: "ddp"}
	// Default weights: WEB-DL (200) is above WEBRip (150), the rule applies.
	if got := upgradeReasonUntil(&webdl, &webrip, 1350, 1266, 200, 0, nil); got != "source" {
		t.Fatalf("default weights: got %q, want source", got)
	}
	// The same weight for both: same quality, no upgrade.
	same := map[string]string{"score_source_webrip": "200"}
	if got := upgradeReasonUntil(&webdl, &webrip, 1350, 1266, 200, 0, same); got != "" {
		t.Fatalf("equal source weights: got %q, want no upgrade", got)
	}
	// Other gains still count: an HDR WEB-DL is an HDR upgrade.
	hdr := webdl
	hdr.HDR = "HDR10"
	if got := upgradeReasonUntil(&hdr, &webrip, 1450, 1266, 200, 0, same); got != "hdr" {
		t.Fatalf("HDR WEB-DL over WEBRip with equal sources: got %q, want hdr", got)
	}
}
