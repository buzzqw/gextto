// Package qbittorrent is a small, dependency-free client for the qBittorrent
// Web API (v2). It deliberately does not import Gextto types so it can be unit
// tested against a fake HTTP server and reused by any backend adapter.
//
// Scope: login/session handling, torrent add/list/inspect, pause/resume,
// delete, recheck, storage move, per-torrent and global transfer limits,
// incremental sync (sync/maindata) and version negotiation.
package qbittorrent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Errors returned by the client. Callers can match them with errors.Is.
var (
	ErrUnavailable = errors.New("qbittorrent: backend unavailable")
	ErrAuth        = errors.New("qbittorrent: authentication failed")
	ErrNotFound    = errors.New("qbittorrent: torrent not found")
	ErrRejected    = errors.New("qbittorrent: torrent rejected")
)

const loginPath = "/api/v2/auth/login"

// Config configures a Client.
type Config struct {
	BaseURL        string
	Username       string
	Password       string
	RequestTimeout time.Duration
}

// Client talks to one qBittorrent-nox instance. It is safe for concurrent use.
type Client struct {
	base string
	user string
	pass string
	http *http.Client

	mu       sync.Mutex
	loggedIn bool
	appVer   string
	apiVer   string
}

// New builds a client. The BaseURL must include scheme and host (and any path
// prefix). A cookie jar is created automatically for the SID session cookie.
func New(cfg Config) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("qbittorrent: empty base url")
	}
	if _, err := url.Parse(base); err != nil {
		return nil, fmt.Errorf("qbittorrent: invalid base url: %w", err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Client{
		base: base,
		user: cfg.Username,
		pass: cfg.Password,
		http: &http.Client{Timeout: timeout, Jar: jar},
	}, nil
}

// AddOptions are the add-time parameters shared by magnets and .torrent files.
type AddOptions struct {
	SavePath   string
	Category   string
	Tags       string
	Paused     bool
	Sequential bool
	FirstLast  bool
}

// Torrent is the subset of /torrents/info this client consumes.
type Torrent struct {
	Hash         string  `json:"hash"`
	Name         string  `json:"name"`
	State        string  `json:"state"`
	Progress     float64 `json:"progress"`
	Size         int64   `json:"size"`
	Downloaded   int64   `json:"downloaded"`
	Uploaded     int64   `json:"uploaded"`
	DownloadRate int64   `json:"dlspeed"`
	UploadRate   int64   `json:"upspeed"`
	NumSeeds     int     `json:"num_seeds"`
	NumLeechs    int     `json:"num_leechs"`
	SavePath     string  `json:"save_path"`
	Category     string  `json:"category"`
	Tags         string  `json:"tags"`
	Ratio        float64 `json:"ratio"`
	ETASeconds   int64   `json:"eta"`
	SeedingTime  int64   `json:"seeding_time"`
	AddedOn      int64   `json:"added_on"`
	CompletedOn  int64   `json:"completion_on"`
	Error        string  `json:"-"`
}

// Completeness reports whether the torrent finished downloading.
func (t Torrent) Completeness() float64 {
	if t.Progress <= 0 {
		return 0
	}
	if t.Progress > 1 {
		return 1
	}
	return t.Progress
}

// NormalizeState maps a qBittorrent state to the normalized Gextto vocabulary.
func NormalizeState(state string) string {
	switch state {
	case "metaDL", "forcedMetaDL":
		return "downloading_metadata"
	case "downloading", "forcedDL":
		return "downloading"
	case "stalledDL":
		return "stalled"
	case "queuedDL", "queuedUP":
		return "queued"
	case "pausedDL", "stoppedDL", "pausedUP", "stoppedUP":
		return "paused"
	case "checkingDL", "checkingUP", "checkingResumeData", "allocating":
		return "checking_files"
	case "moving":
		return "moving"
	case "uploading", "forcedUP", "stalledUP":
		return "seeding"
	case "error", "missingFiles":
		return "error"
	default:
		return "unknown"
	}
}

type bodyBuilder func() (io.Reader, string)

func staticBody(raw, contentType string) bodyBuilder {
	return func() (io.Reader, string) {
		return strings.NewReader(raw), contentType
	}
}

func formBody(form url.Values) bodyBuilder {
	encoded := form.Encode()
	return staticBody(encoded, "application/x-www-form-urlencoded")
}

// attempt performs one HTTP round trip. It never follows the session-refresh
// logic so do() can retry cleanly.
func (c *Client) attempt(ctx context.Context, method, path string, build bodyBuilder) (int, []byte, error) {
	var body io.Reader
	contentType := ""
	if build != nil {
		body, contentType = build()
	}
	request, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return 0, nil, err
	}
	// qBittorrent requires a matching Referer/Origin for POST requests (CSRF).
	request.Header.Set("Referer", c.base)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return response.StatusCode, nil, err
	}
	return response.StatusCode, payload, nil
}

// do runs a request, ensures a session, and refreshes it once on 403 (expired
// SID). It returns the raw response body on success.
func (c *Client) do(ctx context.Context, method, path string, build bodyBuilder) ([]byte, error) {
	if path != loginPath {
		if err := c.ensureLogin(ctx); err != nil {
			return nil, err
		}
	}
	status, payload, err := c.attempt(ctx, method, path, build)
	if err != nil {
		return nil, err
	}
	if status == http.StatusForbidden {
		// Session expired (or banned): re-login once and retry.
		c.markLoggedOut()
		if err := c.Login(ctx); err != nil {
			return nil, err
		}
		status, payload, err = c.attempt(ctx, method, path, build)
		if err != nil {
			return nil, err
		}
	}
	switch {
	case status == http.StatusNotFound:
		return nil, ErrNotFound
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return nil, ErrAuth
	case status == http.StatusUnsupportedMediaType || status == http.StatusBadRequest:
		return nil, fmt.Errorf("%w: HTTP %d", ErrRejected, status)
	case status < 200 || status >= 300:
		return nil, fmt.Errorf("qbittorrent: %s %s: HTTP %d", method, path, status)
	}
	return payload, nil
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	payload, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	return decodeJSON(path, payload, out)
}

// getText fetches an endpoint that returns plain text (app/version, ...).
func (c *Client) getText(ctx context.Context, path string) (string, error) {
	payload, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func decodeJSON(path string, payload []byte, out any) error {
	if out == nil || len(payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("qbittorrent: decode %s: %w", path, err)
	}
	return nil
}

func (c *Client) postForm(ctx context.Context, path string, form url.Values) error {
	_, err := c.do(ctx, http.MethodPost, path, formBody(form))
	return err
}

func (c *Client) markLoggedOut() {
	c.mu.Lock()
	c.loggedIn = false
	c.mu.Unlock()
}

func (c *Client) ensureLogin(ctx context.Context) error {
	c.mu.Lock()
	logged := c.loggedIn
	c.mu.Unlock()
	if logged {
		return nil
	}
	return c.Login(ctx)
}

// Login authenticates and stores the SID cookie in the jar.
func (c *Client) Login(ctx context.Context) error {
	form := url.Values{"username": {c.user}, "password": {c.pass}}
	status, payload, err := c.attempt(ctx, http.MethodPost, loginPath, formBody(form))
	if err != nil {
		return err
	}
	if status == http.StatusForbidden {
		return fmt.Errorf("%w: banned or too many attempts", ErrAuth)
	}
	if status != http.StatusOK {
		return fmt.Errorf("%w: HTTP %d", ErrAuth, status)
	}
	body := strings.TrimSpace(string(payload))
	if body != "Ok." {
		return fmt.Errorf("%w: %s", ErrAuth, body)
	}
	c.mu.Lock()
	c.loggedIn = true
	c.mu.Unlock()
	return nil
}

// AppVersion returns the qBittorrent version string (e.g. "v5.0.0").
func (c *Client) AppVersion(ctx context.Context) (string, error) {
	c.mu.Lock()
	cached := c.appVer
	c.mu.Unlock()
	if cached != "" {
		return cached, nil
	}
	var raw string
	raw, err := c.getText(ctx, "/api/v2/app/version")
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.appVer = raw
	c.mu.Unlock()
	return raw, nil
}

// WebAPIVersion returns the Web API version (e.g. "2.11.2").
func (c *Client) WebAPIVersion(ctx context.Context) (string, error) {
	c.mu.Lock()
	cached := c.apiVer
	c.mu.Unlock()
	if cached != "" {
		return cached, nil
	}
	var raw string
	raw, err := c.getText(ctx, "/api/v2/app/webapiVersion")
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.apiVer = raw
	c.mu.Unlock()
	return raw, nil
}

// Torrents lists every torrent.
func (c *Client) Torrents(ctx context.Context) ([]Torrent, error) {
	var items []Torrent
	if err := c.getJSON(ctx, "/api/v2/torrents/info", &items); err != nil {
		return nil, err
	}
	return items, nil
}

// TorrentInfo returns a single torrent by hash.
func (c *Client) TorrentInfo(ctx context.Context, hash string) (Torrent, error) {
	query := url.Values{"hashes": {strings.ToLower(hash)}}
	var items []Torrent
	if err := c.getJSON(ctx, "/api/v2/torrents/info?"+query.Encode(), &items); err != nil {
		return Torrent{}, err
	}
	if len(items) == 0 {
		return Torrent{}, ErrNotFound
	}
	return items[0], nil
}

// AddMagnet adds a magnet link and returns its lowercased infohash.
func (c *Client) AddMagnet(ctx context.Context, magnet string, opts AddOptions) (string, error) {
	hash := magnetHash(magnet)
	if err := c.add(ctx, magnet, "", opts); err != nil {
		return "", err
	}
	return hash, nil
}

// AddTorrentFile uploads a .torrent file.
func (c *Client) AddTorrentFile(ctx context.Context, path string, opts AddOptions) error {
	return c.add(ctx, "", path, opts)
}

func (c *Client) add(ctx context.Context, magnet, torrentPath string, opts AddOptions) error {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	var writeErr error
	field := func(key, value string) {
		if writeErr != nil || value == "" {
			return
		}
		writeErr = writer.WriteField(key, value)
	}
	field("urls", magnet)
	field("savepath", opts.SavePath)
	field("category", opts.Category)
	field("tags", opts.Tags)
	if opts.Paused {
		field("paused", "true")
	}
	if opts.Sequential {
		field("sequentialDownload", "true")
	}
	if opts.FirstLast {
		field("firstLastPiecePrio", "true")
	}
	if torrentPath != "" && writeErr == nil {
		var file *os.File
		file, writeErr = os.Open(torrentPath)
		if writeErr == nil {
			defer file.Close()
			var part io.Writer
			part, writeErr = writer.CreateFormFile("torrents", filepath.Base(torrentPath))
			if writeErr == nil {
				_, writeErr = io.Copy(part, file)
			}
		}
	}
	if writeErr == nil {
		writeErr = writer.Close()
	}
	if writeErr != nil {
		return writeErr
	}
	contentType := writer.FormDataContentType()
	_, err := c.do(ctx, http.MethodPost, "/api/v2/torrents/add", staticBody(buffer.String(), contentType))
	return err
}

// Pause stops (or pauses) the given torrents. qBittorrent 5.x renamed the
// endpoint, so the modern name is tried first and the legacy one on 404.
func (c *Client) Pause(ctx context.Context, hashes ...string) error {
	return c.control(ctx, hashes, "stop", "pause")
}

// Resume starts (or resumes) the given torrents.
func (c *Client) Resume(ctx context.Context, hashes ...string) error {
	return c.control(ctx, hashes, "start", "resume")
}

func (c *Client) control(ctx context.Context, hashes []string, modern, legacy string) error {
	form := url.Values{"hashes": {joinHashes(hashes)}}
	_, err := c.do(ctx, http.MethodPost, "/api/v2/torrents/"+modern, formBody(form))
	if errors.Is(err, ErrNotFound) {
		_, err = c.do(ctx, http.MethodPost, "/api/v2/torrents/"+legacy, formBody(form))
	}
	return err
}

// Delete removes torrents, optionally deleting their data.
func (c *Client) Delete(ctx context.Context, deleteFiles bool, hashes ...string) error {
	form := url.Values{
		"hashes":      {joinHashes(hashes)},
		"deleteFiles": {strconv.FormatBool(deleteFiles)},
	}
	return c.postForm(ctx, "/api/v2/torrents/delete", form)
}

// Recheck forces a data re-check.
func (c *Client) Recheck(ctx context.Context, hashes ...string) error {
	return c.postForm(ctx, "/api/v2/torrents/recheck", url.Values{"hashes": {joinHashes(hashes)}})
}

// Reannounce forces a tracker announce.
func (c *Client) Reannounce(ctx context.Context, hashes ...string) error {
	return c.postForm(ctx, "/api/v2/torrents/reannounce", url.Values{"hashes": {joinHashes(hashes)}})
}

// SetLocation moves the torrent storage to location (as seen by qBittorrent).
func (c *Client) SetLocation(ctx context.Context, location string, hashes ...string) error {
	form := url.Values{"hashes": {joinHashes(hashes)}, "location": {location}}
	return c.postForm(ctx, "/api/v2/torrents/setLocation", form)
}

// SetTorrentDownloadLimit sets a per-torrent download limit (bytes/s; 0 = none).
func (c *Client) SetTorrentDownloadLimit(ctx context.Context, limit int64, hashes ...string) error {
	form := url.Values{"hashes": {joinHashes(hashes)}, "limit": {strconv.FormatInt(limit, 10)}}
	return c.postForm(ctx, "/api/v2/torrents/setDownloadLimit", form)
}

// SetTorrentUploadLimit sets a per-torrent upload limit (bytes/s; 0 = none).
func (c *Client) SetTorrentUploadLimit(ctx context.Context, limit int64, hashes ...string) error {
	form := url.Values{"hashes": {joinHashes(hashes)}, "limit": {strconv.FormatInt(limit, 10)}}
	return c.postForm(ctx, "/api/v2/torrents/setUploadLimit", form)
}

// SetGlobalDownloadLimit sets the session download limit (bytes/s).
func (c *Client) SetGlobalDownloadLimit(ctx context.Context, limit int64) error {
	return c.postForm(ctx, "/api/v2/transfer/setDownloadLimit", url.Values{"limit": {strconv.FormatInt(limit, 10)}})
}

// SetGlobalUploadLimit sets the session upload limit (bytes/s).
func (c *Client) SetGlobalUploadLimit(ctx context.Context, limit int64) error {
	return c.postForm(ctx, "/api/v2/transfer/setUploadLimit", url.Values{"limit": {strconv.FormatInt(limit, 10)}})
}

// File is one entry of /torrents/files.
type File struct {
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
	Priority int     `json:"priority"`
}

// Files lists a torrent's files.
func (c *Client) Files(ctx context.Context, hash string) ([]File, error) {
	query := url.Values{"hash": {strings.ToLower(hash)}}
	var files []File
	if err := c.getJSON(ctx, "/api/v2/torrents/files?"+query.Encode(), &files); err != nil {
		return nil, err
	}
	return files, nil
}

// Tracker is one entry of /torrents/trackers.
type Tracker struct {
	URL      string `json:"url"`
	Status   int    `json:"status"`
	NumPeers int    `json:"num_peers"`
	Message  string `json:"msg"`
}

// Trackers lists a torrent's trackers.
func (c *Client) Trackers(ctx context.Context, hash string) ([]Tracker, error) {
	query := url.Values{"hash": {strings.ToLower(hash)}}
	var trackers []Tracker
	if err := c.getJSON(ctx, "/api/v2/torrents/trackers?"+query.Encode(), &trackers); err != nil {
		return nil, err
	}
	return trackers, nil
}

// Peer is one entry of sync/torrentPeers.
type Peer struct {
	IP        string  `json:"ip"`
	Port      int     `json:"port"`
	Client    string  `json:"client"`
	Progress  float64 `json:"progress"`
	DownSpeed int64   `json:"dl_speed"`
	UpSpeed   int64   `json:"up_speed"`
}

// Peers lists a torrent's connected peers.
func (c *Client) Peers(ctx context.Context, hash string) ([]Peer, error) {
	query := url.Values{"hash": {strings.ToLower(hash)}, "rid": {"0"}}
	var payload struct {
		Peers map[string]Peer `json:"peers"`
	}
	if err := c.getJSON(ctx, "/api/v2/sync/torrentPeers?"+query.Encode(), &payload); err != nil {
		return nil, err
	}
	peers := make([]Peer, 0, len(payload.Peers))
	for _, peer := range payload.Peers {
		peers = append(peers, peer)
	}
	return peers, nil
}

// TransferInfo returns the global transfer statistics.
func (c *Client) TransferInfo(ctx context.Context) (map[string]any, error) {
	var info map[string]any
	if err := c.getJSON(ctx, "/api/v2/transfer/info", &info); err != nil {
		return nil, err
	}
	return info, nil
}

// MainData is the incremental sync payload from sync/maindata.
type MainData struct {
	RID         int64              `json:"rid"`
	FullUpdate  bool               `json:"full_update"`
	Torrents    map[string]Torrent `json:"torrents"`
	Removed     []string           `json:"torrents_removed"`
	ServerState map[string]any     `json:"server_state"`
}

// Sync fetches the changes since rid (0 for a full snapshot).
func (c *Client) Sync(ctx context.Context, rid int64) (MainData, error) {
	query := url.Values{"rid": {strconv.FormatInt(rid, 10)}}
	var data MainData
	if err := c.getJSON(ctx, "/api/v2/sync/maindata?"+query.Encode(), &data); err != nil {
		return MainData{}, err
	}
	return data, nil
}

func joinHashes(hashes []string) string {
	lowered := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		hash = strings.ToLower(strings.TrimSpace(hash))
		if hash != "" {
			lowered = append(lowered, hash)
		}
	}
	return strings.Join(lowered, "|")
}

// magnetHash extracts the btih infohash from a magnet URI, if present.
func magnetHash(magnet string) string {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(magnet)), "magnet:") {
		return ""
	}
	parsed, err := url.Parse(magnet)
	if err != nil {
		return ""
	}
	for _, xt := range parsed.Query()["xt"] {
		lowered := strings.ToLower(xt)
		if strings.HasPrefix(lowered, "urn:btih:") {
			hash := strings.TrimPrefix(lowered, "urn:btih:")
			// Base32 infohashes are 32 chars, hex 40: return as-is (lowercased).
			return strings.TrimSpace(hash)
		}
	}
	return ""
}
