package gextto

import (
	"fmt"
	"testing"
)

// benchmarkMovieConfig is a library of 35 monitored films with the aliases and
// translations TMDB gives a popular title, as in a real installation.
func benchmarkMovieConfig() *Config {
	cfg := &Config{}
	for i := 0; i < 35; i++ {
		movie := MovieConfig{Name: fmt.Sprintf("Example Movie Number %d", i), Year: "2024", Enabled: true,
			OriginalTitle: fmt.Sprintf("Original Example Title %d", i), TitleTranslations: map[string]string{}}
		for a := 0; a < 15; a++ {
			movie.Aliases = append(movie.Aliases, fmt.Sprintf("Alternative Example Name %d Part %d", i, a))
		}
		for n, lang := range []string{"it", "de", "fr", "es", "pl", "pt", "ja", "ko", "ru", "zh"} {
			movie.TitleTranslations[lang] = fmt.Sprintf("Translated Élite Title %d Edition %d", i, n)
		}
		cfg.Movies = append(cfg.Movies, movie)
	}
	return cfg
}

// BenchmarkFindMovieMatch is one release checked against the library: a cycle
// does it for every one of tens of thousands of releases.
func BenchmarkFindMovieMatch(b *testing.B) {
	cfg := benchmarkMovieConfig()
	year := int64(2024)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		cfg.FindMovieMatch("Some.Unrelated.Film.2024.1080p.WEB-DL.DDP5.1.H264-GROUP", &year)
	}
}

// The library title that a release opens with is still found.
func TestFindMovieMatchFindsAnAliasAndATranslation(t *testing.T) {
	cfg := benchmarkMovieConfig()
	year := int64(2024)
	for _, title := range []string{
		"Example.Movie.Number.7.2024.1080p.WEB-DL-GROUP",
		"Alternative.Example.Name.7.Part.3.2024.1080p.WEB-DL-GROUP",
		"Translated.Elite.Title.7.Edition.3.2024.1080p.WEB-DL-GROUP",
	} {
		movie := cfg.FindMovieMatch(title, &year)
		if movie == nil || movie.Name != "Example Movie Number 7" {
			t.Fatalf("%s matched %v, want the seventh film", title, movie)
		}
	}
	if movie := cfg.FindMovieMatch("Some.Unrelated.Film.2024.1080p.WEB-DL-GROUP", &year); movie != nil {
		t.Fatalf("an unrelated release matched %q", movie.Name)
	}
}

// An accented capital is folded like a small letter: a title, an alias or a
// translation that starts with one ("Élite", "È") meets the release that
// spells it without the accent.
func TestMatchingNameFoldsAccentedCapitals(t *testing.T) {
	for in, want := range map[string]string{
		"Élite":             "elite",
		"È Arrivata":        "e arrivata",
		"À Bout De Souffle": "a bout de souffle",
		"élite":             "elite",
	} {
		if got := matchingName(in); got != want {
			t.Fatalf("matchingName(%q) = %q, want %q", in, got, want)
		}
	}
}
