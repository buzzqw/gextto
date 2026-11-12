package gextto

import (
	"context"
	"testing"

	"github.com/buzzqw/gextto/internal/amule/amuletest"
)

// TestGapPassAsksED2KOncePerSeasonAndFillsItsGaps is the whole path the manual
// missing search used to have to do by hand: three gaps of the same season, one
// eD2k query for the season, three downloads started in the same cycle.
func TestGapPassAsksED2KOncePerSeasonAndFillsItsGaps(t *testing.T) {
	core, coreCfg := ed2kTestCore(t, map[string]string{"ed2k_fallback": "true"})
	core.SetResults([]amuletest.Result{
		{Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "Rare Show S02E02 1080p ITA.mkv", Size: 900000000, Sources: 4},
		{Hash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Name: "Rare Show S02E03 1080p ITA.mkv", Size: 905123456, Sources: 4},
		{Hash: "cccccccccccccccccccccccccccccccc", Name: "Rare Show S02E04 1080p ITA.mkv", Size: 911987654, Sources: 4},
	}, "")

	state := newTestAppState(t)
	state.setActiveEngine(fakeAddEngine{})

	if _, err := state.db.db.Exec("INSERT INTO series(id,name) VALUES (1,'Rare Show')"); err != nil {
		t.Fatal(err)
	}
	// Episode 1 is in the library, 2 to 4 are missing: the season has four.
	if _, err := state.db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,archive_path,downloaded_at) VALUES (1,2,1,'Rare Show S02E01','/media/ep1.mkv',datetime('now'))"); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO series_metadata(series_name,season,episode_count,updated_at) VALUES ('Rare Show',2,4,datetime('now'))"); err != nil {
		t.Fatal(err)
	}

	cfg := *state.cfg
	// The aMule core of the test lives in coreCfg: the cycle uses the
	// configuration it is given, so the settings must be there.
	if cfg.Settings == nil {
		cfg.Settings = map[string]string{}
	}
	for key, value := range coreCfg.Settings {
		cfg.Settings[key] = value
	}
	cfg.Series = []SeriesConfig{{Name: "Rare Show", Seasons: "2", Enabled: true, Quality: "720p-1080p", Language: "ita"}}
	cfg.FeedURLs = nil
	cfg.Indexers = nil
	cfg.WebsearchEngines = nil
	cfg.DryRun = false

	domain := "series"
	stats, err := RunCycleDomain(
		context.Background(),
		&cfg,
		state.engine,
		state.db,
		state.archive,
		state.comics,
		state.notifier,
		state.activeEngine(),
		&domain,
	)
	if err != nil {
		t.Fatalf("RunCycleDomain: %v", err)
	}
	if stats.DownloadsStarted != 3 {
		t.Fatalf("DownloadsStarted = %d, want 3 (S02E02, S02E03, S02E04): %+v", stats.DownloadsStarted, stats.ErrorDetails)
	}
	seasonQueries := 0
	for _, query := range core.Searches() {
		if query == "Rare Show S02" {
			seasonQueries++
		}
	}
	if seasonQueries != 1 {
		t.Fatalf("season searches = %d (%v), want one for the three episodes", seasonQueries, core.Searches())
	}
}
