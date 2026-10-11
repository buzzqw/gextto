package rss

import (
	"testing"
	"time"
)

func TestEpisodeFilterMatch(t *testing.T) {
	cases := []struct {
		filter string
		title  string
		want   bool
	}{
		{"", "Anything at all", true}, // empty filter matches everything

		{"1x01", "Show S01E01 1080p", true},
		{"1x01", "Show 1x01", true},
		{"1x01", "Show S01E02 1080p", false},
		{"1x01", "Show without an episode marker", false},

		{"1x01-05", "Show S01E03", true},
		{"1x01-05", "Show S01E05", true},
		{"1x01-05", "Show S01E06", false},

		{"1x01-", "Show S01E01", true},
		{"1x01-", "Show S01E99", true},
		{"1x01-", "Show S02E01", false},

		{"1x??", "Show S01E07", true},
		{"1x??", "Show S02E07", false},

		{"1x01, 1x03", "Show S01E01", true},
		{"1x01, 1x03", "Show S01E03", true},
		{"1x01, 1x03", "Show S01E02", false},

		// Cross-season range: season 1 from ep 5 on, season 2 up to ep 2.
		{"1x05-2x02", "Show S01E05", true},
		{"1x05-2x02", "Show S01E09", true},
		{"1x05-2x02", "Show S01E04", false},
		{"1x05-2x02", "Show S02E02", true},
		{"1x05-2x02", "Show S02E03", false},

		// SxxExx spelling is accepted.
		{"S02E10", "Show S02E10", true},
	}

	for _, tc := range cases {
		filter, err := ParseEpisodeFilter(tc.filter)
		if err != nil {
			t.Fatalf("ParseEpisodeFilter(%q): %v", tc.filter, err)
		}
		if got := filter.Match(tc.title); got != tc.want {
			t.Errorf("filter %q vs %q = %v, want %v", tc.filter, tc.title, got, tc.want)
		}
	}
}

func TestEpisodeFilterRejectsInvalid(t *testing.T) {
	for _, filter := range []string{"abc", "1x", "1x0", "1x05-01", "1x0x2", "x01"} {
		if _, err := ParseEpisodeFilter(filter); err == nil {
			t.Errorf("ParseEpisodeFilter(%q) must fail", filter)
		}
	}
}

func TestRuleCriteriaEpisodeFilter(t *testing.T) {
	criteria := Criteria{EpisodeFilter: "1x01-03"}
	now := time.Now()
	if !criteria.Match(Item{Title: "Show S01E02"}, now) {
		t.Fatal("S01E02 must match the 1x01-03 filter")
	}
	if criteria.Match(Item{Title: "Show S01E04"}, now) {
		t.Fatal("S01E04 must not match the 1x01-03 filter")
	}
	// A malformed filter never matches (a typo must not download the season).
	broken := Criteria{EpisodeFilter: "not-a-filter"}
	if broken.Match(Item{Title: "Show S01E01"}, now) {
		t.Fatal("a malformed episode filter must not match")
	}
}

func TestEvaluateSkipsDisabledRules(t *testing.T) {
	disabled := false
	rules := []Rule{
		{Name: "off", Enabled: &disabled, Fail: true, Match: Criteria{Include: []string{"cam"}}},
		{Name: "on", Match: Criteria{Include: []string{"1080p"}}, Action: Action{Category: "tv"}},
	}
	now := time.Now()
	if _, _, matched := Evaluate(rules, "feed", Item{Title: "Show CAM"}, now); matched {
		t.Fatal("a disabled rule must be skipped, not decide the item")
	}
	if rule, pass, matched := Evaluate(rules, "feed", Item{Title: "Show 1080p"}, now); !matched || !pass || rule.Name != "on" {
		t.Fatalf("the enabled rule must match: rule=%q matched=%v pass=%v", rule.Name, matched, pass)
	}
}
