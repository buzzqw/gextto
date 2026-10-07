// Package gextto's database module implements the core module: the
// SQLite-backed state store for series, movies, torrents, feed history and
// provider backoff. Every SQL statement, column name, default and behaviour is
// preserved from the original implementation.

package gextto

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/backoff"
	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

const downloadHistoryRetentionDays int64 = 30

// DefaultUpgradeMinScoreDiff is the default upgrade threshold used by callers
// without a `Config`.
const DefaultUpgradeMinScoreDiff int64 = 200

// maxDatabaseInt64 is `i64::MAX`.
const maxDatabaseInt64 = int64(^uint64(0) >> 1)

// ReadyPending implements the `ReadyPending` tuple:
// `(series name, title, magnet, season, episode)`.
type ReadyPending struct {
	Name    string
	Title   string
	Magnet  string
	Season  int64
	Episode int64
}

// ReadyPendingMovie implements the `ReadyPendingMovie` tuple:
// `(name, title, magnet, year)`.
type ReadyPendingMovie struct {
	Name   string
	Title  string
	Magnet string
	Year   *int64
}

// SeriesSummary implements the `SeriesSummary` tuple:
// `(name, total episodes, downloaded episodes, last download)`.
type SeriesSummary struct {
	Name           string
	Episodes       int64
	Downloaded     int64
	LastDownloaded *string
}

// SeriesGap is one gap in a series: `(series, season, episode)`.
type SeriesGap struct {
	Series  string `json:"series"`
	Season  int64  `json:"season"`
	Episode int64  `json:"episode"`
}

// EpisodeAirDate is one cached air date: `(season, episode, air_date)`.
type EpisodeAirDate struct {
	Season  int64
	Episode int64
	AirDate string
}

// PackEpisode is one completed episode of a season pack:
// `(episode, path, size_bytes, quality_score)`.
type PackEpisode struct {
	Episode   int64
	Path      string
	SizeBytes int64
	Score     int64
}

// TorrentTimes is the `(created_at, completed_at)` pair of a torrent.
type TorrentTimes struct {
	CreatedAt   string
	CompletedAt *string
}

// airDateKey is the `(series, season, episode)` map key of the air-date cache.
type airDateKey struct {
	Series  string
	Season  int64
	Episode int64
}

// MaintenanceReport is the result of `cleanup`.
type MaintenanceReport struct {
	Rescored             int `json:"rescored"`
	OldCyclesRemoved     int `json:"old_cycles_removed"`
	StaleTorrentsRemoved int `json:"stale_torrents_removed"`
}

// HousekeepingParams are the retention knobs for a housekeeping run. `0`
// disables the corresponding cleanup so the user can keep as much history as
// they want.
type HousekeepingParams struct {
	RetainCycles      int64 `json:"retain_cycles"`
	ErrorAgeDays      int64 `json:"error_age_days"`
	SeenDays          int64 `json:"seen_days"`
	GapLogDays        int64 `json:"gap_log_days"`
	UpgradeBackupDays int64 `json:"upgrade_backup_days"`
	HistoryDays       int64 `json:"history_days"`
}

// DefaultHousekeepingParams mirrors `impl Default for HousekeepingParams`.
func DefaultHousekeepingParams() HousekeepingParams {
	return HousekeepingParams{
		RetainCycles:      200,
		ErrorAgeDays:      7,
		SeenDays:          30,
		GapLogDays:        30,
		UpgradeBackupDays: 30,
		HistoryDays:       0,
	}
}

// FromSettings builds the retention knobs from the settings map.
func (p HousekeepingParams) FromSettings(settings map[string]string) HousekeepingParams {
	number := func(key string, def int64) int64 {
		if value, ok := settings[key]; ok {
			if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
				return parsed
			}
		}
		return def
	}
	defaults := DefaultHousekeepingParams()
	return HousekeepingParams{
		RetainCycles:      clampInt64(number("housekeeping_retain_cycles", defaults.RetainCycles), 1, 100_000),
		ErrorAgeDays:      maxInt64(number("housekeeping_error_age_days", defaults.ErrorAgeDays), 1),
		SeenDays:          maxInt64(number("housekeeping_seen_days", defaults.SeenDays), 0),
		GapLogDays:        maxInt64(number("housekeeping_gap_log_days", defaults.GapLogDays), 0),
		UpgradeBackupDays: maxInt64(number("housekeeping_upgrade_backup_days", defaults.UpgradeBackupDays), 0),
		HistoryDays:       maxInt64(number("housekeeping_history_days", defaults.HistoryDays), 0),
	}
}

// MediaInfoBackfillTarget is one archived entry to (re)probe for the MediaInfo
// backfill.
type MediaInfoBackfillTarget struct {
	Kind    string `json:"kind"`
	Series  string `json:"series"`
	Season  *int64 `json:"season"`
	Episode *int64 `json:"episode"`
	Name    string `json:"name"`
	Year    *int64 `json:"year"`
	Path    string `json:"path"`
}

// displayName names the library item for a person: "Show S01E02" or
// "Movie (2024)".
func (t MediaInfoBackfillTarget) displayName() string {
	if t.Kind == "movie" {
		if t.Year != nil && *t.Year > 0 {
			return fmt.Sprintf("%s (%d)", t.Name, *t.Year)
		}
		return t.Name
	}
	if t.Season != nil && t.Episode != nil {
		return fmt.Sprintf("%s S%02dE%02d", t.Series, *t.Season, *t.Episode)
	}
	return t.Series
}

// HousekeepingReport is the serialisable result of `housekeeping`.
type HousekeepingReport struct {
	OldCyclesRemoved      int `json:"old_cycles_removed"`
	StaleTorrentsRemoved  int `json:"stale_torrents_removed"`
	SeenRemoved           int `json:"seen_removed"`
	GapLogsRemoved        int `json:"gap_logs_removed"`
	UpgradeBackupsRemoved int `json:"upgrade_backups_removed"`
	OldHistoryRemoved     int `json:"old_history_removed"`
	StaleProvidersRemoved int `json:"stale_providers_removed"`
}

// PrunePreview counts what a maintenance run *would* remove.
type PrunePreview struct {
	OldCyclesToRemove     int `json:"old_cycles_to_remove"`
	StaleTorrentsToRemove int `json:"stale_torrents_to_remove"`
	SeenToRemove          int `json:"seen_to_remove"`
}

// StoredTorrent is one row of `torrent_meta` for the UI/API.
type StoredTorrent struct {
	Hash          string  `json:"hash"`
	Name          string  `json:"name"`
	Tag           string  `json:"tag"`
	Source        string  `json:"source"`
	Progress      float64 `json:"progress"`
	Paused        bool    `json:"paused"`
	TotalSize     int64   `json:"total_size"`
	Downloaded    int64   `json:"downloaded"`
	Status        string  `json:"status"`
	UpdatedAt     string  `json:"updated_at"`
	Kind          string  `json:"kind"`
	SeriesName    string  `json:"series_name"`
	Season        int64   `json:"season"`
	Episode       int64   `json:"episode"`
	Year          int64   `json:"year"`
	QualityScore  int64   `json:"quality_score"`
	CompletedAt   string  `json:"completed_at"`
	ProcessedPath string  `json:"processed_path"`
	Error         string  `json:"error"`
	Reason        string  `json:"reason"`
}

// EpisodeView is one episode of the series detail view.
type EpisodeView struct {
	ID           int64   `json:"id"`
	SeriesName   string  `json:"series_name"`
	Season       int64   `json:"season"`
	Episode      int64   `json:"episode"`
	Title        string  `json:"title"`
	AirDate      string  `json:"air_date"`
	RenamedTitle string  `json:"renamed_title"`
	QualityScore int64   `json:"quality_score"`
	DownloadedAt *string `json:"downloaded_at"`
	ArchivePath  *string `json:"archive_path"`
	SizeBytes    int64   `json:"size_bytes"`
	MagnetHash   *string `json:"magnet_hash"`
	MagnetLink   *string `json:"magnet_link"`
	Status       string  `json:"status"`
	Error        string  `json:"error"`
	Ignored      bool    `json:"ignored"`
}

// MovieHistory is one downloaded movie of the history view.
type MovieHistory struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	Year         *int64  `json:"year"`
	Title        string  `json:"title"`
	QualityScore int64   `json:"quality_score"`
	DownloadedAt *string `json:"downloaded_at"`
	SizeBytes    int64   `json:"size_bytes"`
	MagnetHash   *string `json:"magnet_hash"`
}

// DailyConsumption is one day of consumption.
type DailyConsumption struct {
	Date  string `json:"date"`
	Bytes int64  `json:"bytes"`
}

// ConsumptionStats is the aggregate download consumption.
type ConsumptionStats struct {
	TotalBytes      int64              `json:"total_bytes"`
	Last30DaysBytes int64              `json:"last_30_days_bytes"`
	Last7DaysBytes  int64              `json:"last_7_days_bytes"`
	Daily7d         []DailyConsumption `json:"daily_7d"`
}

// RecentDownload is one entry of the merged recent-download list.
type RecentDownload struct {
	Kind         string  `json:"kind"`
	Name         string  `json:"name"`
	Season       *int64  `json:"season"`
	Episode      *int64  `json:"episode"`
	Year         *int64  `json:"year"`
	DownloadedAt string  `json:"downloaded_at"`
	SizeBytes    int64   `json:"size_bytes"`
	ArchivePath  *string `json:"archive_path"`
	QualityScore int64   `json:"quality_score"`
}

// FeedSeenGroup is a group of "seen in feed" releases (same title, optionally
// with a TMDB id).
type FeedSeenGroup struct {
	GroupKey       string `json:"group_key"`
	GroupName      string `json:"group_name"`
	Year           int64  `json:"year"`
	Season         int64  `json:"season"`
	Count          int64  `json:"count"`
	BestScore      int64  `json:"best_score"`
	BestResolution string `json:"best_resolution"`
	LatestFound    string `json:"latest_found"`
	FirstFound     string `json:"first_found"`
}

// FeedSeenEntry is one release "seen in feed".
type FeedSeenEntry struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	Name         string `json:"name"`
	Year         int64  `json:"year"`
	Season       int64  `json:"season"`
	Episode      int64  `json:"episode"`
	Resolution   string `json:"resolution"`
	Codec        string `json:"codec"`
	Audio        string `json:"audio"`
	QualityScore int64  `json:"quality_score"`
	Magnet       string `json:"magnet"`
	Source       string `json:"source"`
	FoundAt      string `json:"found_at"`
}

// upgradeBackup is the private `UpgradeBackup` payload stored in
// `upgrade_backup.payload_json`.
type upgradeBackup struct {
	Kind         string  `json:"kind"`
	RowID        int64   `json:"row_id"`
	SeriesID     *int64  `json:"series_id"`
	SeriesName   *string `json:"series_name"`
	Season       *int64  `json:"season"`
	Episode      *int64  `json:"episode"`
	Name         *string `json:"name"`
	Year         *int64  `json:"year"`
	Title        string  `json:"title"`
	QualityScore int64   `json:"quality_score"`
	MagnetHash   *string `json:"magnet_hash"`
	MagnetLink   *string `json:"magnet_link"`
	DownloadedAt *string `json:"downloaded_at"`
	ArchivePath  *string `json:"archive_path"`
	SizeBytes    int64   `json:"size_bytes"`
}

// sqlExecer is satisfied by both *sql.DB and *sql.Tx, so helpers can run inside
// or outside a transaction.
type sqlExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// sqlQueryRower is satisfied by both *sql.DB and *sql.Tx.
type sqlQueryRower interface {
	QueryRow(query string, args ...any) *sql.Row
}

type sqlQueryExecer interface {
	sqlExecer
	sqlQueryRower
}

// nowSQLite is the canonical in-database timestamp format.
func nowSQLite() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05")
}

func sqliteTimestamp(value time.Time) string {
	return value.UTC().Format("2006-01-02 15:04:05")
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	v := value.String
	return &v
}

func nullInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	v := value.Int64
	return &v
}

func magnetHashOrError(magnet string) (string, error) {
	hash, ok := utils.MagnetHash(magnet)
	if !ok {
		return "", fmt.Errorf("invalid magnet hash")
	}
	return hash, nil
}

func parseOptionalYear(value string) *int64 {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

// fileStats is the real size and default quality score of a file on disk,
// derived from its name. Missing files yield zeros so callers can keep the
// stored values.
func fileStats(cfg *Config, path, kind, title string) (int64, int64) {
	sizeBytes := int64(0)
	if info, err := os.Stat(path); err == nil {
		size := info.Size()
		if size < 0 {
			size = 0
		}
		sizeBytes = size
	}
	return sizeBytes, cfg.FileScore(path, kind, title)
}

func archivedRelease(title, path string, quality models.Quality, kind string, year *int64) models.Release {
	sizeBytes := int64(0)
	if info, err := os.Stat(path); err == nil {
		size := info.Size()
		if size < 0 {
			size = 0
		}
		sizeBytes = size
	}
	return models.Release{
		TorrentURL:   nil,
		Title:        title,
		Magnet:       "",
		Source:       "archive",
		Quality:      quality,
		Kind:         kind,
		Series:       nil,
		Season:       nil,
		Episode:      nil,
		IsPack:       false,
		EpisodeRange: []int64{},
		Year:         year,
		DiscoveredAt: time.Now().UTC(),
		SizeBytes:    sizeBytes,
		Seeders:      -1,
		Peers:        -1,
	}
}

// renamedFileTitle is the name of the renamed library file, derived from the
// archived path. Returns empty for empty paths or folders (old imports).
func renamedFileTitle(path string) string {
	trimmed := strings.TrimRight(path, `/\`)
	if trimmed == "" {
		return ""
	}
	base := trimmed
	if index := strings.LastIndexAny(trimmed, `/\`); index >= 0 {
		base = trimmed[index+1:]
	}
	lower := strings.ToLower(base)
	isMedia := false
	for _, extension := range []string{".mkv", ".mp4", ".avi", ".m4v", ".ts", ".mov", ".wmv", ".flv"} {
		if strings.HasSuffix(lower, extension) {
			isMedia = true
			break
		}
	}
	if !isMedia {
		return ""
	}
	if index := strings.LastIndex(base, "."); index >= 0 {
		return base[:index]
	}
	return base
}

// enrichQualityWithMediaInfo adds stored `ffprobe` data to a quality parsed from
// a title. Additive only: it fills values the filename did not provide.
func enrichQualityWithMediaInfo(raw string, quality *models.Quality) {
	if strings.TrimSpace(raw) == "" {
		return
	}
	var info MediaInfo
	if err := json.Unmarshal([]byte(raw), &info); err == nil {
		info.ApplyToQuality(quality)
	}
}

func meaningfulQuality(text string) (models.Quality, bool) {
	quality := ParseQuality(text)
	if quality.Resolution != "unknown" ||
		quality.Source != "unknown" ||
		quality.Codec != "unknown" ||
		quality.Audio != "unknown" ||
		quality.HDR != "" ||
		quality.Score() > 0 {
		return quality, true
	}
	return quality, false
}

func containsInt64Value(values []int64, needle int64) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func seenLikePattern(query string) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return "%"
	}
	if strings.Contains(trimmed, "*") || strings.Contains(trimmed, "?") {
		return strings.NewReplacer("*", "%", "?", "_").Replace(trimmed)
	}
	return "%" + trimmed + "%"
}

func normalizedKeywordTerms(keywords []string) []string {
	terms := make([]string, 0, len(keywords))
	for _, value := range keywords {
		value = strings.TrimSpace(value)
		if value != "" && len(value) <= 128 {
			terms = append(terms, value)
		}
	}
	return terms
}

func keywordClause(columns []string, termCount int) string {
	parts := make([]string, 0, termCount)
	for i := 0; i < termCount; i++ {
		clauses := make([]string, 0, len(columns))
		for _, column := range columns {
			clauses = append(clauses, column+" LIKE ?")
		}
		parts = append(parts, "("+strings.Join(clauses, " OR ")+")")
	}
	return strings.Join(parts, " OR ")
}

func keywordBindings(keywords, columns []string) []any {
	bindings := make([]any, 0, len(keywords)*len(columns))
	for _, keyword := range keywords {
		pattern := "%" + keyword + "%"
		for range columns {
			bindings = append(bindings, pattern)
		}
	}
	return bindings
}

// OptimizeConnection runs `VACUUM`/`ANALYZE` on an arbitrary connection.
func OptimizeConnection(db *sql.DB, action string) error {
	var statement string
	switch action {
	case "vacuum":
		statement = "VACUUM;"
	case "analyze":
		statement = "ANALYZE;"
	default:
		return fmt.Errorf("unsupported database action: %s", action)
	}
	_, err := db.Exec(statement)
	return err
}

// CheckpointConnection checkpoints the WAL and truncates the `-wal` file.
func CheckpointConnection(db *sql.DB) error {
	_, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE);")
	return err
}

// HardenConnection sets the durability/concurrency pragmas shared by all Gextto
// databases.
func HardenConnection(db *sql.DB) error {
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		return err
	}
	if _, err := db.Exec("PRAGMA synchronous=FULL"); err != nil {
		return err
	}
	if _, err := db.Exec("PRAGMA journal_size_limit=67108864"); err != nil {
		return err
	}
	if _, err := db.Exec("PRAGMA wal_autocheckpoint=1000"); err != nil {
		return err
	}
	return nil
}

// QuickCheck runs `PRAGMA quick_check` and returns the rows (`["ok"]` when
// healthy).
func QuickCheck(db *sql.DB) ([]string, error) {
	rows, err := db.Query("PRAGMA quick_check")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]string, 0, 1)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		results = append(results, value)
	}
	return results, rows.Err()
}

// ConnectionSizeBytes estimates the database size in bytes
// (page_count × page_size).
func ConnectionSizeBytes(db *sql.DB) int64 {
	var pages int64
	_ = db.QueryRow("PRAGMA page_count").Scan(&pages)
	var pageSize int64
	_ = db.QueryRow("PRAGMA page_size").Scan(&pageSize)
	if pageSize != 0 && pages > maxDatabaseInt64/pageSize {
		return maxDatabaseInt64
	}
	return pages * pageSize
}

// ConnectionRowCount returns the total number of rows in the user tables of a
// SQLite connection. SQLite/FTS internal tables are excluded from the count.
func ConnectionRowCount(db *sql.DB) int64 {
	if db == nil {
		return 0
	}
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE '%_fts%'`)
	if err != nil {
		return 0
	}
	tableNames := make([]string, 0)
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			_ = rows.Close()
			return 0
		}
		tableNames = append(tableNames, tableName)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0
	}
	if err := rows.Close(); err != nil {
		return 0
	}
	var total int64
	for _, tableName := range tableNames {
		quoted := `"` + strings.ReplaceAll(tableName, `"`, `""`) + `"`
		var count int64
		if err := db.QueryRow("SELECT COUNT(*) FROM " + quoted).Scan(&count); err != nil {
			continue
		}
		total += count
	}
	return total
}

// databaseSchemaBase is the first `execute_batch` of `Database::migrate`
// (copied verbatim ).
const databaseSchemaBase = `CREATE TABLE IF NOT EXISTS schema_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS series (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, seasons TEXT DEFAULT '1+', quality TEXT DEFAULT '', language TEXT DEFAULT 'ita', enabled INTEGER DEFAULT 1, archive_path TEXT DEFAULT '', tmdb_id TEXT DEFAULT '', aliases TEXT DEFAULT ''); CREATE TABLE IF NOT EXISTS episodes (id INTEGER PRIMARY KEY, series_id INTEGER NOT NULL, season INTEGER NOT NULL, episode INTEGER NOT NULL, title TEXT, quality_score INTEGER NOT NULL DEFAULT 0, is_repack INTEGER DEFAULT 0, magnet_hash TEXT UNIQUE, magnet_link TEXT, downloaded_at TEXT, archive_path TEXT, size_bytes INTEGER DEFAULT 0, original_title TEXT, rename_verified INTEGER DEFAULT 0, UNIQUE(series_id, season, episode)); CREATE TABLE IF NOT EXISTS movies (id INTEGER PRIMARY KEY, name TEXT, year INTEGER, title TEXT, quality_score INTEGER DEFAULT 0, magnet_hash TEXT UNIQUE, magnet_link TEXT, downloaded_at TEXT, size_bytes INTEGER DEFAULT 0, removed_at TEXT); CREATE TABLE IF NOT EXISTS pending_downloads (id INTEGER PRIMARY KEY, series_id INTEGER, season INTEGER, episode INTEGER, best_magnet TEXT, best_quality_score INTEGER, ready_at TEXT); CREATE TABLE IF NOT EXISTS cycle_history (id INTEGER PRIMARY KEY, at TEXT NOT NULL, payload_json TEXT NOT NULL); CREATE TABLE IF NOT EXISTS torrent_meta (hash TEXT PRIMARY KEY, tag TEXT DEFAULT '', source TEXT DEFAULT '', ui_state TEXT DEFAULT '', progress REAL DEFAULT 0, paused INTEGER DEFAULT 0, total_size INTEGER DEFAULT 0, downloaded INTEGER DEFAULT 0, name TEXT DEFAULT '', kind TEXT DEFAULT '', title TEXT DEFAULT '', series_name TEXT DEFAULT '', season INTEGER, episode INTEGER, year INTEGER, quality_score INTEGER DEFAULT 0, metadata_json TEXT DEFAULT '', status TEXT NOT NULL DEFAULT 'queued', completed_at TEXT, processed_path TEXT, error TEXT DEFAULT '', created_at TEXT NOT NULL DEFAULT (datetime('now')), updated_at TEXT NOT NULL); CREATE INDEX IF NOT EXISTS idx_episodes_lookup ON episodes(series_id, season, episode); CREATE INDEX IF NOT EXISTS idx_episodes_magnet ON episodes(magnet_hash); CREATE INDEX IF NOT EXISTS idx_episodes_downloaded ON episodes(downloaded_at); CREATE INDEX IF NOT EXISTS idx_movies_magnet ON movies(magnet_hash); CREATE INDEX IF NOT EXISTS idx_movies_removed ON movies(removed_at); CREATE INDEX IF NOT EXISTS idx_torrent_meta_status ON torrent_meta(status); INSERT INTO schema_meta(key,value,updated_at) VALUES ('schema_version','2',datetime('now')) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at;`

// databaseSchemaExtra is the second `execute_batch` of `Database::migrate`.
const databaseSchemaExtra = `CREATE TABLE IF NOT EXISTS gap_search_log (series_name TEXT NOT NULL, season INTEGER NOT NULL, episode INTEGER NOT NULL, last_searched_at TEXT NOT NULL, PRIMARY KEY(series_name,season,episode)); CREATE TABLE IF NOT EXISTS series_metadata (series_name TEXT NOT NULL, season INTEGER NOT NULL, episode_count INTEGER NOT NULL, updated_at TEXT NOT NULL, PRIMARY KEY(series_name,season)); CREATE TABLE IF NOT EXISTS episode_metadata (series_name TEXT NOT NULL, season INTEGER NOT NULL, episode INTEGER NOT NULL, air_date TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL, PRIMARY KEY(series_name,season,episode)); CREATE TABLE IF NOT EXISTS ignored_episodes (series_name TEXT NOT NULL, season INTEGER NOT NULL, episode INTEGER NOT NULL, reason TEXT DEFAULT '', created_at TEXT NOT NULL DEFAULT (datetime('now')), PRIMARY KEY(series_name,season,episode)); CREATE TABLE IF NOT EXISTS upgrade_backup (new_hash TEXT PRIMARY KEY, payload_json TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT (datetime('now'))); CREATE TABLE IF NOT EXISTS series_status (series_name TEXT PRIMARY KEY, status TEXT NOT NULL DEFAULT '', last_air_date TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL);`

// databasePendingMoviesSchema mirrors the `pending_movies` table.
const databasePendingMoviesSchema = `CREATE TABLE IF NOT EXISTS pending_movies (
                id INTEGER PRIMARY KEY,
                name TEXT NOT NULL,
                year INTEGER,
                best_magnet TEXT,
                best_title TEXT,
                best_quality_score INTEGER DEFAULT 0,
                first_seen_at TEXT,
                delay_hours INTEGER DEFAULT 0,
                due_at TEXT,
                status TEXT DEFAULT 'pending',
                downloaded_at TEXT,
                UNIQUE(name, year)
            );`

// databaseProviderSchema mirrors the `provider_status` table.
const databaseProviderSchema = `CREATE TABLE IF NOT EXISTS provider_status (
                provider TEXT NOT NULL,
                kind TEXT NOT NULL,
                level INTEGER NOT NULL DEFAULT 0,
                initial_failure TEXT,
                most_recent_failure TEXT,
                disabled_till TEXT,
                last_error TEXT DEFAULT '',
                PRIMARY KEY(provider, kind)
            );`

// databaseStallWatchSchema persists the retry and notification schedule for a
// stalled download. Keeping it outside torrent_meta preserves a user's normal
// paused state: only torrents explicitly parked by the stall monitor appear
// here and are resumed/reannounced after a daemon restart.
const databaseStallWatchSchema = `CREATE TABLE IF NOT EXISTS stalled_torrents (
                hash TEXT PRIMARY KEY,
                last_progress_at TEXT NOT NULL,
                last_done INTEGER NOT NULL DEFAULT 0,
                stalled_since TEXT NOT NULL,
                next_retry_at TEXT NOT NULL,
                next_notice_at TEXT,
                notice_step INTEGER NOT NULL DEFAULT 0,
                updated_at TEXT NOT NULL
            );`

// databaseTorrentMoveSchema persists the storage moves the torrent event worker
// is still responsible for: the retry schedule (destination, post-seed flag,
// attempts) and the post-seed protection that keeps the seed cleanup from
// removing a source while it is being archived. Without it a restart lost
// both, and the worker had to guess the state again from the save path.
const databaseTorrentMoveSchema = `CREATE TABLE IF NOT EXISTS torrent_moves (
                hash TEXT PRIMARY KEY,
                has_retry INTEGER NOT NULL DEFAULT 0,
                destination TEXT NOT NULL DEFAULT '',
                retry_post_seed INTEGER NOT NULL DEFAULT 0,
                attempts INTEGER NOT NULL DEFAULT 0,
                post_seed_protected INTEGER NOT NULL DEFAULT 0,
                updated_at TEXT NOT NULL
            );`

// databaseFeedSeenSchema mirrors the "seen in feed" tables and indexes.
const databaseFeedSeenSchema = `CREATE TABLE IF NOT EXISTS movie_feed_seen (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                title TEXT NOT NULL UNIQUE,
                name TEXT,
                year INTEGER DEFAULT 0,
                resolution TEXT DEFAULT 'unknown',
                codec TEXT DEFAULT 'unknown',
                audio TEXT DEFAULT 'unknown',
                quality_score INTEGER DEFAULT 0,
                magnet TEXT,
                source TEXT,
                found_at TEXT NOT NULL,
                first_seen_at TEXT,
                group_key TEXT
            );
            CREATE INDEX IF NOT EXISTS idx_mfs_found ON movie_feed_seen(found_at DESC);
            CREATE INDEX IF NOT EXISTS idx_mfs_group ON movie_feed_seen(group_key);
            CREATE TABLE IF NOT EXISTS series_feed_seen (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                title TEXT NOT NULL UNIQUE,
                name TEXT,
                season INTEGER DEFAULT 0,
                episode INTEGER DEFAULT 0,
                resolution TEXT DEFAULT 'unknown',
                codec TEXT DEFAULT 'unknown',
                audio TEXT DEFAULT 'unknown',
                quality_score INTEGER DEFAULT 0,
                magnet TEXT,
                source TEXT,
                found_at TEXT NOT NULL,
                first_seen_at TEXT,
                group_key TEXT
            );
            CREATE INDEX IF NOT EXISTS idx_sfs_found ON series_feed_seen(found_at DESC);
            CREATE INDEX IF NOT EXISTS idx_sfs_group ON series_feed_seen(group_key);`

// Database implements the `Database`: a wrapper around the single
// SQLite connection of one Gextto database.
type Database struct {
	db   *sql.DB
	path string
}

// Close closes the underlying SQLite connection. The OS reclaims the handle at
// process exit, but an explicit close keeps shutdown deterministic and lets the
// daemon be embedded without leaking connections.
func (d *Database) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	return d.db.Close()
}

// RowCount returns the number of rows stored in the database's user tables.
func (d *Database) RowCount() int64 {
	if d == nil {
		return 0
	}
	return ConnectionRowCount(d.db)
}

// OpenDatabase opens the database at path, applies the shared pragmas and runs
// the schema migrations.
func OpenDatabase(path string) (*Database, error) {
	db, err := OpenSQLite(path)
	if err != nil {
		return nil, err
	}
	if err := HardenConnection(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("harden %s: %w", path, err)
	}
	database := &Database{db: db, path: path}
	if err := database.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return database, nil
}

// Checkpoint truncates the WAL (see `CheckpointConnection`).
func (d *Database) Checkpoint() error {
	return CheckpointConnection(d.db)
}

// QuickCheck runs `PRAGMA quick_check` (see `QuickCheck`).
func (d *Database) QuickCheck() ([]string, error) {
	return QuickCheck(d.db)
}

func (d *Database) migrate() error {
	// Older imports created torrent_meta before lifecycle state existed. Add it
	// before the schema batch creates its index, otherwise startup fails.
	var hasTorrentMeta bool
	if err := d.db.QueryRow("SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='torrent_meta')").Scan(&hasTorrentMeta); err != nil {
		return err
	}
	if hasTorrentMeta {
		var hasStatus bool
		if err := d.db.QueryRow("SELECT EXISTS(SELECT 1 FROM pragma_table_info('torrent_meta') WHERE name='status')").Scan(&hasStatus); err != nil {
			return err
		}
		if !hasStatus {
			if _, err := d.db.Exec("ALTER TABLE torrent_meta ADD COLUMN status TEXT NOT NULL DEFAULT 'queued'"); err != nil {
				return err
			}
		}
	}
	if _, err := d.db.Exec(databaseSchemaBase); err != nil {
		return err
	}
	if _, err := d.db.Exec(databaseSchemaExtra); err != nil {
		return err
	}
	if _, err := d.db.Exec("CREATE TABLE IF NOT EXISTS blocklist (magnet_hash TEXT PRIMARY KEY, title TEXT DEFAULT '', reason TEXT DEFAULT '', created_at TEXT NOT NULL);"); err != nil {
		return err
	}
	// Movies held back by a delay profile (the series equivalent lives in
	// `pending_downloads`, extended below with a precise `due_at`).
	if _, err := d.db.Exec(databasePendingMoviesSchema); err != nil {
		return err
	}
	// Escalating provider backoff (feeds, indexers).
	if _, err := d.db.Exec(databaseProviderSchema); err != nil {
		return err
	}
	if _, err := d.db.Exec(databaseStallWatchSchema); err != nil {
		return err
	}
	if _, err := d.db.Exec(databaseTorrentMoveSchema); err != nil {
		return err
	}
	// Identità della release bloccata (come il legacy `download_blocklist`):
	// hash + serie/stagione/episodio o film/anno, per audit e UI.
	for _, column := range []struct{ table, name, definition string }{
		{"blocklist", "kind", "TEXT DEFAULT ''"},
		{"blocklist", "series_name", "TEXT DEFAULT ''"},
		{"blocklist", "season", "INTEGER"},
		{"blocklist", "episode", "INTEGER"},
		{"blocklist", "movie_name", "TEXT DEFAULT ''"},
		{"blocklist", "movie_year", "INTEGER"},
	} {
		if err := ensureColumn(d.db, column.table, column.name, column.definition); err != nil {
			logging.Warn("schema migration: cannot ensure column", "table", column.table, "column", column.name, "error", err)
		}
	}
	// "Visti nei feed": tutte le release che passano dalle sorgenti.
	if _, err := d.db.Exec(databaseFeedSeenSchema); err != nil {
		return err
	}
	for _, column := range []struct{ table, name, definition string }{
		{"pending_downloads", "best_title", "TEXT DEFAULT ''"},
		{"pending_downloads", "first_seen_at", "TEXT"},
		{"pending_downloads", "timeframe_hours", "INTEGER DEFAULT 0"},
		{"pending_downloads", "status", "TEXT DEFAULT 'pending'"},
		{"pending_downloads", "downloaded_at", "TEXT"},
		{"pending_downloads", "due_at", "TEXT"},
		{"torrent_meta", "metadata_json", "TEXT DEFAULT ''"},
		{"torrent_meta", "ui_state", "TEXT DEFAULT ''"},
		{"torrent_meta", "progress", "REAL DEFAULT 0"},
		{"torrent_meta", "paused", "INTEGER DEFAULT 0"},
		{"torrent_meta", "total_size", "INTEGER DEFAULT 0"},
		{"torrent_meta", "downloaded", "INTEGER DEFAULT 0"},
		{"torrent_meta", "name", "TEXT DEFAULT ''"},
		{"torrent_meta", "completed_at", "TEXT"},
		{"torrent_meta", "processed_path", "TEXT"},
		{"torrent_meta", "kind", "TEXT DEFAULT ''"},
		{"torrent_meta", "title", "TEXT DEFAULT ''"},
		{"torrent_meta", "series_name", "TEXT DEFAULT ''"},
		{"torrent_meta", "season", "INTEGER"},
		{"torrent_meta", "episode", "INTEGER"},
		{"torrent_meta", "year", "INTEGER"},
		{"torrent_meta", "quality_score", "INTEGER DEFAULT 0"},
		{"torrent_meta", "status", "TEXT NOT NULL DEFAULT 'queued'"},
		{"torrent_meta", "error", "TEXT DEFAULT ''"},
		{"torrent_meta", "removed_at", "TEXT"},
		{"torrent_meta", "reason", "TEXT DEFAULT ''"},
		{"torrent_meta", "no_rename", "INTEGER DEFAULT 0"},
		{"torrent_meta", "created_at", "TEXT"},
		{"series", "timeframe", "INTEGER DEFAULT 0"},
		{"series", "ignored_seasons", "TEXT DEFAULT '[]'"},
		{"series", "subtitle", "TEXT DEFAULT ''"},
		{"series", "season_subfolders", "INTEGER DEFAULT 0"},
		{"series", "exclude", "TEXT DEFAULT ''"},
		{"episodes", "media_info_json", "TEXT DEFAULT ''"},
		{"movies", "media_info_json", "TEXT DEFAULT ''"},
	} {
		if err := ensureColumn(d.db, column.table, column.name, column.definition); err != nil {
			logging.Warn("schema migration: cannot ensure column", "table", column.table, "column", column.name, "error", err)
		}
	}
	// I torrent già rimossi prima dell'introduzione di `removed_at` devono
	// comunque comparire nello "Storico download".
	if _, err := d.db.Exec("UPDATE torrent_meta SET removed_at=COALESCE(NULLIF(completed_at,''), updated_at) WHERE status='removed' AND removed_at IS NULL"); err != nil {
		logging.Warn("schema migration: torrent removed_at backfill failed", "error", err)
	}
	// Le righe registrate prima di salvare la sorgente hanno il dato dentro
	// `metadata_json`: lo si riporta nella colonna dedicata.
	if _, err := d.db.Exec("UPDATE torrent_meta SET source=COALESCE(json_extract(metadata_json,'$.release.source'),'') WHERE COALESCE(source,'')='' AND COALESCE(metadata_json,'')<>''"); err != nil {
		logging.Warn("schema migration: torrent source backfill failed", "error", err)
	}
	// Indici di espressione per le ricerche case-insensitive sugli hash.
	if _, err := d.db.Exec("CREATE INDEX IF NOT EXISTS idx_torrent_meta_hash_lower ON torrent_meta(lower(hash)); CREATE INDEX IF NOT EXISTS idx_episodes_magnet_lower ON episodes(lower(magnet_hash)); CREATE INDEX IF NOT EXISTS idx_movies_magnet_lower ON movies(lower(magnet_hash));"); err != nil {
		logging.Warn("schema migration: lower(hash) indexes failed", "error", err)
	}
	// Indici per i percorsi caldi dell'acquisizione: lookup dei film per
	// (name, year), dei torrent attivi per (series_name, season, episode) e
	// della coda pending per (series_id, season, episode). Senza questi indici
	// ogni candidato esegue una scansione completa della tabella.
	if _, err := d.db.Exec("CREATE INDEX IF NOT EXISTS idx_movies_name_year ON movies(name, year); CREATE INDEX IF NOT EXISTS idx_torrent_meta_series_lower ON torrent_meta(lower(series_name), season, episode); CREATE INDEX IF NOT EXISTS idx_pending_downloads_lookup ON pending_downloads(series_id, season, episode);"); err != nil {
		logging.Warn("schema migration: hot-path indexes failed", "error", err)
	}
	return nil
}

// checkSeries implements `Database::check_series`.
func (d *Database) CheckSeries(release *models.Release) (bool, string, error) {
	return d.checkSeriesScoredInner(release, release.Quality.Score(), DefaultUpgradeMinScoreDiff, &models.ApprovalContext{}, false)
}

// CheckSeriesScored implements `Database::check_series_scored`.
func (d *Database) CheckSeriesScored(release *models.Release, score, minScoreDiff int64, context *models.ApprovalContext) (bool, string, error) {
	return d.checkSeriesScoredInner(release, score, minScoreDiff, context, false)
}

// CheckSeriesManualScored approves the explicit "Accoda" action. A download
// incomplete (even present in the client) does not equal a copy: only an
// already archived release, the same active hash or a blocklisted release are
// refused.
func (d *Database) CheckSeriesManualScored(release *models.Release, score, minScoreDiff int64, context *models.ApprovalContext) (bool, string, error) {
	return d.checkSeriesScoredInner(release, score, minScoreDiff, context, true)
}

func (d *Database) checkSeriesScoredInner(release *models.Release, score, minScoreDiff int64, context *models.ApprovalContext, manual bool) (bool, string, error) {
	hash, err := magnetHashOrError(release.Magnet)
	if err != nil {
		return false, "", err
	}
	blocklisted, err := d.IsBlocklisted(hash)
	if err != nil {
		return false, "", err
	}
	if blocklisted {
		return false, "blocklisted", nil
	}
	if context == nil {
		context = &models.ApprovalContext{}
	}
	dryRun := context.DryRun
	live := context.Live
	archive := context.Archive
	// Protezione download attivi (parità col client live del legacy): mai
	// riproporre un hash già nella sessione.
	if live != nil {
		if _, ok := live.Hashes[hash]; ok {
			return false, "active_episode", nil
		}
	}
	if release.IsPack {
		return d.checkSeriesPack(release, hash, score, minScoreDiff, context, manual)
	}
	if release.Season == nil {
		return false, "", fmt.Errorf("series release has no season")
	}
	if release.Episode == nil {
		return false, "", fmt.Errorf("series release has no episode")
	}
	season := *release.Season
	episode := *release.Episode
	seriesName := release.Title
	if release.Series != nil {
		seriesName = *release.Series
	}
	if !dryRun {
		if _, err := d.db.Exec("INSERT OR IGNORE INTO series(name) VALUES (?1)", seriesName); err != nil {
			return false, "", err
		}
	}
	var sid int64
	if err := d.db.QueryRow("SELECT id FROM series WHERE name = ?1", seriesName).Scan(&sid); err != nil {
		if !dryRun {
			return false, "", err
		}
	}
	// A library file can be removed or moved outside Gextto. Do not let its old
	// database row make the episode look permanently downloaded: reset only on
	// a definite ENOENT (permissions and temporary mount errors are preserved).
	missingArchivedFile := false
	var persistedArchivePath string
	err = d.db.QueryRow("SELECT COALESCE(archive_path,'') FROM episodes WHERE series_id=?1 AND season=?2 AND episode=?3", sid, season, episode).Scan(&persistedArchivePath)
	if err == nil && strings.TrimSpace(persistedArchivePath) != "" {
		if _, statErr := os.Stat(persistedArchivePath); errors.Is(statErr, os.ErrNotExist) && archiveFileConfirmedMissing(persistedArchivePath) {
			if !dryRun {
				if _, updateErr := d.db.Exec("UPDATE episodes SET downloaded_at=NULL,archive_path=NULL,size_bytes=0,media_info_json='' WHERE series_id=?1 AND season=?2 AND episode=?3", sid, season, episode); updateErr != nil {
					return false, "", updateErr
				}
			}
			missingArchivedFile = true
			logging.Warn("archive file missing; episode made eligible for recovery", "series", seriesName, "season", season, "episode", episode, "path", persistedArchivePath)
		}
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, "", err
	}
	if !manual && live != nil {
		key := models.LiveEpisodeKey{Series: NormalizeSeriesName(seriesName), Season: season, Episode: episode}
		if _, ok := live.Episodes[key]; ok {
			return false, "active_episode", nil
		}
	}
	if !manual {
		var exists bool
		if err := d.db.QueryRow("SELECT EXISTS(SELECT 1 FROM torrent_meta WHERE lower(series_name)=lower(?1) AND season=?2 AND (episode=?3 OR episode IS NULL) AND status NOT IN ('completed','error','removed'))", seriesName, season, episode).Scan(&exists); err != nil {
			return false, "", err
		}
		if exists {
			return false, "active_episode", nil
		}
	}
	// Core best practice (always on, no user option): never fetch an older
	// episode that is not a recognised gap when a later episode or season is
	// already archived.
	if !manual && !context.GapEpisode && !missingArchivedFile {
		var archivedHere bool
		if err := d.db.QueryRow("SELECT EXISTS(SELECT 1 FROM episodes WHERE series_id=?1 AND season=?2 AND episode=?3 AND (downloaded_at IS NOT NULL OR COALESCE(archive_path,'')<>''))", sid, season, episode).Scan(&archivedHere); err != nil {
			return false, "", err
		}
		if !archivedHere {
			var laterExists bool
			if err := d.db.QueryRow("SELECT EXISTS(SELECT 1 FROM episodes e JOIN series s ON s.id=e.series_id WHERE s.name=?1 AND (e.season > ?2 OR (e.season = ?2 AND e.episode > ?3)) AND (e.downloaded_at IS NOT NULL OR COALESCE(e.archive_path,'')<>''))", seriesName, season, episode).Scan(&laterExists); err != nil {
				return false, "", err
			}
			if laterExists {
				candidateRank := release.Quality.ResolutionRank()
				laterRank, err := d.LaterArchivedMaxResolutionRank(seriesName, season, episode)
				if err != nil {
					return false, "", err
				}
				isUpgrade := laterRank != nil && candidateRank > *laterRank
				if !isUpgrade {
					return false, "smart_episode", nil
				}
			}
		}
	}
	// Considera duplicato solo un episodio già scaricato/archiviato.
	sameHashQuery := "SELECT EXISTS(SELECT 1 FROM episodes WHERE magnet_hash=?1 AND (downloaded_at IS NOT NULL OR COALESCE(archive_path,'') <> ''))"
	if manual {
		sameHashQuery = "SELECT EXISTS(SELECT 1 FROM episodes WHERE magnet_hash=?1 AND COALESCE(archive_path,'') <> '')"
	}
	var sameHashIsArchived bool
	if err := d.db.QueryRow(sameHashQuery, hash).Scan(&sameHashIsArchived); err != nil {
		return false, "", err
	}
	if sameHashIsArchived {
		return false, "duplicate", nil
	}
	var dbID, dbScore int64
	var dbTitle, dbArchivePath, dbMediaInfo string
	dbRowErr := d.db.QueryRow("SELECT id,quality_score,COALESCE(title,''),COALESCE(archive_path,''),COALESCE(media_info_json,'') FROM episodes WHERE series_id=?1 AND season=?2 AND episode=?3", sid, season, episode).Scan(&dbID, &dbScore, &dbTitle, &dbArchivePath, &dbMediaInfo)
	dbRow := dbRowErr == nil
	if dbRowErr != nil && !errors.Is(dbRowErr, sql.ErrNoRows) {
		return false, "", dbRowErr
	}
	// Intelligenza archivio (parità col legacy `_best_quality_in_path`).
	if archive != nil {
		if disk, ok := archive.BestFor(season, episode); ok {
			if manual {
				return false, "duplicate", nil
			}
			comparisonQuality := disk.Quality
			comparisonScore := disk.Score
			if dbRow {
				comparisonQuality = MergeQuality(disk.Quality, ParseQuality(dbTitle))
				enrichQualityWithMediaInfo(dbMediaInfo, &comparisonQuality)
				if dbScore > comparisonScore {
					comparisonScore = dbScore
				}
			}
			if upgradeReasonUntil(&release.Quality, &comparisonQuality, score, comparisonScore, minScoreDiff, context.UpgradeUntilScore) == "" {
				return false, "duplicate", nil
			}
		}
	}
	if dbRow {
		if manual && dbArchivePath != "" {
			return false, "duplicate", nil
		}
		// Quality profile cutoff reached: a real archived file is never
		// replaced.
		if context.ForbidUpgrade {
			if archive != nil {
				if _, ok := archive.BestFor(season, episode); ok {
					return false, "upgrades_disabled", nil
				}
			}
		}
		oldQuality := ParseQuality(dbTitle)
		enrichQualityWithMediaInfo(dbMediaInfo, &oldQuality)
		if !manual && !missingArchivedFile && upgradeReasonUntil(&release.Quality, &oldQuality, score, dbScore, minScoreDiff, context.UpgradeUntilScore) == "" {
			return false, "duplicate", nil
		}
		if dryRun {
			// Upgrade recognised, but do not write the backup/update rows.
			return true, "upgrade", nil
		}
		previous, err := d.loadSeriesUpgradeBackup(d.db, dbID, seriesName)
		if err != nil {
			return false, "", err
		}
		// The backup and the update must be atomic: a failure after the backup
		// write would otherwise leave an orphan upgrade_backup row pointing at
		// an upgrade that was never applied.
		tx, err := d.db.Begin()
		if err != nil {
			return false, "", err
		}
		upgradeCommitted := false
		defer func() {
			if !upgradeCommitted {
				_ = tx.Rollback()
			}
		}()
		if err := d.saveUpgradeBackup(tx, hash, previous); err != nil {
			return false, "", err
		}
		var hashTakenElsewhere bool
		if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM episodes WHERE magnet_hash=?1 AND NOT (series_id=?2 AND season=?3 AND episode=?4))", hash, sid, season, episode).Scan(&hashTakenElsewhere); err != nil {
			return false, "", err
		}
		var episodeHash any
		if !hashTakenElsewhere {
			episodeHash = hash
		}
		if _, err := tx.Exec("UPDATE episodes SET title=?1,quality_score=?2,magnet_hash=COALESCE(?3,magnet_hash),magnet_link=?4,downloaded_at=NULL,archive_path=NULL,media_info_json='' WHERE id=?5", release.Title, score, episodeHash, release.Magnet, dbID); err != nil {
			return false, "", err
		}
		if err := tx.Commit(); err != nil {
			return false, "", err
		}
		upgradeCommitted = true
		return true, "upgrade", nil
	}
	var hashTakenElsewhere bool
	if err := d.db.QueryRow("SELECT EXISTS(SELECT 1 FROM episodes WHERE magnet_hash=?1)", hash).Scan(&hashTakenElsewhere); err != nil {
		return false, "", err
	}
	var episodeHash any
	if !hashTakenElsewhere {
		episodeHash = hash
	}
	if dryRun {
		// First download recognised, but do not create the placeholder row.
		return true, "approved", nil
	}
	if _, err := d.db.Exec("INSERT INTO episodes(series_id,season,episode,title,quality_score,magnet_hash,magnet_link) VALUES (?1,?2,?3,?4,?5,?6,?7)", sid, season, episode, release.Title, score, episodeHash, release.Magnet); err != nil {
		return false, "", err
	}
	return true, "approved", nil
}

// loadSeriesUpgradeBackup reads the previous episode row used to build an
// `UpgradeBackup`.
func (d *Database) loadSeriesUpgradeBackup(reader sqlQueryRower, id int64, seriesName string) (upgradeBackup, error) {
	var backup upgradeBackup
	var title sql.NullString
	var seriesID, season, episode sql.NullInt64
	var magnetHash, magnetLink, downloadedAt, archivePath sql.NullString
	err := reader.QueryRow("SELECT id,series_id,season,episode,title,quality_score,magnet_hash,magnet_link,downloaded_at,archive_path,size_bytes FROM episodes WHERE id=?1", id).Scan(
		&backup.RowID, &seriesID, &season, &episode, &title, &backup.QualityScore, &magnetHash, &magnetLink, &downloadedAt, &archivePath, &backup.SizeBytes,
	)
	if err != nil {
		return backup, err
	}
	backup.Kind = "series"
	backup.SeriesID = nullInt64Ptr(seriesID)
	backup.Season = nullInt64Ptr(season)
	backup.Episode = nullInt64Ptr(episode)
	backup.SeriesName = &seriesName
	if title.Valid {
		backup.Title = title.String
	}
	backup.MagnetHash = nullStringPtr(magnetHash)
	backup.MagnetLink = nullStringPtr(magnetLink)
	backup.DownloadedAt = nullStringPtr(downloadedAt)
	backup.ArchivePath = nullStringPtr(archivePath)
	return backup, nil
}

// loadMovieUpgradeBackup reads the previous movie row used to build an
// `UpgradeBackup`.
func (d *Database) loadMovieUpgradeBackup(id int64) (upgradeBackup, error) {
	var backup upgradeBackup
	var name, title, magnetHash, magnetLink, downloadedAt sql.NullString
	var year sql.NullInt64
	err := d.db.QueryRow("SELECT id,name,year,title,quality_score,magnet_hash,magnet_link,downloaded_at,size_bytes FROM movies WHERE id=?1", id).Scan(
		&backup.RowID, &name, &year, &title, &backup.QualityScore, &magnetHash, &magnetLink, &downloadedAt, &backup.SizeBytes,
	)
	if err != nil {
		return backup, err
	}
	backup.Kind = "movie"
	backup.Year = nullInt64Ptr(year)
	backup.Name = nullStringPtr(name)
	if title.Valid {
		backup.Title = title.String
	}
	backup.MagnetHash = nullStringPtr(magnetHash)
	backup.MagnetLink = nullStringPtr(magnetLink)
	backup.DownloadedAt = nullStringPtr(downloadedAt)
	return backup, nil
}

// checkSeriesPack implements `Database::check_series_pack`.
func (d *Database) checkSeriesPack(release *models.Release, hash string, score, minScoreDiff int64, context *models.ApprovalContext, manual bool) (bool, string, error) {
	if release.Season == nil {
		return false, "", fmt.Errorf("season pack has no season")
	}
	season := *release.Season
	seriesName := release.Title
	if release.Series != nil {
		seriesName = *release.Series
	}
	explicit := make([]int64, 0, len(release.EpisodeRange))
	for _, episode := range release.EpisodeRange {
		if episode > 0 {
			explicit = append(explicit, episode)
		}
	}
	complete := containsInt64Value(release.EpisodeRange, 0) || len(explicit) == 0
	if context == nil {
		context = &models.ApprovalContext{}
	}
	tx, err := d.db.Begin()
	if err != nil {
		return false, "", err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.Exec("INSERT OR IGNORE INTO series(name) VALUES (?1)", seriesName); err != nil {
		return false, "", err
	}
	var seriesID int64
	if err := tx.QueryRow("SELECT id FROM series WHERE name=?1", seriesName).Scan(&seriesID); err != nil {
		return false, "", err
	}
	samePackQuery := "SELECT EXISTS(SELECT 1 FROM episodes WHERE (magnet_hash=?1 OR magnet_link=?2) AND (downloaded_at IS NOT NULL OR COALESCE(archive_path,'') <> ''))"
	if manual {
		samePackQuery = "SELECT EXISTS(SELECT 1 FROM episodes WHERE (magnet_hash=?1 OR magnet_link=?2) AND COALESCE(archive_path,'') <> '')"
	}
	var samePackIsArchived bool
	if err := tx.QueryRow(samePackQuery, hash, release.Magnet).Scan(&samePackIsArchived); err != nil {
		return false, "", err
	}
	if samePackIsArchived {
		return false, "duplicate", nil
	}
	targets := append([]int64{}, explicit...)
	if complete {
		rows, err := tx.Query("SELECT DISTINCT episode FROM episodes WHERE series_id=?1 AND season=?2 AND episode>0", seriesID, season)
		if err != nil {
			return false, "", err
		}
		for rows.Next() {
			var episode int64
			if err := rows.Scan(&episode); err != nil {
				rows.Close()
				return false, "", err
			}
			targets = append(targets, episode)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return false, "", err
		}
		rows.Close()
		var count int64
		countErr := tx.QueryRow("SELECT episode_count FROM series_metadata WHERE series_name=?1 AND season=?2", seriesName, season).Scan(&count)
		if countErr == nil {
			if count > 0 && count <= 500 {
				for episode := int64(1); episode <= count; episode++ {
					targets = append(targets, episode)
				}
			}
		} else if !errors.Is(countErr, sql.ErrNoRows) {
			return false, "", countErr
		}
	}
	filtered := targets[:0]
	for _, episode := range targets {
		if episode > 0 {
			filtered = append(filtered, episode)
		}
	}
	targets = filtered
	sort.Slice(targets, func(i, j int) bool { return targets[i] < targets[j] })
	deduped := targets[:0]
	for index, episode := range targets {
		if index == 0 || episode != targets[index-1] {
			deduped = append(deduped, episode)
		}
	}
	targets = deduped

	inserted := 0
	upgraded := 0
	// L'hash del pack va assegnato a un solo episodio.
	var hashTaken bool
	if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM episodes WHERE magnet_hash=?1)", hash).Scan(&hashTaken); err != nil {
		return false, "", err
	}
	hashAvailable := !hashTaken
	live := context.Live
	archive := context.Archive
	// Pre-load the active torrents for the whole season in one query instead of
	// one EXISTS per episode (a full-season pack would otherwise run N queries
	// inside the write transaction).
	activeEpisodes := map[int64]bool{}
	if !manual {
		activeRows, err := tx.Query("SELECT DISTINCT episode FROM torrent_meta WHERE lower(series_name)=lower(?1) AND season=?2 AND status NOT IN ('completed','error','removed')", seriesName, season)
		if err != nil {
			return false, "", err
		}
		packAlreadyActive := false
		for activeRows.Next() {
			var activeEpisode sql.NullInt64
			if err := activeRows.Scan(&activeEpisode); err != nil {
				activeRows.Close()
				return false, "", err
			}
			if activeEpisode.Valid {
				activeEpisodes[activeEpisode.Int64] = true
			} else {
				packAlreadyActive = true
			}
		}
		if err := activeRows.Err(); err != nil {
			activeRows.Close()
			return false, "", err
		}
		activeRows.Close()
		if packAlreadyActive {
			return false, "active_pack", nil
		}
	}
	// Pre-load the existing episodes for the whole season in one query instead
	// of one SELECT per episode (the same N+1 as the active check above).
	type packEpisodeState struct {
		ID          int64
		Score       int64
		Title       string
		ArchivePath string
		MediaInfo   string
	}
	existingEpisodes := map[int64]packEpisodeState{}
	existingRows, err := tx.Query("SELECT episode, id, quality_score, COALESCE(title,''), COALESCE(archive_path,''), COALESCE(media_info_json,'') FROM episodes WHERE series_id=?1 AND season=?2", seriesID, season)
	if err != nil {
		return false, "", err
	}
	for existingRows.Next() {
		var episode int64
		var state packEpisodeState
		if err := existingRows.Scan(&episode, &state.ID, &state.Score, &state.Title, &state.ArchivePath, &state.MediaInfo); err != nil {
			existingRows.Close()
			return false, "", err
		}
		existingEpisodes[episode] = state
	}
	if err := existingRows.Err(); err != nil {
		existingRows.Close()
		return false, "", err
	}
	existingRows.Close()
	for _, episode := range targets {
		active := activeEpisodes[episode]
		if !manual && !active && live != nil {
			key := models.LiveEpisodeKey{Series: NormalizeSeriesName(seriesName), Season: season, Episode: episode}
			if _, ok := live.Episodes[key]; ok {
				active = true
			}
		}
		if active {
			continue
		}
		state, existing := existingEpisodes[episode]
		if !existing {
			if archive != nil {
				if disk, ok := archive.BestFor(season, episode); ok {
					if upgradeReasonUntil(&release.Quality, &disk.Quality, score, disk.Score, minScoreDiff, context.UpgradeUntilScore) == "" {
						continue
					}
				}
			}
			var episodeHash any
			if hashAvailable {
				hashAvailable = false
				episodeHash = hash
			}
			if _, err := tx.Exec("INSERT INTO episodes(series_id,season,episode,title,quality_score,magnet_hash,magnet_link) VALUES (?1,?2,?3,?4,?5,?6,?7)", seriesID, season, episode, release.Title, score, episodeHash, release.Magnet); err != nil {
				return false, "", err
			}
			inserted++
			continue
		}
		if manual {
			archivedOnDisk := false
			if archive != nil {
				if _, ok := archive.BestFor(season, episode); ok {
					archivedOnDisk = true
				}
			}
			if state.ArchivePath != "" || archivedOnDisk {
				continue
			}
		}
		oldQuality := ParseQuality(state.Title)
		oldScore := state.Score
		if archive != nil {
			if disk, ok := archive.BestFor(season, episode); ok {
				oldQuality = MergeQuality(disk.Quality, ParseQuality(state.Title))
				if disk.Score > oldScore {
					oldScore = disk.Score
				}
			}
		}
		enrichQualityWithMediaInfo(state.MediaInfo, &oldQuality)
		if manual || (upgradeReasonUntil(&release.Quality, &oldQuality, score, oldScore, minScoreDiff, context.UpgradeUntilScore) != "" && !context.ForbidUpgrade) {
			previous, err := d.loadSeriesUpgradeBackup(tx, state.ID, seriesName)
			if err != nil {
				return false, "", err
			}
			if err := d.saveUpgradeBackup(tx, hash, previous); err != nil {
				return false, "", err
			}
			var episodeHash any
			if hashAvailable {
				hashAvailable = false
				episodeHash = hash
			}
			if _, err := tx.Exec("UPDATE episodes SET title=?1,quality_score=?2,magnet_hash=COALESCE(?3,magnet_hash),magnet_link=?4,downloaded_at=NULL,archive_path=NULL,media_info_json='' WHERE id=?5", release.Title, score, episodeHash, release.Magnet, state.ID); err != nil {
				return false, "", err
			}
			upgraded++
		}
	}
	// Stagione sconosciuta (nessun episodio noto): mantieni il placeholder
	// stagionale, come faceva la versione precedente.
	if len(targets) == 0 && complete {
		var id, existingScore int64
		var existingTitle string
		existingErr := tx.QueryRow("SELECT id,quality_score,COALESCE(title,'') FROM episodes WHERE series_id=?1 AND season=?2 AND episode=0", seriesID, season).Scan(&id, &existingScore, &existingTitle)
		existing := existingErr == nil
		if existingErr != nil && !errors.Is(existingErr, sql.ErrNoRows) {
			return false, "", existingErr
		}
		if !existing {
			var episodeHash any
			if hashAvailable {
				episodeHash = hash
			}
			if _, err := tx.Exec("INSERT INTO episodes(series_id,season,episode,title,quality_score,magnet_hash,magnet_link) VALUES (?1,?2,0,?3,?4,?5,?6)", seriesID, season, release.Title, score, episodeHash, release.Magnet); err != nil {
				return false, "", err
			}
			inserted++
		} else {
			oldQuality := ParseQuality(existingTitle)
			if upgradeReasonUntil(&release.Quality, &oldQuality, score, existingScore, minScoreDiff, context.UpgradeUntilScore) != "" {
				var episodeHash any
				if hashAvailable {
					episodeHash = hash
				}
				if _, err := tx.Exec("UPDATE episodes SET title=?1,quality_score=?2,magnet_hash=COALESCE(?3,magnet_hash),magnet_link=?4,downloaded_at=NULL,archive_path=NULL,media_info_json='' WHERE id=?5", release.Title, score, episodeHash, release.Magnet, id); err != nil {
					return false, "", err
				}
				upgraded++
			}
		}
	}
	approved := inserted+upgraded > 0
	reason := "duplicate"
	if approved {
		if upgraded > 0 {
			reason = "upgrade"
		} else {
			reason = "approved"
		}
	}
	if context.DryRun {
		// The deferred rollback discards every insert/update performed above.
		return approved, reason, nil
	}
	if err := tx.Commit(); err != nil {
		return false, "", err
	}
	committed = true
	return approved, reason, nil
}

// CheckMovie implements `Database::check_movie`.
func (d *Database) CheckMovie(release *models.Release) (bool, string, error) {
	return d.CheckMovieScored(release, release.Quality.Score(), DefaultUpgradeMinScoreDiff)
}

// CheckMovieScored implements `Database::check_movie_scored`.
func (d *Database) CheckMovieScored(release *models.Release, score, minScoreDiff int64) (bool, string, error) {
	return d.CheckMovieScoredWith(release, score, minScoreDiff, false)
}

// CheckMovieScoredWith refuses to replace a real imported movie when
// `forbidUpgrade` is set.
func (d *Database) CheckMovieScoredWith(release *models.Release, score, minScoreDiff int64, forbidUpgrade bool) (bool, string, error) {
	return d.checkMovieScoredWith(release, score, minScoreDiff, 0, forbidUpgrade, false)
}

// checkMovieScoredWith is CheckMovieScoredWith with an explicit dry-run switch:
// in dry-run the decision is computed but no movie row is created, restored or
// upgraded.
func (d *Database) checkMovieScoredWith(release *models.Release, score, minScoreDiff, upgradeUntil int64, forbidUpgrade bool, dryRun bool) (bool, string, error) {
	hash, err := magnetHashOrError(release.Magnet)
	if err != nil {
		return false, "", err
	}
	blocklisted, err := d.IsBlocklisted(hash)
	if err != nil {
		return false, "", err
	}
	if blocklisted {
		return false, "blocklisted", nil
	}
	var exists bool
	if err := d.db.QueryRow("SELECT EXISTS(SELECT 1 FROM movies WHERE magnet_hash=?1 AND removed_at IS NULL)", hash).Scan(&exists); err != nil {
		return false, "", err
	}
	if exists {
		return false, "duplicate", nil
	}
	var removedID int64
	removedErr := d.db.QueryRow("SELECT id FROM movies WHERE magnet_hash=?1 AND removed_at IS NOT NULL", hash).Scan(&removedID)
	if removedErr == nil {
		if dryRun {
			return true, "restored", nil
		}
		if _, err := d.db.Exec("UPDATE movies SET name=?1,year=?2,title=?1,quality_score=?3,magnet_link=?4,downloaded_at=NULL,removed_at=NULL WHERE id=?5", release.Title, release.Year, score, release.Magnet, removedID); err != nil {
			return false, "", err
		}
		return true, "restored", nil
	}
	if !errors.Is(removedErr, sql.ErrNoRows) {
		return false, "", removedErr
	}
	var id, existingScore int64
	var metadataJSON, existingMedia string
	var downloadedAt sql.NullString
	// Match exact year if null/same, or allow +/- 1 year tolerance to align with disk cleanup logic
	query := "SELECT m.id,m.quality_score,COALESCE(t.metadata_json,''),m.downloaded_at,COALESCE(m.media_info_json,'') FROM movies m LEFT JOIN torrent_meta t ON lower(t.hash)=lower(m.magnet_hash) WHERE m.removed_at IS NULL AND m.name=?1 AND (m.year IS ?2 OR (?2 IS NOT NULL AND m.year IS NOT NULL AND abs(m.year - ?2) <= 1)) ORDER BY (m.year IS ?2) DESC, m.id DESC LIMIT 1"
	rowErr := d.db.QueryRow(query, release.Title, release.Year).Scan(&id, &existingScore, &metadataJSON, &downloadedAt, &existingMedia)
	if rowErr != nil && !errors.Is(rowErr, sql.ErrNoRows) {
		return false, "", rowErr
	}
	if rowErr == nil {
		// Quality profile cutoff reached on a real imported file: never replace
		// it.
		if forbidUpgrade && downloadedAt.Valid {
			return false, "upgrades_disabled", nil
		}
		var hardUpgrade *bool
		if metadataJSON != "" {
			var metadata models.TorrentMeta
			if err := json.Unmarshal([]byte(metadataJSON), &metadata); err == nil {
				enrichQualityWithMediaInfo(existingMedia, &metadata.Release.Quality)
				value := upgradeReasonUntil(&release.Quality, &metadata.Release.Quality, score, existingScore, minScoreDiff, upgradeUntil) != ""
				hardUpgrade = &value
			}
		}
		upgrade := false
		if hardUpgrade != nil {
			upgrade = *hardUpgrade
		} else {
			upgrade = score > existingScore && score-existingScore >= minScoreDiff &&
				(upgradeUntil <= 0 || existingScore < upgradeUntil)
		}
		if !upgrade {
			return false, "duplicate", nil
		}
		if dryRun {
			return true, "upgrade", nil
		}
		previous, err := d.loadMovieUpgradeBackup(id)
		if err != nil {
			return false, "", err
		}
		// Atomic backup + update: see the series upgrade path for rationale.
		tx, err := d.db.Begin()
		if err != nil {
			return false, "", err
		}
		upgradeCommitted := false
		defer func() {
			if !upgradeCommitted {
				_ = tx.Rollback()
			}
		}()
		if err := d.saveUpgradeBackup(tx, hash, previous); err != nil {
			return false, "", err
		}
		if _, err := tx.Exec("UPDATE movies SET title=?1,quality_score=?2,magnet_hash=?3,magnet_link=?4,downloaded_at=NULL,media_info_json='' WHERE id=?5", release.Title, score, hash, release.Magnet, id); err != nil {
			return false, "", err
		}
		if err := tx.Commit(); err != nil {
			return false, "", err
		}
		upgradeCommitted = true
		return true, "upgrade", nil
	}
	if dryRun {
		return true, "approved", nil
	}
	if _, err := d.db.Exec("INSERT INTO movies(name,year,title,quality_score,magnet_hash,magnet_link,downloaded_at) VALUES (?1,?2,?3,?4,?5,?6,?7)", release.Title, release.Year, release.Title, score, hash, release.Magnet, nil); err != nil {
		return false, "", err
	}
	return true, "approved", nil
}

// RegisterTorrent implements `Database::register_torrent`.
func (d *Database) RegisterTorrent(release *models.Release) error {
	return d.RegisterTorrentScored(release, release.Quality.Score())
}

// RegisterTorrentScored implements `Database::register_torrent_scored`.
func (d *Database) RegisterTorrentScored(release *models.Release, qualityScore int64) error {
	hash, err := magnetHashOrError(release.Magnet)
	if err != nil {
		return err
	}
	now := nowSQLite()
	metadata, err := json.Marshal(models.TorrentMeta{Release: *release})
	if err != nil {
		return err
	}
	_, err = d.db.Exec(
		"INSERT INTO torrent_meta(hash,kind,title,series_name,season,episode,year,quality_score,source,metadata_json,status,created_at,updated_at) VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,'queued',?11,?11) ON CONFLICT(hash) DO UPDATE SET kind=excluded.kind,title=excluded.title,series_name=excluded.series_name,season=excluded.season,episode=excluded.episode,year=excluded.year,quality_score=excluded.quality_score,source=excluded.source,metadata_json=excluded.metadata_json,status=CASE WHEN torrent_meta.status='completed' THEN torrent_meta.status ELSE 'queued' END,error=CASE WHEN torrent_meta.status='completed' THEN torrent_meta.error ELSE NULL END,removed_at=CASE WHEN torrent_meta.status='completed' THEN torrent_meta.removed_at ELSE NULL END,updated_at=excluded.updated_at",
		hash, release.Kind, release.Title, release.Series, release.Season, release.Episode, release.Year, qualityScore, release.Source, string(metadata), now,
	)
	return err
}

// SetTorrentNoRename sets the "do not rename" flag for a torrent (creating the
// row when missing).
func (d *Database) SetTorrentNoRename(hash string, value bool) error {
	_, err := d.db.Exec(
		"INSERT INTO torrent_meta(hash,no_rename,status,created_at,updated_at) VALUES (?1,?2,'queued',datetime('now'),datetime('now')) ON CONFLICT(hash) DO UPDATE SET no_rename=excluded.no_rename, updated_at=excluded.updated_at",
		strings.ToLower(hash), boolToInt(value),
	)
	return err
}

// TorrentNoRename reports the stored flag for a torrent.
func (d *Database) TorrentNoRename(hash string) (bool, error) {
	var value int64
	err := d.db.QueryRow("SELECT COALESCE(no_rename,0) FROM torrent_meta WHERE lower(hash)=lower(?1)", hash).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value != 0, nil
}

// NoRenameTorrents returns torrents explicitly excluded from renaming
// (hash, display name).
func (d *Database) NoRenameTorrents() ([][2]string, error) {
	rows, err := d.db.Query("SELECT hash, COALESCE(NULLIF(name,''), NULLIF(title,''), '') FROM torrent_meta WHERE COALESCE(no_rename,0)!=0 ORDER BY updated_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([][2]string, 0)
	for rows.Next() {
		var hash, name string
		if err := rows.Scan(&hash, &name); err != nil {
			return nil, err
		}
		results = append(results, [2]string{hash, name})
	}
	return results, rows.Err()
}

// ProviderFailure records a failed source attempt and moves it down the
// escalation schedule.
func (d *Database) ProviderFailure(kind, provider, failureError string) error {
	now := time.Now().UTC()
	var current int64
	var lastFailure sql.NullString
	err := d.db.QueryRow("SELECT level, most_recent_failure FROM provider_status WHERE provider=?1 AND kind=?2", provider, kind).Scan(&current, &lastFailure)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) {
		current = 0
	}
	var elapsed *int64
	if lastFailure.Valid {
		if last, ok := utils.ParseTimestamp(lastFailure.String); ok {
			seconds := int64(now.Sub(last).Seconds())
			elapsed = &seconds
		}
	}
	level := backoff.NextLevel(current, elapsed)
	disabledTill := now.Add(time.Duration(backoff.PeriodSecs(level)) * time.Second)
	_, err = d.db.Exec(
		"INSERT INTO provider_status(provider,kind,level,initial_failure,most_recent_failure,disabled_till,last_error) VALUES (?1,?2,?3,?4,?4,?5,?6) ON CONFLICT(provider,kind) DO UPDATE SET level=excluded.level, most_recent_failure=excluded.most_recent_failure, disabled_till=excluded.disabled_till, last_error=excluded.last_error",
		provider, kind, level, sqliteTimestamp(now), sqliteTimestamp(disabledTill), failureError,
	)
	return err
}

// ProviderSuccess steps the escalation level down and lifts the disabled
// window. The row is removed entirely once fully recovered.
func (d *Database) ProviderSuccess(kind, provider string) error {
	var level int64
	err := d.db.QueryRow("SELECT level FROM provider_status WHERE provider=?1 AND kind=?2", provider, kind).Scan(&level)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	next := backoff.SuccessLevel(level)
	if next <= 0 {
		_, err = d.db.Exec("DELETE FROM provider_status WHERE provider=?1 AND kind=?2", provider, kind)
	} else {
		_, err = d.db.Exec("UPDATE provider_status SET level=?3, disabled_till=NULL WHERE provider=?1 AND kind=?2", provider, kind, next)
	}
	return err
}

// ProviderBlocked reports whether a source is inside its disabled window.
func (d *Database) ProviderBlocked(kind, provider string) (bool, error) {
	var disabled sql.NullString
	err := d.db.QueryRow("SELECT disabled_till FROM provider_status WHERE provider=?1 AND kind=?2", provider, kind).Scan(&disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !disabled.Valid {
		return false, nil
	}
	until, ok := utils.ParseTimestamp(disabled.String)
	if !ok {
		return false, nil
	}
	return until.After(time.Now().UTC()), nil
}

// BlockedProviders returns all currently disabled `(kind, provider)` pairs.
func (d *Database) BlockedProviders() (map[[2]string]struct{}, error) {
	now := nowSQLite()
	rows, err := d.db.Query("SELECT kind, provider FROM provider_status WHERE disabled_till IS NOT NULL AND disabled_till > ?1", now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[[2]string]struct{})
	for rows.Next() {
		var kind, provider string
		if err := rows.Scan(&kind, &provider); err != nil {
			return nil, err
		}
		result[[2]string{kind, provider}] = struct{}{}
	}
	return result, rows.Err()
}

// ProviderStatuses is the full provider status list for the UI/API.
func (d *Database) ProviderStatuses() ([]models.ProviderStatus, error) {
	rows, err := d.db.Query("SELECT provider, kind, level, COALESCE(disabled_till,''), COALESCE(most_recent_failure,''), COALESCE(last_error,'') FROM provider_status ORDER BY disabled_till DESC, provider ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]models.ProviderStatus, 0)
	for rows.Next() {
		var status models.ProviderStatus
		if err := rows.Scan(&status.Provider, &status.Kind, &status.Level, &status.DisabledTill, &status.MostRecentFailure, &status.LastError); err != nil {
			return nil, err
		}
		results = append(results, status)
	}
	return results, rows.Err()
}

// ClearProviderStatus clears one provider, or every provider when `provider` is
// nil.
func (d *Database) ClearProviderStatus(provider *string) error {
	if provider != nil {
		// A targeted "Azzera" is a reset, not a removal: the row stays so the
		// Health table keeps showing the provider with an updated (ok) state.
		return d.ResetProviderStatus(*provider)
	}
	_, err := d.db.Exec("DELETE FROM provider_status")
	return err
}

// ResetProviderStatus clears a provider's backoff and error but keeps its row.
func (d *Database) ResetProviderStatus(provider string) error {
	_, err := d.db.Exec(
		"UPDATE provider_status SET level=0, disabled_till=NULL, most_recent_failure=NULL, last_error='' WHERE provider=?1",
		provider)
	return err
}

// SetMediaInfo persists the `ffprobe` result for a release's archived file.
func (d *Database) SetMediaInfo(release *models.Release, info *MediaInfo) error {
	serialized, err := json.Marshal(info)
	if err != nil {
		return err
	}
	if release.Kind == "movie" {
		_, err = d.db.Exec("UPDATE movies SET media_info_json=?1 WHERE name=?2 AND year IS ?3", string(serialized), release.Title, release.Year)
		return err
	}
	if release.Series != nil && release.Season != nil && release.Episode != nil {
		_, err = d.db.Exec("UPDATE episodes SET media_info_json=?1 WHERE series_id=(SELECT id FROM series WHERE name=?2) AND season=?3 AND episode=?4", string(serialized), *release.Series, *release.Season, *release.Episode)
		return err
	}
	return nil
}

// SetEpisodeMediaInfo updates one archived episode's media info directly
// (backfill path).
func (d *Database) SetEpisodeMediaInfo(series string, season, episode int64, jsonValue string) (int, error) {
	result, err := d.db.Exec("UPDATE episodes SET media_info_json=?1 WHERE series_id=(SELECT id FROM series WHERE name=?2) AND season=?3 AND episode=?4", jsonValue, series, season, episode)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	return int(affected), err
}

// SetMovieMediaInfo updates one archived movie's media info directly (backfill
// path).
func (d *Database) SetMovieMediaInfo(name string, year *int64, jsonValue string) (int, error) {
	result, err := d.db.Exec("UPDATE movies SET media_info_json=?1 WHERE name=?2 AND year IS ?3", jsonValue, name, year)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	return int(affected), err
}

// MediaInfoBackfillTargets returns archived entries with a known path, an
// existing regular file, and no stored probe yet. Files that only exist as
// stale database references are deliberately ignored: ffprobe/MediaInfo must
// never be invoked for paths that are not on disk.
func (d *Database) MediaInfoBackfillTargets(limit int) ([]MediaInfoBackfillTarget, error) {
	limit = clampInt(limit, 1, 20_000)
	limit64 := int64(limit)
	targets := make([]MediaInfoBackfillTarget, 0)

	// Read the candidate rows and close the cursor *before* touching the
	// filesystem: with a single SQLite connection, holding *sql.Rows open
	// across os.Stat would block every other query for the whole scan.
	type seriesCandidate struct {
		series          string
		season, episode int64
		path            string
	}
	seriesCandidates := make([]seriesCandidate, 0)
	rows, err := d.db.Query("SELECT s.name, e.season, e.episode, e.archive_path FROM episodes e JOIN series s ON s.id=e.series_id WHERE COALESCE(e.media_info_json,'')='' AND COALESCE(e.archive_path,'')<>'' ORDER BY e.id DESC")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var series, path string
		var season, episode int64
		if err := rows.Scan(&series, &season, &episode, &path); err != nil {
			rows.Close()
			return nil, err
		}
		seriesCandidates = append(seriesCandidates, seriesCandidate{series: series, season: season, episode: episode, path: path})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for _, candidate := range seriesCandidates {
		if !mediaInfoBackfillPathExists(candidate.path) {
			continue
		}
		seasonValue := candidate.season
		episodeValue := candidate.episode
		targets = append(targets, MediaInfoBackfillTarget{
			Kind:    "series",
			Series:  candidate.series,
			Season:  &seasonValue,
			Episode: &episodeValue,
			Path:    candidate.path,
		})
		if int64(len(targets)) >= limit64 {
			return targets, nil
		}
	}

	if len(targets) < limit {
		type movieCandidate struct {
			name string
			year sql.NullInt64
			path string
		}
		movieCandidates := make([]movieCandidate, 0)
		movieRows, err := d.db.Query("SELECT m.name, m.year, t.processed_path FROM movies m JOIN torrent_meta t ON lower(t.hash)=lower(m.magnet_hash) WHERE COALESCE(m.media_info_json,'')='' AND COALESCE(t.processed_path,'')<>'' AND m.removed_at IS NULL ORDER BY m.id DESC")
		if err != nil {
			return nil, err
		}
		for movieRows.Next() {
			var name, path string
			var year sql.NullInt64
			if err := movieRows.Scan(&name, &year, &path); err != nil {
				movieRows.Close()
				return nil, err
			}
			movieCandidates = append(movieCandidates, movieCandidate{name: name, year: year, path: path})
		}
		if err := movieRows.Err(); err != nil {
			movieRows.Close()
			return nil, err
		}
		movieRows.Close()
		for _, candidate := range movieCandidates {
			if !mediaInfoBackfillPathExists(candidate.path) {
				continue
			}
			targets = append(targets, MediaInfoBackfillTarget{
				Kind: "movie",
				Name: candidate.name,
				Year: nullInt64Ptr(candidate.year),
				Path: candidate.path,
			})
			if int64(len(targets)) >= limit64 {
				break
			}
		}
	}
	return targets, nil
}

func mediaInfoBackfillPathExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func parseMediaInfoJSON(raw sql.NullString) map[string]any {
	if !raw.Valid {
		return nil
	}
	value := strings.TrimSpace(raw.String)
	if value == "" {
		return nil
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return nil
	}
	return parsed
}

// EpisodeMediaInfo returns the stored probe of an episode, if any.
func (d *Database) EpisodeMediaInfo(series string, season, episode int64) (map[string]any, error) {
	var raw sql.NullString
	err := d.db.QueryRow("SELECT media_info_json FROM episodes WHERE series_id=(SELECT id FROM series WHERE name=?1) AND season=?2 AND episode=?3", series, season, episode).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseMediaInfoJSON(raw), nil
}

// MovieMediaInfo returns the stored probe of a movie, if any.
func (d *Database) MovieMediaInfo(name string, year *int64) (map[string]any, error) {
	var raw sql.NullString
	err := d.db.QueryRow("SELECT media_info_json FROM movies WHERE name=?1 AND year IS ?2", name, year).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseMediaInfoJSON(raw), nil
}

// IsBlocklisted reports whether a hash is in the blocklist.
func (d *Database) IsBlocklisted(hash string) (bool, error) {
	var exists bool
	err := d.db.QueryRow("SELECT EXISTS(SELECT 1 FROM blocklist WHERE magnet_hash=?1)", strings.ToLower(hash)).Scan(&exists)
	return exists, err
}

// BlocklistedHashes loads the immutable blocklist snapshot used during one
// orchestration cycle.
func (d *Database) BlocklistedHashes() (map[string]struct{}, error) {
	rows, err := d.db.Query("SELECT lower(magnet_hash) FROM blocklist")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hashes := make(map[string]struct{})
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, err
		}
		hashes[hash] = struct{}{}
	}
	return hashes, rows.Err()
}

// Blocklist adds a release to the blocklist.
func (d *Database) Blocklist(release *models.Release, reason string) error {
	hash, err := magnetHashOrError(release.Magnet)
	if err != nil {
		return err
	}
	var movieName string
	var movieYear *int64
	if release.Kind == "movie" {
		movieName = release.Title
		movieYear = release.Year
	}
	_, err = d.db.Exec(
		"INSERT OR REPLACE INTO blocklist(magnet_hash,title,reason,created_at,kind,series_name,season,episode,movie_name,movie_year) VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10)",
		hash, release.Title, reason, nowSQLite(), release.Kind, release.Series, release.Season, release.Episode, movieName, movieYear,
	)
	return err
}

// BlocklistEntries returns the blocklist entries for the UI/API.
func (d *Database) BlocklistEntries(limit int64) ([]any, error) {
	rows, err := d.db.Query("SELECT magnet_hash,title,reason,created_at,COALESCE(kind,''),COALESCE(series_name,''),season,episode,COALESCE(movie_name,''),movie_year FROM blocklist ORDER BY created_at DESC LIMIT ?1", clampInt64(limit, 1, 1000))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]any, 0)
	for rows.Next() {
		var hash, title, reason, createdAt, kind, seriesName, movieName string
		var season, episode, movieYear sql.NullInt64
		if err := rows.Scan(&hash, &title, &reason, &createdAt, &kind, &seriesName, &season, &episode, &movieName, &movieYear); err != nil {
			return nil, err
		}
		var seasonValue, episodeValue, movieYearValue any
		if season.Valid {
			seasonValue = season.Int64
		}
		if episode.Valid {
			episodeValue = episode.Int64
		}
		if movieYear.Valid {
			movieYearValue = movieYear.Int64
		}
		entries = append(entries, map[string]any{
			"hash":        hash,
			"title":       title,
			"reason":      reason,
			"created_at":  createdAt,
			"kind":        kind,
			"series_name": seriesName,
			"season":      seasonValue,
			"episode":     episodeValue,
			"movie_name":  movieName,
			"movie_year":  movieYearValue,
		})
	}
	return entries, rows.Err()
}

// RemoveBlocklist deletes a hash from the blocklist.
func (d *Database) RemoveBlocklist(hash string) (bool, error) {
	result, err := d.db.Exec("DELETE FROM blocklist WHERE lower(magnet_hash)=lower(?1)", hash)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// RollbackRelease removes the placeholder rows of a non-downloaded release.
func (d *Database) RollbackRelease(release *models.Release) error {
	hash, err := magnetHashOrError(release.Magnet)
	if err != nil {
		return err
	}
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	// An upgrade changes the existing library row before the torrent is handed
	// to the engine. If the engine refuses the torrent, restore every previous
	// row captured for this hash instead of deleting the library entry.
	var payload sql.NullString
	backupErr := tx.QueryRow("SELECT payload_json FROM upgrade_backup WHERE new_hash=?1", hash).Scan(&payload)
	if backupErr == nil && payload.Valid {
		backups, err := decodeUpgradeBackups(payload.String)
		if err != nil {
			return err
		}
		if len(backups) > 0 {
			for _, backup := range backups {
				if err := restoreUpgradeBackupTx(tx, backup); err != nil {
					return err
				}
			}
			if _, err := tx.Exec("DELETE FROM upgrade_backup WHERE new_hash=?1", hash); err != nil {
				return err
			}
		}
	} else if backupErr != nil && !errors.Is(backupErr, sql.ErrNoRows) {
		return backupErr
	}
	if _, err := tx.Exec("DELETE FROM episodes WHERE magnet_hash=?1 OR magnet_link=?2", hash, release.Magnet); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM movies WHERE magnet_hash=?1", hash); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// ForgetRemovedTorrent cleans up the database when a torrent is removed by the
// user (or is abandoned).
func (d *Database) ForgetRemovedTorrent(hash string) error {
	normalized := strings.ToLower(hash)
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.Exec("UPDATE episodes SET magnet_hash=NULL WHERE lower(COALESCE(magnet_hash,''))=?1 AND (downloaded_at IS NOT NULL OR COALESCE(archive_path,'')<>'')", normalized); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE movies SET magnet_hash=NULL WHERE lower(COALESCE(magnet_hash,''))=?1 AND (downloaded_at IS NOT NULL OR COALESCE(archive_path,'')<>'')", normalized); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM episodes WHERE lower(COALESCE(magnet_hash,''))=?1 AND downloaded_at IS NULL AND COALESCE(archive_path,'')=''", normalized); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM movies WHERE lower(COALESCE(magnet_hash,''))=?1 AND downloaded_at IS NULL", normalized); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM upgrade_backup WHERE new_hash=?1", normalized); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// QueuePending holds a series release for `timeframe_hours` hours.
func (d *Database) QueuePending(release *models.Release, timeframeHours int64) error {
	return d.QueuePendingScored(release, maxInt64(timeframeHours, 0)*60, release.Quality.Score())
}

// QueuePendingScored holds a series release for `delay_minutes`. The best
// scoring candidate seen during the window is the one that will be grabbed.
func (d *Database) QueuePendingScored(release *models.Release, delayMinutes, qualityScore int64) error {
	seriesName := release.Title
	if release.Series != nil {
		seriesName = *release.Series
	}
	if _, err := d.db.Exec("INSERT OR IGNORE INTO series(name) VALUES (?1)", seriesName); err != nil {
		return err
	}
	var seriesID int64
	if err := d.db.QueryRow("SELECT id FROM series WHERE name=?1", seriesName).Scan(&seriesID); err != nil {
		return err
	}
	now := time.Now().UTC()
	dueAt := now.Add(time.Duration(maxInt64(delayMinutes, 0)) * time.Minute)
	var id, existingScore int64
	existingErr := d.db.QueryRow("SELECT id,best_quality_score FROM pending_downloads WHERE series_id=?1 AND season=?2 AND episode=?3 AND status='pending'", seriesID, release.Season, release.Episode).Scan(&id, &existingScore)
	if existingErr != nil && !errors.Is(existingErr, sql.ErrNoRows) {
		return existingErr
	}
	if existingErr == nil {
		if qualityScore > existingScore {
			if _, err := d.db.Exec("UPDATE pending_downloads SET best_title=?1,best_quality_score=?2,best_magnet=?3 WHERE id=?4", release.Title, qualityScore, release.Magnet, id); err != nil {
				return err
			}
		}
	} else {
		hours := (maxInt64(delayMinutes, 0) + 59) / 60
		if _, err := d.db.Exec("INSERT INTO pending_downloads(series_id,season,episode,best_magnet,best_quality_score,ready_at,best_title,first_seen_at,timeframe_hours,due_at,status) VALUES (?1,?2,?3,?4,?5,?6,?7,?6,?8,?9,'pending')", seriesID, release.Season, release.Episode, release.Magnet, qualityScore, nowSQLite(), release.Title, hours, sqliteTimestamp(dueAt)); err != nil {
			return err
		}
	}
	return nil
}

// ReadyPending returns series releases whose delay window has elapsed.
func (d *Database) ReadyPending() ([]ReadyPending, error) {
	now := time.Now().UTC()
	rows, err := d.db.Query("SELECT s.name,p.best_title,p.best_magnet,p.season,p.episode,p.due_at,p.first_seen_at,p.ready_at,p.timeframe_hours FROM pending_downloads p JOIN series s ON s.id=p.series_id WHERE p.status='pending' AND s.enabled=1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ready := make([]ReadyPending, 0)
	for rows.Next() {
		var name, title, magnet string
		var season, episode int64
		var dueAt, firstSeen, readyAt sql.NullString
		var hours sql.NullInt64
		if err := rows.Scan(&name, &title, &magnet, &season, &episode, &dueAt, &firstSeen, &readyAt, &hours); err != nil {
			return nil, err
		}
		due, ok := pendingDue(now, dueAt, firstSeen, readyAt, hours, true)
		if ok && !now.Before(due) {
			ready = append(ready, ReadyPending{Name: name, Title: title, Magnet: magnet, Season: season, Episode: episode})
		}
	}
	return ready, rows.Err()
}

// pendingDue resolves the due time from `due_at`, falling back to
// `first_seen_at`/`ready_at` plus the stored hours window.
func pendingDue(now time.Time, dueAt, firstSeen, readyAt sql.NullString, hours sql.NullInt64, withReadyAt bool) (time.Time, bool) {
	if dueAt.Valid {
		if due, ok := utils.ParseTimestamp(dueAt.String); ok {
			return due, true
		}
	}
	base := firstSeen
	if !base.Valid && withReadyAt {
		base = readyAt
	}
	if !base.Valid {
		return time.Time{}, false
	}
	parsed, ok := utils.ParseTimestamp(base.String)
	if !ok {
		return time.Time{}, false
	}
	window := int64(0)
	if hours.Valid {
		window = maxInt64(hours.Int64, 0)
	}
	return parsed.Add(time.Duration(window) * time.Hour), true
}

// RemovePending marks a pending series release as downloaded.
func (d *Database) RemovePending(seriesName string, season, episode int64) error {
	_, err := d.db.Exec("UPDATE pending_downloads SET status='downloaded',downloaded_at=?1 WHERE series_id=(SELECT id FROM series WHERE name=?2) AND season=?3 AND episode=?4 AND status='pending'", nowSQLite(), seriesName, season, episode)
	return err
}

// QueuePendingMovieScored holds a movie release for `delay_minutes`.
func (d *Database) QueuePendingMovieScored(release *models.Release, delayMinutes, qualityScore int64) error {
	name := release.Title
	now := time.Now().UTC()
	dueAt := now.Add(time.Duration(maxInt64(delayMinutes, 0)) * time.Minute)
	var id, existingScore int64
	existingErr := d.db.QueryRow("SELECT id,best_quality_score FROM pending_movies WHERE name=?1 AND year IS ?2 AND status='pending'", name, release.Year).Scan(&id, &existingScore)
	if existingErr != nil && !errors.Is(existingErr, sql.ErrNoRows) {
		return existingErr
	}
	if existingErr == nil {
		if qualityScore > existingScore {
			if _, err := d.db.Exec("UPDATE pending_movies SET best_title=?1,best_quality_score=?2,best_magnet=?3 WHERE id=?4", release.Title, qualityScore, release.Magnet, id); err != nil {
				return err
			}
		}
		return nil
	}
	hours := (maxInt64(delayMinutes, 0) + 59) / 60
	_, err := d.db.Exec("INSERT OR REPLACE INTO pending_movies(name,year,best_magnet,best_quality_score,best_title,first_seen_at,delay_hours,due_at,status) VALUES (?1,?2,?3,?4,?5,?6,?7,?8,'pending')", name, release.Year, release.Magnet, qualityScore, release.Title, nowSQLite(), hours, sqliteTimestamp(dueAt))
	return err
}

// ReadyPendingMovies returns movies whose delay window has elapsed.
func (d *Database) ReadyPendingMovies() ([]ReadyPendingMovie, error) {
	now := time.Now().UTC()
	rows, err := d.db.Query("SELECT name,best_title,best_magnet,year,due_at,first_seen_at,delay_hours FROM pending_movies WHERE status='pending'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ready := make([]ReadyPendingMovie, 0)
	for rows.Next() {
		var name, title, magnet string
		var year sql.NullInt64
		var dueAt, firstSeen sql.NullString
		var hours sql.NullInt64
		if err := rows.Scan(&name, &title, &magnet, &year, &dueAt, &firstSeen, &hours); err != nil {
			return nil, err
		}
		due, ok := pendingDue(now, dueAt, firstSeen, sql.NullString{}, hours, false)
		if ok && !now.Before(due) {
			ready = append(ready, ReadyPendingMovie{Name: name, Title: title, Magnet: magnet, Year: nullInt64Ptr(year)})
		}
	}
	return ready, rows.Err()
}

// RemovePendingMovie marks a pending movie release as downloaded.
func (d *Database) RemovePendingMovie(name string, year *int64) error {
	_, err := d.db.Exec("UPDATE pending_movies SET status='downloaded',downloaded_at=?1 WHERE name=?2 AND year IS ?3 AND status='pending'", nowSQLite(), name, year)
	return err
}

// SeriesArchivedMinSize returns the smallest archived episode size for a
// series, used by the adaptive size floor.
func (d *Database) SeriesArchivedMinSize(seriesName string) (*int64, error) {
	var value sql.NullInt64
	err := d.db.QueryRow("SELECT MIN(e.size_bytes) FROM episodes e JOIN series s ON s.id=e.series_id WHERE s.name=?1 AND e.size_bytes>0 AND (e.downloaded_at IS NOT NULL OR COALESCE(e.archive_path,'')<>'')", seriesName).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if value.Valid && value.Int64 > 0 {
		v := value.Int64
		return &v, nil
	}
	return nil, nil
}

// MovieArchivedSize returns the archived file size for a movie, used by the
// adaptive size floor.
func (d *Database) MovieArchivedSize(name string, year *int64) (*int64, error) {
	var value sql.NullInt64
	err := d.db.QueryRow("SELECT size_bytes FROM movies WHERE name=?1 AND year IS ?2 AND size_bytes>0 AND removed_at IS NULL", name, year).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if value.Valid && value.Int64 > 0 {
		v := value.Int64
		return &v, nil
	}
	return nil, nil
}

// LaterArchivedMaxResolutionRank returns the highest resolution rank among
// archived episodes after the given one.
func (d *Database) LaterArchivedMaxResolutionRank(seriesName string, season, episode int64) (*int, error) {
	rows, err := d.db.Query("SELECT COALESCE(e.title,'') FROM episodes e JOIN series s ON s.id=e.series_id WHERE s.name=?1 AND (e.season > ?2 OR (e.season = ?2 AND e.episode > ?3)) AND (e.downloaded_at IS NOT NULL OR COALESCE(e.archive_path,'') <> '')", seriesName, season, episode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var best *int
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			return nil, err
		}
		quality := ParseQuality(title)
		rank := quality.ResolutionRank()
		if rank > 0 && (best == nil || rank > *best) {
			value := rank
			best = &value
		}
	}
	return best, rows.Err()
}

// ArchiveGaps computes the missing episodes for every series/season.
func (d *Database) ArchiveGaps() ([]SeriesGap, error) {
	type seasonTargetKey struct {
		Series string
		Season int64
	}
	seasonTargets := make(map[seasonTargetKey]int64)
	rows, err := d.db.Query("SELECT s.name,e.season,MAX(e.episode) FROM episodes e JOIN series s ON s.id=e.series_id GROUP BY s.name,e.season")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		var season, count int64
		if err := rows.Scan(&name, &season, &count); err != nil {
			rows.Close()
			return nil, err
		}
		key := seasonTargetKey{Series: name, Season: season}
		if existing, ok := seasonTargets[key]; !ok || count > existing {
			seasonTargets[key] = count
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	metaRows, err := d.db.Query("SELECT series_name,season,episode_count FROM series_metadata")
	if err != nil {
		return nil, err
	}
	for metaRows.Next() {
		var name string
		var season, count int64
		if err := metaRows.Scan(&name, &season, &count); err != nil {
			metaRows.Close()
			return nil, err
		}
		key := seasonTargetKey{Series: name, Season: season}
		if existing, ok := seasonTargets[key]; !ok || count > existing {
			seasonTargets[key] = count
		}
	}
	if err := metaRows.Err(); err != nil {
		metaRows.Close()
		return nil, err
	}
	metaRows.Close()
	// Materialize the three sets once.
	completePacks := make(map[seasonTargetKey]struct{})
	packRows, err := d.db.Query("SELECT s.name,e.season FROM episodes e JOIN series s ON s.id=e.series_id WHERE e.episode=0 AND (e.downloaded_at IS NOT NULL OR EXISTS(SELECT 1 FROM torrent_meta t WHERE t.hash=e.magnet_hash AND t.status NOT IN ('error','removed')))")
	if err != nil {
		return nil, err
	}
	for packRows.Next() {
		var name string
		var season int64
		if err := packRows.Scan(&name, &season); err != nil {
			packRows.Close()
			return nil, err
		}
		completePacks[seasonTargetKey{Series: name, Season: season}] = struct{}{}
	}
	if err := packRows.Err(); err != nil {
		packRows.Close()
		return nil, err
	}
	packRows.Close()
	present := make(map[seasonTargetKey]map[int64]struct{})
	episodeRows, err := d.db.Query("SELECT s.name,e.season,e.episode FROM episodes e JOIN series s ON s.id=e.series_id")
	if err != nil {
		return nil, err
	}
	for episodeRows.Next() {
		var name string
		var season, episode int64
		if err := episodeRows.Scan(&name, &season, &episode); err != nil {
			episodeRows.Close()
			return nil, err
		}
		key := seasonTargetKey{Series: name, Season: season}
		if present[key] == nil {
			present[key] = make(map[int64]struct{})
		}
		present[key][episode] = struct{}{}
	}
	if err := episodeRows.Err(); err != nil {
		episodeRows.Close()
		return nil, err
	}
	episodeRows.Close()
	ignored := make(map[seasonTargetKey]map[int64]struct{})
	ignoredRows, err := d.db.Query("SELECT series_name,season,episode FROM ignored_episodes")
	if err != nil {
		return nil, err
	}
	for ignoredRows.Next() {
		var name string
		var season, episode int64
		if err := ignoredRows.Scan(&name, &season, &episode); err != nil {
			ignoredRows.Close()
			return nil, err
		}
		key := seasonTargetKey{Series: name, Season: season}
		if ignored[key] == nil {
			ignored[key] = make(map[int64]struct{})
		}
		ignored[key][episode] = struct{}{}
	}
	if err := ignoredRows.Err(); err != nil {
		ignoredRows.Close()
		return nil, err
	}
	ignoredRows.Close()
	downloading, err := d.activeDownloadEpisodes()
	if err != nil {
		return nil, err
	}
	keys := make([]seasonTargetKey, 0, len(seasonTargets))
	for key := range seasonTargets {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Series != keys[j].Series {
			return keys[i].Series < keys[j].Series
		}
		return keys[i].Season < keys[j].Season
	})
	gaps := make([]SeriesGap, 0)
	for _, key := range keys {
		maxEpisode := seasonTargets[key]
		if _, ok := completePacks[key]; ok {
			continue
		}
		if downloading.season(key.Series, key.Season) {
			continue
		}
		episodes := present[key]
		for episode := int64(1); episode <= maxEpisode; episode++ {
			if _, ok := ignored[key][episode]; ok {
				continue
			}
			if _, ok := episodes[episode]; ok {
				continue
			}
			if downloading.episode(key.Series, key.Season, episode) {
				continue
			}
			gaps = append(gaps, SeriesGap{Series: key.Series, Season: key.Season, Episode: episode})
		}
	}
	return gaps, nil
}

// activeDownloads are the episodes and whole seasons a torrent is still
// downloading or seeding. Downloads approved by the cycle already have an
// episode row; this also covers the ones added by hand, from a watched folder
// or from the phone, and season packs, so they are not searched as missing.
type activeDownloads struct {
	seasons  map[string]struct{}
	episodes map[string]struct{}
}

func activeDownloadKey(series string, season int64) string {
	return NormalizeSeriesName(series) + "\x00" + strconv.FormatInt(season, 10)
}

func (a activeDownloads) season(series string, season int64) bool {
	_, ok := a.seasons[activeDownloadKey(series, season)]
	return ok
}

func (a activeDownloads) episode(series string, season, episode int64) bool {
	_, ok := a.episodes[activeDownloadKey(series, season)+"\x00"+strconv.FormatInt(episode, 10)]
	return ok
}

func (d *Database) activeDownloadEpisodes() (activeDownloads, error) {
	active := activeDownloads{seasons: map[string]struct{}{}, episodes: map[string]struct{}{}}
	rows, err := d.db.Query("SELECT COALESCE(series_name,''), season, episode, COALESCE(metadata_json,'') FROM torrent_meta WHERE status NOT IN ('completed','error','removed') AND season IS NOT NULL AND COALESCE(series_name,'')<>''")
	if err != nil {
		return active, err
	}
	defer rows.Close()
	for rows.Next() {
		var name, metadataJSON string
		var season int64
		var episode sql.NullInt64
		if err := rows.Scan(&name, &season, &episode, &metadataJSON); err != nil {
			return active, err
		}
		key := activeDownloadKey(name, season)
		// A multi-episode release lists its episodes in the stored metadata.
		var metadata models.TorrentMeta
		if metadataJSON != "" && json.Unmarshal([]byte(metadataJSON), &metadata) == nil && len(metadata.Release.EpisodeRange) > 1 {
			for _, value := range metadata.Release.EpisodeRange {
				active.episodes[key+"\x00"+strconv.FormatInt(value, 10)] = struct{}{}
			}
			continue
		}
		if !episode.Valid || episode.Int64 == 0 {
			active.seasons[key] = struct{}{}
			continue
		}
		active.episodes[key+"\x00"+strconv.FormatInt(episode.Int64, 10)] = struct{}{}
	}
	return active, rows.Err()
}

// ArchiveGapsForSeries is the same gap calculation restricted to one series.
func (d *Database) ArchiveGapsForSeries(seriesName string) ([][2]int64, error) {
	targets := make(map[int64]int64)
	rows, err := d.db.Query("SELECT season,MAX(episode) FROM episodes WHERE series_id=(SELECT id FROM series WHERE name=?1) GROUP BY season", seriesName)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var season, count int64
		if err := rows.Scan(&season, &count); err != nil {
			rows.Close()
			return nil, err
		}
		if existing, ok := targets[season]; !ok || count > existing {
			targets[season] = count
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	metaRows, err := d.db.Query("SELECT season,episode_count FROM series_metadata WHERE series_name=?1", seriesName)
	if err != nil {
		return nil, err
	}
	for metaRows.Next() {
		var season, count int64
		if err := metaRows.Scan(&season, &count); err != nil {
			metaRows.Close()
			return nil, err
		}
		if existing, ok := targets[season]; !ok || count > existing {
			targets[season] = count
		}
	}
	if err := metaRows.Err(); err != nil {
		metaRows.Close()
		return nil, err
	}
	metaRows.Close()
	completeSeasons := make(map[int64]struct{})
	packRows, err := d.db.Query("SELECT e.season FROM episodes e WHERE e.series_id=(SELECT id FROM series WHERE name=?1) AND e.episode=0 AND (e.downloaded_at IS NOT NULL OR EXISTS(SELECT 1 FROM torrent_meta t WHERE t.hash=e.magnet_hash AND t.status NOT IN ('error','removed')))", seriesName)
	if err != nil {
		return nil, err
	}
	for packRows.Next() {
		var season int64
		if err := packRows.Scan(&season); err != nil {
			packRows.Close()
			return nil, err
		}
		completeSeasons[season] = struct{}{}
	}
	if err := packRows.Err(); err != nil {
		packRows.Close()
		return nil, err
	}
	packRows.Close()
	present := make(map[[2]int64]struct{})
	episodeRows, err := d.db.Query("SELECT season,episode FROM episodes WHERE series_id=(SELECT id FROM series WHERE name=?1)", seriesName)
	if err != nil {
		return nil, err
	}
	for episodeRows.Next() {
		var season, episode int64
		if err := episodeRows.Scan(&season, &episode); err != nil {
			episodeRows.Close()
			return nil, err
		}
		present[[2]int64{season, episode}] = struct{}{}
	}
	if err := episodeRows.Err(); err != nil {
		episodeRows.Close()
		return nil, err
	}
	episodeRows.Close()
	ignored := make(map[[2]int64]struct{})
	ignoredRows, err := d.db.Query("SELECT season,episode FROM ignored_episodes WHERE series_name=?1", seriesName)
	if err != nil {
		return nil, err
	}
	for ignoredRows.Next() {
		var season, episode int64
		if err := ignoredRows.Scan(&season, &episode); err != nil {
			ignoredRows.Close()
			return nil, err
		}
		ignored[[2]int64{season, episode}] = struct{}{}
	}
	if err := ignoredRows.Err(); err != nil {
		ignoredRows.Close()
		return nil, err
	}
	ignoredRows.Close()
	seasons := make([]int64, 0, len(targets))
	for season := range targets {
		seasons = append(seasons, season)
	}
	sort.Slice(seasons, func(i, j int) bool { return seasons[i] < seasons[j] })
	downloading, err := d.activeDownloadEpisodes()
	if err != nil {
		return nil, err
	}
	gaps := make([][2]int64, 0)
	for _, season := range seasons {
		if _, ok := completeSeasons[season]; ok {
			continue
		}
		if downloading.season(seriesName, season) {
			continue
		}
		for episode := int64(1); episode <= targets[season]; episode++ {
			if _, ok := ignored[[2]int64{season, episode}]; ok {
				continue
			}
			if _, ok := present[[2]int64{season, episode}]; ok {
				continue
			}
			if downloading.episode(seriesName, season, episode) {
				continue
			}
			gaps = append(gaps, [2]int64{season, episode})
		}
	}
	return gaps, nil
}

// EpisodeAirDates is the date cache for the global missing-episodes view.
func (d *Database) EpisodeAirDates() (map[airDateKey]string, error) {
	rows, err := d.db.Query("SELECT series_name,season,episode,air_date FROM episode_metadata")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	dates := make(map[airDateKey]string)
	for rows.Next() {
		var series, airDate string
		var season, episode int64
		if err := rows.Scan(&series, &season, &episode, &airDate); err != nil {
			return nil, err
		}
		dates[airDateKey{Series: series, Season: season, Episode: episode}] = airDate
	}
	return dates, rows.Err()
}

// UnarchivedEpisodesForSeries returns the expected episodes of a series that do
// not have an archive path yet (manual "Cerca mancanti").
func (d *Database) UnarchivedEpisodesForSeries(seriesName string, ignoredSeasons []int64) ([][2]int64, error) {
	targets := make(map[int64]int64)
	metaRows, err := d.db.Query("SELECT season,episode_count FROM series_metadata WHERE series_name=?1", seriesName)
	if err != nil {
		return nil, err
	}
	for metaRows.Next() {
		var season, count int64
		if err := metaRows.Scan(&season, &count); err != nil {
			metaRows.Close()
			return nil, err
		}
		if existing, ok := targets[season]; !ok || count > existing {
			targets[season] = count
		}
	}
	if err := metaRows.Err(); err != nil {
		metaRows.Close()
		return nil, err
	}
	metaRows.Close()
	knownRows, err := d.db.Query("SELECT e.season,MAX(e.episode) FROM episodes e JOIN series s ON s.id=e.series_id WHERE s.name=?1 AND e.episode>0 GROUP BY e.season", seriesName)
	if err != nil {
		return nil, err
	}
	for knownRows.Next() {
		var season, count int64
		if err := knownRows.Scan(&season, &count); err != nil {
			knownRows.Close()
			return nil, err
		}
		if existing, ok := targets[season]; !ok || count > existing {
			targets[season] = count
		}
	}
	if err := knownRows.Err(); err != nil {
		knownRows.Close()
		return nil, err
	}
	knownRows.Close()
	archived := make(map[[2]int64]struct{})
	archivedRows, err := d.db.Query("SELECT e.season,e.episode FROM episodes e JOIN series s ON s.id=e.series_id WHERE s.name=?1 AND e.episode>0 AND COALESCE(e.archive_path,'')<>''", seriesName)
	if err != nil {
		return nil, err
	}
	for archivedRows.Next() {
		var season, episode int64
		if err := archivedRows.Scan(&season, &episode); err != nil {
			archivedRows.Close()
			return nil, err
		}
		archived[[2]int64{season, episode}] = struct{}{}
	}
	if err := archivedRows.Err(); err != nil {
		archivedRows.Close()
		return nil, err
	}
	archivedRows.Close()
	ignored := make(map[[2]int64]struct{})
	ignoredRows, err := d.db.Query("SELECT season,episode FROM ignored_episodes WHERE series_name=?1", seriesName)
	if err != nil {
		return nil, err
	}
	for ignoredRows.Next() {
		var season, episode int64
		if err := ignoredRows.Scan(&season, &episode); err != nil {
			ignoredRows.Close()
			return nil, err
		}
		ignored[[2]int64{season, episode}] = struct{}{}
	}
	if err := ignoredRows.Err(); err != nil {
		ignoredRows.Close()
		return nil, err
	}
	ignoredRows.Close()
	seasons := make([]int64, 0, len(targets))
	for season := range targets {
		seasons = append(seasons, season)
	}
	sort.Slice(seasons, func(i, j int) bool { return seasons[i] < seasons[j] })
	missing := make([][2]int64, 0)
	for _, season := range seasons {
		if containsInt64Value(ignoredSeasons, season) {
			continue
		}
		for episode := int64(1); episode <= targets[season]; episode++ {
			if _, ok := ignored[[2]int64{season, episode}]; ok {
				continue
			}
			if _, ok := archived[[2]int64{season, episode}]; ok {
				continue
			}
			missing = append(missing, [2]int64{season, episode})
		}
	}
	return missing, nil
}

// EpisodesForSeries returns every episode of a series, merging the configured
// ignored seasons with the DB ones.
func (d *Database) EpisodesForSeries(seriesName string, extraIgnored []int64) ([]EpisodeView, error) {
	ignoredSeasons := make(map[int64]struct{})
	var ignoredJSON string
	err := d.db.QueryRow("SELECT COALESCE(ignored_seasons,'[]') FROM series WHERE name=?1", seriesName).Scan(&ignoredJSON)
	if err == nil {
		var seasons []int64
		if json.Unmarshal([]byte(ignoredJSON), &seasons) == nil {
			for _, season := range seasons {
				ignoredSeasons[season] = struct{}{}
			}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	for _, season := range extraIgnored {
		ignoredSeasons[season] = struct{}{}
	}
	airDates := make(map[[2]int64]string)
	airRows, err := d.db.Query("SELECT season,episode,air_date FROM episode_metadata WHERE series_name=?1", seriesName)
	if err != nil {
		return nil, err
	}
	for airRows.Next() {
		var season, episode int64
		var airDate string
		if err := airRows.Scan(&season, &episode, &airDate); err != nil {
			airRows.Close()
			return nil, err
		}
		airDates[[2]int64{season, episode}] = airDate
	}
	if err := airRows.Err(); err != nil {
		airRows.Close()
		return nil, err
	}
	airRows.Close()
	ignoredEpisodes := make(map[[2]int64]struct{})
	ignoredRows, err := d.db.Query("SELECT season,episode FROM ignored_episodes WHERE series_name=?1", seriesName)
	if err != nil {
		return nil, err
	}
	for ignoredRows.Next() {
		var season, episode int64
		if err := ignoredRows.Scan(&season, &episode); err != nil {
			ignoredRows.Close()
			return nil, err
		}
		ignoredEpisodes[[2]int64{season, episode}] = struct{}{}
	}
	if err := ignoredRows.Err(); err != nil {
		ignoredRows.Close()
		return nil, err
	}
	ignoredRows.Close()
	rows, err := d.db.Query(`SELECT e.id,s.name,e.season,e.episode,COALESCE(e.title,''),e.quality_score,
                    e.downloaded_at,e.archive_path,COALESCE(e.size_bytes,0),e.magnet_hash,
                    e.magnet_link,
                    CASE WHEN e.downloaded_at IS NOT NULL THEN 'downloaded'
                         ELSE COALESCE(t.status,'missing') END,
                    COALESCE(t.error,'')
             FROM episodes e
             JOIN series s ON s.id=e.series_id
             LEFT JOIN torrent_meta t ON lower(t.hash)=lower(e.magnet_hash)
             WHERE s.name=?1 AND e.episode > 0
             ORDER BY e.season,e.episode`, seriesName)
	if err != nil {
		return nil, err
	}
	items := make([]EpisodeView, 0)
	for rows.Next() {
		var item EpisodeView
		var downloadedAt, archivePath, magnetHash, magnetLink sql.NullString
		if err := rows.Scan(&item.ID, &item.SeriesName, &item.Season, &item.Episode, &item.Title, &item.QualityScore, &downloadedAt, &archivePath, &item.SizeBytes, &magnetHash, &magnetLink, &item.Status, &item.Error); err != nil {
			rows.Close()
			return nil, err
		}
		item.AirDate = airDates[[2]int64{item.Season, item.Episode}]
		item.DownloadedAt = nullStringPtr(downloadedAt)
		item.ArchivePath = nullStringPtr(archivePath)
		item.MagnetHash = nullStringPtr(magnetHash)
		item.MagnetLink = nullStringPtr(magnetLink)
		_, item.Ignored = ignoredEpisodes[[2]int64{item.Season, item.Episode}]
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	filtered := items[:0]
	for _, item := range items {
		if _, ok := ignoredSeasons[item.Season]; ok {
			continue
		}
		filtered = append(filtered, item)
	}
	items = filtered
	present := make(map[[2]int64]struct{}, len(items))
	for _, item := range items {
		present[[2]int64{item.Season, item.Episode}] = struct{}{}
	}
	expectedRows, err := d.db.Query("SELECT season,episode_count FROM series_metadata WHERE series_name=?1 ORDER BY season", seriesName)
	if err != nil {
		return nil, err
	}
	expected := make([][2]int64, 0)
	for expectedRows.Next() {
		var season, count int64
		if err := expectedRows.Scan(&season, &count); err != nil {
			expectedRows.Close()
			return nil, err
		}
		expected = append(expected, [2]int64{season, count})
	}
	if err := expectedRows.Err(); err != nil {
		expectedRows.Close()
		return nil, err
	}
	expectedRows.Close()
	for _, pair := range expected {
		season, count := pair[0], pair[1]
		if _, ok := ignoredSeasons[season]; ok {
			continue
		}
		for episode := int64(1); episode <= count; episode++ {
			if _, ok := present[[2]int64{season, episode}]; ok {
				continue
			}
			_, ignored := ignoredEpisodes[[2]int64{season, episode}]
			items = append(items, EpisodeView{
				ID:           0,
				SeriesName:   seriesName,
				Season:       season,
				Episode:      episode,
				Title:        "",
				AirDate:      airDates[[2]int64{season, episode}],
				RenamedTitle: "",
				QualityScore: 0,
				DownloadedAt: nil,
				ArchivePath:  nil,
				SizeBytes:    0,
				MagnetHash:   nil,
				MagnetLink:   nil,
				Status:       "missing",
				Error:        "",
				Ignored:      ignored,
			})
		}
	}
	// Titolo mostrato nel dettaglio serie: il nome del file rinominato.
	for index := range items {
		if items[index].ArchivePath != nil {
			items[index].RenamedTitle = renamedFileTitle(*items[index].ArchivePath)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Season != items[j].Season {
			return items[i].Season < items[j].Season
		}
		return items[i].Episode < items[j].Episode
	})
	return items, nil
}

// DownloadedMovies returns the downloaded movies of the history view.
func (d *Database) DownloadedMovies(limit int) ([]MovieHistory, error) {
	rows, err := d.db.Query("SELECT id,COALESCE(name,''),year,COALESCE(title,name,''),quality_score,downloaded_at,COALESCE(size_bytes,0),magnet_hash FROM movies WHERE downloaded_at IS NOT NULL AND removed_at IS NULL ORDER BY downloaded_at DESC LIMIT ?1", clampInt(limit, 1, 2000))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	movies := make([]MovieHistory, 0)
	for rows.Next() {
		var movie MovieHistory
		var year sql.NullInt64
		var downloadedAt, magnetHash sql.NullString
		if err := rows.Scan(&movie.ID, &movie.Name, &year, &movie.Title, &movie.QualityScore, &downloadedAt, &movie.SizeBytes, &magnetHash); err != nil {
			return nil, err
		}
		movie.Year = nullInt64Ptr(year)
		movie.DownloadedAt = nullStringPtr(downloadedAt)
		movie.MagnetHash = nullStringPtr(magnetHash)
		movies = append(movies, movie)
	}
	return movies, rows.Err()
}

// ConsumptionStats returns the aggregate download consumption.
func (d *Database) ConsumptionStats() (ConsumptionStats, error) {
	var total int64
	if err := d.db.QueryRow("SELECT COALESCE((SELECT SUM(size_bytes) FROM episodes WHERE downloaded_at IS NOT NULL),0) + COALESCE((SELECT SUM(size_bytes) FROM movies WHERE downloaded_at IS NOT NULL AND removed_at IS NULL),0)").Scan(&total); err != nil {
		return ConsumptionStats{}, err
	}
	var last30 int64
	if err := d.db.QueryRow("SELECT COALESCE((SELECT SUM(size_bytes) FROM episodes WHERE datetime(downloaded_at) >= datetime('now','-30 days')),0) + COALESCE((SELECT SUM(size_bytes) FROM movies WHERE datetime(downloaded_at) >= datetime('now','-30 days') AND removed_at IS NULL),0)").Scan(&last30); err != nil {
		return ConsumptionStats{}, err
	}
	var last7 int64
	if err := d.db.QueryRow("SELECT COALESCE((SELECT SUM(size_bytes) FROM episodes WHERE datetime(downloaded_at) >= datetime('now','-7 days')),0) + COALESCE((SELECT SUM(size_bytes) FROM movies WHERE datetime(downloaded_at) >= datetime('now','-7 days') AND removed_at IS NULL),0)").Scan(&last7); err != nil {
		return ConsumptionStats{}, err
	}
	rows, err := d.db.Query("SELECT date, SUM(bytes) FROM (SELECT substr(downloaded_at,1,10) AS date, COALESCE(size_bytes,0) AS bytes FROM episodes WHERE datetime(downloaded_at) >= datetime('now','-7 days') UNION ALL SELECT substr(downloaded_at,1,10), COALESCE(size_bytes,0) FROM movies WHERE datetime(downloaded_at) >= datetime('now','-7 days') AND removed_at IS NULL) WHERE date IS NOT NULL GROUP BY date ORDER BY date")
	if err != nil {
		return ConsumptionStats{}, err
	}
	defer rows.Close()
	daily := make([]DailyConsumption, 0)
	for rows.Next() {
		var day DailyConsumption
		if err := rows.Scan(&day.Date, &day.Bytes); err != nil {
			return ConsumptionStats{}, err
		}
		daily = append(daily, day)
	}
	if err := rows.Err(); err != nil {
		return ConsumptionStats{}, err
	}
	return ConsumptionStats{
		TotalBytes:      total,
		Last30DaysBytes: last30,
		Last7DaysBytes:  last7,
		Daily7d:         daily,
	}, nil
}

// RecentDownloads merges series and movie downloads.
func (d *Database) RecentDownloads(limit int) ([]RecentDownload, error) {
	rows, err := d.db.Query("SELECT kind,name,season,episode,year,downloaded_at,size_bytes,archive_path,quality_score FROM (SELECT 'series' AS kind,s.name AS name,e.season AS season,e.episode AS episode,NULL AS year,e.downloaded_at AS downloaded_at,COALESCE(e.size_bytes,0) AS size_bytes,e.archive_path AS archive_path,e.quality_score AS quality_score FROM episodes e JOIN series s ON s.id=e.series_id WHERE e.downloaded_at IS NOT NULL UNION ALL SELECT 'movie' AS kind,COALESCE(m.name,m.title) AS name,NULL AS season,NULL AS episode,m.year AS year,m.downloaded_at AS downloaded_at,COALESCE(m.size_bytes,0) AS size_bytes,NULL AS archive_path,m.quality_score AS quality_score FROM movies m WHERE m.downloaded_at IS NOT NULL AND m.removed_at IS NULL) ORDER BY downloaded_at DESC LIMIT ?1", clampInt(limit, 1, 2000))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	downloads := make([]RecentDownload, 0)
	for rows.Next() {
		var item RecentDownload
		var season, episode, year sql.NullInt64
		var archivePath sql.NullString
		if err := rows.Scan(&item.Kind, &item.Name, &season, &episode, &year, &item.DownloadedAt, &item.SizeBytes, &archivePath, &item.QualityScore); err != nil {
			return nil, err
		}
		item.Season = nullInt64Ptr(season)
		item.Episode = nullInt64Ptr(episode)
		item.Year = nullInt64Ptr(year)
		item.ArchivePath = nullStringPtr(archivePath)
		downloads = append(downloads, item)
	}
	return downloads, rows.Err()
}

// SetEpisodeIgnored marks (or unmarks) an episode as ignored.
func (d *Database) SetEpisodeIgnored(seriesName string, season, episode int64, ignored bool, reason string) error {
	if ignored {
		_, err := d.db.Exec("INSERT INTO ignored_episodes(series_name,season,episode,reason) VALUES (?1,?2,?3,?4) ON CONFLICT(series_name,season,episode) DO UPDATE SET reason=excluded.reason", seriesName, season, episode, reason)
		return err
	}
	_, err := d.db.Exec("DELETE FROM ignored_episodes WHERE series_name=?1 AND season=?2 AND episode=?3", seriesName, season, episode)
	return err
}

// ResetEpisode resets the state of one episode.
func (d *Database) ResetEpisode(seriesName string, season, episode int64, removeHistory bool) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.Exec("DELETE FROM pending_downloads WHERE series_id=(SELECT id FROM series WHERE name=?1) AND season=?2 AND episode=?3", seriesName, season, episode); err != nil {
		return err
	}
	if removeHistory {
		if _, err := tx.Exec("DELETE FROM episodes WHERE series_id=(SELECT id FROM series WHERE name=?1) AND season=?2 AND episode=?3", seriesName, season, episode); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("DELETE FROM ignored_episodes WHERE series_name=?1 AND season=?2 AND episode=?3", seriesName, season, episode); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// SeriesMetadataStale reports whether the series metadata is older than
// `max_age_hours`.
func (d *Database) SeriesMetadataStale(seriesName string, maxAgeHours int64) (bool, error) {
	var updated sql.NullString
	err := d.db.QueryRow("SELECT MAX(updated_at) FROM series_metadata WHERE series_name=?1", seriesName).Scan(&updated)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if !updated.Valid {
		return true, nil
	}
	value, ok := utils.ParseTimestamp(updated.String)
	if !ok {
		return true, nil
	}
	return int64(time.Now().UTC().Sub(value).Hours()) >= maxAgeHours, nil
}

// SaveSeriesMetadata upserts the TMDB season episode counts of a series.
func (d *Database) SaveSeriesMetadata(seriesName string, counts [][2]int64) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	now := nowSQLite()
	for _, pair := range counts {
		season, count := pair[0], pair[1]
		if season <= 0 || count <= 0 {
			continue
		}
		if _, err := tx.Exec("INSERT INTO series_metadata(series_name,season,episode_count,updated_at) VALUES (?1,?2,?3,?4) ON CONFLICT(series_name,season) DO UPDATE SET episode_count=excluded.episode_count,updated_at=excluded.updated_at", seriesName, season, count, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// SaveSeriesStatus stores the TMDB status of a series.
func (d *Database) SaveSeriesStatus(seriesName, status, lastAirDate string) error {
	_, err := d.db.Exec("INSERT INTO series_status(series_name,status,last_air_date,updated_at) VALUES (?1,?2,?3,?4) ON CONFLICT(series_name) DO UPDATE SET status=excluded.status,last_air_date=excluded.last_air_date,updated_at=excluded.updated_at", seriesName, status, lastAirDate, nowSQLite())
	return err
}

// SeriesStatuses returns every stored series status.
func (d *Database) SeriesStatuses() (map[string]string, error) {
	rows, err := d.db.Query("SELECT series_name,status FROM series_status")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	statuses := make(map[string]string)
	for rows.Next() {
		var name, status string
		if err := rows.Scan(&name, &status); err != nil {
			return nil, err
		}
		statuses[name] = status
	}
	return statuses, rows.Err()
}

// SaveEpisodeAirDates caches the TMDB air dates of a series.
func (d *Database) SaveEpisodeAirDates(seriesName string, episodes []EpisodeAirDate) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	now := nowSQLite()
	for _, episode := range episodes {
		if episode.Season < 1 || episode.Episode < 1 {
			continue
		}
		if _, err := tx.Exec("INSERT INTO episode_metadata(series_name,season,episode,air_date,updated_at) VALUES (?1,?2,?3,?4,?5) ON CONFLICT(series_name,season,episode) DO UPDATE SET air_date=excluded.air_date,updated_at=excluded.updated_at", seriesName, episode.Season, episode.Episode, episode.AirDate, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// SyncArchiveFile records an archived episode file, scoring it from its title.
func (d *Database) SyncArchiveFile(seriesName string, season, episode int64, title, path string, sizeBytes int64) error {
	quality := ParseQuality(title)
	return d.SyncArchiveFileScored(seriesName, season, episode, title, path, sizeBytes, quality.Score())
}

// SyncArchiveFileScored records an archived episode file with an explicit
// score.
func (d *Database) SyncArchiveFileScored(seriesName string, season, episode int64, title, path string, sizeBytes, qualityScore int64) error {
	if _, err := d.db.Exec("INSERT OR IGNORE INTO series(name) VALUES (?1)", seriesName); err != nil {
		return err
	}
	var seriesID int64
	if err := d.db.QueryRow("SELECT id FROM series WHERE name=?1", seriesName).Scan(&seriesID); err != nil {
		return err
	}
	// A scan may encounter duplicate files in arbitrary filesystem order. Never
	// replace the path of a better existing copy with an inferior manual/legacy
	// file; do replace it when the recorded path has disappeared.
	replacePath := true
	var currentPath string
	var currentScore int64
	rowErr := d.db.QueryRow("SELECT COALESCE(archive_path,''),quality_score FROM episodes WHERE series_id=?1 AND season=?2 AND episode=?3", seriesID, season, episode).Scan(&currentPath, &currentScore)
	if rowErr == nil && qualityScore < currentScore && strings.TrimSpace(currentPath) != "" {
		if _, statErr := os.Stat(currentPath); statErr == nil {
			replacePath = false
		}
	} else if rowErr != nil && !errors.Is(rowErr, sql.ErrNoRows) {
		return rowErr
	}
	_, err := d.db.Exec("INSERT INTO episodes(series_id,season,episode,title,quality_score,downloaded_at,archive_path,size_bytes) VALUES (?1,?2,?3,?4,?5,datetime('now'),?6,?7) ON CONFLICT(series_id,season,episode) DO UPDATE SET title=CASE WHEN excluded.quality_score>=episodes.quality_score THEN excluded.title ELSE episodes.title END,downloaded_at=excluded.downloaded_at,archive_path=CASE WHEN ?8 THEN excluded.archive_path ELSE episodes.archive_path END,size_bytes=CASE WHEN ?8 THEN excluded.size_bytes ELSE episodes.size_bytes END,quality_score=MAX(excluded.quality_score, episodes.quality_score)", seriesID, season, episode, title, qualityScore, path, sizeBytes, replacePath)
	return err
}

// SetEpisodeArchivePath updates the stored archive path of an episode,
// together with the real size and (monotonic) quality score of the file.
func (d *Database) SetEpisodeArchivePath(cfg *Config, seriesName string, season, episode int64, path string) error {
	sizeBytes, qualityScore := fileStats(cfg, path, "series", seriesName)
	_, err := d.db.Exec("UPDATE episodes SET archive_path=?1, downloaded_at=COALESCE(downloaded_at, datetime('now')), size_bytes=CASE WHEN ?2>0 THEN ?2 ELSE size_bytes END, quality_score=CASE WHEN ?3>quality_score THEN ?3 ELSE quality_score END WHERE series_id=(SELECT id FROM series WHERE name=?4) AND season=?5 AND episode=?6", path, sizeBytes, qualityScore, seriesName, season, episode)
	return err
}

// RefreshEpisodeFileStats re-aligns size and quality score of an episode to the
// file it currently points to, without touching its title.
func (d *Database) RefreshEpisodeFileStats(cfg *Config, seriesName string, season, episode int64, path string) error {
	sizeBytes, qualityScore := fileStats(cfg, path, "series", seriesName)
	_, err := d.db.Exec("UPDATE episodes SET size_bytes=CASE WHEN ?1>0 THEN ?1 ELSE size_bytes END, quality_score=CASE WHEN ?2>quality_score THEN ?2 ELSE quality_score END, downloaded_at=COALESCE(downloaded_at, datetime('now')) WHERE series_id=(SELECT id FROM series WHERE name=?3) AND season=?4 AND episode=?5", sizeBytes, qualityScore, seriesName, season, episode)
	return err
}

// SeriesSeasonCounts returns the recorded TMDB episode counts of a series.
func (d *Database) SeriesSeasonCounts(seriesName string) ([][2]int64, error) {
	rows, err := d.db.Query("SELECT season, episode_count FROM series_metadata WHERE series_name=?1 ORDER BY season", seriesName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make([][2]int64, 0)
	for rows.Next() {
		var season, count int64
		if err := rows.Scan(&season, &count); err != nil {
			return nil, err
		}
		counts = append(counts, [2]int64{season, count})
	}
	return counts, rows.Err()
}

// SeriesSeasonCountsBulk returns the recorded episode counts of every series in
// one query.
func (d *Database) SeriesSeasonCountsBulk() (map[string][][2]int64, error) {
	rows, err := d.db.Query("SELECT series_name, season, episode_count FROM series_metadata ORDER BY series_name, season")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[string][][2]int64)
	for rows.Next() {
		var name string
		var season, count int64
		if err := rows.Scan(&name, &season, &count); err != nil {
			return nil, err
		}
		counts[name] = append(counts[name], [2]int64{season, count})
	}
	return counts, rows.Err()
}

// GapRecentlySearched reports whether a gap was searched in the last `hours`.
func (d *Database) GapRecentlySearched(seriesName string, season, episode, hours int64) (bool, error) {
	var found sql.NullString
	err := d.db.QueryRow("SELECT last_searched_at FROM gap_search_log WHERE series_name=?1 AND season=?2 AND episode=?3", seriesName, season, episode).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !found.Valid {
		return false, nil
	}
	value, ok := utils.ParseTimestamp(found.String)
	if !ok {
		return false, nil
	}
	return int64(time.Now().UTC().Sub(value).Hours()) < hours, nil
}

// MarkGapSearched records the last search time of a gap.
func (d *Database) MarkGapSearched(seriesName string, season, episode int64) error {
	_, err := d.db.Exec("INSERT INTO gap_search_log(series_name,season,episode,last_searched_at) VALUES (?1,?2,?3,?4) ON CONFLICT(series_name,season,episode) DO UPDATE SET last_searched_at=excluded.last_searched_at", seriesName, season, episode, nowSQLite())
	return err
}

// ClearGapSearched forgets the last online search of a gap so the next cycle
// retries it immediately. Used when a dead-swarm download is abandoned and an
// alternative release must be sought without waiting for the throttle.
func (d *Database) ClearGapSearched(seriesName string, season, episode int64) error {
	_, err := d.db.Exec("DELETE FROM gap_search_log WHERE series_name=?1 AND season=?2 AND episode=?3", seriesName, season, episode)
	return err
}

// SetTorrentTag sets the tag of a torrent, creating the row when it is a
// manually added torrent.
func (d *Database) SetTorrentTag(hash, tag string) error {
	result, err := d.db.Exec("UPDATE torrent_meta SET tag=?1, updated_at=datetime('now') WHERE lower(hash)=lower(?2)", tag, hash)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated == 0 {
		_, err = d.db.Exec("INSERT INTO torrent_meta(hash,tag,status,created_at,updated_at) VALUES (?1,?2,'queued',datetime('now'),datetime('now'))", strings.ToLower(hash), tag)
		return err
	}
	return nil
}

// TorrentTags returns the `(hash, tag)` pairs of tagged torrents.
func (d *Database) TorrentTags() ([][2]string, error) {
	rows, err := d.db.Query("SELECT lower(hash), COALESCE(tag,'') FROM torrent_meta WHERE TRIM(COALESCE(tag,'')) != ''")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := make([][2]string, 0)
	for rows.Next() {
		var hash, tag string
		if err := rows.Scan(&hash, &tag); err != nil {
			return nil, err
		}
		tags = append(tags, [2]string{hash, tag})
	}
	return tags, rows.Err()
}

// TorrentProcessed returns the path where a completed torrent was archived.
func (d *Database) TorrentProcessed(hash string) (*string, error) {
	var processed sql.NullString
	err := d.db.QueryRow("SELECT processed_path FROM torrent_meta WHERE hash=?1", strings.ToLower(hash)).Scan(&processed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return nullStringPtr(processed), nil
}

// SetTorrentReason records why a release was accepted and put in download.
func (d *Database) SetTorrentReason(hash, reason string) error {
	_, err := d.db.Exec("UPDATE torrent_meta SET reason=?2 WHERE hash=?1", strings.ToLower(hash), reason)
	return err
}

// TorrentAux returns `(processed_path, source, reason)` for a torrent.
func (d *Database) TorrentAux(hash string) ([3]string, error) {
	var processed, source, reason string
	err := d.db.QueryRow("SELECT COALESCE(processed_path,''), COALESCE(source,''), COALESCE(reason,'') FROM torrent_meta WHERE hash=?1", strings.ToLower(hash)).Scan(&processed, &source, &reason)
	if errors.Is(err, sql.ErrNoRows) {
		return [3]string{}, nil
	}
	if err != nil {
		return [3]string{}, err
	}
	return [3]string{processed, source, reason}, nil
}

// TorrentAuxBulk is `TorrentAux` for many hashes in one query.
func (d *Database) TorrentAuxBulk(hashes []string) (map[string][3]string, error) {
	aux := make(map[string][3]string)
	if len(hashes) == 0 {
		return aux, nil
	}
	lowered := make([]string, len(hashes))
	for index, hash := range hashes {
		lowered[index] = strings.ToLower(hash)
	}
	for start := 0; start < len(lowered); start += 500 {
		end := start + 500
		if end > len(lowered) {
			end = len(lowered)
		}
		chunk := lowered[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")
		query := "SELECT hash, COALESCE(processed_path,''), COALESCE(source,''), COALESCE(reason,'') FROM torrent_meta WHERE hash IN (" + placeholders + ")"
		args := make([]any, len(chunk))
		for index, value := range chunk {
			args[index] = value
		}
		rows, err := d.db.Query(query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var hash, processed, source, reason string
			if err := rows.Scan(&hash, &processed, &source, &reason); err != nil {
				rows.Close()
				return nil, err
			}
			aux[strings.ToLower(hash)] = [3]string{processed, source, reason}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return aux, nil
}

// TorrentReason returns the recorded reason for a torrent, if present and not
// empty.
func (d *Database) TorrentReason(hash string) (*string, error) {
	var reason string
	err := d.db.QueryRow("SELECT COALESCE(reason,'') FROM torrent_meta WHERE hash=?1", strings.ToLower(hash)).Scan(&reason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(reason) == "" {
		return nil, nil
	}
	return &reason, nil
}

// TorrentStatus returns the stored status of a torrent.
func (d *Database) TorrentStatus(hash string) (*string, error) {
	var status string
	err := d.db.QueryRow("SELECT COALESCE(status,'queued') FROM torrent_meta WHERE hash=?1", strings.ToLower(hash)).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &status, nil
}

// LoadStallWatches restores torrents deliberately parked by the stalled
// download monitor. Normal user-paused torrents are not represented here.
func (d *Database) LoadStallWatches() (map[string]StallWatch, error) {
	watches := map[string]StallWatch{}
	if d == nil || d.db == nil {
		return watches, nil
	}
	rows, err := d.db.Query(`SELECT hash, last_progress_at, last_done, stalled_since,
		next_retry_at, COALESCE(next_notice_at, ''), notice_step FROM stalled_torrents`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var hash, lastProgress, stalledSince, nextRetry, nextNotice string
		var lastDone int64
		var step uint8
		if err := rows.Scan(&hash, &lastProgress, &lastDone, &stalledSince, &nextRetry, &nextNotice, &step); err != nil {
			return nil, err
		}
		parse := func(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }
		progressAt, err := parse(lastProgress)
		if err != nil {
			continue
		}
		entry := StallWatch{lastProgressAt: progressAt, lastDone: lastDone, retryStep: step, persisted: true}
		// An empty stalled_since is an idle download not parked yet: only its
		// progress clock was saved.
		if stalledSince != "" {
			stalledAt, err := parse(stalledSince)
			if err != nil {
				continue
			}
			retryAt, err := parse(nextRetry)
			if err != nil {
				continue
			}
			entry.stalledSince = &stalledAt
			entry.nextRetryAt = retryAt
		}
		watches[strings.ToLower(hash)] = entry
	}
	return watches, rows.Err()
}

// SaveStallWatch atomically records a stalled torrent's retry state, or the
// progress clock of a download idle long enough to be persisted (empty
// stalled_since).
func (d *Database) SaveStallWatch(hash string, entry StallWatch) error {
	if d == nil || d.db == nil {
		return nil
	}
	stalledSince := ""
	if entry.stalledSince != nil {
		stalledSince = entry.stalledSince.UTC().Format(time.RFC3339Nano)
	}
	// next_notice_at is a leftover of the old log-only backoff: the retry
	// itself now backs off, so only notice_step (the retry step) is used.
	nextNotice := ""
	_, err := d.db.Exec(`INSERT INTO stalled_torrents(hash,last_progress_at,last_done,stalled_since,next_retry_at,next_notice_at,notice_step,updated_at)
		VALUES(?1,?2,?3,?4,?5,?6,?7,?8)
		ON CONFLICT(hash) DO UPDATE SET last_progress_at=excluded.last_progress_at,last_done=excluded.last_done,
		stalled_since=excluded.stalled_since,next_retry_at=excluded.next_retry_at,next_notice_at=excluded.next_notice_at,
		notice_step=excluded.notice_step,updated_at=excluded.updated_at`,
		strings.ToLower(hash), entry.lastProgressAt.UTC().Format(time.RFC3339Nano), entry.lastDone,
		stalledSince, entry.nextRetryAt.UTC().Format(time.RFC3339Nano),
		nextNotice, entry.retryStep, nowSQLite())
	return err
}

// TorrentMoveState is the persisted storage-move state of one torrent.
type TorrentMoveState struct {
	Retry         *StorageMoveRetry
	PostSeedGuard bool
}

// LoadTorrentMoves restores the storage-move state saved by SaveTorrentMove.
// The in-flight flag and the next attempt time are not stored: after a restart
// no move is running, and the caller decides when to check them again.
func (d *Database) LoadTorrentMoves() (map[string]TorrentMoveState, error) {
	moves := map[string]TorrentMoveState{}
	if d == nil || d.db == nil {
		return moves, nil
	}
	rows, err := d.db.Query(`SELECT hash, has_retry, destination, retry_post_seed, attempts, post_seed_protected FROM torrent_moves`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var hash, destination string
		var hasRetry, retryPostSeed, protected bool
		var attempts int64
		if err := rows.Scan(&hash, &hasRetry, &destination, &retryPostSeed, &attempts, &protected); err != nil {
			return nil, err
		}
		state := TorrentMoveState{PostSeedGuard: protected}
		if hasRetry && strings.TrimSpace(destination) != "" {
			if attempts < 0 {
				attempts = 0
			}
			if attempts > int64(tev_maxStorageMoveRetries) {
				attempts = int64(tev_maxStorageMoveRetries)
			}
			state.Retry = &StorageMoveRetry{destination: destination, postSeed: retryPostSeed, attempts: uint8(attempts)}
		}
		moves[strings.ToLower(hash)] = state
	}
	return moves, rows.Err()
}

// SaveTorrentMove records the storage-move state of a torrent, or deletes its
// row when there is nothing left to remember.
func (d *Database) SaveTorrentMove(hash string, state TorrentMoveState) error {
	if d == nil || d.db == nil {
		return nil
	}
	if state.Retry == nil && !state.PostSeedGuard {
		_, err := d.db.Exec("DELETE FROM torrent_moves WHERE hash=?1", strings.ToLower(hash))
		return err
	}
	var destination string
	var retryPostSeed bool
	var attempts uint8
	if state.Retry != nil {
		destination = state.Retry.destination
		retryPostSeed = state.Retry.postSeed
		attempts = state.Retry.attempts
	}
	_, err := d.db.Exec(`INSERT INTO torrent_moves(hash,has_retry,destination,retry_post_seed,attempts,post_seed_protected,updated_at)
		VALUES(?1,?2,?3,?4,?5,?6,?7)
		ON CONFLICT(hash) DO UPDATE SET has_retry=excluded.has_retry,destination=excluded.destination,
		retry_post_seed=excluded.retry_post_seed,attempts=excluded.attempts,
		post_seed_protected=excluded.post_seed_protected,updated_at=excluded.updated_at`,
		strings.ToLower(hash), boolToInt(state.Retry != nil), destination, boolToInt(retryPostSeed),
		int64(attempts), boolToInt(state.PostSeedGuard), nowSQLite())
	return err
}

// DeleteStallWatch forgets monitor state once progress resumes, completion is
// reached, or the torrent leaves the session.
func (d *Database) DeleteStallWatch(hash string) error {
	if d == nil || d.db == nil {
		return nil
	}
	_, err := d.db.Exec("DELETE FROM stalled_torrents WHERE hash=?1", strings.ToLower(hash))
	return err
}

// TorrentMeta returns the release associated with a torrent hash, restoring it
// from episodes/movies when no metadata was stored.
func (d *Database) TorrentMeta(hash string) (*models.TorrentMeta, error) {
	normalized := strings.ToLower(hash)
	var metadata sql.NullString
	err := d.db.QueryRow("SELECT metadata_json FROM torrent_meta WHERE hash=?1", normalized).Scan(&metadata)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if metadata.Valid && metadata.String != "" {
		var meta models.TorrentMeta
		if err := json.Unmarshal([]byte(metadata.String), &meta); err != nil {
			return nil, err
		}
		return &meta, nil
	}
	var seriesTitle, seriesMagnet sql.NullString
	var seriesName string
	var season, episode int64
	seriesErr := d.db.QueryRow("SELECT e.title,s.name,e.season,e.episode,e.magnet_link FROM episodes e JOIN series s ON s.id=e.series_id WHERE e.magnet_hash=?1", normalized).Scan(&seriesTitle, &seriesName, &season, &episode, &seriesMagnet)
	if seriesErr == nil {
		title := ""
		if seriesTitle.Valid {
			title = seriesTitle.String
		}
		magnet := ""
		if seriesMagnet.Valid {
			magnet = seriesMagnet.String
		}
		seriesPtr := seriesName
		seasonPtr := season
		episodePtr := episode
		return &models.TorrentMeta{Release: models.Release{
			TorrentURL:   nil,
			Title:        title,
			Magnet:       magnet,
			Source:       "restored-db",
			Quality:      models.Quality{},
			Kind:         "series",
			Series:       &seriesPtr,
			Season:       &seasonPtr,
			Episode:      &episodePtr,
			IsPack:       false,
			EpisodeRange: []int64{episode},
			Year:         nil,
			DiscoveredAt: time.Now().UTC(),
			SizeBytes:    0,
			Seeders:      -1,
			Peers:        -1,
		}}, nil
	}
	if !errors.Is(seriesErr, sql.ErrNoRows) {
		return nil, seriesErr
	}
	var movieTitle, movieMagnet sql.NullString
	var year sql.NullInt64
	movieErr := d.db.QueryRow("SELECT COALESCE(title,name),year,magnet_link FROM movies WHERE magnet_hash=?1", normalized).Scan(&movieTitle, &year, &movieMagnet)
	if movieErr == nil {
		title := movieTitle.String
		magnet := movieMagnet.String
		yearPtr := year.Int64
		return &models.TorrentMeta{Release: models.Release{
			TorrentURL:   nil,
			Title:        title,
			Magnet:       magnet,
			Source:       "restored-db",
			Quality:      models.Quality{},
			Kind:         "movie",
			Series:       nil,
			Season:       nil,
			Episode:      nil,
			IsPack:       false,
			EpisodeRange: []int64{},
			Year:         &yearPtr,
			DiscoveredAt: time.Now().UTC(),
			SizeBytes:    0,
			Seeders:      -1,
			Peers:        -1,
		}}, nil
	}
	if !errors.Is(movieErr, sql.ErrNoRows) {
		return nil, movieErr
	}
	return nil, nil
}

// ReconcilePackRelease persists the identity found in the actual torrent name
// when an indexer advertised a different season.
func (d *Database) ReconcilePackRelease(hash string, release *models.Release, cfg *Config) error {
	score := cfg.ReleaseScore(release)
	normalized := strings.ToLower(hash)
	old, err := d.TorrentMeta(normalized)
	if err != nil {
		return err
	}
	if old == nil {
		return nil
	}
	if old.Release.Kind != "series" || !old.Release.IsPack {
		return nil
	}
	oldMagnet := old.Release.Magnet
	metadata, err := json.Marshal(models.TorrentMeta{Release: *release})
	if err != nil {
		return err
	}
	now := nowSQLite()
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	updateTorrent := func() error {
		_, err := tx.Exec("UPDATE torrent_meta SET kind=?1,title=?2,series_name=?3,season=?4,episode=?5,year=?6,quality_score=?7,source=?8,metadata_json=?9,updated_at=?10 WHERE hash=?11", release.Kind, release.Title, release.Series, release.Season, release.Episode, release.Year, score, release.Source, string(metadata), now, normalized)
		return err
	}
	var newSeriesID *int64
	if release.Series != nil {
		var id int64
		err := tx.QueryRow("SELECT id FROM series WHERE name=?1", *release.Series).Scan(&id)
		if err == nil {
			newSeriesID = &id
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if newSeriesID == nil {
		if err := updateTorrent(); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		committed = true
		return nil
	}
	targets := make(map[int64]struct{})
	for _, episode := range release.EpisodeRange {
		if episode >= 0 {
			targets[episode] = struct{}{}
		}
	}
	type episodePair struct {
		ID      int64
		Episode int64
	}
	rows, err := tx.Query("SELECT id,episode FROM episodes WHERE (lower(COALESCE(magnet_hash,''))=?1 OR magnet_link=?2) AND downloaded_at IS NULL AND COALESCE(archive_path,'')=''", normalized, oldMagnet)
	if err != nil {
		return err
	}
	pairs := make([]episodePair, 0)
	for rows.Next() {
		var pair episodePair
		if err := rows.Scan(&pair.ID, &pair.Episode); err != nil {
			rows.Close()
			return err
		}
		pairs = append(pairs, pair)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, pair := range pairs {
		if _, ok := targets[pair.Episode]; !ok {
			if _, err := tx.Exec("DELETE FROM episodes WHERE id=?1", pair.ID); err != nil {
				return err
			}
			continue
		}
		var conflictID int64
		conflictErr := tx.QueryRow("SELECT id FROM episodes WHERE series_id=?1 AND season=?2 AND episode=?3 AND id<>?4", *newSeriesID, release.Season, pair.Episode, pair.ID).Scan(&conflictID)
		if conflictErr != nil && !errors.Is(conflictErr, sql.ErrNoRows) {
			return conflictErr
		}
		if conflictErr == nil {
			if _, err := tx.Exec("DELETE FROM episodes WHERE id=?1", pair.ID); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.Exec("UPDATE episodes SET series_id=?1,season=?2,title=?3,magnet_link=?4 WHERE id=?5", *newSeriesID, release.Season, release.Title, release.Magnet, pair.ID); err != nil {
			return err
		}
	}
	if err := updateTorrent(); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// ReconcileMissingTorrents marks orphan active torrents as `error` and removes
// their not-yet-downloaded placeholders.
func (d *Database) ReconcileMissingTorrents(liveHashes map[string]struct{}) (int, error) {
	rows, err := d.db.Query("SELECT hash FROM torrent_meta WHERE status NOT IN ('completed','error','removed')")
	if err != nil {
		return 0, err
	}
	missing := make([]string, 0)
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			rows.Close()
			return 0, err
		}
		if _, ok := liveHashes[strings.ToLower(hash)]; !ok {
			missing = append(missing, hash)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	reconciled := 0
	for _, hash := range missing {
		var release *models.Release
		if meta, err := d.TorrentMeta(hash); err == nil && meta != nil {
			release = &meta.Release
		}
		if _, err := d.db.Exec("UPDATE torrent_meta SET status='error', error=CASE WHEN COALESCE(error,'')='' THEN 'missing from session' ELSE error END, updated_at=?2 WHERE hash=?1", strings.ToLower(hash), nowSQLite()); err != nil {
			return reconciled, err
		}
		if release != nil {
			if err := d.clearPlaceholders(d.db, release.Magnet); err != nil {
				return reconciled, err
			}
		}
		reconciled++
	}
	// Download conclusi non più nella sessione: sono stati "puliti".
	completedRows, err := d.db.Query("SELECT hash FROM torrent_meta WHERE status='completed' AND removed_at IS NULL")
	if err != nil {
		return reconciled, err
	}
	cleared := make([]string, 0)
	for completedRows.Next() {
		var hash string
		if err := completedRows.Scan(&hash); err != nil {
			completedRows.Close()
			return reconciled, err
		}
		if _, ok := liveHashes[strings.ToLower(hash)]; !ok {
			cleared = append(cleared, hash)
		}
	}
	if err := completedRows.Err(); err != nil {
		completedRows.Close()
		return reconciled, err
	}
	completedRows.Close()
	for _, hash := range cleared {
		if err := d.MarkTorrentRemovedAt(hash); err != nil {
			logging.Warn("reconcile: cannot record removed_at for completed torrent", "hash", hash, "error", err)
		}
	}
	return reconciled, nil
}

// MarkTorrentRemoved marks a non-completed torrent as `removed` and clears its
// not-yet-downloaded placeholders.
func (d *Database) MarkTorrentRemoved(hash string) error {
	var release *models.Release
	if meta, err := d.TorrentMeta(hash); err == nil && meta != nil {
		release = &meta.Release
	}
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.Exec("UPDATE torrent_meta SET status='removed', updated_at=?2 WHERE hash=?1 AND status NOT IN ('completed','error','removed')", strings.ToLower(hash), nowSQLite()); err != nil {
		return err
	}
	if err := d.markTorrentRemovedAt(tx, hash); err != nil {
		return err
	}
	if release != nil {
		if err := d.clearPlaceholders(tx, release.Magnet); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// MarkTorrentRemovedAt records that a torrent has left the session (entering
// the download history).
func (d *Database) MarkTorrentRemovedAt(hash string) error {
	return d.markTorrentRemovedAt(d.db, hash)
}

// markTorrentRemovedAt runs the removed_at update on the given executor so it
// can participate in a caller's transaction.
func (d *Database) markTorrentRemovedAt(exec sqlExecer, hash string) error {
	_, err := exec.Exec("UPDATE torrent_meta SET removed_at=?2, updated_at=?2 WHERE hash=?1 AND removed_at IS NULL", strings.ToLower(hash), nowSQLite())
	return err
}

// TorrentTimes returns the `created_at` / `completed_at` of a torrent.
func (d *Database) TorrentTimes(hash string) (*TorrentTimes, error) {
	var createdAt string
	var completedAt sql.NullString
	err := d.db.QueryRow("SELECT created_at, completed_at FROM torrent_meta WHERE lower(hash)=lower(?1)", hash).Scan(&createdAt, &completedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	times := &TorrentTimes{CreatedAt: createdAt, CompletedAt: nullStringPtr(completedAt)}
	return times, nil
}

func storedTorrentFromRow(rows *sql.Rows) (StoredTorrent, error) {
	var item StoredTorrent
	var paused int64
	err := rows.Scan(
		&item.Hash, &item.Name, &item.Tag, &item.Source, &item.Progress, &paused,
		&item.TotalSize, &item.Downloaded, &item.Status, &item.UpdatedAt,
		&item.Kind, &item.SeriesName, &item.Season, &item.Episode,
		&item.Year, &item.QualityScore, &item.CompletedAt, &item.ProcessedPath, &item.Error, &item.Reason,
	)
	item.Paused = paused != 0
	return item, err
}

// StoredTorrents returns the torrents still downloading.
func (d *Database) StoredTorrents(limit int) ([]StoredTorrent, error) {
	return d.storedTorrentsQuery(limit, true)
}

// StoredTorrentsAll returns every named torrent row.
func (d *Database) StoredTorrentsAll(limit int) ([]StoredTorrent, error) {
	return d.storedTorrentsQuery(limit, false)
}

func (d *Database) storedTorrentsQuery(limit int, activeOnly bool) ([]StoredTorrent, error) {
	filter := ""
	if activeOnly {
		filter = "AND progress > 0 AND progress < 1"
	}
	query := fmt.Sprintf(`SELECT hash,COALESCE(name,''),COALESCE(tag,''),COALESCE(source,''),COALESCE(progress,0),COALESCE(paused,0),
                    COALESCE(total_size,0),COALESCE(downloaded,0),COALESCE(status,'queued'),COALESCE(updated_at,''),
                    COALESCE(kind,''),COALESCE(series_name,''),COALESCE(season,0),COALESCE(episode,0),
                    COALESCE(year,0),COALESCE(quality_score,0),COALESCE(completed_at,''),COALESCE(processed_path,''),COALESCE(error,''),COALESCE(reason,'')
             FROM torrent_meta WHERE TRIM(COALESCE(name,'')) != '' %s ORDER BY updated_at DESC LIMIT ?1`, filter)
	rows, err := d.db.Query(query, clampInt(limit, 1, 2000))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	torrents := make([]StoredTorrent, 0)
	for rows.Next() {
		item, err := storedTorrentFromRow(rows)
		if err != nil {
			return nil, err
		}
		torrents = append(torrents, item)
	}
	return torrents, rows.Err()
}

// CompletedTorrents returns the download-history page and total count.
func (d *Database) CompletedTorrents(offset, limit int, query string) ([]StoredTorrent, int64, error) {
	whereClause := fmt.Sprintf(`FROM torrent_meta
             WHERE removed_at IS NOT NULL
               AND (status IN ('completed','error') OR COALESCE(progress,0) >= 1)
               AND datetime(COALESCE(NULLIF(completed_at,''), NULLIF(removed_at,''), updated_at)) >= datetime('now', '-%d days')
               AND COALESCE(NULLIF(name,''),NULLIF(title,''),NULLIF(series_name,'')) <> ''`, downloadHistoryRetentionDays)
	terms := make([]string, 0, 8)
	for _, term := range strings.Fields(query) {
		if term != "" {
			terms = append(terms, term)
		}
		if len(terms) >= 8 {
			break
		}
	}
	for range terms {
		whereClause += " AND lower(COALESCE(name,'') || ' ' || COALESCE(title,'') || ' ' || COALESCE(series_name,'') || ' ' || COALESCE(tag,'') || ' ' || COALESCE(kind,'') || ' ' || COALESCE(status,'') || ' ' || COALESCE(processed_path,'')) LIKE '%' || lower(?) || '%'"
	}
	totalArgs := make([]any, len(terms))
	for index, term := range terms {
		totalArgs[index] = term
	}
	var total int64
	if err := d.db.QueryRow("SELECT COUNT(*) "+whereClause, totalArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}
	selectQuery := fmt.Sprintf(`SELECT hash,
                    COALESCE(NULLIF(name,''),NULLIF(title,''),NULLIF(series_name,''),hash),
                    COALESCE(tag,''),COALESCE(source,''),COALESCE(progress,0),COALESCE(paused,0),
                    COALESCE(total_size,0),COALESCE(downloaded,0),COALESCE(status,'queued'),COALESCE(updated_at,''),
                    COALESCE(kind,''),COALESCE(series_name,''),COALESCE(season,0),COALESCE(episode,0),
                    COALESCE(year,0),COALESCE(quality_score,0),COALESCE(completed_at,''),COALESCE(processed_path,''),COALESCE(error,''),COALESCE(reason,'')
              %s
              ORDER BY COALESCE(NULLIF(removed_at,''), NULLIF(completed_at,''), updated_at) DESC LIMIT ? OFFSET ?`, whereClause)
	values := make([]any, 0, len(terms)+2)
	for _, term := range terms {
		values = append(values, term)
	}
	values = append(values, clampInt(limit, 1, 2000), offset)
	rows, err := d.db.Query(selectQuery, values...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]StoredTorrent, 0)
	for rows.Next() {
		item, err := storedTorrentFromRow(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// MarkTorrentCompleted marks a torrent and its files as completed.
func (d *Database) MarkTorrentCompleted(hash, path string, sizeBytes int64) error {
	now := nowSQLite()
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.Exec("UPDATE episodes SET downloaded_at=?1,archive_path=?2,size_bytes=?3 WHERE magnet_hash=?4", now, path, sizeBytes, hash); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE movies SET downloaded_at=?1,size_bytes=?2 WHERE magnet_hash=?3", now, sizeBytes, hash); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE torrent_meta SET status='completed',completed_at=?1,processed_path=?2,error='',updated_at=?1 WHERE hash=?3", now, path, strings.ToLower(hash)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// MarkTorrentCompletedUnarchived records the first completion of a torrent
// that has no registered release metadata (manually added or foreign). It is
// completed but not archived, so `processed_path` is left untouched and the
// downloaded files are never treated as a disposable archived copy. The bool
// is false for duplicate completion events, allowing callers to notify once.
func (d *Database) MarkTorrentCompletedUnarchived(hash, name string) (bool, error) {
	now := nowSQLite()
	result, err := d.db.Exec(
		`INSERT INTO torrent_meta(hash,name,status,completed_at,created_at,updated_at)
		 VALUES (?1,?2,'completed',?3,?3,?3)
		 ON CONFLICT(hash) DO UPDATE SET
		   name=CASE WHEN excluded.name<>'' THEN excluded.name ELSE torrent_meta.name END,
		   status='completed',completed_at=COALESCE(torrent_meta.completed_at,excluded.completed_at),
		   error='',updated_at=excluded.updated_at
		 WHERE COALESCE(torrent_meta.status,'')<>'completed'`,
		strings.ToLower(hash), name, now,
	)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

// MarkReleaseCompleted marks a torrent as completed, updating every episode
// covered by the release.
func (d *Database) MarkReleaseCompleted(release *models.Release, path string, sizeBytes int64) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	hash, err := magnetHashOrError(release.Magnet)
	if err != nil {
		return err
	}
	now := nowSQLite()
	if release.Kind == "series" {
		if release.Series != nil && release.Season != nil {
			series := *release.Series
			season := *release.Season
			// Ensure the series row exists so a completion can always record the
			// episode, even after a full reset.
			if _, err := tx.Exec("INSERT OR IGNORE INTO series(name) VALUES (?1)", series); err != nil {
				return err
			}
			var seriesID int64
			if err := tx.QueryRow("SELECT id FROM series WHERE name=?1", series).Scan(&seriesID); err != nil {
				return err
			}
			{
				// Upsert so a completion always records the episode even after
				// the row was reset or deleted by the user. Existing title and
				// quality score are preserved.
				upsertEpisode := func(episode int64) error {
					_, err := tx.Exec("INSERT INTO episodes(series_id,season,episode,title,quality_score,downloaded_at,archive_path,size_bytes) VALUES (?1,?2,?3,'',0,?4,?5,?6) ON CONFLICT(series_id,season,episode) DO UPDATE SET downloaded_at=excluded.downloaded_at, archive_path=excluded.archive_path, size_bytes=excluded.size_bytes", seriesID, season, episode, now, path, sizeBytes)
					return err
				}
				if release.IsPack && containsInt64Value(release.EpisodeRange, 0) {
					if err := upsertEpisode(0); err != nil {
						return err
					}
				}
				var episodes []int64
				if release.IsPack && (len(release.EpisodeRange) == 0 || containsInt64Value(release.EpisodeRange, 0)) {
					var count sql.NullInt64
					countErr := tx.QueryRow("SELECT MAX(episode_count) FROM series_metadata WHERE series_name=?1 AND season=?2", series, season).Scan(&count)
					if countErr != nil && !errors.Is(countErr, sql.ErrNoRows) {
						return countErr
					}
					if count.Valid && count.Int64 > 0 {
						for episode := int64(1); episode <= count.Int64; episode++ {
							episodes = append(episodes, episode)
						}
					}
				} else if len(release.EpisodeRange) == 0 {
					episode := int64(0)
					if release.Episode != nil {
						episode = *release.Episode
					}
					episodes = []int64{episode}
				} else {
					for _, episode := range release.EpisodeRange {
						// E00 is a single special/recap when IsPack is false and
						// must be recorded like any other completed episode.
						if episode >= 0 {
							episodes = append(episodes, episode)
						}
					}
				}
				for _, episode := range episodes {
					if err := upsertEpisode(episode); err != nil {
						return err
					}
				}
			}
		}
	} else {
		if _, err := tx.Exec("UPDATE movies SET downloaded_at=?1,size_bytes=?2 WHERE magnet_hash=?3", now, sizeBytes, hash); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("UPDATE torrent_meta SET status='completed',completed_at=?1,processed_path=?2,error='',updated_at=?1 WHERE hash=?3", now, path, hash); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// MarkPackCompleted marks a season pack as completed for each archived episode.
func (d *Database) MarkPackCompleted(release *models.Release, episodes []PackEpisode, path string, sizeBytes int64) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	hash, err := magnetHashOrError(release.Magnet)
	if err != nil {
		return err
	}
	now := nowSQLite()
	if release.Series != nil && release.Season != nil {
		series := *release.Series
		season := *release.Season
		var seriesID int64
		seriesErr := tx.QueryRow("SELECT id FROM series WHERE name=?1", series).Scan(&seriesID)
		if seriesErr != nil && !errors.Is(seriesErr, sql.ErrNoRows) {
			return seriesErr
		}
		if seriesErr == nil {
			if containsInt64Value(release.EpisodeRange, 0) {
				if _, err := tx.Exec("UPDATE episodes SET downloaded_at=?1,archive_path=?2,size_bytes=?3 WHERE series_id=?4 AND season=?5 AND episode=0", now, path, sizeBytes, seriesID, season); err != nil {
					return err
				}
			}
			for _, episode := range episodes {
				if _, err := tx.Exec("UPDATE episodes SET downloaded_at=?1,archive_path=?2,size_bytes=?3,quality_score=CASE WHEN ?4>quality_score THEN ?4 ELSE quality_score END WHERE series_id=?5 AND season=?6 AND episode=?7", now, episode.Path, episode.SizeBytes, episode.Score, seriesID, season, episode.Episode); err != nil {
					return err
				}
			}
		}
	}
	if _, err := tx.Exec("UPDATE torrent_meta SET status='completed',completed_at=?1,processed_path=?2,error='',updated_at=?1 WHERE hash=?3", now, path, hash); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// clearPlaceholders deletes the not-yet-downloaded placeholders of a release.
func (d *Database) clearPlaceholders(exec sqlExecer, magnet string) error {
	digest, ok := utils.MagnetHash(magnet)
	if !ok {
		// No usable magnet hash: match only by the exact magnet link so an
		// empty digest cannot delete unrelated rows that legitimately have an
		// empty magnet_hash.
		_, err := exec.Exec("DELETE FROM episodes WHERE magnet_link=?1 AND downloaded_at IS NULL AND COALESCE(archive_path,'')=''", magnet)
		return err
	}
	_, err := exec.Exec("DELETE FROM episodes WHERE (lower(magnet_hash)=lower(?1) OR magnet_link=?2) AND downloaded_at IS NULL AND COALESCE(archive_path,'')=''", digest, magnet)
	return err
}

// MarkTorrentError marks a torrent as failed and clears its placeholders.
func (d *Database) MarkTorrentError(hash, failureError string) error {
	var release *models.Release
	if meta, err := d.TorrentMeta(hash); err == nil && meta != nil {
		release = &meta.Release
	}
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.Exec("UPDATE torrent_meta SET status='error',error=?,updated_at=? WHERE hash=?", failureError, nowSQLite(), strings.ToLower(hash)); err != nil {
		return err
	}
	if release != nil {
		if err := d.clearPlaceholders(tx, release.Magnet); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (d *Database) saveUpgradeBackup(exec sqlQueryExecer, newHash string, backup upgradeBackup) error {
	backups := []upgradeBackup{}
	var existing sql.NullString
	err := exec.QueryRow("SELECT payload_json FROM upgrade_backup WHERE new_hash=?1", strings.ToLower(newHash)).Scan(&existing)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && existing.Valid {
		backups, err = decodeUpgradeBackups(existing.String)
		if err != nil {
			return err
		}
	}
	backups = append(backups, backup)
	// Preserve the old single-object representation for ordinary upgrades;
	// packs use an array once more than one episode needs restoring.
	var payloadBytes []byte
	if len(backups) == 1 && strings.TrimSpace(existing.String) == "" {
		payloadBytes, err = json.Marshal(backups[0])
	} else {
		payloadBytes, err = json.Marshal(backups)
	}
	if err != nil {
		return err
	}
	_, err = exec.Exec("INSERT OR REPLACE INTO upgrade_backup(new_hash,payload_json,created_at) VALUES (?1,?2,?3)", strings.ToLower(newHash), string(payloadBytes), nowSQLite())
	return err
}

// decodeUpgradeBackups accepts both the original single-backup JSON object and
// the multi-row form used by season-pack upgrades.
func decodeUpgradeBackups(raw string) ([]upgradeBackup, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var backups []upgradeBackup
		if err := json.Unmarshal([]byte(trimmed), &backups); err != nil {
			return nil, err
		}
		return validUpgradeBackups(backups), nil
	}
	var backup upgradeBackup
	if err := json.Unmarshal([]byte(trimmed), &backup); err != nil {
		return nil, err
	}
	return validUpgradeBackups([]upgradeBackup{backup}), nil
}

func validUpgradeBackups(backups []upgradeBackup) []upgradeBackup {
	valid := backups[:0]
	for _, backup := range backups {
		if backup.Kind != "" && backup.RowID > 0 {
			valid = append(valid, backup)
		}
	}
	return valid
}

func restoreUpgradeBackupTx(tx *sql.Tx, backup upgradeBackup) error {
	if backup.Kind == "series" {
		_, err := tx.Exec("UPDATE episodes SET quality_score=?1,magnet_hash=?2,magnet_link=?3,downloaded_at=?4,archive_path=?5,size_bytes=?6,title=?7 WHERE id=?8", backup.QualityScore, backup.MagnetHash, backup.MagnetLink, backup.DownloadedAt, backup.ArchivePath, backup.SizeBytes, backup.Title, backup.RowID)
		return err
	}
	_, err := tx.Exec("UPDATE movies SET name=?1,year=?2,title=?3,quality_score=?4,magnet_hash=?5,magnet_link=?6,downloaded_at=?7,size_bytes=?8,removed_at=NULL WHERE id=?9", backup.Name, backup.Year, backup.Title, backup.QualityScore, backup.MagnetHash, backup.MagnetLink, backup.DownloadedAt, backup.SizeBytes, backup.RowID)
	return err
}

// RestoreUpgrade restores a previous release from its upgrade backup.
func (d *Database) RestoreUpgrade(hash string) (bool, error) {
	normalized := strings.ToLower(hash)
	var payload sql.NullString
	err := d.db.QueryRow("SELECT payload_json FROM upgrade_backup WHERE new_hash=?1", normalized).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !payload.Valid {
		return false, nil
	}
	backups, err := decodeUpgradeBackups(payload.String)
	if err != nil {
		return false, err
	}
	if len(backups) == 0 {
		return false, nil
	}
	tx, err := d.db.Begin()
	if err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	// A season pack can have inserted episodes that were not upgrades and thus
	// have no backup row of their own. Recover the release magnet from the
	// registered metadata so those placeholders are removed together with the
	// restored upgrades.
	newMagnet := ""
	var metadataJSON sql.NullString
	if err := tx.QueryRow("SELECT metadata_json FROM torrent_meta WHERE lower(hash)=?1", normalized).Scan(&metadataJSON); err == nil && metadataJSON.Valid {
		var metadata models.TorrentMeta
		if json.Unmarshal([]byte(metadataJSON.String), &metadata) == nil {
			newMagnet = metadata.Release.Magnet
		}
	}
	for _, backup := range backups {
		if err := restoreUpgradeBackupTx(tx, backup); err != nil {
			return false, err
		}
	}
	if _, err := tx.Exec("DELETE FROM upgrade_backup WHERE new_hash=?1", normalized); err != nil {
		return false, err
	}
	if _, err := tx.Exec("DELETE FROM episodes WHERE lower(COALESCE(magnet_hash,''))=?1", normalized); err != nil {
		return false, err
	}
	if newMagnet != "" {
		if _, err := tx.Exec("DELETE FROM episodes WHERE magnet_link=?1", newMagnet); err != nil {
			return false, err
		}
	}
	if _, err := tx.Exec("DELETE FROM movies WHERE lower(COALESCE(magnet_hash,''))=?1", normalized); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	committed = true
	return true, nil
}

// UpgradeReplacedInfo returns the name of the previous release replaced by this
// upgrade, if an upgrade backup was recorded. It returns false when there is no
// usable backup entry.
func (d *Database) UpgradeReplacedInfo(hash string) (string, bool) {
	normalized := strings.ToLower(hash)
	var payload sql.NullString
	err := d.db.QueryRow("SELECT payload_json FROM upgrade_backup WHERE new_hash=?1", normalized).Scan(&payload)
	if err != nil || !payload.Valid || payload.String == "" {
		return "", false
	}
	backups, err := decodeUpgradeBackups(payload.String)
	if err != nil || len(backups) == 0 {
		return "", false
	}
	first := backups[0]
	name := first.Title
	if first.ArchivePath != nil && *first.ArchivePath != "" {
		name = filepath.Base(*first.ArchivePath)
	}
	if strings.TrimSpace(name) == "" {
		return "", false
	}
	return name, true
}

// SaveCycle stores one cycle history entry.
func (d *Database) SaveCycle(value *models.CycleStats) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = d.db.Exec("INSERT INTO cycle_history(at,payload_json) VALUES (?1,?2)", nowSQLite(), string(payload))
	return err
}

// RecentCycles returns the most recent cycle history entries with an `at`
// field merged into the payload.
func (d *Database) RecentCycles(limit int64) ([]any, error) {
	rows, err := d.db.Query("SELECT at,payload_json FROM cycle_history ORDER BY id DESC LIMIT ?1", clampInt64(limit, 1, 1000))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]any, 0)
	for rows.Next() {
		var at, payload string
		if err := rows.Scan(&at, &payload); err != nil {
			return nil, err
		}
		var parsed any
		if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
			items = append(items, map[string]any{"raw": payload})
			continue
		}
		if object, ok := parsed.(map[string]any); ok {
			object["at"] = at
			items = append(items, object)
		} else {
			items = append(items, map[string]any{"at": at, "payload": payload})
		}
	}
	return items, rows.Err()
}

// LastCycleAt returns the start time of the most recent persisted cycle, so a
// restart can resume the schedule instead of running a full scrape immediately.
func (d *Database) LastCycleAt() (time.Time, bool) {
	var at, payload string
	if err := d.db.QueryRow("SELECT at,payload_json FROM cycle_history ORDER BY id DESC LIMIT 1").Scan(&at, &payload); err != nil {
		return time.Time{}, false
	}
	// Prefer the cycle's own start time from the payload; fall back to the row
	// timestamp for entries written before that field existed.
	var object map[string]any
	if err := json.Unmarshal([]byte(payload), &object); err == nil {
		if value, ok := object["last_started_at"].(string); ok {
			if parsed, ok := utils.ParseTimestamp(value); ok {
				return parsed, true
			}
		}
	}
	parsed, ok := utils.ParseTimestamp(at)
	if !ok {
		return time.Time{}, false
	}
	return parsed, true
}

// Rescore recomputes every stored quality score with the configured weights.
// SyncScoresWithSettings recomputes the stored quality scores with the current
// scoring weights, but only when those weights changed since the last run. A
// weight change (for example a higher Dolby Vision bonus) otherwise leaves every
// archived release with a stale score, so the next cycle sees an artificial
// improvement and re-downloads content that already has the right quality.
func (d *Database) SyncScoresWithSettings(cfg *Config) (int, error) {
	if d == nil || cfg == nil {
		return 0, nil
	}
	fingerprint := cfg.ScoreWeightsFingerprint()
	if strings.TrimSpace(cfg.Settings["_score_weights_fingerprint"]) == fingerprint {
		return 0, nil
	}
	updated, err := d.Rescore(cfg)
	if err != nil {
		return updated, err
	}
	if err := saveConfigSetting(cfg.DataDir, "_score_weights_fingerprint", fingerprint); err != nil {
		return updated, err
	}
	// Keep the in-memory config aligned so a second call in the same process is
	// a no-op instead of rescoring the whole library again.
	cfg.Settings["_score_weights_fingerprint"] = fingerprint
	return updated, nil
}

func (d *Database) Rescore(cfg *Config) (int, error) {
	type metadataRow struct {
		Hash string
		JSON string
	}
	rows, err := d.db.Query("SELECT hash,metadata_json FROM torrent_meta WHERE metadata_json!=''")
	if err != nil {
		return 0, err
	}
	metadataRows := make([]metadataRow, 0)
	for rows.Next() {
		var row metadataRow
		if err := rows.Scan(&row.Hash, &row.JSON); err != nil {
			rows.Close()
			return 0, err
		}
		metadataRows = append(metadataRows, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	// Pre-load the stored media info once per table instead of issuing two
	// lookups per torrent row (an N+1 on a maintenance path that runs over the
	// whole library). magnet_hash is UNIQUE in both tables, so a map keyed by
	// the lowercased hash is exact.
	episodeMediaMap := map[string]string{}
	episodeMediaRows, err := d.db.Query("SELECT lower(magnet_hash), COALESCE(media_info_json,'') FROM episodes WHERE COALESCE(media_info_json,'')<>'' AND magnet_hash IS NOT NULL")
	if err != nil {
		return 0, err
	}
	for episodeMediaRows.Next() {
		var hash, media string
		if err := episodeMediaRows.Scan(&hash, &media); err != nil {
			episodeMediaRows.Close()
			return 0, err
		}
		episodeMediaMap[hash] = media
	}
	if err := episodeMediaRows.Err(); err != nil {
		episodeMediaRows.Close()
		return 0, err
	}
	episodeMediaRows.Close()
	movieMediaMap := map[string]string{}
	movieMediaRows, err := d.db.Query("SELECT lower(magnet_hash), COALESCE(media_info_json,'') FROM movies WHERE COALESCE(media_info_json,'')<>'' AND magnet_hash IS NOT NULL")
	if err != nil {
		return 0, err
	}
	for movieMediaRows.Next() {
		var hash, media string
		if err := movieMediaRows.Scan(&hash, &media); err != nil {
			movieMediaRows.Close()
			return 0, err
		}
		movieMediaMap[hash] = media
	}
	if err := movieMediaRows.Err(); err != nil {
		movieMediaRows.Close()
		return 0, err
	}
	movieMediaRows.Close()

	changed := 0
	for _, row := range metadataRows {
		var meta models.TorrentMeta
		if err := json.Unmarshal([]byte(row.JSON), &meta); err != nil {
			continue
		}
		mediaInfo := episodeMediaMap[strings.ToLower(row.Hash)]
		if mediaInfo == "" {
			mediaInfo = movieMediaMap[strings.ToLower(row.Hash)]
		}
		enrichQualityWithMediaInfo(mediaInfo, &meta.Release.Quality)
		score := cfg.ReleaseScore(&meta.Release)
		if result, err := d.db.Exec("UPDATE episodes SET quality_score=?1 WHERE lower(magnet_hash)=lower(?2)", score, row.Hash); err != nil {
			return changed, err
		} else if affected, _ := result.RowsAffected(); affected > 0 {
			changed += int(affected)
		}
		if result, err := d.db.Exec("UPDATE movies SET quality_score=?1 WHERE lower(magnet_hash)=lower(?2)", score, row.Hash); err != nil {
			return changed, err
		} else if affected, _ := result.RowsAffected(); affected > 0 {
			changed += int(affected)
		}
		if result, err := d.db.Exec("UPDATE torrent_meta SET quality_score=?1,updated_at=?2 WHERE lower(hash)=lower(?3)", score, nowSQLite(), row.Hash); err != nil {
			return changed, err
		} else if affected, _ := result.RowsAffected(); affected > 0 {
			changed += int(affected)
		}
	}
	// Episodi senza una release tracciata: normalizza dal titolo o dal file.
	episodeRows, err := d.db.Query("SELECT e.id, COALESCE(e.title,''), COALESCE(e.archive_path,''), COALESCE(e.media_info_json,'') FROM episodes e WHERE e.magnet_hash IS NULL OR NOT EXISTS(SELECT 1 FROM torrent_meta t WHERE lower(t.hash)=lower(e.magnet_hash) AND COALESCE(t.metadata_json,'') != '')")
	if err != nil {
		return changed, err
	}
	type episodeScoreRow struct {
		ID        int64
		Title     string
		Path      string
		MediaInfo string
	}
	episodeScoreRows := make([]episodeScoreRow, 0)
	for episodeRows.Next() {
		var row episodeScoreRow
		if err := episodeRows.Scan(&row.ID, &row.Title, &row.Path, &row.MediaInfo); err != nil {
			episodeRows.Close()
			return changed, err
		}
		episodeScoreRows = append(episodeScoreRows, row)
	}
	if err := episodeRows.Err(); err != nil {
		episodeRows.Close()
		return changed, err
	}
	episodeRows.Close()
	for _, row := range episodeScoreRows {
		quality, ok := meaningfulQuality(row.Title)
		if !ok {
			name := ""
			if row.Path != "" {
				name = filepath.Base(row.Path)
			}
			quality, ok = meaningfulQuality(name)
		}
		if !ok {
			continue
		}
		enrichQualityWithMediaInfo(row.MediaInfo, &quality)
		release := archivedRelease(row.Title, row.Path, quality, "series", nil)
		if result, err := d.db.Exec("UPDATE episodes SET quality_score=?1 WHERE id=?2", cfg.ReleaseScore(&release), row.ID); err != nil {
			return changed, err
		} else if affected, _ := result.RowsAffected(); affected > 0 {
			changed += int(affected)
		}
	}
	// Film senza una release tracciata.
	movieRows, err := d.db.Query("SELECT m.id, COALESCE(NULLIF(m.title,''), m.name, ''), m.year, COALESCE(m.media_info_json,'') FROM movies m WHERE m.magnet_hash IS NULL OR NOT EXISTS(SELECT 1 FROM torrent_meta t WHERE lower(t.hash)=lower(m.magnet_hash) AND COALESCE(t.metadata_json,'') != '')")
	if err != nil {
		return changed, err
	}
	type movieScoreRow struct {
		ID        int64
		Title     string
		Year      sql.NullInt64
		MediaInfo string
	}
	movieScoreRows := make([]movieScoreRow, 0)
	for movieRows.Next() {
		var row movieScoreRow
		if err := movieRows.Scan(&row.ID, &row.Title, &row.Year, &row.MediaInfo); err != nil {
			movieRows.Close()
			return changed, err
		}
		movieScoreRows = append(movieScoreRows, row)
	}
	if err := movieRows.Err(); err != nil {
		movieRows.Close()
		return changed, err
	}
	movieRows.Close()
	for _, row := range movieScoreRows {
		quality, ok := meaningfulQuality(row.Title)
		if !ok {
			continue
		}
		enrichQualityWithMediaInfo(row.MediaInfo, &quality)
		release := archivedRelease(row.Title, "", quality, "movie", nullInt64Ptr(row.Year))
		if result, err := d.db.Exec("UPDATE movies SET quality_score=?1 WHERE id=?2", cfg.ReleaseScore(&release), row.ID); err != nil {
			return changed, err
		} else if affected, _ := result.RowsAffected(); affected > 0 {
			changed += int(affected)
		}
	}
	return changed, nil
}

// Cleanup removes old cycle history and stale errored torrents.
func (d *Database) Cleanup(retainCycles, errorAgeDays int64) (MaintenanceReport, error) {
	var report MaintenanceReport
	result, err := d.db.Exec("DELETE FROM cycle_history WHERE id NOT IN (SELECT id FROM cycle_history ORDER BY id DESC LIMIT ?1)", clampInt64(retainCycles, 1, 10000))
	if err != nil {
		return report, err
	}
	if affected, err := result.RowsAffected(); err == nil {
		report.OldCyclesRemoved = int(affected)
	}
	result, err = d.db.Exec("DELETE FROM torrent_meta WHERE status='error' AND updated_at < datetime('now', ?1)", fmt.Sprintf("-%d days", maxInt64(errorAgeDays, 1)))
	if err != nil {
		return report, err
	}
	if affected, err := result.RowsAffected(); err == nil {
		report.StaleTorrentsRemoved = int(affected)
	}
	return report, nil
}

// Housekeeping trims the bounded tables and compacts the database.
func (d *Database) Housekeeping(params HousekeepingParams) (HousekeepingReport, error) {
	var report HousekeepingReport
	cleanup, err := d.Cleanup(params.RetainCycles, params.ErrorAgeDays)
	if err != nil {
		return report, err
	}
	now := time.Now().UTC()
	cutoff := func(days int64) string {
		return sqliteTimestamp(now.AddDate(0, 0, -retentionDays(days, 0)))
	}
	report.OldCyclesRemoved = cleanup.OldCyclesRemoved
	report.StaleTorrentsRemoved = cleanup.StaleTorrentsRemoved
	if params.SeenDays > 0 {
		removed, err := d.PruneSeenOlderThan(params.SeenDays)
		if err != nil {
			return report, err
		}
		report.SeenRemoved = removed
	}
	if params.GapLogDays > 0 {
		result, err := d.db.Exec("DELETE FROM gap_search_log WHERE last_searched_at < ?1", cutoff(params.GapLogDays))
		if err != nil {
			return report, err
		}
		if affected, err := result.RowsAffected(); err == nil {
			report.GapLogsRemoved = int(affected)
		}
	}
	if params.UpgradeBackupDays > 0 {
		result, err := d.db.Exec("DELETE FROM upgrade_backup WHERE created_at < ?1", cutoff(params.UpgradeBackupDays))
		if err != nil {
			return report, err
		}
		if affected, err := result.RowsAffected(); err == nil {
			report.UpgradeBackupsRemoved = int(affected)
		}
	}
	// Removed-torrent history is only trimmed when the user opts in.
	if params.HistoryDays > 0 {
		result, err := d.db.Exec("DELETE FROM torrent_meta WHERE removed_at IS NOT NULL AND removed_at < ?1", cutoff(params.HistoryDays))
		if err != nil {
			return report, err
		}
		if affected, err := result.RowsAffected(); err == nil {
			report.OldHistoryRemoved = int(affected)
		}
	}
	// Expired provider backoff rows older than a week are dead state.
	result, err := d.db.Exec("DELETE FROM provider_status WHERE disabled_till IS NOT NULL AND disabled_till < ?1", cutoff(7))
	if err != nil {
		return report, err
	}
	if affected, err := result.RowsAffected(); err == nil {
		report.StaleProvidersRemoved = int(affected)
	}
	return report, nil
}

// CountKeyword counts the rows matching one keyword.
func (d *Database) CountKeyword(keyword string) (int64, error) {
	return d.CountKeywords([]string{keyword})
}

// CountKeywords counts the rows matching every keyword.
func (d *Database) CountKeywords(keywords []string) (int64, error) {
	terms := normalizedKeywordTerms(keywords)
	if len(terms) == 0 {
		return 0, nil
	}
	torrentClause := keywordClause([]string{"name", "title", "series_name"}, len(terms))
	movieClause := keywordClause([]string{"COALESCE(title,name)"}, len(terms))
	episodeClause := keywordClause([]string{"s.name"}, len(terms))
	seriesClause := keywordClause([]string{"name"}, len(terms))
	seenClause := keywordClause([]string{"COALESCE(name,title)"}, len(terms))
	bindings := keywordBindings(terms, []string{"name", "title", "series_name"})
	bindings = append(bindings, keywordBindings(terms, []string{"COALESCE(title,name)"})...)
	bindings = append(bindings, keywordBindings(terms, []string{"s.name"})...)
	bindings = append(bindings, keywordBindings(terms, []string{"name"})...)
	bindings = append(bindings, keywordBindings(terms, []string{"COALESCE(name,title)"})...)
	bindings = append(bindings, keywordBindings(terms, []string{"COALESCE(name,title)"})...)
	query := fmt.Sprintf("SELECT (SELECT COUNT(*) FROM torrent_meta WHERE %s) + (SELECT COUNT(*) FROM movies WHERE %s) + (SELECT COUNT(*) FROM episodes e JOIN series s ON s.id=e.series_id WHERE %s) + (SELECT COUNT(*) FROM series WHERE %s) + (SELECT COUNT(*) FROM movie_feed_seen WHERE %s) + (SELECT COUNT(*) FROM series_feed_seen WHERE %s)", torrentClause, movieClause, episodeClause, seriesClause, seenClause, seenClause)
	var count int64
	if err := d.db.QueryRow(query, bindings...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// PruneKeyword removes the rows matching one keyword.
func (d *Database) PruneKeyword(keyword string) (int, error) {
	return d.PruneKeywords([]string{keyword})
}

// PruneKeywords removes the rows matching every keyword.
func (d *Database) PruneKeywords(keywords []string) (int, error) {
	terms := normalizedKeywordTerms(keywords)
	if len(terms) == 0 {
		return 0, nil
	}
	removed := 0
	torrentClause := keywordClause([]string{"name", "title", "series_name"}, len(terms))
	torrentBindings := keywordBindings(terms, []string{"name", "title", "series_name"})
	if result, err := d.db.Exec(fmt.Sprintf("DELETE FROM torrent_meta WHERE %s", torrentClause), torrentBindings...); err != nil {
		return removed, err
	} else if affected, _ := result.RowsAffected(); affected > 0 {
		removed += int(affected)
	}
	movieClause := keywordClause([]string{"COALESCE(title,name)"}, len(terms))
	movieBindings := keywordBindings(terms, []string{"COALESCE(title,name)"})
	if result, err := d.db.Exec(fmt.Sprintf("DELETE FROM movies WHERE %s", movieClause), movieBindings...); err != nil {
		return removed, err
	} else if affected, _ := result.RowsAffected(); affected > 0 {
		removed += int(affected)
	}
	episodeClause := keywordClause([]string{"name"}, len(terms))
	episodeBindings := keywordBindings(terms, []string{"name"})
	if result, err := d.db.Exec(fmt.Sprintf("DELETE FROM episodes WHERE series_id IN (SELECT id FROM series WHERE %s)", episodeClause), episodeBindings...); err != nil {
		return removed, err
	} else if affected, _ := result.RowsAffected(); affected > 0 {
		removed += int(affected)
	}
	seriesClause := keywordClause([]string{"name"}, len(terms))
	seriesBindings := keywordBindings(terms, []string{"name"})
	if result, err := d.db.Exec(fmt.Sprintf("DELETE FROM series WHERE %s", seriesClause), seriesBindings...); err != nil {
		return removed, err
	} else if affected, _ := result.RowsAffected(); affected > 0 {
		removed += int(affected)
	}
	seenClause := keywordClause([]string{"COALESCE(name,title)"}, len(terms))
	seenBindings := keywordBindings(terms, []string{"COALESCE(name,title)"})
	if result, err := d.db.Exec(fmt.Sprintf("DELETE FROM movie_feed_seen WHERE %s", seenClause), seenBindings...); err != nil {
		return removed, err
	} else if affected, _ := result.RowsAffected(); affected > 0 {
		removed += int(affected)
	}
	if result, err := d.db.Exec(fmt.Sprintf("DELETE FROM series_feed_seen WHERE %s", seenClause), seenBindings...); err != nil {
		return removed, err
	} else if affected, _ := result.RowsAffected(); affected > 0 {
		removed += int(affected)
	}
	return removed, nil
}

// PurgeSeries removes every trace of a series that is no longer monitored.
func (d *Database) PurgeSeries(name string) (int, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	removed := 0
	rows, err := tx.Query("SELECT id FROM series WHERE lower(name)=lower(?1)", name)
	if err != nil {
		return removed, err
	}
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return removed, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return removed, err
	}
	rows.Close()
	for _, id := range ids {
		if result, err := tx.Exec("DELETE FROM episodes WHERE series_id=?1", id); err != nil {
			return removed, err
		} else if affected, _ := result.RowsAffected(); affected > 0 {
			removed += int(affected)
		}
		if result, err := tx.Exec("DELETE FROM pending_downloads WHERE series_id=?1", id); err != nil {
			return removed, err
		} else if affected, _ := result.RowsAffected(); affected > 0 {
			removed += int(affected)
		}
		if result, err := tx.Exec("DELETE FROM series WHERE id=?1", id); err != nil {
			return removed, err
		} else if affected, _ := result.RowsAffected(); affected > 0 {
			removed += int(affected)
		}
	}
	for _, table := range []string{"series_metadata", "episode_metadata", "ignored_episodes", "gap_search_log", "series_status"} {
		result, err := tx.Exec(fmt.Sprintf("DELETE FROM %s WHERE lower(series_name)=lower(?1)", table), name)
		if err != nil {
			return removed, err
		}
		if affected, _ := result.RowsAffected(); affected > 0 {
			removed += int(affected)
		}
	}
	if err := tx.Commit(); err != nil {
		return removed, err
	}
	committed = true
	return removed, nil
}

// PurgeMovie removes every trace of a movie that is no longer monitored.
func (d *Database) PurgeMovie(name string) (int, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	removed := 0
	if result, err := tx.Exec("DELETE FROM pending_movies WHERE lower(name)=lower(?1)", name); err != nil {
		return removed, err
	} else if affected, _ := result.RowsAffected(); affected > 0 {
		removed += int(affected)
	}
	if result, err := tx.Exec("DELETE FROM movies WHERE lower(COALESCE(name,''))=lower(?1)", name); err != nil {
		return removed, err
	} else if affected, _ := result.RowsAffected(); affected > 0 {
		removed += int(affected)
	}
	if err := tx.Commit(); err != nil {
		return removed, err
	}
	committed = true
	return removed, nil
}

// SearchKeywords previews the rows matching the given keywords without removing
// them.
func (d *Database) SearchKeywords(keywords []string, limit int) ([]any, error) {
	terms := normalizedKeywordTerms(keywords)
	if len(terms) == 0 {
		return []any{}, nil
	}
	limit = clampInt(limit, 1, 1000)
	queries := []struct {
		source  string
		sql     string
		columns []string
	}{
		{"Torrent", "SELECT COALESCE(NULLIF(name,''),NULLIF(title,''),series_name,hash), COALESCE(status,'') FROM torrent_meta WHERE ", []string{"name", "title", "series_name"}},
		{"Film", "SELECT COALESCE(NULLIF(title,''),name,''), CAST(COALESCE(year,0) AS TEXT) FROM movies WHERE ", []string{"COALESCE(title,name)"}},
		{"Episodio", "SELECT s.name || ' S' || printf('%02d',e.season) || 'E' || printf('%02d',e.episode), COALESCE(e.title,'') FROM episodes e JOIN series s ON s.id=e.series_id WHERE ", []string{"s.name"}},
		{"Serie", "SELECT name, COALESCE(quality,'') FROM series WHERE ", []string{"name"}},
		{"Film (feed)", "SELECT COALESCE(NULLIF(name,''),title), 'visto nei feed' FROM movie_feed_seen WHERE ", []string{"COALESCE(name,title)"}},
		{"Serie (feed)", "SELECT COALESCE(NULLIF(name,''),title), 'visto nei feed' FROM series_feed_seen WHERE ", []string{"COALESCE(name,title)"}},
	}
	items := make([]any, 0)
	for _, entry := range queries {
		clause := keywordClause(entry.columns, len(terms))
		bindings := keywordBindings(terms, entry.columns)
		statement := fmt.Sprintf("%s%s LIMIT %d", entry.sql, clause, limit)
		rows, err := d.db.Query(statement, bindings...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var title, detail string
			if err := rows.Scan(&title, &detail); err != nil {
				rows.Close()
				return nil, err
			}
			items = append(items, map[string]any{
				"source": entry.source,
				"title":  title,
				"detail": detail,
			})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return items, nil
}

// SeenCounts returns the number of distinct "seen in feed" groups
// (movies, series).
func (d *Database) SeenCounts() (int64, int64, error) {
	var movies int64
	if err := d.db.QueryRow("SELECT COUNT(DISTINCT group_key) FROM movie_feed_seen WHERE group_key IS NOT NULL AND group_key <> ''").Scan(&movies); err != nil {
		return 0, 0, err
	}
	var series int64
	if err := d.db.QueryRow("SELECT COUNT(DISTINCT group_key) FROM series_feed_seen WHERE group_key IS NOT NULL AND group_key <> ''").Scan(&series); err != nil {
		return 0, 0, err
	}
	return movies, series, nil
}

// PrunePreview counts what `cleanup` + `prune_seen_older_than` would remove,
// without deleting anything.
func (d *Database) PrunePreview(retainCycles, errorAgeDays, seenDays int64) (PrunePreview, error) {
	var preview PrunePreview
	retain := clampInt64(retainCycles, 1, 10000)
	var oldCycles int64
	if err := d.db.QueryRow("SELECT COUNT(*) FROM cycle_history WHERE id NOT IN (SELECT id FROM cycle_history ORDER BY id DESC LIMIT ?1)", retain).Scan(&oldCycles); err != nil {
		return preview, err
	}
	var staleTorrents int64
	if err := d.db.QueryRow("SELECT COUNT(*) FROM torrent_meta WHERE status='error' AND updated_at < datetime('now', ?1)", fmt.Sprintf("-%d days", maxInt64(errorAgeDays, 1))).Scan(&staleTorrents); err != nil {
		return preview, err
	}
	seenToRemove := 0
	if seenDays > 0 {
		cutoff := sqliteTimestamp(time.Now().UTC().AddDate(0, 0, -int(maxInt64(seenDays, 1))))
		var count int64
		if err := d.db.QueryRow("SELECT (SELECT COUNT(*) FROM movie_feed_seen WHERE found_at < ?1) + (SELECT COUNT(*) FROM series_feed_seen WHERE found_at < ?1)", cutoff).Scan(&count); err != nil {
			return preview, err
		}
		if count > 0 {
			seenToRemove = int(count)
		}
	}
	preview.OldCyclesToRemove = int(maxInt64(oldCycles, 0))
	preview.StaleTorrentsToRemove = int(maxInt64(staleTorrents, 0))
	preview.SeenToRemove = seenToRemove
	return preview, nil
}

// PruneSeenByIDs deletes specific "seen in feed" rows by id.
func (d *Database) PruneSeenByIDs(movieIDs, seriesIDs []int64) (int, int, error) {
	movies := 0
	for index, id := range movieIDs {
		if index >= 1000 {
			break
		}
		if result, err := d.db.Exec("DELETE FROM movie_feed_seen WHERE id=?1", id); err != nil {
			return movies, 0, err
		} else if affected, _ := result.RowsAffected(); affected > 0 {
			movies += int(affected)
		}
	}
	series := 0
	for index, id := range seriesIDs {
		if index >= 1000 {
			break
		}
		if result, err := d.db.Exec("DELETE FROM series_feed_seen WHERE id=?1", id); err != nil {
			return movies, series, err
		} else if affected, _ := result.RowsAffected(); affected > 0 {
			series += int(affected)
		}
	}
	return movies, series, nil
}

// maxRetentionDays caps a configured retention window (about a century) so the
// int conversion handed to time.AddDate can never wrap.
const maxRetentionDays = 36500

// retentionDays converts a retention window in days to int, raised to minimum
// and capped at maxRetentionDays.
func retentionDays(days, minimum int64) int {
	days = maxInt64(days, minimum)
	if days > maxRetentionDays {
		return maxRetentionDays
	}
	return int(days)
}

// PruneSeenOlderThan deletes the "seen" releases older than `days` days.
func (d *Database) PruneSeenOlderThan(days int64) (int, error) {
	if days <= 0 {
		return 0, nil
	}
	cutoff := sqliteTimestamp(time.Now().UTC().AddDate(0, 0, -retentionDays(days, 1)))
	removed := 0
	if result, err := d.db.Exec("DELETE FROM movie_feed_seen WHERE found_at < ?1", cutoff); err != nil {
		return removed, err
	} else if affected, _ := result.RowsAffected(); affected > 0 {
		removed += int(affected)
	}
	if result, err := d.db.Exec("DELETE FROM series_feed_seen WHERE found_at < ?1", cutoff); err != nil {
		return removed, err
	} else if affected, _ := result.RowsAffected(); affected > 0 {
		removed += int(affected)
	}
	return removed, nil
}

// ArchivedEpisodeFiles returns `(series name, archive path)` for downloaded
// episodes with a stored archive path.
func (d *Database) ArchivedEpisodeFiles() ([][2]string, error) {
	rows, err := d.db.Query("SELECT COALESCE(s.name,''), COALESCE(e.archive_path,'') FROM episodes e JOIN series s ON s.id=e.series_id WHERE COALESCE(e.archive_path,'')<>'' AND e.downloaded_at IS NOT NULL")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := make([][2]string, 0)
	for rows.Next() {
		var name, path string
		if err := rows.Scan(&name, &path); err != nil {
			return nil, err
		}
		files = append(files, [2]string{name, path})
	}
	return files, rows.Err()
}

// PendingCount returns the number of pending downloads.
func (d *Database) PendingCount() (int64, error) {
	var count int64
	err := d.db.QueryRow("SELECT COUNT(*) FROM pending_downloads").Scan(&count)
	return count, err
}

// Optimize runs SQLite maintenance (`vacuum` or `analyze`).
func (d *Database) Optimize(action string) error {
	return OptimizeConnection(d.db, action)
}

// DBSizeBytes returns the estimated database size in bytes.
func (d *Database) DBSizeBytes() int64 {
	return ConnectionSizeBytes(d.db)
}

// DBTotalRows returns the total number of rows in the main tables.
func (d *Database) DBTotalRows() int64 {
	var total int64
	for _, table := range []string{"series", "episodes", "movies", "pending_downloads", "cycle_history", "torrent_meta", "gap_search_log", "ignored_episodes", "blocklist"} {
		var count int64
		_ = d.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
		total += count
	}
	return total
}

// SeriesSummaries returns the per-series aggregate.
func (d *Database) SeriesSummaries() ([]SeriesSummary, error) {
	rows, err := d.db.Query("SELECT s.name, COUNT(e.id), COALESCE(SUM(CASE WHEN e.downloaded_at IS NOT NULL THEN 1 ELSE 0 END),0), MAX(e.downloaded_at) FROM series s LEFT JOIN episodes e ON e.series_id=s.id GROUP BY s.name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	summaries := make([]SeriesSummary, 0)
	for rows.Next() {
		var summary SeriesSummary
		var lastDownloaded sql.NullString
		if err := rows.Scan(&summary.Name, &summary.Episodes, &summary.Downloaded, &lastDownloaded); err != nil {
			return nil, err
		}
		summary.LastDownloaded = nullStringPtr(lastDownloaded)
		summaries = append(summaries, summary)
	}
	return summaries, rows.Err()
}

// Info returns the database statistics for the UI/API.
func (d *Database) Info() (map[string]any, error) {
	counts := make(map[string]any)
	for _, table := range []string{"series", "episodes", "movies", "pending_downloads", "cycle_history", "torrent_meta", "gap_search_log", "ignored_episodes", "blocklist"} {
		var count int64
		if err := d.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count); err != nil {
			return nil, err
		}
		counts[table] = count
	}
	var downloadedEpisodes int64
	if err := d.db.QueryRow("SELECT COUNT(*) FROM episodes WHERE downloaded_at IS NOT NULL").Scan(&downloadedEpisodes); err != nil {
		return nil, err
	}
	var downloadedMovies int64
	if err := d.db.QueryRow("SELECT COUNT(*) FROM movies WHERE downloaded_at IS NOT NULL AND removed_at IS NULL").Scan(&downloadedMovies); err != nil {
		return nil, err
	}
	return map[string]any{
		"counts":              counts,
		"downloaded_episodes": downloadedEpisodes,
		"downloaded_movies":   downloadedMovies,
	}, nil
}

// HasData reports whether the database holds any series, movie or episode.
func (d *Database) HasData() (bool, error) {
	var count int64
	if err := d.db.QueryRow("SELECT (SELECT COUNT(*) FROM series) + (SELECT COUNT(*) FROM movies) + (SELECT COUNT(*) FROM episodes)").Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// ResetMovieByName clears the download state of a movie.
func (d *Database) ResetMovieByName(name string) (int, error) {
	result, err := d.db.Exec("UPDATE movies SET downloaded_at=NULL, removed_at=NULL WHERE name=?1", name)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	return int(affected), err
}

// RenameMovieIdentity updates the name/year identity of a movie without
// touching hash, quality, dates or download state.
func (d *Database) RenameMovieIdentity(oldName, oldYear, newName, newYear string) (int, error) {
	result, err := d.db.Exec("UPDATE movies SET name=?1, year=?2 WHERE name=?3 AND year IS ?4", newName, parseOptionalYear(newYear), oldName, parseOptionalYear(oldYear))
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	return int(affected), err
}

// RecordSeenBatch records every release of a cycle in the "seen in feed" tables.
func (d *Database) RecordSeenBatch(releases []models.Release, cfg *Config) error {
	if len(releases) == 0 {
		return nil
	}
	// Chunk in transazioni più piccole come SaveBatch: un'unica transazione da
	// migliaia di righe fa crescere il WAL e tiene occupata la connessione a
	// lungo. Con chunk da 1000 righe e un checkpoint tra i chunk l'atomicità
	// resta per chunk e un crash recupera meno lavoro.
	const chunkSize = 1000
	for start := 0; start < len(releases); start += chunkSize {
		end := start + chunkSize
		if end > len(releases) {
			end = len(releases)
		}
		if err := d.recordSeenChunk(releases[start:end], cfg); err != nil {
			return err
		}
		_ = d.Checkpoint()
	}
	return nil
}

func (d *Database) recordSeenChunk(releases []models.Release, cfg *Config) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	for index := range releases {
		release := &releases[index]
		if release.Kind == "series" {
			if err := insertSeriesSeen(tx, release, cfg.ReleaseScore(release)); err != nil {
				return err
			}
		} else {
			if err := insertMovieSeen(tx, release, cfg.ReleaseScore(release)); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func insertMovieSeen(tx *sql.Tx, release *models.Release, qualityScore int64) error {
	name := utils.ExtractCleanMovieName(release.Title)
	groupKey := utils.CondensedKey(name)
	year := int64(0)
	if release.Year != nil {
		year = *release.Year
	}
	now := nowSQLite()
	_, err := tx.Exec(`INSERT INTO movie_feed_seen (title,name,year,resolution,codec,audio,quality_score,magnet,source,found_at,first_seen_at,group_key)
         VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,?10,?11)
         ON CONFLICT(title) DO UPDATE SET
             found_at=excluded.found_at,
             magnet=excluded.magnet,
             source=excluded.source,
             name=excluded.name,
             year=excluded.year,
             quality_score=excluded.quality_score,
             resolution=excluded.resolution,
             codec=excluded.codec,
             audio=excluded.audio,
             group_key=excluded.group_key,
             first_seen_at=COALESCE(movie_feed_seen.first_seen_at, excluded.first_seen_at)`,
		release.Title, name, year, release.Quality.Resolution, release.Quality.Codec, release.Quality.Audio, qualityScore, release.Magnet, release.Source, now, groupKey)
	return err
}

func insertSeriesSeen(tx *sql.Tx, release *models.Release, qualityScore int64) error {
	name := release.Title
	if release.Series != nil {
		name = *release.Series
	}
	groupKey := utils.CondensedKey(name)
	season := int64(0)
	if release.Season != nil {
		season = *release.Season
	}
	episode := int64(0)
	if release.Episode != nil {
		episode = *release.Episode
	}
	now := nowSQLite()
	_, err := tx.Exec(`INSERT INTO series_feed_seen (title,name,season,episode,resolution,codec,audio,quality_score,magnet,source,found_at,first_seen_at,group_key)
         VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,?11,?11,?12)
         ON CONFLICT(title) DO UPDATE SET
             found_at=excluded.found_at,
             magnet=excluded.magnet,
             source=excluded.source,
             name=excluded.name,
             season=excluded.season,
             episode=excluded.episode,
             quality_score=excluded.quality_score,
             resolution=excluded.resolution,
             codec=excluded.codec,
             audio=excluded.audio,
             group_key=excluded.group_key,
             first_seen_at=COALESCE(series_feed_seen.first_seen_at, excluded.first_seen_at)`,
		release.Title, name, season, episode, release.Quality.Resolution, release.Quality.Codec, release.Quality.Audio, qualityScore, release.Magnet, release.Source, now, groupKey)
	return err
}

func feedSeenGroupFromRow(rows *sql.Rows) (FeedSeenGroup, error) {
	var group FeedSeenGroup
	err := rows.Scan(&group.GroupKey, &group.GroupName, &group.Year, &group.Season, &group.Count, &group.BestScore, &group.BestResolution, &group.LatestFound, &group.FirstFound)
	return group, err
}

func feedSeenEntryFromRow(rows *sql.Rows) (FeedSeenEntry, error) {
	var entry FeedSeenEntry
	err := rows.Scan(&entry.ID, &entry.Title, &entry.Name, &entry.Year, &entry.Season, &entry.Episode, &entry.Resolution, &entry.Codec, &entry.Audio, &entry.QualityScore, &entry.Magnet, &entry.Source, &entry.FoundAt)
	return entry, err
}

// MoviesSeenGrouped returns the grouped "seen in feed" movies and total count.
func (d *Database) MoviesSeenGrouped(offset, limit int, query string) ([]FeedSeenGroup, int64, error) {
	like := seenLikePattern(query)
	var total int64
	if err := d.db.QueryRow("SELECT COUNT(DISTINCT group_key) FROM movie_feed_seen WHERE group_key IS NOT NULL AND group_key <> '' AND COALESCE(name,title) LIKE ?1", like).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := d.db.Query(`SELECT group_key,
                    MAX(COALESCE(NULLIF(name,''),title)) AS group_name,
                    MAX(year) AS year,
                    0 AS season,
                    COUNT(*) AS cnt,
                    MAX(quality_score) AS best_score,
                    SUBSTR(MAX(PRINTF('%010d', quality_score) || COALESCE(resolution,'unknown')),11) AS best_resolution,
                    MAX(found_at) AS latest_found,
                    MIN(COALESCE(first_seen_at,found_at)) AS first_found
             FROM movie_feed_seen
             WHERE group_key IS NOT NULL AND group_key <> '' AND COALESCE(name,title) LIKE ?3
             GROUP BY group_key ORDER BY latest_found DESC LIMIT ?1 OFFSET ?2`, clampInt(limit, 1, 500), offset, like)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	groups := make([]FeedSeenGroup, 0)
	for rows.Next() {
		group, err := feedSeenGroupFromRow(rows)
		if err != nil {
			return nil, 0, err
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return groups, total, nil
}

// SeriesSeenGrouped returns the grouped "seen in feed" series and total count.
func (d *Database) SeriesSeenGrouped(offset, limit int, query string) ([]FeedSeenGroup, int64, error) {
	like := seenLikePattern(query)
	var total int64
	if err := d.db.QueryRow("SELECT COUNT(DISTINCT group_key) FROM series_feed_seen WHERE group_key IS NOT NULL AND group_key <> '' AND COALESCE(name,title) LIKE ?1", like).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := d.db.Query(`SELECT group_key,
                    MAX(COALESCE(NULLIF(name,''),title)) AS group_name,
                    0 AS year,
                    MAX(season) AS season,
                    COUNT(*) AS cnt,
                    MAX(quality_score) AS best_score,
                    SUBSTR(MAX(PRINTF('%010d', quality_score) || COALESCE(resolution,'unknown')),11) AS best_resolution,
                    MAX(found_at) AS latest_found,
                    MIN(COALESCE(first_seen_at,found_at)) AS first_found
             FROM series_feed_seen
             WHERE group_key IS NOT NULL AND group_key <> '' AND COALESCE(name,title) LIKE ?3
             GROUP BY group_key ORDER BY latest_found DESC LIMIT ?1 OFFSET ?2`, clampInt(limit, 1, 500), offset, like)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	groups := make([]FeedSeenGroup, 0)
	for rows.Next() {
		group, err := feedSeenGroupFromRow(rows)
		if err != nil {
			return nil, 0, err
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return groups, total, nil
}

// SeenByGroup returns the releases of one "seen" group, best quality first.
func (d *Database) SeenByGroup(kind, groupKey string, limit int) ([]FeedSeenEntry, error) {
	table := "movie_feed_seen"
	if kind == "series" {
		table = "series_feed_seen"
	}
	year := "COALESCE(year,0)"
	season := "0"
	episode := "0"
	if kind == "series" {
		year = "0"
		season = "COALESCE(season,0)"
		episode = "COALESCE(episode,0)"
	}
	query := fmt.Sprintf("SELECT id,title,COALESCE(name,''),%s,%s,%s,COALESCE(resolution,'unknown'),COALESCE(codec,'unknown'),COALESCE(audio,'unknown'),COALESCE(quality_score,0),COALESCE(magnet,''),COALESCE(source,''),found_at FROM %s WHERE group_key=?1 ORDER BY quality_score DESC, found_at DESC LIMIT ?2", year, season, episode, table)
	rows, err := d.db.Query(query, groupKey, clampInt(limit, 1, 1000))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]FeedSeenEntry, 0)
	for rows.Next() {
		entry, err := feedSeenEntryFromRow(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// SeriesFeedForEpisode returns the releases of an episode already seen during
// RSS/HTML scans.
func (d *Database) SeriesFeedForEpisode(season, episode int64, limit int) ([][3]string, error) {
	rows, err := d.db.Query("SELECT title,COALESCE(magnet,''),COALESCE(source,'') FROM series_feed_seen WHERE season=?1 AND episode=?2 ORDER BY quality_score DESC, found_at DESC LIMIT ?3", season, episode, clampInt(limit, 1, 200))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([][3]string, 0)
	for rows.Next() {
		var title, magnet, source string
		if err := rows.Scan(&title, &magnet, &source); err != nil {
			return nil, err
		}
		results = append(results, [3]string{title, magnet, source})
	}
	return results, rows.Err()
}

// archiveFileConfirmedMissing tells a file that was really deleted from one
// that merely sits on an archive volume that is not available. An unmounted NAS
// leaves an empty mount-point directory, so `stat` answers ENOENT for every
// archived file and the whole history would be reset.
//
// The file counts as deleted when its own directory still exists (the folder
// structure is there, so the volume is mounted), or, when that directory is gone
// too, when the nearest existing ancestor is readable and not empty. An empty or
// unreadable ancestor (or only the filesystem root) means "volume unavailable"
// and the database row is preserved.
func archiveFileConfirmedMissing(path string) bool {
	dir := filepath.Dir(filepath.Clean(path))
	if info, err := os.Stat(dir); err == nil {
		return info.IsDir() && filepath.Dir(dir) != dir
	} else if !errors.Is(err, os.ErrNotExist) {
		return false
	}
	for {
		dir = filepath.Dir(dir)
		// The filesystem root always exists and is never evidence of a deletion.
		if filepath.Dir(dir) == dir {
			return false
		}
		info, err := os.Stat(dir)
		if err == nil && info.IsDir() {
			entries, readErr := os.ReadDir(dir)
			if readErr != nil {
				return false
			}
			return len(entries) > 0
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false
		}
	}
}
