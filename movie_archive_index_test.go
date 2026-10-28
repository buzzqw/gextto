package gextto

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// writeMovieFile writes a file of the given name under the archive root.
func writeMovieFile(t *testing.T, root, name string, size int) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// TestMovieArchiveIndexFindsTheBestCopy checks what a movie decision reads from
// the archive: the best copy of the same title and year, ignoring other films,
// other years and the series files that share the volume.
func TestMovieArchiveIndexFindsTheBestCopy(t *testing.T) {
	root := t.TempDir()
	writeMovieFile(t, root, "Film 2020/Film (2020) 720p.mkv", 1024)
	writeMovieFile(t, root, "Film 2020/Film (2020) 1080p.mkv", 2048)
	writeMovieFile(t, root, "Film 2020/Film (2020) 2160p.mkv", 4096)
	writeMovieFile(t, root, "Film 2020/Film (2020) 2160p.ita.srt", 10)
	writeMovieFile(t, root, "Film 2021/Film (2021) 2160p.mkv", 4096)
	writeMovieFile(t, root, "Altro Film 2020/Altro Film (2020) 2160p.mkv", 4096)
	writeMovieFile(t, root, "Serie/Serie S01E01 2160p.mkv", 4096)

	cfg := DefaultConfig()
	cfg.ArchiveRoot = &root
	year := int64(2020)
	release := models.Release{Title: "Film", Kind: "movie", Year: &year}

	found := ArchiveMovieQuality(&cfg, &release)
	if found == nil {
		t.Fatal("the archived copy was not found")
	}
	if found.Quality.Resolution != "2160p" {
		t.Fatalf("best copy resolution = %q, want the 2160p one", found.Quality.Resolution)
	}

	// The whole-cycle index must give the same answer as the single call.
	index := BuildMovieArchiveIndex(&cfg)
	fromIndex := index.Best(&cfg, &release)
	if fromIndex == nil || fromIndex.Score != found.Score || fromIndex.Quality.Resolution != found.Quality.Resolution {
		t.Fatalf("index answer %+v differs from the single call %+v", fromIndex, found)
	}

	// A film that is not on the volume has no copy, and neither has a series.
	missing := models.Release{Title: "Film Assente", Kind: "movie", Year: &year}
	if index.Best(&cfg, &missing) != nil {
		t.Fatal("a movie missing from the archive must not match")
	}
	series := models.Release{Title: "Serie", Kind: "series"}
	if index.Best(&cfg, &series) != nil {
		t.Fatal("a series release must not be looked up in the movie index")
	}

	// Another year is a different film: the 2020 copy must not answer for 2015.
	otherYear := int64(2015)
	older := models.Release{Title: "Film", Kind: "movie", Year: &otherYear}
	if index.Best(&cfg, &older) != nil {
		t.Fatal("a copy two years apart must not match")
	}
}

// TestMovieArchiveIndexWithoutArchiveRoot makes sure an installation without an
// archive volume answers "no copy" instead of walking something else.
func TestMovieArchiveIndexWithoutArchiveRoot(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ArchiveRoot = nil
	year := int64(2020)
	release := models.Release{Title: "Film", Kind: "movie", Year: &year}
	if found := BuildMovieArchiveIndex(&cfg).Best(&cfg, &release); found != nil {
		t.Fatalf("found %+v without an archive root", found)
	}
}
