package gextto

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func TestTitlePriorityPersistsForSeriesAndMovies(t *testing.T) {
	dir := t.TempDir()
	if err := SaveLibrary(dir,
		[]SeriesConfig{{Name: "High Show", Seasons: "1+", Enabled: true, Priority: titlePriorityHigh}},
		[]MovieConfig{{Name: "Low Movie", Year: "2026", Enabled: true, Priority: titlePriorityLow}},
	); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.DataDir = dir
	if err := cfg.loadConfigDB(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Series) != 1 || cfg.Series[0].Priority != titlePriorityHigh {
		t.Fatalf("series priority = %+v", cfg.Series)
	}
	if len(cfg.Movies) != 1 || cfg.Movies[0].Priority != titlePriorityLow {
		t.Fatalf("movie priority = %+v", cfg.Movies)
	}
}

func TestTitlePriorityRejectsUnknownValues(t *testing.T) {
	state := newTestAppState(t)
	Router(state)
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{{Name: "Show", Seasons: "1+", Enabled: true}}, []MovieConfig{{Name: "Movie", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	seriesBody, _ := json.Marshal(map[string]string{"priority": "urgent"})
	if _, code := v2InternalJSON(state, http.MethodPost, "/api/series/Show", nil, seriesBody); code != http.StatusBadRequest {
		t.Fatalf("series priority -> %d, want 400", code)
	}
	movieBody, _ := json.Marshal(map[string]any{"name": "Movie", "priority": "urgent"})
	if _, code := v2InternalJSON(state, http.MethodPost, "/api/movies/1", nil, movieBody); code != http.StatusBadRequest {
		t.Fatalf("movie priority -> %d, want 400", code)
	}
}

func TestV2SeriesPriorityFormSaves(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{{Name: "Show", Seasons: "1+", Enabled: true}}, nil); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	code, _ := v2Request(t, server, http.MethodPost, "/series/save", url.Values{
		"name": {"Show"}, "original_name": {"Show"}, "seasons": {"1+"}, "priority": {"high"},
	})
	if code != http.StatusNoContent {
		t.Fatalf("series save -> %d", code)
	}
	if got := latestConfig(state).Series[0].Priority; got != titlePriorityHigh {
		t.Fatalf("priority = %q, want high", got)
	}
}

func TestV2MoviePriorityFormSaves(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveLibrary(state.cfg.DataDir, nil, []MovieConfig{{ID: 1, Name: "Movie", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	postSectionForm(t, server, url.Values{
		"view": {"movies"}, "path": {"/api/movies/1"}, "redirect": {"/?view=movies&movie=1"},
		"name": {"Movie"}, "priority": {"low"}, "_v2_kind_priority": {"select"},
	})
	if got := latestConfig(state).Movies[0].Priority; got != titlePriorityLow {
		t.Fatalf("priority = %q, want low", got)
	}
}

func TestSortReleasesByTitlePriority(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Series = []SeriesConfig{
		{Name: "Low Show", Seasons: "1+", Enabled: true, Priority: titlePriorityLow},
		{Name: "High Show", Seasons: "1+", Enabled: true, Priority: titlePriorityHigh},
		{Name: "Normal Show", Seasons: "1+", Enabled: true, Priority: titlePriorityNormal},
	}
	low, high, normal := "Low Show", "High Show", "Normal Show"
	season := int64(1)
	releases := []models.Release{
		{Title: "low", Kind: "series", Series: &low, Season: &season},
		{Title: "normal", Kind: "series", Series: &normal, Season: &season},
		{Title: "high", Kind: "series", Series: &high, Season: &season},
	}
	sortReleasesByTitlePriority(&cfg, releases, nil)
	got := []string{releases[0].Title, releases[1].Title, releases[2].Title}
	if want := []string{"high", "normal", "low"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestSortSeriesGapsByTitlePriority(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Series = []SeriesConfig{
		{Name: "Low Show", Seasons: "1+", Enabled: true, Priority: titlePriorityLow},
		{Name: "High Show", Seasons: "1+", Enabled: true, Priority: titlePriorityHigh},
	}
	gaps := []SeriesGap{{Series: "Low Show", Season: 1, Episode: 1}, {Series: "High Show", Season: 1, Episode: 2}}
	sortSeriesGapsByTitlePriority(&cfg, gaps)
	if gaps[0].Series != "High Show" {
		t.Fatalf("gaps = %+v, want high-priority series first", gaps)
	}
}

func TestMoviePriorityColumnIsAddedToAnOlderConfig(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenConfigDB(filepath.Join(dir, "gextto_config.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE movies_config (id INTEGER PRIMARY KEY, name TEXT NOT NULL, year TEXT DEFAULT '', quality TEXT DEFAULT '', language TEXT DEFAULT '', enabled INTEGER DEFAULT 1, subtitle TEXT DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if err := SaveLibrary(dir, nil, []MovieConfig{{Name: "Movie", Enabled: true, Priority: titlePriorityHigh}}); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.DataDir = dir
	if err := cfg.loadConfigDB(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Movies) != 1 || cfg.Movies[0].Priority != titlePriorityHigh {
		t.Fatalf("movies after migration = %+v", cfg.Movies)
	}
	if strings.TrimSpace(cfg.Movies[0].Name) != "Movie" {
		t.Fatalf("movie name = %q", cfg.Movies[0].Name)
	}
}
