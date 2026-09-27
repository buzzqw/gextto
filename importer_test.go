package gextto

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The legacy `extto` schema below is copied from a real extto data directory, so
// the importer reads exactly the columns the /Go port expects.

const legacySeriesSchema = `
CREATE TABLE series (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE,
    quality_requirement TEXT,
    is_completed BOOLEAN DEFAULT 0, is_ended BOOLEAN DEFAULT 0,
    aliases TEXT DEFAULT '', seasons TEXT DEFAULT '1+',
    language TEXT DEFAULT 'ita', enabled INTEGER DEFAULT 1,
    archive_path TEXT DEFAULT '', timeframe INTEGER DEFAULT 0,
    ignored_seasons TEXT DEFAULT '[]', tmdb_id TEXT DEFAULT '',
    subtitle TEXT DEFAULT '', season_subfolders INTEGER DEFAULT 0,
    exclude TEXT DEFAULT '');
CREATE TABLE episodes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id INTEGER, season INTEGER, episode INTEGER, title TEXT,
    quality_score INTEGER, is_repack INTEGER, magnet_hash TEXT UNIQUE,
    magnet_link TEXT, downloaded_at TEXT, archive_path TEXT,
    size_bytes INTEGER DEFAULT 0, original_title TEXT, rename_verified INTEGER DEFAULT 0,
    UNIQUE(series_id, season, episode));
CREATE TABLE movies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT, year INTEGER, title TEXT, quality_score INTEGER,
    magnet_hash TEXT UNIQUE, magnet_link TEXT, downloaded_at TEXT,
    size_bytes INTEGER DEFAULT 0, removed_at TEXT DEFAULT NULL);
CREATE TABLE torrent_meta (
    hash TEXT PRIMARY KEY, tag TEXT DEFAULT '', ui_state TEXT DEFAULT '',
    progress REAL DEFAULT 0, paused INTEGER DEFAULT 0, total_size INTEGER DEFAULT 0,
    downloaded INTEGER DEFAULT 0, name TEXT DEFAULT '', updated_at INTEGER DEFAULT 0,
    pp_notified INTEGER DEFAULT 0, no_rename INTEGER DEFAULT 0,
    dl_source TEXT DEFAULT '', pack_copied INTEGER DEFAULT 0);
`

const legacySeriesInsert = `
INSERT INTO series(id,name,quality_requirement,is_completed,is_ended,aliases,seasons,language,enabled,archive_path,timeframe,ignored_seasons,tmdb_id,subtitle,season_subfolders,exclude) VALUES
    (1,'Alpha','1080p',0,0,'a1,a2','1+','ita',1,'/srv/alpha',24,'[1]','111','ita',1,''),
    (2,'Beta','720p',0,0,'','1+','eng',1,'/srv/beta',0,'[]','222','eng',0,'');
INSERT INTO series(id,name) VALUES (3,'');
INSERT INTO episodes(series_id,season,episode,title,quality_score,is_repack,magnet_hash,magnet_link,downloaded_at,archive_path,size_bytes,original_title,rename_verified) VALUES
    (1,1,1,'A1',100,0,'1111111111111111111111111111111111111111','','2026-01-01 00:00:00','/a1',1000,'Alpha One',1),
    (1,1,2,'A2',90,1,'','magnet:?xt=urn:btih:2222222222222222222222222222222222222222','2026-01-02 00:00:00','/a2',2000,'',0),
    (99,5,5,'Orphan',0,0,NULL,NULL,NULL,NULL,0,NULL,0);
INSERT INTO movies(name,year,title,quality_score,magnet_hash,magnet_link,downloaded_at,size_bytes,removed_at) VALUES
    ('Movie One',2020,'Movie One Title',80,'3333333333333333333333333333333333333333','','2026-01-01 00:00:00',5000,NULL),
    ('Movie Two',2021,'Movie Two Title',70,'','magnet:?xt=urn:btih:4444444444444444444444444444444444444444','2026-01-02 00:00:00',6000,'2026-02-01 00:00:00');
INSERT INTO torrent_meta(hash,tag,dl_source,ui_state,progress,paused,total_size,downloaded,name,updated_at) VALUES
    ('UPPERHASH','tag1','dl_source1','ui_state1',0.5,1,1000,500,'name1',0),
    ('HASH2','','','',0,0,0,0,'name2',0),
    ('','','','',0,0,0,0,'blank',0);
`

const legacyArchiveSchema = `
CREATE TABLE archive (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT UNIQUE, magnet TEXT, source TEXT, added_at TEXT);
`

const legacyArchiveInsert = `
INSERT INTO archive(title,magnet,source,added_at) VALUES
    ('Arc One','magnet:?xt=urn:btih:5555555555555555555555555555555555555555','feed','2026-01-01 00:00:00'),
    ('Arc Two','magnet:?xt=urn:btih:6666666666666666666666666666666666666666','feed2','2026-01-02 00:00:00'),
    ('Arc One Duplicate','magnet:?xt=urn:btih:5555555555555555555555555555555555555555','feed3','2026-01-03 00:00:00');
`

const legacyConfigSchema = `
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '');
CREATE TABLE movies_config (
    id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL,
    year TEXT NOT NULL DEFAULT '', quality TEXT NOT NULL DEFAULT 'any',
    language TEXT NOT NULL DEFAULT 'ita', enabled INTEGER NOT NULL DEFAULT 1,
    subtitle TEXT NOT NULL DEFAULT '', exclude TEXT NOT NULL DEFAULT '',
    language_requirements TEXT NOT NULL DEFAULT '',
    subtitle_requirements TEXT NOT NULL DEFAULT '');
CREATE TABLE translations (
    lang TEXT NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (lang, key));
CREATE TABLE torrent_limits (
    info_hash TEXT PRIMARY KEY, dl_bytes INTEGER NOT NULL DEFAULT -1,
    ul_bytes INTEGER NOT NULL DEFAULT -1,
    updated_at TEXT NOT NULL DEFAULT (datetime('now')));
`

const legacyConfigInsert = `
INSERT INTO settings(key,value) VALUES ('ui_language','eng'),('download_dir','/downloads');
INSERT INTO movies_config(id,name,year,quality,language,enabled,subtitle,exclude,language_requirements,subtitle_requirements) VALUES
    (1,'Conf Movie','2020','1080p','eng',1,'','','','');
INSERT INTO translations(lang,key,value) VALUES ('eng','hello','Hello');
INSERT INTO torrent_limits(info_hash,dl_bytes,ul_bytes) VALUES ('LIMITHASH',123,456);
`

const legacyComicsSchema = `
CREATE TABLE comics_monitored (
    id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL, tag_url TEXT NOT NULL,
    cover_url TEXT DEFAULT '', publisher TEXT DEFAULT '', description TEXT DEFAULT '',
    from_date TEXT NOT NULL, save_path TEXT DEFAULT '', enabled INTEGER DEFAULT 1,
    added_at TEXT DEFAULT (datetime('now')), last_checked TEXT DEFAULT NULL,
    UNIQUE(tag_url));
CREATE TABLE comics_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    monitored_id INTEGER NOT NULL REFERENCES comics_monitored(id) ON DELETE CASCADE,
    post_url TEXT NOT NULL, title TEXT NOT NULL, magnet TEXT DEFAULT '',
    torrent_url TEXT DEFAULT '', sent_at TEXT DEFAULT (datetime('now')),
    size_bytes INTEGER DEFAULT 0, UNIQUE(post_url));
CREATE TABLE comics_weekly (
    id INTEGER PRIMARY KEY AUTOINCREMENT, pack_date TEXT NOT NULL UNIQUE,
    magnet TEXT DEFAULT '', torrent_url TEXT DEFAULT '', sent_at TEXT DEFAULT NULL,
    found_at TEXT DEFAULT (datetime('now')), size_bytes INTEGER DEFAULT 0);
CREATE TABLE comics_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
`

// The orphan history row deliberately references a missing monitored id; foreign
// keys are disabled only for that insert so the schema stays faithful.
const legacyComicsInsert = `PRAGMA foreign_keys=OFF;
INSERT INTO comics_monitored(id,title,tag_url,cover_url,publisher,description,from_date,save_path,enabled,added_at,last_checked) VALUES
    (1,'Comic One','/tag/comic-one','','DC','','2026-01-01','/comics',1,'2026-01-01 00:00:00',NULL);
INSERT INTO comics_history(id,monitored_id,post_url,title,magnet,torrent_url,sent_at,size_bytes) VALUES
    (1,1,'https://example.test/post/1','Comic One 001','magnet:?xt=urn:btih:7777777777777777777777777777777777777777','','2026-01-02 00:00:00',100),
    (2,99,'https://example.test/post/2','Orphan','','',NULL,0);
INSERT INTO comics_weekly(id,pack_date,magnet,torrent_url,sent_at,found_at,size_bytes) VALUES
    (1,'2026-01-05','magnet:?xt=urn:btih:8888888888888888888888888888888888888888','',NULL,'2026-01-05 00:00:00',200);
INSERT INTO comics_settings(key,value) VALUES ('comics_enabled','1');
`

func execImporterScript(t *testing.T, db *sql.DB, script string) {
	t.Helper()
	if _, err := db.Exec(script); err != nil {
		t.Fatalf("exec legacy script: %v\n%s", err, script)
	}
}

// openLegacyDb opens a fresh writable legacy database, runs the checkpoint and
// closes it so the importer's read-only snapshot sees all committed data.
func openLegacyDb(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open legacy db %s: %v", path, err)
	}
	return db
}

func closeLegacyDb(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatalf("checkpoint legacy db: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}
}

// createLegacyExttoFixture builds a minimal but faithful `extto` source
// directory with every table the importer reads.
func createLegacyExttoFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "extto")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}

	series := openLegacyDb(t, filepath.Join(source, "extto_series.db"))
	execImporterScript(t, series, legacySeriesSchema)
	execImporterScript(t, series, legacySeriesInsert)
	closeLegacyDb(t, series)

	archive := openLegacyDb(t, filepath.Join(source, "extto_archive.db"))
	execImporterScript(t, archive, legacyArchiveSchema)
	execImporterScript(t, archive, legacyArchiveInsert)
	closeLegacyDb(t, archive)

	configDB := openLegacyDb(t, filepath.Join(source, "extto_config.db"))
	execImporterScript(t, configDB, legacyConfigSchema)
	execImporterScript(t, configDB, legacyConfigInsert)
	closeLegacyDb(t, configDB)

	comics := openLegacyDb(t, filepath.Join(source, "comics.db"))
	execImporterScript(t, comics, legacyComicsSchema)
	execImporterScript(t, comics, legacyComicsInsert)
	closeLegacyDb(t, comics)

	writeBackupFile(t, filepath.Join(source, "extto_torrents_state", "a.fastresume"), "fastresume")
	writeBackupFile(t, filepath.Join(source, "extto_torrents_state", "b.torrent"), "torrent")
	writeBackupFile(t, filepath.Join(source, "extto_torrents_state", "ignore.txt"), "ignored")
	return source
}

func TestImporterImportsLegacyExttoDirectory(t *testing.T) {
	source := createLegacyExttoFixture(t)
	destination := filepath.Join(filepath.Dir(source), "dest")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatalf("mkdir destination: %v", err)
	}

	report, err := ImportExtto(source, destination)
	if err != nil {
		t.Fatalf("ImportExtto: %v", err)
	}

	if !report.ConfigCopied {
		t.Error("ConfigCopied = false, want true")
	}
	if report.ConfigSettings != 2 {
		t.Errorf("ConfigSettings = %d, want 2", report.ConfigSettings)
	}
	if report.ConfigMovies != 1 {
		t.Errorf("ConfigMovies = %d, want 1", report.ConfigMovies)
	}
	if report.ConfigTorrentLimits != 1 {
		t.Errorf("ConfigTorrentLimits = %d, want 1", report.ConfigTorrentLimits)
	}
	if report.Series != 2 {
		t.Errorf("Series = %d, want 2", report.Series)
	}
	if report.Episodes != 2 {
		t.Errorf("Episodes = %d, want 2", report.Episodes)
	}
	if report.Movies != 2 {
		t.Errorf("Movies = %d, want 2", report.Movies)
	}
	if report.Archive != 2 {
		t.Errorf("Archive = %d, want 2", report.Archive)
	}
	if report.TorrentMeta != 2 {
		t.Errorf("TorrentMeta = %d, want 2", report.TorrentMeta)
	}
	if report.TorrentState != 2 {
		t.Errorf("TorrentState = %d, want 2", report.TorrentState)
	}
	if report.Comics.Monitored != 1 {
		t.Errorf("Comics.Monitored = %d, want 1", report.Comics.Monitored)
	}
	if report.Comics.History != 1 {
		t.Errorf("Comics.History = %d, want 1", report.Comics.History)
	}
	if report.Comics.Weekly != 1 {
		t.Errorf("Comics.Weekly = %d, want 1", report.Comics.Weekly)
	}
	if report.Comics.Settings != 1 {
		t.Errorf("Comics.Settings = %d, want 1", report.Comics.Settings)
	}
	if report.Comics.Skipped != 1 {
		t.Errorf("Comics.Skipped = %d, want 1 (orphan history)", report.Comics.Skipped)
	}
	// One empty-named series, one orphan episode and one duplicate archive
	// magnet. The orphan comics history row lands in report.Comics.Skipped.
	if report.Skipped != 3 {
		t.Errorf("Skipped = %d, want 3", report.Skipped)
	}
	if len(report.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none", report.Warnings)
	}

	assertImportedSeriesDatabase(t, destination)
	assertImportedArchiveDatabase(t, destination)
	assertImportedComicsDatabase(t, destination)
	assertImportedConfigDatabase(t, destination)
	assertImportedTorrentState(t, destination)
}

func TestImporterRefusesSourceEqualToDestination(t *testing.T) {
	source := t.TempDir()
	_, err := ImportExtto(source, source)
	if err == nil {
		t.Fatal("ImportExtto into its own source should fail")
	}
	if !strings.Contains(err.Error(), "separate directories") {
		t.Errorf("error = %v, want a 'separate directories' error", err)
	}
}

func TestImporterFailsWithoutSourceDatabase(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "extto")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	destination := filepath.Join(root, "dest")
	_, err := ImportExtto(source, destination)
	if err == nil {
		t.Fatal("ImportExtto without extto_series.db should fail")
	}
	if !strings.Contains(err.Error(), "missing source database") {
		t.Errorf("error = %v, want a 'missing source database' error", err)
	}
}

func assertImportedSeriesDatabase(t *testing.T, destination string) {
	t.Helper()
	db, err := OpenSQLite(filepath.Join(destination, "gextto_series.db"))
	if err != nil {
		t.Fatalf("open destination series db: %v", err)
	}
	defer db.Close()

	if count := importerCount(db, "SELECT COUNT(*) FROM series"); count != 2 {
		t.Errorf("destination series = %d, want 2", count)
	}
	if count := importerCount(db, "SELECT COUNT(*) FROM episodes"); count != 2 {
		t.Errorf("destination episodes = %d, want 2", count)
	}
	if count := importerCount(db, "SELECT COUNT(*) FROM movies"); count != 2 {
		t.Errorf("destination movies = %d, want 2", count)
	}
	if count := importerCount(db, "SELECT COUNT(*) FROM torrent_meta"); count != 2 {
		t.Errorf("destination torrent_meta = %d, want 2", count)
	}

	var seasons, quality, language, archivePath, tmdbID, aliases, ignoredSeasons, subtitle, exclude sql.NullString
	var enabled, timeframe, seasonSubfolders sql.NullInt64
	if err := db.QueryRow("SELECT seasons,quality,language,enabled,archive_path,tmdb_id,aliases,timeframe,ignored_seasons,subtitle,season_subfolders,exclude FROM series WHERE name='Alpha'").Scan(
		&seasons, &quality, &language, &enabled, &archivePath, &tmdbID, &aliases, &timeframe, &ignoredSeasons, &subtitle, &seasonSubfolders, &exclude,
	); err != nil {
		t.Fatalf("read Alpha: %v", err)
	}
	if seasons.String != "1+" || quality.String != "1080p" || language.String != "ita" || !enabled.Valid || enabled.Int64 != 1 {
		t.Errorf("Alpha basics = %q/%q/%q/%v", seasons.String, quality.String, language.String, enabled)
	}
	if archivePath.String != "/srv/alpha" || tmdbID.String != "111" || aliases.String != "a1,a2" {
		t.Errorf("Alpha metadata = %q/%q/%q", archivePath.String, tmdbID.String, aliases.String)
	}
	if !timeframe.Valid || timeframe.Int64 != 24 || ignoredSeasons.String != "[1]" || subtitle.String != "ita" || !seasonSubfolders.Valid || seasonSubfolders.Int64 != 1 {
		t.Errorf("Alpha extras = timeframe=%v ignored=%q subtitle=%q subfolders=%v", timeframe, ignoredSeasons.String, subtitle.String, seasonSubfolders)
	}

	var magnetHash, downloadedAt, episodeArchive string
	var sizeBytes int64
	if err := db.QueryRow("SELECT magnet_hash,downloaded_at,archive_path,size_bytes FROM episodes WHERE series_id=1 AND season=1 AND episode=2").Scan(
		&magnetHash, &downloadedAt, &episodeArchive, &sizeBytes,
	); err != nil {
		t.Fatalf("read imported episode: %v", err)
	}
	if magnetHash != "2222222222222222222222222222222222222222" {
		t.Errorf("episode magnet_hash = %q, want the hash derived from the magnet link", magnetHash)
	}
	if downloadedAt != "2026-01-02 00:00:00" || episodeArchive != "/a2" || sizeBytes != 2000 {
		t.Errorf("episode metadata = %q/%q/%d", downloadedAt, episodeArchive, sizeBytes)
	}

	var movieHash, removedAt string
	if err := db.QueryRow("SELECT magnet_hash,removed_at FROM movies WHERE name='Movie Two'").Scan(&movieHash, &removedAt); err != nil {
		t.Fatalf("read imported movie: %v", err)
	}
	if movieHash != "4444444444444444444444444444444444444444" {
		t.Errorf("movie magnet_hash = %q, want the hash derived from the magnet link", movieHash)
	}
	if removedAt != "2026-02-01 00:00:00" {
		t.Errorf("movie removed_at = %q, want 2026-02-01 00:00:00", removedAt)
	}

	var tag, source, uiState, status string
	var progress float64
	var paused, totalSize, downloaded int64
	if err := db.QueryRow("SELECT tag,source,ui_state,progress,paused,total_size,downloaded,status FROM torrent_meta WHERE hash='upperhash'").Scan(
		&tag, &source, &uiState, &progress, &paused, &totalSize, &downloaded, &status,
	); err != nil {
		t.Fatalf("read imported torrent_meta: %v", err)
	}
	if tag != "tag1" || source != "dl_source1" || uiState != "ui_state1" || progress != 0.5 {
		t.Errorf("torrent_meta basics = %q/%q/%q/%v", tag, source, uiState, progress)
	}
	if paused != 1 || totalSize != 1000 || downloaded != 500 || status != "queued" {
		t.Errorf("torrent_meta extras = paused=%d total=%d downloaded=%d status=%q", paused, totalSize, downloaded, status)
	}
}

func assertImportedArchiveDatabase(t *testing.T, destination string) {
	t.Helper()
	db, err := OpenSQLite(filepath.Join(destination, "gextto_archive.db"))
	if err != nil {
		t.Fatalf("open destination archive db: %v", err)
	}
	defer db.Close()

	if count := importerCount(db, "SELECT COUNT(*) FROM archive"); count != 2 {
		t.Errorf("destination archive = %d, want 2", count)
	}
	var magnetHash string
	var qualityScore int64
	if err := db.QueryRow("SELECT magnet_hash,quality_score FROM archive WHERE title='Arc One'").Scan(&magnetHash, &qualityScore); err != nil {
		t.Fatalf("read imported archive row: %v", err)
	}
	if magnetHash != "5555555555555555555555555555555555555555" || qualityScore != 0 {
		t.Errorf("archive row = %q/%d", magnetHash, qualityScore)
	}
}

func assertImportedComicsDatabase(t *testing.T, destination string) {
	t.Helper()
	db, err := OpenSQLite(filepath.Join(destination, "gextto_comics.db"))
	if err != nil {
		t.Fatalf("open destination comics db: %v", err)
	}
	defer db.Close()

	for _, table := range []string{"comics_monitored", "comics_history", "comics_weekly", "comics_settings"} {
		if count := importerCount(db, "SELECT COUNT(*) FROM "+table); count != 1 {
			t.Errorf("destination %s = %d, want 1", table, count)
		}
	}
	var title, tagURL string
	if err := db.QueryRow("SELECT title,tag_url FROM comics_monitored").Scan(&title, &tagURL); err != nil {
		t.Fatalf("read comics_monitored: %v", err)
	}
	if title != "Comic One" || tagURL != "/tag/comic-one" {
		t.Errorf("comics_monitored = %q/%q", title, tagURL)
	}
	var historyTitle string
	if err := db.QueryRow("SELECT title FROM comics_history").Scan(&historyTitle); err != nil {
		t.Fatalf("read comics_history: %v", err)
	}
	if historyTitle != "Comic One 001" {
		t.Errorf("comics_history title = %q", historyTitle)
	}
}

func assertImportedConfigDatabase(t *testing.T, destination string) {
	t.Helper()
	db, err := OpenSQLite(filepath.Join(destination, "gextto_config.db"))
	if err != nil {
		t.Fatalf("open destination config db: %v", err)
	}
	defer db.Close()

	if count := importerCount(db, "SELECT COUNT(*) FROM settings"); count != 2 {
		t.Errorf("destination settings = %d, want 2", count)
	}
	if count := importerCount(db, "SELECT COUNT(*) FROM translations"); count != 1 {
		t.Errorf("destination translations = %d, want 1", count)
	}
	if count := importerCount(db, "SELECT COUNT(*) FROM movies_config"); count != 1 {
		t.Errorf("destination movies_config = %d, want 1", count)
	}
	if count := importerCount(db, "SELECT COUNT(*) FROM torrent_limits"); count != 1 {
		t.Errorf("destination torrent_limits = %d, want 1", count)
	}
	var value string
	if err := db.QueryRow("SELECT value FROM settings WHERE key='download_dir'").Scan(&value); err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if value != "/downloads" {
		t.Errorf("download_dir = %q, want /downloads", value)
	}
	var translation string
	if err := db.QueryRow("SELECT value FROM translations WHERE key='hello'").Scan(&translation); err != nil {
		t.Fatalf("read translation: %v", err)
	}
	if translation != "Hello" {
		t.Errorf("translation hello = %q, want Hello", translation)
	}
	var name string
	if err := db.QueryRow("SELECT name FROM movies_config").Scan(&name); err != nil {
		t.Fatalf("read movies_config: %v", err)
	}
	if name != "Conf Movie" {
		t.Errorf("movies_config name = %q, want Conf Movie", name)
	}
	var infoHash string
	var dl, ul int64
	if err := db.QueryRow("SELECT info_hash,dl_bytes,ul_bytes FROM torrent_limits").Scan(&infoHash, &dl, &ul); err != nil {
		t.Fatalf("read torrent_limits: %v", err)
	}
	if infoHash != "limithash" || dl != 123 || ul != 456 {
		t.Errorf("torrent_limits = %q/%d/%d, want lowercased hash", infoHash, dl, ul)
	}
}

func assertImportedTorrentState(t *testing.T, destination string) {
	t.Helper()
	stateDir := filepath.Join(destination, "gextto_torrents_state")
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatalf("read torrent state dir: %v", err)
	}
	names := map[string]bool{}
	for _, entry := range entries {
		names[entry.Name()] = true
	}
	if !names["a.fastresume"] || !names["b.torrent"] {
		t.Errorf("torrent state files = %v, want a.fastresume and b.torrent", names)
	}
	if names["ignore.txt"] {
		t.Errorf("non-state file ignore.txt should not have been copied: %v", names)
	}
}
