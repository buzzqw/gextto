package gextto

import (
	"strconv"
	"time"

	"github.com/buzzqw/gextto/internal/constants"
	"github.com/buzzqw/gextto/internal/logging"
)

// uiweb_shell.go builds the always-visible chrome of the new UI (sidebar footer,
// top bar metrics, language) so the server-rendered interface matches the
// classic one, which showed the same figures from /api/status, /api/health and
// /api/torrents.
type uiShellChrome struct {
	Version   string
	DryRun    bool
	Status    string
	CPU       string
	RAM       string
	Download  string
	Upload    string
	NextCycle string
	Lang      string
	Torrents  int
	Peers     int
	Seeds     int
}

func uiShellChromeFrom(s *AppState) uiShellChrome {
	cfg := latestConfig(s)
	chrome := uiShellChrome{
		Version:   "1.0." + constants.Build,
		DryRun:    cfg.DryRun,
		Status:    "offline",
		CPU:       "—",
		RAM:       "0 B",
		Download:  "0 B",
		Upload:    "0 B",
		NextCycle: "—",
		Lang:      "it",
	}

	trash := ""
	if s.cfg.TrashPath != nil {
		trash = *s.cfg.TrashPath
	}
	ramdisk := ""
	if value, ok := s.cfg.Settings["libtorrent_ramdisk_dir"]; ok {
		ramdisk = value
	}
	health := CheckWithPaths(&HealthPaths{
		DataDir:      s.cfg.DataDir,
		TrashPath:    trash,
		DownloadPath: s.cfg.LibtorrentDir,
		ArchiveRoot:  gh3DerefString(s.cfg.ArchiveRoot),
		RamdiskPath:  ramdisk,
	})
	if health.Status != "" {
		chrome.Status = health.Status
	}
	if health.ProcessCPUPercent != nil {
		chrome.CPU = strconv.FormatFloat(*health.ProcessCPUPercent, 'f', 1, 64) + "%"
	}
	chrome.RAM = logging.HumanBytesI64(saturatingInt64(health.ResidentBytes))

	var downloadRate, uploadRate uint64
	for _, view := range s.activeEngine().List() {
		chrome.Torrents++
		chrome.Peers += view.NumPeers
		chrome.Seeds += view.NumSeeds
		downloadRate += view.DownloadRate
		uploadRate += view.UploadRate
	}
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
