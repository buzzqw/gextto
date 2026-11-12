package gextto

import (
	"time"
)

// linesplit.go makes gx-torrent and aMule share one line. Without it aMule got
// the same limits as the torrents, so the two together could use twice the
// line, and Auto saw aMule only as "someone else on the line". When aMule is
// active and the line has a limit (fixed, scheduled or Auto's), the limit is
// split: each engine has a floor (the torrents ed2kLineShare percent, eD2k the
// rest) and lends to the other the part of its floor it is not using, read
// from the live rates. Each engine can grow to the whole line while the other
// is idle; when both want more, they meet at the floors.

// ed2kLineShareSetting is the torrents' floor, in percent of a shared line.
const ed2kLineShareSetting = settingTorrentEd2kLineShare

const (
	defaultEd2kLineShare = 60
	// lineSplitMinKib keeps a floor for an engine that is lent everything.
	lineSplitMinKib = 16
)

// ed2kLineShare returns the torrents' floor in percent (10..90).
func ed2kLineShare(cfg *Config) int {
	share := int(numberSettingInt(cfg.Settings, ed2kLineShareSetting, defaultEd2kLineShare))
	if share < 10 {
		return 10
	}
	if share > 90 {
		return 90
	}
	return share
}

// lineSplitActive reports whether aMule shares the line with the torrents:
// eD2k is enabled and the core answers.
func lineSplitActive(cfg *Config) bool {
	if cfg == nil || !ed2kEnabled(cfg) {
		return false
	}
	_, err := ed2kStatus(cfg)
	return err == nil
}

// ed2kLiveRates are aMule's current download and upload rates in bytes/s.
func ed2kLiveRates(cfg *Config) (down, up uint64) {
	status, err := ed2kStatus(cfg)
	if err != nil {
		return 0, 0
	}
	return uint64(max(status.DownloadRate, 0)), uint64(max(status.UploadRate, 0))
}

// splitLine divides a limit of totalKib KiB/s (0 = unlimited: no split)
// between the torrents and eD2k, given what each moves now (bytes/s) and the
// torrents' floor in percent.
func splitLine(totalKib int64, torrentRate, ed2kRate uint64, sharePct int) (torrentKib, ed2kKib int64) {
	if totalKib <= 0 {
		return 0, 0
	}
	floorT := totalKib * int64(sharePct) / 100
	floorE := totalKib - floorT
	// A little headroom over the measured rate, so an engine that is using its
	// floor is not taken as idle because of a slow second.
	useT := int64(torrentRate/1024) * 5 / 4
	useE := int64(ed2kRate/1024) * 5 / 4
	lendT := maxInt64(floorT-useT, 0)
	lendE := maxInt64(floorE-useE, 0)
	torrentKib = maxInt64(clampKib(floorT+lendE, totalKib), lineSplitMinKib)
	ed2kKib = maxInt64(clampKib(floorE+lendT, totalKib), lineSplitMinKib)
	return torrentKib, ed2kKib
}

// clampKib keeps value at most limit.
func clampKib(value, limit int64) int64 {
	if value > limit {
		return limit
	}
	return value
}

// applyLineLimits gives aMule its part of the line limit and returns the
// torrents' part, to push to the torrent engine. Without an active aMule it
// keeps the old rule (aMule follows the torrents' limits) and returns the
// limit unchanged.
func applyLineLimits(cfg *Config, engine TorrentEngine, downloadKib, uploadKib int64, now time.Time) (int64, int64) {
	if !lineSplitActive(cfg) {
		applyEd2kSpeedLimits(cfg, downloadKib, uploadKib, now)
		return downloadKib, uploadKib
	}
	var torrentDown, torrentUp uint64
	if engine != nil {
		for _, view := range SessionSnapshot(engine) {
			torrentDown += view.DownloadRate
			torrentUp += view.UploadRate
		}
	}
	ed2kDown, ed2kUp := ed2kLiveRates(cfg)
	share := ed2kLineShare(cfg)
	gxDown, edDown := splitLine(downloadKib, torrentDown, ed2kDown, share)
	gxUp, edUp := splitLine(uploadKib, torrentUp, ed2kUp, share)
	applyEd2kSpeedLimits(cfg, edDown, edUp, now)
	return gxDown, gxUp
}
