package tui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// Torrent mirrors the JSON returned by /api/torrents.
type Torrent struct {
	Hash            string   `json:"hash"`
	Name            string   `json:"name"`
	State           string   `json:"state"`
	Progress        float64  `json:"progress"`
	TotalSize       uint64   `json:"total_size"`
	TotalDone       uint64   `json:"total_done"`
	AllTimeDownload uint64   `json:"all_time_download"`
	AllTimeUpload   uint64   `json:"all_time_upload"`
	DownloadRate    uint64   `json:"download_rate"`
	UploadRate      uint64   `json:"upload_rate"`
	NumPeers        int64    `json:"num_peers"`
	NumSeeds        int64    `json:"num_seeds"`
	QueuePosition   int64    `json:"queue_position"`
	SeedRatio       *float64 `json:"seed_ratio"`
	SeedDays        *float64 `json:"seed_days"`
	HasMetadata     bool     `json:"has_metadata"`
	AutoManaged     bool     `json:"auto_managed"`
	IsSeeding       bool     `json:"is_seeding"`
	Archived        bool     `json:"archived"`
	SavePath        string   `json:"save_path"`
	CurrentTracker  string   `json:"current_tracker"`
	Source          string   `json:"source"`
	Reason          string   `json:"reason"`
	TorrentVersion  string   `json:"torrent_version"`
	Error           string   `json:"error"`
	DownloadLimit   int64    `json:"download_limit"`
	UploadLimit     int64    `json:"upload_limit"`
	SuperSeeding    bool     `json:"super_seeding"`
	// Diagnosis is the daemon's short code for the torrent's situation
	// (dead_swarm, no_connected_seed, stalled, no_peers, metadata, ...).
	Diagnosis string `json:"diagnosis"`
	// StalledSince and NextRetryAt (RFC 3339) are set while the stall monitor
	// keeps the torrent set aside.
	StalledSince string `json:"stalled_since"`
	NextRetryAt  string `json:"next_retry_at"`
}

// ComicDownload mirrors one live HTTP/MEGA comic download.
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
	Tag             string  `json:"tag"`
}

// SeriesLibraryItem is the compact row returned by /api/series.
type SeriesLibraryItem struct {
	Name        string   `json:"name"`
	Seasons     string   `json:"seasons"`
	Quality     string   `json:"quality"`
	Language    string   `json:"language"`
	ArchivePath string   `json:"archive_path"`
	Enabled     bool     `json:"enabled"`
	Aliases     []string `json:"aliases"`
	// Present only in /api/config/library.
	EpisodesTotal      int64   `json:"episodes_total"`
	EpisodesDownloaded int64   `json:"episodes_downloaded"`
	LastDownloadedAt   *string `json:"last_downloaded_at"`
	TmdbStatus         string  `json:"tmdb_status"`
}

// MovieLibraryItem is the compact row returned by /api/movies.
type MovieLibraryItem struct {
	ID                   int64  `json:"id"`
	Name                 string `json:"name"`
	Year                 string `json:"year"`
	Quality              string `json:"quality"`
	Language             string `json:"language"`
	LanguageRequirements string `json:"language_requirements"`
	Enabled              bool   `json:"enabled"`
}

// ComicLibraryItem is the compact row returned by /api/comics.
type ComicLibraryItem struct {
	ID                    int64  `json:"id"`
	Title                 string `json:"title"`
	Publisher             string `json:"publisher"`
	FromDate              string `json:"from_date"`
	LatestDownloadedTitle string `json:"latest_downloaded_title"`
	Enabled               bool   `json:"enabled"`
}

// TorrentDetail is the response of /api/torrents/{hash}.
type TorrentDetail struct {
	OK       bool    `json:"ok"`
	Magnet   string  `json:"magnet"`
	NoRename bool    `json:"no_rename"`
	Torrent  Torrent `json:"torrent"`
}

// TorrentStats is the summary block of /api/status.
type TorrentStats struct {
	Count       int64 `json:"count"`
	Downloading int64 `json:"downloading"`
	Queued      int64 `json:"queued"`
	Seeding     int64 `json:"seeding"`
	Stalled     int64 `json:"stalled"`
}

// LastCycle is the last cycle summary of /api/status.
type LastCycle struct {
	Scraped          int64   `json:"scraped"`
	Candidates       int64   `json:"candidates"`
	DownloadsStarted int64   `json:"downloads_started"`
	GapsFilled       int64   `json:"gaps_filled"`
	Errors           int64   `json:"errors"`
	LastStartedAt    *string `json:"last_started_at"`
}

// Seen counts feed entries.
type Seen struct {
	Movies int64 `json:"movies"`
	Series int64 `json:"series"`
	Groups int64 `json:"groups"`
}

// Status mirrors /api/status.
type Status struct {
	Name         string       `json:"name"`
	Version      string       `json:"version"`
	Active       bool         `json:"active"`
	DryRun       bool         `json:"dry_run"`
	NextCycleAt  *string      `json:"next_cycle_at"`
	TorrentStats TorrentStats `json:"torrent_stats"`
	LastCycle    LastCycle    `json:"last_cycle"`
	Seen         Seen         `json:"seen"`
}

// ConsumptionStats mirrors the aggregate transfer consumption in /api/stats.
type ConsumptionStats struct {
	TotalBytes      int64              `json:"total_bytes"`
	Last30DaysBytes int64              `json:"last_30_days_bytes"`
	Last7DaysBytes  int64              `json:"last_7_days_bytes"`
	Daily7d         []DailyConsumption `json:"daily_7d"`
}

// DailyConsumption is one day of recorded download consumption.
type DailyConsumption struct {
	Date  string `json:"date"`
	Bytes int64  `json:"bytes"`
}

// DashboardStats mirrors /api/stats. TorrentStats is intentionally dynamic
// because the active backend exposes different metric names.
type DashboardStats struct {
	LastCycle    LastCycle         `json:"last_cycle"`
	TorrentStats map[string]any    `json:"torrent_stats"`
	Consumption  *ConsumptionStats `json:"consumption"`
}

// ConfigSnapshot mirrors the editable subset of GET /api/config.
type ConfigSnapshot struct {
	Active          bool   `json:"active"`
	DryRun          bool   `json:"dry_run"`
	RefreshSecs     uint64 `json:"refresh_secs"`
	DefaultLanguage string `json:"default_language"`
	Libtorrent      struct {
		DownloadLimitKib int64 `json:"download_limit_kib"`
		UploadLimitKib   int64 `json:"upload_limit_kib"`
	} `json:"libtorrent"`
}

// SpeedPolicy mirrors GET /api/torrents/temp-limits: the global limits in
// force and where they come from ("temp", "schedule" or "base").
type SpeedPolicy struct {
	Source           string `json:"source"`
	DownloadKib      int64  `json:"download_kib"`
	UploadKib        int64  `json:"upload_kib"`
	TempActive       bool   `json:"temp_active"`
	TempDownloadKib  int64  `json:"temp_download_kib"`
	TempUploadKib    int64  `json:"temp_upload_kib"`
	TempRemainingSec int64  `json:"temp_remaining_sec"`
	SchedActive      bool   `json:"sched_active"`
	BaseDownloadKib  int64  `json:"base_download_kib"`
	BaseUploadKib    int64  `json:"base_upload_kib"`
	// AutoRemoveCompleted is not a limit, but the same downloads panel.
	AutoRemoveCompleted bool `json:"auto_remove_completed"`
}

// PathCheck is one health path entry.
type PathCheck struct {
	Label    string `json:"label"`
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	Writable bool   `json:"writable"`
}

// DiskInfo is one mounted disk.
type DiskInfo struct {
	Mount      string `json:"mount"`
	Filesystem string `json:"filesystem"`
	TotalBytes uint64 `json:"total_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
}

// RamDiskInfo describes the RAM disk.
type RamDiskInfo struct {
	Path       string `json:"path"`
	TotalBytes uint64 `json:"total_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
}

// Health mirrors /api/health.
type Health struct {
	Status               string       `json:"status"`
	ProcessID            int64        `json:"process_id"`
	ResidentBytes        uint64       `json:"resident_bytes"`
	MemoryTotalBytes     uint64       `json:"memory_total_bytes"`
	MemoryAvailableBytes uint64       `json:"memory_available_bytes"`
	CPUPercent           *float64     `json:"cpu_percent"`
	ProcessCPUPercent    *float64     `json:"process_cpu_percent"`
	LoadAverage          *float64     `json:"load_average"`
	DiskTotalBytes       uint64       `json:"disk_total_bytes"`
	DiskFreeBytes        uint64       `json:"disk_free_bytes"`
	UptimeSeconds        uint64       `json:"uptime_seconds"`
	ProcessUptimeSeconds uint64       `json:"process_uptime_seconds"`
	TrashFileCount       uint64       `json:"trash_file_count"`
	TrashBytes           uint64       `json:"trash_bytes"`
	DataDirWritable      bool         `json:"data_dir_writable"`
	Paths                []PathCheck  `json:"paths"`
	Disks                []DiskInfo   `json:"disks"`
	Ramdisk              *RamDiskInfo `json:"ramdisk"`
	LastErrors           []string     `json:"last_errors"`
}

// Event is one entry of /api/torrent-events.
type Event struct {
	Kind     string `json:"kind"`
	Hash     string `json:"hash"`
	Name     string `json:"name"`
	SavePath string `json:"save_path"`
	Message  string `json:"message"`
}

// ArchiveEntry mirrors one row of /api/archive.
type ArchiveEntry struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	Magnet       string `json:"magnet"`
	Source       string `json:"source"`
	QualityScore int64  `json:"quality_score"`
	AddedAt      string `json:"added_at"`
}

// Gap mirrors one missing episode from /api/gaps.
type Gap struct {
	Series  string `json:"series"`
	Season  int64  `json:"season"`
	Episode int64  `json:"episode"`
	AirDate string `json:"air_date"`
}

// BlocklistEntry mirrors one row of /api/blocklist.
type BlocklistEntry struct {
	Hash       string `json:"hash"`
	Title      string `json:"title"`
	Reason     string `json:"reason"`
	CreatedAt  string `json:"created_at"`
	Kind       string `json:"kind"`
	SeriesName string `json:"series_name"`
	Season     *int64 `json:"season"`
	Episode    *int64 `json:"episode"`
	MovieName  string `json:"movie_name"`
	MovieYear  *int64 `json:"movie_year"`
}

// APIError carries the HTTP status of a failed request.
type APIError struct {
	Status int
	Detail string
}

func (e *APIError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("HTTP %d", e.Status)
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Detail)
}

// Client talks to a running gextto daemon over HTTP.
type Client struct {
	base    string
	http    *http.Client
	traffic *trafficCounter
}

// NewClient builds a client for base (e.g. http://127.0.0.1:5000).
func NewClient(base string) *Client {
	traffic := &trafficCounter{}
	return &Client{
		base: strings.TrimRight(strings.TrimSpace(base), "/"),
		http: &http.Client{Timeout: 30 * time.Second, Transport: &countingTransport{
			base: http.DefaultTransport, counter: traffic, apiKey: strings.TrimSpace(os.Getenv("GEXTTO_API_KEY")),
		}},
		traffic: traffic,
	}
}

// Traffic returns the bytes sent to and received from the daemon so far
// (request and response headers are estimated, bodies are exact) and the
// number of requests made.
func (c *Client) Traffic() (sent, received, requests int64) {
	return c.traffic.sent.Load(), c.traffic.received.Load(), c.traffic.requests.Load()
}

// trafficCounter accumulates API traffic for the footer bandwidth meter.
type trafficCounter struct {
	sent     atomic.Int64
	received atomic.Int64
	requests atomic.Int64
}

// countingTransport counts the bytes of every request and response,
// including long-lived SSE streams, as they are read.
type countingTransport struct {
	base    http.RoundTripper
	counter *trafficCounter
	// apiKey (GEXTTO_API_KEY) reaches a daemon whose access control asks
	// for a login from outside the local network.
	apiKey string
}

func (t *countingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if t.apiKey != "" && request.Header.Get("X-Api-Key") == "" {
		request = request.Clone(request.Context())
		request.Header.Set("X-Api-Key", t.apiKey)
	}
	sent := int64(len(request.Method) + len(request.URL.RequestURI()) + 12 + headerSize(request.Header))
	if request.ContentLength > 0 {
		sent += request.ContentLength
	}
	t.counter.sent.Add(sent)
	t.counter.requests.Add(1)
	response, err := t.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	t.counter.received.Add(int64(len(response.Status) + 11 + headerSize(response.Header)))
	response.Body = &countingBody{ReadCloser: response.Body, counter: &t.counter.received}
	return response, nil
}

func headerSize(header http.Header) int {
	size := 2
	for key, values := range header {
		for _, value := range values {
			size += len(key) + len(value) + 4
		}
	}
	return size
}

type countingBody struct {
	io.ReadCloser
	counter *atomic.Int64
}

func (b *countingBody) Read(buffer []byte) (int, error) {
	count, err := b.ReadCloser.Read(buffer)
	b.counter.Add(int64(count))
	return count, err
}

func (c *Client) do(request *http.Request) (*http.Response, error) {
	request.Header.Set("Accept", "application/json")
	return c.http.Do(request)
}

func (c *Client) decode(response *http.Response, out any) error {
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return &APIError{Status: response.StatusCode, Detail: strings.TrimSpace(string(detail))}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	return json.NewDecoder(response.Body).Decode(out)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	response, err := c.do(request)
	if err != nil {
		return err
	}
	return c.decode(response, out)
}

func (c *Client) postJSON(ctx context.Context, path string, body any, out any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.do(request)
	if err != nil {
		return err
	}
	return c.decode(response, out)
}

// Status fetches the daemon summary.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var status Status
	err := c.get(ctx, "/api/status", &status)
	return status, err
}

// Stats fetches aggregate transfer and consumption statistics.
func (c *Client) Stats(ctx context.Context) (DashboardStats, error) {
	var stats DashboardStats
	err := c.get(ctx, "/api/stats", &stats)
	return stats, err
}

// Config fetches the editable daemon settings.
func (c *Client) Config(ctx context.Context) (ConfigSnapshot, error) {
	var config ConfigSnapshot
	err := c.get(ctx, "/api/config", &config)
	return config, err
}

// SaveSetting persists one configuration setting.
func (c *Client) SaveSetting(ctx context.Context, key, value string) error {
	return c.postJSON(ctx, "/api/config/settings", map[string]string{"key": key, "value": value}, nil)
}

// SetLanguage changes the daemon's active UI language.
func (c *Client) SetLanguage(ctx context.Context, language string) error {
	return c.postJSON(ctx, "/api/i18n/active", map[string]string{"lang": language}, nil)
}

// Torrents fetches the session torrents (already sorted by name).
func (c *Client) Torrents(ctx context.Context) ([]Torrent, error) {
	var torrents []Torrent
	err := c.get(ctx, "/api/torrents", &torrents)
	if torrents == nil {
		torrents = []Torrent{}
	}
	return torrents, err
}

// ComicDownloads fetches the live HTTP/MEGA comic downloads.
func (c *Client) ComicDownloads(ctx context.Context) ([]ComicDownload, error) {
	var downloads []ComicDownload
	err := c.get(ctx, "/api/comics/downloads", &downloads)
	if downloads == nil {
		downloads = []ComicDownload{}
	}
	return downloads, err
}

// Series fetches the monitored TV series summary rows.
func (c *Client) Series(ctx context.Context) ([]SeriesLibraryItem, error) {
	var response struct {
		Items []SeriesLibraryItem `json:"items"`
	}
	err := c.get(ctx, "/api/series", &response)
	if response.Items == nil {
		response.Items = []SeriesLibraryItem{}
	}
	return response.Items, err
}

// Movies fetches the monitored movie summary rows.
func (c *Client) Movies(ctx context.Context) ([]MovieLibraryItem, error) {
	var response struct {
		Items []MovieLibraryItem `json:"items"`
	}
	err := c.get(ctx, "/api/movies", &response)
	if response.Items == nil {
		response.Items = []MovieLibraryItem{}
	}
	return response.Items, err
}

// Comics fetches the monitored comic summary rows.
func (c *Client) Comics(ctx context.Context) ([]ComicLibraryItem, error) {
	var items []ComicLibraryItem
	err := c.get(ctx, "/api/comics", &items)
	if items == nil {
		items = []ComicLibraryItem{}
	}
	return items, err
}

// TorrentDetail fetches one torrent with its magnet and no-rename flag.
func (c *Client) TorrentDetail(ctx context.Context, hash string) (TorrentDetail, error) {
	var detail TorrentDetail
	err := c.get(ctx, "/api/torrents/"+url.PathEscape(hash), &detail)
	return detail, err
}

// TorrentCollection fetches trackers, files or peers for a torrent.
func (c *Client) TorrentCollection(ctx context.Context, hash, kind string) ([]map[string]any, error) {
	var raw map[string]json.RawMessage
	if err := c.get(ctx, "/api/torrents/"+url.PathEscape(hash)+"/"+kind, &raw); err != nil {
		return nil, err
	}
	items := []map[string]any{}
	if payload, ok := raw[kind]; ok {
		_ = json.Unmarshal(payload, &items)
	}
	return items, nil
}

// Logs fetches the last log lines.
func (c *Client) Logs(ctx context.Context, limit int) ([]string, error) {
	var response struct {
		Items []string `json:"items"`
	}
	err := c.get(ctx, fmt.Sprintf("/api/logs?limit=%d", limit), &response)
	if response.Items == nil {
		response.Items = []string{}
	}
	return response.Items, err
}

// Health fetches the system health report.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var health Health
	err := c.get(ctx, "/api/health", &health)
	return health, err
}

// Events fetches the recent torrent events.
func (c *Client) Events(ctx context.Context) ([]Event, error) {
	var events []Event
	err := c.get(ctx, "/api/torrent-events", &events)
	if events == nil {
		events = []Event{}
	}
	return events, err
}

// Archive fetches a page of archived releases.
func (c *Client) Archive(ctx context.Context, query string, page int) ([]ArchiveEntry, int, int, error) {
	var response struct {
		Items []ArchiveEntry `json:"items"`
		Total int            `json:"total"`
		Pages int            `json:"pages"`
	}
	if page < 1 {
		page = 1
	}
	path := fmt.Sprintf("/api/archive?limit=200&page=%d", page)
	if strings.TrimSpace(query) != "" {
		path += "&q=" + url.QueryEscape(query)
	}
	err := c.get(ctx, path, &response)
	if response.Items == nil {
		response.Items = []ArchiveEntry{}
	}
	return response.Items, response.Total, response.Pages, err
}

// Gaps fetches the currently missing monitored episodes.
func (c *Client) Gaps(ctx context.Context) ([]Gap, error) {
	var response struct {
		Items []Gap `json:"items"`
	}
	err := c.get(ctx, "/api/gaps", &response)
	if response.Items == nil {
		response.Items = []Gap{}
	}
	return response.Items, err
}

// Blocklist fetches the recent blocklist entries.
func (c *Client) Blocklist(ctx context.Context) ([]BlocklistEntry, error) {
	var response struct {
		Items []BlocklistEntry `json:"items"`
	}
	err := c.get(ctx, "/api/blocklist", &response)
	if response.Items == nil {
		response.Items = []BlocklistEntry{}
	}
	return response.Items, err
}

// RemoveBlocklist removes one blocklist entry by magnet hash.
func (c *Client) RemoveBlocklist(ctx context.Context, hash string) error {
	return c.postJSON(ctx, "/api/blocklist/"+url.PathEscape(hash)+"/remove", map[string]any{}, nil)
}

// RunCycle starts a cycle (full, series, movies or comics).
func (c *Client) RunCycle(ctx context.Context, domain string) (map[string]any, error) {
	path := "/api/run_now"
	if domain != "" && domain != "full" {
		path += "?domain=" + url.QueryEscape(domain)
	}
	var result map[string]any
	err := c.postJSON(ctx, path, map[string]any{}, &result)
	return result, err
}

// AddMagnet adds a magnet or torrent URL.
func (c *Client) AddMagnet(ctx context.Context, magnet string) error {
	return c.postJSON(ctx, "/api/send-magnet", map[string]any{"magnet": magnet}, nil)
}

// AddTorrentFile uploads a .torrent file.
func (c *Client) AddTorrentFile(ctx context.Context, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return fmt.Errorf("empty file")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/upload-torrent", bytes.NewReader(data))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-bittorrent")
	response, err := c.do(request)
	if err != nil {
		return err
	}
	return c.decode(response, nil)
}

// Search runs a manual search.
func (c *Client) Search(ctx context.Context, query string) ([]map[string]any, error) {
	var response struct {
		Results []map[string]any `json:"results"`
	}
	if err := c.postJSON(ctx, "/api/search", map[string]any{"query": query}, &response); err != nil {
		return nil, err
	}
	if response.Results == nil {
		response.Results = []map[string]any{}
	}
	return response.Results, nil
}

// AddRelease queues one search result.
func (c *Client) AddRelease(ctx context.Context, release map[string]any) error {
	return c.postJSON(ctx, "/api/search/add", map[string]any{"release": release}, nil)
}

// Pause pauses a torrent.
func (c *Client) Pause(ctx context.Context, hash string) error {
	return c.postJSON(ctx, "/api/torrents/"+url.PathEscape(hash)+"/pause", map[string]any{}, nil)
}

// Resume resumes a torrent.
func (c *Client) Resume(ctx context.Context, hash string) error {
	return c.postJSON(ctx, "/api/torrents/"+url.PathEscape(hash)+"/resume", map[string]any{}, nil)
}

// PauseHTTPDownload pauses a live comic HTTP download.
func (c *Client) PauseHTTPDownload(ctx context.Context, id string) error {
	return c.postJSON(ctx, "/api/comics/downloads/"+url.PathEscape(id)+"/pause", map[string]any{}, nil)
}

// ResumeHTTPDownload resumes a paused comic HTTP download.
func (c *Client) ResumeHTTPDownload(ctx context.Context, id string) error {
	return c.postJSON(ctx, "/api/comics/downloads/"+url.PathEscape(id)+"/resume", map[string]any{}, nil)
}

// RemoveHTTPDownload removes a live comic HTTP download without deleting files.
func (c *Client) RemoveHTTPDownload(ctx context.Context, id string) error {
	return c.postJSON(ctx, "/api/comics/downloads/"+url.PathEscape(id)+"/remove", map[string]any{"delete_files": false}, nil)
}

// Restart restarts a torrent.
func (c *Client) Restart(ctx context.Context, hash string) error {
	return c.postJSON(ctx, "/api/torrents/"+url.PathEscape(hash)+"/restart", map[string]any{}, nil)
}

// Recheck forces a data check.
func (c *Client) Recheck(ctx context.Context, hash string) error {
	return c.postJSON(ctx, "/api/torrents/"+url.PathEscape(hash)+"/recheck", map[string]any{}, nil)
}

// Reannounce re-announces to trackers.
func (c *Client) Reannounce(ctx context.Context, hash string) error {
	return c.postJSON(ctx, "/api/torrents/"+url.PathEscape(hash)+"/reannounce", map[string]any{}, nil)
}

// Remove removes a torrent, optionally deleting its files.
func (c *Client) Remove(ctx context.Context, hash string, deleteFiles, blocklist bool) error {
	return c.postJSON(ctx, "/api/torrents/"+url.PathEscape(hash)+"/remove",
		map[string]any{"delete_files": deleteFiles, "blocklist": blocklist}, nil)
}

// Pin pins a torrent.
func (c *Client) Pin(ctx context.Context, hash string) error {
	return c.postJSON(ctx, "/api/torrents/pin", map[string]any{"hash": hash}, nil)
}

// Unpin clears the pin.
func (c *Client) Unpin(ctx context.Context) error {
	return c.postJSON(ctx, "/api/torrents/unpin", map[string]any{}, nil)
}

// SetNoRename toggles the no-rename flag.
func (c *Client) SetNoRename(ctx context.Context, hash string, value bool) error {
	return c.postJSON(ctx, "/api/torrents/"+url.PathEscape(hash)+"/no_rename", map[string]any{"value": value}, nil)
}

// RemoveCompleted removes completed torrents that reached their seed limit.
func (c *Client) RemoveCompleted(ctx context.Context, deleteFiles bool) (map[string]any, error) {
	var result map[string]any
	err := c.postJSON(ctx, "/api/torrents/remove_completed", map[string]any{"delete_files": deleteFiles}, &result)
	return result, err
}

// SetSpeedLimits sets the global KiB/s limits.
func (c *Client) SetSpeedLimits(ctx context.Context, downloadKib, uploadKib int64) error {
	return c.postJSON(ctx, "/api/set-speed-limits",
		map[string]any{"download_kib": downloadKib, "upload_kib": uploadKib}, nil)
}

// SpeedPolicy fetches the global speed limits in force.
func (c *Client) SpeedPolicy(ctx context.Context) (SpeedPolicy, error) {
	var policy SpeedPolicy
	err := c.get(ctx, "/api/torrents/temp-limits", &policy)
	return policy, err
}

// SetTempLimits applies temporary KiB/s limits for the given minutes (0 keeps
// them until removed); clear removes them and restores the normal limits.
func (c *Client) SetTempLimits(ctx context.Context, downloadKib, uploadKib, minutes int64, clear bool) error {
	return c.postJSON(ctx, "/api/torrents/temp-limits",
		map[string]any{"download_kib": downloadKib, "upload_kib": uploadKib, "minutes": minutes, "clear": clear}, nil)
}

// CleanTrash empties the trash.
func (c *Client) CleanTrash(ctx context.Context) (map[string]any, error) {
	var result map[string]any
	err := c.postJSON(ctx, "/api/maintenance/clean-trash", map[string]any{"force": true}, &result)
	return result, err
}

// Language asks the daemon for its active UI language.
func (c *Client) Language(ctx context.Context) (string, error) {
	var response struct {
		Lang string `json:"lang"`
	}
	if err := c.get(ctx, "/api/i18n/active", &response); err != nil {
		return "", err
	}
	return response.Lang, nil
}

// StreamLogs consumes the SSE log stream until ctx is cancelled. connected is
// called after the HTTP stream is established; snapshots replaces the buffer,
// lines appends single entries.
func (c *Client) StreamLogs(ctx context.Context, connected func(), snapshots func([]string), lines func(string)) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/logs/stream?limit=500", nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "text/event-stream")
	// A stream must not inherit Client's 30-second request timeout. The
	// context supplied by the TUI owns its lifetime and reconnects on errors.
	streamHTTP := *c.http
	streamHTTP.Timeout = 0
	response, err := streamHTTP.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return &APIError{Status: response.StatusCode}
	}
	if connected != nil {
		connected()
	}
	reader := bufio.NewReader(response.Body)
	var data []string
	for {
		raw, err := reader.ReadString('\n')
		if err != nil {
			if len(data) > 0 {
				c.emitEvent(strings.Join(data, "\n"), snapshots, lines)
			}
			return err
		}
		line := strings.TrimRight(raw, "\r\n")
		switch {
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		case line == "" && len(data) > 0:
			c.emitEvent(strings.Join(data, "\n"), snapshots, lines)
			data = nil
		}
	}
}

// StreamNotifications consumes torrent lifecycle events until ctx is cancelled.
func (c *Client) StreamNotifications(ctx context.Context, events func(Event)) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/notifications/stream", nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "text/event-stream")
	streamHTTP := *c.http
	streamHTTP.Timeout = 0
	response, err := streamHTTP.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return &APIError{Status: response.StatusCode}
	}
	reader := bufio.NewReader(response.Body)
	var data []string
	eventName := ""
	for {
		raw, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		line := strings.TrimRight(raw, "\r\n")
		switch {
		case strings.HasPrefix(line, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		case line == "" && len(data) > 0:
			if eventName == "torrent" && events != nil {
				var event Event
				if json.Unmarshal([]byte(strings.Join(data, "\n")), &event) == nil {
					events(event)
				}
			}
			data = nil
			eventName = ""
		}
	}
}

func (c *Client) emitEvent(payload string, snapshots func([]string), lines func(string)) {
	var value map[string]any
	if err := json.Unmarshal([]byte(payload), &value); err != nil {
		return
	}
	if snapshot, ok := value["snapshot"].([]any); ok {
		converted := make([]string, 0, len(snapshot))
		for _, entry := range snapshot {
			converted = append(converted, fmt.Sprint(entry))
		}
		if snapshots != nil {
			snapshots(converted)
		}
		return
	}
	if line, ok := value["line"]; ok && lines != nil {
		lines(fmt.Sprint(line))
	}
}
