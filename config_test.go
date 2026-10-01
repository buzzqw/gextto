package gextto

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

func TestResolveArchivePathAutoDetectsSeriesFolder(t *testing.T) {
	base := filepath.Join(t.TempDir(), "archive-root")
	if err := os.MkdirAll(filepath.Join(base, "Example Show"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := base
	cfg := DefaultConfig()
	cfg.ArchiveRoot = &root

	series := SeriesConfig{Name: "Example Show"}
	got := cfg.ResolveArchivePath(&series)
	want := filepath.Join(base, "Example Show")
	if got == nil || *got != want {
		t.Fatalf("expected %q, got %v", want, got)
	}

	// `archive_path` configurato ha la priorità sull'auto-detect.
	configured := SeriesConfig{Name: "Example Show", ArchivePath: "/custom/path"}
	got = cfg.ResolveArchivePath(&configured)
	if got == nil || *got != "/custom/path" {
		t.Fatalf("expected /custom/path, got %v", got)
	}

	// Nessuna cartella corrispondente: nessun risultato.
	other := SeriesConfig{Name: "Altra Serie"}
	if cfg.ResolveArchivePath(&other) != nil {
		t.Fatal("expected no archive path for unmatched series")
	}
}

func TestPrepareDirsDoesNotCreateTheImportSource(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig()
	cfg.DataDir = filepath.Join(root, "data")
	cfg.StateDir = filepath.Join(cfg.DataDir, "state")
	cfg.LibtorrentDir = filepath.Join(cfg.DataDir, "downloads")
	temp := filepath.Join(cfg.DataDir, "incomplete")
	cfg.LibtorrentTempDir = &temp
	trash := filepath.Join(cfg.DataDir, "trash")
	cfg.TrashPath = &trash
	cfg.ImportSourceDir = filepath.Join(root, "external-legacy")

	if err := cfg.PrepareDirs(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(cfg.DataDir); err != nil || !info.IsDir() {
		t.Fatal("data dir was not created")
	}
	if _, err := os.Stat(cfg.ImportSourceDir); err == nil {
		t.Fatal("import source dir must not be created")
	}
}

func TestNormalizesLanguageCodes(t *testing.T) {
	cases := map[string]string{
		"IT":       "ita",
		"ITA":      "ita",
		"italian":  "ita",
		"English":  "eng",
		"eng":      "eng",
		" custom ": "custom",
	}
	for input, want := range cases {
		if got := normalizeLanguageCode(input); got != want {
			t.Fatalf("normalizeLanguageCode(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLanguageRequirementsSkipNonRequiredAndParseCSV(t *testing.T) {
	jsonValue := `[{"language":"ita","required":true},{"language":"eng","required":false},{"language":"fra"}]`
	if got, want := parseLanguageRequirements(jsonValue), []string{"ita", "fra"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("json requirements = %v, want %v", got, want)
	}
	if got, want := parseLanguageRequirements("ita, eng"), []string{"ita", "eng"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("csv requirements = %v, want %v", got, want)
	}
	if got, want := parseLanguageRequirements("EN+ita"), []string{"eng", "ita"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("plus requirements = %v, want %v", got, want)
	}
	if got := parseLanguageRequirements("   "); len(got) != 0 {
		t.Fatalf("blank requirements = %v, want empty", got)
	}
}

func TestPreservesMovieIDsWhenClientSendsZero(t *testing.T) {
	dir := t.TempDir()
	movie := func(id int64) MovieConfig {
		return MovieConfig{ID: id, Name: "Afterburn", Year: "2025"}
	}
	readID := func() int64 {
		conn, err := OpenConfigDB(filepath.Join(dir, "gextto_config.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		var id int64
		if err := conn.QueryRow("SELECT id FROM movies_config WHERE name='Afterburn'").Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	if err := SaveLibrary(dir, []SeriesConfig{}, []MovieConfig{movie(0)}); err != nil {
		t.Fatal(err)
	}
	assigned := readID()
	if assigned <= 0 {
		t.Fatalf("expected an assigned id, got %d", assigned)
	}
	// Un secondo salvataggio con `id: 0` (come farebbe la UI) non deve
	// riassegnare un id diverso.
	if err := SaveLibrary(dir, []SeriesConfig{}, []MovieConfig{movie(0)}); err != nil {
		t.Fatal(err)
	}
	if got := readID(); got != assigned {
		t.Fatalf("id changed from %d to %d", assigned, got)
	}
	// Un id esplicito valido viene mantenuto.
	if err := SaveLibrary(dir, []SeriesConfig{}, []MovieConfig{movie(12345)}); err != nil {
		t.Fatal(err)
	}
	if got := readID(); got != 12345 {
		t.Fatalf("explicit id not preserved: %d", got)
	}
}

func TestLoadConfigOrdersMoviesAlphabetically(t *testing.T) {
	dir := t.TempDir()
	if err := SaveLibrary(dir, []SeriesConfig{}, []MovieConfig{
		{Name: "Zulu", Year: "2024"},
		{Name: "alpha", Year: "2025"},
		{Name: "Beta", Year: "2023"},
	}); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "gextto.json")
	if err := os.WriteFile(configPath, []byte(`{"data_dir":"`+dir+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(cfg.Movies))
	for _, movie := range cfg.Movies {
		got = append(got, movie.Name)
	}
	want := []string{"alpha", "Beta", "Zulu"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("movie order = %v, want %v", got, want)
	}
}

func TestMatchesEnabledSeriesAliasAndRespectsIgnoredSeason(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Series = append(cfg.Series, SeriesConfig{
		Name:           "Grey's Anatomy",
		Aliases:        []string{"Greys Anatomy"},
		Enabled:        true,
		IgnoredSeasons: []int64{14},
	})
	season15 := int64(15)
	if got := cfg.FindSeriesMatch("Greys.Anatomy", &season15); got == nil || got.Name != "Grey's Anatomy" {
		t.Fatalf("expected alias match, got %v", got)
	}
	season14 := int64(14)
	if cfg.FindSeriesMatch("Greys Anatomy", &season14) != nil {
		t.Fatal("ignored season must not match")
	}
}

func TestMatchesOnlyConfiguredSeasonRanges(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Series = append(cfg.Series, SeriesConfig{
		Name:    "Example",
		Seasons: "1,3-4,7+",
		Enabled: true,
	})
	for _, season := range []int64{1, 4, 8} {
		value := season
		if cfg.FindSeriesMatch("Example", &value) == nil {
			t.Fatalf("season %d should match", season)
		}
	}
	season2 := int64(2)
	if cfg.FindSeriesMatch("Example", &season2) != nil {
		t.Fatal("season 2 should not match")
	}
}

func TestPrefersExactSeriesNameBeforeYearSuffixMatching(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Series = append(cfg.Series,
		SeriesConfig{Name: "Scrubs", Enabled: true},
		SeriesConfig{Name: "Scrubs 2026", Enabled: true},
	)
	season := int64(1)
	got := cfg.FindSeriesMatch("Scrubs 2026", &season)
	if got == nil || got.Name != "Scrubs 2026" {
		t.Fatalf("expected exact match Scrubs 2026, got %v", got)
	}
}

func TestContentFilterExcludesNotRequires(t *testing.T) {
	filters := []string{"[non-latino]"}
	if titleIsContentFiltered("Example S01E01 1080p ITA", filters) {
		t.Fatal("latin title must not be filtered")
	}
	if !titleIsContentFiltered("Пример сериала 1080p", filters) {
		t.Fatal("cyrillic title must be filtered")
	}
	words := []string{"anime"}
	if !titleIsContentFiltered("Some.Anime.S01E01", words) {
		t.Fatal("anime token must be filtered")
	}
	if titleIsContentFiltered("Some.Show.S01E01", words) {
		t.Fatal("unrelated title must not be filtered")
	}
	adult := []string{"[porno]"}
	if !titleIsContentFiltered("Brazzers.Exxtra.22.03.13.MILF", adult) {
		t.Fatal("adult release must be filtered")
	}
	if !titleIsContentFiltered("My.Porn.Movie.2024", adult) {
		t.Fatal("porn word must be filtered")
	}
	if titleIsContentFiltered("Analisi.Di.Un.Film.2024", adult) {
		t.Fatal("analisi must not match anal")
	}
	if titleIsContentFiltered("The.Analyst.2024.1080p", adult) {
		t.Fatal("analyst must not match anal")
	}
}

func TestBlacklistUsesWordBoundaries(t *testing.T) {
	patterns := []string{"ts"}
	if titleIsBlacklisted("Subtitles.ITA.1080p", patterns) {
		t.Fatal("subtitles must not match ts")
	}
	if !titleIsBlacklisted("Movie.TS.1080p", patterns) {
		t.Fatal("ts token must match")
	}
}

func TestMovieLanguageRequirementsJSONIsParsed(t *testing.T) {
	cfg := DefaultConfig()
	movie := MovieConfig{
		Name:                 "Example",
		Quality:              "any",
		Language:             "ita",
		LanguageRequirements: `[{"language": "ita", "required": true}]`,
	}
	italian := models.Quality{Languages: []string{"ita"}, Language: "ita"}
	if !cfg.MovieReleaseAllowed(&movie, &italian) {
		t.Fatal("italian release should be allowed")
	}
	english := models.Quality{Languages: []string{"eng"}, Language: "eng"}
	if cfg.MovieReleaseAllowed(&movie, &english) {
		t.Fatal("english release should be rejected")
	}
}

func TestMovieLanguageRequirementsPlainListIsParsed(t *testing.T) {
	cfg := DefaultConfig()
	movie := MovieConfig{
		Name:     "Example",
		Quality:  "any",
		Language: "ita,eng",
	}
	english := models.Quality{Languages: []string{"eng"}, Language: "eng"}
	if !cfg.MovieReleaseAllowed(&movie, &english) {
		t.Fatal("english release should be allowed")
	}
}

func TestMovieSubtitlesOptionalButRequirementsMandatory(t *testing.T) {
	base := models.Quality{Language: "ita", Languages: []string{"ita"}}

	// "Sottotitoli" è solo una preferenza: una release senza sottotitoli
	// resta valida, ma non riceve il bonus.
	preferred := MovieConfig{
		Name:     "Example",
		Quality:  "any",
		Language: "ita",
		Subtitle: "ita",
	}
	cfg := DefaultConfig()
	if !cfg.MovieReleaseAllowed(&preferred, &base) {
		t.Fatal("preferred subtitles must not block")
	}
	if got := MovieSubtitleBonus(&preferred, &base); got != 0 {
		t.Fatalf("expected 0 bonus, got %d", got)
	}
	withSub := base
	withSub.HasSubtitle = true
	withSub.SubtitleLanguages = []string{"ita"}
	if got := MovieSubtitleBonus(&preferred, &withSub); got != 50 {
		t.Fatalf("expected 50 bonus, got %d", got)
	}

	// "Requisiti sottotitoli" è obbligatorio: senza quei sottotitoli la
	// release viene scartata.
	strict := MovieConfig{
		Name:                 "Example",
		Quality:              "any",
		Language:             "ita",
		SubtitleRequirements: "ita",
	}
	if cfg.MovieReleaseAllowed(&strict, &base) {
		t.Fatal("strict requirements must reject release without subtitles")
	}
	if !cfg.MovieReleaseAllowed(&strict, &withSub) {
		t.Fatal("strict requirements must accept release with subtitles")
	}
}

func TestSeriesSubtitlePreference(t *testing.T) {
	cfg := DefaultConfig()
	series := SeriesConfig{Name: "Example", Seasons: "1+", Quality: "any", Language: "ita", Subtitle: "ita,eng"}
	withoutSubtitles := models.Quality{Language: "ita", Languages: []string{"ita"}}
	if !cfg.SeriesReleaseAllowed(&series, &withoutSubtitles, "Example S01E01") {
		t.Fatal("preferred subtitles must not block a series release")
	}
	if got := SeriesSubtitleBonus(&series, &withoutSubtitles); got != 0 {
		t.Fatalf("expected no subtitle bonus, got %d", got)
	}
	withItalianSubtitles := withoutSubtitles
	withItalianSubtitles.HasSubtitle = true
	withItalianSubtitles.SubtitleLanguages = []string{"ita"}
	if got := SeriesSubtitleBonus(&series, &withItalianSubtitles); got != 50 {
		t.Fatalf("expected Italian subtitle bonus, got %d", got)
	}
}

func TestMovieMatchingRequiresYearAndHonorsExclusions(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Movies = append(cfg.Movies, MovieConfig{
		Name:    "The Batman",
		Year:    "2022",
		Enabled: true,
		Exclude: "animated,extended",
	})
	year2022 := int64(2022)
	if cfg.FindMovieMatch("The.Batman.2022.1080p.WEB-DL", &year2022) == nil {
		t.Fatal("2022 release should match")
	}
	year2025 := int64(2025)
	if cfg.FindMovieMatch("The.Batman.2025.1080p.WEB-DL", &year2025) != nil {
		t.Fatal("2025 release should not match")
	}
	if cfg.FindMovieMatch("The.Batman.2022.Extended.1080p", &year2022) != nil {
		t.Fatal("excluded word must reject the release")
	}
	cfg.Movies = append(cfg.Movies, MovieConfig{Name: "No Year Movie", Enabled: true})
	if cfg.FindMovieMatch("No.Year.Movie.2022", &year2022) != nil {
		t.Fatal("movie without a configured year should not match")
	}
}

func TestManualMovieMatchingToleratesPunctuationAndMissingYear(t *testing.T) {
	cfg := DefaultConfig()
	for _, entry := range [][2]string{
		{"Spider-Man Brand New Day", "2026"},
		{"Minions & Monsters", "2026"},
		{"Highlander", "2027"},
		{"Creation of the Gods Under Heaven", ""},
	} {
		cfg.Movies = append(cfg.Movies, MovieConfig{
			Name:    entry[0],
			Year:    entry[1],
			Enabled: true,
		})
	}
	// Punteggiatura e nomi di una parola: il match manuale ora funziona.
	if cfg.FindMovieMatchManual("Spider-Man.Brand.New.Day.1080p", nil) == nil {
		t.Fatal("punctuated manual match failed")
	}
	if cfg.FindMovieMatchManual("Minions.and.Monsters.2026.1080p", nil) == nil {
		t.Fatal("ampersand manual match failed")
	}
	year2027 := int64(2027)
	if cfg.FindMovieMatchManual("Highlander.2027.1080p", &year2027) == nil {
		t.Fatal("single word manual match failed")
	}
	// Anno del film vuoto: l'accoda manuale accetta, quella automatica no.
	if cfg.FindMovieMatchManual("Creation.of.the.Gods.Under.Heaven.1080p", nil) == nil {
		t.Fatal("missing movie year must be accepted manually")
	}
	if cfg.FindMovieMatch("Creation.of.the.Gods.Under.Heaven.1080p", nil) != nil {
		t.Fatal("missing movie year must be rejected automatically")
	}
	// Anno palesemente diverso: resta il vincolo ±1.
	year2010 := int64(2010)
	if cfg.FindMovieMatchManual("Highlander.2010.1080p", &year2010) != nil {
		t.Fatal("year difference beyond ±1 must be rejected")
	}
}

func TestQualityRulesSupportRangesAndMultiLanguageRequirements(t *testing.T) {
	quality := models.Quality{
		Resolution: "1080p",
		Language:   "ita",
		Languages:  []string{"ita", "eng"},
	}
	if !QualityAllowed(&quality, "720p-1080p", "ita,eng", "") {
		t.Fatal("720p-1080p should allow 1080p")
	}
	high := quality
	high.Resolution = "2160p"
	if !QualityAllowed(&high, "1080p", "ita", "") {
		t.Fatal("1080p requirement should allow 2160p")
	}
	if QualityAllowed(&high, "720p-1080p", "ita", "") {
		t.Fatal("720p-1080p must reject 2160p")
	}
	other := quality
	other.Language = "deu"
	other.Languages = []string{"deu"}
	if QualityAllowed(&other, "", "ita,eng", "") {
		t.Fatal("unsupported language must be rejected")
	}
	cfg := DefaultConfig()
	movie := MovieConfig{
		Language:             "eng",
		SubtitleRequirements: "ita",
	}
	if !cfg.MovieReleaseAllowed(&movie, &models.Quality{
		Language:          "eng",
		Languages:         []string{"eng"},
		HasSubtitle:       true,
		SubtitleLanguages: []string{"ita"},
	}) {
		t.Fatal("ita subtitles should satisfy the requirement")
	}
	if cfg.MovieReleaseAllowed(&movie, &models.Quality{
		Language:          "eng",
		Languages:         []string{"eng"},
		HasSubtitle:       true,
		SubtitleLanguages: []string{"eng"},
	}) {
		t.Fatal("eng subtitles must not satisfy an ita requirement")
	}
}

func TestGlobalFiltersRejectBlacklistedAndStaleReleases(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Blacklist = []string{"cam"}
	cfg.ContentFilters = []string{"[non-latino]"}
	cfg.MaxReleaseAgeDays = 7
	release := func(title string, ageDays int64) models.Release {
		year := int64(2026)
		return models.Release{
			Title:        title,
			Magnet:       "magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
			Source:       "test",
			Kind:         "movie",
			EpisodeRange: []int64{},
			Year:         &year,
			Seeders:      -1,
			Peers:        -1,
			DiscoveredAt: time.Now().UTC().AddDate(0, 0, int(-ageDays)),
		}
	}
	clean := release("Movie.1080p.ITA", 1)
	if !cfg.ReleaseAllowed(&clean) {
		t.Fatal("clean release should be allowed")
	}
	blacklisted := release("Movie.1080p.CAM.ITA", 1)
	if cfg.ReleaseAllowed(&blacklisted) {
		t.Fatal("blacklisted release should be rejected")
	}
	cyrillic := release("Фильм.1080p.ITA", 1)
	if cfg.ReleaseAllowed(&cyrillic) {
		t.Fatal("content filtered release should be rejected")
	}
	unrelated := release("Movie.1080p.ENG", 1)
	if !cfg.ReleaseAllowed(&unrelated) {
		t.Fatal("unrelated release should be allowed")
	}
	stale := release("Movie.1080p.ITA", 8)
	if cfg.ReleaseAllowed(&stale) {
		t.Fatal("stale release should be rejected")
	}
}

func TestSourceFiltersBlockOnlyTheirOwnSource(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SourceFilters = []SourceFilter{{
		Source:   "ExtTo - MIRCrewRS",
		Keywords: []string{"x265"},
		Enabled:  true,
	}}
	release := func(title string, source string) models.Release {
		year := int64(2026)
		return models.Release{
			Title:        title,
			Magnet:       "magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
			Source:       source,
			Kind:         "movie",
			EpisodeRange: []int64{},
			Year:         &year,
			Seeders:      -1,
			Peers:        -1,
			DiscoveredAt: time.Now().UTC(),
		}
	}
	// Blocked: matching source and keyword.
	blocked := release("Film 1080p x265", "ExtTo - MIRCrewRS")
	if cfg.ReleaseAllowed(&blocked) {
		t.Fatal("matching source and keyword should be blocked")
	}
	// Different source: allowed even with the keyword.
	other := release("Film 1080p x265", "jackett")
	if !cfg.ReleaseAllowed(&other) {
		t.Fatal("other source should be allowed")
	}
	// Same source, no keyword: allowed.
	noKeyword := release("Film 1080p h264", "ExtTo - MIRCrewRS")
	if !cfg.ReleaseAllowed(&noKeyword) {
		t.Fatal("same source without keyword should be allowed")
	}
	// Disabled filter never blocks.
	cfg.SourceFilters[0].Enabled = false
	disabled := release("Film 1080p x265", "ExtTo - MIRCrewRS")
	if !cfg.ReleaseAllowed(&disabled) {
		t.Fatal("disabled filter must not block")
	}
}

func TestSavesAndReloadsLibraryConfiguration(t *testing.T) {
	dir := t.TempDir()
	series := []SeriesConfig{{Name: "Example", Enabled: true}}
	movies := []MovieConfig{{Name: "Example Movie", Year: "2024", Enabled: true}}
	if err := SaveLibrary(dir, series, movies); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.DataDir = dir
	if err := cfg.loadConfigDB(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Series) == 0 || cfg.Series[0].Name != "Example" {
		t.Fatalf("series not reloaded: %+v", cfg.Series)
	}
	if len(cfg.Movies) == 0 || cfg.Movies[0].Name != "Example Movie" {
		t.Fatalf("movies not reloaded: %+v", cfg.Movies)
	}
}

func TestSavesAndReloadsMovieStaticMetadata(t *testing.T) {
	dir := t.TempDir()
	movies := []MovieConfig{{
		Name:          "Titolo Italiano",
		Year:          "2024",
		TmdbID:        "12345",
		OriginalTitle: "Original Title",
		Overview:      "Trama del film.",
		PosterPath:    "/poster.jpg",
		Quality:       "1080p",
		Language:      "ita",
		Enabled:       true,
	}}
	if err := SaveLibrary(dir, []SeriesConfig{}, movies); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.DataDir = dir
	if err := cfg.loadConfigDB(); err != nil {
		t.Fatal(err)
	}
	movie := cfg.Movies[0]
	if movie.TmdbID != "12345" {
		t.Fatalf("tmdb id = %q", movie.TmdbID)
	}
	if movie.OriginalTitle != "Original Title" {
		t.Fatalf("original title = %q", movie.OriginalTitle)
	}
	if movie.Overview != "Trama del film." {
		t.Fatalf("overview = %q", movie.Overview)
	}
	if movie.PosterPath != "/poster.jpg" {
		t.Fatalf("poster path = %q", movie.PosterPath)
	}
	// I dati di download restano separati e invariati.
	if movie.Quality != "1080p" {
		t.Fatalf("quality = %q", movie.Quality)
	}
	if movie.Language != "ita" {
		t.Fatalf("language = %q", movie.Language)
	}
}

func TestPersistsActiveSettingWithoutForcingDryRunDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := SaveSetting(dir, "active", "yes"); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.DataDir = dir
	if err := cfg.loadConfigDB(); err != nil {
		t.Fatal(err)
	}
	if !cfg.Active {
		t.Fatal("active setting was not applied")
	}
	if !cfg.DryRun {
		t.Fatal("dry_run default should be preserved")
	}
}

func TestPersistsGlobalFiltersFromJSONOrCSVValues(t *testing.T) {
	dir := t.TempDir()
	if err := SaveSetting(dir, "blacklist", `["cam","ts"]`); err != nil {
		t.Fatal(err)
	}
	if err := SaveSetting(dir, "content_filters", "ita,web-dl"); err != nil {
		t.Fatal(err)
	}
	if err := SaveSetting(dir, "max_release_age_days", "14"); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.DataDir = dir
	if err := cfg.loadConfigDB(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Blacklist, []string{"cam", "ts"}) {
		t.Fatalf("blacklist = %v", cfg.Blacklist)
	}
	if !reflect.DeepEqual(cfg.ContentFilters, []string{"ita", "web-dl"}) {
		t.Fatalf("content filters = %v", cfg.ContentFilters)
	}
	if cfg.MaxReleaseAgeDays != 14 {
		t.Fatalf("max release age = %d", cfg.MaxReleaseAgeDays)
	}
}

func TestLoadsImportedSeriesAndLegacyExttoFilterKeys(t *testing.T) {
	dir := t.TempDir()
	if err := SaveSetting(dir, "content_filter", "ita"); err != nil {
		t.Fatal(err)
	}
	if err := SaveSetting(dir, "max_age_days", "21"); err != nil {
		t.Fatal(err)
	}
	db, err := OpenSQLite(filepath.Join(dir, "gextto_series.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE IF NOT EXISTS series (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, seasons TEXT DEFAULT '1+', quality TEXT DEFAULT '', language TEXT DEFAULT 'ita', enabled INTEGER DEFAULT 1, archive_path TEXT DEFAULT '', tmdb_id TEXT DEFAULT '', aliases TEXT DEFAULT '')"); err != nil {
		t.Fatal(err)
	}
	for _, column := range [][2]string{
		{"timeframe", "INTEGER DEFAULT 0"},
		{"ignored_seasons", "TEXT DEFAULT '[]'"},
		{"subtitle", "TEXT DEFAULT ''"},
		{"exclude", "TEXT DEFAULT ''"},
	} {
		if err := ensureColumn(db, "series", column[0], column[1]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("INSERT INTO series(name,seasons,quality,language,enabled,archive_path,tmdb_id,aliases,timeframe,ignored_seasons,subtitle,exclude) VALUES ('Imported Show','1-3','1080p','ita',1,'/nas/shows','123','[\"Imported Alias\"]',48,'[2]','yes','cam')"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	cfg := DefaultConfig()
	cfg.DataDir = dir
	if err := cfg.loadConfigDB(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.ContentFilters, []string{"ita"}) {
		t.Fatalf("content filters = %v", cfg.ContentFilters)
	}
	if cfg.MaxReleaseAgeDays != 21 {
		t.Fatalf("max release age = %d", cfg.MaxReleaseAgeDays)
	}
	if len(cfg.Series) != 1 {
		t.Fatalf("series = %+v", cfg.Series)
	}
	if cfg.Series[0].Timeframe != 48 {
		t.Fatalf("timeframe = %d", cfg.Series[0].Timeframe)
	}
	if !reflect.DeepEqual(cfg.Series[0].IgnoredSeasons, []int64{2}) {
		t.Fatalf("ignored seasons = %v", cfg.Series[0].IgnoredSeasons)
	}
	if cfg.Series[0].Exclude != "cam" {
		t.Fatalf("exclude = %q", cfg.Series[0].Exclude)
	}
}

func TestLegacyQualityRangesStillCoverTheCommonProfiles(t *testing.T) {
	cfg := DefaultConfig()
	quality := func(resolution string) models.Quality {
		return models.Quality{Resolution: resolution}
	}
	// "720p" means 720p and above; "1080p" means 1080p and above.
	if !cfg.MovieReleaseAllowed(&MovieConfig{
		Name:    "Film",
		Year:    "2020",
		Quality: "720p",
		Enabled: true,
	}, &models.Quality{Resolution: "2160p"}) {
		t.Fatal("720p should allow 2160p")
	}
	// "720p-1080p" caps the range.
	ranged := MovieConfig{
		Name:    "Film",
		Year:    "2020",
		Quality: "720p-1080p",
		Enabled: true,
	}
	res1080 := quality("1080p")
	res720 := quality("720p")
	res2160 := quality("2160p")
	res576 := quality("576p")
	if !cfg.MovieReleaseAllowed(&ranged, &res1080) {
		t.Fatal("720p-1080p should allow 1080p")
	}
	if !cfg.MovieReleaseAllowed(&ranged, &res720) {
		t.Fatal("720p-1080p should allow 720p")
	}
	if cfg.MovieReleaseAllowed(&ranged, &res2160) {
		t.Fatal("720p-1080p must reject 2160p")
	}
	if cfg.MovieReleaseAllowed(&ranged, &res576) {
		t.Fatal("720p-1080p must reject 576p")
	}
}

func TestTitlesDefaultToAllowingUpgrades(t *testing.T) {
	series := SeriesConfig{}
	movie := MovieConfig{}
	if series.DisableUpgrades {
		t.Fatal("series must default to upgrades allowed")
	}
	if movie.DisableUpgrades {
		t.Fatal("movie must default to upgrades allowed")
	}
	// The flag round-trips through the JSON config.
	toggled := SeriesConfig{DisableUpgrades: true, Name: "Show"}
	encoded, err := json.Marshal(toggled)
	if err != nil {
		t.Fatal(err)
	}
	var parsed SeriesConfig
	if err := json.Unmarshal(encoded, &parsed); err != nil {
		t.Fatal(err)
	}
	if !parsed.DisableUpgrades {
		t.Fatal("disable_upgrades did not round-trip")
	}
	if parsed.UpgradesAllowed() {
		t.Fatal("upgrades_allowed should be false when disabled")
	}
}

// TestConfigUnmarshalKeepsDefaultsForAbsentKeys locks in the fix for a partial
// gextto.json: unmarshalling `{}` (or a file with only some keys) must not wipe
// data_dir, listen and the other DefaultConfig values.
func TestConfigUnmarshalKeepsDefaultsForAbsentKeys(t *testing.T) {
	cfg := DefaultConfig()
	if err := json.Unmarshal([]byte("{}"), &cfg); err != nil {
		t.Fatalf("unmarshal empty object: %v", err)
	}
	if cfg.DataDir == "" {
		t.Fatal("data_dir default was wiped by an empty JSON")
	}
	if cfg.Listen == "" {
		t.Fatal("listen default was wiped by an empty JSON")
	}
	if cfg.EngineListen == "" {
		t.Fatal("engine_listen default was wiped by an empty JSON")
	}
	if cfg.RefreshSecs == 0 {
		t.Fatal("refresh_secs default was wiped by an empty JSON")
	}
	if cfg.LibtorrentTempDir == nil {
		t.Fatal("libtorrent temp dir default was wiped by an empty JSON")
	}

	// A single key only overrides that key.
	cfg2 := DefaultConfig()
	if err := json.Unmarshal([]byte(`{"data_dir":"/srv/gextto","refresh_secs":42}`), &cfg2); err != nil {
		t.Fatalf("unmarshal partial: %v", err)
	}
	if cfg2.DataDir != "/srv/gextto" {
		t.Fatalf("data_dir = %q, want /srv/gextto", cfg2.DataDir)
	}
	if cfg2.RefreshSecs != 42 {
		t.Fatalf("refresh_secs = %d, want 42", cfg2.RefreshSecs)
	}
	if cfg2.Listen == "" {
		t.Fatal("listen default was wiped by a partial JSON")
	}
}

// TestLoadConfigPartialFileKeepsDefaults exercises the real loader: a
// gextto.json containing only `{}` must behave like the defaults.
func TestLoadConfigPartialFileKeepsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gextto.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.DataDir == "" || cfg.Listen == "" || cfg.EngineListen == "" || cfg.RefreshSecs == 0 {
		t.Fatalf("partial config lost defaults: data_dir=%q listen=%q engine_listen=%q refresh=%d",
			cfg.DataDir, cfg.Listen, cfg.EngineListen, cfg.RefreshSecs)
	}

	customDir := filepath.Join(dir, "custom")
	if err := os.WriteFile(path, []byte(`{"data_dir":"`+customDir+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig custom: %v", err)
	}
	if cfg.DataDir != customDir {
		t.Fatalf("data_dir = %q, want %q", cfg.DataDir, customDir)
	}
	if cfg.Listen == "" {
		t.Fatal("listen default was wiped by a partial JSON file")
	}
}
