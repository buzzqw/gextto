package gextto

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

// ppEvent builds a TorrentEvent for completion-path tests.
func ppEvent(name string) models.TorrentEvent {
	return models.TorrentEvent{
		Kind:     "torrent_finished",
		Hash:     "abcdef",
		Name:     name,
		SavePath: "/downloads",
	}
}

// assertStringPointer fails when got is nil or does not equal want.
func assertStringPointer(t *testing.T, got *string, want string) {
	t.Helper()
	if got == nil {
		t.Fatalf("expected %q, got nil", want)
	}
	if *got != want {
		t.Fatalf("expected %q, got %q", want, *got)
	}
}

// assertNilString fails when got is not nil.
func assertNilString(t *testing.T, got *string) {
	t.Helper()
	if got != nil {
		t.Fatalf("expected nil, got %q", *got)
	}
}

func TestMediaTagsMatchExttoForms(t *testing.T) {
	value := map[string]any{"media": map[string]any{"track": []any{
		map[string]any{
			"@type": "Video", "Width": "3840", "Height": "2160",
			"HDR_Format":               "Dolby Vision / SMPTE ST 2094 App 4",
			"HDR_Format_String":        "Dolby Vision / SMPTE ST 2094 App 4 / HDR10",
			"Transfer_characteristics": "PQ", "Format": "HEVC", "CodecID": "hvc1",
		},
		map[string]any{"@type": "Audio", "Format": "E-AC-3", "Channel_s": "6", "Language": "it", "Default": "Yes"},
		map[string]any{"@type": "Audio", "Format": "AC-3", "Channel_s": "2", "Language": "en-US"},
	}}}
	tags := parseMediaTags(value)
	assertStringPointer(t, tags.Resolution, "2160p")
	assertStringPointer(t, tags.VideoCodec, "h265")
	assertStringPointer(t, tags.HDR, "DV HDR10")
	assertStringPointer(t, tags.AudioCodec, "EAC3")
	assertStringPointer(t, tags.Channels, "5.1")
	assertStringPointer(t, tags.Languages, "IT+EN")

	// AC3 5.1 form (legacy's `{Audio}` joins codec and channels).
	ac3 := map[string]any{"media": map[string]any{"track": []any{
		map[string]any{"@type": "Video", "Width": "1920", "Height": "1080", "Format": "AVC"},
		map[string]any{"@type": "Audio", "Format": "AC-3", "Channel_s": "6", "Language": "it"},
	}}}
	tags = parseMediaTags(ac3)
	assertStringPointer(t, tags.AudioCodec, "AC3")
	assertStringPointer(t, tags.Channels, "5.1")
	assertNilString(t, tags.HDR)
}

func TestAtomicFileCopyPublishesCompleteTarget(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.mkv")
	target := filepath.Join(root, "archive", "episode.mkv")
	content := bytes.Repeat([]byte{'x'}, 128*1024)
	if err := os.WriteFile(source, content, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := copyFileAtomically(source, target); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("copied content differs from the source")
	}
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".gextto-copy-") {
			t.Fatalf("temporary copy left behind: %s", entry.Name())
		}
	}
}

func TestFilenameParticlesFollowExtto(t *testing.T) {
	if got := sourceLabel("webdl"); got != "WEB-DL" {
		t.Fatalf("sourceLabel(webdl) = %q", got)
	}
	if got := sourceLabel("bluray"); got != "BluRay" {
		t.Fatalf("sourceLabel(bluray) = %q", got)
	}
	if got := sourceLabel("webrip"); got != "WEBRip" {
		t.Fatalf("sourceLabel(webrip) = %q", got)
	}
	if got := sourceLabel("hdtv"); got != "HDTV" {
		t.Fatalf("sourceLabel(hdtv) = %q", got)
	}
	if got := sourceLabel("dvdrip"); got != "DVDRip" {
		t.Fatalf("sourceLabel(dvdrip) = %q", got)
	}
	// Empty brackets (with or without inner spaces) are removed.
	if got := cleanupFilename("Show - S01E01 - T - [][1080p][DV] "); got != "Show - S01E01 - T - [1080p][DV]" {
		t.Fatalf("cleanupFilename = %q", got)
	}
	if got := cleanupFilename("Show - [  ][1080p][]  "); got != "Show - [1080p]" {
		t.Fatalf("cleanupFilename = %q", got)
	}
	// Double spaces collapse and dangling separators are trimmed.
	if got := cleanupFilename("A  -  B - "); got != "A - B" {
		t.Fatalf("cleanupFilename = %q", got)
	}
	if got := sanitizeInvalid("Luci: spente? / test"); got != "Luci spente  test" {
		t.Fatalf("sanitizeInvalid = %q", got)
	}
	// Apostrophes are preserved (legacy test_sanitize_preserva_apostrofo).
	if got := sanitizeInvalid("Widow's Bay"); got != "Widow's Bay" {
		t.Fatalf("sanitizeInvalid = %q", got)
	}
	if got := sanitizeInvalid(""); got != "" {
		t.Fatalf("sanitizeInvalid(empty) = %q", got)
	}
}

func TestRestoresSourceBeforeResolution(t *testing.T) {
	got, ok := RestoreSourceToken(
		"Show - S01E01 - Titolo - [1080p][h264][AAC][IT].mkv",
		"webdl",
	)
	if !ok || got != "Show - S01E01 - Titolo - [WEB-DL][1080p][h264][AAC][IT].mkv" {
		t.Fatalf("RestoreSourceToken = %q, %v", got, ok)
	}
	// Già presente: niente da fare.
	if _, ok := RestoreSourceToken("Show - S01E01 - T - [WEB-DL][720p].mkv", "webdl"); ok {
		t.Fatal("already-present source token should not be restored")
	}
	// Sorgente sconosciuta o nessun tag risoluzione: niente.
	if _, ok := RestoreSourceToken("Show - S01E01 - T - [1080p].mkv", "unknown"); ok {
		t.Fatal("unknown source should not be restored")
	}
	if _, ok := RestoreSourceToken("Show - S01E01 - T.mkv", "webdl"); ok {
		t.Fatal("missing resolution marker should not be restored")
	}
}

func TestRejectsNamesWithEmptyBracketArtifacts(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RenameEpisodes = true
	cfg.RenameFormat = "custom"
	cfg.RenameTemplate = "{Serie} - {Stagione}{Episodio} - {Titolo} - [{Source}][{Risoluzione}][{VideoCodec}][{HDR}][{Audio}][{Lingue}]"
	cfg.Series = append(cfg.Series, SeriesConfig{
		Name:    "Only Murders in the Building",
		Enabled: true,
	})
	release := models.Release{
		Title:        "Only Murders in the Building S01E01",
		Magnet:       "magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
		Source:       "rss",
		Quality:      models.Quality{},
		Kind:         "series",
		Series:       stringPtr("Only Murders in the Building"),
		Season:       int64Ptr(1),
		Episode:      int64Ptr(1),
		EpisodeRange: []int64{1},
		Seeders:      -1,
		Peers:        -1,
		DiscoveredAt: time.Now().UTC(),
	}
	// Gruppi vuoti `[]`: NON conforme, va rinominato per ripulirlo.
	broken := "Only Murders in the Building - S01E01 - True Crime - [][480p][h264][][AAC][IT].mkv"
	if EpisodeNameConforms(broken, &release, &cfg) {
		t.Fatal("empty bracket artifacts must not conform")
	}
	// Un placeholder non sostituito non è un nome conforme.
	unrendered := "Only Murders in the Building - S01E01 - True Crime - [{Source}][480p][h264][AAC][IT].mkv"
	if EpisodeNameConforms(unrendered, &release, &cfg) {
		t.Fatal("unrendered placeholder must not conform")
	}
	// Nome già pulito: conforme.
	clean := "Only Murders in the Building - S01E01 - True Crime - [480p][h264][AAC][IT].mkv"
	if !EpisodeNameConforms(clean, &release, &cfg) {
		t.Fatal("clean name should conform")
	}
}

func TestConformCheckSanitizesInvalidSeriesNameCharacters(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RenameEpisodes = true
	cfg.RenameFormat = "standard"
	cfg.Series = append(cfg.Series, SeriesConfig{
		Name:    "Star Trek: Strange New Worlds",
		Enabled: true,
	})
	release := models.Release{
		Title:        "Star Trek Strange New Worlds S03E10",
		Source:       "archive",
		Quality:      models.Quality{},
		Kind:         "series",
		Series:       stringPtr("Star Trek: Strange New Worlds"),
		Season:       int64Ptr(3),
		Episode:      int64Ptr(10),
		EpisodeRange: []int64{10},
		Seeders:      -1,
		Peers:        -1,
		DiscoveredAt: time.Now().UTC(),
	}
	file := "/x/Star Trek Strange New Worlds - S03E10 - Nuove forme - [2160p][h265].mkv"
	if !EpisodeNameConforms(file, &release, &cfg) {
		t.Fatal("sanitised series name should conform")
	}
}

func TestRenamesSidecarsAndTrashesDuplicates(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "Show.01x01.Pilot.ITA.WEBRIP.mkv")
	if err := os.WriteFile(source, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Show.01x01.Pilot.ITA.WEBRIP-thumb.jpg"), []byte("thumb"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Show.01x01.Pilot.ITA.WEBRIP.srt"), []byte("sub"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Show.01x01.Pilot.ITA.WEBRIP-thumb (copia 1).jpg"), []byte("dup"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "Show - S01E01 - Pilot.mkv")
	cfg := DefaultConfig()
	cfg.CleanupAction = "delete"
	moved, err := RenameSidecars(source, target, &cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "Show - S01E01 - Pilot-thumb.jpg")); err != nil {
		t.Fatal("thumbnail was not renamed")
	}
	if _, err := os.Stat(filepath.Join(root, "Show - S01E01 - Pilot.srt")); err != nil {
		t.Fatal("subtitle was not renamed")
	}
	if _, err := os.Stat(filepath.Join(root, "Show.01x01.Pilot.ITA.WEBRIP-thumb (copia 1).jpg")); err == nil {
		t.Fatal("duplicate sidecar was not trashed")
	}
	if len(moved) != 3 {
		t.Fatalf("moved %d sidecars, want 3", len(moved))
	}
}

func TestResolvesSeriesArchiveBeforeGlobalArchive(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ArchiveRoot = stringPtr("/archive")
	cfg.Series = append(cfg.Series, SeriesConfig{
		Name:        "Example",
		ArchivePath: "/nas/example",
		Enabled:     true,
	})
	release := models.Release{
		Title:        "Example S01E01",
		Magnet:       "magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
		Source:       "rss",
		Quality:      models.Quality{},
		Kind:         "series",
		Series:       stringPtr("Example"),
		Season:       int64Ptr(1),
		Episode:      int64Ptr(1),
		EpisodeRange: []int64{1},
		Seeders:      -1,
		Peers:        -1,
		DiscoveredAt: time.Now().UTC(),
	}
	got, ok := DestinationFor(&release, &cfg)
	if !ok || got != "/nas/example" {
		t.Fatalf("DestinationFor = %q, %v", got, ok)
	}
}

func TestPlacesSeriesEpisodesInConfiguredSeasonSubfolder(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Series = append(cfg.Series, SeriesConfig{
		Name:             "Example",
		ArchivePath:      "/nas/example",
		Enabled:          true,
		SeasonSubfolders: true,
	})
	release := models.Release{
		Title:        "Example S02E03",
		Magnet:       "magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
		Source:       "rss",
		Quality:      models.Quality{},
		Kind:         "series",
		Series:       stringPtr("Example"),
		Season:       int64Ptr(2),
		Episode:      int64Ptr(3),
		EpisodeRange: []int64{3},
		Seeders:      -1,
		Peers:        -1,
		DiscoveredAt: time.Now().UTC(),
	}
	got, ok := DestinationFor(&release, &cfg)
	if !ok || got != filepath.Join("/nas/example", "Stagione 02") {
		t.Fatalf("DestinationFor = %q, %v", got, ok)
	}
}

func TestMatchesLegacyExttoTagNamesForMoviesAndSeries(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Settings["tag_dir_rules"] = `[{"tag":"Film","temp_dir":"","final_dir":"/home/user/film"},{"tag":"Serie TV","temp_dir":"","final_dir":"/home/user/serie"}]`
	movie := models.Release{
		Title:        "Example Movie",
		Magnet:       "magnet:?xt=urn:btih:abcdefabcdefabcdefabcdefabcdefabcdefabcd",
		Source:       "archive",
		Quality:      models.Quality{},
		Kind:         "movie",
		EpisodeRange: []int64{},
		Year:         int64Ptr(2024),
		Seeders:      -1,
		Peers:        -1,
		DiscoveredAt: time.Now().UTC(),
	}
	if got, ok := DestinationFor(&movie, &cfg); !ok || got != "/home/user/film" {
		t.Fatalf("movie DestinationFor = %q, %v", got, ok)
	}
	series := movie
	series.Kind = "series"
	series.Series = stringPtr("Example")
	series.Season = int64Ptr(1)
	series.Episode = int64Ptr(1)
	series.EpisodeRange = []int64{1}
	if got, ok := DestinationFor(&series, &cfg); !ok || got != "/home/user/serie" {
		t.Fatalf("series DestinationFor = %q, %v", got, ok)
	}
}

func TestCopiesPackFilesFlatWithoutRemovingTheSource(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.MkdirAll(filepath.Join(source, "Example"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "Example", "Example.S01E01.mkv"), []byte("episode"), 0o644); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "archive")
	copied, err := CopyPackFiles(source, destination)
	if err != nil {
		t.Fatal(err)
	}
	if len(copied) != 1 {
		t.Fatalf("copied %d files, want 1", len(copied))
	}
	if _, err := os.Stat(filepath.Join(source, "Example", "Example.S01E01.mkv")); err != nil {
		t.Fatal("source file was removed")
	}
	if _, err := os.Stat(filepath.Join(destination, "Example.S01E01.mkv")); err != nil {
		t.Fatal("pack file was not copied flat")
	}
}

func TestMatchesNumberedPartialPackWithoutSeasonInFileNames(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "01.mkv"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Episode 02.mkv"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	release := models.Release{
		Title:        "Example.S03E01-02.1080p.WEB-DL",
		Magnet:       "magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
		Source:       "test",
		Quality:      models.Quality{},
		Kind:         "series",
		Series:       stringPtr("Example"),
		Season:       int64Ptr(3),
		Episode:      int64Ptr(1),
		IsPack:       true,
		EpisodeRange: []int64{1, 2},
		Seeders:      -1,
		Peers:        -1,
		DiscoveredAt: time.Now().UTC(),
	}
	files, err := MatchingPackFiles(root, &release)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("matched %d files, want 2", len(files))
	}
	episodes := []int64{files[0].Episode, files[1].Episode}
	if episodes[0] != 1 || episodes[1] != 2 {
		t.Fatalf("episodes = %v, want [1 2]", episodes)
	}
	for _, file := range files {
		if file.Season != 3 {
			t.Fatalf("season = %d, want 3", file.Season)
		}
	}
}

func TestDoesNotGuessEpisodesForCompletePackWithOpaqueNames(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "01.mkv"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	release := models.Release{
		Title:        "Example.S03.COMPLETE.1080p.WEB-DL",
		Magnet:       "magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
		Source:       "test",
		Quality:      models.Quality{},
		Kind:         "series",
		Series:       stringPtr("Example"),
		Season:       int64Ptr(3),
		Episode:      int64Ptr(0),
		IsPack:       true,
		EpisodeRange: []int64{0},
		Seeders:      -1,
		Peers:        -1,
		DiscoveredAt: time.Now().UTC(),
	}
	files, err := MatchingPackFiles(root, &release)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("matched %d files, want none", len(files))
	}
}

func TestRejectsPartialPackWhenAnyDeclaredEpisodeIsMissing(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Example.S03E01.mkv"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	release := models.Release{
		Title:        "Example.S03E01-02.1080p.WEB-DL",
		Magnet:       "magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
		Source:       "test",
		Quality:      models.Quality{},
		Kind:         "series",
		Series:       stringPtr("Example"),
		Season:       int64Ptr(3),
		Episode:      int64Ptr(1),
		IsPack:       true,
		EpisodeRange: []int64{1, 2},
		Seeders:      -1,
		Peers:        -1,
		DiscoveredAt: time.Now().UTC(),
	}
	files, err := MatchingPackFiles(root, &release)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("matched %d files, want none", len(files))
	}
}

func TestVideoFilesIncludeLegacyWebmAndWmvExtensions(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Example.S01E01.webm"), []byte("webm"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Example.S01E02.wmv"), []byte("wmv"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := VideoFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("found %d video files, want 2", len(files))
	}
}

func TestRefusesDestinationInsideSourceTree(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "downloads", "pack")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(source, "archive")
	if err := ValidateDestinationFrom(source, destination); err == nil {
		t.Fatal("destination inside the source tree should be refused")
	}
}

func TestRenamesEpisodeWithoutTmdbKeyUsingSafeFallback(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source.mkv"), []byte("episode"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.RenameEpisodes = true
	cfg.RenameFormat = "standard"
	cfg.Series = append(cfg.Series, SeriesConfig{Name: "Example", Enabled: true})
	release := models.Release{
		Title:        "Example S01E01",
		Magnet:       "magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
		Source:       "rss",
		Quality:      models.Quality{},
		Kind:         "series",
		Series:       stringPtr("Example"),
		Season:       int64Ptr(1),
		Episode:      int64Ptr(1),
		EpisodeRange: []int64{1},
		Seeders:      -1,
		Peers:        -1,
		DiscoveredAt: time.Now().UTC(),
	}
	renamed, err := RenameEpisode(context.Background(), root, &release, &cfg, NewTmdbClient(nil))
	if err != nil {
		t.Fatal(err)
	}
	if renamed == "" || !strings.Contains(filepath.Base(renamed), "S01E01") {
		t.Fatalf("renamed = %q", renamed)
	}
	if _, err := os.Stat(filepath.Join(root, "source.mkv")); err == nil {
		t.Fatal("source file still exists")
	}
}

func TestPreviewPicksEpisodeFileFromSeriesFolder(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Example - S01E01 - Primo.mkv"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Example - S01E02 - Secondo.mkv"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.RenameEpisodes = true
	cfg.RenameFormat = "standard"
	cfg.Series = append(cfg.Series, SeriesConfig{Name: "Example", Seasons: "1+", Enabled: true})
	release := models.Release{
		Title:        "Example S01E02",
		Source:       "archive",
		Quality:      models.Quality{},
		Kind:         "series",
		Series:       stringPtr("Example"),
		Season:       int64Ptr(1),
		Episode:      int64Ptr(2),
		EpisodeRange: []int64{2},
		Seeders:      -1,
		Peers:        -1,
		DiscoveredAt: time.Now().UTC(),
	}
	target, err := PreviewEpisodeRename(context.Background(), root, &release, &cfg, NewTmdbClient(nil))
	if err != nil {
		t.Fatal(err)
	}
	if target == "" {
		t.Fatal("expected a preview target")
	}
	name := filepath.Base(target)
	if !strings.Contains(name, "S01E02") {
		t.Fatalf("nome inatteso: %s", name)
	}
	if strings.Contains(name, "S01E01") {
		t.Fatalf("ha scelto l'episodio sbagliato: %s", name)
	}
}

func TestRenamesMovieWithoutTmdbKeyUsingConfiguredTitle(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Example.Movie.2024.1080p.mkv"), []byte("movie"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.RenameEpisodes = true
	cfg.Movies = append(cfg.Movies, MovieConfig{Name: "Example Movie", Year: "2024", Enabled: true})
	release := models.Release{
		Title:        "Example.Movie.2024.1080p",
		Magnet:       "magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
		Source:       "rss",
		Quality:      models.Quality{},
		Kind:         "movie",
		EpisodeRange: []int64{},
		Year:         int64Ptr(2024),
		Seeders:      -1,
		Peers:        -1,
		DiscoveredAt: time.Now().UTC(),
	}
	renamed, err := RenameMovie(context.Background(), root, &release, &cfg, NewTmdbClient(nil))
	if err != nil {
		t.Fatal(err)
	}
	if renamed == "" || !strings.Contains(filepath.Base(renamed), "Example Movie") {
		t.Fatalf("renamed = %q", renamed)
	}
}

func TestParsesMediaInfoTagsForFullRename(t *testing.T) {
	value := map[string]any{"media": map[string]any{"track": []any{
		map[string]any{"@type": "Video", "Width": "1920", "Height": "1080", "Format": "HEVC", "HDR_Format": "HDR10"},
		map[string]any{"@type": "Audio", "Commercial_Name": "Dolby Digital Plus", "Channel_s": "6", "Language": "it"},
		map[string]any{"@type": "Audio", "Format": "AAC", "Channel_s": "2", "Language": "en"},
	}}}
	tags := parseMediaTags(value)
	assertStringPointer(t, tags.Resolution, "1080p")
	assertStringPointer(t, tags.VideoCodec, "h265")
	assertStringPointer(t, tags.HDR, "HDR10")
	assertStringPointer(t, tags.Channels, "5.1")
	assertStringPointer(t, tags.Languages, "IT+EN")
}

func TestTagRulesChooseTempAndFinalDirectories(t *testing.T) {
	root := t.TempDir()
	temp := filepath.Join(root, "temp")
	finalDir := filepath.Join(root, "final")
	if err := os.MkdirAll(temp, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(finalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Settings["tag_dir_rules"] = `[{"tag":"Serie TV","temp_dir":` + postprocessJSONString(temp) + `,"final_dir":` + postprocessJSONString(finalDir) + `}]`
	release := models.Release{
		Title:        "Example S01E01",
		Magnet:       "magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
		Source:       "rss",
		Quality:      models.Quality{},
		Kind:         "series",
		Series:       stringPtr("Example"),
		Season:       int64Ptr(1),
		Episode:      int64Ptr(1),
		EpisodeRange: []int64{1},
		Seeders:      -1,
		Peers:        -1,
		DiscoveredAt: time.Now().UTC(),
	}
	if got, ok := DownloadDirFor(&release, &cfg); !ok || got != temp {
		t.Fatalf("DownloadDirFor = %q, %v", got, ok)
	}
	if got, ok := DestinationFor(&release, &cfg); !ok || got != finalDir {
		t.Fatalf("DestinationFor = %q, %v", got, ok)
	}
}

func TestComicCategoryUsesTagDirRules(t *testing.T) {
	root := t.TempDir()
	temp := filepath.Join(root, "temp")
	finalDir := filepath.Join(root, "final")
	if err := os.MkdirAll(temp, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(finalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Settings["tag_dir_rules"] = `[{"tag":"Film","temp_dir":"","final_dir":"/tmp/film"},{"tag":"Comic","temp_dir":` +
		postprocessJSONString(temp) + `,"final_dir":` + postprocessJSONString(finalDir) + `}]`
	categoryTemp, tempOK, categoryFinal, finalOK := CategoryDirs(&cfg, "Comic")
	if !tempOK || categoryTemp != temp {
		t.Fatalf("CategoryDirs temp = %q, %v", categoryTemp, tempOK)
	}
	if !finalOK || categoryFinal != finalDir {
		t.Fatalf("CategoryDirs final = %q, %v", categoryFinal, finalOK)
	}
	// La cartella di download preferisce la destinazione finale.
	if got, ok := ComicDownloadDir(&cfg); !ok || got != finalDir {
		t.Fatalf("ComicDownloadDir = %q, %v", got, ok)
	}
	if got, ok := CategoryDownloadDir(&cfg, "Comic"); !ok || got != finalDir {
		t.Fatalf("CategoryDownloadDir = %q, %v", got, ok)
	}
}

func TestIgnoresMissingTempDirectoryInTagRules(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Settings["tag_dir_rules"] = `[{"tag":"movie","temp_dir":"/nonexistent/gextto-temp","final_dir":"/tmp/final"}]`
	release := models.Release{
		Title:        "Example Movie 2024",
		Magnet:       "magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
		Source:       "rss",
		Quality:      models.Quality{},
		Kind:         "movie",
		EpisodeRange: []int64{},
		Year:         int64Ptr(2024),
		Seeders:      -1,
		Peers:        -1,
		DiscoveredAt: time.Now().UTC(),
	}
	if _, ok := DownloadDirFor(&release, &cfg); ok {
		t.Fatal("missing temp directory should be ignored")
	}
}

func TestCompletionPathNeverEscapesSavePath(t *testing.T) {
	get := func(name string) string {
		event := ppEvent(name)
		return CompletionPath(&event)
	}
	if got := get("Show.S01E01.mkv"); got != "/downloads/Show.S01E01.mkv" {
		t.Fatalf("got %q", got)
	}
	// Absolute and traversal names keep only the final component.
	if got := get("/etc/passwd"); got != "/downloads/passwd" {
		t.Fatalf("got %q", got)
	}
	if got := get("../../etc/passwd"); got != "/downloads/passwd" {
		t.Fatalf("got %q", got)
	}
	if got := get("a/b/c.bin"); got != "/downloads/c.bin" {
		t.Fatalf("got %q", got)
	}
	// A name whose final component is `..` or empty must not resolve to the
	// shared root; callers must see a missing path.
	rootFallback := get("..")
	if rootFallback == "/downloads" {
		t.Fatal("traversal name resolved to the shared root")
	}
	if filepath.Dir(rootFallback) != "/downloads" {
		t.Fatalf("parent = %q", filepath.Dir(rootFallback))
	}
	if !strings.HasPrefix(filepath.Base(rootFallback), ".gextto-missing-") {
		t.Fatalf("base = %q", filepath.Base(rootFallback))
	}
	if got := get(""); got == "/downloads" {
		t.Fatal("empty name resolved to the shared root")
	}
}

func TestCompletionPathReturnsNamedPathEvenWhenMissing(t *testing.T) {
	event := ppEvent("not-here.mkv")
	missing := CompletionPath(&event)
	if missing != "/downloads/not-here.mkv" {
		t.Fatalf("got %q", missing)
	}
	if _, err := os.Stat(missing); err == nil {
		t.Fatal("path unexpectedly exists")
	}
}

func TestBestEpisodeFilePrefersHigherResolutionAndIgnoresPartials(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Show - S01E01 - T - [WEB-DL][1080p][h265].mkv"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Show.S01E01.2160p.WEB-DL.H265.mkv"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Show.S01E01.2160p.WEB-DL.H265.mkv.gextto-part"), []byte("c"), 0o644); err != nil {
		t.Fatal(err)
	}
	best, ok := BestEpisodeFile(dir, 1, 1)
	if !ok {
		t.Fatal("episode present")
	}
	if filepath.Base(best) != "Show.S01E01.2160p.WEB-DL.H265.mkv" {
		t.Fatalf("best = %q", filepath.Base(best))
	}
	if _, ok := BestEpisodeFile(dir, 1, 2); ok {
		t.Fatal("episode 2 should be absent")
	}
}

// jsonString encodes a filesystem path as a JSON string literal.
func postprocessJSONString(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return `"` + value + `"`
	}
	return string(encoded)
}
