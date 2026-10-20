package gextto

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func int64Ref(value int64) *int64 { return &value }

func TestEpisodeFileRun(t *testing.T) {
	series := "TestShow"
	season := int64(1)
	cases := []struct {
		name    string
		release *models.Release
		want    []int64
	}{
		{name: "nil", release: nil, want: nil},
		{name: "single", release: &models.Release{
			Series: &series, Season: &season, Episode: int64Ref(3), EpisodeRange: []int64{3},
		}, want: []int64{3}},
		{name: "contiguous range", release: &models.Release{
			Series: &series, Season: &season, Episode: int64Ref(1), EpisodeRange: []int64{1, 2},
		}, want: []int64{1, 2}},
		{name: "longer range", release: &models.Release{
			Series: &series, Season: &season, Episode: int64Ref(4), EpisodeRange: []int64{4, 5, 6},
		}, want: []int64{4, 5, 6}},
		{name: "non contiguous falls back to the first", release: &models.Release{
			Series: &series, Season: &season, Episode: int64Ref(1), EpisodeRange: []int64{1, 3},
		}, want: []int64{1}},
		{name: "special", release: &models.Release{
			Series: &series, Season: &season, Episode: int64Ref(0), EpisodeRange: []int64{0},
		}, want: []int64{0}},
		{name: "daily", release: &models.Release{
			Series: &series, Season: int64Ref(2026), Episode: int64Ref(292), EpisodeRange: []int64{292},
		}, want: []int64{292}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := episodeFileRun(tc.release)
			if len(got) != len(tc.want) {
				t.Fatalf("episodeFileRun = %v, want %v", got, tc.want)
			}
			for index := range got {
				if got[index] != tc.want[index] {
					t.Fatalf("episodeFileRun = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestRenderEpisodeNameCarriesTheRange(t *testing.T) {
	quality := ParseQuality("TestShow.S01E01E02.1080p.WEB-DL.H.265.mkv")
	cases := []struct {
		name   string
		format string
		want   string
	}{
		{name: "standard", format: "standard", want: "TestShow - S01E01-E02 - Episodio 1 [1080p][h265]"},
		{name: "default", format: "", want: "TestShow - S01E01-E02 - Episodio 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.RenameFormat = tc.format
			got := renderEpisodeName(&cfg, "TestShow", "Episodio 1", 1, []int64{1, 2}, quality, "", "", "")
			if got != tc.want {
				t.Fatalf("renderEpisodeName = %q, want %q", got, tc.want)
			}
		})
	}

	cfg := DefaultConfig()
	cfg.RenameFormat = "custom"
	cfg.RenameTemplate = "{Serie}/{Stagione}{Episodio} - {Titolo}"
	got := renderEpisodeName(&cfg, "TestShow", "Episodio 1", 1, []int64{1, 2}, quality, "", "", "")
	if got != "TestShow/S01E01-E02 - Episodio 1" {
		t.Fatalf("custom template = %q", got)
	}
}

func TestRenameEpisodeNamesAMultiEpisodeFile(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.RenameEpisodes = true
	cfg.RenameFormat = "standard"
	cfg.CleanupUpgrades = true
	trashDir := t.TempDir()
	cfg.TrashPath = &trashDir
	seriesName := "TestShow"
	cfg.Series = append(cfg.Series, SeriesConfig{Name: seriesName, ArchivePath: dir})

	source := filepath.Join(dir, "TestShow.S01E01E02.1080p.WEB-DL.H.265.mkv")
	writeFile(t, source, validMKV(4096))
	season := int64(1)
	episode := int64(1)
	release := &models.Release{
		Kind:         "series",
		Series:       &seriesName,
		Season:       &season,
		Episode:      &episode,
		EpisodeRange: []int64{1, 2},
		Title:        "TestShow.S01E01E02.1080p.WEB-DL.H.265",
		Quality:      ParseQuality("TestShow.S01E01E02.1080p.WEB-DL.H.265.mkv"),
	}
	renamed, err := RenameEpisode(context.Background(), source, release, &cfg, nil)
	if err != nil {
		t.Fatalf("RenameEpisode: %v", err)
	}
	base := filepath.Base(renamed)
	if !strings.Contains(base, "S01E01-E02") {
		t.Fatalf("renamed file = %q, want the episode range in the name", base)
	}
	if _, err := os.Stat(renamed); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
}

func TestFilenameMatchesEpisodeRange(t *testing.T) {
	cases := []struct {
		name    string
		episode int64
		want    bool
	}{
		{"TestShow - S01E01-E02 - Episodio 1.mkv", 1, true},
		{"TestShow - S01E01-E02 - Episodio 1.mkv", 2, true},
		{"TestShow - S01E01-E02 - Episodio 1.mkv", 3, false},
		{"TestShow.S01E01E02.1080p.mkv", 2, true},
		{"TestShow - S01E03 - solo.mkv", 2, false},
		{"TestShow - S02E01-E02.mkv", 2, false},
		// A dash before the resolution or a year is not an episode range.
		{"TestShow.S01E01-1080p.mkv", 5, false},
		{"TestShow.S01E01-1080p.mkv", 1, true},
		{"TestShow.S01E05-2019.mkv", 50, false},
	}
	for _, tc := range cases {
		if got := filenameMatchesEpisode(tc.name, 1, tc.episode); got != tc.want {
			t.Fatalf("filenameMatchesEpisode(%q, 1, %d) = %v, want %v", tc.name, tc.episode, got, tc.want)
		}
	}
}

func TestArchiveScanRegistersEveryEpisodeOfARangeFile(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDatabase(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := DefaultConfig()
	cfg.ArchiveRoot = &dir
	series := SeriesConfig{Name: "TestShow", ArchivePath: dir}

	file := filepath.Join(dir, "TestShow - S01E01-E02 - Episodio 1 [1080p][h265].mkv")
	writeFile(t, file, validMKV(4096))

	found, updated, err := gh5_scanArchivePath(db, &series, dir, &cfg)
	if err != nil {
		t.Fatalf("gh5_scanArchivePath: %v", err)
	}
	if found != 1 || updated != 1 {
		t.Fatalf("found=%d updated=%d, want 1 and 1", found, updated)
	}
	for _, episode := range []int64{1, 2} {
		path, err := db.ArchivedEpisodePath(series.Name, 1, episode)
		if err != nil {
			t.Fatalf("ArchivedEpisodePath(%d): %v", episode, err)
		}
		if path != file {
			t.Fatalf("episode %d archive path = %q, want %q", episode, path, file)
		}
	}
}
