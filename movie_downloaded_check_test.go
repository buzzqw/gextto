package gextto

// movie_downloaded_check_test.go locks the check the monitored-movies list
// shows next to the status when a copy is already in the library, with the
// archive path (or the title) in its tooltip.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMovieLibraryMarksDownloadedCopies drives the real endpoint: a monitored
// movie with an archived copy carries the path of the file, one without a copy
// carries nothing.
func TestMovieLibraryMarksDownloadedCopies(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveLibraryConfig(state.cfg.DataDir, nil, []MovieConfig{
		{ID: 1, Name: "Example Movie", Year: "2024", Enabled: true},
		{ID: 2, Name: "Another Film", Year: "2021", Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	const hash = "0123456789abcdef0123456789abcdef01234567"
	if _, err := state.db.db.Exec("INSERT INTO movies(name,year,title,quality_score,magnet_hash,downloaded_at,size_bytes) VALUES (?1,?2,?3,?4,?5,datetime('now'),?6)",
		"Example.Movie.2024.1080p.BluRay.x264-GRUPPO", 2024, "Example Movie 2024", 1200, hash, 2<<30); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.db.Exec("INSERT INTO torrent_meta(hash,status,processed_path,updated_at) VALUES (?1,'completed',?2,datetime('now'))",
		hash, "/archive/Example Movie (2024)/Example.Movie.2024.mkv"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, _, body := webGet(t, server, "/api/config/library")
	if code != http.StatusOK {
		t.Fatalf("library -> %d: %s", code, body)
	}
	var listing struct {
		Movies []map[string]any `json:"movies"`
	}
	if err := json.Unmarshal(body, &listing); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if len(listing.Movies) != 2 {
		t.Fatalf("movies = %d, want 2: %s", len(listing.Movies), body)
	}
	var downloaded, missing map[string]any
	for _, movie := range listing.Movies {
		switch v2String(movie["name"]) {
		case "Example Movie":
			downloaded = movie
		case "Another Film":
			missing = movie
		}
	}
	if downloaded["downloaded"] != true {
		t.Fatalf("the downloaded movie is not marked: %+v", downloaded)
	}
	if got := v2String(downloaded["downloaded_path"]); got != "/archive/Example Movie (2024)/Example.Movie.2024.mkv" {
		t.Fatalf("downloaded_path = %q", got)
	}
	if _, ok := missing["downloaded"]; ok {
		t.Fatalf("a movie without a copy is marked downloaded: %+v", missing)
	}
}

// TestMovieStatusCellShowsDownloadedCheck checks the cell itself: the status
// stays readable and the check carries the path in its tooltip.
func TestMovieStatusCellShowsDownloadedCheck(t *testing.T) {
	downloaded := v2FormatCell(map[string]any{
		"enabled":         true,
		"downloaded_path": "/archive/Example Movie (2024)/Example.Movie.2024.mkv",
	}, uiColumn{Key: "enabled", Format: "movie_status"})
	if !strings.Contains(string(downloaded), "attiva") || !strings.Contains(string(downloaded), "✓") {
		t.Fatalf("downloaded cell = %q", downloaded)
	}
	if !strings.Contains(string(downloaded), `title="/archive/Example Movie (2024)/Example.Movie.2024.mkv"`) {
		t.Fatalf("the path is not in the tooltip: %q", downloaded)
	}
	if !strings.Contains(string(downloaded), `aria-label="Scaricato"`) {
		t.Fatalf("the check has no accessible name: %q", downloaded)
	}

	// Without a path the archived title is the tooltip; without a copy there is
	// no check at all.
	titleOnly := v2FormatCell(map[string]any{"enabled": false, "downloaded_title": "Example Movie 2024"}, uiColumn{Key: "enabled", Format: "movie_status"})
	if !strings.Contains(string(titleOnly), "in pausa") || !strings.Contains(string(titleOnly), `title="Example Movie 2024"`) {
		t.Fatalf("title-only cell = %q", titleOnly)
	}
	missing := v2FormatCell(map[string]any{"enabled": true}, uiColumn{Key: "enabled", Format: "movie_status"})
	if strings.Contains(string(missing), "✓") || !strings.Contains(string(missing), "attiva") {
		t.Fatalf("cell without a copy = %q", missing)
	}
}

// TestMovieDownloadedCopyMatching locks the attribution rules: punctuation and
// the year tolerance decide whether a downloaded release belongs to a monitored
// movie.
func TestMovieDownloadedCopyMatching(t *testing.T) {
	year := func(value int64) *int64 { return &value }
	movie := MovieConfig{Name: "Spider-Man", Year: "2024"}
	copies := []DownloadedMovieCopy{
		{ID: 2, Name: "Spider-Man.Across.the.Spider-Verse.2020", Year: year(2020), Title: "Spider-Man: Across the Spider-Verse"},
		{ID: 1, Name: "Spider-Man.2024.1080p", Year: year(2024), Title: "Spider-Man"},
	}
	if copy, ok := movieDownloadedCopyFor(movie, copies); !ok || copy.ID != 1 {
		t.Fatalf("match = %+v ok=%v, want the copy with the right year", copy, ok)
	}
	if _, ok := movieDownloadedCopyFor(MovieConfig{Name: "Other Film", Year: "2024"}, copies); ok {
		t.Fatal("an unrelated title matched a downloaded copy")
	}
}
