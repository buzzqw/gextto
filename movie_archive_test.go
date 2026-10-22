package gextto

import (
	"path/filepath"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func TestArchiveMovieQualityFindsAFileWithoutADatabaseRow(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "Example.Movie.2024.1080p.WEB-DL.mkv")
	writeFile(t, file, make([]byte, 100))
	cfg := DefaultConfig()
	cfg.ArchiveRoot = &root
	year := int64(2024)
	release := &models.Release{
		Title:   "Example Movie",
		Year:    &year,
		Kind:    "movie",
		Quality: models.Quality{Resolution: "1080p", Source: "webdl"},
	}
	disk := ArchiveMovieQuality(&cfg, release)
	if disk == nil {
		t.Fatal("the movie in the archive was not found")
	}
	if disk.Quality.Resolution != "1080p" || disk.Score <= 0 {
		t.Fatalf("disk quality = %+v", *disk)
	}
}

func TestMovieEqualOrLowerOnDiskIsRejectedBeforeDownload(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "Example.Movie.2024.1080p.WEB-DL.mkv")
	writeFile(t, file, make([]byte, 100))
	cfg := DefaultConfig()
	cfg.ArchiveRoot = &root
	year := int64(2024)
	release := models.Release{
		Title:   "Example Movie",
		Year:    &year,
		Kind:    "movie",
		Quality: models.Quality{Resolution: "1080p", Source: "webdl"},
		Magnet:  "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	disk := ArchiveMovieQuality(&cfg, &release)
	if disk == nil {
		t.Fatal("the movie in the archive was not found")
	}
	db := newTestDB(t)
	score := cfg.ReleaseScore(&release)

	approved, reason, err := db.checkMovieScoredWithDisk(&release, score, 0, 0, false, true, disk, nil)
	if err != nil {
		t.Fatal(err)
	}
	if approved || reason != "duplicate" {
		t.Fatalf("equal movie = approved:%v reason:%q, want duplicate", approved, reason)
	}

	lower := release
	lower.Quality = models.Quality{Resolution: "720p", Source: "webdl", Codec: "h264"}
	approved, reason, err = db.checkMovieScoredWithDisk(&lower, cfg.ReleaseScore(&lower), 0, 0, false, true, disk, nil)
	if err != nil {
		t.Fatal(err)
	}
	if approved || reason != "duplicate" {
		t.Fatalf("lower movie = approved:%v reason:%q, want duplicate", approved, reason)
	}

	better := release
	better.Magnet = "magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	better.Quality = models.Quality{Resolution: "2160p", Source: "webdl", Codec: "h265"}
	approved, reason, err = db.checkMovieScoredWithDisk(&better, cfg.ReleaseScore(&better), 0, 0, false, true, disk, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !approved || reason != "upgrade" {
		t.Fatalf("better movie = approved:%v reason:%q, want upgrade", approved, reason)
	}
}

func TestMovieArchiveQualityIgnoresAnotherYear(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Example.Movie.2020.1080p.WEB-DL.mkv"), make([]byte, 10))
	cfg := DefaultConfig()
	cfg.ArchiveRoot = &root
	year := int64(2024)
	release := &models.Release{Title: "Example Movie", Year: &year, Kind: "movie"}
	if disk := ArchiveMovieQuality(&cfg, release); disk != nil {
		t.Fatalf("found the wrong year: %+v", disk)
	}
}

func TestSeriesFileOnDiskBlocksEqualDownload(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Show.S01E01.1080p.WEB-DL.mkv"), make([]byte, 100))
	cfg := DefaultConfig()
	cfg.Series = []SeriesConfig{{Name: "Show", ArchivePath: root}}
	index := IndexArchive("Show", root, &cfg)
	season, episode := int64(1), int64(1)
	series := "Show"
	release := models.Release{
		Title:   "Show S01E01 1080p WEB-DL",
		Series:  &series,
		Season:  &season,
		Episode: &episode,
		Kind:    "series",
		Quality: models.Quality{Resolution: "1080p", Source: "webdl"},
		Magnet:  "magnet:?xt=urn:btih:cccccccccccccccccccccccccccccccccccccccc",
	}
	db := newTestDB(t)
	approved, reason, err := db.CheckSeriesScored(&release, cfg.ReleaseScore(&release), 0, &models.ApprovalContext{
		Archive: &index,
		DryRun:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved || reason != "duplicate" {
		t.Fatalf("series file on disk = approved:%v reason:%q, want duplicate", approved, reason)
	}
}
