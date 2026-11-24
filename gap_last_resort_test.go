package gextto

import (
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

func TestSeriesLastResortAcceptsOnlyAResolutionOutsideTheProfile(t *testing.T) {
	cfg := DefaultConfig()
	series := SeriesConfig{Name: "FBI: International", Seasons: "1+", Quality: "720p-1080p", Language: "ita", Exclude: "sample"}
	italianUnknown := models.Quality{Language: "ita", Languages: []string{"ita"}}
	italianSD := models.Quality{Resolution: "480p", Language: "ita", Languages: []string{"ita"}}
	italianHD := models.Quality{Resolution: "720p", Language: "ita", Languages: []string{"ita"}}
	english := models.Quality{Language: "eng", Languages: []string{"eng"}}

	if cfg.SeriesReleaseAllowed(&series, &italianUnknown, "FBI International 3x01") {
		t.Fatal("the profile itself must still reject an unknown resolution")
	}
	if !cfg.SeriesLastResortAllowed(&series, &italianUnknown, "FBI International 3x01") {
		t.Fatal("an Italian copy with an unknown resolution is a last resort")
	}
	if !cfg.SeriesLastResortAllowed(&series, &italianSD, "FBI International 3x01 480p") {
		t.Fatal("an Italian copy below the profile is a last resort")
	}
	if cfg.SeriesLastResortAllowed(&series, &italianHD, "FBI International 3x01 720p") {
		t.Fatal("a copy within the profile is a normal candidate, not a last resort")
	}
	if cfg.SeriesLastResortAllowed(&series, &english, "FBI International 3x01") {
		t.Fatal("the language still applies to a last resort")
	}
	if cfg.SeriesLastResortAllowed(&series, &italianUnknown, "FBI International 3x01 sample") {
		t.Fatal("the exclusions still apply to a last resort")
	}
	cfg.Settings["gap_last_resort"] = "false"
	if cfg.SeriesLastResortAllowed(&series, &italianUnknown, "FBI International 3x01") {
		t.Fatal("the switch turns the last resort off")
	}
}

// The last resort bypasses only the resolution filter, now expressed as the
// explicit accepted list. Language, subtitles, audio and exclusions still hold.
func TestLastResortUsesTheExplicitAcceptedQualities(t *testing.T) {
	cfg := DefaultConfig()
	series := SeriesConfig{Name: "Show", Seasons: "1+", Quality: "720p+",
		Qualities: []string{"720p", "1080p", "2160p"}, Language: "ita", Enabled: true}
	italianSD := models.Quality{Resolution: "480p", Language: "ita", Languages: []string{"ita"}}
	italianHD := models.Quality{Resolution: "720p", Language: "ita", Languages: []string{"ita"}}
	if cfg.SeriesReleaseAllowed(&series, &italianSD, "Show S01E01 480p") ||
		!cfg.SeriesLastResortAllowed(&series, &italianSD, "Show S01E01 480p") {
		t.Fatal("a 480p copy outside the accepted list must be a last resort")
	}
	if !cfg.SeriesReleaseAllowed(&series, &italianHD, "Show S01E01 720p") ||
		cfg.SeriesLastResortAllowed(&series, &italianHD, "Show S01E01 720p") {
		t.Fatal("a copy inside the accepted list stays a normal candidate")
	}
}

// With no copy inside the profile, the only candidate is taken even though
// its resolution is far below the accepted qualities: the gate sees a missing
// episode, so there is no archived copy to compare it with.
func TestLastResortForMissingEpisodeIsApproved(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	series := SeriesConfig{Name: "Show", Seasons: "1+", Quality: "720p+",
		Qualities: []string{"720p", "1080p", "2160p"}, Language: "ita", Enabled: true}
	cfg.Series = []SeriesConfig{series}
	release := testRelease()
	release.Kind = "series"
	release.Series, release.Season, release.Episode = stringPtr("Show"), int64Ptr(1), int64Ptr(1)
	release.Quality = models.Quality{Resolution: "480p", Source: "hdtv", Language: "ita", Languages: []string{"ita"}}
	release.LastResort = true

	if !releaseAllowedByTitleProfile(&cfg, &release) {
		t.Fatal("a last resort outside the accepted qualities must pass the title filter")
	}
	context := &models.ApprovalContext{
		Archive: &models.ArchiveQualityIndex{}, Live: &models.LiveDownloads{}, DryRun: true,
	}
	approved, reason, err := db.CheckSeriesScored(&release, cfg.ReleaseScore(&release), cfg.UpgradeMinScoreDiff, context)
	if err != nil || !approved {
		t.Fatalf("last resort -> approved=%v reason=%q err=%v", approved, reason, err)
	}
}

func TestEd2kRowsCarryTheLastResortBadge(t *testing.T) {
	db := newTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	for _, row := range []struct{ hash, reason string }{
		{"0123456789abcdef0123456789abcdef", "last_resort"},
		{"fedcba9876543210fedcba9876543210", "gap_fill"},
	} {
		if _, err := db.db.Exec("INSERT INTO torrent_meta(hash,reason,updated_at) VALUES (?1,?2,?3)", row.hash, row.reason, now); err != nil {
			t.Fatal(err)
		}
	}
	rows := []uiEd2kRow{
		{Hash: "0123456789ABCDEF0123456789ABCDEF"},
		{Hash: "fedcba9876543210fedcba9876543210"},
		{Hash: "00000000000000000000000000000000"},
	}
	uiEd2kRowsLastResort(db, rows)
	if !rows[0].LastResort || rows[1].LastResort || rows[2].LastResort {
		t.Fatalf("last resort flags = %v %v %v, want true false false", rows[0].LastResort, rows[1].LastResort, rows[2].LastResort)
	}
}

func TestDuplicateCopiesKeepTheLastResortMark(t *testing.T) {
	title := "FBI.International.3x01.June.ITA.DLMux.x264-UBi.mkv"
	link := "ed2k://|file|FBI.International.3x01.June.ITA.DLMux.x264-UBi.mkv|462422016|0123456789ABCDEF0123456789ABCDEF|/"
	fromTitleSearch := models.Release{Title: title, Kind: "series", Magnet: link, SizeBytes: 462422016, Source: "indexarr:DDUnlimited"}
	fromGapPass := fromTitleSearch
	fromGapPass.LastResort = true
	for _, order := range [][]models.Release{{fromTitleSearch, fromGapPass}, {fromGapPass, fromTitleSearch}} {
		merged := mergeDuplicateReleases(order)
		if len(merged) != 1 {
			t.Fatalf("merged = %d releases, want 1", len(merged))
		}
		if !merged[0].LastResort {
			t.Fatalf("merged copy lost the last resort mark: %+v", merged[0])
		}
	}
}

// TestManualSearchListsTheLastResortCopy: the cycle takes a copy outside the
// profile when the gap has nothing better; the manual search filtered it out,
// so the operator read "no results" for something Gextto was about to download.
func TestManualSearchListsTheLastResortCopy(t *testing.T) {
	cfg := DefaultConfig()
	series := SeriesConfig{Name: "FBI: International", Seasons: "1+", Quality: "720p-1080p", Language: "ita"}
	sd := models.Release{
		Title:     "FBI International 3x01 480p ITA",
		Kind:      "series",
		Magnet:    "magnet:?xt=urn:btih:" + strings.Repeat("b", 40),
		Quality:   models.Quality{Resolution: "480p", Language: "ita", Languages: []string{"ita"}},
		SizeBytes: 700000000,
	}
	hd := models.Release{
		Title:   "FBI International 3x01 1080p ITA",
		Kind:    "series",
		Magnet:  "magnet:?xt=urn:btih:" + strings.Repeat("c", 40),
		Quality: models.Quality{Resolution: "1080p", Language: "ita", Languages: []string{"ita"}},
	}
	sources := []episodeSource{
		{Release: sd, Origin: episodeOriginIndexer},
		{Release: hd, Origin: episodeOriginIndexer},
	}
	if got := lastResortEpisodeSources(sources, &cfg, &series); len(got) != 1 || !got[0].Release.LastResort {
		t.Fatalf("last resorts = %+v, want the 480p copy marked", got)
	}
	// A copy within the profile is never labelled: the table would say last
	// resort for a normal download.
	cfg.Settings["gap_last_resort"] = "false"
	if got := lastResortEpisodeSources([]episodeSource{{Release: sd, Origin: episodeOriginIndexer}}, &cfg, &series); len(got) != 0 {
		t.Fatalf("the switch must turn the listing off: %+v", got)
	}
}
