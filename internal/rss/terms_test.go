package rss

import (
	"testing"
	"time"
)

func TestHasTermNormalizesSeparatorsAndWordStarts(t *testing.T) {
	cases := []struct {
		title, term string
		want        bool
	}{
		{"The.Office.US.S03E01.1080p", "the office", true},
		{"Show S01E01 SUB-ITA x264", "sub ita", true},
		{"Show.S01E01.SUB.ITA", "sub-ita", true},
		{"Film 2024 ITA ENG", "ita", true},
		{"Film 2024 Italian", "ita", true},
		{"Capitan America 2011 ENG", "ita", false},
		{"Show S01E01 English", "eng", true},
		{"Movie 1080p", "1080", true},
		{"Movie 1080p", "080p", false},
		{"Movie H.264", "h.264", true},
		{"Movie WEB-DL", "web dl", true},
		{"Movie", "  ", false},
		{"Movie [ITA]", "ita", true},
	}
	for _, c := range cases {
		if got := HasTerm(c.title, c.term); got != c.want {
			t.Errorf("HasTerm(%q, %q) = %v, want %v", c.title, c.term, got, c.want)
		}
	}
}

// TestCriteriaRequireIsAnAnd covers the "every term" list next to Include
// (one of): a rule for one series in one quality no longer takes every
// release in that quality.
func TestCriteriaRequireIsAnAnd(t *testing.T) {
	now := time.Now()
	c := Criteria{Require: []string{"the office", "1080p"}}
	if !c.Match(Item{Title: "The.Office.US.S03E01.1080p.BluRay"}, now) {
		t.Fatal("a title with every required term must match")
	}
	if c.Match(Item{Title: "Ted.Lasso.S01E01.1080p.WEB-DL"}, now) {
		t.Fatal("a title missing a required term must not match")
	}
	any := Criteria{Include: []string{"1080p", "2160p"}, Require: []string{"ita"}}
	if !any.Match(Item{Title: "Movie 2160p ITA"}, now) || any.Match(Item{Title: "Movie 2160p ENG"}, now) || any.Match(Item{Title: "Movie 720p ITA"}, now) {
		t.Fatal("Include is one of, Require is all of, together")
	}
	if (Criteria{Exclude: []string{"cam"}}).Match(Item{Title: "Movie.2024.CAM.ITA"}, now) {
		t.Fatal("an excluded term separated by dots must still exclude")
	}
}
