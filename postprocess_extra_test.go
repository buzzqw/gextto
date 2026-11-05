package gextto

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func TestAllPackFilesDiscardedIncludesEmptyProcessingResult(t *testing.T) {
	if !tevAllPackFilesDiscarded(nil) {
		t.Fatal("empty pack processing must be discarded, not marked completed")
	}
	if !tevAllPackFilesDiscarded([]PackFileResult{{Discarded: true}}) {
		t.Fatal("fully discarded pack must be rejected")
	}
	if tevAllPackFilesDiscarded([]PackFileResult{{Discarded: true}, {Discarded: false}}) {
		t.Fatal("pack with a retained file must be accepted")
	}
}

func validMKV(extra int) []byte {
	data := append([]byte{0x1A, 0x45, 0xDF, 0xA3}, make([]byte, extra)...)
	return data
}

func TestRenameEpisodeAppliesTemplate(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "Show.S01E01.1080p.WEB-DL.ITA.ENG.H264-TBK.mkv")
	writeFile(t, source, validMKV(4096))

	cfg := DefaultConfig()
	cfg.DataDir = dir
	cfg.TrashPath = nil
	cfg.RenameEpisodes = true
	cfg.RenameFormat = "" // default: "Serie - SxxExx - Titolo"
	cfg.Series = []SeriesConfig{{
		Name:        "Show",
		ArchivePath: dir,
		TmdbID:      "123",
		Enabled:     true,
	}}

	release := ParseRelease("Show.S01E01.1080p.WEB-DL.ITA.ENG.H264-TBK", "magnet:?xt=urn:btih:"+repeatTest("a", 40), "test")
	if release == nil {
		t.Fatal("release not parsed")
	}
	// The TmdbClient has no API key: EpisodeTitle returns no title without
	// touching the network, so the fallback "Episodio N" is used.
	renamed, err := RenameEpisode(context.Background(), source, release, &cfg, NewTmdbClientWithLanguage(nil, "it-IT"))
	if err != nil {
		t.Fatalf("RenameEpisode: %v", err)
	}
	const want = "Show - S01E01 - Episodio 1.mkv"
	if filepath.Base(renamed) != want {
		t.Fatalf("renamed to %q, want %q", filepath.Base(renamed), want)
	}
	if _, err := os.Stat(renamed); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
	if _, err := os.Stat(source); err == nil {
		t.Fatal("original file still present after rename")
	}
	if err := validateCompletedFile(renamed); err != nil {
		t.Fatalf("renamed file failed validation: %v", err)
	}
	if !EpisodeNameConforms(renamed, release, &cfg) {
		t.Fatal("renamed episode does not conform to the configured format")
	}
}

func TestMatchingPackFilesSelectsRequestedEpisodes(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"Show.S01E01.1080p.WEB-DL.mkv",
		"Show.S01E02.1080p.WEB-DL.mkv",
		"Show.S01E03.1080p.WEB-DL.mkv",
	} {
		writeFile(t, filepath.Join(dir, name), validMKV(2048))
	}
	season := int64(1)
	pack := &models.Release{
		Title:        "Show.S01.1080p.WEB-DL",
		Quality:      models.Quality{Resolution: "1080p", Source: "webdl"},
		Kind:         "series",
		Season:       &season,
		IsPack:       true,
		EpisodeRange: []int64{1, 2},
	}
	matched, err := MatchingPackFiles(dir, pack)
	if err != nil {
		t.Fatalf("MatchingPackFiles: %v", err)
	}
	if len(matched) != 2 {
		t.Fatalf("matched %d files, want 2: %+v", len(matched), matched)
	}
	seen := map[int64]bool{}
	for _, file := range matched {
		seen[file.Episode] = true
		if file.Season != 1 {
			t.Fatalf("season = %d, want 1", file.Season)
		}
	}
	if !seen[1] || !seen[2] {
		t.Fatalf("episodes = %v, want 1 and 2", seen)
	}

	best, ok := BestEpisodeFile(dir, 1, 2)
	if !ok || filepath.Base(best) != "Show.S01E02.1080p.WEB-DL.mkv" {
		t.Fatalf("BestEpisodeFile = %q (%v)", best, ok)
	}
	if _, ok := BestEpisodeFile(dir, 1, 9); ok {
		t.Fatal("found a nonexistent episode")
	}
}

func TestRenameMovieWithMockedTmdb(t *testing.T) {
	server := tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Only the movie search endpoint is needed for this test.
		tmdbWriteJSON(t, w, `{"results":[{"id":42,"title":"The Veil","original_title":"The Veil","release_date":"2024-01-01"}]}`)
	})
	_ = server

	dir := t.TempDir()
	source := filepath.Join(dir, "The.Veil.2024.1080p.WEB-DL.mkv")
	writeFile(t, source, validMKV(4096))

	cfg := DefaultConfig()
	cfg.DataDir = dir
	cfg.RenameEpisodes = true
	cfg.RenameFormat = ""
	key := "test-key"
	year := int64(2024)
	release := &models.Release{
		Title:        "The Veil 2024 1080p WEB-DL",
		Quality:      models.Quality{Resolution: "1080p", Source: "webdl"},
		Kind:         "movie",
		Year:         &year,
		EpisodeRange: []int64{},
	}

	renamed, err := RenameMovie(context.Background(), source, release, &cfg, NewTmdbClientWithLanguage(&key, "it-IT"))
	if err != nil {
		t.Fatalf("RenameMovie: %v", err)
	}
	if renamed == "" {
		t.Fatal("movie was not renamed")
	}
	if _, err := os.Stat(renamed); err != nil {
		t.Fatalf("renamed movie missing: %v", err)
	}
	if err := validateCompletedFile(renamed); err != nil {
		t.Fatalf("renamed movie failed validation: %v", err)
	}
}

func repeatTest(value string, count int) string {
	out := ""
	for i := 0; i < count; i++ {
		out += value
	}
	return out
}

// TestRenameMovieFollowsTheMovieTemplate: with the Custom format a movie takes
// its own template, with the tokens a movie has; the default keeps the name
// movies had before the template existed.
func TestRenameMovieFollowsTheMovieTemplate(t *testing.T) {
	year := int64(1999)
	release := func() *models.Release {
		return &models.Release{
			Title:   "Example Movie 1999 2160p WEB-DL x265 HDR10-GRP",
			Quality: models.Quality{Resolution: "2160p", Source: "webdl", Codec: "x265", HDR: "HDR10", Group: "GRP"},
			Kind:    "movie",
			Year:    &year,
		}
	}
	rename := func(template string) string {
		t.Helper()
		dir := t.TempDir()
		source := filepath.Join(dir, "Example.Movie.1999.2160p.WEB-DL.x265.HDR10-GRP.mkv")
		writeFile(t, source, validMKV(4096))
		cfg := DefaultConfig()
		cfg.DataDir = dir
		cfg.RenameEpisodes = true
		cfg.RenameFormat = "custom"
		cfg.RenameMovieTemplate = template
		cfg.Movies = []MovieConfig{{Name: "Example Movie", Year: "1999", Enabled: true}}
		renamed, err := RenameMovie(context.Background(), source, release(), &cfg, nil)
		if err != nil {
			t.Fatalf("RenameMovie: %v", err)
		}
		return filepath.Base(renamed)
	}
	if got := rename(defaultRenameMovieTemplateValue); got != "Example Movie (1999) [2160p].mkv" {
		t.Fatalf("default movie template = %q", got)
	}
	if got := rename("{Titolo} ({Anno}) [{Risoluzione}][{HDR}][{VideoCodec}] - {Gruppo}"); got != "Example Movie (1999) [2160p][HDR10][x265] - GRP.mkv" {
		t.Fatalf("custom movie template = %q", got)
	}
	// Episode tokens mean nothing for a movie: they vanish with their brackets.
	if got := rename("{Titolo} [{Stagione}{Episodio}] ({Anno})"); got != "Example Movie (1999).mkv" {
		t.Fatalf("episode tokens in a movie template = %q", got)
	}
}

func TestRenameMoviePreviewUsesTheMovieTemplate(t *testing.T) {
	if got := v2RenameComposeMovie("custom", "{Titolo} ({Anno}) [{Risoluzione}][{Lingue}]"); got != "Titolo Film (2024) [2160p][ita].mkv" {
		t.Fatalf("movie preview = %q", got)
	}
	if got := v2RenameComposeMovie("custom", ""); got != "Titolo Film (2024) [2160p].mkv" {
		t.Fatalf("empty movie template preview = %q", got)
	}
}
