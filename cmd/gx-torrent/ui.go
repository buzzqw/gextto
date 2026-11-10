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
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
	"github.com/buzzqw/gextto/internal/webui/assets"
)

type uiPageData struct {
	Version string
	Uptime  string
	Now     string
	Notice  string
	Error   bool
	// UpdateNotes is the "updated" popup of a new version, already rendered
	// (it carries its own labels) or empty.
	UpdateNotes template.HTML `json:"-"`

	Torrents int
	Down     int
	Seeding  int
	Stalled  int
	Paused   int
	Moving   int

	DownloadRate string
	UploadRate   string
	// Raw rates (bytes/s) for the page's transfer graph.
	DownRateRaw int64
	UpRateRaw   int64
	TotalDown   int64
	TotalUp     int64

	PeerPort   int
	Listen     string
	Router     string
	ExternalIP string
	PortOpen   bool
	// PortReachable lights the peer-port dot: a router mapping, or incoming
	// peers (proof the port is reachable even when forwarded by hand).
	PortReachable bool
	PeerErrors    string
	// UTPSocket is the preformatted uTP socket dial/accept summary for the
	// status bar, empty while the socket never dialled nor accepted.
	UTPSocket string
	// UpdateAvailable lights the status bar's update indicator (standalone,
	// after a check found a newer gx-torrent); UpdateDate is its version, or
	// its build day with an older manifest.
	UpdateAvailable bool
	UpdateDate      string
	// AltSpeed lights the alternative-limits toggle in the status bar
	// (standalone): the scheduler's window or the manual toggle.
	AltSpeed     bool
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
	// CPUPercent and ProcRSS are the gx-torrent process' own CPU load (percent
	// since the previous render) and resident memory.
	CPUPercent float64
	ProcRSS    int64

	CountAll       int
	CountDown      int
	CountSeeding   int
	CountCompleted int
	CountRunning   int
	CountPaused    int
	CountActive    int
	CountInactive  int
	CountStalled   int
	CountChecking  int
	CountMoving    int
	CountError     int

	// TrackerHosts feeds the sidebar's tracker filter: the host of each
	// torrent's working tracker, with how many torrents use it. An empty
	// host groups the torrents without a working tracker.
	TrackerHosts []uiNamedCount
	// GlobalDownKib and GlobalUpKib are the configured global speed limits
	// (KiB/s, 0 = unlimited), shown and edited from the status bar.
	GlobalDownKib int64
	GlobalUpKib   int64

	// Categories and Tags are the qBittorrent-style labels (standalone mode).
	Categories []string
	Tags       []string
	// SearchEnabled shows the indexer search box (standalone with indexers).
	SearchEnabled bool
	// RowsLimited is set when only the first RowsShown of RowsTotal rows are
	// rendered, so a very large list does not build a huge DOM (standalone).
	RowsLimited bool
	RowsShown   int
	RowsTotal   int
	// FeedsEnabled shows the RSS feeds toolbar (standalone with feeds).
	FeedsEnabled bool
	FeedCount    int
	// Standalone shows the standalone navigation (torrents, RSS & indexers).
	Standalone bool
	// LoginEnabled shows the sign-out control: it is useless (and absent)
	// when the page has no password.
	LoginEnabled bool
	// LangOptions feeds the sidebar's language picker (standalone): the
	// interface languages with the endonym, the current one marked. It is not
	// part of the JSON the table refresh reads.
	LangOptions []uiLangChoice `json:"-"`

	Rows []uiTorrentRow
}

type uiTorrentRow struct {
	Hash  string
	Name  string
	State string
	// StateLabel is the human name of State (translated by the page).
	StateLabel string
	Progress   float64
	ProgressS  string
	// Size is the formatted total size (the column shows it; DoneSize keeps
	// done / total for the tooltip).
	Size      string
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
	AutoTMM   bool

	TotalDone int64
	TotalSize int64
	DLRate    int64
	ULRate    int64

	// Optional columns (the column chooser): the formatted value and the raw
	// one used to sort.
	Added         string
	AddedVal      int64
	Completed     string
	CompletedVal  int64
	Downloaded    string
	DownloadedVal int64
	Uploaded      string
	UploadedVal   int64
	Remaining     string
	RemainingVal  int64
	QueuePos      int
	Tracker       string
	SwarmSeeds    int
	SwarmPeers    int
	// Availability is the swarm availability (average pieces available, -1
	// while the metadata is missing); ActiveVal/LastActiveVal are seconds
	// since the torrent started and the Unix time of its last data movement.
	Availability    string
	AvailabilityVal float64
	Active          string
	ActiveVal       int64
	LastActive      string
	LastActiveVal   int64
	Wasted          string
	WastedVal       int64
	// Per-torrent toggles and limits, ticked in the context menu and used to
	// prefill the limits dialog (KiB/s, -1 global, 0 unlimited).
	Sequential   bool
	FirstLast    bool
	Pinned       bool
	SuperSeeding bool
	DLLimitKib   int64
	ULLimitKib   int64
}

// uiNamedCount is one sidebar filter entry with its torrent count.
type uiNamedCount struct {
	Name  string
	Count int
}

// uiFileRow, uiPeerRow and uiTrackerRow back the torrent detail tabs.
type uiFileRow struct {
	Index    int
	Path     string
	Size     int64
	Done     int64
	Wanted   bool
	Priority int
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
	// SuperSeeding is BEP 16 super-seeding, a seeding strategy.
	SuperSeeding bool
	Pinned       bool
	Private      bool

	// Category and Tags are the qBittorrent-style labels (standalone).
	Category   string
	Tags       string
	Categories []string
	// Standalone gates the standalone-only controls (category/tag editor).
	Standalone bool

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
	// AvailabilityPct is the swarm availability in percent (-1 = unknown);
	// ActiveSeconds counts since the torrent started, LastActiveVal is the
	// Unix time of its last data movement (0 = never), NextAnnounce the
	// seconds to the next tracker announce (-1 = none scheduled).
	AvailabilityPct float64
	ActiveSeconds   int64
	ActiveStr       string
	LastActiveVal   int64
	LastActiveStr   string
	NextAnnounce    int64
	SeedingSeconds  int64
	SeedingStr      string
	PiecesPercent   float64
	Magnet          string

	// Counts for the tab labels, valid on every tab: the lists themselves are
	// loaded only for the tab being shown, so a count from the list would read
	// zero on the others.
	PeerCount    int
	TrackerCount int

	Files    []uiFileRow
	Peers    []uiPeerRow
	Trackers []uiTrackerRow
	Pieces   []uiPieceRun
	// PieceBar and AvailBar are the General tab's two thin bars, at most
	// barCells cells each: the share of each stretch of pieces downloaded,
	// and the fewest connected peers holding a piece of that stretch.
	// AvailBar is empty when nothing is being downloaded; AvailMissing counts
	// the wanted pieces no connected peer has.
	// Log is the torrent's recent history (Log tab), newest first.
	Log          []uiLogRow
	PieceBar     []uiBarCell
	AvailBar     []uiBarCell
	AvailMissing int
	AvailMost    uint32
	WebSeeds     []string
	// BEP 38 hints (see bep38.go), shown only when the torrent carries some.
	HasBEP38       bool
	CollectionsStr string
	SimilarCount   int
	LocalDataStr   string
}

// uiPieceRun is one run of consecutive pieces sharing a state, with the width
// (percentage) used to draw the piece map.
// uiLogRow is one line of the Log tab.
type uiLogRow struct {
	When   string
	Msg    string
	Detail string
	Count  int
	Bad    bool
}

// uiBarCell is one cell of a thin bar: its fill (0..1, as a CSS opacity) and
// a class (have, partial, missing, skip; avail, none).
type uiBarCell struct {
	Op    string
	Class string
}

// barCells caps the cells of the General tab's bars: enough to see where the
// holes are, few enough for a torrent of tens of thousands of pieces.
const barCells = 160

// pieceBarCells groups the piece states into at most barCells stretches and
// gives each the share of its wanted pieces already downloaded.
func pieceBarCells(states []string) []uiBarCell {
	n := len(states)
	if n == 0 {
		return nil
	}
	cells := min(n, barCells)
	out := make([]uiBarCell, cells)
	for c := range out {
		lo, hi := c*n/cells, (c+1)*n/cells
		have, wanted := 0, 0
		for _, st := range states[lo:hi] {
			if st == "skipped" {
				continue
			}
			wanted++
			if st == "have" {
				have++
			}
		}
		switch {
		case wanted == 0:
			out[c] = uiBarCell{Op: "1", Class: "skip"}
		case have == wanted:
			out[c] = uiBarCell{Op: "1", Class: "have"}
		case have == 0:
			out[c] = uiBarCell{Op: "1", Class: "missing"}
		default:
			out[c] = uiBarCell{Op: strconv.FormatFloat(0.25+0.75*float64(have)/float64(wanted), 'f', 2, 64), Class: "partial"}
		}
	}
	return out
}

// availCounts groups the per-piece holder counts into at most cells stretches:
// each gives the fewest peers holding one of its wanted, missing pieces (the
// pieces already here do not need a source), or -1 when the stretch needs
// nothing. It also returns how many wanted pieces nobody connected has, and the
// highest count seen. The daemon page and Gextto's piece tab draw the same
// numbers.
func availCounts(states []string, holders []uint32, cells int) ([]int, int, uint32) {
	n := len(holders)
	if n == 0 || len(states) != n || cells <= 0 {
		return nil, 0, 0
	}
	var most uint32
	for _, h := range holders {
		most = max(most, h)
	}
	missing := 0
	cells = min(n, cells)
	out := make([]int, cells)
	for c := range out {
		lo, hi := c*n/cells, (c+1)*n/cells
		fewest, needed := uint32(0), false
		for i := lo; i < hi; i++ {
			if states[i] == "have" || states[i] == "skipped" {
				continue
			}
			if !needed || holders[i] < fewest {
				fewest = holders[i]
			}
			needed = true
			if holders[i] == 0 {
				missing++
			}
		}
		out[c] = -1
		if needed {
			out[c] = int(fewest)
		}
	}
	return out, missing, most
}

// availBarCells turns availCounts into the cells of the General tab's
// availability bar.
func availBarCells(states []string, holders []uint32) ([]uiBarCell, int, uint32) {
	counts, missing, most := availCounts(states, holders, barCells)
	if counts == nil {
		return nil, 0, 0
	}
	out := make([]uiBarCell, len(counts))
	for c, fewest := range counts {
		switch {
		case fewest < 0:
			out[c] = uiBarCell{Op: "1", Class: "done"}
		case fewest == 0:
			out[c] = uiBarCell{Op: "1", Class: "none"}
		default:
			op := 0.3 + 0.7*float64(fewest)/float64(max(most, 1))
			out[c] = uiBarCell{Op: strconv.FormatFloat(min(op, 1), 'f', 2, 64), Class: "avail"}
		}
	}
	return out, missing, most
}

type uiPieceRun struct {
	Begin int
	End   int
	State string
	Pct   string
}

// The web UI templates are parsed per page and language in i18n_tr.go: the
// template source lives in internal/webui/assets (embed.FS), not in Go strings,
// and `tr` has to be bound to the dictionary of the language in force.

// handleFavicon serves the site icon; like the page shell it needs no token.
func handleFavicon(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(assets.FaviconSVG()))
}

// handleStaticAsset serves an embedded asset with a strong ETag, so a browser
// revalidates it (304) instead of downloading it again on every page, and a
// new build is picked up at once.
func handleStaticAsset(contentType string, body func() string) http.HandlerFunc {
	var (
		once sync.Once
		data []byte
		etag string
	)
	return func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() {
			data = []byte(body())
			sum := sha256.Sum256(data)
			etag = `"` + hex.EncodeToString(sum[:8]) + `"`
		})
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write(data)
	}
}

// handleUI renders the page (or the alternative interface when enabled).
func (d *Daemon) handleUI(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	// With the alternative interface on, the root address serves it; /ui keeps
	// the standard page (the way back).
	if r.URL.Path == "/" && d.altWebUIEnabled() && d.altWebUIAvailable() {
		d.handleAltWebUI(w, r)
		return
	}
	lang := d.uiLang(r)
	page, err := d.uiPageData(d.uiRowLimit(r), lang)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if page.Standalone {
		page.LangOptions = uiLanguageChoices(lang)
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
	name := "fragments"
	if r.URL.Query().Get("notable") == "1" {
		// The table travels as JSON (/ui/rows); the chrome (filters,
		// speeds, status) still travels as HTML, translated by the server.
		name = "fragments-notable"
	}
	page, err := d.uiPageData(d.uiRowLimit(r), d.uiLang(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	d.renderUI(w, r, name, page)
}

// handleUIRows renders the torrent table as JSON, for the page's periodic
// refresh: a row list is several times smaller than the same rows as HTML,
// and the browser updates the cells without rebuilding the page chrome.
func (d *Daemon) handleUIRows(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	page, err := d.uiPageData(d.uiRowLimit(r), d.uiLang(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, page)
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

// handleUILog returns the tail of the daemon's own recent log lines for the
// standalone log view. It reads the in-memory ring, so it works whether the log
// goes to stderr or to the rotating file.
func (d *Daemon) handleUILog(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	lines = min(max(lines, 50), 5000)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"path": logFilePath, "lines": logRing.tail(lines)})
}

// handleUIPortCheck renders the "Test porte" result for the status bar: a
// coloured dot (green open, amber listening but not forwarded, red closed) and
// a short explanation.
// uiPeerErrors renders the demoted peer/tracker error totals compactly, e.g.
// "handshake 12 · reset 3". Empty when there is no noise yet.
func uiPeerErrors(lang string, counts map[string]int64) string {
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
		parts = append(parts, uiTr(lang, "%s %d", key, counts[key]))
	}
	return strings.Join(parts, " · ")
}

// uiUTPSocketSummary renders the uTP socket counter summary for the status
// bar, or ("", false) while the socket never dialled nor accepted. Resends and
// dropped SYNs are the congestion/burst signals and appear only when non-zero.
func uiUTPSocketSummary(lang string, dials, accepts, dialErrs, acceptErrs, resendsTimeout, resendsFast, synDrops int64) (string, bool) {
	if dials <= 0 && accepts <= 0 {
		return "", false
	}
	out := uiTr(lang, "%d dial / %d accept", dials, accepts)
	if dialErrs+acceptErrs > 0 {
		out += uiTr(lang, " · errors: %d dial, %d accept", dialErrs, acceptErrs)
	}
	if resendsTimeout+resendsFast > 0 {
		out += uiTr(lang, " · resends: %d RTO, %d fast", resendsTimeout, resendsFast)
	}
	if synDrops > 0 {
		out += uiTr(lang, " · SYN drops: %d", synDrops)
	}
	return out, true
}

func (d *Daemon) handleUIPortCheck(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	targetPort := 0
	if p := strings.TrimSpace(r.URL.Query().Get("port")); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 && n <= 65535 {
			targetPort = n
		}
	}
	check := d.checkPort(targetPort)
	class, label := "bad", "port closed"
	switch {
	case check.Port <= 0:
		class, label = "bad", "no peer port configured"
	case check.Open:
		class, label = "ok", "port open"
	case check.Listening:
		class, label = "warn", "listening, not forwarded"
	}
	label = uiTr(d.uiLang(r), label)
	detail := check.Detail
	if check.ExternalIP != "" {
		if detail != "" {
			detail += " · "
		}
		detail += "IP " + check.ExternalIP
	}
	if check.ReflectedPort > 0 {
		if detail != "" {
			detail += " · "
		}
		detail += natHint(check)
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
	lang := d.uiLang(r)
	data, err := d.uiDetailData(r.URL.Query().Get("hash"), r.URL.Query().Get("tab"), lang)
	if err != nil {
		http.Error(w, "torrent not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	d.renderUI(w, r, "detail", data)
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
		case "up":
			err = d.shiftPriority(hash, true)
		case "down":
			err = d.shiftPriority(hash, false)
		case "remove":
			err = d.remove(hash, false)
		case "remove-files":
			err = d.remove(hash, true)
		case "bottom":
			err = d.moveToBottom(hash)
		case "force-start":
			// qBittorrent's force start is the daemon's pin: the queue does
			// not rotate the torrent out. Resume it too, like qBittorrent.
			if err = d.setPin(hash, true); err == nil {
				err = d.resume(hash)
			}
		case "unforce":
			err = d.setPin(hash, false)
		case "sequential":
			err = d.toggleSequential(hash)
		case "first-last":
			err = d.toggleFirstLast(hash)
		case "tmm-on":
			err = d.setAutoManagement(hash, true)
		case "tmm-off":
			err = d.setAutoManagement(hash, false)
		case "super-seed-on":
			err = d.setSuperSeeding(hash, true)
		case "super-seed-off":
			err = d.setSuperSeeding(hash, false)
		default:
			err = fmt.Errorf("unknown action: %s", op)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	// The count and the action are two values inside one sentence: the phrase
	// is translated first and filled after, so a translator can order them.
	d.uiDonef(w, r, firstErr, "%d torrent: %s", len(hashes), uiTr(d.uiLang(r), uiActionMessage(op)))
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

// handleUIRename renames a torrent: its display name and content root.
func (d *Daemon) handleUIRename(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	hash := strings.TrimSpace(r.FormValue("hash"))
	name := strings.TrimSpace(r.FormValue("name"))
	var err error
	if hash == "" || name == "" {
		err = fmt.Errorf("a name is required")
	} else {
		err = d.renameTorrent(hash, name)
	}
	d.uiDone(w, r, "Renamed", err)
}

// handleUIRenameFile renames one file of a torrent (from the Files tab).
func (d *Daemon) handleUIRenameFile(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	hash := strings.TrimSpace(r.FormValue("hash"))
	oldPath := strings.TrimSpace(r.FormValue("old"))
	newPath := strings.TrimSpace(r.FormValue("new"))
	var err error
	if hash == "" || oldPath == "" || newPath == "" {
		err = fmt.Errorf("hash, old and new are required")
	} else {
		err = d.renameFile(hash, oldPath, newPath)
	}
	d.uiDoneDetail(w, r, hash, "files", "File renamed", err)
}

// handleUIRenameFolder renames one top folder of a torrent (from the grouped
// Files tab). Like every rename it moves the content on disk and never
// changes the torrent identity.
func (d *Daemon) handleUIRenameFolder(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	hash := strings.TrimSpace(r.FormValue("hash"))
	oldPath := strings.TrimSpace(r.FormValue("old"))
	newPath := strings.TrimSpace(r.FormValue("new"))
	var err error
	if hash == "" || oldPath == "" || newPath == "" {
		err = fmt.Errorf("hash, old and new are required")
	} else {
		err = d.renameFolder(hash, oldPath, newPath)
	}
	d.uiDoneDetail(w, r, hash, "files", "Folder renamed", err)
}

// handleUIAltSpeed toggles the manual alternative speed limits (the qBittorrent
// speed-limit button).
func (d *Daemon) handleUIAltSpeed(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	if d.opts.Settings == nil {
		d.uiDone(w, r, "", fmt.Errorf("not available"))
		return
	}
	_, _, manual := d.altSpeedSettings()
	d.opts.Settings.Set(altSpeedEnabledKey, strconv.FormatBool(!manual))
	err := d.opts.Settings.Save()
	d.applyScheduleNow()
	d.uiDone(w, r, "Alternative limits toggled", err)
}

// uiBuildAddRequest builds one add request from an add form: an uploaded
// .torrent file wins when present, otherwise a magnet link or an http(s) URL
// to a .torrent in the source field. The form must already be parsed.
func uiBuildAddRequest(d *Daemon, r *http.Request) (addRequest, error) {
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
		data, magnet, err := d.fetchTorrentSource(source)
		if err != nil {
			return req, fmt.Errorf("download: %w", err)
		}
		if magnet != "" {
			req.Magnet = magnet
		} else {
			req.TorrentData = data
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
	// The page's "Default" layout is the one chosen in the settings.
	if req.ContentLayout == "" {
		req.ContentLayout = d.defaultContentLayout()
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
	req, err := uiBuildAddRequest(d, r)
	d.uiFinishAdd(w, r, req, err)
}

// handleUIAddPreview answers the add dialog with the file tree of a .torrent,
// so the operator can choose what to download before the torrent is added.
// A magnet has no metadata yet: the dialog says the list will appear when it
// arrives (the Files tab after the add).
func (d *Daemon) handleUIAddPreview(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		r.Body = http.MaxBytesReader(w, r.Body, maxTorrentFile+1<<20)
		if err := r.ParseMultipartForm(maxTorrentFile); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		_ = r.ParseForm()
	}
	req, err := uiBuildAddRequest(d, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	lang := d.uiLang(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if req.Magnet != "" {
		_, _ = io.WriteString(w, `<p class="muted">`+html.EscapeString(uiTr(lang, "The file list appears when the metadata arrives."))+`</p>`)
		return
	}
	files, err := torrent.PreviewFiles(req.TorrentData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var b strings.Builder
	b.WriteString(`<div class="filetree"><p class="muted">` + html.EscapeString(uiTr(lang, "Select the files to download; unchecked files are skipped.")) + `</p>`)
	b.WriteString(`<div class="row"><button type="button" class="ghost sm" data-file-all="1" title="` + html.EscapeString(uiTr(lang, "Select every file")) + `">` + html.EscapeString(uiTr(lang, "Select all")) + `</button>`)
	b.WriteString(`<button type="button" class="ghost sm" data-file-none="1" title="` + html.EscapeString(uiTr(lang, "Select no file")) + `">` + html.EscapeString(uiTr(lang, "Select none")) + `</button></div>`)
	prioTitle := html.EscapeString(uiTr(lang, "Download priority of this file: higher priorities are fetched first"))
	prioOptions := `<option value="4">` + html.EscapeString(uiTr(lang, "Normal")) + `</option><option value="6">` +
		html.EscapeString(uiTr(lang, "High")) + `</option><option value="7">` + html.EscapeString(uiTr(lang, "Maximum")) + `</option>`
	for i, f := range files {
		idx := strconv.Itoa(i)
		b.WriteString(`<div class="addfile">`)
		b.WriteString(`<label class="chk" title="` + html.EscapeString(uiTr(lang, "Check this file to download it; leave it unchecked to skip it")) + `">`)
		b.WriteString(`<input type="checkbox" data-file-index="` + idx + `" checked> `)
		b.WriteString(html.EscapeString(f.Name) + ` <span class="muted">` + humanBytes(f.Length) + `</span></label>`)
		b.WriteString(`<select class="sm" data-file-prio="` + idx + `" aria-label="` + html.EscapeString(uiTr(lang, "Priority")+": "+f.Name) + `" title="` + prioTitle + `">` + prioOptions + `</select>`)
		b.WriteString(`</div>`)
	}
	b.WriteString(`</div>`)
	_, _ = io.WriteString(w, b.String())
}

// handleUIDhtScrape returns the BEP 33 DHT swarm estimate (seeders, peers) for
// one torrent, for the page's "DHT swarm" button.
func (d *Daemon) handleUIDhtScrape(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	d.mu.Lock()
	t, _ := d.findLocked(strings.TrimSpace(r.URL.Query().Get("hash")))
	d.mu.Unlock()
	if t == nil {
		http.Error(w, "torrent not found", http.StatusNotFound)
		return
	}
	seeders, peers, err := t.DHTScrape(6 * time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"seeders": seeders, "peers": peers})
}

// handleUICreateTorrent builds a .torrent from a folder or file on the server
// and returns it as a download: the standalone convenience mirror of mktorrent.
// The path is typed by the operator (the page is authenticated).
func (d *Daemon) handleUICreateTorrent(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	_ = r.ParseForm()
	path := strings.TrimSpace(r.FormValue("path"))
	name := strings.TrimSpace(r.FormValue("name"))
	var trackers []string
	for _, line := range strings.Split(r.FormValue("trackers"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			trackers = append(trackers, line)
		}
	}
	var webseeds []string
	for _, line := range strings.Split(r.FormValue("webseeds"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			webseeds = append(webseeds, line)
		}
	}
	// The piece size is chosen in KiB; 0 lets the engine pick one from the
	// total size. Anything below 16 KiB or not a multiple of it is ignored,
	// because the BitTorrent piece layout cannot use it.
	var pieceLength uint32
	if kib := formInt(r, "piece_size", 0); kib >= 16 && kib <= 16384 && kib%16 == 0 {
		pieceLength = uint32(kib) * 1024
	}
	data, err := torrent.CreateTorrent(path, torrent.CreateOptions{
		Name:        name,
		Trackers:    trackers,
		Webseeds:    webseeds,
		Private:     formBool(r, "private"),
		PieceLength: pieceLength,
		Comment:     strings.TrimSpace(r.FormValue("comment")),
		Format:      strings.TrimSpace(r.FormValue("format")),
		Source:      strings.TrimSpace(r.FormValue("source")),
	})
	if err != nil {
		d.uiDone(w, r, "", err)
		return
	}
	if formBool(r, "start_seeding") {
		base := filepath.Base(filepath.Clean(path))
		if name != "" && name != base {
			d.uiDone(w, r, "", errors.New("start seeding needs the torrent name to match the folder or file name"))
			return
		}
		if _, _, err := d.add(addRequest{
			TorrentData: data,
			Destination: filepath.Dir(filepath.Clean(path)),
			SeedMode:    true,
			SeedRatio:   -1,
			SeedDays:    -1,
		}); err != nil {
			d.uiDone(w, r, "", err)
			return
		}
		d.uiDone(w, r, "Torrent created and added to the session", nil)
		return
	}
	if name == "" {
		name = path
	}
	name = safeFilename(name)
	w.Header().Set("Content-Type", "application/x-bittorrent")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.torrent"`)
	_, _ = w.Write(data)
}

// safeFilename turns a path (or a suggested name) into a bare file name free of
// separators and characters that would break the Content-Disposition header.
func safeFilename(name string) string {
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == '"' || r == '\\' || r == '/' || r == ':' {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		return "torrent"
	}
	return name
}

// handleUIEvents streams session events (torrent added/completed/stopped/error)
// as Server-Sent Events, so a client can react instead of polling. The
// subscription is cancelled when the client disconnects or the session is
// replaced (sse.go).
func (d *Daemon) handleUIEvents(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	d.streamSessionEvents(w, r, "")
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
	req, err := uiBuildAddRequest(d, r)
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
	// The settings page's "Load now" posts its own form: the source is its
	// "ipfilter" field and the answer goes back to that page.
	fromSettings := r.FormValue("return") == "settings"
	done := func(err error, message string, args ...any) {
		if !fromSettings {
			d.uiDonef(w, r, err, message, args...)
			return
		}
		ok := "1"
		msg := uiTr(d.uiLang(r), message, args...)
		if err != nil {
			ok, msg = "0", uiTr(d.uiLang(r), "Error: %s", err.Error())
		}
		http.Redirect(w, r, uiSettingsPath+"?msg="+url.QueryEscape(msg)+"&ok="+ok+"#ipfilter", http.StatusSeeOther)
	}
	source := strings.TrimSpace(r.FormValue("source"))
	if source == "" {
		source = strings.TrimSpace(r.FormValue("ipfilter"))
	}
	if source == "" {
		done(fmt.Errorf("provide a URL or a file path"), "")
		return
	}
	rules, err := d.loadIPFilterSource(source)
	if err != nil {
		done(err, "")
		return
	}
	done(nil, "IP filter loaded: %d rules", rules)
}

// handleUIFilePriority sets one file's priority. 0 = skip, 1..4 = normal,
// 5..7 = high (downloaded before the rest).
func (d *Daemon) handleUIFilePriority(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	if !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	hash := strings.TrimSpace(r.FormValue("hash"))
	priority, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("priority")))
	if priority < 0 {
		priority = 0
	}
	if priority > 7 {
		priority = 7
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
		priorities[i] = normalFilePriority
	}
	for i := 0; i < len(priorities) && i < len(previous); i++ {
		priorities[i] = previous[i]
	}
	// One select sends one index; the Files tab's bulk bar sends several.
	for _, raw := range r.Form["index"] {
		if index, convErr := strconv.Atoi(strings.TrimSpace(raw)); convErr == nil && index >= 0 && index < len(priorities) {
			priorities[index] = priority
		}
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
	if r.FormValue("op") == "edit" {
		oldURL := strings.TrimSpace(r.FormValue("old"))
		newURL := strings.TrimSpace(r.FormValue("new"))
		if newURL == "" {
			d.uiDoneDetail(w, r, hash, "trackers", "", fmt.Errorf("the tracker URL is required"))
			return
		}
		err := d.editTracker(hash, oldURL, newURL)
		d.uiDoneDetail(w, r, hash, "trackers", "Tracker updated", err)
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
	d.uiDoneDetailf(w, r, err, hash, "trackers", "%d trackers added", len(urls))
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
	case "up":
		return "Moved up in the queue"
	case "down":
		return "Moved down in the queue"
	case "remove":
		return "removed (files kept)"
	case "remove-files":
		return "removed with files"
	case "bottom":
		return "Moved to the bottom of the queue"
	case "force-start":
		return "Force started"
	case "unforce":
		return "Back under the queue"
	case "sequential":
		return "Sequential download toggled"
	case "first-last":
		return "First and last pieces toggled"
	case "tmm-on", "tmm-off":
		return "Automatic management updated"
	case "super-seed-on", "super-seed-off":
		return "Super-seeding updated"
	}
	return "Done"
}

// uiDone redirects back to the page with a short message. A redirect (instead of
// rendering) keeps a refresh from repeating the action.
func (d *Daemon) uiDone(w http.ResponseWriter, r *http.Request, okMessage string, err error) {
	d.uiRedirect(w, r, nil, okMessage, nil, err)
}

// uiDonef is uiDone for a message with a value inside the phrase: the phrase is
// translated first and the values are filled after, so a translator can move
// them where the language wants them.
func (d *Daemon) uiDonef(w http.ResponseWriter, r *http.Request, err error, okMessage string, args ...any) {
	d.uiRedirect(w, r, nil, okMessage, args, err)
}

// uiDoneDetail is uiDone but reopens the torrent's detail tab after the action.
func (d *Daemon) uiDoneDetail(w http.ResponseWriter, r *http.Request, hash, tab, okMessage string, err error) {
	d.uiDoneDetailf(w, r, err, hash, tab, okMessage)
}

// uiDoneDetailf is uiDoneDetail for a message with values inside the phrase.
func (d *Daemon) uiDoneDetailf(w http.ResponseWriter, r *http.Request, err error, hash, tab, okMessage string, args ...any) {
	extra := url.Values{}
	if strings.TrimSpace(hash) != "" {
		extra.Set("open", hash)
		extra.Set("tab", tab)
	}
	d.uiRedirect(w, r, extra, okMessage, args, err)
}

func (d *Daemon) uiRedirect(w http.ResponseWriter, r *http.Request, extra url.Values, okMessage string, args []any, err error) {
	lang := d.uiLang(r)
	msg := ""
	if err != nil {
		msg = uiTr(lang, "Error: %s", err.Error())
	} else {
		if okMessage == "" {
			okMessage = "Done"
		}
		msg = uiTr(lang, okMessage, args...)
	}
	// The page's own script posts with X-GX-Fetch and wants the outcome, not
	// a reload: answer with JSON.
	if r.Header.Get("X-GX-Fetch") == "1" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": err == nil, "msg": msg})
		return
	}
	query := url.Values{}
	query.Set("ok", "1")
	if err != nil {
		query.Set("ok", "0")
	}
	query.Set("msg", msg)
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

func (d *Daemon) uiPageData(limit int, lang string) (uiPageData, error) {
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
		DownRateRaw:    stats.DownloadRate,
		UpRateRaw:      stats.UploadRate,
		TotalDown:      stats.Session["bytes_downloaded"],
		TotalUp:        stats.Session["bytes_uploaded"],
		PeerPort:       stats.PeerPort,
		Listen:         stats.ListenAddress,
		DHT:            stats.DHT,
		DHTNodes:       stats.Session["dht_nodes"],
		UTP:            stats.UTP,
		Encryption:     uiTr(lang, uiEncryption(stats.Encryption)),
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
	page.UTPSocket, _ = uiUTPSocketSummary(lang,
		stats.Session["utp_dials"], stats.Session["utp_accepts"],
		stats.Session["utp_dial_errors"], stats.Session["utp_accept_errors"],
		stats.Session["utp_resends_timeout"], stats.Session["utp_resends_fast"],
		stats.Session["utp_syn_drops"])
	page.CPUPercent, page.ProcRSS = d.processUsage(time.Now())
	if d.opts.Mode == ModeStandalone {
		page.Standalone = true
		page.LoginEnabled = d.loginEnabled()
		update := d.updateStatus()
		page.UpdateAvailable, page.UpdateDate = update.Available && !update.Applying, update.LatestDate
		page.AltSpeed = d.altSpeedActive(time.Now())
		if update.LatestVersion != "" {
			page.UpdateDate = update.LatestVersion
		}
	}
	switch {
	case stats.PortMapping.Method != "":
		page.PortOpen = true
		page.Router = strings.ToUpper(stats.PortMapping.Method)
		page.ExternalIP = stats.PortMapping.ExternalIP
	case stats.PortMapping.Error != "":
		page.Router = uiTr(lang, "not open")
	default:
		page.Router = "—"
	}
	page.PortReachable = page.PortOpen || page.IncomingConns > 0
	page.PeerErrors = uiPeerErrors(lang, stats.PeerErrors)
	for name := range d.categoriesSnapshot() {
		page.Categories = append(page.Categories, name)
	}
	sort.Strings(page.Categories)
	page.Tags = d.tagsSnapshot()
	page.SearchEnabled = d.opts.Mode == ModeStandalone && len(d.indexers()) > 0
	if d.opts.Mode == ModeStandalone {
		page.Standalone = true
		if feeds := d.feeds(); len(feeds) > 0 {
			page.FeedsEnabled = true
			page.FeedCount = len(feeds)
		}
	}
	for _, view := range views {
		row := uiTorrentRow{
			Hash:      view.Hash,
			Name:      view.Name,
			State:     view.State,
			Progress:  view.Progress,
			ProgressS: fmt.Sprintf("%.1f%%", view.Progress),
			DoneSize:  uiBytes(view.TotalDone) + " / " + uiBytes(view.TotalSize),
			Size:      uiBytes(view.TotalSize),
			Down:      uiRate(view.DownloadRate),
			Up:        uiRate(view.UploadRate),
			Peers:     view.NumPeers,
			Seeds:     view.NumSeeds,
			ETA:       "—",
			SavePath:  view.SavePath,
			Category:  view.Category,
			AutoTMM:   view.AutoTMM,
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
			row.RatioGoal = uiTr(lang, "seed until ratio %.2f", view.SeedRatio)
		case view.SeedRatio == 0:
			row.RatioGoal = uiTr(lang, "seed without a ratio limit")
		default:
			row.RatioGoal = uiTr(lang, "global seed ratio")
		}
		if view.DiskWait {
			// Paused by the disk guard: shown apart from a user pause.
			row.State = "disk_wait"
		}
		row.StateLabel = uiTr(lang, uiStateLabel(row.State))
		if view.Progress < 99.99 && view.ETASeconds >= 0 {
			row.ETA = uiDuration(view.ETASeconds)
			row.ETAVal = view.ETASeconds
		}
		uiFillOptionalColumns(&row, view)
		page.Rows = append(page.Rows, row)
	}
	sort.Slice(page.Rows, func(i, j int) bool {
		if page.Rows[i].State != page.Rows[j].State {
			return page.Rows[i].State < page.Rows[j].State
		}
		return page.Rows[i].Name < page.Rows[j].Name
	})
	page.CountAll = len(page.Rows)
	trackers := map[string]int{}
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
		case "paused", "disk_wait":
			page.CountPaused++
		}
		// The same predicates as the page's status filter (matchStatus).
		if row.State == "checking_files" {
			page.CountChecking++
		}
		if row.Progress >= 100 {
			page.CountCompleted++
		}
		if row.State != "paused" && row.State != "disk_wait" {
			page.CountRunning++
		}
		if row.DLRate > 0 || row.ULRate > 0 {
			page.CountActive++
		} else {
			page.CountInactive++
		}
		trackers[row.Tracker]++
	}
	for host, n := range trackers {
		page.TrackerHosts = append(page.TrackerHosts, uiNamedCount{Name: host, Count: n})
	}
	sort.Slice(page.TrackerHosts, func(i, j int) bool {
		// The torrents without a working tracker come last.
		a, b := page.TrackerHosts[i].Name, page.TrackerHosts[j].Name
		if (a == "") != (b == "") {
			return b == ""
		}
		return a < b
	})
	cfg, _, _ := d.config()
	page.GlobalDownKib, page.GlobalUpKib = cfg.SpeedLimitDownload, cfg.SpeedLimitUpload
	page.RowsTotal = len(page.Rows)
	if limit > 0 && len(page.Rows) > limit {
		page.Rows = page.Rows[:limit]
		page.RowsLimited = true
		page.RowsShown = limit
	}
	return page, nil
}

// uiFillOptionalColumns fills the columns a user can add with the column
// chooser (added/completed dates, transferred bytes, queue, tracker, swarm)
// and the per-torrent toggles the context menu ticks.
func uiFillOptionalColumns(row *uiTorrentRow, view torrentInfo) {
	row.AddedVal, row.CompletedVal = view.AddedAt, view.CompletedAt
	row.Added, row.Completed = "—", "—"
	if view.AddedAt > 0 {
		row.Added = time.Unix(view.AddedAt, 0).Format("2006-01-02 15:04")
	}
	if view.CompletedAt > 0 {
		row.Completed = time.Unix(view.CompletedAt, 0).Format("2006-01-02 15:04")
	}
	row.DownloadedVal, row.UploadedVal = view.Downloaded, view.Uploaded
	row.Downloaded, row.Uploaded = uiBytes(view.Downloaded), uiBytes(view.Uploaded)
	row.RemainingVal = max(view.TotalSize-view.TotalDone, 0)
	row.Remaining = uiBytes(row.RemainingVal)
	row.QueuePos = view.QueuePosition
	row.Tracker = uiTrackerHost(view.CurrentTracker)
	row.SwarmSeeds, row.SwarmPeers = view.NumComplete, view.NumIncomplete
	row.Sequential, row.FirstLast = view.Sequential, view.FirstLast
	row.Pinned, row.SuperSeeding = view.Pinned, view.SuperSeeding
	row.DLLimitKib, row.ULLimitKib = view.DownloadLimitKib, view.UploadLimitKib
	// Availability (qBittorrent semantics: average pieces available).
	row.AvailabilityVal = -1
	row.Availability = "—"
	if view.PiecesTotal > 0 {
		row.AvailabilityVal = float64(view.PiecesAvailable) / float64(view.PiecesTotal)
		row.Availability = fmt.Sprintf("%.2f", row.AvailabilityVal)
	}
	row.ActiveVal = view.ActiveSeconds
	row.Active = "—"
	if view.ActiveSeconds > 0 {
		row.Active = uiDuration(view.ActiveSeconds)
	}
	row.LastActiveVal = view.LastActive
	row.LastActive = "—"
	if view.LastActive > 0 {
		row.LastActive = time.Unix(view.LastActive, 0).Format("2006-01-02 15:04")
	}
	row.WastedVal = view.Wasted
	row.Wasted = uiBytes(view.Wasted)
}

// uiTrackerHost is the host of a tracker URL ("" for none), the name the
// tracker filter groups torrents by.
func uiTrackerHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return raw
}

// defaultUIRowLimit bounds the rows a standalone page renders at once, so a
// list of thousands of torrents does not build a huge DOM on every refresh.
const defaultUIRowLimit = 300

// uiRowLimit is the render window for this request: 0 (all rows) in managed,
// which must stay identically rendered, or the default/`?rows=` window in
// standalone.
func (d *Daemon) uiRowLimit(r *http.Request) int {
	if d.opts.Mode != ModeStandalone {
		return 0
	}
	raw := strings.TrimSpace(r.URL.Query().Get("rows"))
	if raw == "" {
		return defaultUIRowLimit
	}
	// "all" asks for the whole session: the page then virtualises the table
	// client-side (the JSON is small, the DOM stays bounded).
	if strings.EqualFold(raw, "all") {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultUIRowLimit
	}
	if n > 100000 {
		n = 100000
	}
	return n
}

// uiDetailData builds one torrent's detail tab. The torrent pointers are read
// under the daemon lock, the per-torrent calls (files/peers/trackers) run
// outside it, like the REST inspection does.
func (d *Daemon) uiDetailData(hash, tab, lang string) (uiDetailData, error) {
	if tab != "files" && tab != "peers" && tab != "trackers" && tab != "pieces" && tab != "speed" && tab != "log" {
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
	// Volatile timings, copied under the lock (the tick rewrites them).
	activeSeconds := int64(0)
	if !rt.startedAt.IsZero() {
		activeSeconds = int64(time.Since(rt.startedAt).Seconds())
	}
	lastActive := int64(0)
	if !rt.lastActive.IsZero() {
		lastActive = rt.lastActive.Unix()
	}
	seedingSeconds := int64(stats.SeededFor.Seconds())
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
	data.Standalone = d.opts.Mode == ModeStandalone
	if data.PiecesTotal > 0 {
		data.PiecesPercent = float64(data.PiecesAvailable) * 100 / float64(data.PiecesTotal)
	}
	data.AvailabilityPct = -1
	if data.PiecesTotal > 0 {
		data.AvailabilityPct = float64(data.PiecesAvailable) / float64(data.PiecesTotal)
	}
	data.ActiveSeconds = activeSeconds
	if activeSeconds > 0 {
		data.ActiveStr = uiDuration(activeSeconds)
	}
	data.LastActiveVal = lastActive
	if lastActive > 0 {
		data.LastActiveStr = time.Unix(lastActive, 0).Format("2006-01-02 15:04")
	}
	data.SeedingSeconds = seedingSeconds
	if seedingSeconds > 0 {
		data.SeedingStr = uiDuration(seedingSeconds)
	}
	data.NextAnnounce = -1
	if addedAt > 0 {
		data.AddedStr = time.Unix(addedAt, 0).Format("2006-01-02 15:04")
	}
	if completedAt > 0 {
		data.CompletedStr = time.Unix(completedAt, 0).Format("2006-01-02 15:04")
	}

	trackers := t.Trackers()
	data.PeerCount = len(t.Peers())
	data.TrackerCount = len(trackers)
	for _, tracker := range trackers {
		if !tracker.NextAnnounce.IsZero() {
			if next := int64(time.Until(tracker.NextAnnounce).Seconds()); next >= 0 &&
				(data.NextAnnounce < 0 || next < data.NextAnnounce) {
				data.NextAnnounce = next
			}
		}
	}
	var webseeds []string
	for _, ws := range t.Webseeds() {
		webseeds = append(webseeds, ws.URL)
	}
	data.WebSeeds = webseeds

	// BEP 38: the torrent's collection/similar hints and the local torrents
	// whose data could be reused. Shown only when there is something to show.
	cols := t.Collections()
	similar := t.SimilarTorrents()
	d.mu.Lock()
	local := d.collectionsLocked(t)
	d.mu.Unlock()
	var localNames []string
	for _, c := range local.Local {
		localNames = append(localNames, c.Name)
	}
	if len(cols) > 0 || len(similar) > 0 || len(localNames) > 0 {
		data.HasBEP38 = true
		data.CollectionsStr = strings.Join(cols, ", ")
		data.SimilarCount = len(similar)
		data.LocalDataStr = strings.Join(localNames, ", ")
	}
	switch tab {
	case "general":
		if states, ready := t.PieceStates(); ready {
			data.PieceBar = pieceBarCells(states)
			if holders, ok := t.PieceAvailability(); ok {
				data.AvailBar, data.AvailMissing, data.AvailMost = availBarCells(states, holders)
			}
		}
	case "log":
		for _, e := range d.tlog.entries(data.Hash) {
			when := e.At.Format("15:04:05")
			if time.Since(e.At) > 20*time.Hour {
				when = e.At.Format("2006-01-02 15:04")
			}
			data.Log = append(data.Log, uiLogRow{When: when, Msg: e.Msg, Detail: e.Detail, Count: e.Count, Bad: e.Bad})
		}
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
			priority := uiPriorityValue(filePriorityMeta(priorities, index))
			data.Files = append(data.Files, uiFileRow{
				Index: index, Path: file.Path(), Size: file.Length(), Done: done[index],
				Wanted: priority > 0, Priority: priority,
			})
		}
	case "peers":
		for _, peer := range t.Peers() {
			data.Peers = append(data.Peers, uiPeerRow{
				Address: peer.Addr.String(), Client: peer.Client,
				Down: int64(peer.DownloadSpeed), Up: int64(peer.UploadSpeed), Progress: peer.Progress * 100,
				Seed: peer.Seed, Incoming: peer.Source == torrent.SourceIncoming,
				Encrypted: peer.EncryptedStream, UTP: peer.UTP,
				Source: uiTr(lang, sourceName(peer.Source)), Downloading: peer.Downloading,
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
			status = uiTr(lang, status)
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
	return normalFilePriority
}

// uiPriorityValue maps any stored priority to one of the four levels the page
// offers (0 skip, 4 normal, 6 high, 7 maximum), so the select always shows the
// right option even for values written by another client.
func uiPriorityValue(p int) int {
	switch {
	case p <= 0:
		return 0
	case p >= 7:
		return 7
	case p >= 5:
		return 6
	default:
		return normalFilePriority
	}
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

// uiStateLabel is the human name of a torrent state, a catalog key the page
// translator localizes.
// uiStateIconData is the icon before a torrent's name and its label.
type uiStateIconData struct {
	Icon  string
	Label string
}

// uiStateIcon picks the state icon of a list row: the sidebar's icon for the
// state, a check mark for a paused torrent that is complete. The page's
// stIcon mirrors it for the rows drawn in the browser.
func uiStateIcon(state string, progress float64) uiStateIconData {
	switch state {
	case "downloading", "downloading_metadata":
		return uiStateIconData{"i-dl", uiStateLabel(state)}
	case "checking_files":
		return uiStateIconData{"i-check", uiStateLabel(state)}
	case "seeding":
		return uiStateIconData{"i-seed", uiStateLabel(state)}
	case "stalled":
		return uiStateIconData{"i-warn", uiStateLabel(state)}
	case "moving":
		return uiStateIconData{"i-move", uiStateLabel(state)}
	case "error":
		return uiStateIconData{"i-xc", uiStateLabel(state)}
	case "disk_wait":
		return uiStateIconData{"i-disk", uiStateLabel(state)}
	case "paused":
		if progress >= 100 {
			return uiStateIconData{"i-done", "Completed"}
		}
		return uiStateIconData{"i-pausec", uiStateLabel(state)}
	}
	return uiStateIconData{"i-sleep", uiStateLabel(state)}
}

func uiStateLabel(state string) string {
	switch state {
	case "downloading":
		return "Downloading"
	case "downloading_metadata":
		return "Metadata"
	case "checking_files":
		return "Checking"
	case "seeding":
		return "Seeding"
	case "paused":
		return "Paused"
	case "stalled":
		return "Stalled"
	case "moving":
		return "Moving"
	case "error":
		return "Error"
	case "disk_wait":
		return "Waiting for disk space"
	}
	return state
}

// handleUIMagnet returns a torrent's magnet link as text, for the page's
// "Copy magnet" menu entry.
func (d *Daemon) handleUIMagnet(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	d.mu.Lock()
	t, _ := d.findLocked(strings.TrimSpace(r.URL.Query().Get("hash")))
	d.mu.Unlock()
	if t == nil {
		http.Error(w, "torrent not found", http.StatusNotFound)
		return
	}
	magnet, err := t.Magnet()
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(magnet))
}
