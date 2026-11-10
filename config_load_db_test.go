package gextto

// config_load_db_test.go pins what loadConfigDB reads from an older
// installation: legacy keys still honoured, retired ones dropped, a movies
// table without the newer columns, an unreadable list that must not empty the
// configuration, and the typed fields derived from the settings.

import (
	"path/filepath"
	"reflect"
	"testing"
)

func writeConfigDB(t *testing.T, dir string, settings map[string]string, movieSchema string, movies ...string) {
	t.Helper()
	conn, err := OpenConfigDB(filepath.Join(dir, "gextto_config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Exec("CREATE TABLE settings(key TEXT PRIMARY KEY, value TEXT)"); err != nil {
		t.Fatal(err)
	}
	for key, value := range settings {
		if _, err := conn.Exec("INSERT INTO settings(key,value) VALUES (?1,?2)", key, value); err != nil {
			t.Fatal(err)
		}
	}
	if movieSchema != "" {
		if _, err := conn.Exec(movieSchema); err != nil {
			t.Fatal(err)
		}
		for _, insert := range movies {
			if _, err := conn.Exec(insert); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func loadTestConfigDB(t *testing.T, dir string) Config {
	t.Helper()
	cfg := DefaultConfig()
	cfg.DataDir = dir
	cfg.Settings = map[string]string{}
	cfg.Series = []SeriesConfig{{Name: "From the file", Enabled: true}}
	if err := cfg.loadConfigDB(); err != nil {
		t.Fatalf("loadConfigDB: %v", err)
	}
	return cfg
}

// An older installation: legacy keys are read, a retired one is dropped from
// the database, the old movie table loads with defaults for its missing
// columns, and an unreadable series list keeps the list already loaded.
func TestLoadConfigDBReadsAnOlderInstallation(t *testing.T) {
	dir := t.TempDir()
	writeConfigDB(t, dir, map[string]string{
		"api_token":               "secret-from-an-old-version",
		"content_filter":          "cam, ts",
		"max_age_days":            "30",
		"jackett_url":             "http://jackett.local:9117",
		"jackett_api":             "key",
		"websearch_engines":       "Knaben, torrentz2",
		"_migrated_series":        "{not json",
		"refresh_interval":        "21600",
		"active":                  "yes",
		"rename_episodes":         "1",
		"rename_format":           "something-unknown",
		"trash_path":              "   ",
		"cleanup_action":          "shred",
		"upgrade_min_score_diff":  "-5",
		"notify_cooldown_minutes": "-1",
		"tmdb_api_key":            "",
	}, "CREATE TABLE movies_config(id INTEGER PRIMARY KEY, name TEXT, year TEXT, quality TEXT, language TEXT, enabled INTEGER, subtitle TEXT)",
		"INSERT INTO movies_config(id,name,year,quality,language,enabled,subtitle) VALUES (2,'Example Movie','1999','1080p','ita',1,'')",
		"INSERT INTO movies_config(id,name,year,quality,language,enabled,subtitle) VALUES (1,'another movie','2001','','',0,'')")

	cfg := loadTestConfigDB(t, dir)

	if _, kept := cfg.Settings["api_token"]; kept {
		t.Fatal("the retired api_token is still in the settings")
	}
	if value, err := loadConfigSetting(dir, "api_token"); err != nil || value != "" {
		t.Fatalf("api_token still stored: %q %v", value, err)
	}
	if !reflect.DeepEqual(cfg.ContentFilters, []string{"cam", "ts"}) {
		t.Fatalf("content filters from the legacy key = %v", cfg.ContentFilters)
	}
	if cfg.MaxReleaseAgeDays != 30 {
		t.Fatalf("max age from the legacy key = %d", cfg.MaxReleaseAgeDays)
	}
	found := false
	for _, indexer := range cfg.Indexers {
		if indexer.Name == "jackett" && indexer.URL == "http://jackett.local:9117" && indexer.APIKey == "key" && indexer.Enabled {
			found = true
		}
	}
	if !found {
		t.Fatalf("the legacy Jackett keys did not become an indexer: %+v", cfg.Indexers)
	}
	if !reflect.DeepEqual(cfg.WebsearchEngines, []string{"knaben"}) {
		t.Fatalf("web engines = %v, want the comma list without the retired engine", cfg.WebsearchEngines)
	}
	if len(cfg.Series) != 1 || cfg.Series[0].Name != "From the file" {
		t.Fatalf("an unreadable series list replaced the loaded one: %+v", cfg.Series)
	}
	if len(cfg.Movies) != 2 || cfg.Movies[0].Name != "another movie" || cfg.Movies[1].Name != "Example Movie" {
		t.Fatalf("movies = %+v, want both in name order", cfg.Movies)
	}
	if !cfg.Movies[1].Enabled || cfg.Movies[1].TmdbID != "" || cfg.Movies[1].DisableUpgrades {
		t.Fatalf("old-schema movie = %+v", cfg.Movies[1])
	}
	if cfg.RefreshSecs != 21600 || !cfg.Active || !cfg.RenameEpisodes {
		t.Fatalf("refresh %d active %v rename %v", cfg.RefreshSecs, cfg.Active, cfg.RenameEpisodes)
	}
	if cfg.RenameFormat != defaultRenameFormatValue {
		t.Fatalf("unknown rename format kept: %q", cfg.RenameFormat)
	}
	if cfg.TrashPath != nil {
		t.Fatalf("a blank trash path = %q, want none", *cfg.TrashPath)
	}
	if cfg.CleanupAction != "move" {
		t.Fatalf("unknown cleanup action = %q, want move", cfg.CleanupAction)
	}
	if cfg.UpgradeMinScoreDiff != 0 || cfg.CleanupMinScoreDiff != 0 || cfg.NotifyCooldownMinutes != 0 {
		t.Fatalf("negative values not clamped: %d %d %d", cfg.UpgradeMinScoreDiff, cfg.CleanupMinScoreDiff, cfg.NotifyCooldownMinutes)
	}
	if cfg.TmdbAPIKey != nil {
		t.Fatal("an empty TMDB key was kept as a key")
	}
	// Switches absent from an older installation keep notifying everything.
	if !cfg.NotifyEventDownloads || !cfg.NotifyEventErrors || !cfg.NotifyEventHealth || !cfg.NotifyEventMaintenance {
		t.Fatal("a missing notification switch turned a group off")
	}
}

// The current keys win over the legacy ones, and a missing settings table is a
// first run with nothing to overlay.
func TestLoadConfigDBPrefersCurrentKeys(t *testing.T) {
	dir := t.TempDir()
	writeConfigDB(t, dir, map[string]string{
		"content_filters":      "hdcam",
		"content_filter":       "cam, ts",
		"max_release_age_days": "7",
		"max_age_days":         "30",
		"_migrated_series":     `[{"name":"Series A","enabled":true}]`,
		"cleanup_action":       "delete",
	}, "")
	cfg := loadTestConfigDB(t, dir)
	if !reflect.DeepEqual(cfg.ContentFilters, []string{"hdcam"}) || cfg.MaxReleaseAgeDays != 7 {
		t.Fatalf("filters %v age %d, want the current keys", cfg.ContentFilters, cfg.MaxReleaseAgeDays)
	}
	if len(cfg.Series) != 1 || cfg.Series[0].Name != "Series A" {
		t.Fatalf("series = %+v, want the stored list", cfg.Series)
	}
	if cfg.CleanupAction != "delete" {
		t.Fatalf("cleanup action = %q", cfg.CleanupAction)
	}
	if len(cfg.Movies) != 0 {
		t.Fatalf("movies without a table = %+v", cfg.Movies)
	}

	empty := t.TempDir()
	conn, err := OpenConfigDB(filepath.Join(empty, "gextto_config.db"))
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	first := loadTestConfigDB(t, empty)
	if len(first.Series) != 1 || first.Series[0].Name != "From the file" {
		t.Fatalf("a first run changed the loaded series: %+v", first.Series)
	}
}
