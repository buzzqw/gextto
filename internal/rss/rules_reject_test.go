package rss

import (
	"testing"
	"time"
)

// TestRejectReasonKeys checks each criterion reports its own stable key.
func TestRejectReasonKeys(t *testing.T) {
	now := time.Now()
	item := Item{Title: "Show.Name.S02E07.1080p.WEB", Size: 2 << 30, Seeders: 30, Leechers: 5, Published: now.Add(-2 * time.Hour)}
	cases := []struct {
		criteria Criteria
		want     string
	}{
		{Criteria{Include: []string{"2160p"}}, "include"},
		{Criteria{Require: []string{"show", "nope"}}, "require"},
		{Criteria{Exclude: []string{"web"}}, "exclude"},
		{Criteria{Regex: `720p`}, "regex"},
		{Criteria{Regex: `[`}, "regex"},
		{Criteria{NotRegex: `1080p`}, "not_regex"},
		{Criteria{MinSize: 4 << 30}, "min_size"},
		{Criteria{MaxSize: 1 << 30}, "max_size"},
		{Criteria{MinSeeders: 50}, "min_seeders"},
		{Criteria{MaxSeeders: 10}, "max_seeders"},
		{Criteria{MinPeers: 50}, "min_peers"},
		{Criteria{RequireEpisode: true}, ""},
		{Criteria{EpisodeFilter: "1x01-05"}, "episode_filter"},
		{Criteria{Include: []string{"1080p"}, Exclude: []string{"cam"}}, ""},
	}
	for _, c := range cases {
		if got := c.criteria.RejectReason(item, now); got != c.want {
			t.Errorf("RejectReason(%+v) = %q, want %q", c.criteria, got, c.want)
		}
	}
	if got := (Criteria{RequireEpisode: true}).RejectReason(Item{Title: "Movie 2024"}, now); got != "require_episode" {
		t.Errorf("non-episode RejectReason = %q, want require_episode", got)
	}
	old := Item{Title: "Old Show S01E01", Published: now.Add(-10 * 24 * time.Hour)}
	if got := (Criteria{MaxAgeDays: 3}).RejectReason(old, now); got != "max_age_days" {
		t.Errorf("old item RejectReason = %q, want max_age_days", got)
	}
}

// TestRejectReasonAgreesWithMatch is the invariant the preview relies on: an
// empty reason means Match, a reason means no match.
func TestRejectReasonAgreesWithMatch(t *testing.T) {
	now := time.Now()
	criteria := []Criteria{
		{},
		{Include: []string{"1080p", "720p"}, Require: []string{"show"}, Exclude: []string{"cam", "ts"}},
		{Regex: `S\d+E\d+`, NotRegex: `cam`, MinSize: 1 << 30, MaxSize: 8 << 30},
		{MinSeeders: 5, MaxSeeders: 100, MinPeers: 2, MaxAgeDays: 30},
		{RequireEpisode: true, EpisodeFilter: "2x01-10", SmartEpisode: true},
		{Regex: `[`},
	}
	items := []Item{
		{Title: "Show.Name.S02E07.1080p.WEB", Size: 2 << 30, Seeders: 30, Leechers: 5, Published: now.Add(-2 * time.Hour)},
		{Title: "Show.Name.S02E11.1080p.WEB", Size: 2 << 30, Seeders: 1, Leechers: 0, Published: now.Add(-40 * 24 * time.Hour)},
		{Title: "Movie.2024.2160p.BluRay", Size: 20 << 30, Seeders: 200, Leechers: 50, Published: now.Add(-time.Hour)},
		{Title: "Show CAM", Published: now},
		{Title: "Other.S01E01.720p", Seeders: 3, Published: now},
	}
	for _, c := range criteria {
		for _, item := range items {
			matched := c.Match(item, now)
			reason := c.RejectReason(item, now)
			if matched && reason != "" {
				t.Errorf("Match=true but RejectReason=%q for %+v on %q", reason, c, item.Title)
			}
			if !matched && reason == "" {
				t.Errorf("Match=false but empty RejectReason for %+v on %q", c, item.Title)
			}
		}
	}
}
