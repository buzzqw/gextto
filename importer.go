// Package gextto's importer module implements the core module: it migrates
// a legacy `extto` data directory (`extto_series.db`, `extto_archive.db`,
// `extto_config.db`, `comics.db`) into the `gextto` databases. Every SQL
// statement, column mapping and warning message is copied verbatim from the
// source.

package gextto

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/buzzqw/gextto/internal/constants"
	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/utils"
)

// ImportReport implements the `ImportReport`. The JSON field names
// mirror the JSON serialisation exactly.
type ImportReport struct {
	SnapshotDir         string             `json:"snapshot_dir"`
	ConfigCopied        bool               `json:"config_copied"`
	ConfigSettings      int                `json:"config_settings"`
	ConfigMovies        int                `json:"config_movies"`
	ConfigTorrentLimits int                `json:"config_torrent_limits"`
	Series              int                `json:"series"`
	Episodes            int                `json:"episodes"`
	Movies              int                `json:"movies"`
	Archive             int                `json:"archive"`
	TorrentMeta         int                `json:"torrent_meta"`
	TorrentState        int                `json:"torrent_state"`
	Comics              ComicsImportReport `json:"comics"`
	Skipped             int                `json:"skipped"`
	Warnings            []string           `json:"warnings"`
}

// importSnapshots is the Go replacement for the `Snapshots` tuple:
// `(snapshot_dir, archive, comics, config)`.
type importSnapshots struct {
	dir     string
	archive *string
	comics  *string
	config  *string
}

// importerHasTable mirrors the `has_table` helper.
func importerHasTable(db *sql.DB, name string) (bool, error) {
	var exists bool
	err := db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?1)",
		name,
	).Scan(&exists)
	return exists, err
}

// importerColumnOr mirrors the `column_or` helper: it keeps a real column
// when it exists and otherwise substitutes a SQL default expression.
func importerColumnOr(columns map[string]bool, name, fallback string) string {
	if columns[name] {
		return name
	}
	return fallback
}

// importerText mirrors the `text` helper: NULL and unusable values become
// the empty string.
func importerText(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

// importerNumber mirrors the `number` helper: NULL and unusable values
// become zero.
func importerNumber(value sql.NullInt64) int64 {
	if value.Valid {
		return value.Int64
	}
	return 0
}

// importerFloat mirrors `row.get::<_, f64>(index).unwrap_or(0.0)`.
func importerFloat(value sql.NullFloat64) float64 {
	if value.Valid {
		return value.Float64
	}
	return 0
}

// importerAffected is the Go equivalent of the row count returned by
// `rusqlite::Transaction::execute`.
func importerAffected(result sql.Result) int64 {
	affected, err := result.RowsAffected()
	if err != nil {
		return 0
	}
	return affected
}

// importerCount runs a `COUNT(*)` query and defaults to zero on error, matching
// the `unwrap_or(0)` calls of the import.
func importerCount(db *sql.DB, query string) int {
	var count int
	if err := db.QueryRow(query).Scan(&count); err != nil {
		return 0
	}
	return count
}

// importerOpenReadonly mirrors the `readonly` helper: the legacy source
// databases are never modified.
func importerOpenReadonly(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(10000)")
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return db, nil
}

// importerSnapshotDatabase mirrors the `snapshot_database` of importer.rs
// (read-only source). `VACUUM INTO` is the Go equivalent of the online backup
// API used by rusqlite: it produces a transactionally consistent copy.
func importerSnapshotDatabase(source, destination string) error {
	src, err := importerOpenReadonly(source)
	if err != nil {
		return err
	}
	defer src.Close()
	if _, err := src.Exec("VACUUM INTO ?", destination); err != nil {
		return fmt.Errorf("create snapshot %s: %w", destination, err)
	}
	return nil
}

// importerIsFile mirrors `Path::is_file`.
func importerIsFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// makeImportSnapshots mirrors the `make_snapshots`: it copies the source
// databases into a fresh temporary directory so the import never observes a
// live connection.
func makeImportSnapshots(sourceDir string) (importSnapshots, error) {
	snapshotDir, err := os.MkdirTemp("", "gextto-import-")
	if err != nil {
		return importSnapshots{}, err
	}
	series := filepath.Join(sourceDir, "extto_series.db")
	if _, err := os.Stat(series); err != nil {
		return importSnapshots{}, fmt.Errorf("missing source database %s", series)
	}
	if err := importerSnapshotDatabase(series, filepath.Join(snapshotDir, "extto_series.db")); err != nil {
		return importSnapshots{}, err
	}
	snapshots := importSnapshots{dir: snapshotDir}
	optional := []struct {
		name   string
		target **string
	}{
		{"extto_archive.db", &snapshots.archive},
		{"comics.db", &snapshots.comics},
		{"extto_config.db", &snapshots.config},
	}
	for _, item := range optional {
		source := filepath.Join(sourceDir, item.name)
		if !importerIsFile(source) {
			continue
		}
		destination := filepath.Join(snapshotDir, item.name)
		if err := importerSnapshotDatabase(source, destination); err != nil {
			return importSnapshots{}, err
		}
		*item.target = &destination
	}
	return snapshots, nil
}

// importTorrentState mirrors the `import_torrent_state`.
func importTorrentState(sourceDir, destinationDir string, report *ImportReport) {
	parent := filepath.Dir(sourceDir)
	candidates := []string{
		filepath.Join(sourceDir, "extto_torrents_state"),
		filepath.Join(sourceDir, "torrents_state"),
		filepath.Join(parent, "extto_torrents_state"),
	}
	source := ""
	found := false
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			source = candidate
			found = true
			break
		}
	}
	if !found {
		report.Warnings = append(report.Warnings, "torrent state directory not found: active session not imported")
		return
	}
	target := filepath.Join(destinationDir, constants.DefaultStateDir)
	if err := os.MkdirAll(target, 0o755); err != nil {
		report.Warnings = append(report.Warnings, fmt.Sprintf("cannot create torrent state directory %s: %s", target, err))
		return
	}
	copied := 0
	if entries, err := os.ReadDir(source); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			extension := strings.TrimPrefix(filepath.Ext(name), ".")
			if extension != "fastresume" && extension != "torrent" {
				continue
			}
			if copyFile(filepath.Join(source, name), filepath.Join(target, name)) == nil {
				copied++
			}
		}
	}
	report.TorrentState = copied
	logging.Info("imported libtorrent session state", "source", source, "copied", copied)
}

// importerSeries copies the legacy `series` table.
func importerSeries(sourceDB *sql.DB, tx *sql.Tx, report *ImportReport, seriesIDs map[int64]int64) error {
	ok, err := importerHasTable(sourceDB, "series")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	rows, err := sourceDB.Query("SELECT id,name,seasons,quality_requirement,language,enabled,archive_path,tmdb_id,aliases,timeframe,ignored_seasons,subtitle,season_subfolders,exclude FROM series")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var oldID, enabled, timeframe, seasonSubfolders sql.NullInt64
		var name, seasons, quality, language, archivePath, tmdbID, aliases, ignoredSeasons, subtitle, exclude sql.NullString
		if err := rows.Scan(&oldID, &name, &seasons, &quality, &language, &enabled, &archivePath, &tmdbID, &aliases, &timeframe, &ignoredSeasons, &subtitle, &seasonSubfolders, &exclude); err != nil {
			return err
		}
		seriesName := importerText(name)
		if seriesName == "" {
			report.Skipped++
			continue
		}
		result, err := tx.Exec(
			"INSERT INTO series(id,name,seasons,quality,language,enabled,archive_path,tmdb_id,aliases,timeframe,ignored_seasons,subtitle,season_subfolders,exclude) VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,?11,?12,?13,?14) ON CONFLICT(name) DO UPDATE SET seasons=excluded.seasons,quality=excluded.quality,language=excluded.language,enabled=excluded.enabled,archive_path=excluded.archive_path,tmdb_id=excluded.tmdb_id,aliases=excluded.aliases,timeframe=excluded.timeframe,ignored_seasons=excluded.ignored_seasons,subtitle=excluded.subtitle,season_subfolders=excluded.season_subfolders,exclude=excluded.exclude",
			importerNumber(oldID), seriesName, importerText(seasons), importerText(quality), importerText(language), importerNumber(enabled), importerText(archivePath), importerText(tmdbID), importerText(aliases), importerNumber(timeframe), importerText(ignoredSeasons), importerText(subtitle), importerNumber(seasonSubfolders), importerText(exclude),
		)
		if err != nil {
			return err
		}
		var newID int64
		if err := tx.QueryRow("SELECT id FROM series WHERE name=?1", seriesName).Scan(&newID); err != nil {
			return err
		}
		seriesIDs[importerNumber(oldID)] = newID
		if importerAffected(result) > 0 {
			report.Series++
		}
	}
	return rows.Err()
}

// importerEpisodes copies the legacy `episodes` table, remapping `series_id`.
func importerEpisodes(sourceDB *sql.DB, tx *sql.Tx, report *ImportReport, seriesIDs map[int64]int64) error {
	ok, err := importerHasTable(sourceDB, "episodes")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	rows, err := sourceDB.Query("SELECT series_id,season,episode,title,quality_score,is_repack,magnet_hash,magnet_link,downloaded_at,archive_path,size_bytes,original_title,rename_verified FROM episodes")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var oldSID, season, episode, score, repack, size, verified sql.NullInt64
		var title, hash, magnet, downloaded, archivePath, original sql.NullString
		if err := rows.Scan(&oldSID, &season, &episode, &title, &score, &repack, &hash, &magnet, &downloaded, &archivePath, &size, &original, &verified); err != nil {
			return err
		}
		sid, found := seriesIDs[importerNumber(oldSID)]
		if !found {
			report.Skipped++
			continue
		}
		var magnetHash any
		if hashText := importerText(hash); hashText != "" {
			magnetHash = hashText
		} else if computed, ok := utils.MagnetHash(importerText(magnet)); ok {
			magnetHash = computed
		}
		result, err := tx.Exec(
			"INSERT OR IGNORE INTO episodes(series_id,season,episode,title,quality_score,is_repack,magnet_hash,magnet_link,downloaded_at,archive_path,size_bytes,original_title,rename_verified) VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,?11,?12,?13)",
			sid, importerNumber(season), importerNumber(episode), importerText(title), importerNumber(score), importerNumber(repack), magnetHash, importerText(magnet), importerText(downloaded), importerText(archivePath), importerNumber(size), importerText(original), importerNumber(verified),
		)
		if err != nil {
			return err
		}
		if importerAffected(result) > 0 {
			report.Episodes++
		} else {
			report.Skipped++
		}
	}
	return rows.Err()
}

// importerMovies copies the legacy `movies` table.
func importerMovies(sourceDB *sql.DB, tx *sql.Tx, report *ImportReport) error {
	ok, err := importerHasTable(sourceDB, "movies")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	rows, err := sourceDB.Query("SELECT name,year,title,quality_score,magnet_hash,magnet_link,downloaded_at,size_bytes,removed_at FROM movies")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var year, score, size sql.NullInt64
		var name, title, hash, magnet, downloaded, removed sql.NullString
		if err := rows.Scan(&name, &year, &title, &score, &hash, &magnet, &downloaded, &size, &removed); err != nil {
			return err
		}
		var magnetHash any
		if hashText := importerText(hash); hashText != "" {
			magnetHash = hashText
		} else if computed, ok := utils.MagnetHash(importerText(magnet)); ok {
			magnetHash = computed
		}
		var removedAt any
		if removedText := importerText(removed); removedText != "" {
			removedAt = removedText
		}
		result, err := tx.Exec(
			"INSERT OR IGNORE INTO movies(name,year,title,quality_score,magnet_hash,magnet_link,downloaded_at,size_bytes,removed_at) VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9)",
			importerText(name), importerNumber(year), importerText(title), importerNumber(score), magnetHash, importerText(magnet), importerText(downloaded), importerNumber(size), removedAt,
		)
		if err != nil {
			return err
		}
		if importerAffected(result) > 0 {
			report.Movies++
		} else {
			report.Skipped++
		}
	}
	return rows.Err()
}

// importerTorrentMeta copies the legacy `torrent_meta` table, tolerating the
// columns missing from the oldest schema with `column_or` defaults.
func importerTorrentMeta(sourceDB *sql.DB, tx *sql.Tx, report *ImportReport) error {
	ok, err := importerHasTable(sourceDB, "torrent_meta")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	columns, err := tableColumns(sourceDB, "torrent_meta")
	if err != nil {
		return err
	}
	query := fmt.Sprintf(
		"SELECT hash, %s, %s, %s, %s, %s, %s, %s, %s FROM torrent_meta",
		importerColumnOr(columns, "tag", "''"),
		importerColumnOr(columns, "dl_source", "''"),
		importerColumnOr(columns, "ui_state", "''"),
		importerColumnOr(columns, "progress", "0"),
		importerColumnOr(columns, "paused", "0"),
		importerColumnOr(columns, "total_size", "0"),
		importerColumnOr(columns, "downloaded", "0"),
		importerColumnOr(columns, "name", "''"),
	)
	rows, err := sourceDB.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var hash, tag, source, uiState, name sql.NullString
		var progress sql.NullFloat64
		var paused, totalSize, downloaded sql.NullInt64
		if err := rows.Scan(&hash, &tag, &source, &uiState, &progress, &paused, &totalSize, &downloaded, &name); err != nil {
			return err
		}
		hashText := importerText(hash)
		if strings.TrimSpace(hashText) == "" {
			continue
		}
		result, err := tx.Exec(
			"INSERT INTO torrent_meta(hash,tag,source,ui_state,progress,paused,total_size,downloaded,name,status,updated_at) VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,'queued',?10) ON CONFLICT(hash) DO UPDATE SET tag=excluded.tag,source=excluded.source,ui_state=excluded.ui_state,progress=excluded.progress,paused=excluded.paused,total_size=excluded.total_size,downloaded=excluded.downloaded,name=excluded.name,updated_at=excluded.updated_at",
			strings.ToLower(hashText), importerText(tag), importerText(source), importerText(uiState), importerFloat(progress), importerNumber(paused), importerNumber(totalSize), importerNumber(downloaded), importerText(name), nowSQLite(),
		)
		if err != nil {
			return err
		}
		if importerAffected(result) > 0 {
			report.TorrentMeta++
		}
	}
	return rows.Err()
}

// importerImportArchive copies the legacy `archive` table.
func importerImportArchive(archiveDB, targetArchive *sql.DB, report *ImportReport) error {
	rows, err := archiveDB.Query("SELECT title,magnet,source,added_at FROM archive")
	if err != nil {
		return err
	}
	defer rows.Close()
	tx, err := targetArchive.Begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for rows.Next() {
		var title, magnet, source, at sql.NullString
		if err := rows.Scan(&title, &magnet, &source, &at); err != nil {
			return err
		}
		hash := ""
		if computed, ok := utils.MagnetHash(importerText(magnet)); ok {
			hash = computed
		}
		result, err := tx.Exec(
			"INSERT OR IGNORE INTO archive(title,magnet,magnet_hash,source,quality_score,added_at) VALUES (?1,?2,?3,?4,?5,?6)",
			importerText(title), importerText(magnet), hash, importerText(source), int64(0), importerText(at),
		)
		if err != nil {
			return err
		}
		if importerAffected(result) > 0 {
			report.Archive++
		} else {
			report.Skipped++
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// ImportExtto implements the `import_extto`: it imports a legacy
// `extto` data directory into `destinationDir` and reports what was copied.
func ImportExtto(sourceDir, destinationDir string) (ImportReport, error) {
	var report ImportReport
	sourceRoot, err := filepath.EvalSymlinks(sourceDir)
	if err != nil {
		return report, fmt.Errorf("resolve import source %s: %w", sourceDir, err)
	}
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		return report, err
	}
	destinationRoot, err := filepath.EvalSymlinks(destinationDir)
	if err != nil {
		return report, err
	}
	separator := string(os.PathSeparator)
	if sourceRoot == destinationRoot ||
		strings.HasPrefix(sourceRoot, destinationRoot+separator) ||
		strings.HasPrefix(destinationRoot, sourceRoot+separator) {
		return report, fmt.Errorf("import source and destination must be separate directories")
	}
	snapshots, err := makeImportSnapshots(sourceDir)
	if err != nil {
		return report, err
	}
	defer func() { _ = os.RemoveAll(snapshots.dir) }()

	sourceDB, err := importerOpenReadonly(filepath.Join(snapshots.dir, "extto_series.db"))
	if err != nil {
		return report, err
	}
	defer sourceDB.Close()

	targetPath := filepath.Join(destinationDir, "gextto_series.db")
	// Migrate the destination schema before opening the transaction used by the
	// import. The migration connection is closed right away, exactly like the
	// dropped `Database` of the implementation.
	migration, err := OpenDatabase(targetPath)
	if err != nil {
		return report, err
	}
	_ = migration.db.Close()

	target, err := OpenSQLite(targetPath)
	if err != nil {
		return report, err
	}
	defer target.Close()
	if _, err := target.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return report, err
	}
	_, _ = target.Exec("PRAGMA busy_timeout=5000")

	tx, err := target.Begin()
	if err != nil {
		return report, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	report = ImportReport{SnapshotDir: snapshots.dir, Warnings: []string{}}
	seriesIDs := map[int64]int64{}

	if err := importerSeries(sourceDB, tx, &report, seriesIDs); err != nil {
		return report, err
	}
	if err := importerEpisodes(sourceDB, tx, &report, seriesIDs); err != nil {
		return report, err
	}
	if err := importerMovies(sourceDB, tx, &report); err != nil {
		return report, err
	}
	if err := importerTorrentMeta(sourceDB, tx, &report); err != nil {
		return report, err
	}
	if err := tx.Commit(); err != nil {
		return report, err
	}
	committed = true

	if snapshots.archive != nil {
		archiveDB, err := importerOpenReadonly(*snapshots.archive)
		if err != nil {
			return report, err
		}
		defer archiveDB.Close()
		targetArchive, err := OpenSQLite(filepath.Join(destinationDir, "gextto_archive.db"))
		if err != nil {
			return report, err
		}
		defer targetArchive.Close()
		_, _ = targetArchive.Exec("PRAGMA busy_timeout=5000")
		if _, err := targetArchive.Exec("CREATE TABLE IF NOT EXISTS archive (id INTEGER PRIMARY KEY, title TEXT NOT NULL, magnet TEXT NOT NULL UNIQUE, magnet_hash TEXT, source TEXT, quality_score INTEGER, added_at TEXT NOT NULL);"); err != nil {
			return report, err
		}
		ok, err := importerHasTable(archiveDB, "archive")
		if err != nil {
			return report, err
		}
		if ok {
			if err := importerImportArchive(archiveDB, targetArchive, &report); err != nil {
				return report, err
			}
		}
	}

	comicsConn, err := OpenSQLite(filepath.Join(destinationDir, "gextto_comics.db"))
	if err != nil {
		return report, err
	}
	defer comicsConn.Close()
	_, _ = comicsConn.Exec("PRAGMA busy_timeout=5000")
	comicsReport, err := ImportFromExtto(snapshots.dir, comicsConn)
	if err != nil {
		return report, err
	}
	report.Comics = comicsReport

	if snapshots.config != nil {
		if err := importExttoConfig(*snapshots.config, destinationDir, &report); err != nil {
			return report, err
		}
	}

	importTorrentState(sourceDir, destinationDir, &report)
	return report, nil
}

// importExttoConfig copies the legacy `extto_config.db` into `gextto_config.db`
// (only when the destination configuration is still fresh).
func importExttoConfig(configSnapshot, destinationDir string, report *ImportReport) error {
	configConn, err := importerOpenReadonly(configSnapshot)
	if err != nil {
		return err
	}
	defer configConn.Close()
	report.ConfigSettings = importerCount(configConn, "SELECT COUNT(*) FROM settings")
	report.ConfigMovies = importerCount(configConn, "SELECT COUNT(*) FROM movies_config")
	report.ConfigTorrentLimits = importerCount(configConn, "SELECT COUNT(*) FROM torrent_limits")

	targetConfig, err := OpenSQLite(filepath.Join(destinationDir, "gextto_config.db"))
	if err != nil {
		return err
	}
	defer targetConfig.Close()
	_, _ = targetConfig.Exec("PRAGMA busy_timeout=5000")
	if _, err := targetConfig.Exec("CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL); CREATE TABLE IF NOT EXISTS translations (lang TEXT NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL, PRIMARY KEY(lang,key)); CREATE TABLE IF NOT EXISTS movies_config (id INTEGER PRIMARY KEY, name TEXT NOT NULL, year TEXT DEFAULT '', quality TEXT DEFAULT '', language TEXT DEFAULT '', enabled INTEGER DEFAULT 1, subtitle TEXT DEFAULT '', exclude TEXT DEFAULT '', language_requirements TEXT DEFAULT '', subtitle_requirements TEXT DEFAULT ''); CREATE TABLE IF NOT EXISTS torrent_limits (info_hash TEXT PRIMARY KEY, dl_bytes INTEGER NOT NULL DEFAULT -1, ul_bytes INTEGER NOT NULL DEFAULT -1, updated_at TEXT NOT NULL DEFAULT (datetime('now')));"); err != nil {
		return err
	}
	_, _ = targetConfig.Exec("ALTER TABLE movies_config ADD COLUMN language_requirements TEXT DEFAULT ''")
	_, _ = targetConfig.Exec("ALTER TABLE movies_config ADD COLUMN subtitle_requirements TEXT DEFAULT ''")

	var existingSettings, existingTranslations, existingMovies int64
	if err := targetConfig.QueryRow("SELECT COUNT(*) FROM settings").Scan(&existingSettings); err != nil {
		return err
	}
	if err := targetConfig.QueryRow("SELECT COUNT(*) FROM translations").Scan(&existingTranslations); err != nil {
		return err
	}
	if err := targetConfig.QueryRow("SELECT COUNT(*) FROM movies_config").Scan(&existingMovies); err != nil {
		return err
	}
	if !(existingSettings == 0 && existingTranslations == 0 && existingMovies == 0) {
		report.Warnings = append(report.Warnings, "gextto_config.db esiste gia': configurazione preservata")
		return nil
	}

	if err := copyExttoSettings(configConn, targetConfig); err != nil {
		return err
	}
	if err := copyExttoTranslations(configConn, targetConfig); err != nil {
		return err
	}
	if err := copyExttoMoviesConfig(configConn, targetConfig); err != nil {
		return err
	}
	if err := copyExttoTorrentLimits(configConn, targetConfig); err != nil {
		return err
	}
	report.ConfigCopied = true
	return nil
}

// copyExttoSettings mirrors the `settings` copy of the importer.
func copyExttoSettings(configConn, targetConfig *sql.DB) error {
	ok, err := importerHasTable(configConn, "settings")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	rows, err := configConn.Query("SELECT key,value FROM settings")
	if err != nil {
		return err
	}
	type setting struct{ key, value string }
	var settings []setting
	for rows.Next() {
		var key, value sql.NullString
		if err := rows.Scan(&key, &value); err != nil {
			rows.Close()
			return err
		}
		settings = append(settings, setting{importerText(key), importerText(value)})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range settings {
		if _, err := targetConfig.Exec("INSERT OR REPLACE INTO settings(key,value) VALUES (?1,?2)", item.key, item.value); err != nil {
			return err
		}
	}
	return nil
}

// copyExttoTranslations mirrors the `translations` copy of the importer.
func copyExttoTranslations(configConn, targetConfig *sql.DB) error {
	ok, err := importerHasTable(configConn, "translations")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	rows, err := configConn.Query("SELECT lang,key,value FROM translations")
	if err != nil {
		return err
	}
	type translation struct{ lang, key, value string }
	var translations []translation
	for rows.Next() {
		var lang, key, value sql.NullString
		if err := rows.Scan(&lang, &key, &value); err != nil {
			rows.Close()
			return err
		}
		translations = append(translations, translation{importerText(lang), importerText(key), importerText(value)})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range translations {
		if _, err := targetConfig.Exec("INSERT OR REPLACE INTO translations(lang,key,value) VALUES (?1,?2,?3)", item.lang, item.key, item.value); err != nil {
			return err
		}
	}
	return nil
}

// copyExttoMoviesConfig mirrors the `movies_config` copy of the importer,
// tolerating old schemas through `column_or`.
func copyExttoMoviesConfig(configConn, targetConfig *sql.DB) error {
	ok, err := importerHasTable(configConn, "movies_config")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	columns, err := tableColumns(configConn, "movies_config")
	if err != nil {
		return err
	}
	query := fmt.Sprintf(
		"SELECT %s,%s,%s,%s,%s,%s,%s,%s,%s,%s FROM movies_config",
		importerColumnOr(columns, "id", "0"),
		importerColumnOr(columns, "name", "''"),
		importerColumnOr(columns, "year", "''"),
		importerColumnOr(columns, "quality", "''"),
		importerColumnOr(columns, "language", "''"),
		importerColumnOr(columns, "enabled", "1"),
		importerColumnOr(columns, "subtitle", "''"),
		importerColumnOr(columns, "exclude", "''"),
		importerColumnOr(columns, "language_requirements", "''"),
		importerColumnOr(columns, "subtitle_requirements", "''"),
	)
	rows, err := configConn.Query(query)
	if err != nil {
		return err
	}
	type movieConfig struct {
		id, enabled                                                       int64
		name, year, quality, language, subtitle, exclude, langReq, subReq string
	}
	var movies []movieConfig
	for rows.Next() {
		var id, enabled sql.NullInt64
		var name, year, quality, language, subtitle, exclude, langReq, subReq sql.NullString
		if err := rows.Scan(&id, &name, &year, &quality, &language, &enabled, &subtitle, &exclude, &langReq, &subReq); err != nil {
			rows.Close()
			return err
		}
		movies = append(movies, movieConfig{
			id:       importerNumber(id),
			name:     importerText(name),
			year:     importerText(year),
			quality:  importerText(quality),
			language: importerText(language),
			enabled:  importerNumber(enabled),
			subtitle: importerText(subtitle),
			exclude:  importerText(exclude),
			langReq:  importerText(langReq),
			subReq:   importerText(subReq),
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range movies {
		if _, err := targetConfig.Exec("INSERT OR REPLACE INTO movies_config(id,name,year,quality,language,enabled,subtitle,exclude,language_requirements,subtitle_requirements) VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10)", item.id, item.name, item.year, item.quality, item.language, item.enabled, item.subtitle, item.exclude, item.langReq, item.subReq); err != nil {
			return err
		}
	}
	return nil
}

// copyExttoTorrentLimits mirrors the `torrent_limits` copy of the
// importer; blank hashes are skipped and the rest lower-cased.
func copyExttoTorrentLimits(configConn, targetConfig *sql.DB) error {
	ok, err := importerHasTable(configConn, "torrent_limits")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	columns, err := tableColumns(configConn, "torrent_limits")
	if err != nil {
		return err
	}
	query := fmt.Sprintf(
		"SELECT %s,%s,%s FROM torrent_limits",
		importerColumnOr(columns, "info_hash", "''"),
		importerColumnOr(columns, "dl_bytes", "-1"),
		importerColumnOr(columns, "ul_bytes", "-1"),
	)
	rows, err := configConn.Query(query)
	if err != nil {
		return err
	}
	type torrentLimit struct {
		hash   string
		dl, ul int64
	}
	var limits []torrentLimit
	for rows.Next() {
		var hash sql.NullString
		var dl, ul sql.NullInt64
		if err := rows.Scan(&hash, &dl, &ul); err != nil {
			rows.Close()
			return err
		}
		limits = append(limits, torrentLimit{importerText(hash), importerNumber(dl), importerNumber(ul)})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range limits {
		if strings.TrimSpace(item.hash) == "" {
			continue
		}
		if _, err := targetConfig.Exec("INSERT OR REPLACE INTO torrent_limits(info_hash,dl_bytes,ul_bytes) VALUES (?1,?2,?3)", strings.ToLower(item.hash), item.dl, item.ul); err != nil {
			return err
		}
	}
	return nil
}
