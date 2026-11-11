package gextto

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFolderRenameScanProposesTMDBEpisodeName(t *testing.T) {
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/tv":
			tmdbWriteJSON(t, w, `{"results":[{"id":1399,"name":"The Office","first_air_date":"2005-03-24"}]}`)
		case "/tv/1399/season/1/episode/1":
			tmdbWriteJSON(t, w, `{"name":"Pilot"}`)
		default:
			t.Fatalf("unexpected TMDB path %q", r.URL.Path)
		}
	})

	root := t.TempDir()
	source := filepath.Join(root, "The.Office.S01E01.1080p.mkv")
	if err := os.WriteFile(source, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	key := "test-key"
	cfg := DefaultConfig()
	cfg.TmdbAPIKey = &key
	items, err := folderRenameScan(t.Context(), root, &cfg)
	if err != nil {
		t.Fatalf("folderRenameScan: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	item := items[0]
	if item.Kind != "series" || item.Status != "proposta" {
		t.Fatalf("item classification = %#v", item)
	}
	if !strings.Contains(filepath.Base(item.Target), "The Office - S01E01 - Pilot") {
		t.Fatalf("target = %q", item.Target)
	}
}

// TestFolderRenameScanUsesTheConfiguredTemplate: the review tool must build the
// proposed name with the operator's rename template (quality block included),
// like the import/rename path, not with its own fixed minimal name.
func TestFolderRenameScanUsesTheConfiguredTemplate(t *testing.T) {
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/tv":
			tmdbWriteJSON(t, w, `{"results":[{"id":1399,"name":"The Office","first_air_date":"2005-03-24"}]}`)
		case "/tv/1399/season/1/episode/1":
			tmdbWriteJSON(t, w, `{"name":"Pilot"}`)
		default:
			t.Fatalf("unexpected TMDB path %q", r.URL.Path)
		}
	})

	root := t.TempDir()
	source := filepath.Join(root, "The.Office.S01E01.1080p.WEB-DL.H265.IT+EN.mkv")
	if err := os.WriteFile(source, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	key := "test-key"
	cfg := DefaultConfig()
	cfg.TmdbAPIKey = &key
	cfg.RenameFormat = "custom"
	cfg.RenameTemplate = "{Serie} - {Stagione}{Episodio} - {Titolo} - [{Source}][{Risoluzione}][{VideoCodec}][{HDR}][{Audio}][{Lingue}]"
	items, err := folderRenameScan(t.Context(), root, &cfg)
	if err != nil {
		t.Fatalf("folderRenameScan: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	want := "The Office - S01E01 - Pilot - [WEB-DL][1080p][h265][IT+EN].mkv"
	if got := filepath.Base(items[0].Target); got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
}

// TestFolderRenameScanGivesMoviesTheMovieTemplate: a movie in the reviewed
// folder takes the operator's movie template, with the quality the file name
// carries, not a bare "Title (Year)" that drops every tag.
func TestFolderRenameScanGivesMoviesTheMovieTemplate(t *testing.T) {
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/movie" {
			t.Fatalf("unexpected TMDB path %q", r.URL.Path)
		}
		tmdbWriteJSON(t, w, `{"results":[{"id":7,"title":"Example Movie","release_date":"1999-05-01"}]}`)
	})

	root := t.TempDir()
	source := filepath.Join(root, "Example.Movie.1999.2160p.BluRay.HDR10.H265.ITA-ENG-GRP.mkv")
	if err := os.WriteFile(source, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	key := "test-key"
	cfg := DefaultConfig()
	cfg.TmdbAPIKey = &key
	cfg.RenameFormat = "custom"
	cfg.RenameMovieTemplate = "{Titolo} ({Anno}) - [{Source}][{Risoluzione}][{VideoCodec}][{HDR}][{Lingue}]"
	items, err := folderRenameScan(t.Context(), root, &cfg)
	if err != nil {
		t.Fatalf("folderRenameScan: %v", err)
	}
	if len(items) != 1 || items[0].Kind != "movie" {
		t.Fatalf("items = %#v", items)
	}
	want := "Example Movie (1999) - [BluRay][2160p][h265][HDR10][IT+EN].mkv"
	if got := filepath.Base(items[0].Target); got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
}

// TestFolderRenameScanPicksTheMovieOfTheFileYear: two films share the title;
// the one released in the year the file name carries is proposed, and among
// partial matches the closer title beats a long special that contains it.
func TestFolderRenameScanPicksTheMovieOfTheFileYear(t *testing.T) {
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("query") {
		case "Example Movie":
			tmdbWriteJSON(t, w, `{"results":[{"id":1,"title":"Example Movie","release_date":"1969-01-01"},{"id":2,"title":"Example Movie","release_date":"2026-07-01"}]}`)
		default:
			tmdbWriteJSON(t, w, `{"results":[{"id":3,"title":"Studio Insider: Another Film and Friend - A Look Behind the Scenes","release_date":"2026-01-01"},{"id":4,"title":"Saga: Another Film and Friend","release_date":"2026-05-01"}]}`)
		}
	})
	root := t.TempDir()
	for _, name := range []string{"Example Movie (2026) [2160p].mkv", "Another Film and Friend (2026) 2160p WEB-DL.mkv"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("video"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	key := "test-key"
	cfg := DefaultConfig()
	cfg.TmdbAPIKey = &key
	items, err := folderRenameScan(t.Context(), root, &cfg)
	if err != nil {
		t.Fatalf("folderRenameScan: %v", err)
	}
	got := map[string]string{}
	for _, item := range items {
		got[filepath.Base(item.Source)] = filepath.Base(item.Target)
	}
	if want := "Example Movie (2026).mkv"; got["Example Movie (2026) [2160p].mkv"] != want {
		t.Fatalf("same-titled films: target = %q, want %q", got["Example Movie (2026) [2160p].mkv"], want)
	}
	if want := "Saga Another Film and Friend (2026).mkv"; got["Another Film and Friend (2026) 2160p WEB-DL.mkv"] != want {
		t.Fatalf("partial titles: target = %q, want %q", got["Another Film and Friend (2026) 2160p WEB-DL.mkv"], want)
	}
}

// TestFolderRenameScanTrustsTheFollowedFilmTMDBID: a file of a followed film
// takes the TMDB entry chosen when the film was added, even when the file name
// has no year and a same-titled film comes first in the search.
func TestFolderRenameScanTrustsTheFollowedFilmTMDBID(t *testing.T) {
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		tmdbWriteJSON(t, w, `{"results":[{"id":1,"title":"Example Movie","release_date":"1969-01-01"},{"id":2,"title":"Example Movie","release_date":"2026-07-01"}]}`)
	})
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Example.Movie.2160p.WEB-DL.mkv"), []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	key := "test-key"
	cfg := DefaultConfig()
	cfg.TmdbAPIKey = &key
	cfg.Movies = []MovieConfig{{Name: "Example Movie", Year: "2026", TmdbID: "2", Enabled: true}}
	items, err := folderRenameScan(t.Context(), root, &cfg)
	if err != nil {
		t.Fatalf("folderRenameScan: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %#v", items)
	}
	if got, want := filepath.Base(items[0].Target), "Example Movie (2026).mkv"; got != want || items[0].Status != "proposta" {
		t.Fatalf("target = %q (%s), want %q proposed", got, items[0].Status, want)
	}
}
