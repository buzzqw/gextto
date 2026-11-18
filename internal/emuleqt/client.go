// Package emuleqt talks to an emule-qt core (emulecored) through its REST API
// (/api/v1, API key). It is the second way Gextto reaches the eD2k and Kad
// networks, beside aMule's External Connections protocol: the same jobs —
// state, search, downloads, shared files, uploads, limits — over HTTP and
// JSON. Only what Gextto uses is modelled; the core's own contract is the
// OpenAPI document it serves at /api/v1/openapi.json.
package emuleqt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config locates a core.
type Config struct {
	// BaseURL is the core's web server, "http://127.0.0.1:4711".
	BaseURL string
	// APIKey is webserver.apiKey of the core's preferences.yml.
	APIKey string
	// Timeout bounds one request (default 15 s).
	Timeout time.Duration
	// HTTP replaces the default client (tests).
	HTTP *http.Client
}

// Client is safe for concurrent use.
type Client struct {
	base   string
	key    string
	client *http.Client
}

// ErrAuth is returned when the core refuses the API key.
var ErrAuth = errors.New("emule-qt refused the API key")

// APIError is an answer of the core that is not a success.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("emule-qt answered %d", e.Status)
	}
	return fmt.Sprintf("emule-qt answered %d: %s", e.Status, e.Message)
}

// TransportError is a core that could not be reached at all.
type TransportError struct{ Err error }

func (e *TransportError) Error() string { return "emule-qt: connect: " + e.Err.Error() }
func (e *TransportError) Unwrap() error { return e.Err }

// New returns a client for cfg.
func New(cfg Config) *Client {
	client := cfg.HTTP
	if client == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = 15 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	return &Client{base: strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"), key: cfg.APIKey, client: client}
}

// pageSize is the largest page the core serves.
const pageSize = 1000

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	if c.base == "" {
		return errors.New("emule-qt: no address configured")
	}
	target := c.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.key != "" {
		request.Header.Set("X-Api-Key", c.key)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return &TransportError{Err: err}
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil {
		return &TransportError{Err: err}
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return ErrAuth
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		var failure struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &failure)
		return &APIError{Status: response.StatusCode, Message: failure.Error.Message}
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("emule-qt: unreadable answer to %s %s: %w", method, path, err)
	}
	return nil
}

// list reads every page of a paged list into items.
func list[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	var all []T
	for offset := 0; ; {
		var page struct {
			Items []T `json:"items"`
			Total int `json:"total"`
		}
		query := url.Values{"limit": {strconv.Itoa(pageSize)}, "offset": {strconv.Itoa(offset)}}
		if err := c.do(ctx, http.MethodGet, path, query, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		offset += len(page.Items)
		if len(page.Items) == 0 || offset >= page.Total {
			return all, nil
		}
	}
}

// Health answers when the core is up; it needs no key.
func (c *Client) Health(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/api/v1/health", nil, nil, nil)
}

// Snapshot is the state of both networks and the transfer rates.
type Snapshot struct {
	Version string `json:"version"`
	Offline bool   `json:"offline"`
	Ed2k    struct {
		Connected  bool   `json:"connected"`
		Connecting bool   `json:"connecting"`
		LowID      bool   `json:"lowID"`
		Server     string `json:"server"`
	} `json:"ed2k"`
	Kad struct {
		Running    bool `json:"running"`
		Connected  bool `json:"connected"`
		Firewalled bool `json:"firewalled"`
	} `json:"kad"`
	Rates struct {
		DownKiBps float64 `json:"downKiBps"`
		UpKiBps   float64 `json:"upKiBps"`
	} `json:"rates"`
	Uploads struct {
		Active  int64 `json:"active"`
		Waiting int64 `json:"waiting"`
	} `json:"uploads"`
	SharedFiles int64 `json:"sharedFiles"`
}

// Snapshot reads the dashboard view of the core.
func (c *Client) Snapshot(ctx context.Context) (Snapshot, error) {
	var out Snapshot
	err := c.do(ctx, http.MethodGet, "/api/v1/snapshot", nil, nil, &out)
	return out, err
}

// Connection is the eD2k server link.
type Connection struct {
	Connected     bool  `json:"isConnected"`
	Connecting    bool  `json:"isConnecting"`
	LowID         bool  `json:"isLowID"`
	ClientID      int64 `json:"clientID"`
	CurrentServer *struct {
		Name    string `json:"name"`
		Address string `json:"address"`
		Port    int    `json:"port"`
	} `json:"currentServer"`
}

// Connection reads the eD2k server link.
func (c *Client) Connection(ctx context.Context) (Connection, error) {
	var out Connection
	err := c.do(ctx, http.MethodGet, "/api/v1/connection", nil, nil, &out)
	return out, err
}

// Connect connects to any eD2k server; Disconnect drops the link.
func (c *Client) Connect(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/api/v1/connection/connect", nil, nil, nil)
}

func (c *Client) Disconnect(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/api/v1/connection/disconnect", nil, nil, nil)
}

// ConnectServer connects to one server, "host:port".
func (c *Client) ConnectServer(ctx context.Context, address string) error {
	host, port, ok := strings.Cut(strings.TrimSpace(address), ":")
	if !ok || host == "" || port == "" {
		return fmt.Errorf("emule-qt: %q is not host:port", address)
	}
	return c.do(ctx, http.MethodPost, "/api/v1/servers/"+url.PathEscape(host)+"/"+url.PathEscape(port)+"/connect", nil, nil, nil)
}

// StartKad and StopKad switch Kad.
func (c *Client) StartKad(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/api/v1/kad/start", nil, nil, nil)
}

func (c *Client) StopKad(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/api/v1/kad/stop", nil, nil, nil)
}

// Download is one file of the download queue.
type Download struct {
	Hash             string  `json:"hash"`
	FileName         string  `json:"fileName"`
	FileSize         int64   `json:"fileSize"`
	CompletedSize    int64   `json:"completedSize"`
	PercentCompleted float64 `json:"percentCompleted"`
	Datarate         int64   `json:"datarate"`
	// Status is ready, empty, waitingforhash, hashing, error, insufficient,
	// paused, completing or complete.
	Status       string   `json:"status"`
	Paused       bool     `json:"isPaused"`
	Stopped      bool     `json:"isStopped"`
	Sources      int64    `json:"sourceCount"`
	Transferring int64    `json:"transferringSrcCount"`
	Priority     string   `json:"downPriority"`
	AutoPriority bool     `json:"isAutoDownPriority"`
	Category     int      `json:"category"`
	Confidence   string   `json:"confidence"`
	FakeScore    int      `json:"fakeScore"`
	FakeReasons  []string `json:"fakeReasons"`
}

// Downloads lists the download queue.
func (c *Client) Downloads(ctx context.Context) ([]Download, error) {
	return list[Download](ctx, c, "/api/v1/downloads")
}

// AddLink queues one ed2k:// file link.
func (c *Client) AddLink(ctx context.Context, link string, category int) error {
	return c.do(ctx, http.MethodPost, "/api/v1/downloads", nil, map[string]any{"link": link, "category": category}, nil)
}

func (c *Client) fileOp(ctx context.Context, hash, op string, body any) error {
	return c.do(ctx, http.MethodPost, "/api/v1/downloads/"+url.PathEscape(strings.ToLower(hash))+"/"+op, nil, body, nil)
}

// Pause, Resume and Cancel act on one download; Cancel cannot be undone.
func (c *Client) Pause(ctx context.Context, hash string) error {
	return c.fileOp(ctx, hash, "pause", nil)
}
func (c *Client) Resume(ctx context.Context, hash string) error {
	return c.fileOp(ctx, hash, "resume", nil)
}
func (c *Client) Cancel(ctx context.Context, hash string) error {
	return c.fileOp(ctx, hash, "cancel", map[string]any{"confirm": true})
}

// SetPriority sets low, normal, high or auto.
func (c *Client) SetPriority(ctx context.Context, hash, priority string) error {
	return c.do(ctx, http.MethodPatch, "/api/v1/downloads/"+url.PathEscape(strings.ToLower(hash)), nil, map[string]any{"priority": priority}, nil)
}

// ClearCompleted removes the finished files from the list.
func (c *Client) ClearCompleted(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/api/v1/downloads/clear-completed", nil, nil, nil)
}

// Search types of POST /search.
const (
	SearchAutomatic = "automatic"
	SearchKad       = "kad"
	SearchGlobal    = "ed2kGlobal"
	SearchServer    = "ed2kServer"
)

// SearchResult is one file a search found.
type SearchResult struct {
	Hash            string `json:"hash"`
	FileName        string `json:"fileName"`
	FileSize        int64  `json:"fileSize"`
	Sources         int64  `json:"sourceCount"`
	CompleteSources int64  `json:"completeSourceCount"`
	Confidence      string `json:"confidence"`
	FakeScore       int    `json:"fakeScore"`
	SpamRating      int    `json:"spamRating"`
	// MetaKind is set on a server-catalogue release (torrent, nzb, web):
	// not an eD2k file.
	MetaKind string `json:"metaKind"`
}

// SearchOptions shape one search.
type SearchOptions struct {
	Type    string
	MinSize int64
	MaxSize int64
	// Wait bounds the search; Poll is the interval between reads (default
	// 750 ms).
	Wait time.Duration
	Poll time.Duration
}

// Search runs one search to its end (or Wait) and returns its eD2k files.
// The search is dropped on the core afterwards.
func (c *Client) Search(ctx context.Context, expression string, options SearchOptions) ([]SearchResult, error) {
	body := map[string]any{"expression": expression}
	if options.Type != "" {
		body["type"] = options.Type
	}
	if options.MinSize > 0 {
		body["minSize"] = options.MinSize
	}
	if options.MaxSize > 0 {
		body["maxSize"] = options.MaxSize
	}
	var started struct {
		SearchID int64 `json:"searchID"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/search", nil, body, &started); err != nil {
		return nil, err
	}
	if started.SearchID <= 0 {
		return nil, errors.New("emule-qt: the search got no id")
	}
	id := strconv.FormatInt(started.SearchID, 10)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = c.do(cleanup, http.MethodDelete, "/api/v1/search/"+id, nil, nil, nil)
	}()
	wait, poll := options.Wait, options.Poll
	if wait <= 0 {
		wait = 60 * time.Second
	}
	if poll <= 0 {
		poll = 750 * time.Millisecond
	}
	deadline := time.Now().Add(wait)
	for {
		results, state, err := c.searchResults(ctx, id)
		if err != nil {
			return nil, err
		}
		if state == "finished" || state == "failed" || !time.Now().Before(deadline) {
			return results, nil
		}
		select {
		case <-ctx.Done():
			return results, nil
		case <-time.After(poll):
		}
	}
}

func (c *Client) searchResults(ctx context.Context, id string) ([]SearchResult, string, error) {
	var page struct {
		Items []SearchResult `json:"items"`
		State string         `json:"state"`
	}
	query := url.Values{"limit": {strconv.Itoa(pageSize)}}
	if err := c.do(ctx, http.MethodGet, "/api/v1/search/"+id+"/results", query, nil, &page); err != nil {
		return nil, "", err
	}
	files := page.Items[:0]
	for _, item := range page.Items {
		if item.MetaKind == "" {
			files = append(files, item)
		}
	}
	return files, page.State, nil
}

// SharedFile is one file the core shares.
type SharedFile struct {
	Hash     string `json:"hash"`
	FileName string `json:"fileName"`
	FileSize int64  `json:"fileSize"`
}

// SharedFiles lists the shared files.
func (c *Client) SharedFiles(ctx context.Context) ([]SharedFile, error) {
	return list[SharedFile](ctx, c, "/api/v1/shared")
}

// Directories are the core's folders, on the machine it runs on.
type Directories struct {
	Shared   []string `json:"directories"`
	Incoming string   `json:"incomingDir"`
	Temp     []string `json:"tempDirs"`
}

// SharedDirectories reads the shared, incoming and temporary folders.
func (c *Client) SharedDirectories(ctx context.Context) (Directories, error) {
	var out Directories
	err := c.do(ctx, http.MethodGet, "/api/v1/shared/directories", nil, nil, &out)
	return out, err
}

// ReloadSharedFiles rescans the shared folders.
func (c *Client) ReloadSharedFiles(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/api/v1/shared/reload", nil, nil, nil)
}

// UploadClient is a peer the core is sending to.
type UploadClient struct {
	UserName       string `json:"userName"`
	Software       string `json:"software"`
	Addr           string `json:"addr"`
	UploadFileName string `json:"uploadFileName"`
	UpDatarate     int64  `json:"upDatarate"`
	SessionUp      int64  `json:"sessionUp"`
	TransferredUp  int64  `json:"transferredUp"`
}

// Uploads lists the peers being uploaded to.
func (c *Client) Uploads(ctx context.Context) ([]UploadClient, error) {
	return list[UploadClient](ctx, c, "/api/v1/uploads")
}

// Server is one eD2k server of the list.
type Server struct {
	Name        string `json:"name"`
	Address     string `json:"address"`
	Port        int    `json:"port"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Users       int64  `json:"users"`
	Files       int64  `json:"files"`
	Ping        int64  `json:"ping"`
	Failed      int64  `json:"failedCount"`
	Static      bool   `json:"isStatic"`
}

// Servers lists the server list.
func (c *Client) Servers(ctx context.Context) ([]Server, error) {
	return list[Server](ctx, c, "/api/v1/servers")
}

// ImportServers loads a server.met from a URL; ImportNodes a nodes.dat.
func (c *Client) ImportServers(ctx context.Context, source string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/servers/import", nil, map[string]any{"url": source}, nil)
}

func (c *Client) ImportNodes(ctx context.Context, source string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/kad/import-nodes", nil, map[string]any{"url": source}, nil)
}

// Stats are the transfer counters of this session.
type Stats struct {
	RateDown      float64 `json:"rateDown"`
	RateUp        float64 `json:"rateUp"`
	ReceivedBytes int64   `json:"sessionReceivedBytes"`
	SentBytes     int64   `json:"sessionSentBytes"`
	UptimeSeconds int64   `json:"uptimeSeconds"`
}

// Stats reads the session counters.
func (c *Client) Stats(ctx context.Context) (Stats, error) {
	var out Stats
	err := c.do(ctx, http.MethodGet, "/api/v1/stats", nil, nil, &out)
	return out, err
}

// LogRecord is one line of the core's log ring.
type LogRecord struct {
	ID        int64  `json:"id"`
	Timestamp any    `json:"timestamp"`
	Severity  string `json:"severity"`
	Category  string `json:"category"`
	Message   string `json:"message"`
}

// Logs reads the newest records, oldest first.
func (c *Client) Logs(ctx context.Context, limit int) ([]LogRecord, error) {
	if limit <= 0 || limit > pageSize {
		limit = 200
	}
	var out struct {
		Items []LogRecord `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/logs", url.Values{"limit": {strconv.Itoa(limit)}, "level": {"info"}}, nil, &out)
	return out.Items, err
}

// Preferences are the values Gextto reads.
type Preferences struct {
	IncomingDir string `json:"incomingDir"`
	// MaxDownload and MaxUpload are KiB/s, 0 = unlimited.
	MaxDownload int64 `json:"maxDownload"`
	MaxUpload   int64 `json:"maxUpload"`
}

// Preferences reads the preference values.
func (c *Client) Preferences(ctx context.Context) (Preferences, error) {
	var out Preferences
	err := c.do(ctx, http.MethodGet, "/api/v1/preferences", nil, nil, &out)
	return out, err
}

// PreferenceValues reads every preference value the core reports, by schema
// key: the core answers one value per setting it has, which is how a caller
// tells the settings a given version knows from the ones it dropped.
func (c *Client) PreferenceValues(ctx context.Context) (map[string]any, error) {
	out := map[string]any{}
	err := c.do(ctx, http.MethodGet, "/api/v1/preferences", nil, nil, &out)
	return out, err
}

// PatchPreferences changes preferences by their schema key; one bad value
// and nothing is changed.
func (c *Client) PatchPreferences(ctx context.Context, values map[string]any) error {
	return c.do(ctx, http.MethodPatch, "/api/v1/preferences", nil, values, nil)
}

// SetLimits sets the speed limits in KiB/s (0 = unlimited).
func (c *Client) SetLimits(ctx context.Context, downloadKiB, uploadKiB int64) error {
	return c.do(ctx, http.MethodPatch, "/api/v1/preferences", nil, map[string]any{"maxDownload": downloadKiB, "maxUpload": uploadKiB}, nil)
}
