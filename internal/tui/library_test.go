package tui

import (
	"strings"
	"testing"
)

func strPtr(value string) *string { return &value }

func sampleSeriesDetail() SeriesDetail {
	return SeriesDetail{
		Series: SeriesConfig{Name: "Show", Seasons: "1+", Quality: "1080p", Language: "ita", TvdbID: "77", Aliases: []string{"Lo Show"}, Enabled: true, IgnoredSeasons: []int64{3}},
		Episodes: []Episode{
			{Season: 1, Episode: 1, Title: "Pilot", Status: "downloaded", ArchivePath: strPtr("/nas/Show/S01E01.mkv"), SizeBytes: 1 << 30, QualityScore: 800, MagnetLink: strPtr("magnet:?xt=urn:btih:one")},
			{Season: 1, Episode: 2, Status: "missing", AirDate: "2020-01-08"},
			{Season: 2, Episode: 1, Status: "missing", AirDate: "2999-01-01"},
			{Season: 2, Episode: 2, Status: "missing", Ignored: true},
		},
		Metadata: []SeasonCount{{Season: 1, Count: 2}, {Season: 2, Count: 2}, {Season: 4, Count: 8}},
	}
}

func openSampleSeries(t *testing.T) *Model {
	t.Helper()
	m := NewModel(NewTranslator("it"))
	m.Tab = TabLibrary
	m.SetLibraryData([]SeriesLibraryItem{{Name: "Show", Seasons: "1+", Enabled: true, EpisodesTotal: 4, EpisodesDownloaded: 1}}, nil)
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionLoadSeries || action.Text != "Show" {
		t.Fatalf("Enter should load the series, got %+v", action)
	}
	m.SetSeriesDetail(sampleSeriesDetail())
	return m
}

func TestSeriesDetailNavigationAndEpisodeActions(t *testing.T) {
	m := openSampleSeries(t)
	view := m.SeriesView
	if view.Season != 1 || len(view.SeasonEpisodes()) != 2 {
		t.Fatalf("detail should open on season 1, got season %d", view.Season)
	}
	if seasons := view.Seasons(); len(seasons) != 4 {
		t.Fatalf("seasons = %v, want 1 2 3 4", seasons)
	}
	// The downloaded pilot is selected: Enter lists its archived sources.
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionEpisodeSources || action.Season != 1 || action.Episode != 1 {
		t.Fatalf("Enter = %+v", action)
	}
	if action := m.Update(runeKey('y')); action.Kind != ActionCopy || action.Text != "magnet:?xt=urn:btih:one" {
		t.Fatalf("y = %+v", action)
	}
	m.Update(kindKey(KeyDown))
	if action := m.Update(runeKey('s')); action.Kind != ActionEpisodeSearch || action.Episode != 2 {
		t.Fatalf("s should search the episode online, got %+v", action)
	}
	if action := m.Update(runeKey('i')); action.Kind != ActionEpisodeIgnore || !action.Flag {
		t.Fatalf("i should ignore the episode, got %+v", action)
	}
	m.Update(runeKey('R'))
	if action := m.Update(runeKey('s')); action.Kind != ActionEpisodeRedownload || action.Episode != 2 {
		t.Fatalf("R+s should redownload, got %+v", action)
	}

	// Season 2: switching it off; season 3 is off and can be switched on.
	m.Update(kindKey(KeyRight))
	if view.Season != 2 || view.Selected != 0 {
		t.Fatalf("→ should move to season 2, got %d", view.Season)
	}
	if action := m.Update(runeKey(' ')); action.Kind != ActionToggleSeason || action.Season != 2 || action.Flag {
		t.Fatalf("Space on a monitored season should switch it off, got %+v", action)
	}
	m.Update(kindKey(KeyRight))
	if action := m.Update(runeKey(' ')); action.Kind != ActionToggleSeason || action.Season != 3 || !action.Flag {
		t.Fatalf("Space on an ignored season should switch it on, got %+v", action)
	}
	// Season 4 is known to TMDB but outside the "seasons" field.
	m.Update(kindKey(KeyRight))
	if action := m.Update(runeKey(' ')); action.Kind != ActionNone || !strings.Contains(m.Message, "4") {
		t.Fatalf("an excluded season cannot be toggled: %+v %q", action, m.Message)
	}
	// Digits do not switch the library list behind the detail.
	m.Update(runeKey('2'))
	if m.Library != LibrarySeries {
		t.Fatal("a digit changed the library list behind the detail")
	}
	if action := m.Update(runeKey('m')); action.Kind != ActionSeriesSearchMissing || action.Text != "Show" {
		t.Fatalf("m = %+v", action)
	}
	if action := m.Update(runeKey('n')); action.Kind != ActionRenamePreview {
		t.Fatalf("n = %+v", action)
	}
	if action := m.Update(runeKey('p')); action.Kind != ActionSaveSeries || action.Fields["enabled"] != false {
		t.Fatalf("p = %+v", action)
	}
	m.Update(kindKey(KeyEsc))
	if m.SeriesView != nil {
		t.Fatal("Esc should close the series detail")
	}
}

func TestSeriesReloadKeepsSeasonAndEpisode(t *testing.T) {
	m := openSampleSeries(t)
	m.Update(kindKey(KeyRight))
	detail := sampleSeriesDetail()
	detail.Episodes = append([]Episode{{Season: 2, Episode: 0, Status: "missing"}}, detail.Episodes...)
	m.SetSeriesDetail(detail)
	if m.SeriesView.Season != 2 {
		t.Fatalf("reload moved to season %d", m.SeriesView.Season)
	}
	if episode := m.SeriesView.selectedEpisode(); episode == nil || episode.Episode != 1 {
		t.Fatalf("reload lost the selected episode: %+v", episode)
	}
}

func TestSeriesViewRender(t *testing.T) {
	m := openSampleSeries(t)
	screen := m.Render(120, 24)
	for _, want := range []string{"Show · attiva · 1/3 ep.", "[S1 1/2]", "S3 off", "S4 -", "✓ E01", "✗ E02"} {
		if !lineContains(screen, want) {
			t.Fatalf("%q missing from the series view: %+v", want, screen.Lines)
		}
	}
	m.Update(kindKey(KeyRight))
	screen = m.Render(120, 24)
	if !lineContains(screen, "· E01 2999-01-01 in uscita") || !lineContains(screen, "- E02") {
		t.Fatalf("season 2 rows: %+v", screen.Lines)
	}
}

func TestSeriesEditFormSendsOnlyChangedFields(t *testing.T) {
	m := openSampleSeries(t)
	m.Update(runeKey('e'))
	if m.Form == nil || m.Form.Kind != FormSeriesEdit {
		t.Fatal("e should open the edit form")
	}
	// Quality is the second field.
	m.Update(kindKey(KeyDown))
	m.Update(kindKey(KeyEnter))
	m.Update(kindKey(KeyCtrlU))
	typeText(m, "720p")
	m.Update(kindKey(KeyEnter))
	// Aliases.
	for m.Form.Fields[m.Form.Selected].Key != "aliases" {
		m.Update(kindKey(KeyDown))
	}
	m.Update(kindKey(KeyEnter))
	typeText(m, ", Show US")
	m.Update(kindKey(KeyEnter))
	m.Update(kindKey(KeyEnd))
	m.Update(runeKey(' '))
	if screen := m.Render(120, 24); !lineContains(screen, "*Qualità") {
		t.Fatalf("changed field not marked: %+v", screen.Lines)
	}
	action := m.Update(runeKey('s'))
	if action.Kind != ActionSaveSeries || action.Text != "Show" || len(action.Fields) != 3 {
		t.Fatalf("save = %+v", action)
	}
	if action.Fields["quality"] != "720p" || action.Fields["disable_upgrades"] != true {
		t.Fatalf("fields = %+v", action.Fields)
	}
	if aliases, _ := action.Fields["aliases"].([]string); len(aliases) != 2 || aliases[1] != "Show US" {
		t.Fatalf("aliases = %#v", action.Fields["aliases"])
	}
	if m.Form != nil {
		t.Fatal("the form should close after saving")
	}
	// Saving an unchanged form does nothing.
	m.Update(runeKey('e'))
	if action := m.Update(runeKey('s')); action.Kind != ActionNone || m.Message != m.Tr.T("msg.nochanges") {
		t.Fatalf("unchanged save = %+v %q", action, m.Message)
	}
}

func TestEditFromSeriesListOpensFormOnceLoaded(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabLibrary
	m.SetLibraryData([]SeriesLibraryItem{{Name: "Show", Enabled: true}}, nil)
	if action := m.Update(runeKey('e')); action.Kind != ActionLoadSeries {
		t.Fatalf("e on the list should load the series first, got %+v", action)
	}
	if m.Form != nil {
		t.Fatal("the form must wait for the complete series")
	}
	m.SetSeriesDetail(sampleSeriesDetail())
	if m.Form == nil || m.Form.value("tvdb_id") != "77" {
		t.Fatalf("form after load = %+v", m.Form)
	}
}

func TestAddSeriesFromTmdb(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabLibrary
	m.Update(runeKey('a'))
	if m.Prompt == nil || m.Prompt.Kind != PromptTmdbSeries {
		t.Fatal("a in the series list should ask what to search on TMDB")
	}
	typeText(m, "the show")
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionTmdbSearch || action.Domain != "series" || action.Text != "the show" {
		t.Fatalf("TMDB search = %+v", action)
	}
	m.SetPickerResults(SearchTmdbSeries, "the show", []map[string]any{
		{"id": 10.0, "name": "Old Show", "first_air_date": "2001-01-01", "in_library": true},
		{"id": 42.0, "name": "The Show", "first_air_date": "2019-05-01", "vote_average": 7.5, "overview": "A show."},
	})
	if screen := m.Render(120, 24); !lineContains(screen, "The Show (2019) · voto 7.5") || !lineContains(screen, "già in libreria") {
		t.Fatalf("TMDB results: %+v", screen.Lines)
	}
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionNone || m.Message != m.Tr.T("msg.inlibrary") || m.Form != nil {
		t.Fatalf("a title already in the library cannot be added: %+v", action)
	}
	m.Update(kindKey(KeyDown))
	m.Update(kindKey(KeyEnter))
	if m.Form == nil || m.Form.Kind != FormSeriesAdd || m.Overlay != OverlayNone {
		t.Fatal("Enter should open the add form")
	}
	action := m.Update(runeKey('s'))
	if action.Kind != ActionAddToLibrary {
		t.Fatalf("save = %+v", action)
	}
	for key, want := range map[string]string{"kind": "series", "name": "The Show", "tmdb_id": "42", "seasons": "1+", "language": "ita"} {
		if action.Fields[key] != want {
			t.Fatalf("%s = %v, want %s", key, action.Fields[key], want)
		}
	}
}

func TestMovieListAndDetailActions(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabLibrary
	m.Update(runeKey('2'))
	movie := MovieConfig{ID: 7, Name: "Film", Year: "2020", Quality: "1080p", Language: "ita", Enabled: true, TmdbID: "99", LanguageRequirements: "ita"}
	m.SetLibraryData(nil, []MovieConfig{movie})
	if action := m.Update(runeKey('p')); action.Kind != ActionSaveMovie || action.Movie.Enabled || action.Movie.LanguageRequirements != "ita" || action.Movie.TmdbID != "99" {
		t.Fatalf("p should save the whole movie disabled, got %+v", action.Movie)
	}
	m.Update(runeKey('d'))
	if action := m.Update(runeKey('s')); action.Kind != ActionDeleteMovie || action.ID != 7 {
		t.Fatalf("d+s = %+v", action)
	}
	if action := m.Update(runeKey('m')); action.Kind != ActionMovieSearch || action.ID != 7 {
		t.Fatalf("m = %+v", action)
	}
	m.Update(runeKey('a'))
	if m.Prompt == nil || m.Prompt.Kind != PromptTmdbMovie {
		t.Fatal("a in the movie list should search movies on TMDB")
	}
	m.Update(kindKey(KeyEsc))

	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionLoadMovie || action.ID != 7 {
		t.Fatalf("Enter = %+v", action)
	}
	m.SetMovieDetail(MovieDetail{Movie: movie, Metadata: map[string]any{"overview": "Plot."}, Matches: []ArchiveMatch{{Title: "Film.2020.1080p", Magnet: "magnet:?xt=urn:btih:film", Source: "feed"}}})
	if screen := m.Render(120, 24); !lineContains(screen, "Film (2020) · attiva") || !lineContains(screen, "Film.2020.1080p") {
		t.Fatalf("movie view: %+v", screen.Lines)
	}
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionAddMagnet || action.Text != "magnet:?xt=urn:btih:film" {
		t.Fatalf("Enter on a match = %+v", action)
	}
	m.Update(runeKey('e'))
	m.Update(kindKey(KeyDown))
	m.Update(kindKey(KeyEnter))
	m.Update(kindKey(KeyCtrlU))
	typeText(m, "2021")
	m.Update(kindKey(KeyEnter))
	if action := m.Update(runeKey('s')); action.Kind != ActionSaveMovie || action.Movie.Year != "2021" || action.Movie.LanguageRequirements != "ita" {
		t.Fatalf("movie save = %+v", action.Movie)
	}
	m.Update(kindKey(KeyEsc))
	if m.MovieView != nil {
		t.Fatal("Esc should close the movie detail")
	}
}

func TestMissingTabOpensSearchesAndIgnores(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabMissing
	m.Missing = []Gap{{Series: "Show", Season: 1, Episode: 2}}
	if action := m.Update(runeKey('s')); action.Kind != ActionEpisodeSearch || action.Text != "Show" || action.Episode != 2 {
		t.Fatalf("s = %+v", action)
	}
	m.Update(runeKey('i'))
	if action := m.Update(runeKey('s')); action.Kind != ActionEpisodeIgnore || !action.Flag {
		t.Fatalf("i+s = %+v", action)
	}
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionLoadSeries || m.Tab != TabLibrary {
		t.Fatalf("Enter should open the series, got %+v on tab %d", action, m.Tab)
	}
	m.SetSeriesDetail(sampleSeriesDetail())
	if episode := m.SeriesView.selectedEpisode(); episode == nil || episode.Season != 1 || episode.Episode != 2 {
		t.Fatalf("the missing episode should be selected, got %+v", episode)
	}
}

func TestFlattenReleases(t *testing.T) {
	flat := flattenReleases([]map[string]any{
		{"release": map[string]any{"title": "Show S01E02", "magnet": "magnet:x"}, "origin": "archive", "score": 900.0, "season": 1.0},
		{"title": "plain"},
	})
	if len(flat) != 2 || flat[0]["title"] != "Show S01E02" || flat[0]["origin"] != "archive" || flat[0]["score"] != 900.0 || flat[0]["season"] != 1.0 || flat[1]["title"] != "plain" {
		t.Fatalf("flatten = %+v", flat)
	}
}
