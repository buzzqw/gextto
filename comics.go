// Package gextto's comics module implements the core module: the GetComics
// scraper, the comics SQLite database, the in-process HTTP download registry and
// the comics download cycle. SQL statements, log messages and behaviour are
// preserved from the original implementation.
//
// The original relied on an HTML scraper (tokenizer + selectors) and
// `reqwest`. The Go port keeps the dependency set to the standard library, so a
// small HTML tree builder and the handful of CSS selectors GetComics needs are
// implemented at the bottom of this file.

package gextto

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/utils"
)

// ComicMonitored is one monitored GetComics tag or post.
type ComicMonitored struct {
	ID                    int64   `json:"id"`
	Title                 string  `json:"title"`
	TagURL                string  `json:"tag_url"`
	PostURL               string  `json:"post_url"`
	CoverURL              string  `json:"cover_url"`
	Publisher             string  `json:"publisher"`
	Description           string  `json:"description"`
	FromDate              string  `json:"from_date"`
	SavePath              string  `json:"save_path"`
	Enabled               bool    `json:"enabled"`
	LastChecked           *string `json:"last_checked"`
	LatestDownloadedTitle string  `json:"latest_downloaded_title"`
}

// ComicPost is one article found on a GetComics listing or search page.
type ComicPost struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	TagURL      string `json:"tag_url"`
	CoverURL    string `json:"cover_url"`
	Publisher   string `json:"publisher"`
	Description string `json:"description"`
	Date        string `json:"date"`
}

// ComicLinks groups the download links found in a GetComics post.
type ComicLinks struct {
	Magnets     []string `json:"magnets"`
	Torrents    []string `json:"torrents"`
	Mega        []string `json:"mega"`
	Direct      []string `json:"direct"`
	DownloadNow []string `json:"download_now"`
}

// ComicTorrent is one torrent queued by the comics module.
type ComicTorrent struct {
	Hash          string  `json:"hash"`
	Title         string  `json:"title"`
	PostURL       string  `json:"post_url"`
	SavePath      string  `json:"save_path"`
	CompletedAt   *string `json:"completed_at"`
	ProcessedPath *string `json:"processed_path"`
}

// ComicDownload is one visible entry of the comics download list. HTTP and MEGA
// downloads are kept in memory for their whole lifetime, so the row is never
// persisted.
type ComicDownload struct {
	ID              string  `json:"id"`
	Title           string  `json:"title"`
	Method          string  `json:"method"`
	Status          string  `json:"status"`
	Progress        float64 `json:"progress"`
	DownloadedBytes uint64  `json:"downloaded_bytes"`
	TotalBytes      *uint64 `json:"total_bytes"`
	SpeedBytes      uint64  `json:"speed_bytes"`
	ETASeconds      *uint64 `json:"eta_seconds"`
	Error           *string `json:"error"`
	// Free-form label assigned by the user, shown in the unified download list
	// together with the torrent tags. Kept in memory for the download lifetime
	// because the download row itself is not persisted.
	Tag string `json:"tag"`
	// Resolved URL the download is reading from.
	URL string `json:"url"`
	// Final file path (available once the response headers are known).
	Destination string `json:"destination"`
	// Temporary `.part` path used while the download is in progress.
	Temporary string `json:"temporary"`
	UpdatedAt string `json:"updated_at"`
}

// WeeklyPending is one recorded weekly pack that has a link but was never sent.
type WeeklyPending struct {
	PackDate   string
	Magnet     string
	TorrentURL string
}

// ComicsImportReport is the Result of `import_from_extto`.
type ComicsImportReport struct {
	Monitored int `json:"monitored"`
	History   int `json:"history"`
	Weekly    int `json:"weekly"`
	Settings  int `json:"settings"`
	Skipped   int `json:"skipped"`
}

// ComicsDb wraps the SQLite connection of the comics database.
type ComicsDb struct {
	db *sql.DB
}

// comicsSchema is `ensure_schema` (copied verbatim ).
const comicsSchema = `CREATE TABLE IF NOT EXISTS comics_monitored (
            id INTEGER PRIMARY KEY, title TEXT NOT NULL, tag_url TEXT NOT NULL UNIQUE,
            post_url TEXT DEFAULT '',
            cover_url TEXT DEFAULT '', publisher TEXT DEFAULT '', description TEXT DEFAULT '',
            from_date TEXT NOT NULL, save_path TEXT DEFAULT '', enabled INTEGER DEFAULT 1,
            added_at TEXT DEFAULT (datetime('now')), last_checked TEXT
        );
        CREATE TABLE IF NOT EXISTS comics_history (
            id INTEGER PRIMARY KEY, monitored_id INTEGER NOT NULL, post_url TEXT NOT NULL UNIQUE,
            title TEXT NOT NULL, magnet TEXT DEFAULT '', torrent_url TEXT DEFAULT '',
            sent_at TEXT DEFAULT (datetime('now')), size_bytes INTEGER DEFAULT 0
        );
        CREATE TABLE IF NOT EXISTS comics_weekly (
            id INTEGER PRIMARY KEY, pack_date TEXT NOT NULL UNIQUE, magnet TEXT DEFAULT '',
            torrent_url TEXT DEFAULT '', sent_at TEXT, found_at TEXT DEFAULT (datetime('now')),
            size_bytes INTEGER DEFAULT 0
        );
        CREATE TABLE IF NOT EXISTS comics_torrents (
            hash TEXT PRIMARY KEY, post_url TEXT NOT NULL, title TEXT NOT NULL,
            save_path TEXT NOT NULL, queued_at TEXT DEFAULT (datetime('now')),
            completed_at TEXT, processed_path TEXT
        );
        CREATE TABLE IF NOT EXISTS comics_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);`

// EnsureComicsSchema creates the comics tables and applies the `post_url`
// migration, mirroring `comics::ensure_schema`.
func EnsureComicsSchema(db *sql.DB) error {
	if _, err := db.Exec(comicsSchema); err != nil {
		return err
	}
	columns, err := tableColumns(db, "comics_monitored")
	if err != nil {
		return err
	}
	if !columns["post_url"] {
		if _, err := db.Exec("ALTER TABLE comics_monitored ADD COLUMN post_url TEXT DEFAULT ''"); err != nil {
			return err
		}
	}
	return nil
}

// OpenComicsDb opens (creating it if needed) the comics database at path.
func OpenComicsDb(path string) (*ComicsDb, error) {
	db, err := OpenSQLite(path)
	if err != nil {
		return nil, err
	}
	// The original sets journal_mode=WAL (already done by OpenSQLite) and
	// then hardens the connection before creating the schema.
	if err := HardenConnection(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := EnsureComicsSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &ComicsDb{db: db}, nil
}

// Checkpoint truncates the WAL after a checkpoint.
func (d *ComicsDb) Checkpoint() error {
	return CheckpointConnection(d.db)
}

// QuickCheck runs `PRAGMA quick_check`.
func (d *ComicsDb) QuickCheck() ([]string, error) {
	return QuickCheck(d.db)
}

// Optimize runs `VACUUM`/`ANALYZE`.
func (d *ComicsDb) Optimize(action string) error {
	return OptimizeConnection(d.db, action)
}

// SizeBytes estimates the database size in bytes.
func (d *ComicsDb) SizeBytes() int64 {
	return ConnectionSizeBytes(d.db)
}

// ListMonitored returns the monitored comics, ordered by title.
func (d *ComicsDb) ListMonitored(enabledOnly bool) ([]ComicMonitored, error) {
	sqlText := "SELECT m.id,m.title,m.tag_url,m.post_url,m.cover_url,m.publisher,m.description,m.from_date,m.save_path,m.enabled,m.last_checked,COALESCE((SELECT h.title FROM comics_history h WHERE h.monitored_id=m.id ORDER BY h.id DESC LIMIT 1),'') FROM comics_monitored m ORDER BY m.title COLLATE NOCASE"
	if enabledOnly {
		sqlText = "SELECT m.id,m.title,m.tag_url,m.post_url,m.cover_url,m.publisher,m.description,m.from_date,m.save_path,m.enabled,m.last_checked,COALESCE((SELECT h.title FROM comics_history h WHERE h.monitored_id=m.id ORDER BY h.id DESC LIMIT 1),'') FROM comics_monitored m WHERE m.enabled=1 ORDER BY m.title COLLATE NOCASE"
	}
	rows, err := d.db.Query(sqlText)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ComicMonitored{}
	for rows.Next() {
		var item ComicMonitored
		var enabled int64
		var lastChecked sql.NullString
		if err := rows.Scan(
			&item.ID,
			&item.Title,
			&item.TagURL,
			&item.PostURL,
			&item.CoverURL,
			&item.Publisher,
			&item.Description,
			&item.FromDate,
			&item.SavePath,
			&enabled,
			&lastChecked,
			&item.LatestDownloadedTitle,
		); err != nil {
			return nil, err
		}
		item.Enabled = enabled != 0
		item.LastChecked = nullStringPtr(lastChecked)
		items = append(items, item)
	}
	return items, rows.Err()
}

// AddMonitored inserts a monitored title without the rich metadata.
func (d *ComicsDb) AddMonitored(title, tagURL, fromDate, savePath string) (int64, error) {
	return d.AddMonitoredWithMetadata(title, tagURL, "", "", "", "", fromDate, savePath)
}

// normalizeComicTagURL reproduces the tag URL normalisation: an absolute
// URL must belong to getcomics.org (only its path and query are kept), a
// leading slash is kept as-is and anything else is prefixed with a slash.
func normalizeComicTagURL(tagURL string) (string, error) {
	parsed, err := url.Parse(tagURL)
	if err == nil && parsed.Scheme != "" {
		scheme := strings.ToLower(parsed.Scheme)
		host := strings.ToLower(parsed.Hostname())
		if (scheme != "http" && scheme != "https") || (host != "getcomics.org" && host != "www.getcomics.org") {
			return "", fmt.Errorf("comic tag URL must belong to getcomics.org")
		}
		path := parsed.Path
		if path == "" {
			// the `Url` always reports at least "/" for an authority URL.
			path = "/"
		}
		if parsed.RawQuery != "" {
			path += "?" + parsed.RawQuery
		}
		return path, nil
	}
	if strings.HasPrefix(tagURL, "/") {
		return tagURL, nil
	}
	return "/" + tagURL, nil
}

// AddMonitoredWithMetadata inserts (or updates) a monitored title.
func (d *ComicsDb) AddMonitoredWithMetadata(title, tagURL, postURL, coverURL, publisher, description, fromDate, savePath string) (int64, error) {
	normalized, err := normalizeComicTagURL(tagURL)
	if err != nil {
		return 0, err
	}
	_, err = d.db.Exec(
		"INSERT INTO comics_monitored(title,tag_url,post_url,cover_url,publisher,description,from_date,save_path) VALUES (?1,?2,?3,?4,?5,?6,?7,?8) ON CONFLICT(tag_url) DO UPDATE SET title=excluded.title,post_url=CASE WHEN excluded.post_url<>'' THEN excluded.post_url ELSE comics_monitored.post_url END,cover_url=CASE WHEN excluded.cover_url<>'' THEN excluded.cover_url ELSE comics_monitored.cover_url END,publisher=CASE WHEN excluded.publisher<>'' THEN excluded.publisher ELSE comics_monitored.publisher END,description=CASE WHEN excluded.description<>'' THEN excluded.description ELSE comics_monitored.description END,from_date=excluded.from_date,save_path=excluded.save_path,enabled=1",
		title, normalized, postURL, coverURL, publisher, description, fromDate, savePath,
	)
	if err != nil {
		return 0, err
	}
	var id int64
	if err := d.db.QueryRow("SELECT id FROM comics_monitored WHERE tag_url=?1", normalized).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// SetEnabled enables or disables a monitored comic.
func (d *ComicsDb) SetEnabled(id int64, enabled bool) (bool, error) {
	value := int64(0)
	if enabled {
		value = 1
	}
	result, err := d.db.Exec("UPDATE comics_monitored SET enabled=?1 WHERE id=?2", value, id)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// RemoveMonitored deletes a monitored comic.
func (d *ComicsDb) RemoveMonitored(id int64) (bool, error) {
	result, err := d.db.Exec("DELETE FROM comics_monitored WHERE id=?1", id)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// MarkChecked records the last check time of a monitored comic.
func (d *ComicsDb) MarkChecked(id int64) error {
	_, err := d.db.Exec("UPDATE comics_monitored SET last_checked=datetime('now') WHERE id=?1", id)
	return err
}

// AlreadySent reports whether a post URL is already in the comics history.
func (d *ComicsDb) AlreadySent(postURL string) (bool, error) {
	var exists bool
	err := d.db.QueryRow("SELECT EXISTS(SELECT 1 FROM comics_history WHERE post_url=?1)", postURL).Scan(&exists)
	return exists, err
}

// AddHistory records a downloaded post. It returns false when the post URL was
// already present.
func (d *ComicsDb) AddHistory(monitoredID int64, postURL, title, magnet, torrentURL string) (bool, error) {
	result, err := d.db.Exec("INSERT OR IGNORE INTO comics_history(monitored_id,post_url,title,magnet,torrent_url) VALUES (?1,?2,?3,?4,?5)", monitoredID, postURL, title, magnet, torrentURL)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// AddTorrent registers a torrent queued by the comics module.
func (d *ComicsDb) AddTorrent(hash, postURL, title, savePath string) error {
	_, err := d.db.Exec("INSERT OR REPLACE INTO comics_torrents(hash,post_url,title,save_path) VALUES (?1,?2,?3,?4)", strings.ToLower(hash), postURL, title, savePath)
	return err
}

// Torrent returns the comics torrent with the given hash, or nil.
func (d *ComicsDb) Torrent(hash string) (*ComicTorrent, error) {
	var item ComicTorrent
	var completedAt, processedPath sql.NullString
	err := d.db.QueryRow("SELECT hash,title,post_url,save_path,completed_at,processed_path FROM comics_torrents WHERE hash=?1", strings.ToLower(hash)).Scan(
		&item.Hash, &item.Title, &item.PostURL, &item.SavePath, &completedAt, &processedPath,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.CompletedAt = nullStringPtr(completedAt)
	item.ProcessedPath = nullStringPtr(processedPath)
	return &item, nil
}

// CompleteTorrent marks a comics torrent as completed with its processed path.
func (d *ComicsDb) CompleteTorrent(hash, path string) error {
	_, err := d.db.Exec("UPDATE comics_torrents SET completed_at=datetime('now'),processed_path=?1 WHERE hash=?2", path, strings.ToLower(hash))
	return err
}

// History returns the most recent history rows.
func (d *ComicsDb) History(limit int64) ([]map[string]any, error) {
	rows, err := d.db.Query("SELECT post_url,title,magnet,torrent_url,sent_at,size_bytes FROM comics_history ORDER BY id DESC LIMIT ?1", clampInt64(limit, 1, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var postURL, title, magnet, torrentURL, sentAt string
		var sizeBytes int64
		if err := rows.Scan(&postURL, &title, &magnet, &torrentURL, &sentAt, &sizeBytes); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{
			"post_url":    postURL,
			"title":       title,
			"magnet":      magnet,
			"torrent_url": torrentURL,
			"sent_at":     sentAt,
			"size_bytes":  sizeBytes,
		})
	}
	return items, rows.Err()
}

// RemoveHistory deletes the history row of a post URL.
func (d *ComicsDb) RemoveHistory(postURL string) (bool, error) {
	result, err := d.db.Exec("DELETE FROM comics_history WHERE post_url=?1", postURL)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// Weekly returns the most recent weekly pack rows.
func (d *ComicsDb) Weekly(limit int64) ([]map[string]any, error) {
	rows, err := d.db.Query("SELECT pack_date,magnet,torrent_url,sent_at,found_at,size_bytes FROM comics_weekly ORDER BY pack_date DESC LIMIT ?1", clampInt64(limit, 1, 200))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var packDate, magnet, torrentURL, foundAt string
		var sentAt sql.NullString
		var sizeBytes int64
		if err := rows.Scan(&packDate, &magnet, &torrentURL, &sentAt, &foundAt, &sizeBytes); err != nil {
			return nil, err
		}
		var sent any
		if sentAt.Valid {
			sent = sentAt.String
		}
		items = append(items, map[string]any{
			"pack_date":   packDate,
			"magnet":      magnet,
			"torrent_url": torrentURL,
			"sent_at":     sent,
			"found_at":    foundAt,
			"size_bytes":  sizeBytes,
		})
	}
	return items, rows.Err()
}

// Setting reads a comics setting, returning the default when it is missing.
func (d *ComicsDb) Setting(key, fallback string) (string, error) {
	var value string
	if err := d.db.QueryRow("SELECT value FROM comics_settings WHERE key=?1", key).Scan(&value); err != nil {
		return fallback, nil
	}
	return value, nil
}

// SetSetting writes a comics setting.
func (d *ComicsDb) SetSetting(key, value string) error {
	_, err := d.db.Exec("INSERT INTO comics_settings(key,value) VALUES (?1,?2) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, value)
	return err
}

// AddWeekly records a weekly pack. It returns false when the pack date already
// existed.
func (d *ComicsDb) AddWeekly(packDate, magnet, torrentURL string) (bool, error) {
	result, err := d.db.Exec("INSERT OR IGNORE INTO comics_weekly(pack_date,magnet,torrent_url) VALUES (?1,?2,?3)", packDate, magnet, torrentURL)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// MarkWeeklySent marks a weekly pack as sent.
func (d *ComicsDb) MarkWeeklySent(packDate string) error {
	_, err := d.db.Exec("UPDATE comics_weekly SET sent_at=datetime('now') WHERE pack_date=?1", packDate)
	return err
}

// WeeklySent reports whether a weekly pack was already sent.
func (d *ComicsDb) WeeklySent(packDate string) (bool, error) {
	var exists bool
	err := d.db.QueryRow("SELECT EXISTS(SELECT 1 FROM comics_weekly WHERE pack_date=?1 AND sent_at IS NOT NULL)", packDate).Scan(&exists)
	return exists, err
}

// UpsertWeeklyLinks records the links of a weekly pack, filling a row that was
// created before the torrent was available. It returns true when the row now
// has a link and is still to be sent.
func (d *ComicsDb) UpsertWeeklyLinks(packDate, magnet, torrentURL string) (bool, error) {
	_, err := d.db.Exec(
		"INSERT INTO comics_weekly(pack_date,magnet,torrent_url) VALUES (?1,?2,?3) ON CONFLICT(pack_date) DO UPDATE SET magnet=CASE WHEN COALESCE(comics_weekly.magnet,'')='' THEN excluded.magnet ELSE comics_weekly.magnet END,torrent_url=CASE WHEN COALESCE(comics_weekly.torrent_url,'')='' THEN excluded.torrent_url ELSE comics_weekly.torrent_url END",
		packDate, magnet, torrentURL,
	)
	if err != nil {
		return false, err
	}
	var eligible bool
	if err := d.db.QueryRow("SELECT sent_at IS NULL AND (COALESCE(magnet,'')<>'' OR COALESCE(torrent_url,'')<>'') FROM comics_weekly WHERE pack_date=?1", packDate).Scan(&eligible); err != nil {
		return false, err
	}
	return eligible, nil
}

// PendingWeekly returns the recorded weekly packs that have a link but were
// never sent.
func (d *ComicsDb) PendingWeekly() ([]WeeklyPending, error) {
	rows, err := d.db.Query(
		"SELECT pack_date,COALESCE(magnet,''),COALESCE(torrent_url,'') FROM comics_weekly WHERE sent_at IS NULL AND (COALESCE(magnet,'')<>'' OR COALESCE(torrent_url,'')<>'') ORDER BY pack_date",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []WeeklyPending{}
	for rows.Next() {
		var item WeeklyPending
		if err := rows.Scan(&item.PackDate, &item.Magnet, &item.TorrentURL); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ---------------------------------------------------------------------------
// In-process HTTP download registry.
//
// kept two `OnceLock<Mutex<BTreeMap<...>>>` statics. The Go port uses two
// mutex-protected maps with goroutines for the active downloads.
// ---------------------------------------------------------------------------

var (
	httpDownloadsMu    sync.Mutex
	httpDownloadsStore = map[string]*ComicDownload{}
	httpControlsMu     sync.Mutex
	httpControls       = map[string]*httpControl{}
)

// httpControl carries the runtime controls of one HTTP download. It is kept
// beside the visible ComicDownload so pause/resume/cancel can act on the running
// task without leaking atomics into the JSON API.
type httpControl struct {
	paused         atomic.Bool
	cancelled      atomic.Bool
	running        atomic.Bool
	deleteOnCancel atomic.Bool
	url            string
	title          string
	targetDir      string
	client         *http.Client
	pathsMu        sync.Mutex
	temporary      *string
	destination    *string
}

// newComicDownloadID builds the `comic-<uuid-v4>` identifier used by .
func newComicDownloadID() string {
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("comic-%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}

// comicTimestamp is `chrono::Utc::now().to_rfc3339()`.
func comicTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func httpControlByID(id string) *httpControl {
	httpControlsMu.Lock()
	defer httpControlsMu.Unlock()
	return httpControls[id]
}

func httpDownloadStatus(id string) (string, bool) {
	httpDownloadsMu.Lock()
	defer httpDownloadsMu.Unlock()
	download, ok := httpDownloadsStore[id]
	if !ok {
		return "", false
	}
	return download.Status, true
}

func updateHTTPDownload(id string, update func(*ComicDownload)) {
	httpDownloadsMu.Lock()
	defer httpDownloadsMu.Unlock()
	if download, ok := httpDownloadsStore[id]; ok {
		update(download)
		download.UpdatedAt = comicTimestamp()
	}
}

func removeHTTPDownload(id string) {
	httpDownloadsMu.Lock()
	delete(httpDownloadsStore, id)
	httpDownloadsMu.Unlock()
	httpControlsMu.Lock()
	delete(httpControls, id)
	httpControlsMu.Unlock()
}

// HTTPDownloads returns a snapshot of the comics downloads, most recently
// updated first.
func HTTPDownloads() []ComicDownload {
	httpDownloadsMu.Lock()
	items := make([]ComicDownload, 0, len(httpDownloadsStore))
	for _, download := range httpDownloadsStore {
		items = append(items, *download)
	}
	httpDownloadsMu.Unlock()
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt > items[j].UpdatedAt })
	return items
}

// SetHTTPDownloadTag updates the user-assigned tag of an HTTP download.
func SetHTTPDownloadTag(id, tag string) bool {
	httpDownloadsMu.Lock()
	defer httpDownloadsMu.Unlock()
	download, ok := httpDownloadsStore[id]
	if !ok {
		return false
	}
	download.Tag = strings.TrimSpace(tag)
	download.UpdatedAt = comicTimestamp()
	return true
}

// PauseHTTPDownload pauses a running HTTP download, keeping the `.part` file.
func PauseHTTPDownload(id string) bool {
	if status, ok := httpDownloadStatus(id); !ok || status != "downloading" {
		return false
	}
	control := httpControlByID(id)
	if control == nil {
		return false
	}
	control.paused.Store(true)
	return true
}

// ResumeHTTPDownload resumes a paused HTTP download, spawning a task when none
// is running.
func ResumeHTTPDownload(id string) bool {
	if status, ok := httpDownloadStatus(id); !ok || status != "paused" {
		return false
	}
	control := httpControlByID(id)
	if control == nil {
		return false
	}
	control.cancelled.Store(false)
	control.paused.Store(false)
	updateHTTPDownload(id, func(download *ComicDownload) {
		download.Status = "downloading"
		download.Error = nil
	})
	if !control.running.Load() {
		go runHTTPDownload(id)
	}
	return true
}

// CancelHTTPDownload removes a download from the list. With deleteFiles it also
// deletes the partial `.part` file and the finished file. MEGA downloads (which
// have no control) are only removed from the list.
func CancelHTTPDownload(id string, deleteFiles bool) bool {
	control := httpControlByID(id)
	if control == nil {
		removeHTTPDownload(id)
		return true
	}
	control.deleteOnCancel.Store(deleteFiles)
	control.cancelled.Store(true)
	control.paused.Store(false)
	if !control.running.Load() {
		finalizeCancelled(id, control)
	}
	return true
}

func finalizeCancelled(id string, control *httpControl) {
	removeHTTPDownload(id)
	if control.deleteOnCancel.Load() {
		control.pathsMu.Lock()
		temporary := control.temporary
		destination := control.destination
		control.pathsMu.Unlock()
		if temporary != nil {
			_ = os.Remove(*temporary)
		}
		if destination != nil {
			_ = os.Remove(*destination)
		}
	}
}

// ClearFinishedHTTPDownloads removes completed/errored downloads from the list,
// keeping their files.
func ClearFinishedHTTPDownloads() int {
	httpDownloadsMu.Lock()
	finished := []string{}
	for id, download := range httpDownloadsStore {
		if download.Status == "completed" || download.Status == "error" {
			finished = append(finished, id)
		}
	}
	httpDownloadsMu.Unlock()
	for _, id := range finished {
		removeHTTPDownload(id)
	}
	return len(finished)
}

func registerHTTPDownload(title, method, rawURL string) string {
	id := newComicDownloadID()
	progress := 0.0
	if method == "mega" {
		progress = -1.0
	}
	download := &ComicDownload{
		ID:        id,
		Title:     title,
		Method:    method,
		Status:    "downloading",
		Progress:  progress,
		Tag:       "Comic",
		URL:       rawURL,
		UpdatedAt: comicTimestamp(),
	}
	httpDownloadsMu.Lock()
	httpDownloadsStore[id] = download
	httpDownloadsMu.Unlock()
	return id
}

func installHTTPControl(id string, client *http.Client, rawURL, targetDir, title string) *httpControl {
	control := &httpControl{
		url:       rawURL,
		title:     title,
		targetDir: targetDir,
		client:    client,
	}
	httpControlsMu.Lock()
	httpControls[id] = control
	httpControlsMu.Unlock()
	return control
}

// ---------------------------------------------------------------------------
// GetComics client.
// ---------------------------------------------------------------------------

const (
	comicsUserAgent            = "gextto/0.1 comics"
	comicsHTTPDownloadTimeout  = 1800 * time.Second
	comicsHTTPDownloadAttempts = 3
)

// GetComicsClient is the GetComics scraper.
type GetComicsClient struct {
	client  *http.Client
	baseURL string
}

// NewGetComicsClient is `GetComicsClient::new`.
func NewGetComicsClient() *GetComicsClient {
	return &GetComicsClient{
		client:  &http.Client{Timeout: 15 * time.Second},
		baseURL: "https://getcomics.org",
	}
}

// absolute joins a path to the base URL, mirroring `Url::join`.
func (c *GetComicsClient) absolute(path string) (string, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return "", err
	}
	reference, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(reference).String(), nil
}

// do performs a GET with the comics user agent and treats 4xx/5xx as errors.
func (c *GetComicsClient) do(rawURL string) (*http.Response, error) {
	request, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", comicsUserAgent)
	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 400 {
		response.Body.Close()
		return nil, fmt.Errorf("HTTP status %d", response.StatusCode)
	}
	return response, nil
}

func (c *GetComicsClient) fetch(rawURL string) (string, error) {
	response, err := c.do(rawURL)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// TagPosts fetches a tag page and parses its articles.
func (c *GetComicsClient) TagPosts(tagURL, fromDate string) ([]ComicPost, error) {
	absolute, err := c.absolute(tagURL)
	if err != nil {
		return nil, err
	}
	html, err := c.fetch(absolute)
	if err != nil {
		return nil, err
	}
	return parseComicArticles(html, fromDate, absolute), nil
}

// SearchPosts searches GetComics through its WordPress search endpoint. The
// returned URL can also be stored as a monitored source.
func (c *GetComicsClient) SearchPosts(query string) (string, []ComicPost, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", nil, fmt.Errorf("search query is required")
	}
	target := c.baseURL + "?s=" + url.QueryEscape(query)
	posts, err := c.TagPosts(target, "")
	if err != nil {
		return "", nil, err
	}
	return target, posts, nil
}

// Links parses the download links of a post page.
func (c *GetComicsClient) Links(postURL string) (ComicLinks, error) {
	html, err := c.fetch(postURL)
	if err != nil {
		return ComicLinks{}, err
	}
	return parseComicLinks(html, postURL)
}

// DownloadDirect resolves a GetComics redirect and downloads it to disk.
func (c *GetComicsClient) DownloadDirect(rawURL, targetDir, title string) (string, error) {
	resolved, err := c.resolveDirectURL(rawURL)
	if err != nil {
		return "", err
	}
	return DownloadHTTP(c.client, resolved, targetDir, title)
}

// StartDirectDownload resolves a GetComics redirect and starts the HTTP download
// in the background, returning the id used by the comics download list.
func (c *GetComicsClient) StartDirectDownload(rawURL, targetDir, title string) (string, error) {
	resolved, err := c.resolveDirectURL(rawURL)
	if err != nil {
		return "", err
	}
	return startHTTPDownload(c.client, resolved, targetDir, title), nil
}

// DownloadTorrent downloads a `.torrent` file.
func (c *GetComicsClient) DownloadTorrent(rawURL, targetDir string) (string, error) {
	return DownloadTorrentFile(c.client, rawURL, targetDir)
}

// ResolveMega resolves a GetComics redirect to its final MEGA URL.
func (c *GetComicsClient) ResolveMega(rawURL string) (string, error) {
	if parsed, err := url.Parse(rawURL); err == nil {
		host := strings.ToLower(parsed.Hostname())
		if host == "mega.nz" || host == "mega.co.nz" {
			return rawURL, nil
		}
	}
	response, err := c.do(rawURL)
	if err != nil {
		return "", err
	}
	finalHost := strings.ToLower(response.Request.URL.Hostname())
	if finalHost == "mega.nz" || finalHost == "mega.co.nz" {
		finalURL := response.Request.URL.String()
		response.Body.Close()
		return finalURL, nil
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		return "", err
	}
	pattern := utils.MustCachedRegex(`https?://(?:mega\.nz|mega\.co\.nz)/[^\s"'<>]+`)
	for _, match := range pattern.FindAllString(string(body), -1) {
		candidate := strings.ReplaceAll(match, "&amp;", "&")
		if parsed, err := url.Parse(candidate); err == nil {
			host := strings.ToLower(parsed.Hostname())
			if host == "mega.nz" || host == "mega.co.nz" {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("Mega URL not found in GetComics redirect")
}

// resolveDirectURL converts a GetComics `/dls/...` PixelDrain page into the
// PixelDrain binary API endpoint.
func (c *GetComicsClient) resolveDirectURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	host := strings.ToLower(parsed.Hostname())
	isRedirect := (host == "getcomics.org" || host == "www.getcomics.org") && strings.HasPrefix(parsed.Path, "/dls/")
	if !isRedirect {
		return rawURL, nil
	}
	response, err := c.do(rawURL)
	if err != nil {
		return "", err
	}
	response.Body.Close()
	redirected := response.Request.URL
	redirectedHost := strings.ToLower(redirected.Hostname())
	if redirectedHost == "pixeldrain.com" || redirectedHost == "www.pixeldrain.com" {
		segments := strings.Split(redirected.EscapedPath(), "/")
		if len(segments) > 0 && segments[0] == "" {
			segments = segments[1:]
		}
		if len(segments) > 0 && segments[0] == "u" {
			if len(segments) > 1 && segments[1] != "" {
				return fmt.Sprintf("https://pixeldrain.com/api/file/%s?download=1", segments[1]), nil
			}
		}
	}
	return redirected.String(), nil
}

// WeeklyLinks finds the weekly pack page of a date, trying the known paths and
// then GetComics' search.
func (c *GetComicsClient) WeeklyLinks(date string) (string, ComicLinks, error) {
	for _, path := range []string{
		fmt.Sprintf("/blog/%s-weekly-pack/", date),
		fmt.Sprintf("/%s-weekly-pack/", date),
	} {
		target, err := c.absolute(path)
		if err != nil {
			return "", ComicLinks{}, err
		}
		response, err := c.do(target)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			return "", ComicLinks{}, err
		}
		links, _ := parseComicLinks(string(body), target)
		if len(links.Magnets) > 0 || len(links.Torrents) > 0 || len(links.Mega) > 0 || len(links.Direct) > 0 {
			return target, links, nil
		}
	}
	// Strategy 3 (like the legacy extto): textual search.
	if parsed, err := time.Parse("2006-01-02", date); err == nil {
		query := fmt.Sprintf("%s Weekly Pack", parsed.Format("2006.01.02"))
		if _, posts, err := c.SearchPosts(query); err == nil {
			for _, post := range posts {
				title := strings.ToLower(post.Title)
				if !strings.Contains(title, "weekly") || !strings.Contains(title, "pack") {
					continue
				}
				if links, err := c.Links(post.URL); err == nil {
					if len(links.Magnets) > 0 || len(links.Torrents) > 0 || len(links.Mega) > 0 || len(links.Direct) > 0 {
						return post.URL, links, nil
					}
				}
			}
		}
	}
	return "", ComicLinks{}, fmt.Errorf("weekly pack not found")
}

// ---------------------------------------------------------------------------
// HTTP / torrent / MEGA downloads.
// ---------------------------------------------------------------------------

type comicHTTPOutcome int

const (
	comicHTTPCompleted comicHTTPOutcome = iota
	comicHTTPPaused
	comicHTTPCancelled
)

// comicsDownloadClient strips the requester timeout so the long per-request
// context of a file download is not cut short by the 15 s page timeout.
func comicsDownloadClient(client *http.Client) *http.Client {
	if client == nil {
		return &http.Client{}
	}
	return &http.Client{Transport: client.Transport}
}

// comicDownloadFilename is the `.cbz` name derived from a post title.
func comicDownloadFilename(title string) string {
	var builder strings.Builder
	for _, r := range title {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			builder.WriteRune(r)
		} else {
			builder.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(builder.String()), " ") + ".cbz"
}

// withPartExtension mirrors `Path::with_extension("part")`.
func withPartExtension(path string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + ".part"
}

// DownloadHTTP is the awaited HTTP download used by the automatic comics cycle.
// It registers a row and controls so it can still be paused or removed from the
// UI.
func DownloadHTTP(client *http.Client, rawURL, targetDir, title string) (string, error) {
	id := registerHTTPDownload(title, "http", rawURL)
	control := installHTTPControl(id, client, rawURL, targetDir, title)
	control.running.Store(true)
	outcome, path, err := downloadHTTPRegistered(client, rawURL, targetDir, title, id, control)
	control.running.Store(false)
	return handleHTTPOutcome(id, control, outcome, path, err)
}

func startHTTPDownload(client *http.Client, rawURL, targetDir, title string) string {
	id := registerHTTPDownload(title, "http", rawURL)
	installHTTPControl(id, client, rawURL, targetDir, title)
	logging.Info("comic HTTP download started", "title", title, "download_id", id)
	go runHTTPDownload(id)
	return id
}

// runHTTPDownload drives one background HTTP download to completion, pause or
// cancellation.
func runHTTPDownload(id string) {
	control := httpControlByID(id)
	if control == nil {
		return
	}
	control.running.Store(true)
	outcome, path, err := downloadHTTPRegistered(control.client, control.url, control.targetDir, control.title, id, control)
	control.running.Store(false)
	_, _ = handleHTTPOutcome(id, control, outcome, path, err)
}

func handleHTTPOutcome(id string, control *httpControl, outcome comicHTTPOutcome, path string, err error) (string, error) {
	switch {
	case err != nil:
		message := err.Error()
		updateHTTPDownload(id, func(download *ComicDownload) {
			download.Status = "error"
			download.Error = &message
			download.SpeedBytes = 0
			download.ETASeconds = nil
		})
		logging.Warn("comic download failed", "title", control.title, "download_id", id, "error", err)
		return "", err
	case outcome == comicHTTPCompleted:
		updateHTTPDownload(id, func(download *ComicDownload) {
			download.Status = "completed"
			download.Progress = 100.0
			download.SpeedBytes = 0
			download.ETASeconds = nil
		})
		logging.Info("comic HTTP download completed", "title", control.title, "download_id", id, "path", path)
		return path, nil
	case outcome == comicHTTPPaused:
		updateHTTPDownload(id, func(download *ComicDownload) {
			download.Status = "paused"
			download.SpeedBytes = 0
			download.ETASeconds = nil
		})
		return "", fmt.Errorf("comic download paused")
	default:
		finalizeCancelled(id, control)
		return "", fmt.Errorf("comic download cancelled")
	}
}

func downloadHTTPRegistered(client *http.Client, rawURL, targetDir, title, id string, control *httpControl) (comicHTTPOutcome, string, error) {
	var lastErr error
	for attempt := 1; attempt <= comicsHTTPDownloadAttempts; attempt++ {
		if control.cancelled.Load() {
			return comicHTTPCancelled, "", nil
		}
		if control.paused.Load() {
			return comicHTTPPaused, "", nil
		}
		outcome, path, err := downloadHTTPOnce(client, id, rawURL, targetDir, title, control)
		if err == nil {
			return outcome, path, nil
		}
		wrapped := fmt.Errorf("GET %s: %w", rawURL, err)
		if attempt < comicsHTTPDownloadAttempts {
			logging.Warn("comic download attempt failed; retrying", "attempt", attempt, "error", wrapped)
			time.Sleep(time.Duration(2*attempt) * time.Second)
		}
		lastErr = wrapped
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("comic download failed")
	}
	return comicHTTPCompleted, "", lastErr
}

func downloadHTTPOnce(client *http.Client, id, rawURL, targetDir, title string, control *httpControl) (comicHTTPOutcome, string, error) {
	if control.cancelled.Load() {
		return comicHTTPCancelled, "", nil
	}
	if control.paused.Load() {
		return comicHTTPPaused, "", nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return comicHTTPCompleted, "", fmt.Errorf("invalid comic download URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return comicHTTPCompleted, "", fmt.Errorf("invalid comic download URL")
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return comicHTTPCompleted, "", err
	}
	destination := filepath.Join(targetDir, comicDownloadFilename(title))
	temporary := withPartExtension(destination)
	control.pathsMu.Lock()
	control.destination = &destination
	control.temporary = &temporary
	control.pathsMu.Unlock()
	updateHTTPDownload(id, func(download *ComicDownload) {
		download.Destination = destination
		download.Temporary = temporary
	})
	offset := int64(0)
	if info, statErr := os.Stat(temporary); statErr == nil {
		offset = info.Size()
	}
	ctx, cancel := context.WithTimeout(context.Background(), comicsHTTPDownloadTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return comicHTTPCompleted, "", err
	}
	request.Header.Set("User-Agent", comicsUserAgent)
	// Binary files: no decompression, so a decoder error cannot involve the
	// content.
	request.Header.Set("Accept-Encoding", "identity")
	if offset > 0 {
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	response, err := comicsDownloadClient(client).Do(request)
	if err != nil {
		return comicHTTPCompleted, "", err
	}
	defer response.Body.Close()
	// The `.part` file was already complete: the server answers 416 and it only
	// needs to be renamed.
	if offset > 0 && response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		if err := os.Rename(temporary, destination); err != nil {
			return comicHTTPCompleted, "", err
		}
		return comicHTTPCompleted, destination, nil
	}
	if response.StatusCode >= 400 {
		return comicHTTPCompleted, "", fmt.Errorf("HTTP status %d", response.StatusCode)
	}
	if contentType := strings.ToLower(response.Header.Get("Content-Type")); strings.Contains(contentType, "text/html") {
		return comicHTTPCompleted, "", fmt.Errorf("comic download returned HTML instead of a file")
	}
	// Resume only when the server confirms the Range (206); otherwise restart.
	resumed := offset > 0 && response.StatusCode == http.StatusPartialContent
	if offset > 0 && !resumed {
		_ = os.Remove(temporary)
	}
	flags := os.O_CREATE | os.O_WRONLY
	if resumed {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(temporary, flags, 0o644)
	if err != nil {
		return comicHTTPCompleted, "", err
	}
	base := int64(0)
	if resumed {
		base = offset
	}
	var total *uint64
	if response.ContentLength >= 0 {
		value := uint64(response.ContentLength) + uint64(base)
		total = &value
	}
	updateHTTPDownload(id, func(download *ComicDownload) {
		download.TotalBytes = total
		download.DownloadedBytes = uint64(base)
		download.Status = "downloading"
		download.Error = nil
	})
	started := time.Now()
	downloaded := base
	buffer := make([]byte, 64*1024)
	for {
		read, readErr := response.Body.Read(buffer)
		if read > 0 {
			if control.cancelled.Load() {
				_ = file.Sync()
				_ = file.Close()
				return comicHTTPCancelled, "", nil
			}
			if control.paused.Load() {
				_ = file.Sync()
				_ = file.Close()
				return comicHTTPPaused, "", nil
			}
			if _, writeErr := file.Write(buffer[:read]); writeErr != nil {
				_ = file.Close()
				return comicHTTPCompleted, "", writeErr
			}
			downloaded += int64(read)
			elapsed := time.Since(started).Seconds()
			if elapsed < 0.001 {
				elapsed = 0.001
			}
			speed := uint64(float64(downloaded-base) / elapsed)
			progress := -1.0
			if total != nil {
				if *total == 0 {
					progress = 0
				} else {
					progress = math.Min(float64(downloaded)*100.0/float64(*total), 100.0)
				}
			}
			var eta *uint64
			if total != nil && speed > 0 && uint64(downloaded) < *total {
				value := (*total - uint64(downloaded)) / speed
				eta = &value
			}
			updateHTTPDownload(id, func(download *ComicDownload) {
				download.DownloadedBytes = uint64(downloaded)
				download.SpeedBytes = speed
				download.Progress = progress
				download.ETASeconds = eta
			})
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			_ = file.Close()
			return comicHTTPCompleted, "", readErr
		}
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return comicHTTPCompleted, "", err
	}
	if err := file.Close(); err != nil {
		return comicHTTPCompleted, "", err
	}
	if downloaded == 0 {
		return comicHTTPCompleted, "", fmt.Errorf("comic download is empty")
	}
	if err := os.Rename(temporary, destination); err != nil {
		return comicHTTPCompleted, "", err
	}
	return comicHTTPCompleted, destination, nil
}

// DownloadTorrentFile downloads a `.torrent` file to targetDir.
func DownloadTorrentFile(client *http.Client, rawURL, targetDir string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid torrent URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("refusing non-HTTP torrent URL")
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", err
	}
	request, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", comicsUserAgent)
	response, err := comicsDownloadClient(client).Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP status %d", response.StatusCode)
	}
	if contentType := strings.ToLower(response.Header.Get("Content-Type")); strings.Contains(contentType, "text/html") {
		return "", fmt.Errorf("torrent URL returned HTML instead of a torrent file")
	}
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	if len(payload) == 0 {
		return "", fmt.Errorf("torrent file is empty")
	}
	destination := filepath.Join(targetDir, utils.StableID(rawURL)+".torrent")
	temporary := withPartExtension(destination)
	if err := os.WriteFile(temporary, payload, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(temporary, destination); err != nil {
		return "", err
	}
	return destination, nil
}

func comicDirEntries(dir string) (map[string]struct{}, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		names[entry.Name()] = struct{}{}
	}
	return names, nil
}

// DownloadMega downloads a MEGA URL with an external downloader.
func DownloadMega(executable, rawURL, targetDir string) (string, error) {
	id := registerHTTPDownload(rawURL, "mega", rawURL)
	path, err := downloadMegaInner(executable, rawURL, targetDir)
	if err != nil {
		message := err.Error()
		updateHTTPDownload(id, func(download *ComicDownload) {
			download.Status = "error"
			download.Error = &message
		})
		return "", err
	}
	updateHTTPDownload(id, func(download *ComicDownload) {
		download.Status = "completed"
		download.Progress = 100.0
	})
	return path, nil
}

func downloadMegaInner(executable, rawURL, targetDir string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid Mega URL: %w", err)
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "mega.nz" && host != "mega.co.nz" {
		return "", fmt.Errorf("refusing non-Mega URL")
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", err
	}
	before, err := comicDirEntries(targetDir)
	if err != nil {
		return "", err
	}
	command := exec.Command(executable, "--path", targetDir, rawURL)
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("Mega downloader exited with error: %w", err)
	}
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if _, ok := before[entry.Name()]; !ok {
			return filepath.Join(targetDir, entry.Name()), nil
		}
	}
	return "", fmt.Errorf("Mega downloader completed without creating a file")
}

// ---------------------------------------------------------------------------
// Comics cycle.
// ---------------------------------------------------------------------------

// comicQueuedDownload is the result of one successfully started download.
type comicQueuedDownload struct {
	path   string
	method string
	hash   *string
}

func firstComicString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// RunComicsCycle implements `comics::run_cycle` (renamed to avoid the
// collision with the orchestrator's own run cycle).
func RunComicsCycle(db *ComicsDb, client *GetComicsClient, notifier *Notifier, defaultRoot string, torrents TorrentEngine, mainDB *Database, cfg *Config) (int, error) {
	monitored, err := db.ListMonitored(true)
	if err != nil {
		return 0, err
	}
	downloaded := 0
	logging.Info("comics cycle: checking monitored titles", "monitored", len(monitored))
	for _, comic := range monitored {
		logging.Info("comics: checking title", "comic", comic.Title, "tag", comic.TagURL)
		// A numbered monitor identifies one exact issue: its publication date
		// may be older than the generic monitoring start date, so that date
		// must not hide the requested issue.
		exactIssue := issueNumber(comic.Title) != nil
		tagFromDate := ""
		if !exactIssue {
			tagFromDate = comic.FromDate
		}
		// A result chosen by the search is a precise post: it must not be
		// replaced by a new generic search of the same name.
		var exactPostURL *string
		if strings.TrimSpace(comic.PostURL) != "" {
			value := comic.PostURL
			exactPostURL = &value
		} else if !strings.Contains(comic.TagURL, "/tag/") {
			// Compatibility with rows created by previous versions.
			value := comic.TagURL
			exactPostURL = &value
		}
		// Even with a stale (404) tag the name search finds the new issues,
		// like the legacy extto: merge the two sources.
		var posts []ComicPost
		var tagError *string
		if exactPostURL != nil {
			posts = []ComicPost{{
				Title:       comic.Title,
				URL:         *exactPostURL,
				TagURL:      comic.TagURL,
				CoverURL:    comic.CoverURL,
				Publisher:   comic.Publisher,
				Description: comic.Description,
				Date:        comic.FromDate,
			}}
		} else {
			fetched, fetchErr := client.TagPosts(comic.TagURL, tagFromDate)
			if fetchErr != nil {
				message := fetchErr.Error()
				tagError = &message
			} else {
				posts = fetched
			}
		}
		clean := cleanSearchTitle(comic.Title)
		var searchPosts []ComicPost
		if exactPostURL == nil && clean != "" {
			_, found, searchErr := client.SearchPosts(clean)
			if searchErr != nil {
				logging.Warn("comics: name search failed", "comic", comic.Title, "error", searchErr)
			} else {
				searchPosts = found
			}
		}
		if tagError != nil && len(searchPosts) > 0 {
			logging.Debug("comics: tag unavailable, using name search", "comic", comic.Title, "error", *tagError)
		} else if tagError != nil && len(searchPosts) == 0 {
			logging.Warn("comics tag fetch failed and name search returned nothing", "comic", comic.Title, "error", *tagError)
		}
		merged := map[string]ComicPost{}
		for _, post := range posts {
			if !matchesMonitoredIssue(comic.Title, post.Title) {
				logging.Debug("comics: ignoring post for a different issue or collection", "comic", comic.Title, "post", post.Title)
				continue
			}
			if !exactIssue && strings.TrimSpace(comic.FromDate) != "" && post.Date != "" && post.Date < comic.FromDate {
				continue
			}
			merged[post.URL] = post
		}
		for _, post := range searchPosts {
			if !matchesMonitoredIssue(comic.Title, post.Title) {
				logging.Debug("comics: ignoring post for a different issue or collection", "comic", comic.Title, "post", post.Title)
				continue
			}
			if !exactIssue && strings.TrimSpace(comic.FromDate) != "" && post.Date != "" && post.Date < comic.FromDate {
				continue
			}
			merged[post.URL] = post
		}
		keys := make([]string, 0, len(merged))
		for key := range merged {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		logging.Info("comics: candidate posts", "comic", comic.Title, "posts", len(keys))
		queued := 0
		for _, key := range keys {
			post := merged[key]
			alreadySent, err := db.AlreadySent(post.URL)
			if err != nil {
				return downloaded, err
			}
			if alreadySent {
				continue
			}
			links, err := client.Links(post.URL)
			if err != nil {
				logging.Warn("comics post fetch failed", "post", post.URL, "error", err)
				continue
			}
			if cfg.DryRun {
				logging.Info("dry-run: comic release discovered, download skipped", "title", post.Title)
				continue
			}
			target := comicDownloadDir(cfg)
			if target == nil {
				value := defaultRoot
				target = &value
			}
			if strings.TrimSpace(comic.SavePath) != "" {
				value := comic.SavePath
				target = &value
			}
			// A magnet honours a preferred path only when it is already a
			// directory: create it first so the "Comic" category is honoured.
			_ = os.MkdirAll(*target, 0o755)
			var result comicQueuedDownload
			var resultErr error
			if direct := firstComicString(links.Direct); direct != "" {
				path, err := client.DownloadDirect(direct, *target, post.Title)
				result = comicQueuedDownload{path: path, method: "http"}
				resultErr = err
			} else if mega := firstComicString(links.Mega); mega != "" {
				executable := os.Getenv("GEXTTO_MEGADL")
				if executable == "" {
					executable = "megadl"
				}
				resolved, err := client.ResolveMega(mega)
				if err != nil {
					resultErr = err
				} else {
					path, err := DownloadMega(executable, resolved, *target)
					result = comicQueuedDownload{path: path, method: "mega"}
					resultErr = err
				}
			} else if magnet := firstComicString(links.Magnets); magnet != "" {
				added, err := torrents.AddWithPath(magnet, cfg, target)
				switch {
				case err != nil:
					resultErr = err
				case !added:
					resultErr = fmt.Errorf("comic magnet is already queued")
				default:
					hash, ok := utils.MagnetHash(magnet)
					if !ok {
						resultErr = fmt.Errorf("comic magnet has no valid info hash")
						break
					}
					_ = mainDB.SetTorrentTag(hash, "Comic")
					_ = notifier.NotifyEvent("comic_queued", map[string]any{"title": post.Title, "magnet_hash": hash, "method": "torrent"})
					value := hash
					result = comicQueuedDownload{path: filepath.Join(*target, post.Title), method: "torrent", hash: &value}
				}
			} else if torrentURL := firstComicString(links.Torrents); torrentURL != "" {
				torrentDir := filepath.Join(*target, ".torrents")
				path, err := client.DownloadTorrent(torrentURL, torrentDir)
				if err != nil {
					resultErr = err
				} else {
					hash, err := torrents.AddTorrentFile(path, *target)
					switch {
					case err != nil:
						resultErr = err
					case hash == nil:
						resultErr = fmt.Errorf("torrent client unavailable")
					default:
						_ = os.Remove(path)
						_ = mainDB.SetTorrentTag(*hash, "Comic")
						_ = notifier.NotifyEvent("comic_queued", map[string]any{"title": post.Title, "torrent_url": torrentURL, "hash": *hash, "method": "torrent"})
						result = comicQueuedDownload{path: filepath.Join(*target, post.Title), method: "torrent", hash: hash}
					}
				}
			} else {
				logging.Info("comics post found but has no Download Now or torrent link yet; leaving it pending", "post", post.URL, "title", post.Title)
				_ = notifier.NotifyEvent("comic_pending", map[string]any{"title": post.Title, "post_url": post.URL, "kind": "comic"})
				continue
			}
			if resultErr != nil {
				logging.Warn("comic direct download failed", "post", post.URL, "error", resultErr)
				continue
			}
			added, err := db.AddHistory(comic.ID, post.URL, post.Title, firstComicString(links.Magnets), firstComicString(links.Torrents))
			if err != nil {
				return downloaded, err
			}
			if !added {
				continue
			}
			if result.hash != nil {
				if err := db.AddTorrent(*result.hash, post.URL, post.Title, *target); err != nil {
					return downloaded, err
				}
			}
			downloaded++
			queued++
			size := uint64(0)
			if info, statErr := os.Stat(result.path); statErr == nil {
				size = uint64(info.Size())
			}
			if err := notifier.NotifyComicComplete(post.Title, result.path, size, result.method); err != nil {
				logging.Warn("comic notification failed", "error", err)
			}
		}
		if err := db.MarkChecked(comic.ID); err != nil {
			return downloaded, err
		}
		logging.Info("comics: title checked", "comic", comic.Title, "queued", queued)
	}
	weeklyEnabled, err := db.Setting("weekly_enabled", "no")
	if err != nil {
		return downloaded, err
	}
	if weeklyEnabled == "yes" && !cfg.DryRun {
		weeklyFromDate, err := db.Setting("weekly_from_date", "")
		if err != nil {
			return downloaded, err
		}
		logging.Info("comics: checking weekly packs", "from_date", weeklyFromDate)
		// 1A. Retry packs that were recorded but never sent.
		pendingWeekly, _ := db.PendingWeekly()
		for _, pending := range pendingWeekly {
			accepted, err := sendWeeklyPack(db, client, torrents, notifier, defaultRoot, mainDB, cfg, pending.PackDate, pending.Magnet, pending.TorrentURL, "")
			if err != nil {
				logging.Warn("comics: pending weekly pack failed", "date", pending.PackDate, "error", err)
			} else if accepted {
				logging.Info("comics: pending weekly pack sent", "date", pending.PackDate)
			} else {
				logging.Debug("comics: pending weekly pack not accepted", "date", pending.PackDate)
			}
		}
		// 1B. Find new packs with a single search, like the legacy extto.
		weeklyChecked := 0
		if _, posts, err := client.SearchPosts("Weekly Pack"); err != nil {
			logging.Warn("comics: weekly pack search failed", "error", err)
		} else {
			for _, post := range posts {
				title := strings.ToLower(post.Title)
				if !strings.Contains(title, "weekly") || !strings.Contains(title, "pack") {
					continue
				}
				date := extractPackDate(post.Title)
				if date == "" {
					continue
				}
				if strings.TrimSpace(weeklyFromDate) != "" && date < weeklyFromDate {
					continue
				}
				weeklyChecked++
				links, err := client.Links(post.URL)
				if err != nil {
					logging.Debug("comics: weekly pack links failed", "date", date, "post", post.URL)
					continue
				}
				magnet := firstComicString(links.Magnets)
				torrentURL := firstComicString(links.Torrents)
				directURL := firstComicString(links.DownloadNow)
				if directURL == "" {
					directURL = firstComicString(links.Direct)
				}
				if magnet == "" && torrentURL == "" && directURL == "" {
					if _, err := db.UpsertWeeklyLinks(date, "", ""); err != nil {
						return downloaded, err
					}
					logging.Info("comics: weekly pack found without Download Now or torrent link yet", "date", date)
					_ = notifier.NotifyEvent("comic_pending", map[string]any{
						"title":    fmt.Sprintf("Weekly Pack %s", date),
						"post_url": post.URL,
						"kind":     "weekly",
					})
					continue
				}
				eligible, err := db.UpsertWeeklyLinks(date, magnet, torrentURL)
				if err != nil {
					return downloaded, err
				}
				sent, err := db.WeeklySent(date)
				if err != nil {
					return downloaded, err
				}
				shouldSend := !sent && (eligible || directURL != "")
				logging.Info("comics: weekly pack recorded", "date", date, "eligible", shouldSend)
				if !shouldSend {
					continue
				}
				accepted, err := sendWeeklyPack(db, client, torrents, notifier, defaultRoot, mainDB, cfg, date, magnet, torrentURL, directURL)
				if err != nil {
					logging.Warn("comics: weekly pack failed", "date", date, "error", err)
				} else if accepted {
					logging.Info("comics: weekly pack queued", "date", date)
					break
				} else {
					logging.Debug("comics: weekly pack not accepted", "date", date)
				}
			}
		}
		logging.Info("comics: weekly check done", "checked", weeklyChecked)
	} else if weeklyEnabled != "yes" {
		logging.Info("comics: weekly packs disabled")
	}
	return downloaded, nil
}

// sendWeeklyPack sends a weekly pack (direct file, magnet or `.torrent`) to the
// client. It returns true when it was accepted and marked as sent.
func sendWeeklyPack(db *ComicsDb, client *GetComicsClient, torrents TorrentEngine, notifier *Notifier, defaultRoot string, mainDB *Database, cfg *Config, date, magnet, torrentURL, directURL string) (bool, error) {
	target := comicDownloadDir(cfg)
	if target == nil {
		value := defaultRoot
		target = &value
	}
	_ = os.MkdirAll(*target, 0o755)
	if directURL != "" {
		title := fmt.Sprintf("Weekly Pack %s", date)
		path, err := client.DownloadDirect(directURL, *target, title)
		if err != nil {
			return false, err
		}
		if err := db.MarkWeeklySent(date); err != nil {
			return false, err
		}
		size := uint64(0)
		if info, statErr := os.Stat(path); statErr == nil {
			size = uint64(info.Size())
		}
		_ = notifier.NotifyComicComplete(title, path, size, "http")
		return true, nil
	}
	if magnet != "" {
		hash, ok := utils.MagnetHash(magnet)
		if ok {
			added, err := torrents.AddWithPath(magnet, cfg, target)
			if err != nil {
				return false, err
			}
			if added {
				title := fmt.Sprintf("Weekly Pack %s", date)
				_ = mainDB.SetTorrentTag(hash, "Comic")
				if err := db.AddTorrent(hash, fmt.Sprintf("weekly:%s", date), title, *target); err != nil {
					return false, err
				}
				if err := db.MarkWeeklySent(date); err != nil {
					return false, err
				}
				_ = notifier.NotifyEvent("comic_queued", map[string]any{"title": title, "hash": hash, "method": "torrent"})
				return true, nil
			}
		}
	} else if torrentURL != "" {
		title := fmt.Sprintf("Weekly Pack %s", date)
		torrentDir := filepath.Join(*target, ".torrents")
		if path, err := client.DownloadTorrent(torrentURL, torrentDir); err == nil {
			if hash, err := torrents.AddTorrentFile(path, *target); err == nil && hash != nil {
				_ = os.Remove(path)
				_ = mainDB.SetTorrentTag(*hash, "Comic")
				if err := db.AddTorrent(*hash, fmt.Sprintf("weekly:%s", date), title, *target); err != nil {
					return false, err
				}
				if err := db.MarkWeeklySent(date); err != nil {
					return false, err
				}
				_ = notifier.NotifyEvent("comic_queued", map[string]any{"title": title, "hash": *hash, "method": "torrent"})
				return true, nil
			}
		}
	}
	return false, nil
}

// ---------------------------------------------------------------------------
// GetComics category directories (implementation of `postprocess::{category_dirs,
// category_download_dir, comic_download_dir}`).
// ---------------------------------------------------------------------------

func comicCategoryDirs(cfg *Config, category string) (*string, *string) {
	if cfg == nil || cfg.Settings == nil {
		return nil, nil
	}
	raw, ok := cfg.Settings["tag_dir_rules"]
	if !ok {
		return nil, nil
	}
	var rules []map[string]any
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return nil, nil
	}
	for _, rule := range rules {
		tag, _ := rule["tag"].(string)
		if !strings.EqualFold(strings.TrimSpace(tag), category) {
			continue
		}
		var temporary, finalDir *string
		if value, ok := rule["temp_dir"].(string); ok {
			trimmed := strings.TrimSpace(value)
			if trimmed != "" {
				if info, err := os.Stat(trimmed); err == nil && info.IsDir() {
					v := trimmed
					temporary = &v
				}
			}
		}
		if value, ok := rule["final_dir"].(string); ok {
			trimmed := strings.TrimSpace(value)
			if trimmed != "" {
				v := trimmed
				finalDir = &v
			}
		}
		return temporary, finalDir
	}
	return nil, nil
}

func comicCategoryDownloadDir(cfg *Config, category string) *string {
	temporary, finalDir := comicCategoryDirs(cfg, category)
	if finalDir != nil {
		return finalDir
	}
	return temporary
}

// comicDownloadDir accepts both "Comic" and "Fumetto" as the category name.
func comicDownloadDir(cfg *Config) *string {
	if dir := comicCategoryDownloadDir(cfg, "Comic"); dir != nil {
		return dir
	}
	return comicCategoryDownloadDir(cfg, "Fumetto")
}

// ---------------------------------------------------------------------------
// Import from extto.
// ---------------------------------------------------------------------------

// ImportFromExtto imports the comics data of an extto source directory into the
// destination database. `source` is the directory containing `comics.db`.
func ImportFromExtto(source string, destination *sql.DB) (ComicsImportReport, error) {
	report := ComicsImportReport{}
	if err := EnsureComicsSchema(destination); err != nil {
		return report, err
	}
	sourceDB := filepath.Join(source, "comics.db")
	if _, err := os.Stat(sourceDB); err != nil {
		return report, nil
	}
	sourceConn, err := sql.Open("sqlite", "file:"+sourceDB+"?mode=ro")
	if err != nil {
		return report, fmt.Errorf("open source database %s: %w", sourceDB, err)
	}
	sourceConn.SetMaxOpenConns(1)
	defer sourceConn.Close()
	if err := sourceConn.Ping(); err != nil {
		return report, fmt.Errorf("open source database %s: %w", sourceDB, err)
	}
	tx, err := destination.Begin()
	if err != nil {
		return report, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	idMap := map[int64]int64{}

	if present, err := comicsHasTable(sourceConn, "comics_monitored"); err != nil {
		return report, err
	} else if present {
		rows, err := sourceConn.Query("SELECT id,title,tag_url,cover_url,publisher,description,from_date,save_path,enabled,added_at,last_checked FROM comics_monitored")
		if err != nil {
			return report, err
		}
		for rows.Next() {
			var oldID int64
			var title, tagURL, fromDate string
			var cover, publisher, description, savePath, addedAt, lastChecked sql.NullString
			var enabled sql.NullInt64
			if err := rows.Scan(&oldID, &title, &tagURL, &cover, &publisher, &description, &fromDate, &savePath, &enabled, &addedAt, &lastChecked); err != nil {
				rows.Close()
				return report, err
			}
			enabledValue := int64(1)
			if enabled.Valid {
				enabledValue = enabled.Int64
			}
			result, err := tx.Exec(
				"INSERT OR IGNORE INTO comics_monitored(id,title,tag_url,cover_url,publisher,description,from_date,save_path,enabled,added_at,last_checked) VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,?11)",
				oldID, title, tagURL, cover.String, publisher.String, description.String, fromDate, savePath.String, enabledValue, nullableComicString(addedAt), nullableComicString(lastChecked),
			)
			if err != nil {
				rows.Close()
				return report, err
			}
			var inserted int64
			if count, err := result.RowsAffected(); err == nil {
				inserted = count
			}
			var newID int64
			if err := tx.QueryRow("SELECT id FROM comics_monitored WHERE tag_url=?1", tagURL).Scan(&newID); err == nil {
				idMap[oldID] = newID
				if inserted > 0 {
					report.Monitored++
				}
			} else {
				report.Skipped++
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return report, err
		}
		rows.Close()
	}

	if present, err := comicsHasTable(sourceConn, "comics_history"); err != nil {
		return report, err
	} else if present {
		rows, err := sourceConn.Query("SELECT id,monitored_id,post_url,title,magnet,torrent_url,sent_at,size_bytes FROM comics_history")
		if err != nil {
			return report, err
		}
		for rows.Next() {
			var id, oldMonitored int64
			var postURL, title string
			var magnet, torrentURL, sentAt sql.NullString
			var size sql.NullInt64
			if err := rows.Scan(&id, &oldMonitored, &postURL, &title, &magnet, &torrentURL, &sentAt, &size); err != nil {
				rows.Close()
				return report, err
			}
			monitoredID, ok := idMap[oldMonitored]
			if !ok {
				report.Skipped++
				continue
			}
			sizeValue := int64(0)
			if size.Valid {
				sizeValue = size.Int64
			}
			result, err := tx.Exec(
				"INSERT OR IGNORE INTO comics_history(id,monitored_id,post_url,title,magnet,torrent_url,sent_at,size_bytes) VALUES (?1,?2,?3,?4,?5,?6,?7,?8)",
				id, monitoredID, postURL, title, magnet.String, torrentURL.String, nullableComicString(sentAt), sizeValue,
			)
			if err != nil {
				rows.Close()
				return report, err
			}
			var inserted int64
			if count, err := result.RowsAffected(); err == nil {
				inserted = count
			}
			if inserted > 0 {
				report.History++
			} else {
				report.Skipped++
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return report, err
		}
		rows.Close()
	}

	if present, err := comicsHasTable(sourceConn, "comics_weekly"); err != nil {
		return report, err
	} else if present {
		rows, err := sourceConn.Query("SELECT id,pack_date,magnet,torrent_url,sent_at,found_at,size_bytes FROM comics_weekly")
		if err != nil {
			return report, err
		}
		for rows.Next() {
			var id int64
			var date string
			var magnet, torrentURL, sentAt, foundAt sql.NullString
			var size sql.NullInt64
			if err := rows.Scan(&id, &date, &magnet, &torrentURL, &sentAt, &foundAt, &size); err != nil {
				rows.Close()
				return report, err
			}
			sizeValue := int64(0)
			if size.Valid {
				sizeValue = size.Int64
			}
			result, err := tx.Exec(
				"INSERT OR IGNORE INTO comics_weekly(id,pack_date,magnet,torrent_url,sent_at,found_at,size_bytes) VALUES (?1,?2,?3,?4,?5,?6,?7)",
				id, date, magnet.String, torrentURL.String, nullableComicString(sentAt), nullableComicString(foundAt), sizeValue,
			)
			if err != nil {
				rows.Close()
				return report, err
			}
			var inserted int64
			if count, err := result.RowsAffected(); err == nil {
				inserted = count
			}
			if inserted > 0 {
				report.Weekly++
			} else {
				report.Skipped++
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return report, err
		}
		rows.Close()
	}

	if present, err := comicsHasTable(sourceConn, "comics_settings"); err != nil {
		return report, err
	} else if present {
		rows, err := sourceConn.Query("SELECT key,value FROM comics_settings")
		if err != nil {
			return report, err
		}
		for rows.Next() {
			var key, value string
			if err := rows.Scan(&key, &value); err != nil {
				rows.Close()
				return report, err
			}
			result, err := tx.Exec("INSERT OR IGNORE INTO comics_settings(key,value) VALUES (?1,?2)", key, value)
			if err != nil {
				rows.Close()
				return report, err
			}
			var inserted int64
			if count, err := result.RowsAffected(); err == nil {
				inserted = count
			}
			if inserted > 0 {
				report.Settings++
			} else {
				report.Skipped++
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return report, err
		}
		rows.Close()
	}

	if err := tx.Commit(); err != nil {
		return report, err
	}
	committed = true
	return report, nil
}

func comicsHasTable(db *sql.DB, name string) (bool, error) {
	var exists bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?1)", name).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func nullableComicString(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

// ---------------------------------------------------------------------------
// Small text helpers (implementation of the private functions ).
// ---------------------------------------------------------------------------

var comicISODatePattern = utils.MustCachedRegex(`(\d{4}-\d{2}-\d{2})`)
var comicPackDatePattern = utils.MustCachedRegex(`(\d{4})[.\-](\d{2})[.\-](\d{2})`)

// extractPackDate returns the canonical `YYYY-MM-DD` date found in a weekly pack
// title.
func extractPackDate(title string) string {
	captures := comicPackDatePattern.FindStringSubmatch(title)
	if len(captures) < 4 {
		return ""
	}
	return fmt.Sprintf("%s-%s-%s", captures[1], captures[2], captures[3])
}

// cleanSearchTitle removes the issue number and the year, like the legacy extto.
func cleanSearchTitle(title string) string {
	base := title
	if index := strings.Index(title, "#"); index >= 0 {
		base = title[:index]
	}
	base = strings.TrimSpace(base)
	withoutYear := base
	if strings.HasSuffix(base, ")") {
		if index := strings.LastIndex(base, "("); index >= 0 {
			year := base[index+1 : len(base)-1]
			if len(year) == 4 && allASCIIDigits(year) {
				withoutYear = strings.TrimSpace(base[:index])
			}
		}
	}
	var builder strings.Builder
	for _, r := range withoutYear {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ' ' {
			builder.WriteRune(r)
		} else {
			builder.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(builder.String()), " ")
}

// issueNumber extracts the issue number from titles such as `Poison Ivy #41`.
func issueNumber(title string) *string {
	index := strings.Index(title, "#")
	if index < 0 {
		return nil
	}
	after := strings.TrimLeftFunc(title[index+1:], unicode.IsSpace)
	var builder strings.Builder
	for _, r := range after {
		if r < '0' || r > '9' {
			break
		}
		builder.WriteRune(r)
	}
	digits := builder.String()
	if digits == "" {
		return nil
	}
	return &digits
}

func matchesMonitoredIssue(monitoredTitle, postTitle string) bool {
	monitored := issueNumber(monitoredTitle)
	post := issueNumber(postTitle)
	if monitored != nil && post != nil {
		return *monitored == *post
	}
	if monitored != nil && post == nil {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Minimal HTML tree builder and CSS selector matching.
//
// The original used `scraper` (html5ever + selectors). This port implements
// only the small subset needed by the GetComics parsing functions.
// ---------------------------------------------------------------------------

type htmlNode struct {
	tag      string
	attrs    map[string]string
	children []*htmlNode
	parent   *htmlNode
	text     string
}

func isHTMLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func isVoidHTMLElement(name string) bool {
	switch name {
	case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr":
		return true
	default:
		return false
	}
}

func isRawTextHTMLElement(name string) bool {
	switch name {
	case "script", "style", "textarea", "title":
		return true
	default:
		return false
	}
}

func findHTMLTagEnd(html string, start int) int {
	var quote byte
	for i := start + 1; i < len(html); i++ {
		c := html[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if c == '>' {
			return i
		}
	}
	return len(html)
}

func parseHTMLStartTag(content string) (string, map[string]string) {
	attrs := map[string]string{}
	i := 0
	n := len(content)
	for i < n && !isHTMLSpace(content[i]) {
		i++
	}
	name := strings.ToLower(content[:i])
	for i < n {
		for i < n && isHTMLSpace(content[i]) {
			i++
		}
		if i >= n {
			break
		}
		start := i
		for i < n && !isHTMLSpace(content[i]) && content[i] != '=' {
			i++
		}
		key := strings.ToLower(content[start:i])
		value := ""
		for i < n && isHTMLSpace(content[i]) {
			i++
		}
		if i < n && content[i] == '=' {
			i++
			for i < n && isHTMLSpace(content[i]) {
				i++
			}
			if i < n && (content[i] == '"' || content[i] == '\'') {
				quote := content[i]
				i++
				start = i
				for i < n && content[i] != quote {
					i++
				}
				value = content[start:i]
				if i < n {
					i++
				}
			} else {
				start = i
				for i < n && !isHTMLSpace(content[i]) {
					i++
				}
				value = content[start:i]
			}
		}
		if key != "" {
			attrs[key] = value
		}
	}
	return name, attrs
}

func parseHTML(html string) *htmlNode {
	root := &htmlNode{tag: "#root", attrs: map[string]string{}}
	stack := []*htmlNode{root}
	i := 0
	for i < len(html) {
		if html[i] != '<' {
			next := strings.IndexByte(html[i:], '<')
			if next < 0 {
				next = len(html) - i
			}
			text := html[i : i+next]
			if text != "" {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, &htmlNode{text: text, parent: parent})
			}
			i += next
			continue
		}
		if strings.HasPrefix(html[i:], "<!--") {
			end := strings.Index(html[i+4:], "-->")
			if end < 0 {
				break
			}
			i += 4 + end + 3
			continue
		}
		if strings.HasPrefix(html[i:], "<!") || strings.HasPrefix(html[i:], "<?") {
			end := strings.IndexByte(html[i:], '>')
			if end < 0 {
				break
			}
			i += end + 1
			continue
		}
		if strings.HasPrefix(html[i:], "</") {
			end := strings.IndexByte(html[i:], '>')
			if end < 0 {
				break
			}
			name := strings.ToLower(strings.TrimSpace(html[i+2 : i+end]))
			for index := len(stack) - 1; index >= 1; index-- {
				if stack[index].tag == name {
					stack = stack[:index]
					break
				}
			}
			i += end + 1
			continue
		}
		end := findHTMLTagEnd(html, i)
		content := strings.TrimSpace(html[i+1 : end])
		selfClosing := strings.HasSuffix(content, "/")
		if selfClosing {
			content = strings.TrimSpace(strings.TrimSuffix(content, "/"))
		}
		name, attrs := parseHTMLStartTag(content)
		i = end + 1
		if name == "" {
			continue
		}
		node := &htmlNode{tag: name, attrs: attrs}
		parent := stack[len(stack)-1]
		node.parent = parent
		parent.children = append(parent.children, node)
		if selfClosing || isVoidHTMLElement(name) {
			continue
		}
		stack = append(stack, node)
		if isRawTextHTMLElement(name) {
			closing := "</" + name
			lower := strings.ToLower(html[i:])
			if index := strings.Index(lower, closing); index >= 0 {
				if index > 0 {
					node.children = append(node.children, &htmlNode{text: html[i : i+index], parent: node})
				}
				closeEnd := strings.IndexByte(html[i+index:], '>')
				if closeEnd < 0 {
					i = len(html)
				} else {
					i += index + closeEnd + 1
				}
				stack = stack[:len(stack)-1]
			}
		}
	}
	return root
}

func textContentHTML(node *htmlNode) string {
	if node == nil {
		return ""
	}
	if node.tag == "" {
		return node.text
	}
	var builder strings.Builder
	var walk func(*htmlNode)
	walk = func(current *htmlNode) {
		if current.tag == "" {
			builder.WriteString(current.text)
			return
		}
		for _, child := range current.children {
			walk(child)
		}
	}
	walk(node)
	return builder.String()
}

type cssAttr struct {
	name  string
	op    string
	value string
}

type cssCompound struct {
	tag     string
	id      string
	classes []string
	attrs   []cssAttr
}

type cssGroup struct {
	compounds []cssCompound
}

func parseCSSCompound(input string) cssCompound {
	var compound cssCompound
	i := 0
	for i < len(input) && input[i] != '.' && input[i] != '#' && input[i] != '[' {
		i++
	}
	compound.tag = strings.ToLower(input[:i])
	for i < len(input) {
		switch input[i] {
		case '.':
			i++
			start := i
			for i < len(input) && input[i] != '.' && input[i] != '#' && input[i] != '[' {
				i++
			}
			compound.classes = append(compound.classes, input[start:i])
		case '#':
			i++
			start := i
			for i < len(input) && input[i] != '.' && input[i] != '#' && input[i] != '[' {
				i++
			}
			compound.id = input[start:i]
		case '[':
			i++
			start := i
			for i < len(input) && input[i] != ']' {
				i++
			}
			inner := input[start:i]
			if i < len(input) {
				i++
			}
			var name, op, value string
			if index := strings.IndexAny(inner, "=*^$~|"); index >= 0 {
				name = strings.TrimSpace(inner[:index])
				rest := inner[index:]
				switch {
				case strings.HasPrefix(rest, "*="):
					op, rest = "*=", rest[2:]
				case strings.HasPrefix(rest, "^="):
					op, rest = "^=", rest[2:]
				case strings.HasPrefix(rest, "$="):
					op, rest = "$=", rest[2:]
				case strings.HasPrefix(rest, "~="):
					op, rest = "~=", rest[2:]
				case strings.HasPrefix(rest, "|="):
					op, rest = "|=", rest[2:]
				case strings.HasPrefix(rest, "="):
					op, rest = "=", rest[1:]
				}
				value = strings.Trim(rest, `"'`)
			} else {
				name = strings.TrimSpace(inner)
			}
			compound.attrs = append(compound.attrs, cssAttr{name: strings.ToLower(name), op: op, value: value})
		default:
			i++
		}
	}
	return compound
}

func parseComicSelector(selector string) []cssGroup {
	groups := []cssGroup{}
	for _, rawGroup := range strings.Split(selector, ",") {
		rawGroup = strings.TrimSpace(rawGroup)
		if rawGroup == "" {
			continue
		}
		compounds := []cssCompound{}
		for _, rawCompound := range strings.Fields(rawGroup) {
			compounds = append(compounds, parseCSSCompound(rawCompound))
		}
		if len(compounds) > 0 {
			groups = append(groups, cssGroup{compounds: compounds})
		}
	}
	return groups
}

func comicHTMLHasClass(node *htmlNode, class string) bool {
	value, ok := node.attrs["class"]
	if !ok {
		return false
	}
	for _, candidate := range strings.Fields(value) {
		if candidate == class {
			return true
		}
	}
	return false
}

func matchCSSCompound(node *htmlNode, compound cssCompound) bool {
	if node == nil || node.tag == "" {
		return false
	}
	if compound.tag != "" && node.tag != compound.tag {
		return false
	}
	if compound.id != "" && node.attrs["id"] != compound.id {
		return false
	}
	for _, class := range compound.classes {
		if !comicHTMLHasClass(node, class) {
			return false
		}
	}
	for _, attr := range compound.attrs {
		value, ok := node.attrs[attr.name]
		if !ok {
			return false
		}
		switch attr.op {
		case "":
		case "=":
			if value != attr.value {
				return false
			}
		case "*=":
			if !strings.Contains(value, attr.value) {
				return false
			}
		case "^=":
			if !strings.HasPrefix(value, attr.value) {
				return false
			}
		case "$=":
			if !strings.HasSuffix(value, attr.value) {
				return false
			}
		case "~=":
			found := false
			for _, field := range strings.Fields(value) {
				if field == attr.value {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}

func matchCSSGroup(node *htmlNode, group cssGroup) bool {
	if len(group.compounds) == 0 {
		return false
	}
	if !matchCSSCompound(node, group.compounds[len(group.compounds)-1]) {
		return false
	}
	index := len(group.compounds) - 2
	for ancestor := node.parent; ancestor != nil && index >= 0; ancestor = ancestor.parent {
		if matchCSSCompound(ancestor, group.compounds[index]) {
			index--
		}
	}
	return index < 0
}

func selectHTMLNodes(root *htmlNode, groups []cssGroup) []*htmlNode {
	results := []*htmlNode{}
	var walk func(*htmlNode)
	walk = func(node *htmlNode) {
		if node != root && node.tag != "" {
			for _, group := range groups {
				if matchCSSGroup(node, group) {
					results = append(results, node)
					break
				}
			}
		}
		for _, child := range node.children {
			if child.tag != "" {
				walk(child)
			}
		}
	}
	walk(root)
	return results
}

var (
	comicSelectorArticle     = parseComicSelector("article.post")
	comicSelectorTitle       = parseComicSelector("h1.post-title a, h2.post-title a, .post-title a")
	comicSelectorImage       = parseComicSelector("img.wp-post-image, .post-img img, img[src]")
	comicSelectorDate        = parseComicSelector("time[datetime], .post-date, .entry-date, .published")
	comicSelectorTag         = parseComicSelector("a[href*='/tag/']")
	comicSelectorPublisher   = parseComicSelector("a[href*='/cat/'], a[href*='/tag/']")
	comicSelectorDescription = parseComicSelector(".post-info p, .entry-content p")
	comicSelectorAnchor      = parseComicSelector("a[href]")
)

func resolveComicURL(pageURL, raw string) (string, error) {
	base, err := url.Parse(pageURL)
	if err != nil {
		return "", err
	}
	reference, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(reference).String(), nil
}

func extractComicTagURL(article *htmlNode, pageURL, title string) string {
	var slugBuilder strings.Builder
	for _, r := range strings.ToLower(title) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			slugBuilder.WriteRune(r)
		} else {
			slugBuilder.WriteByte('-')
		}
	}
	slug := slugBuilder.String()
	fallback := ""
	for _, anchor := range selectHTMLNodes(article, comicSelectorTag) {
		href, ok := anchor.attrs["href"]
		if !ok {
			continue
		}
		joined, err := resolveComicURL(pageURL, href)
		if err != nil {
			continue
		}
		if fallback == "" {
			fallback = joined
		}
		parsed, err := url.Parse(joined)
		if err == nil {
			segments := strings.Split(parsed.Path, "/")
			if len(segments) > 0 && segments[0] == "" {
				segments = segments[1:]
			}
			for _, segment := range segments {
				if strings.Contains(slug, segment) {
					return joined
				}
			}
		}
	}
	if fallback != "" {
		return fallback
	}
	words := []string{}
	for _, word := range strings.Split(slug, "-") {
		if word != "" {
			words = append(words, word)
		}
	}
	if len(words) > 0 {
		last := words[len(words)-1]
		if len(last) == 4 && allASCIIDigits(last) {
			words = words[:len(words)-1]
		}
	}
	if len(words) > 0 {
		return "https://getcomics.org/tag/" + strings.Join(words, "-") + "/"
	}
	return ""
}

func extractComicPublisher(article *htmlNode) string {
	known := []struct {
		needle    string
		publisher string
	}{
		{"dc", "DC Comics"},
		{"marvel", "Marvel"},
		{"image", "Image Comics"},
		{"dark-horse", "Dark Horse"},
		{"idw", "IDW"},
		{"dynamite", "Dynamite"},
		{"boom", "BOOM! Studios"},
	}
	for _, anchor := range selectHTMLNodes(article, comicSelectorPublisher) {
		value := strings.ToLower(anchor.attrs["href"] + " " + textContentHTML(anchor))
		for _, candidate := range known {
			if strings.Contains(value, candidate.needle) {
				return candidate.publisher
			}
		}
	}
	return ""
}

func extractComicDescription(article *htmlNode) string {
	elements := selectHTMLNodes(article, comicSelectorDescription)
	if len(elements) == 0 {
		return ""
	}
	text := strings.TrimSpace(textContentHTML(elements[0]))
	runes := []rune(text)
	if len(runes) > 300 {
		runes = runes[:300]
	}
	return string(runes)
}

func parseComicArticles(html, fromDate, pageURL string) []ComicPost {
	root := parseHTML(html)
	articles := selectHTMLNodes(root, comicSelectorArticle)
	posts := []ComicPost{}
	for _, article := range articles {
		titles := selectHTMLNodes(article, comicSelectorTitle)
		if len(titles) == 0 {
			continue
		}
		titleElement := titles[0]
		title := strings.TrimSpace(textContentHTML(titleElement))
		href, ok := titleElement.attrs["href"]
		if !ok {
			continue
		}
		postURL, err := resolveComicURL(pageURL, href)
		if err != nil {
			continue
		}
		coverURL := ""
		images := selectHTMLNodes(article, comicSelectorImage)
		if len(images) > 0 {
			var raw *string
			if value, present := images[0].attrs["src"]; present {
				raw = &value
			} else if value, present := images[0].attrs["data-src"]; present {
				raw = &value
			}
			if raw != nil {
				if resolved, err := resolveComicURL(pageURL, *raw); err == nil {
					coverURL = resolved
				}
			}
		}
		date := ""
		dates := selectHTMLNodes(article, comicSelectorDate)
		if len(dates) > 0 {
			raw := ""
			if value, present := dates[0].attrs["datetime"]; present {
				raw = value
			} else {
				raw = strings.TrimSpace(textContentHTML(dates[0]))
			}
			if captures := comicISODatePattern.FindStringSubmatch(raw); len(captures) > 1 {
				date = captures[1]
			}
		}
		if fromDate != "" && date != "" && date < fromDate {
			continue
		}
		posts = append(posts, ComicPost{
			Title:       title,
			URL:         postURL,
			TagURL:      extractComicTagURL(article, pageURL, title),
			CoverURL:    coverURL,
			Publisher:   extractComicPublisher(article),
			Description: extractComicDescription(article),
			Date:        date,
		})
	}
	return posts
}

func parseComicLinks(html, pageURL string) (ComicLinks, error) {
	root := parseHTML(html)
	links := ComicLinks{
		Magnets:     []string{},
		Torrents:    []string{},
		Mega:        []string{},
		Direct:      []string{},
		DownloadNow: []string{},
	}
	for _, anchor := range selectHTMLNodes(root, comicSelectorAnchor) {
		raw, ok := anchor.attrs["href"]
		if !ok {
			continue
		}
		// GetComics sometimes renders a decorative "Download Now" anchor with an
		// empty href before the real link: joining an empty href to the post URL
		// would turn the post page itself into a download candidate.
		if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "#" {
			continue
		}
		// A single invalid href must not fail the parsing of the whole page.
		joined, err := resolveComicURL(pageURL, raw)
		if err != nil {
			continue
		}
		text := strings.ToLower(textContentHTML(anchor))
		normalizedText := strings.NewReplacer("-", " ", "_", " ").Replace(text)
		titleAttr := strings.ToLower(anchor.attrs["title"])
		isDownloadNow := strings.Contains(normalizedText, "download now") ||
			strings.Contains(strings.NewReplacer("-", " ", "_", " ").Replace(titleAttr), "download now")
		parsed, _ := url.Parse(joined)
		path := ""
		host := ""
		if parsed != nil {
			path = strings.ToLower(parsed.Path)
			host = strings.ToLower(parsed.Hostname())
		}
		// The help link contains "download" but is not a file.
		if strings.Contains(path, "/how-to-download") || strings.Contains(normalizedText, "how to") {
			continue
		}
		lowerHref := strings.ToLower(joined)
		switch {
		case strings.HasPrefix(joined, "magnet:"):
			if !containsComicString(links.Magnets, joined) {
				links.Magnets = append(links.Magnets, joined)
			}
		case strings.Contains(lowerHref, "mega.nz") || strings.Contains(lowerHref, "mega.co.nz") || strings.Contains(text, "mega"):
			if !containsComicString(links.Mega, joined) {
				links.Mega = append(links.Mega, joined)
			}
		case strings.HasSuffix(lowerHref, ".torrent"):
			if !containsComicString(links.Torrents, joined) {
				links.Torrents = append(links.Torrents, joined)
			}
		case ((strings.HasPrefix(path, "/dls/") && (host == "getcomics.org" || host == "www.getcomics.org")) ||
			hasComicFileExtension(path) ||
			strings.Contains(text, "download") ||
			isDownloadNow) && !containsComicString(links.Direct, joined):
			if isDownloadNow && !containsComicString(links.DownloadNow, joined) {
				links.DownloadNow = append(links.DownloadNow, joined)
			}
			links.Direct = append(links.Direct, joined)
		}
	}
	return links, nil
}

func containsComicString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func hasComicFileExtension(path string) bool {
	for _, extension := range []string{".cbr", ".cbz", ".rar", ".zip"} {
		if strings.HasSuffix(path, extension) {
			return true
		}
	}
	return false
}
