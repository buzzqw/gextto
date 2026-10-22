package gextto

import (
	"testing"
)

// TestGapsGroupedBySeriesAndSeason: the Missing tab shows one block per series
// (alphabetical) with its seasons in order and the episodes as codes, not one
// flat row per episode; the filter keeps the matching series and opens them.
func TestGapsGroupedBySeriesAndSeason(t *testing.T) {
	gaps := []libraryGap{
		{Series: "Walker", Season: 4, Episode: 3, AirDate: "2024-03-01"},
		{Series: "FBI", Season: 6, Episode: 2},
		{Series: "Walker", Season: 3, Episode: 9, AirDate: "2023-05-01"},
		{Series: "FBI", Season: 6, Episode: 1},
		{Series: "Walker", Season: 4, Episode: 1, AirDate: "2024-02-15"},
		{Series: "FBI: International", Season: 2, Episode: 5},
		{Series: "Dark Matter", Season: 1, Episode: 1},
		{Series: "New Tricks", Season: 1, Episode: 1},
	}
	view := v2GapsViewFrom(gaps, "")
	if view.Total != 8 || view.SeriesCount != 5 || view.Open {
		t.Fatalf("total=%d series=%d open=%v", view.Total, view.SeriesCount, view.Open)
	}
	if view.Series[0].Name != "Dark Matter" || view.Series[1].Name != "FBI" || view.Series[4].Name != "Walker" {
		t.Fatalf("order = %v", []string{view.Series[0].Name, view.Series[1].Name, view.Series[4].Name})
	}
	walker := view.Series[4]
	if walker.Count != 3 || walker.Latest != "2024-03-01" || len(walker.Seasons) != 2 {
		t.Fatalf("walker = %+v", walker)
	}
	if walker.Seasons[0].Season != 3 || walker.Seasons[1].Season != 4 ||
		walker.Seasons[1].Episodes[0].Code != "S04E01" || walker.Seasons[1].Episodes[1].Code != "S04E03" {
		t.Fatalf("walker seasons = %+v", walker.Seasons)
	}
	filtered := v2GapsViewFrom(gaps, "fbi")
	if filtered.SeriesCount != 2 || filtered.Total != 3 || !filtered.Open {
		t.Fatalf("filtered = %+v", filtered)
	}
}
