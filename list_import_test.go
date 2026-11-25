package gextto

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestCollectListImportItemsPreservesIdentity(t *testing.T) {
	value := map[string]any{
		"shows":  []any{map[string]any{"show": map[string]any{"title": "Example Show", "year": json.Number("2020"), "ids": map[string]any{"tmdb": json.Number("9999999999"), "tvdb": "77"}}}},
		"movies": []any{map[string]any{"movie": map[string]any{"name": "Example Movie", "year": "1999", "ids": map[string]any{"imdb": "tt1234567"}}}},
		"anime":  []any{map[string]any{"title": "Example Anime"}},
	}
	items := collectListImportItems("simkl", value)
	if len(items) != 3 {
		t.Fatalf("items = %+v", items)
	}
	byTitle := map[string]ListImportItem{}
	for _, item := range items {
		byTitle[item.Title] = item
	}
	show := byTitle["Example Show"]
	if show.Kind != "series" || show.Year != "2020" || show.ExternalIDs["tmdb"] != "9999999999" || show.ExternalIDs["tvdb"] != "77" {
		t.Fatalf("show = %+v", show)
	}
	movie := byTitle["Example Movie"]
	if movie.Kind != "movie" || movie.Year != "1999" || movie.ExternalIDs["imdb"] != "tt1234567" {
		t.Fatalf("movie = %+v", movie)
	}
	if !byTitle["Example Anime"].Anime {
		t.Fatalf("anime flag lost: %+v", byTitle["Example Anime"])
	}
}

func TestPlanListImportUsesIDsBeforeTitleAndYear(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Series = []SeriesConfig{{Name: "Localized Show", Year: "2020", TmdbID: "42"}}
	cfg.Movies = []MovieConfig{{Name: "Example Movie", Year: "1999"}, {Name: "Example Movie", Year: "2026"}}
	items := []ListImportItem{
		{Source: "simkl", Kind: "series", Title: "Original Show", Year: "2020", ExternalIDs: map[string]string{"tmdb": "42"}},
		{Source: "simkl", Kind: "movie", Title: "Example Movie", Year: "1999"},
		{Source: "simkl", Kind: "movie", Title: "Example Movie", Year: "2026"},
		{Source: "simkl", Kind: "movie", Title: "Example Movie", Year: "2030"},
	}
	planned := planListImport(&cfg, items)
	if planned[0].Status != "duplicate_id" || planned[0].MatchedName != "Localized Show" {
		t.Fatalf("id match = %+v", planned[0])
	}
	if planned[1].Status != "duplicate_title_year" || planned[2].Status != "duplicate_title_year" || planned[3].Status != "new" {
		t.Fatalf("movie remake planning = %+v", planned)
	}
}

func TestApplyListImportIsTransactionalAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := SaveLibrary(dir, []SeriesConfig{{Name: "Existing Show", Seasons: "1+", Enabled: true}}, []MovieConfig{{Name: "Existing Movie", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.DataDir = dir
	root := filepath.Join(dir, "library")
	items := []ListImportItem{
		{Source: "simkl", Kind: "series", Title: "Imported Show", Year: "2024", ExternalIDs: map[string]string{"tmdb": "101"}, Key: "series-1"},
		{Source: "simkl", Kind: "movie", Title: "Imported Movie", Year: "2025", ExternalIDs: map[string]string{"tvdb": "202"}, Key: "movie-1"},
	}
	options := ListImportOptions{SeriesRoot: root, SeasonSubfolders: true}
	report, err := ApplyListImport(&cfg, items, options)
	if err != nil {
		t.Fatal(err)
	}
	if report.SeriesAdded != 1 || report.MoviesAdded != 1 {
		t.Fatalf("first report = %+v", report)
	}
	loaded := DefaultConfig()
	loaded.DataDir = dir
	if err := loaded.loadConfigDB(); err != nil {
		t.Fatal(err)
	}
	if len(loaded.Series) != 2 || len(loaded.Movies) != 2 {
		t.Fatalf("library after import = %+v / %+v", loaded.Series, loaded.Movies)
	}
	var imported SeriesConfig
	for _, series := range loaded.Series {
		if series.Name == "Imported Show" {
			imported = series
		}
	}
	if imported.TmdbID != "101" || imported.ArchivePath != filepath.Join(root, "Imported Show") || !imported.SeasonSubfolders {
		t.Fatalf("imported series = %+v", imported)
	}
	for _, movie := range loaded.Movies {
		if movie.Name == "Imported Movie" && (movie.TvdbID != "202" || movie.ExternalIDs["tvdb"] != "202") {
			t.Fatalf("imported movie = %+v", movie)
		}
	}
	second, err := ApplyListImport(&cfg, items, options)
	if err != nil {
		t.Fatal(err)
	}
	if second.SeriesAdded != 0 || second.MoviesAdded != 0 || second.AlreadyExists != 2 {
		t.Fatalf("second report = %+v", second)
	}
}

func TestApplyListImportDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DataDir = dir
	items := []ListImportItem{{Source: "simkl", Kind: "series", Title: "Preview Show", Key: "one"}}
	report, err := ApplyListImport(&cfg, items, ListImportOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !report.DryRun || report.SeriesAdded != 1 {
		t.Fatalf("dry-run report = %+v", report)
	}
	if _, err := OpenConfigDB(filepath.Join(dir, "gextto_config.db")); err != nil {
		t.Fatalf("dry-run created a config database: %v", err)
	}
}

func TestApplyListImportRollsBackSeriesWhenMovieInsertFails(t *testing.T) {
	dir := t.TempDir()
	if err := SaveLibrary(dir, []SeriesConfig{{Name: "Existing", Seasons: "1+"}}, nil); err != nil {
		t.Fatal(err)
	}
	db, err := OpenConfigDB(filepath.Join(dir, "gextto_config.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_movie_import BEFORE INSERT ON movies_config BEGIN SELECT RAISE(ABORT, 'forced movie failure'); END;`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	cfg := DefaultConfig()
	cfg.DataDir = dir
	items := []ListImportItem{
		{Source: "simkl", Kind: "series", Title: "Would Be Added", Key: "series"},
		{Source: "simkl", Kind: "movie", Title: "Fails", Key: "movie"},
	}
	if _, err := ApplyListImport(&cfg, items, ListImportOptions{}); err == nil {
		t.Fatal("expected the movie insert to fail")
	}
	loaded := DefaultConfig()
	loaded.DataDir = dir
	if err := loaded.loadConfigDB(); err != nil {
		t.Fatal(err)
	}
	if len(loaded.Series) != 1 || loaded.Series[0].Name != "Existing" {
		t.Fatalf("series import was not rolled back: %+v", loaded.Series)
	}
}

func TestListImportPreviewAndApplyAPI(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveSetting(state.cfg.DataDir, "dry_run", "no"); err != nil {
		t.Fatal(err)
	}
	if err := SaveSetting(state.cfg.DataDir, "simkl_client_id", "client"); err != nil {
		t.Fatal(err)
	}
	if err := SaveSetting(state.cfg.DataDir, "simkl_access_token", "token"); err != nil {
		t.Fatal(err)
	}
	simklTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		integrationWriteJSON(t, w, `{"shows":[{"show":{"title":"Imported Show","year":"2024","ids":{"tmdb":"101"}}}],"movies":[{"movie":{"title":"Imported Movie","year":"2025","ids":{"tvdb":"202"}}}]}`)
	})
	Router(state)
	raw, status := v2InternalJSON(state, http.MethodGet, "/api/list-import/preview?source=simkl", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("preview -> %d: %s", status, raw)
	}
	var preview listImportPreviewResponse
	if err := json.Unmarshal(raw, &preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.Items) != 2 || preview.Items[0].Status != "new" || preview.Items[1].Status != "new" {
		t.Fatalf("preview = %+v", preview)
	}
	body, _ := json.Marshal(listImportApplyRequest{Source: "simkl", Items: preview.Items, Options: ListImportOptions{}})
	raw, status = v2InternalJSON(state, http.MethodPost, "/api/list-import/apply", nil, body)
	if status != http.StatusOK {
		t.Fatalf("apply -> %d: %s", status, raw)
	}
	var report ListImportReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if report.SeriesAdded != 1 || report.MoviesAdded != 1 {
		t.Fatalf("report = %+v", report)
	}
}

func TestListImportApplyRespectsGlobalDryRun(t *testing.T) {
	state := newTestAppState(t)
	Router(state)
	body, _ := json.Marshal(listImportApplyRequest{Source: "simkl", Items: []ListImportItem{{Source: "simkl", Kind: "movie", Title: "Movie", Key: "one"}}, Options: ListImportOptions{}})
	if _, status := v2InternalJSON(state, http.MethodPost, "/api/list-import/apply", nil, body); status != http.StatusConflict {
		t.Fatalf("dry-run apply -> %d, want 409", status)
	}
}

func TestApplyListImportConcurrentDifferentItemsAllSurvive(t *testing.T) {
	dir := t.TempDir()
	if err := SaveLibrary(dir, nil, nil); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.DataDir = dir
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, err := ApplyListImport(&cfg, []ListImportItem{{Source: "simkl", Kind: "movie", Title: string(rune('A' + index)), Key: string(rune('a' + index))}}, ListImportOptions{})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	loaded := DefaultConfig()
	loaded.DataDir = dir
	if err := loaded.loadConfigDB(); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, movie := range loaded.Movies {
		names = append(names, movie.Name)
	}
	if !reflect.DeepEqual(names, []string{"A", "B"}) {
		t.Fatalf("movies after concurrent imports = %v", names)
	}
}
