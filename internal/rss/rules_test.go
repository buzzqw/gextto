package rss

import (
	"testing"
	"time"
)

func TestFindEpisode(t *testing.T) {
	cases := []struct {
		title           string
		series          string
		season, episode int
		ok              bool
	}{
		{"Show.Name.S02E07.1080p.WEB", "show name", 2, 7, true},
		{"Show Name 2x07 720p", "show name", 2, 7, true},
		{"Movie.2024.1080p", "", 0, 0, false},
		{"Another-Show-S01E100", "another show", 1, 100, true},
	}
	for _, c := range cases {
		series, season, episode, ok := FindEpisode(c.title)
		if ok != c.ok || series != c.series || season != c.season || episode != c.episode {
			t.Errorf("FindEpisode(%q) = %q,%d,%d,%v want %q,%d,%d,%v",
				c.title, series, season, episode, ok, c.series, c.season, c.episode, c.ok)
		}
	}
}

func TestCriteriaMatch(t *testing.T) {
	now := time.Now()
	item := Item{Title: "Show S02E07 1080p", Size: 2 << 30, Seeders: 30, Leechers: 5, Published: now.Add(-2 * time.Hour)}

	if !(Criteria{Regex: `1080p`, MinSeeders: 10}).Match(item, now) {
		t.Fatal("matching criteria rejected")
	}
	if (Criteria{Regex: `1080p`, MinSeeders: 50}).Match(item, now) {
		t.Fatal("too few seeders accepted")
	}
	if (Criteria{MaxSize: 1 << 30}).Match(item, now) {
		t.Fatal("oversized item accepted")
	}
	old := Item{Title: "Old Show S01E01", Published: now.Add(-10 * 24 * time.Hour)}
	if (Criteria{MaxAgeDays: 3}).Match(old, now) {
		t.Fatal("old item accepted")
	}
	if (Criteria{Exclude: []string{"cam"}}).Match(Item{Title: "Show 1080p CAM"}, now) {
		t.Fatal("excluded item accepted")
	}
	if !(Criteria{RequireEpisode: true}).Match(item, now) {
		t.Fatal("episode item rejected")
	}
	if (Criteria{RequireEpisode: true}).Match(Item{Title: "Movie 2024"}, now) {
		t.Fatal("non-episode item accepted with RequireEpisode")
	}
	if (Criteria{Regex: "["}).Match(item, now) {
		t.Fatal("a broken regex must not match")
	}
}

func TestEvaluateOrderedRules(t *testing.T) {
	now := time.Now()
	rules := []Rule{
		{Name: "cam fail", Fail: true, Match: Criteria{Regex: `(?i)cam`}},
		{Name: "1080", Match: Criteria{Include: []string{"1080p"}}, Action: Action{Category: "movies"}},
		{Name: "anything", Match: Criteria{}, Action: Action{Category: "other"}},
	}

	if _, pass, matched := Evaluate(rules, "feedA", Item{Title: "Show 1080p CAM"}, now); !matched || pass {
		t.Fatal("FAIL rule must skip the CAM item")
	}
	if r, pass, matched := Evaluate(rules, "feedA", Item{Title: "Show 1080p"}, now); !matched || !pass || r.Action.Category != "movies" {
		t.Fatalf("1080 rule: %+v pass=%v matched=%v", r, pass, matched)
	}
	if r, _, _ := Evaluate(rules, "feedA", Item{Title: "Show 720p"}, now); r.Action.Category != "other" {
		t.Fatalf("catch-all rule: %+v", r)
	}

	// Feed scoping: a rule bound to another feed does not apply.
	scoped := []Rule{{Name: "onlyB", Feeds: []string{"feedB"}, Match: Criteria{}, Action: Action{Category: "b"}}}
	if _, _, matched := Evaluate(scoped, "feedA", Item{Title: "x"}, now); matched {
		t.Fatal("a rule for feedB must not match feedA")
	}
	if _, pass, matched := Evaluate(scoped, "feedB", Item{Title: "x"}, now); !matched || !pass {
		t.Fatal("the rule must match its own feed")
	}
}
