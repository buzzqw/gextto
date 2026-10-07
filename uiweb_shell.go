package gextto

import (
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
	Version      string
	DryRun       bool
	Status       string
	CurrentTime  string
	CPU          string
	CPUFrequency string
	RAM          string
	Download     string
	Upload       string
	NextCycle    string
	Lang         string
	Torrents     int
	Peers        int
	Seeds        int
	// Transferring and Stuck split the unfinished torrents for the phone
	// status strip; Problems is logging.ProblemCount (the page compares it
	// with the value seen last time to show only new warnings).
	Transferring int
	Stuck        int
	Problems     uint64
}

func uiShellChromeFrom(s *AppState) uiShellChrome {
	cfg := latestConfig(s)
	chrome := uiShellChrome{
		Version:      "1.0." + constants.Build,
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

	health := CheckWithPaths(uiHealthPathsFrom(s))
	if health.Status != "" {
		chrome.Status = s.health_status.update(health.Status, time.Now())
	}
	if health.ProcessCPUPercent != nil {
		chrome.CPU = strconv.FormatFloat(*health.ProcessCPUPercent, 'f', 1, 64) + "%"
	}
	chrome.RAM = logging.HumanBytesI64(saturatingInt64(health.ResidentBytes))

	var downloadRate, uploadRate uint64
	var parked map[string]StallWatch
	if s.db != nil {
		parked, _ = s.db.LoadStallWatches()
	}
	for _, view := range s.activeEngine().List() {
		chrome.Torrents++
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
		if download.Status == "downloading" {
			downloadRate += download.SpeedBytes
		}
	}
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
func uiNavCounts(s *AppState, cfg *Config) map[string]int {
	http := 0
	for _, download := range HTTPDownloads() {
		if download.Status == "downloading" {
			http++
		}
	}
	counts := map[string]int{
		"downloads": len(s.activeEngine().List()) + http,
		"series":    len(cfg.Series),
		"movies":    len(cfg.Movies),
	}
	if s.comics != nil {
		if items, err := s.comics.ListMonitored(false); err == nil {
			counts["comics"] = len(items)
		}
	}
	return counts
}
