package gextto

// parser_matcher_table_test.go pins what the ordered matcher table of
// ParseReleaseSource must do: a pattern that matches a title but refuses its
// values (a season of zero, a date that does not exist) lets the next pattern
// read the same title, and a title no pattern reads stays a movie.
//
// The archive of this installation (342.587 distinct titles) changed kind for
// no title when the chain became this table: the guards that refuse a title
// are doing their job on the real names that reach them ("DD 5 1x264",
// "2026.2.0"), which must stay movies. The synthetic cases below cover the
// fall-through itself, which no real title needed yet.

import (
	"testing"
)

func TestGuardsFallThroughToTheNextMatcher(t *testing.T) {
	// The date pattern matches "2024.13.40" but month 13 and day 40 do not
	// exist, so the title used to stay a movie without the season pack
	// pattern ever seeing it.
	r := ParseRelease("Show.Name.S01.Complete.2024.13.40.1080p", parserTestMagnet, "test")
	if r == nil || r.Kind != "series" || r.Season == nil || *r.Season != 1 || !r.IsPack {
		t.Fatalf("a refused date must not stop the season-pack pattern: %+v", r)
	}
	// The NxNN pattern reads "0x01" as season zero and refuses it, so the
	// "Stagione 2" pack behind it must still be read.
	r = ParseRelease("Show.Name.0x01.Stagione.2.Complete.ITA", parserTestMagnet, "test")
	if r == nil || r.Kind != "series" || r.Season == nil || *r.Season != 2 || !r.IsPack {
		t.Fatalf("a refused NxNN must not stop the season-pack pattern: %+v", r)
	}
}

func TestRefusedTitlesStayMovies(t *testing.T) {
	// Real titles from the archive whose only "season and episode" is a codec
	// or a version number: the guard that refuses them is the reason they are
	// not read as a series, and a later pattern must not change that.
	for _, title := range []string{
		"Lenin 2026 Telugu 1080p WEB DL DD 5 1x264 KIN",
		"BrawlinCellBlock992017720pBluRayDD5 1x264 LoRD",
		"Lupin III  The Castle of Cagliostro (1979) (BDRip 1920x1040p x265 HEVC FLAC 2 0x1  amp  1 0x1)",
		"Maxon CINEMA 4D 2026.2.0 (x64) Multilingual + Activation",
		"Boris FX VEGAS Pro 2026.0.0.105 FINAL + Patch",
	} {
		r := ParseRelease(title, parserTestMagnet, "test")
		if r == nil {
			t.Fatalf("%q: nil release", title)
		}
		if r.Kind != "movie" {
			t.Fatalf("%q: kind=%s, want movie", title, r.Kind)
		}
	}
}

// An "in progress" pack whose count is unusable is dropped, not read as a
// complete season: a complete season would invent gaps up to the provider
// count and download the pack for nothing.
func TestInProgressPackWithoutACountIsDropped(t *testing.T) {
	for _, title := range []string{
		"Show.Name.S01.IN.CORSO.00/18.1080p",
		"Show.Name.S01.In.Progress.00/06.ITA",
	} {
		if r := ParseRelease(title, parserTestMagnet, "test"); r != nil {
			t.Fatalf("%q: parsed as %+v, want the release dropped", title, r)
		}
	}
}
