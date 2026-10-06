package gextto

import "testing"

const parserTestMagnet = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"

func TestParseReleaseYearIgnoresNumericTitle(t *testing.T) {
	cases := map[string]int64{
		"Movie.1917.2019.1080p.WEB-DL":           2019,
		"2001.A.Space.Odyssey.1968.1080p.BluRay": 1968,
		"Wonder.Woman.1984.2020.2160p.WEB-DL":    2020,
		"The.Movie.2019.1080p.BluRay.x264-GRP":   2019,
		"1917.2019.REMUX.2160p.UHD.BluRay":       2019,
		"Movie.1080p.BluRay.2015":                2015, // year only after the tags
	}
	for title, want := range cases {
		release := ParseRelease(title, parserTestMagnet, "test")
		if release == nil || release.Year == nil || *release.Year != want {
			t.Errorf("%q: year = %v, want %d", title, release.Year, want)
		}
	}
}

func TestParseReleaseSeasonFormats(t *testing.T) {
	cases := []struct {
		title   string
		kind    string
		series  string
		season  int64
		episode int64
		pack    bool
	}{
		{"Show.Name.S2024E05.1080p", "series", "Show Name", 2024, 5, false},
		{"Show Name - Stagione 2 Episodio 3 ITA", "series", "Show Name", 2, 3, false},
		{"Show.Name.Stagione.2.Puntata.10.720p", "series", "Show Name", 2, 10, false},
		{"Show Name Stagione 3 COMPLETA ITA", "series", "Show Name", 3, 0, true},
		{"Show.Name.2160p.S02E10.HDR.DV.ATMOS", "series", "Show Name", 2, 10, false},
		{"Show.Name.S01E01.1080p", "series", "Show Name", 1, 1, false},
	}
	for _, tc := range cases {
		release := ParseRelease(tc.title, parserTestMagnet, "test")
		if release == nil || release.Kind != tc.kind || release.Series == nil || release.Season == nil || release.Episode == nil {
			t.Errorf("%q: not parsed as a series: %+v", tc.title, release)
			continue
		}
		name := *release.Series
		if name != tc.series {
			t.Errorf("%q: series = %q, want %q", tc.title, name, tc.series)
		}
		if *release.Season != tc.season || *release.Episode != tc.episode || release.IsPack != tc.pack {
			t.Errorf("%q: S%d E%d pack=%v, want S%d E%d pack=%v", tc.title, *release.Season, *release.Episode, release.IsPack, tc.season, tc.episode, tc.pack)
		}
	}
}

func TestParseQualityNoFalsePositives(t *testing.T) {
	if got := ParseQuality("Show.Name.S01E01.1080p.WEBRip.x265.10bit").Audio; got != "unknown" {
		t.Errorf("x265.10bit must not yield audio %q", got)
	}
	if got := ParseQuality("Show.Name.2024.05.12.1080p.WEB").Audio; got != "unknown" {
		t.Errorf("date must not yield audio %q", got)
	}
	if got := ParseQuality("Show.S01E01.1080p.WEB-DL.DD5.1.H264").Audio; got != "ac3" && got != "5.1" {
		t.Errorf("real 5.1 audio lost: %q", got)
	}
	if got := ParseQuality("Capital.S01E01.WEB-DL").Resolution; got != "unknown" {
		t.Errorf("'Capital' must not imply 576p, got %q", got)
	}
	if got := ParseQuality("Show.S01E01.HDR.DV").Resolution; got != "unknown" {
		t.Errorf("HDR must not imply 720p, got %q", got)
	}
	if got := ParseQuality("Show.S01E01.HDTV.x264").Resolution; got != "720p" {
		t.Errorf("HDTV keeps 720p, got %q", got)
	}
	if got := ParseQuality("Show.S01E01.1080p.WEB-DL").Group; got != "unknown" {
		t.Errorf("WEB-DL is not a group, got %q", got)
	}
	if got := ParseQuality("Show.S01E01.1080p.WEB-DL.H264-GRP").Group; got != "grp" {
		t.Errorf("group lost, got %q", got)
	}
}

func TestParseEpisodeKeyFourDigitSeason(t *testing.T) {
	key := ParseEpisodeKey("Show.Name.S2024E05.1080p")
	if key == nil || key.Season != 2024 || key.Episode != 5 {
		t.Errorf("unexpected key: %+v", key)
	}
}
