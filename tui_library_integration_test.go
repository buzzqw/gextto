package gextto

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/buzzqw/gextto/internal/tui"
)

// TestTUILibraryClientAgainstTheRouter drives the TUI client against the real
// API, so a payload or route the TUI relies on cannot drift unnoticed.
func TestTUILibraryClientAgainstTheRouter(t *testing.T) {
	state := newTestAppState(t)
	series := SeriesConfig{Name: "Show", Seasons: "1+", Quality: "1080p", Language: "ita", TvdbID: "77", DisableUpgrades: true, Enabled: true}
	movie := MovieConfig{ID: 7, Name: "Film", Year: "2020", Language: "ita", Enabled: true, LanguageRequirements: `[{"language":"ita","required":true}]`}
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{series}, []MovieConfig{movie}); err != nil {
		t.Fatal(err)
	}
	if err := state.db.SaveSeriesMetadata("Show", [][2]int64{{1, 3}, {2, 2}}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	client := tui.NewClient(server.URL)
	ctx := context.Background()

	list, movies, err := client.Library(ctx)
	if err != nil || len(list) != 1 || list[0].EpisodesTotal != 5 || len(movies) != 1 || movies[0].ID != 7 {
		t.Fatalf("Library = %+v %+v %v", list, movies, err)
	}
	detail, err := client.SeriesDetail(ctx, "Show")
	if err != nil || detail.Series.TvdbID != "77" || len(detail.Episodes) != 5 || len(detail.Metadata) != 2 {
		t.Fatalf("SeriesDetail = %+v %v", detail, err)
	}

	if err := client.UpdateSeries(ctx, "Show", map[string]any{"quality": "720p", "aliases": []string{"Lo Show"}}); err != nil {
		t.Fatal(err)
	}
	if err := client.ToggleSeason(ctx, "Show", 2, false); err != nil {
		t.Fatal(err)
	}
	if err := client.IgnoreEpisode(ctx, "Show", 1, 2, true); err != nil {
		t.Fatal(err)
	}
	detail, err = client.SeriesDetail(ctx, "Lo Show")
	if err != nil {
		t.Fatal(err)
	}
	got := detail.Series
	if got.Quality != "720p" || got.TvdbID != "77" || !got.DisableUpgrades || len(got.IgnoredSeasons) != 1 || got.IgnoredSeasons[0] != 2 {
		t.Fatalf("series after edits = %+v", got)
	}
	ignored := false
	for _, episode := range detail.Episodes {
		if episode.Season == 2 {
			t.Fatalf("episodes of the switched-off season are listed: %+v", episode)
		}
		if episode.Season == 1 && episode.Episode == 2 {
			ignored = episode.Ignored
		}
	}
	if !ignored {
		t.Fatal("S01E02 should be ignored")
	}
	if items, err := client.EpisodeSources(ctx, "Show", 1, 1); err != nil || items == nil {
		t.Fatalf("EpisodeSources = %v %v", items, err)
	}

	movieDetail, err := client.MovieDetail(ctx, 7)
	if err != nil || movieDetail.Movie.Name != "Film" {
		t.Fatalf("MovieDetail = %+v %v", movieDetail, err)
	}
	edited := movieDetail.Movie
	edited.Year = "2021"
	edited.Enabled = false
	if err := client.UpdateMovie(ctx, edited); err != nil {
		t.Fatal(err)
	}
	saved := latestConfig(state).Movies[0]
	if saved.Year != "2021" || saved.Enabled || saved.LanguageRequirements != movie.LanguageRequirements {
		t.Fatalf("movie after edit = %+v", saved)
	}

	if err := client.DeleteMovie(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteSeries(ctx, "Show"); err != nil {
		t.Fatal(err)
	}
	if cfg := latestConfig(state); len(cfg.Series) != 0 || len(cfg.Movies) != 0 {
		t.Fatalf("library after deletes = %+v %+v", cfg.Series, cfg.Movies)
	}
}
