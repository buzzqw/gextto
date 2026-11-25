package gextto

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func TestMoveSidecarsToTargetPreservesSuffixAndReplacesOld(t *testing.T) {
	download := t.TempDir()
	archive := t.TempDir()
	video := filepath.Join(download, "Example.Movie.2024.2160p.mkv")
	for name, data := range map[string]string{
		filepath.Base(video):                      "video",
		"Example.Movie.2024.2160p.ita.forced.srt": "new subtitles",
		"Example.Movie.2024.2160p.eng.srt":        "english subtitles",
		"Example.Movie.2.ita.srt":                 "unrelated",
	} {
		if err := os.WriteFile(filepath.Join(download, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(archive, "Example Movie (2024) [2160p].mkv")
	oldSub := filepath.Join(archive, "Example Movie (2024) [2160p].ita.forced.srt")
	if err := os.WriteFile(oldSub, []byte("old subtitles"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.CleanupAction = "delete"

	moved, err := MoveSidecarsToTarget(video, target, &cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 2 {
		t.Fatalf("moved = %v, want the two associated subtitles", moved)
	}
	for name, want := range map[string]string{
		"Example Movie (2024) [2160p].ita.forced.srt": "new subtitles",
		"Example Movie (2024) [2160p].eng.srt":        "english subtitles",
	} {
		data, err := os.ReadFile(filepath.Join(archive, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Fatalf("%s = %q, want %q", name, data, want)
		}
	}
	if _, err := os.Stat(filepath.Join(download, "Example.Movie.2.ita.srt")); err != nil {
		t.Fatalf("unrelated subtitle was moved: %v", err)
	}
}

func TestCopySidecarsToTargetLeavesTheSeedingCopy(t *testing.T) {
	download := t.TempDir()
	archive := t.TempDir()
	video := filepath.Join(download, "Show.S01E01.1080p.mkv")
	sub := filepath.Join(download, "Show.S01E01.1080p.ita.srt")
	if err := os.WriteFile(video, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sub, []byte("subtitles"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(archive, "Show - S01E01 - Titolo.mkv")
	cfg := DefaultConfig()
	if _, err := CopySidecarsToTarget(video, target, &cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sub); err != nil {
		t.Fatalf("the seeding copy lost its subtitle: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(archive, "Show - S01E01 - Titolo.ita.srt"))
	if err != nil || string(data) != "subtitles" {
		t.Fatalf("archived subtitle = %q, err = %v", data, err)
	}
}

func TestRenameMovieBringsItsSidecars(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "Example.Movie.2024.1080p.mkv")
	if err := os.WriteFile(source, []byte("movie"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Example.Movie.2024.1080p.ita.srt"), []byte("sub"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.RenameEpisodes = true
	cfg.Movies = append(cfg.Movies, MovieConfig{Name: "Example Movie", Year: "2024", Enabled: true})
	release := models.Release{Title: "Example.Movie.2024.1080p", Kind: "movie", Year: int64Ptr(2024)}

	renamed, err := RenameMovie(context.Background(), root, &release, &cfg, NewTmdbClient(nil))
	if err != nil {
		t.Fatal(err)
	}
	if renamed == "" || renamed == source {
		t.Fatalf("renamed = %q", renamed)
	}
	stem := strings.TrimSuffix(filepath.Base(renamed), filepath.Ext(renamed))
	if _, err := os.Stat(filepath.Join(root, stem+".ita.srt")); err != nil {
		t.Fatalf("subtitle did not follow the movie: %v", err)
	}
}

func TestDiscardSidecarsDoesNotTouchSimilarlyNamedFiles(t *testing.T) {
	root := t.TempDir()
	video := filepath.Join(root, "Example Movie.mkv")
	for name := range map[string]bool{
		"Example Movie.mkv":       true,
		"Example Movie.ita.srt":   true,
		"Example Movie 2.ita.srt": true,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := DefaultConfig()
	cfg.CleanupAction = "delete"
	removed, err := DiscardSidecars(video, &cfg)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(filepath.Join(root, "Example Movie 2.ita.srt")); err != nil {
		t.Fatalf("similarly named subtitle was removed: %v", err)
	}
}
