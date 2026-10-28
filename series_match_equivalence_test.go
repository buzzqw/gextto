package gextto

import "testing"

// referenceFindSeriesMatch is the original implementation of FindSeriesMatch,
// kept only for the test: it scans every configured name and alias for every
// call. The indexed version must answer identically, so the test compares them
// on names that exercise every branch of the matcher.
func referenceFindSeriesMatch(c *Config, name string, season *int64) *SeriesConfig {
	normalized := NormalizeSeriesName(name)
	eligible := func(series *SeriesConfig) bool {
		if !series.Enabled {
			return false
		}
		if season != nil {
			if containsInt64(series.IgnoredSeasons, *season) || !seasonAllowed(series.Seasons, *season) {
				return false
			}
		}
		return true
	}
	candidates := func(series *SeriesConfig) []string {
		values := make([]string, 0, len(series.Aliases)+1)
		values = append(values, series.Name)
		values = append(values, series.Aliases...)
		return values
	}
	for index := range c.Series {
		series := &c.Series[index]
		if !eligible(series) {
			continue
		}
		for _, candidate := range candidates(series) {
			if NormalizeSeriesName(candidate) == normalized {
				return series
			}
		}
	}
	for index := range c.Series {
		series := &c.Series[index]
		if !eligible(series) {
			continue
		}
		for _, candidate := range candidates(series) {
			if SeriesNamesMatch(candidate, normalized) {
				return series
			}
		}
	}
	return nil
}

// TestFindSeriesMatchMatchesTheReference locks the indexed lookup to the
// original scan: same series, same order, same handling of aliases, plurals,
// years, stylised titles, numeric titles, seasons and disabled series.
func TestFindSeriesMatchMatchesTheReference(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Series = []SeriesConfig{
		{Name: "Scrubs", Seasons: "1+", Enabled: true},
		{Name: "Scrubs 2026", Seasons: "1+", Enabled: true},
		{Name: "9-1-1", Seasons: "1+", Enabled: true},
		{Name: "Pluribus", Seasons: "1+", Enabled: true},
		{Name: "FBI", Aliases: []string{"F.B.I.", "Federal Bureau"}, Seasons: "1+", Enabled: true},
		{Name: "The Office", Seasons: "1+", Enabled: true},
		{Name: "Doctor Who", Seasons: "1+", Enabled: true},
		{Name: "Only Season One", Seasons: "1", Enabled: true},
		{Name: "Skip Season Two", Seasons: "1+", IgnoredSeasons: []int64{2}, Enabled: true},
		{Name: "Disabled Show", Seasons: "1+", Enabled: false},
		{Name: "Alias Only", Aliases: []string{"AO", "Alias-Only"}, Seasons: "1+", Enabled: true},
		{Name: "Show", Seasons: "1+", Enabled: true},
		{Name: "Shows", Seasons: "1+", Enabled: true},
	}
	// The names are the parsed series names a release carries (release.Series),
	// plus a few full titles: both must go through the two passes unchanged.
	releases := []string{
		"Scrubs", "Scrubs 2026", "Scrubs 2024", "Scrubs.2001",
		"9-1-1", "9 1 1", "911", "9-1-1 Lone Star",
		"PLUR1BUS", "Pluribus", "PLURIBUS",
		"FBI", "F.B.I.", "F B I", "Federal Bureau", "FBI Most Wanted",
		"The Office", "The Office US", "Office", "Doctor Who", "Doctor Who 2005",
		"Only Season One", "Skip Season Two", "Disabled Show",
		"Alias Only", "AO", "Alias-Only",
		"Show", "Shows", "Show 2024", "The Show",
		"Unknown Show", "Some Random Release 1080p", "",
		"Scrubs.S01E01.1080p", "9-1-1 S05E01 1080p",
	}
	seasons := []*int64{nil, int64Ptr(1), int64Ptr(2), int64Ptr(3)}

	matched := 0
	for _, release := range releases {
		for _, season := range seasons {
			want := referenceFindSeriesMatch(&cfg, release, season)
			got := cfg.FindSeriesMatch(release, season)
			wantName, gotName := "", ""
			if want != nil {
				wantName = want.Name
			}
			if got != nil {
				gotName = got.Name
			}
			if wantName != gotName {
				t.Fatalf("FindSeriesMatch(%q, season=%v) = %q, reference says %q", release, seasonValue(season), gotName, wantName)
			}
			if gotName != "" {
				matched++
			}
		}
	}
	// A test that only compares misses would pass on two broken matchers.
	if matched == 0 {
		t.Fatal("no release matched any series: the comparison never exercised a hit")
	}
}

// TestFindSeriesMatchFollowsAnInPlaceEdit checks that adding a series to the
// same Config is seen immediately: the index is keyed by content, not by the
// pointer of the configuration it was built from.
func TestFindSeriesMatchFollowsAnInPlaceEdit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Series = []SeriesConfig{{Name: "First Show", Seasons: "1+", Enabled: true}}
	season := int64(1)
	if series := cfg.FindSeriesMatch("Second Show", &season); series != nil {
		t.Fatalf("matched %q before it was configured", series.Name)
	}
	cfg.Series = append(cfg.Series, SeriesConfig{Name: "Second Show", Seasons: "1+", Enabled: true})
	series := cfg.FindSeriesMatch("Second Show", &season)
	if series == nil || series.Name != "Second Show" {
		t.Fatalf("the series added in place was not matched: %v", series)
	}
	cfg.Series[0].Aliases = append(cfg.Series[0].Aliases, "Alt Title")
	if series := cfg.FindSeriesMatch("Alt Title", &season); series == nil || series.Name != "First Show" {
		t.Fatalf("the alias added in place was not matched: %v", series)
	}
}

func seasonValue(season *int64) any {
	if season == nil {
		return nil
	}
	return *season
}
