package gextto

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func TestSeasonStrategyAllowsOnlyTheChosenReleaseShape(t *testing.T) {
	single := &models.Release{IsPack: false, EpisodeRange: []int64{1}}
	partial := &models.Release{IsPack: true, EpisodeRange: []int64{1, 2}}
	complete := &models.Release{IsPack: true, EpisodeRange: []int64{0}}
	cases := []struct {
		strategy string
		release  *models.Release
		want     bool
	}{
		{seasonStrategyAuto, single, true},
		{seasonStrategyAuto, partial, true},
		{seasonStrategySingles, single, true},
		{seasonStrategySingles, partial, false},
		{seasonStrategySingles, complete, false},
		{seasonStrategyWaitForPack, single, false},
		{seasonStrategyWaitForPack, partial, false},
		{seasonStrategyWaitForPack, complete, true},
	}
	for _, tc := range cases {
		series := &SeriesConfig{SeasonStrategy: tc.strategy}
		if got := seasonStrategyAllowsRelease(series, tc.release); got != tc.want {
			t.Fatalf("%s with %+v = %v, want %v", tc.strategy, tc.release, got, tc.want)
		}
	}
}

func TestSeasonStrategyPersistsAndRejectsUnknownValues(t *testing.T) {
	state := newTestAppState(t)
	Router(state)
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{{Name: "Show", Seasons: "1+", Enabled: true, SeasonStrategy: seasonStrategyWaitForPack}}, nil); err != nil {
		t.Fatal(err)
	}
	if got := latestConfig(state).Series[0].SeasonStrategy; got != seasonStrategyWaitForPack {
		t.Fatalf("stored strategy = %q", got)
	}
	body, _ := json.Marshal(map[string]string{"season_strategy": "anything"})
	if _, code := v2InternalJSON(state, http.MethodPost, "/api/series/Show", nil, body); code != http.StatusBadRequest {
		t.Fatalf("invalid strategy -> %d, want 400", code)
	}
}

func TestV2SeasonStrategyFormSaves(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{{Name: "Show", Seasons: "1+", Enabled: true}}, nil); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	code, _ := v2Request(t, server, http.MethodPost, "/series/save", url.Values{
		"name": {"Show"}, "original_name": {"Show"}, "seasons": {"1+"}, "season_strategy": {"singles"},
	})
	if code != http.StatusNoContent {
		t.Fatalf("series save -> %d", code)
	}
	if got := latestConfig(state).Series[0].SeasonStrategy; got != seasonStrategySingles {
		t.Fatalf("strategy = %q, want singles", got)
	}
}
