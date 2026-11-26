package main

// seedlimit.go enforces the global seeding limits (qBittorrent's max_ratio /
// max_seeding_time): a seeding torrent whose per-torrent limit is "global"
// stops (paused, or removed keeping the files) when it reaches the configured
// share ratio or seeding time. Per-torrent limits set through the API always
// win. In managed mode Gextto owns the seed policy, so this only runs
// standalone.

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

const (
	maxRatioEnabledKey       = "max-ratio-enabled"
	maxRatioKey              = "max-ratio"
	maxRatioActKey           = "max-ratio-act"
	maxSeedingTimeEnabledKey = "max-seeding-time-enabled"
	maxSeedingTimeKey        = "max-seeding-time"
	// The inactivity limit (qBittorrent max_inactive_seeding_time): a seed
	// that uploaded nothing for that many minutes while seeding stops.
	maxInactiveSeedingTimeEnabledKey = "max-inactive-seeding-time-enabled"
	maxInactiveSeedingTimeKey        = "max-inactive-seeding-time"
	// recheckOnCompleteKey verifies a finished download once before it seeds
	// (qBittorrent's "Recheck torrents on completion").
	recheckOnCompleteKey = "recheck-on-complete"
	// contentLayoutKey is the default content layout of the torrents added
	// from the page and the compatible API (qBittorrent torrent_content_layout).
	contentLayoutKey = "torrent-content-layout"
	// autoTMMEnabledKey is the global default for qBittorrent automatic torrent
	// management (the per-torrent flag is what actually drives a relocation).
	autoTMMEnabledKey = "auto-tmm-enabled"
)

// settingInt reads an integer setting, with a fallback when unset or invalid.
func (d *Daemon) settingInt(key string, fallback int) int {
	if d.opts.Settings == nil {
		return fallback
	}
	if v, err := strconv.Atoi(strings.TrimSpace(d.opts.Settings.Get(key, ""))); err == nil {
		return v
	}
	return fallback
}

// settingFloat reads a float setting, with a fallback when unset or invalid.
func (d *Daemon) settingFloat(key string, fallback float64) float64 {
	if d.opts.Settings == nil {
		return fallback
	}
	if v, err := strconv.ParseFloat(strings.TrimSpace(d.opts.Settings.Get(key, "")), 64); err == nil {
		return v
	}
	return fallback
}

// shareLimitsAllKey turns the three limits into one condition: seed until the
// ratio, the time and the inactivity are all satisfied instead of stopping at
// the first one that fires (qBittorrent's share_limits_mode "All").
const shareLimitsAllKey = "share-limits-all"

// seedRatioState reports the effective share ratio of a torrent and whether it
// was reached. The per-torrent value wins; -1 = global, 0 = never stop.
func (d *Daemon) seedRatioState(meta *torrentMeta, stats torrent.Stats) (set, hit bool) {
	ratio := meta.SeedRatio
	if ratio < 0 {
		if d.settingBool(maxRatioEnabledKey, false) {
			ratio = d.settingFloat(maxRatioKey, -1)
		} else {
			ratio = 0
		}
	}
	if ratio <= 0 {
		return false, false
	}
	return true, stats.Bytes.Total > 0 && float64(stats.Bytes.Uploaded) >= ratio*float64(stats.Bytes.Total)
}

// seedTimeState reports the effective seeding time of a torrent and whether it
// elapsed. The qBittorrent-compatible minutes win; older state still uses the
// frozen days field (-1 = global minutes, 0 = never stop).
func (d *Daemon) seedTimeState(meta *torrentMeta, stats torrent.Stats) (set, hit bool) {
	var seconds int64
	switch {
	case meta.SeedMinutes != nil:
		if *meta.SeedMinutes > 0 {
			seconds = *meta.SeedMinutes * 60
		}
	case meta.SeedDays > 0:
		seconds = meta.SeedDays * 86400
	case meta.SeedDays < 0:
		if d.settingBool(maxSeedingTimeEnabledKey, false) {
			seconds = int64(d.settingInt(maxSeedingTimeKey, -1)) * 60
		}
	}
	if seconds <= 0 {
		return false, false
	}
	return true, int64(stats.SeededFor.Seconds()) >= seconds
}

// seedTimeForever reports whether a torrent has no seeding-time limit. An
// infinite qBittorrent limit (-1 minutes) is different from a global one (-2,
// stored as a nil override) because the latter still reads the setting.
func seedTimeForever(meta *torrentMeta) bool {
	if meta.SeedMinutes != nil {
		return *meta.SeedMinutes <= 0
	}
	return meta.SeedDays == 0
}

// effectiveInactiveMinutes resolves the per-torrent inactive limit: a nil
// override follows the global setting, -1 disables it, a positive value is
// an exact number of minutes.
func (d *Daemon) effectiveInactiveMinutes(meta *torrentMeta) int64 {
	if meta.InactiveMinutes != nil {
		if *meta.InactiveMinutes < 0 {
			return -1
		}
		return *meta.InactiveMinutes
	}
	if !d.settingBool(maxInactiveSeedingTimeEnabledKey, false) {
		return -1
	}
	return int64(d.settingInt(maxInactiveSeedingTimeKey, -1))
}

// seedIdleConfigured reports whether the inactivity limit applies to a torrent:
// it needs the setting on a positive value and a torrent that does not seed
// forever, like seedIdleReached.
func (d *Daemon) seedIdleConfigured(meta *torrentMeta) bool {
	if meta.SeedRatio == 0 || seedTimeForever(meta) {
		return false
	}
	return d.effectiveInactiveMinutes(meta) > 0
}

// seedLimitStop decides whether the global seeding limits stop a torrent. With
// the default rule the first limit that fires is enough; with share-limits-all
// every configured limit has to be reached, so the seed lasts as long as the
// three conditions together require. idle is true only when the inactivity
// limit alone ended the seed, which is the sentence the log writes.
func (d *Daemon) seedLimitStop(rt *runtimeInfo, meta *torrentMeta, stats torrent.Stats, now time.Time) (remove, idle, reached bool) {
	remove = d.settingInt(maxRatioActKey, 0) == 1
	ratioSet, ratioHit := d.seedRatioState(meta, stats)
	timeSet, timeHit := d.seedTimeState(meta, stats)
	idleSet := d.seedIdleConfigured(meta)
	idleHit := d.seedIdleReached(rt, meta, stats, now)

	if d.settingBool(shareLimitsAllKey, true) {
		if !ratioSet && !timeSet && !idleSet {
			return remove, false, false
		}
		if (ratioSet && !ratioHit) || (timeSet && !timeHit) || (idleSet && !idleHit) {
			return remove, false, false
		}
		return remove, false, true
	}
	if ratioHit || timeHit {
		return remove, false, true
	}
	return remove, idleHit, idleHit
}

// seedIdleReached reports a seed that uploaded nothing for the global inactive
// seeding time, and records the observation in rt. The clock runs only while
// the torrent is seeding, so a queued or paused seed is never stopped for being
// idle; a torrent set to seed forever (ratio or days 0) is never stopped either.
// The stop action is the one of the other limits (pause, or remove with
// max-ratio-act=1), like qBittorrent's single share-limit action.
func (d *Daemon) seedIdleReached(rt *runtimeInfo, meta *torrentMeta, stats torrent.Stats, now time.Time) bool {
	if stats.Status != torrent.Seeding {
		rt.seedIdleSince = time.Time{}
		return false
	}
	if rt.seedIdleSince.IsZero() || stats.Bytes.Uploaded != rt.seedUpload {
		rt.seedUpload = stats.Bytes.Uploaded
		rt.seedIdleSince = now
		return false
	}
	if meta.SeedRatio == 0 || seedTimeForever(meta) {
		return false
	}
	minutes := d.effectiveInactiveMinutes(meta)
	return minutes > 0 && now.Sub(rt.seedIdleSince) >= time.Duration(minutes)*time.Minute
}

// defaultContentLayout is the content layout of a torrent added without one
// from the page or the compatible API (Original unless set in the settings).
func (d *Daemon) defaultContentLayout() string {
	if d.opts.Settings == nil {
		return torrent.ContentLayoutOriginal
	}
	if layout, ok := torrent.ParseContentLayout(d.opts.Settings.Get(contentLayoutKey, "")); ok {
		return layout
	}
	return torrent.ContentLayoutOriginal
}

// qbitContentLayout reads the compatible API's add parameters: contentLayout,
// or the older root_folder (true = Subfolder, false = NoSubfolder); neither
// means the settings default.
func (d *Daemon) qbitContentLayout(r *http.Request) string {
	if layout := strings.TrimSpace(r.FormValue("contentLayout")); layout != "" {
		return layout
	}
	switch strings.ToLower(strings.TrimSpace(r.FormValue("root_folder"))) {
	case "true":
		return torrent.ContentLayoutSubfolder
	case "false":
		return torrent.ContentLayoutNoSubfolder
	}
	return d.defaultContentLayout()
}
