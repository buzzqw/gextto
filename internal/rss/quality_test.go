package rss

import "testing"

func TestQualityRank(t *testing.T) {
	cases := []struct {
		title string
		label string
		rank  int
	}{
		{"Show.S01E01.2160p.WEB", "2160p", 6},
		{"Show 4K HDR", "2160p", 6},
		{"Show S01E01 1080p", "1080p", 5},
		{"Show S01E01 720p", "720p", 4},
		{"Show S01E01 576p", "576p", 3},
		{"Show S01E01 480p", "480p", 2},
		{"Show S01E01 360p", "360p", 1},
		{"Show S01E01", "", 0},
		{"x1080p", "", 0},
	}
	for _, tc := range cases {
		label, rank := QualityRank(tc.title)
		if label != tc.label || rank != tc.rank {
			t.Errorf("QualityRank(%q) = %q/%d, want %q/%d", tc.title, label, rank, tc.label, tc.rank)
		}
	}
}

func TestQualityRankOf(t *testing.T) {
	for label, want := range map[string]int{
		"720p": 4, "1080p": 5, "2160p": 6, "720p+": 4, "1080p+": 5, "2160p+": 6,
		"720p-1080p": 4, "any": 0, "": 0,
	} {
		if got := QualityRankOf(label); got != want {
			t.Errorf("QualityRankOf(%q) = %d, want %d", label, got, want)
		}
	}
}
