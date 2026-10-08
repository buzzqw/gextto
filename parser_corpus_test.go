package gextto

import (
	"reflect"
	"testing"
)

// TestParseReleaseCorpus is our own corpus of real-world release names (no
// fixtures copied from other projects). It locks the episode/season parsing
// edge cases so a future change cannot silently regress them.
func TestParseReleaseCorpus(t *testing.T) {
	type want struct {
		series  string
		season  int64
		episode int64
		pack    bool
	}
	seriesCases := []struct {
		title string
		want  want
	}{
		{"Show.Name.S01E01.1080p.WEB-DL", want{"Show Name", 1, 1, false}},
		{"Show.Name.S01.E01.1080p", want{"Show Name", 1, 1, false}},
		{"Show Name S01 E01", want{"Show Name", 1, 1, false}},
		{"Show.Name.S1E1.720p", want{"Show Name", 1, 1, false}},
		{"Show.Name.S01E001.720p", want{"Show Name", 1, 1, false}},
		{"Show.Name.S01E01E02.1080p", want{"Show Name", 1, 1, true}},
		{"Show.Name.S01E01-E02.1080p", want{"Show Name", 1, 1, true}},
		{"Show.Name.S01E01-02.1080p", want{"Show Name", 1, 1, true}},
		{"Show.Name.1x01.1080p", want{"Show Name", 1, 1, false}},
		{"Show.Name.1x01-1x02.1080p", want{"Show Name", 1, 1, true}},
		{"Show.Name.Season.1.Episode.3.1080p", want{"Show Name", 1, 3, false}},
		{"Show.Name.Season 2 Episode 10 720p", want{"Show Name", 2, 10, false}},
		{"Show.Name.Season.1.Complete.1080p", want{"Show Name", 1, 0, true}},
		{"Show.Name.Stagione.2.Puntata.10", want{"Show Name", 2, 10, false}},
		{"Show.Name.Stagione.3.COMPLETA.ITA", want{"Show Name", 3, 0, true}},
		{"Show.Name.S00E05.1080p", want{"Show Name", 0, 5, false}},
		{"Show.Name.S01.1080p", want{"Show Name", 1, 0, true}},
		{"Show.Name.S2024E05.1080p", want{"Show Name", 2024, 5, false}},
		{"Show.Name.2160p.S02E10.HDR.DV.ATMOS", want{"Show Name", 2, 10, false}},
		{"Show.Name.S01E01.REPACK.1080p", want{"Show Name", 1, 1, false}},
		{"Show.Name.S01E01.MULTI.1080p", want{"Show Name", 1, 1, false}},
	}
	for _, tc := range seriesCases {
		r := ParseRelease(tc.title, parserTestMagnet, "test")
		if r == nil || r.Kind != "series" || r.Series == nil || r.Season == nil || r.Episode == nil {
			t.Errorf("%q: not parsed as a series: %+v", tc.title, r)
			continue
		}
		if *r.Series != tc.want.series || *r.Season != tc.want.season || *r.Episode != tc.want.episode {
			t.Errorf("%q: got %q S%dE%d, want %q S%dE%d",
				tc.title, *r.Series, *r.Season, *r.Episode, tc.want.series, tc.want.season, tc.want.episode)
		}
		if r.IsPack != tc.want.pack {
			t.Errorf("%q: pack=%v, want %v", tc.title, r.IsPack, tc.want.pack)
		}
	}
}

// TestParseReleaseEpisodeRanges checks the episode expansion of multi-episode
// and range releases.
func TestParseReleaseEpisodeRanges(t *testing.T) {
	cases := map[string][]int64{
		"Show.Name.S01E01E02.1080p":  {1, 2},
		"Show.Name.S01E01-E03.1080p": {1, 2, 3},
		"Show.Name.S01E05-06.1080p":  {5, 6},
		"Show.Name.1x01-1x03.720p":   {1, 2, 3},
		"Show.Name.S02E10.1080p":     {10},
		"Show.Name.S01.1080p":        {0},
	}
	for title, want := range cases {
		r := ParseRelease(title, parserTestMagnet, "test")
		if r == nil {
			t.Errorf("%q: nil release", title)
			continue
		}
		if !reflect.DeepEqual(r.EpisodeRange, want) {
			t.Errorf("%q: range=%v, want %v", title, r.EpisodeRange, want)
		}
	}
}

// TestParseReleaseAnimeAbsolute covers anime-style absolute episode numbers.
func TestParseReleaseAnimeAbsolute(t *testing.T) {
	cases := map[string]int64{
		"[SubsPlease] One Piece - 1071 (1080p)": 1071,
		"One Piece Ep 1071 SUB ITA":             1071,
		"One.Piece.1071.SUB.ITA":                1071,
	}
	for title, want := range cases {
		r := ParseRelease(title, parserTestMagnet, "test")
		if r == nil || r.AbsoluteEpisode == nil || *r.AbsoluteEpisode != want {
			t.Errorf("%q: absolute episode = %v, want %d", title, r.AbsoluteEpisode, want)
		}
	}
}
