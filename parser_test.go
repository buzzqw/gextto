package gextto

import (
	"reflect"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

const testMagnet = "magnet:?xt=urn:btih:0123456789012345678901234567890123456789"

func TestParsesPartialSeasonPack(t *testing.T) {
	release := ParseRelease("Example.Show.S02E01-05.1080p.WEB-DL.ITA", testMagnet, "test")
	if release == nil {
		t.Fatal("ParseRelease returned nil")
	}
	if release.Series == nil || *release.Series != "Example Show" {
		t.Fatalf("series = %v, want Example Show", release.Series)
	}
	if release.Season == nil || *release.Season != 2 {
		t.Fatalf("season = %v, want 2", release.Season)
	}
	if want := []int64{1, 2, 3, 4, 5}; !reflect.DeepEqual(release.EpisodeRange, want) {
		t.Fatalf("episode_range = %v, want %v", release.EpisodeRange, want)
	}
}

func TestParsesMultiEpisodeAboveNinetyNine(t *testing.T) {
	release := ParseRelease("Long.Running.Show.S01E100E101.1080p.WEB-DL", testMagnet, "test")
	if release == nil || release.Kind != "series" || !release.IsPack {
		t.Fatalf("release = %#v, want multi-episode series pack", release)
	}
	if want := []int64{100, 101}; !reflect.DeepEqual(release.EpisodeRange, want) {
		t.Fatalf("episode_range = %v, want %v", release.EpisodeRange, want)
	}
}

func TestCorrectsPackSeasonFromTheActualTorrentName(t *testing.T) {
	advertised := ParseRelease("Slow.Horses.S06E01-06.1080p.WEB-DL.ITA", testMagnet, "test")
	if advertised == nil {
		t.Fatal("advertised release is nil")
	}
	corrected := ReconcilePackIdentity(advertised, "Slow Horses S05e01-06 (1080p Ita Eng Spa h265 10bit SubS) byMe7alh")
	if corrected == nil {
		t.Fatal("torrent name should override the wrong RSS season")
	}
	if corrected.Season == nil || *corrected.Season != 5 {
		t.Fatalf("season = %v, want 5", corrected.Season)
	}
	if want := []int64{1, 2, 3, 4, 5, 6}; !reflect.DeepEqual(corrected.EpisodeRange, want) {
		t.Fatalf("episode_range = %v, want %v", corrected.EpisodeRange, want)
	}
	if corrected.Series == nil || *corrected.Series != "Slow Horses" {
		t.Fatalf("series = %v, want Slow Horses", corrected.Series)
	}
}

func TestParsesCompleteSeasonPackVariants(t *testing.T) {
	for _, title := range []string{
		"Breaking.Bad.Season.5.1080p.ITA",
		"Succession.S03.2160p.ITA",
	} {
		release := ParseRelease(title, testMagnet, "test")
		if release == nil {
			t.Fatalf("%s: release is nil", title)
		}
		if !release.IsPack {
			t.Fatalf("%s: expected is_pack", title)
		}
		if want := []int64{0}; !reflect.DeepEqual(release.EpisodeRange, want) {
			t.Fatalf("%s: episode_range = %v, want %v", title, release.EpisodeRange, want)
		}
	}
}

func TestParsesRenamedBracketQualityTagsWithoutFalseLanguageMatches(t *testing.T) {
	quality := ParseQuality("The Pitt - S01E01 - Pilot [1080p][DV HDR10][h265][IT+EN].mkv")
	if quality.Resolution != "1080p" {
		t.Fatalf("resolution = %q, want 1080p", quality.Resolution)
	}
	if quality.Codec != "h265" {
		t.Fatalf("codec = %q, want h265", quality.Codec)
	}
	if !quality.IsDV {
		t.Fatal("expected is_dv")
	}
	if want := []string{"ita", "eng"}; !reflect.DeepEqual(quality.Languages, want) {
		t.Fatalf("languages = %v, want %v", quality.Languages, want)
	}
	if ParseQuality("Series.S01E01.1080p.WEB-DL.with.subtitles").IsIta {
		t.Fatal("expected not is_ita for subtitles-only title")
	}
	if ParseQuality("Series.S01E01.1080p.WEB-DL.ENG.sub[IT]").IsIta {
		t.Fatal("expected not is_ita for eng sub[IT] title")
	}
	subtitles := ParseQuality("Movie.2024.1080p.WEB-DL.SUB.ITA.SUB.ENG")
	if want := []string{"ita", "eng"}; !reflect.DeepEqual(subtitles.SubtitleLanguages, want) {
		t.Fatalf("subtitle_languages = %v, want %v", subtitles.SubtitleLanguages, want)
	}
	if len(subtitles.Languages) != 0 {
		t.Fatalf("languages = %v, want empty", subtitles.Languages)
	}
}

func TestRejectsMagnetTruncatedTitles(t *testing.T) {
	// BTDigg a volte usa un magnet troncato come testo del link: non è un
	// titolo valido e non deve produrre una release.
	if release := ParseRelease(
		"magnet:?xt=urn:btih:0006c977cb45...",
		"magnet:?xt=urn:btih:0006c977cb45abcdefabcdefabcdefabcdefabcdef",
		"BTDigg",
	); release != nil {
		t.Fatalf("expected nil release, got %+v", release)
	}
}

func TestAcceptsJackettDownloadURLsAsTorrentSources(t *testing.T) {
	url := "http://jackett:9117/dl/limetorrents/?path=ZXhhbXBsZQ"
	if !IsTorrentURL(url) {
		t.Fatalf("expected %q to be a torrent URL", url)
	}
	release := ParseReleaseSource(
		"Example.Show.S01E01.1080p.ITA",
		"",
		&url,
		"Jackett RSS - LimeTorrents",
		time.Now().UTC(),
	)
	if release == nil {
		t.Fatal("expected Jackett URL release")
	}
	if release.Magnet != "" {
		t.Fatalf("magnet = %q, want empty", release.Magnet)
	}
	if release.TorrentURL == nil || *release.TorrentURL != url {
		t.Fatalf("torrent_url = %v, want %q", release.TorrentURL, url)
	}
}

func TestReleaseDedupKeyRetainsTorrentOnlyRelease(t *testing.T) {
	url := "https://prowlarr.example/12/download?link=abc"
	release := &models.Release{TorrentURL: &url}
	if got, ok := releaseDedupKey(release); !ok || got != "url:"+url {
		t.Fatalf("dedup key = %q, %v", got, ok)
	}
}

func TestMergesSourceFromOriginalReleaseTitle(t *testing.T) {
	// Il nome file ha perso la sorgente; il titolo originale la conserva.
	file := ParseQuality("Show - S01E01 - Titolo - [1080p][h264][AAC][IT].mkv")
	original := ParseQuality("Show.S01E01.1080p.WEB-DL.H.264.ITA.AAC")
	if file.Source != "unknown" {
		t.Fatalf("file source = %q, want unknown", file.Source)
	}
	merged := MergeQuality(original, file)
	if merged.Source != "webdl" {
		t.Fatalf("merged source = %q, want webdl", merged.Source)
	}
	if merged.Resolution != "1080p" {
		t.Fatalf("merged resolution = %q, want 1080p", merged.Resolution)
	}
	if merged.Codec != "h264" {
		t.Fatalf("merged codec = %q, want h264", merged.Codec)
	}
}

func TestRejectsCodecAsNxEpisode(t *testing.T) {
	release := ParseRelease("Example.Show.20x265.1080p.WEB-DL", testMagnet, "test")
	if release == nil {
		t.Fatal("release is nil")
	}
	if release.Kind != "movie" {
		t.Fatalf("kind = %q, want movie", release.Kind)
	}
}

// FuzzParseRelease feeds arbitrary release titles to the parser, the component
// that sees the most untrusted input. It must never panic or allocate without
// bound, whatever the upstream feed contains.
func FuzzParseRelease(f *testing.F) {
	for _, seed := range []string{
		"The.Pitt.S01E01.Pilot.1080p.WEB-DL.DDP5.1.H.264-ABC",
		"Movie.2024.2160p.UHD.BluRay.REMUX.DV.HDR10.ITA.ENG.DTS-HD.x265-GROUP",
		"Show.S02E01-05.720p.HDTV.x264.ITA.SUB.ITA",
		"Nightcrawler.2014.2160p.iT.WEB-DL.DV.HDR10+.MULTi.DTS-HD.MA.5.1.H265-BTM",
		"Daily.Show.2026-09-21.WEB-DL.1080p.ITA",
		"", "....", "S01E01", "1080p",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, title string) {
		quality := ParseQuality(title)
		_ = quality.Score()
		if release := ParseRelease(title, testMagnet, "fuzz"); release != nil {
			_ = release.Quality.Score()
		}
	})
}

func BenchmarkParseQuality(b *testing.B) {
	titles := []string{
		"The.Pitt.S01E01.Pilot.1080p.WEB-DL.DDP5.1.H.264-ABC",
		"Movie.2024.2160p.UHD.BluRay.REMUX.DV.HDR10.ITA.ENG.DTS-HD.x265-GROUP",
		"Show.S02E01-05.720p.HDTV.x264.ITA.SUB.ITA",
		"Another.Movie.2021.1080p.WEBRip.iT.WEB-DL.AAC2.0.x264",
		"Daily.Show.2026-09-21.WEB-DL.1080p.ITA",
	}
	var acc int64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, title := range titles {
			quality := ParseQuality(title)
			acc += quality.Score()
			if release := ParseRelease(title, testMagnet, "bench"); release != nil {
				acc += release.Quality.Score()
			}
		}
	}
	_ = acc
}

func TestMatchesAliasesAfterNormalization(t *testing.T) {
	if !SeriesNamesMatch("Grey's Anatomy", "Greys.Anatomy") {
		t.Fatal("expected Grey's Anatomy to match Greys.Anatomy")
	}
}

func TestMatchesSeriesNamesWithOrWithoutColon(t *testing.T) {
	if !SeriesNamesMatch("Star Trek: Strange New Worlds", "Star.Trek.Strange.New.Worlds") {
		t.Fatal("expected colon variant to match")
	}
}

func TestMatchesSafeYearAndAccentVariantsWithoutSubstringFalsePositives(t *testing.T) {
	if !SeriesNamesMatch("Dark Matter", "Dark Matter 2024") {
		t.Fatal("expected Dark Matter to match with bare year suffix")
	}
	if !SeriesNamesMatch("Astrid Raphaëlle", "Astrid.Raphaelle") {
		t.Fatal("expected accent folding")
	}
	if !SeriesNamesMatch("FBI: International", "FBI-International") {
		t.Fatal("expected colon/hyphen equivalence")
	}
	if SeriesNamesMatch("Invasion", "Secret Invasion") {
		t.Fatal("unexpected substring match")
	}
	if SeriesNamesMatch("New Tricks", "Old Dog New Tricks") {
		t.Fatal("unexpected prefix match")
	}
	if SeriesNamesMatch("Strike", "Strike Back") {
		t.Fatal("unexpected suffix match")
	}
}

func TestMatchesStylizedTitlesWithLeetAndParenthesizedYear(t *testing.T) {
	// File legacy `PLUR1BUS (2025) - S01E01 ...` con serie configurata
	// "Pluribus": il `1` leet e l'anno fra parentesi non devono impedire il
	// collegamento all'archivio.
	if !SeriesNamesMatch("Pluribus", "PLUR1BUS (2025)") {
		t.Fatal("expected leet + year match")
	}
	if !SeriesNamesMatch("Pluribus", "PLUR1BUS") {
		t.Fatal("expected leet match")
	}
	if !SeriesNamesMatch("Pluribus", "Pluribus (2025)") {
		t.Fatal("expected year-suffix match")
	}
	// I titoli numerici non devono essere sfuocati dal folding.
	if !SeriesNamesMatch("9-1-1", "9-1-1") {
		t.Fatal("expected numeric title to match itself")
	}
	if SeriesNamesMatch("9-1-1", "Pluribus") {
		t.Fatal("unexpected match between numeric and named title")
	}
	// Anno "nudo" e serie intitolate a un anno restano distinti.
	if !SeriesNamesMatch("1923", "1923") {
		t.Fatal("expected 1923 to match itself")
	}
	if SeriesNamesMatch("1923", "1883") {
		t.Fatal("unexpected match between bare years")
	}
}

func TestParsesEpisodeKeyFromReleaseName(t *testing.T) {
	key := ParseEpisodeKey("Neagley.S01E05.Trip.2160p.WEB-DL.mkv")
	if key == nil || key.Series != "neagley" || key.Season != 1 || key.Episode != 5 {
		t.Fatalf("key = %+v, want neagley 1 5", key)
	}
	key = ParseEpisodeKey("PLUR1BUS (2025) - S01E01 - We Is Us.mkv")
	if key == nil || key.Series != "plur1bus (2025)" || key.Season != 1 || key.Episode != 1 {
		t.Fatalf("key = %+v, want \"plur1bus (2025)\" 1 1", key)
	}
	key = ParseEpisodeKey("Example 2x03 Title.mkv")
	if key == nil || key.Series != "example" || key.Season != 2 || key.Episode != 3 {
		t.Fatalf("key = %+v, want example 2 3", key)
	}
	if key := ParseEpisodeKey("Movie.2026.1080p.mkv"); key != nil {
		t.Fatalf("expected nil key, got %+v", key)
	}
}

func TestDetectsItalianWithoutStreamingTagFalsePositives(t *testing.T) {
	// iT = iTunes / NF = Netflix must not be read as Italian.
	if ParseQuality("Movie.2026.2160p.iT.WEB-DL.ENG").IsIta {
		t.Fatal("iT.WEB-DL must not be Italian")
	}
	if ParseQuality("Show.S01E01.NF.WEB-DL.ENG").IsIta {
		t.Fatal("NF.WEB-DL must not be Italian")
	}
	// "It" inside the episode title must not count.
	if ParseQuality("Feel.It.Still.2026.1080p.WEB-DL.ENG").IsIta {
		t.Fatal("episode title It must not be Italian")
	}
	// Explicit Italian markers do count.
	if !ParseQuality("Movie.2026.1080p.ITA.WEB-DL").IsIta {
		t.Fatal("ITA must be Italian")
	}
	if !ParseQuality("Movie.2026.1080p.WEB-DL.[IT].x264").IsIta {
		t.Fatal("[IT] must be Italian")
	}
	if !ParseQuality("Movie.2026.1080p.iT.WEB-DL.ITA").IsIta {
		t.Fatal("ITA with iT.WEB must be Italian")
	}
}

func TestWebRipSourceDetection(t *testing.T) {
	cases := map[string]string{
		"Movie.2024.1080p.WEBRip.x264-GRP": "webrip",
		"Movie.2024.1080p.WEB-DL.x264-GRP": "webdl",
		"Movie.2024.1080p.WEBDL.x264-GRP":  "webdl",
		"Movie.2024.720p.WEBRip.AAC-iTA":   "webrip",
		"Movie.2024.720p.WEB.DL.H264":      "webdl",
		"Show.2024.S01.1080p.WEB.x264":     "webdl",
	}
	for title, want := range cases {
		if got := ParseQuality(title).Source; got != want {
			t.Errorf("ParseQuality(%q).Source = %q, want %q", title, got, want)
		}
	}
	// A WEBRip must not be treated as an HDTV->WEB-DL source upgrade over an
	// equal-resolution copy.
	webrip := ParseQuality("Movie.2024.720p.WEBRip.x264-AAC")
	hdtv := ParseQuality("Movie.2024.720p.HDTV.x264-AC3")
	if webrip.Source != "webrip" {
		t.Fatalf("webrip source = %q, want webrip", webrip.Source)
	}
	if reason := webrip.UpgradeReason(&hdtv, webrip.Score(), hdtv.Score(), 200); reason != "" {
		t.Fatalf("equal-resolution WEBRip over HDTV should not upgrade, got %q", reason)
	}
}

func TestDetectsExtendedResolutionSourceHdrAndWordReal(t *testing.T) {
	if got := ParseQuality("Movie.2026.UHD.BluRay").Resolution; got != "2160p" {
		t.Fatalf("UHD resolution = %q, want 2160p", got)
	}
	if got := ParseQuality("Movie.2026.FullHD.WEB").Resolution; got != "1080p" {
		t.Fatalf("FullHD resolution = %q, want 1080p", got)
	}
	if got := ParseQuality("Movie.2026.1080p.BDRip").Source; got != "bluray" {
		t.Fatalf("BDRip source = %q, want bluray", got)
	}
	if got := ParseQuality("Movie.2026.DVDRip.XviD").Source; got != "dvdrip" {
		t.Fatalf("DVDRip source = %q, want dvdrip", got)
	}
	if got := ParseQuality("Movie.2026.1080p.HDR10Plus").HDR; got != "HDR10Plus" {
		t.Fatalf("HDR10Plus hdr = %q, want HDR10Plus", got)
	}
	if got := ParseQuality("Movie.2026.1080p.HLG").HDR; got != "HDR" {
		t.Fatalf("HLG hdr = %q, want HDR", got)
	}
	if !ParseQuality("Movie.2026.1080p.REAL").IsReal {
		t.Fatal("REAL must set is_real")
	}
	if ParseQuality("Movie.2026.1080p.Really.Good").IsReal {
		t.Fatal("Really must not set is_real")
	}
}

func TestParsesConcatenatedMultiEpisode(t *testing.T) {
	release := ParseRelease("Example.Show.S02E01E02E03.1080p.WEB-DL.ITA", testMagnet, "test")
	if release == nil {
		t.Fatal("release is nil")
	}
	if release.Series == nil || *release.Series != "Example Show" {
		t.Fatalf("series = %v, want Example Show", release.Series)
	}
	if release.Season == nil || *release.Season != 2 {
		t.Fatalf("season = %v, want 2", release.Season)
	}
	if release.Episode == nil || *release.Episode != 1 {
		t.Fatalf("episode = %v, want 1", release.Episode)
	}
	if want := []int64{1, 2, 3}; !reflect.DeepEqual(release.EpisodeRange, want) {
		t.Fatalf("episode_range = %v, want %v", release.EpisodeRange, want)
	}
	if !release.IsPack {
		t.Fatal("expected is_pack")
	}
}

func TestMovieFilterRejectsNonMovieReleases(t *testing.T) {
	if !PassesMovieFilter("The.Batman.2022.1080p.WEB-DL") {
		t.Fatal("expected movie to pass filter")
	}
	if PassesMovieFilter("Show.S01E01.1080p") {
		t.Fatal("episode must be rejected")
	}
	if PassesMovieFilter("Breaking.Bad.Season.5.1080p") {
		t.Fatal("season pack must be rejected")
	}
	if PassesMovieFilter("WWE.Raw.2026.1080p.WEB-DL") {
		t.Fatal("wrestling must be rejected")
	}
	if PassesMovieFilter("MotoGP.2026.1080p.WEB-DL") {
		t.Fatal("motorsport must be rejected")
	}
	if PassesMovieFilter("Some.Game.Build.12345.2024") {
		t.Fatal("videogame build must be rejected")
	}
	if PassesMovieFilter("Nintendo.Switch.Game.2024") {
		t.Fatal("console game must be rejected")
	}
	if PassesMovieFilter("(Italo-Disco) Artista - Titolo") {
		t.Fatal("music genre prefix must be rejected")
	}
	if PassesMovieFilter("History.Magazine.2024.1080p") {
		t.Fatal("magazine must be rejected")
	}
}

func TestParsesDateBasedEpisode(t *testing.T) {
	release := ParseRelease("Daily.Show.2026-09-21.1080p.WEB-DL", testMagnet, "test")
	if release == nil {
		t.Fatal("release is nil")
	}
	if release.Kind != "series" {
		t.Fatalf("kind = %q, want series", release.Kind)
	}
	if release.Series == nil || *release.Series != "Daily Show" {
		t.Fatalf("series = %v, want Daily Show", release.Series)
	}
	if release.Season == nil || *release.Season != 2026 {
		t.Fatalf("season = %v, want 2026", release.Season)
	}
	if release.Episode == nil || *release.Episode != 264 {
		t.Fatalf("episode = %v, want 264", release.Episode)
	}
	if release.IsPack {
		t.Fatal("expected not is_pack")
	}
}
