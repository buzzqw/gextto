package main

// ui.go serves a small web page on the daemon's own HTTP port
// (http://127.0.0.1:8890 by default). Gextto drives the daemon through the
// REST API; this page is for a human who opens that address in a browser, like
// qBittorrent's Web UI: session summary, torrent table, add (magnet or
// .torrent), search/sort, bulk actions, per-torrent details (files, peers,
// trackers, seed limits, move, pin), removal and the IP filter.
//
// It is served outside the API token middleware so a browser can reach it: when
// a token is configured the page asks for it (and remembers it in a cookie).
// The API stays protected; only this page uses the cookie.
//
// The cards and table are refreshed with a small fetch of the "live" fragment
// instead of a full page reload, so forms keep their state and there is no
// flicker. The detail of one torrent is a fragment too, loaded into a modal.

import (
	"bytes"
	"context"
	"crypto/subtle"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/webui/assets"
	"github.com/cenkalti/rain/v2/torrent"
)

type uiPageData struct {
	Version string
	Uptime  string
	Now     string
	Notice  string
	Error   bool

	Torrents int
	Down     int
	Seeding  int
	Stalled  int
	Paused   int
	Moving   int

	DownloadRate string
	UploadRate   string
	TotalDown    int64
	TotalUp      int64

	PeerPort     int
	Listen       string
	Router       string
	ExternalIP   string
	PortOpen     bool
	PeerErrors   string
	DHT          bool
	DHTNodes     int64
	UTP          bool
	Encryption   string
	Proxy        bool
	IPFilter     int
	IPFilterPath string
	// IPFilterSource prefills the IP filter field with Gextto's setting.
	IPFilterSource string
	// GexttoLog enables the Gextto log tab.
	GexttoLog     bool
	CacheReadMB   int64
	CacheWB       int64
	ReadOpsTotal  int64
	WriteOpsTotal int64
	IncomingConns int64
	LSDPeers      int64
	DiskFree      int64
	DiskTotal     int64

	CountAll     int
	CountDown    int
	CountSeeding int
	CountPaused  int
	CountStalled int
	CountMoving  int
	CountError   int

	// Categories and Tags are the qBittorrent-style labels (standalone mode).
	Categories []string
	Tags       []string
	// SearchEnabled shows the indexer search box (standalone with indexers).
	SearchEnabled bool

	Rows []uiTorrentRow
}

type uiTorrentRow struct {
	Hash      string
	Name      string
	State     string
	Progress  float64
	ProgressS string
	DoneSize  string
	Down      string
	Up        string
	Peers     int
	Seeds     int
	Ratio     string
	RatioVal  float64
	RatioGoal string
	ETA       string
	ETAVal    int64
	SavePath  string
	Category  string
	Tags      string

	TotalDone int64
	TotalSize int64
	DLRate    int64
	ULRate    int64
}

// uiFileRow, uiPeerRow and uiTrackerRow back the torrent detail tabs.
type uiFileRow struct {
	Index  int
	Path   string
	Size   int64
	Done   int64
	Wanted bool
}

type uiPeerRow struct {
	Address     string
	Client      string
	Down        int64
	Up          int64
	Progress    float64
	Seed        bool
	Incoming    bool
	Encrypted   bool
	UTP         bool
	Source      string
	Downloading bool
	ClientInt   bool
	ClientChoke bool
	PeerInt     bool
	PeerChoke   bool
	Optimistic  bool
	Snubbed     bool
	Handshake   bool
	Connected   int64
}

type uiTrackerRow struct {
	URL      string
	Status   string
	Message  string
	Seeders  int
	Leechers int
	Next     int64
}

type uiDetailData struct {
	Hash     string
	Name     string
	Tab      string
	State    string
	Error    string
	SavePath string

	Progress   float64
	TotalSize  int64
	TotalDone  int64
	Downloaded int64
	Uploaded   int64
	DownRate   int64
	UpRate     int64
	Ratio      float64
	ETA        int64

	NumPeers      int
	NumSeeds      int
	NumComplete   int
	NumIncomplete int

	SeedRatio float64
	SeedDays  int64
	// Per-torrent speed limits in KiB/s (-1 global, 0 unlimited).
	DownloadLimitKib int64
	UploadLimitKib   int64
	// Per-torrent connection/upload-slot caps (-1 global, 0 unlimited).
	MaxConnections int64
	MaxUploads     int64
	// SuperSeeding is BEP 16 super-seeding, a seeding strategy (gextto fork).
	SuperSeeding bool
	Pinned       bool
	Private      bool

	// Category and Tags are the qBittorrent-style labels (standalone).
	Category   string
	Tags       string
	Categories []string

	PiecesTotal     uint32
	PiecesHave      uint32
	PiecesAvailable uint32
	PiecesChecked   uint32
	PieceLength     int64
	Wasted          int64
	Allocated       int64
	FileCount       int
	AddedAt         int64
	CompletedAt     int64
	AddedStr        string
	CompletedStr    string
	PiecesPercent   float64
	Magnet          string

	Files    []uiFileRow
	Peers    []uiPeerRow
	Trackers []uiTrackerRow
	Pieces   []uiPieceRun
	WebSeeds []string
}

// uiPieceRun is one run of consecutive pieces sharing a state, with the width
// (percentage) used to draw the piece map.
type uiPieceRun struct {
	Begin int
	End   int
	State string
	Pct   string
}

// uiTemplates parses and compiles the web UI template once, on first use. A
// managed daemon that never serves the page pays nothing for the parse. The
// template source lives in internal/webui/assets (embed.FS), not in Go strings.
var (
	uiTemplateOnce sync.Once
	uiTemplate     *template.Template
)

func uiTemplates() *template.Template {
	uiTemplateOnce.Do(func() {
		uiTemplate = template.Must(template.New("ui").Funcs(template.FuncMap{
			"percent": func(p float64) string { return fmt.Sprintf("%.1f", p) },
			"bytes":   uiBytes,
			"rate":    uiRate,
			"dur":     uiDuration,
		}).Parse(assets.UITemplate()))
	})
	return uiTemplate
}

// handleFavicon serves the site icon; like the page shell it needs no token.
func handleFavicon(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(assets.FaviconSVG()))
}

// handleUI renders the page.
func (d *Daemon) handleUI(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	page, err := d.uiPageData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if msg := strings.TrimSpace(r.URL.Query().Get("msg")); msg != "" {
		page.Notice = msg
		page.Error = r.URL.Query().Get("ok") == "0"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	d.renderUI(w, r, "", page)
}

// handleUILive renders only the cards and the table, for the page's periodic
// partial refresh (no full reload, form state preserved).
func (d *Daemon) handleUILive(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	page, err := d.uiPageData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	d.renderUI(w, r, "fragments", page)
}

// handleUIDetail renders one tab of a torrent's detail, as a fragment for the
// modal.
// handleUIGexttoLog returns the tail of the Gextto log for the log tab. It is
// read only when the tab is opened or reloaded.
func (d *Daemon) handleUIGexttoLog(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if d.opts.GexttoLog == "" {
		http.Error(w, "no Gextto log configured", http.StatusNotFound)
		return
	}
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	lines = min(max(lines, 50), 5000)
	tail, err := tailLines(d.opts.GexttoLog, lines)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"path": d.opts.GexttoLog, "lines": tail})
}

// handleUIPortCheck renders the "Test porte" result for the status bar: a
// coloured dot (green open, amber listening but not forwarded, red closed) and
// a short explanation.
// uiPeerErrors renders the demoted peer/tracker error totals compactly, e.g.
// "handshake 12 · reset 3". Empty when there is no noise yet.
func uiPeerErrors(counts map[string]int64) string {
	if len(counts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", key, counts[key]))
	}
	return strings.Join(parts, " · ")
}

func (d *Daemon) handleUIPortCheck(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	check := d.mapper.check()
	class, label := "bad", "porta chiusa"
	switch {
	case check.Port <= 0:
		class, label = "bad", "nessuna porta peer configurata"
	case check.Open:
		class, label = "ok", "porta aperta"
	case check.Listening:
		class, label = "warn", "in ascolto, non inoltrata"
	}
	detail := check.Detail
	if check.ExternalIP != "" {
		if detail != "" {
			detail += " · "
		}
		detail += "IP " + check.ExternalIP
	}
	if check.Error != "" {
		if detail != "" {
			detail += " · "
		}
		detail += check.Error
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<span class="portdot %s"></span>%s <span class="muted">%s</span>`,
		class, template.HTMLEscapeString(label), template.HTMLEscapeString(detail))
}

func (d *Daemon) handleUIDetail(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	data, err := d.uiDetailData(r.URL.Query().Get("hash"), r.URL.Query().Get("tab"))
	if err != nil {
		http.Error(w, "torrent not found", http.StatusNotFound)
		return
	}
	// Render to a buffer first: a template error must not leave a half-written
	// fragment (the page would silently truncate).
	var buf bytes.Buffer
	if err := uiTemplates().ExecuteTemplate(&buf, "detail", data); err != nil {
		http.Error(w, "rendering error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(uiTranslateHTML(buf.String(), uiDictionary(d.uiLang(r)))))
}

// handleUITorrentFile streams the .torrent of one torrent, so the page can offer
// the export without exposing the API (the browser carries the page cookie).
func (d *Daemon) handleUITorrentFile(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	hash := strings.TrimSpace(r.URL.Query().Get("hash"))
	d.mu.Lock()
	t, _ := d.findLocked(hash)
	d.mu.Unlock()
	if t == nil {
		http.Error(w, "torrent not found", http.StatusNotFound)
		return
	}
	data, err := t.Torrent()
	if err != nil || len(data) == 0 {
		http.Error(w, "metadata not available", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/x-bittorrent")
	w.Header().Set("Content-Disposition", `attachment; filename="`+t.InfoHash().String()+`.torrent"`)
	_, _ = w.Write(data)
}

// handleUIAction runs one of the per-torrent or bulk actions. It is the same
// set Gextto uses over the API, so the page never grows its own behaviour.
func (d *Daemon) handleUIAction(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	op := strings.TrimSpace(r.FormValue("op"))
	hash := strings.TrimSpace(r.FormValue("hash"))
	var err error
	switch op {
	case "pause":
		err = d.pause(hash)
	case "resume":
		err = d.resume(hash)
	case "verify":
		err = d.verify(hash)
	case "reannounce":
		err = d.reannounce(hash)
	case "top":
		err = d.moveToTop(hash)
	case "pause-all":
		err = d.uiEach(func(h string) error { return d.pause(h) })
	case "resume-all":
		err = d.uiEach(func(h string) error { return d.resume(h) })
	case "verify-all":
		err = d.uiEach(func(h string) error { return d.verify(h) })
	default:
		err = fmt.Errorf("unknown action: %s", op)
	}
	d.uiDone(w, r, uiActionMessage(op), err)
}

// handleUIBulk applies one action to every selected torrent.
func (d *Daemon) handleUIBulk(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	op := strings.TrimSpace(r.FormValue("op"))
	hashes := r.Form["hashes"]
	if len(hashes) == 0 {
		d.uiDone(w, r, "", fmt.Errorf("no torrents selected"))
		return
	}
	var firstErr error
	for _, hash := range hashes {
		var err error
		switch op {
		case "pause":
			err = d.pause(hash)
		case "resume":
			err = d.resume(hash)
		case "verify":
			err = d.verify(hash)
		case "reannounce":
			err = d.reannounce(hash)
		case "top":
			err = d.moveToTop(hash)
		case "remove":
			err = d.remove(hash, false)
		case "remove-files":
			err = d.remove(hash, true)
		default:
			err = fmt.Errorf("unknown action: %s", op)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	d.uiDone(w, r, fmt.Sprintf("%d torrent: %s", len(hashes), uiActionMessage(op)), firstErr)
}

func (d *Daemon) handleUIRemove(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	hash := strings.TrimSpace(r.FormValue("hash"))
	// The forms send delete_files (like the API); files is the legacy name.
	deleteFiles := formBool(r, "delete_files") || formBool(r, "files")
	err := d.remove(hash, deleteFiles)
	d.uiDone(w, r, "Torrent removed", err)
}

// uiBuildAddRequest builds one add request from an add form: an uploaded
// .torrent file wins when present, otherwise a magnet link or an http(s) URL
// to a .torrent in the source field. The form must already be parsed.
func uiBuildAddRequest(r *http.Request) (addRequest, error) {
	req := addRequestFromForm(r)
	if file, _, err := r.FormFile("torrent"); err == nil {
		data, readErr := io.ReadAll(io.LimitReader(file, maxTorrentFile))
		_ = file.Close()
		if readErr != nil {
			return req, readErr
		}
		if len(bytes.TrimSpace(data)) > 0 {
			req.TorrentData = data
			return req, nil
		}
	}
	source := strings.TrimSpace(r.FormValue("source"))
	if source == "" {
		source = strings.TrimSpace(r.FormValue("magnet"))
	}
	if source == "" {
		return req, fmt.Errorf("paste a magnet link, a .torrent URL or choose a .torrent file")
	}
	lower := strings.ToLower(source)
	switch {
	case strings.HasPrefix(lower, "magnet:"):
		req.Magnet = source
	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"):
		data, err := fetchRemoteTorrent(source)
		if err != nil {
			return req, fmt.Errorf("download: %w", err)
		}
		trimmed := strings.TrimSpace(string(data))
		switch {
		case strings.HasPrefix(strings.ToLower(trimmed), "magnet:"):
			req.Magnet = trimmed
		case len(data) > 0 && data[0] == 'd':
			req.TorrentData = data
		default:
			return req, fmt.Errorf("the URL does not contain a .torrent file")
		}
	default:
		return req, fmt.Errorf("paste a magnet link, a .torrent URL or choose a .torrent file")
	}
	return req, nil
}

// uiFinishAdd stores one add request built by uiBuildAddRequest.
func (d *Daemon) uiFinishAdd(w http.ResponseWriter, r *http.Request, req addRequest, err error) {
	if err != nil {
		d.uiDone(w, r, "", err)
		return
	}
	_, existing, err := d.add(req)
	if err != nil {
		d.uiDone(w, r, "", err)
		return
	}
	msg := "Torrent added"
	if existing {
		msg = "Torrent already present"
	}
	d.uiDone(w, r, msg, nil)
}

// handleUIAdd adds a torrent from the single add form: magnet, .torrent URL
// or uploaded .torrent file, with the usual destination/pause/top options.
func (d *Daemon) handleUIAdd(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		r.Body = http.MaxBytesReader(w, r.Body, maxTorrentFile+1<<20)
		if err := r.ParseMultipartForm(maxTorrentFile); err != nil {
			d.uiDone(w, r, "", err)
			return
		}
	} else {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		_ = r.ParseForm()
	}
	req, err := uiBuildAddRequest(r)
	d.uiFinishAdd(w, r, req, err)
}

// fetchRemoteTorrent downloads a .torrent from an http(s) URL, bounded in size
// and time. A magnet link served as plain text is returned as-is so the caller
// can still add it.
func fetchRemoteTorrent(rawURL string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "gx-torrent")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxTorrentFile))
}

// handleUIAddFile is the legacy file-only counterpart of /ui/add: it accepts
// the same request, so old clients keep working while the page shows one form.
func (d *Daemon) handleUIAddFile(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxTorrentFile+1<<20)
	if err := r.ParseMultipartForm(maxTorrentFile); err != nil {
		d.uiDone(w, r, "", err)
		return
	}
	req, err := uiBuildAddRequest(r)
	d.uiFinishAdd(w, r, req, err)
}

func (d *Daemon) handleUIIPFilter(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	source := strings.TrimSpace(r.FormValue("source"))
	if source == "" {
		d.uiDone(w, r, "", fmt.Errorf("provide a URL or a file path"))
		return
	}
	path := source
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		fetched, err := d.fetchIPFilterURL(source)
		if err != nil {
			d.uiDone(w, r, "", fmt.Errorf("download: %w", err))
			return
		}
		path = fetched
	}
	rules, err := d.loadIPFilter(path)
	if err != nil {
		d.uiDone(w, r, "", err)
		return
	}
	d.uiDone(w, r, fmt.Sprintf("IP filter loaded: %d rules", rules), nil)
}

// handleUIFilePriority selects or skips one file of a torrent. rain has no
// priority levels: 0 = skip, any positive value = download.
func (d *Daemon) handleUIFilePriority(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	hash := strings.TrimSpace(r.FormValue("hash"))
	index, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("index")))
	priority, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("priority")))
	if priority < 0 {
		priority = 0
	}

	d.mu.Lock()
	t, meta := d.findLocked(hash)
	if t == nil {
		d.mu.Unlock()
		d.uiDoneDetail(w, r, hash, "files", "", errNotFound)
		return
	}
	previous := append([]int(nil), meta.FilePriorities...)
	d.mu.Unlock()

	files, err := t.Files()
	if err != nil {
		d.uiDoneDetail(w, r, hash, "files", "", fmt.Errorf("metadata not available"))
		return
	}
	priorities := make([]int, len(files))
	for i := range priorities {
		priorities[i] = 4
	}
	for i := 0; i < len(priorities) && i < len(previous); i++ {
		priorities[i] = previous[i]
	}
	if index >= 0 && index < len(priorities) {
		priorities[index] = priority
	}
	err = d.setFilePriorities(hash, priorities)
	d.uiDoneDetail(w, r, hash, "files", "File priority updated", err)
}

func (d *Daemon) handleUITrackers(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	hash := strings.TrimSpace(r.FormValue("hash"))
	if r.FormValue("op") == "remove" {
		url := strings.TrimSpace(r.FormValue("url"))
		var remaining []string
		d.mu.Lock()
		t, _ := d.findLocked(hash)
		d.mu.Unlock()
		if t != nil {
			for _, tr := range t.Trackers() {
				if tr.URL != url {
					remaining = append(remaining, tr.URL)
				}
			}
		}
		err := d.setTrackers(hash, remaining)
		d.uiDoneDetail(w, r, hash, "trackers", "Tracker removed", err)
		return
	}
	var urls []string
	for _, line := range strings.Split(r.FormValue("urls"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			urls = append(urls, line)
		}
	}
	if len(urls) == 0 {
		d.uiDoneDetail(w, r, hash, "trackers", "", fmt.Errorf("no trackers to add"))
		return
	}
	err := d.addTrackers(hash, urls)
	d.uiDoneDetail(w, r, hash, "trackers", fmt.Sprintf("%d trackers added", len(urls)), err)
}

func (d *Daemon) handleUIWebSeeds(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	hash := strings.TrimSpace(r.FormValue("hash"))
	var urls []string
	for _, line := range strings.Split(r.FormValue("urls"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			urls = append(urls, line)
		}
	}
	if len(urls) == 0 {
		d.uiDoneDetail(w, r, hash, "general", "", fmt.Errorf("no web seeds given"))
		return
	}
	var err error
	if r.FormValue("remove") == "1" {
		err = d.removeWebseeds(hash, urls)
	} else {
		err = d.addWebseeds(hash, urls)
	}
	d.uiDoneDetail(w, r, hash, "general", "Web seeds updated", err)
}

func (d *Daemon) handleUISeedLimits(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	hash := strings.TrimSpace(r.FormValue("hash"))
	ratio := formFloat(r, "seed_ratio", -1)
	days := formInt(r, "seed_days", -1)
	download := formInt(r, "download_limit", -1)
	upload := formInt(r, "upload_limit", -1)
	err := d.setLimits(hash, &download, &upload, &ratio, &days)
	maxConnections := formInt(r, "max_connections", -1)
	maxUploads := formInt(r, "max_uploads", -1)
	if err == nil {
		err = d.setConnLimits(hash, &maxConnections, &maxUploads)
	}
	d.uiDoneDetail(w, r, hash, "general", "Limits saved", err)
}

func (d *Daemon) handleUISuperSeeding(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	hash := strings.TrimSpace(r.FormValue("hash"))
	// The toggle sends enabled (like the API action); super_seeding is the
	// legacy alias from the add forms.
	enabled := formBool(r, "enabled") || formBool(r, "super_seeding")
	message := "Super-seeding off"
	if enabled {
		message = "Super-seeding on"
	}
	err := d.setSuperSeeding(hash, enabled)
	d.uiDoneDetail(w, r, hash, "general", message, err)
}

func (d *Daemon) handleUIMove(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	hash := strings.TrimSpace(r.FormValue("hash"))
	destination := strings.TrimSpace(r.FormValue("destination"))
	if destination == "" {
		d.uiDoneDetail(w, r, hash, "general", "", fmt.Errorf("specify the destination"))
		return
	}
	err := d.move(hash, destination, false)
	d.uiDoneDetail(w, r, hash, "general", "Move started", err)
}

func (d *Daemon) handleUIPin(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	hash := strings.TrimSpace(r.FormValue("hash"))
	err := d.setPin(hash, formBool(r, "pinned"))
	d.uiDoneDetail(w, r, hash, "general", "Pin updated", err)
}

func (d *Daemon) uiEach(fn func(hash string) error) error {
	var firstErr error
	for _, view := range d.snapshotViews() {
		if err := fn(view.Hash); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func uiActionMessage(op string) string {
	switch op {
	case "pause", "pause-all":
		return "Paused"
	case "resume", "resume-all":
		return "Resumed"
	case "verify", "verify-all":
		return "Recheck started"
	case "reannounce":
		return "Reannounce sent"
	case "top":
		return "Moved to the top of the queue"
	case "remove":
		return "removed (files kept)"
	case "remove-files":
		return "removed with files"
	}
	return "Done"
}

// uiDone redirects back to the page with a short message. A redirect (instead of
// rendering) keeps a refresh from repeating the action.
func (d *Daemon) uiDone(w http.ResponseWriter, r *http.Request, okMessage string, err error) {
	d.uiRedirect(w, r, nil, okMessage, err)
}

// uiDoneDetail is uiDone but reopens the torrent's detail tab after the action.
func (d *Daemon) uiDoneDetail(w http.ResponseWriter, r *http.Request, hash, tab, okMessage string, err error) {
	extra := url.Values{}
	if strings.TrimSpace(hash) != "" {
		extra.Set("open", hash)
		extra.Set("tab", tab)
	}
	d.uiRedirect(w, r, extra, okMessage, err)
}

func (d *Daemon) uiRedirect(w http.ResponseWriter, r *http.Request, extra url.Values, okMessage string, err error) {
	query := url.Values{}
	if err != nil {
		query.Set("ok", "0")
		query.Set("msg", "Error: "+err.Error())
	} else {
		query.Set("ok", "1")
		if okMessage == "" {
			okMessage = "Done"
		}
		query.Set("msg", okMessage)
	}
	for key, values := range extra {
		for _, value := range values {
			query.Set(key, value)
		}
	}
	http.Redirect(w, r, "/?"+query.Encode(), http.StatusSeeOther)
}

// uiSameOrigin rejects a mutating request coming from another site (basic CSRF
// protection; the page is often reachable on the whole LAN without a token).
func (d *Daemon) uiSameOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || !strings.EqualFold(parsed.Host, r.Host) {
		http.Error(w, "cross-origin request rejected", http.StatusForbidden)
		return false
	}
	return true
}

func (d *Daemon) uiAuthorized(w http.ResponseWriter, r *http.Request) bool {
	if d.opts.Mode == ModeStandalone {
		return d.standaloneAuthorized(w, r)
	}
	token := strings.TrimSpace(d.opts.Token)
	if token == "" {
		return true
	}
	given := r.Header.Get("X-Gx-Token")
	if given == "" {
		given = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	if given == "" {
		given = r.URL.Query().Get("token")
	}
	if given == "" {
		if cookie, err := r.Cookie("gx_token"); err == nil {
			given = cookie.Value
		}
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(given)), []byte(token)) == 1 {
		if r.URL.Query().Get("token") != "" {
			http.SetCookie(w, &http.Cookie{
				Name: "gx_token", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode,
			})
		}
		return true
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprint(w, assets.TokenPage())
	return false
}

func (d *Daemon) uiPageData() (uiPageData, error) {
	stats := d.stats()
	views := d.snapshotViews()
	page := uiPageData{
		Version:        stats.Version,
		Uptime:         uiDuration(stats.UptimeSeconds),
		Now:            time.Now().Format("15:04:05"),
		Torrents:       stats.Torrents,
		Down:           stats.Downloading,
		Seeding:        stats.Seeding,
		Stalled:        stats.Stalled,
		Paused:         stats.Paused,
		Moving:         stats.Moving,
		DownloadRate:   uiRate(stats.DownloadRate),
		UploadRate:     uiRate(stats.UploadRate),
		TotalDown:      stats.Session["bytes_downloaded"],
		TotalUp:        stats.Session["bytes_uploaded"],
		PeerPort:       stats.PeerPort,
		Listen:         stats.ListenAddress,
		DHT:            stats.DHT,
		DHTNodes:       stats.Session["dht_nodes"],
		UTP:            stats.UTP,
		Encryption:     uiEncryption(stats.Encryption),
		Proxy:          stats.Proxy,
		IPFilter:       stats.IPFilterRules,
		IPFilterPath:   stats.IPFilterPath,
		IPFilterSource: d.opts.IPFilterSource,
		GexttoLog:      d.opts.GexttoLog != "",
		CacheReadMB:    stats.CacheReadMB,
		CacheWB:        stats.CacheWriteMB,
		ReadOpsTotal:   stats.Session["read_ops_total"],
		WriteOpsTotal:  stats.Session["write_ops_total"],
		IncomingConns:  stats.Session["peers_incoming_tcp"] + stats.Session["peers_incoming_utp"],
		LSDPeers:       stats.LSD.PeersFound,
		DiskFree:       stats.DiskFreeBytes,
		DiskTotal:      stats.DiskTotalBytes,
	}
	switch {
	case stats.PortMapping.Method != "":
		page.PortOpen = true
		page.Router = strings.ToUpper(stats.PortMapping.Method)
		page.ExternalIP = stats.PortMapping.ExternalIP
	case stats.PortMapping.Error != "":
		page.Router = "not open"
	default:
		page.Router = "—"
	}
	page.PeerErrors = uiPeerErrors(stats.PeerErrors)
	for name := range d.categoriesSnapshot() {
		page.Categories = append(page.Categories, name)
	}
	sort.Strings(page.Categories)
	page.Tags = d.tagsSnapshot()
	page.SearchEnabled = d.opts.Mode == ModeStandalone && len(d.indexers()) > 0
	for _, view := range views {
		row := uiTorrentRow{
			Hash:      view.Hash,
			Name:      view.Name,
			State:     view.State,
			Progress:  view.Progress,
			ProgressS: fmt.Sprintf("%.1f%%", view.Progress),
			DoneSize:  uiBytes(view.TotalDone) + " / " + uiBytes(view.TotalSize),
			Down:      uiRate(view.DownloadRate),
			Up:        uiRate(view.UploadRate),
			Peers:     view.NumPeers,
			Seeds:     view.NumSeeds,
			ETA:       "—",
			SavePath:  view.SavePath,
			Category:  view.Category,
			Tags:      strings.Join(view.Tags, ", "),
			TotalDone: view.TotalDone,
			TotalSize: view.TotalSize,
			DLRate:    int64(view.DownloadRate),
			ULRate:    int64(view.UploadRate),
		}
		// Share ratio like qBittorrent: uploaded over what was downloaded, or
		// over the data held when it came from disk (downloaded is then 0).
		row.Ratio, row.RatioVal = "—", -1
		if base := max(view.Downloaded, view.TotalDone); base > 0 {
			row.RatioVal = float64(view.Uploaded) / float64(base)
			row.Ratio = fmt.Sprintf("%.2f", row.RatioVal)
		}
		switch {
		case view.SeedRatio > 0:
			row.RatioGoal = fmt.Sprintf("seed until ratio %.2f", view.SeedRatio)
		case view.SeedRatio == 0:
			row.RatioGoal = "seed without a ratio limit"
		default:
			row.RatioGoal = "global seed ratio"
		}
		if view.Progress < 99.99 && view.ETASeconds >= 0 {
			row.ETA = uiDuration(view.ETASeconds)
			row.ETAVal = view.ETASeconds
		}
		page.Rows = append(page.Rows, row)
	}
	sort.Slice(page.Rows, func(i, j int) bool {
		if page.Rows[i].State != page.Rows[j].State {
			return page.Rows[i].State < page.Rows[j].State
		}
		return page.Rows[i].Name < page.Rows[j].Name
	})
	page.CountAll = len(page.Rows)
	for _, row := range page.Rows {
		switch row.State {
		case "downloading", "downloading_metadata", "checking_files":
			page.CountDown++
		case "seeding":
			page.CountSeeding++
		case "stalled":
			page.CountStalled++
		case "moving":
			page.CountMoving++
		case "error":
			page.CountError++
		case "paused":
			page.CountPaused++
		}
	}
	return page, nil
}

// uiDetailData builds one torrent's detail tab. The torrent pointers are read
// under the daemon lock, the per-torrent calls (files/peers/trackers) run
// outside it, like the REST inspection does.
func (d *Daemon) uiDetailData(hash, tab string) (uiDetailData, error) {
	if tab != "files" && tab != "peers" && tab != "trackers" && tab != "pieces" {
		tab = "general"
	}
	// Sample the torrent run loop without d.mu: a torrent can be blocked on
	// storage I/O, and holding the daemon lock through the sample would stall
	// every other request.
	d.mu.Lock()
	t, _ := d.findLocked(hash)
	d.mu.Unlock()
	if t == nil {
		return uiDetailData{}, errNotFound
	}
	rawStats := t.Stats()

	d.mu.Lock()
	t, meta := d.findLocked(hash)
	if t == nil {
		d.mu.Unlock()
		return uiDetailData{}, errNotFound
	}
	id := t.ID()
	stats := d.adjustStatsLocked(t, rawStats)
	rt := d.runtimeLocked(id)
	name := stats.Name
	state := stateFor(meta, stats, d.moving[id])
	savePath := meta.SavePath
	seedRatio := meta.SeedRatio
	seedDays := meta.SeedDays
	downloadLimit := kibOrInherit(meta.DownloadLimitKib)
	uploadLimit := kibOrInherit(meta.UploadLimitKib)
	maxConnections := kibOrInherit(meta.MaxConnections)
	maxUploads := kibOrInherit(meta.MaxUploads)
	superSeeding := meta.SuperSeeding
	pinned := meta.Pinned
	swarmSeeds := meta.SwarmSeeds
	swarmPeers := meta.SwarmPeers
	metaError := meta.Error
	addedAt := meta.AddedAt.Unix()
	completedAt := int64(0)
	if !meta.CompletedAt.IsZero() {
		completedAt = meta.CompletedAt.Unix()
	}
	metaCategory := meta.Category
	metaTags := strings.Join(meta.Tags, ", ")
	d.mu.Unlock()

	progress := 0.0
	if stats.Bytes.Total > 0 {
		progress = float64(stats.Bytes.Completed) * 100 / float64(stats.Bytes.Total)
	}
	eta := int64(-1)
	if stats.ETA != nil {
		eta = int64(stats.ETA.Seconds())
	}
	ratio := 0.0
	if stats.Bytes.Downloaded > 0 {
		ratio = float64(stats.Bytes.Uploaded) / float64(stats.Bytes.Downloaded)
	}
	data := uiDetailData{
		Hash: t.InfoHash().String(), Name: name, Tab: tab, State: state, Error: metaError,
		SavePath: savePath, Progress: progress, TotalSize: stats.Bytes.Total,
		TotalDone: stats.Bytes.Completed, Downloaded: stats.Bytes.Downloaded, Uploaded: stats.Bytes.Uploaded,
		DownRate: int64(stats.Speed.Download), UpRate: int64(stats.Speed.Upload), Ratio: ratio, ETA: eta,
		NumPeers: stats.Peers.Total, NumSeeds: rt.numSeeds, NumComplete: swarmSeeds, NumIncomplete: swarmPeers,
		SeedRatio: seedRatio, SeedDays: seedDays, Pinned: pinned, Private: stats.Private,
		DownloadLimitKib: downloadLimit, UploadLimitKib: uploadLimit,
		MaxConnections: maxConnections, MaxUploads: maxUploads, SuperSeeding: superSeeding,
		PiecesTotal: stats.Pieces.Total, PiecesHave: stats.Pieces.Have, PiecesAvailable: stats.Pieces.Available,
		PiecesChecked: stats.Pieces.Checked, PieceLength: int64(stats.PieceLength),
		Wasted: stats.Bytes.Wasted, Allocated: stats.Bytes.Allocated, FileCount: stats.FileCount,
		AddedAt: addedAt, CompletedAt: completedAt,
		Category: metaCategory, Tags: metaTags,
	}
	for name := range d.categoriesSnapshot() {
		data.Categories = append(data.Categories, name)
	}
	sort.Strings(data.Categories)
	if data.PiecesTotal > 0 {
		data.PiecesPercent = float64(data.PiecesAvailable) * 100 / float64(data.PiecesTotal)
	}
	if addedAt > 0 {
		data.AddedStr = time.Unix(addedAt, 0).Format("2006-01-02 15:04")
	}
	if completedAt > 0 {
		data.CompletedStr = time.Unix(completedAt, 0).Format("2006-01-02 15:04")
	}

	trackers := t.Trackers()
	var webseeds []string
	for _, ws := range t.Webseeds() {
		webseeds = append(webseeds, ws.URL)
	}
	data.WebSeeds = webseeds
	switch tab {
	case "files":
		files, err := t.Files()
		if err != nil {
			return data, nil
		}
		done := map[int]int64{}
		if fileStats, err := t.FileStats(); err == nil {
			for index, stat := range fileStats {
				done[index] = stat.BytesCompleted
			}
		}
		d.mu.Lock()
		meta = d.metaLocked(t)
		priorities := append([]int(nil), meta.FilePriorities...)
		d.mu.Unlock()
		for index, file := range files {
			data.Files = append(data.Files, uiFileRow{
				Index: index, Path: file.Path(), Size: file.Length(), Done: done[index],
				Wanted: filePriorityMeta(priorities, index) > 0,
			})
		}
	case "peers":
		for _, peer := range t.Peers() {
			data.Peers = append(data.Peers, uiPeerRow{
				Address: peer.Addr.String(), Client: peer.Client,
				Down: int64(peer.DownloadSpeed), Up: int64(peer.UploadSpeed), Progress: peer.Progress * 100,
				Seed: peer.Seed, Incoming: peer.Source == torrent.SourceIncoming,
				Encrypted: peer.EncryptedStream, UTP: peer.UTP,
				Source: sourceName(peer.Source), Downloading: peer.Downloading,
				ClientInt: peer.ClientInterested, ClientChoke: peer.ClientChoking,
				PeerInt: peer.PeerInterested, PeerChoke: peer.PeerChoking,
				Optimistic: peer.OptimisticUnchoked, Snubbed: peer.Snubbed,
				Handshake: peer.EncryptedHandshake,
				Connected: int64(time.Since(peer.ConnectedAt).Seconds()),
			})
		}
	case "trackers":
		for _, tracker := range trackers {
			status := "not contacted"
			switch tracker.Status {
			case torrent.Working:
				status = "working"
			case torrent.Contacting:
				status = "contacting"
			case torrent.NotWorking:
				status = "not working"
			}
			message := tracker.Warning
			if tracker.Error != nil {
				message = tracker.Error.Error()
			}
			next := int64(0)
			if !tracker.NextAnnounce.IsZero() {
				next = int64(time.Until(tracker.NextAnnounce).Seconds())
			}
			data.Trackers = append(data.Trackers, uiTrackerRow{
				URL: tracker.URL, Status: status, Message: message,
				Seeders: tracker.Seeders, Leechers: tracker.Leechers, Next: next,
			})
		}
	case "pieces":
		states, ready := t.PieceStates()
		if ready {
			total := len(states)
			data.PiecesTotal = uint32(total)
			have := uint32(0)
			for index, state := range states {
				if state == "have" {
					have++
				}
				if last := len(data.Pieces) - 1; last >= 0 && data.Pieces[last].State == state {
					data.Pieces[last].End = index
					data.Pieces[last].Pct = pieceWidth(data.Pieces[last].End-data.Pieces[last].Begin+1, total)
					continue
				}
				data.Pieces = append(data.Pieces, uiPieceRun{Begin: index, End: index, State: state, Pct: pieceWidth(1, total)})
			}
			data.PiecesHave = have
		}
	}
	// Magnet link for the "copy magnet" button.
	data.Magnet = magnetLink(data.Hash, data.Name, trackers)
	return data, nil
}

// pieceWidth returns the percentage width of a run of `length` pieces over
// `total`, for the piece map.
func pieceWidth(length, total int) string {
	if total <= 0 {
		return "0"
	}
	return strconv.FormatFloat(float64(length)*100/float64(total), 'f', 3, 64)
}

// sourceName labels where a peer was discovered.
func sourceName(source torrent.PeerSource) string {
	switch source {
	case torrent.SourceTracker:
		return "tracker"
	case torrent.SourceDHT:
		return "DHT"
	case torrent.SourcePEX:
		return "PEX"
	case torrent.SourceIncoming:
		return "incoming"
	case torrent.SourceManual:
		return "manual"
	case torrent.SourceHolepunch:
		return "holepunch"
	default:
		return "—"
	}
}

// magnetLink rebuilds a magnet from the info hash, name and trackers.
func magnetLink(hash, name string, trackers []torrent.Tracker) string {
	var b strings.Builder
	b.WriteString("magnet:?xt=urn:btih:")
	b.WriteString(hash)
	if strings.TrimSpace(name) != "" {
		b.WriteString("&dn=")
		b.WriteString(url.QueryEscape(name))
	}
	for _, tracker := range trackers {
		if strings.TrimSpace(tracker.URL) == "" {
			continue
		}
		b.WriteString("&tr=")
		b.WriteString(url.QueryEscape(tracker.URL))
	}
	return b.String()
}

// filePriorityMeta reports one file's priority from a raw priority slice.
func filePriorityMeta(priorities []int, index int) int {
	if index < len(priorities) {
		if priorities[index] <= 0 {
			return 0
		}
		return priorities[index]
	}
	return 4
}

func uiBytes(n int64) string {
	if n <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	value := float64(n)
	i := 0
	for value >= 1024 && i < len(units)-1 {
		value /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.2f %s", value, units[i])
}

func uiRate(bytesPerSecond int64) string {
	if bytesPerSecond <= 0 {
		return "—"
	}
	return uiBytes(bytesPerSecond) + "/s"
}

func uiDuration(seconds int64) string {
	if seconds <= 0 {
		return "0s"
	}
	d := time.Duration(seconds) * time.Second
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	secs := int(d.Seconds()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dg %02dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %02dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm %02ds", minutes, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

func uiEncryption(level int) string {
	switch level {
	case 0:
		return "off"
	case 2:
		return "forced"
	default:
		return "on"
	}
}
