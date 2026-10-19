package gextto

import (
	"testing"
	"time"
)

// TestParseEd2kRealNames checks the inference on names actually returned by the
// live eD2k servers. The names are free-form: quality and episode identity must
// survive punctuation, dots, brackets and Italian tags.
func TestParseEd2kRealNames(t *testing.T) {
	cases := []struct {
		name       string
		resolution string
		source     string
	}{
		{"The Matrix.1999.4K.HDR.2160p.BDRip Ita Eng x265-NAHOM.mkv", "2160p", "bluray"},
		{"The.Matrix.1999.BluRay.1080p.x264.DTS-WiKi.mkv", "1080p", "bluray"},
		{"The.Matrix-1999-DVDRip.Xvid.avi", "", "dvdrip"},
		{"The Matrix 1999 REPACK 4K HDR DV 2160p BDRemux Ita Eng x265 NAHOM.mkv", "2160p", "remux"},
	}
	for _, tc := range cases {
		link := "ed2k://|file|" + tc.name + "|1000000|0123456789abcdef0123456789abcdef|/"
		release := ParseEd2kRelease(tc.name, link, "ed2k:server", 1000000, 3, time.Now().UTC())
		if release == nil {
			t.Fatalf("%q: nil release", tc.name)
		}
		if tc.resolution != "" && release.Quality.Resolution != tc.resolution {
			t.Errorf("%q: resolution = %q, want %q", tc.name, release.Quality.Resolution, tc.resolution)
		}
		if release.Quality.Source != tc.source {
			t.Errorf("%q: source = %q, want %q", tc.name, release.Quality.Source, tc.source)
		}
	}
}

func TestParseEd2kEpisodeIdentity(t *testing.T) {
	const name = "Rare.Show.S02E03.720p.ITA.mkv"
	link := "ed2k://|file|" + name + "|900000000|0123456789abcdef0123456789abcdef|/"
	release := ParseEd2kRelease(name, link, "ed2k:kad", 900000000, 8, time.Now().UTC())
	if release == nil {
		t.Fatal("nil release")
	}
	if release.Kind != "series" || release.Season == nil || *release.Season != 2 || release.Episode == nil || *release.Episode != 3 {
		t.Fatalf("identity = %+v", release)
	}
	if release.Quality.Resolution != "720p" {
		t.Fatalf("resolution = %q", release.Quality.Resolution)
	}
}
