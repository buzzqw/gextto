package gextto

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/constants"
	"github.com/buzzqw/gextto/internal/logging"
)

// healthStatusGrace is how long a non-ok health status must persist before the
// top bar shows it. A single failed probe (a momentarily missing mount, a
// transient write error) must not make the always-visible pill flash
// "degraded"; the previous status stays visible until the problem is confirmed.
const healthStatusGrace = 5 * time.Second

// healthStatusDebouncer hides short-lived health problems from the top bar. Its
// zero value is ready to use.
type healthStatusDebouncer struct {
	mu sync.Mutex
	// effective is the status actually shown to the user.
	effective string
	// pending is the last observed non-ok status and since is when it first
	// appeared; together they measure how long the problem has lasted.
	pending string
	since   time.Time
}

// update folds a raw health status into the debounced status to display at now.
func (d *healthStatusDebouncer) update(raw string, now time.Time) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if raw == "" || raw == "ok" {
		d.effective = "ok"
		d.pending = ""
		d.since = time.Time{}
		return d.effective
	}
	if d.pending != raw {
		// A new problem started: keep the previous status until it lasts.
		d.pending = raw
		d.since = now
	}
	if d.effective == "" {
		d.effective = "ok"
	}
	if !d.since.IsZero() && now.Sub(d.since) >= healthStatusGrace {
		d.effective = raw
	}
	return d.effective
}

// uiweb_shell.go builds the always-visible chrome of the new UI (sidebar footer,
// top bar metrics, language) so the server-rendered interface matches the
// classic one, which showed the same figures from /api/status, /api/health and
// /api/torrents.
type uiShellChrome struct {
	Version string
	// EngineLabel and EngineVersion name the torrent engine actually moving
	// the bytes, shown last in the header (e.g. "gx-core 1.1.193" for the
	// gx-torrent daemon, "qBittorrent-nox v5.1.1" for the external engine).
	// EngineLabel is a brand name (never translated); both are empty with an
	// engine that does not report a version.
	EngineLabel   string
	EngineVersion string
	DryRun        bool
	Status        string
	CurrentTime   string
	CPU           string
	CPUFrequency  string
	RAM           string
	// AmuleLoad and GxLoad are "CPU% · RAM" of the local amuled and gx-torrent
	// processes. Each is shown only when that process runs on this machine.
	AmuleLoad string
	GxLoad    string
	Download  string
	Upload    string
	NextCycle string
	Lang      string
	Torrents  int
	Peers     int
	Seeds     int
	// Transferring and Stuck split the unfinished torrents for the phone
	// status strip; Problems is logging.ProblemCount (the page compares it
	// with the value seen last time to show only new warnings).
	Transferring int
	Stuck        int
	Problems     uint64
	// ActiveDownloads is the badge on "Scarico": unfinished torrents plus the
	// direct HTTP downloads still running (seeding torrents are not counted).
	ActiveDownloads string
}

// dataDirWritableTTL is how long the data-dir writability result is reused. The
// chrome is rendered on every page, every partial and every SSE tick (5 s per
// open tab); the datadir does not change from one second to the next, and the
// probe does a create+fsync+delete that is expensive on a network datadir.
// It is a var so tests can disable the cache.
var dataDirWritableTTL = 30 * time.Second

type dataDirWritableEntry struct {
	ok bool
	at time.Time
}

var dataDirWritableCache sync.Map // string -> dataDirWritableEntry

// dataDirWritable reports whether a directory exists and is writable, reusing
// the result for dataDirWritableTTL. The chrome, the Salute tiles and the full
// health check all go through it, so a 5 s poll costs one probe per TTL.
func dataDirWritable(path string) bool {
	if path == "" {
		return false
	}
	if v, ok := dataDirWritableCache.Load(path); ok {
		if e, ok := v.(dataDirWritableEntry); ok && time.Since(e.at) < dataDirWritableTTL {
			return e.ok
		}
	}
	ok := false
	if info, err := os.Stat(path); err == nil && info.IsDir() && probeWritable(path) {
		ok = true
	}
	dataDirWritableCache.Store(path, dataDirWritableEntry{ok: ok, at: time.Now()})
	return ok
}

func uiShellChromeFrom(s *AppState) uiShellChrome {
	cfg := latestConfig(s)
	chrome := uiShellChrome{
		Version:      constants.AppVersion(),
		DryRun:       cfg.DryRun,
		Status:       "offline",
		CurrentTime:  uiNowClock(),
		CPU:          "—",
		CPUFrequency: currentCPUFrequency(),
		RAM:          "0 B",
		Download:     "0 B",
		Upload:       "0 B",
		NextCycle:    "—",
		Lang:         "it",
	}
	if identity, ok := s.activeEngine().(TorrentEngineIdentity); ok {
		chrome.EngineLabel, chrome.EngineVersion = identity.EngineVersion()
	}

	// The top bar only needs the status and the process CPU/RAM. The full
	// CheckWithPaths (mount enumeration, trash walk, and a whole-log scan)
	// belongs to the Salute page, not to every render: the live partial refreshes
	// the metrics every few seconds anyway.
	metrics := ProcessMetrics()
	rawStatus := "degraded"
	if dataDir := uiHealthPathsFrom(s).DataDir; dataDir != "" {
		if dataDirWritable(dataDir) {
			rawStatus = "ok"
		}
	}
	chrome.Status = s.health_status.update(rawStatus, time.Now())
	if metrics.ProcessCPUPercent != nil {
		chrome.CPU = strconv.FormatFloat(*metrics.ProcessCPUPercent, 'f', 1, 64) + "%"
	}
	chrome.RAM = logging.HumanBytesI64(saturatingInt64(metrics.ResidentBytes))
	if reporter, ok := s.activeEngine().(LocalProcessReporter); ok {
		if pid, ok := reporter.LocalProcessPID(); ok {
			if usage, ok := localProcessUsageForPIDs([]int{pid}); ok {
				chrome.GxLoad = localProcessLoadLabel(usage)
			}
		}
	}
	if ed2kEnabled(cfg) && hostIsLocal(uiSettingString(cfg, "amule_host")) {
		if usage, ok := localProcessUsageForName("amuled"); ok {
			chrome.AmuleLoad = localProcessLoadLabel(usage)
		}
	}

	var downloadRate, uploadRate uint64
	var parked map[string]StallWatch
	if s.db != nil {
		parked, _ = s.db.LoadStallWatches()
	}
	var activeDl, totalDl int
	for _, view := range s.activeEngine().List() {
		chrome.Torrents++
		totalDl++
		if view.Progress < 100 {
			activeDl++
		}
		chrome.Peers += view.NumPeers
		chrome.Seeds += view.NumSeeds
		downloadRate += view.DownloadRate
		uploadRate += view.UploadRate
		switch {
		case TorrentTransferring(view) && view.Progress < 100:
			chrome.Transferring++
		case TorrentIdle(view):
			chrome.Stuck++
		case view.Progress < 100 && parked[strings.ToLower(view.Hash)].stalledSince != nil:
			// Set aside by the stall monitor: paused, but still stuck.
			chrome.Stuck++
		}
	}
	chrome.Problems = logging.ProblemCount()
	for _, download := range HTTPDownloads() {
		totalDl++
		if download.Status == "downloading" {
			downloadRate += download.SpeedBytes
			activeDl++
		}
	}
	for _, download := range Ed2kDownloads(cfg) {
		if download.Complete() {
			continue
		}
		totalDl++
		activeDl++
		if download.Speed > 0 {
			chrome.Transferring++
		}
	}
	// The top bar shows the whole traffic: aMule's global rates (uploads
	// included) on top of the torrents'.
	ed2kDown, ed2kUp := v2Ed2kRates(s)
	downloadRate += ed2kDown
	uploadRate += ed2kUp
	chrome.ActiveDownloads = strconv.Itoa(activeDl) + "/" + strconv.Itoa(totalDl)
	chrome.Download = logging.HumanRate(saturatingInt64(downloadRate))
	chrome.Upload = logging.HumanRate(saturatingInt64(uploadRate))

	if cfg.RefreshSecs > 0 {
		start, ok := s.db.LastCycleAt()
		if snapshot := s.last_cycle.Snapshot(); snapshot.LastStartedAt != nil && (!ok || snapshot.LastStartedAt.After(start)) {
			start, ok = *snapshot.LastStartedAt, true
		}
		if ok {
			remaining := int64(start.Add(durationFromSeconds(cfg.RefreshSecs)).Sub(time.Now()).Seconds())
			if remaining < 0 {
				remaining = 0
			}
			chrome.NextCycle = logging.HumanDuration(remaining)
		}
	}
	if s.i18n != nil {
		if lang, err := s.i18n.Language(); err == nil && lang != "" {
			chrome.Lang = lang
		}
	}
	return chrome
}

// uiNavCounts computes the badges shown next to the sidebar entries, matching
// SidebarCount of the classic UI.
func uiNavCounts(s *AppState, cfg *Config) map[string]string {
	active := 0
	total := 0
	for _, download := range HTTPDownloads() {
		total++
		if download.Status == "downloading" {
			active++
		}
	}
	for _, view := range s.activeEngine().List() {
		total++
		if view.Progress < 100 {
			active++
		}
	}
	for _, download := range Ed2kDownloads(cfg) {
		total++
		if !download.Complete() {
			active++
		}
	}
	counts := map[string]string{
		// Downloads in progress, like the live badge (seeding is not counted).
		"downloads": strconv.Itoa(active) + "/" + strconv.Itoa(total),
		"series":    strconv.Itoa(len(cfg.Series)),
		"movies":    strconv.Itoa(len(cfg.Movies)),
	}
	if s.comics != nil {
		if items, err := s.comics.ListMonitored(false); err == nil {
			counts["comics"] = strconv.Itoa(len(items))
		}
	}
	return counts
}
