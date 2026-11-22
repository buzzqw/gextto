package gextto

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestGapSearchOffersOnlyLastResortCopies: when the only copies of the missing
// episodes are outside the preferred profile, the search still returns them,
// marks them as a last resort and lets the picks queue one per episode. Throwing
// them away would leave the episode unfound although a copy is there.
func TestGapSearchOffersOnlyLastResortCopies(t *testing.T) {
	state := newCycleState(t)
	cfg := state.cfg

	series := &SeriesConfig{Name: "Test Show", Quality: "720p-1080p", Language: "ita", Seasons: "1+"}
	if _, err := state.db.db.Exec("INSERT INTO series (name, seasons, quality, language, enabled) VALUES (?,?,?,?,1)",
		series.Name, series.Seasons, series.Quality, series.Language); err != nil {
		t.Fatalf("insert series: %v", err)
	}
	if _, err := state.db.db.Exec("INSERT INTO series_metadata (series_name, season, episode_count, updated_at) VALUES (?,1,2,'2020-01-01')", series.Name); err != nil {
		t.Fatalf("insert metadata: %v", err)
	}
	for _, ep := range []int64{1, 2} {
		title := fmt.Sprintf("Test Show S01E%02d 480p iTALiAN", ep)
		if _, err := state.db.db.Exec(`INSERT INTO series_feed_seen
			(title,name,season,episode,resolution,codec,audio,quality_score,magnet,source,found_at)
			VALUES (?,?,1,?,?,?,?,?,?,?,'2020-01-01')`,
			title, series.Name, ep, "480p", "x264", "ac3", 10,
			"magnet:?xt=urn:btih:"+strings.Repeat(string(rune('a'+ep)), 40), "test"); err != nil {
			t.Fatalf("insert feed row: %v", err)
		}
	}

	outcome := searchMissingEpisodes(context.Background(), state, cfg, series, nil)
	if outcome.TotalMissing != 2 || outcome.Searched != 2 || len(outcome.Results) != 2 {
		t.Fatalf("want 2 episodes searched with 2 results, got %+v", outcome)
	}
	for _, r := range outcome.Results {
		if !r.Release.LastResort {
			t.Fatalf("%q was offered as a normal copy, not as a last resort", r.Release.Title)
		}
	}
	if picks := v2BestGapPicks(outcome.Results); len(picks) != 2 {
		t.Fatalf("want a pick for each episode, got %+v", picks)
	}
}
