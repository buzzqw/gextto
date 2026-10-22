package gextto

import (
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func manualImportConfig(t *testing.T, archive string) *Config {
	t.Helper()
	return &Config{
		DownloadDir:    filepath.Dir(archive),
		Series:         []SeriesConfig{{Name: "Show", Enabled: true, Seasons: "1+", ArchivePath: archive}},
		Movies:         []MovieConfig{{Name: "Film", Year: "2020", Enabled: true}},
		RenameEpisodes: true,
		ArchiveRoot:    &archive,
		Settings:       map[string]string{"default_language": "ita"},
	}
}

func TestManualImportPreviewAndApplyEpisode(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "downloads", "Show.S01E02.1080p.WEB-DL.x264-GRP.mkv")
	archive := filepath.Join(dir, "library")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := manualImportConfig(t, archive)
	candidates, err := ManualImportPreview(cfg, source)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("preview = %+v, %v", candidates, err)
	}
	c := candidates[0]
	if !c.Matched || c.Series != "Show" || c.Season != 1 || c.Episode != 2 || c.Destination != archive {
		t.Fatalf("candidate = %+v", c)
	}
	if !strings.Contains(filepath.Base(c.Target), "S01E02") {
		t.Fatalf("target = %q, want an S01E02 name", c.Target)
	}
	db := newTestDB(t)
	final, err := ManualImport(context.Background(), cfg, db, ManualImportRequest{Path: source, Kind: "series", Series: "Show", Season: 1, Episode: 2})
	if err != nil {
		t.Fatalf("ManualImport: %v", err)
	}
	if _, err := os.Stat(final); err != nil {
		t.Fatalf("the archived file is missing: %v", err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("the source must be moved, stat err = %v", err)
	}
	var path string
	if err := db.db.QueryRow("SELECT e.archive_path FROM episodes e JOIN series s ON s.id=e.series_id WHERE s.name='Show' AND e.season=1 AND e.episode=2").Scan(&path); err != nil {
		t.Fatalf("episode row: %v", err)
	}
	if path != final {
		t.Fatalf("database path = %q, want %q", path, final)
	}
}

func TestManualImportRefusesAnExistingFileWithoutReplace(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "Show.S01E03.720p.mkv")
	archive := filepath.Join(dir, "library")
	if err := os.WriteFile(source, make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := manualImportConfig(t, archive)
	db := newTestDB(t)
	if _, err := ManualImport(context.Background(), cfg, db, ManualImportRequest{Path: source, Kind: "series", Series: "Show", Season: 1, Episode: 3}); err != nil {
		t.Fatalf("first import: %v", err)
	}
	// A second source with the same target name: the old file is still there.
	again := filepath.Join(dir, "Show.S01E03.720p.WEB-DL.mkv")
	if err := os.WriteFile(again, make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ManualImport(context.Background(), cfg, db, ManualImportRequest{Path: again, Kind: "series", Series: "Show", Season: 1, Episode: 3}); err == nil {
		t.Fatal("an existing file must be refused without confirm")
	}
}

func TestManualImportParsesASeriesTitle(t *testing.T) {
	c := manualImportCandidate(&Config{Series: []SeriesConfig{{Name: "Show", Enabled: true, Seasons: "1+"}}, RenameEpisodes: true, Settings: map[string]string{}},
		"/tmp/Show.S01E01.1080p.WEB-DL.mkv")
	if c.Series != "Show" || c.Season != 1 || c.Episode != 1 {
		t.Fatalf("parsed = %+v", c)
	}
}

func TestManualImportPageAndPreview(t *testing.T) {
	state := newTestAppState(t)
	archive := filepath.Join(state.cfg.DataDir, "library")
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{{Name: "Show", Enabled: true, Seasons: "1+", ArchivePath: archive}}, nil); err != nil {
		t.Fatal(err)
	}
	TouchConfigGeneration()
	if err := os.MkdirAll(state.cfg.DownloadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(state.cfg.DownloadDir, "Show.S01E04.1080p.WEB-DL.mkv")
	if err := os.WriteFile(source, make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	V2Page(rec, httptest.NewRequest("GET", "/?view=maintenance&tab=import", nil), state)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Importa un file esistente") {
		t.Fatalf("page = %d", rec.Code)
	}
	req := httptest.NewRequest("GET", "/import/preview?path="+url.QueryEscape(source), nil)
	rec = httptest.NewRecorder()
	V2ImportPreview(rec, req, state)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "Show.S01E04") || !strings.Contains(body, "Importa ora") {
		t.Fatalf("preview = %d %s", rec.Code, body)
	}
}

func TestManualImportMovie(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "Film.2020.1080p.WEB-DL.x264-GRP.mkv")
	archive := filepath.Join(dir, "library")
	if err := os.WriteFile(source, make([]byte, 512), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{
		DownloadDir:    dir,
		Movies:         []MovieConfig{{Name: "Film", Year: "2020", Enabled: true}},
		RenameEpisodes: true,
		ArchiveRoot:    &archive,
		Settings:       map[string]string{"default_language": "ita"},
	}
	candidates, err := ManualImportPreview(cfg, source)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("preview = %+v, %v", candidates, err)
	}
	if !candidates[0].Matched || candidates[0].Kind != "movie" || candidates[0].Destination != archive {
		t.Fatalf("candidate = %+v", candidates[0])
	}
	db := newTestDB(t)
	final, err := ManualImport(context.Background(), cfg, db, ManualImportRequest{Path: source, Kind: "movie", Movie: "Film", Year: 2020})
	if err != nil {
		t.Fatalf("ManualImport: %v", err)
	}
	if _, err := os.Stat(final); err != nil {
		t.Fatalf("archived movie: %v", err)
	}
	var size int64
	if err := db.db.QueryRow("SELECT size_bytes FROM movies WHERE name='Film' AND year=2020").Scan(&size); err != nil {
		t.Fatalf("movie row: %v", err)
	}
	if size != 512 {
		t.Fatalf("movie size = %d", size)
	}
}

func TestManualImportRegistersTheCorrectedEpisode(t *testing.T) {
	dir := t.TempDir()
	// The name claims E01-E02; the operator corrects it to E05.
	source := filepath.Join(dir, "Show.S01E01E02.1080p.mkv")
	archive := filepath.Join(dir, "library")
	if err := os.WriteFile(source, make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := manualImportConfig(t, archive)
	db := newTestDB(t)
	final, err := ManualImport(context.Background(), cfg, db, ManualImportRequest{Path: source, Kind: "series", Series: "Show", Season: 1, Episode: 5})
	if err != nil {
		t.Fatalf("ManualImport: %v", err)
	}
	if !strings.Contains(filepath.Base(final), "S01E05") {
		t.Fatalf("final name %q, want the corrected S01E05", final)
	}
	var episodes []int64
	rows, err := db.db.Query("SELECT e.episode FROM episodes e JOIN series s ON s.id=e.series_id WHERE s.name='Show' AND COALESCE(e.archive_path,'') <> '' ORDER BY e.episode")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var episode int64
		_ = rows.Scan(&episode)
		episodes = append(episodes, episode)
	}
	if len(episodes) != 1 || episodes[0] != 5 {
		t.Fatalf("registered episodes = %v, want [5]", episodes)
	}
}

func TestManualImportMatchesAMovieWithoutAYear(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "Film.1080p.mkv")
	archive := filepath.Join(dir, "library")
	if err := os.WriteFile(source, make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := manualImportConfig(t, archive)
	db := newTestDB(t)
	// The page sends the chosen movie with year 0 when the name has none.
	final, err := ManualImport(context.Background(), cfg, db, ManualImportRequest{Path: source, Kind: "movie", Movie: "Film"})
	if err != nil {
		t.Fatalf("ManualImport: %v", err)
	}
	var year int64
	if err := db.db.QueryRow("SELECT year FROM movies WHERE name='Film'").Scan(&year); err != nil || year != 2020 {
		t.Fatalf("movie row year = %d, %v; want the configured 2020", year, err)
	}
	if !strings.Contains(filepath.Base(final), "2020") {
		t.Fatalf("final name %q has no year", final)
	}
}

func TestManualImportRestoresTheNameOfARefusedMovie(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "library")
	cfg := manualImportConfig(t, archive)
	db := newTestDB(t)
	first := filepath.Join(dir, "in1", "Film.2020.1080p.mkv")
	second := filepath.Join(dir, "in2", "Film.2020.1080p.WEB.mkv")
	for _, path := range []string{first, second} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, make([]byte, 100), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ManualImport(context.Background(), cfg, db, ManualImportRequest{Path: first, Kind: "movie", Movie: "Film", Year: 2020}); err != nil {
		t.Fatalf("first import: %v", err)
	}
	if _, err := ManualImport(context.Background(), cfg, db, ManualImportRequest{Path: second, Kind: "movie", Movie: "Film", Year: 2020}); err == nil {
		t.Fatal("the second copy must be refused without replace")
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatalf("the refused file must keep its original name: %v", err)
	}
}
