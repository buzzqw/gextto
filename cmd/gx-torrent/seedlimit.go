package main

// seedlimit.go enforces the global seeding limits (qBittorrent's max_ratio /
// max_seeding_time): a seeding torrent whose per-torrent limit is "global"
// stops (paused, or removed keeping the files) when it reaches the configured
// share ratio or seeding time. Per-torrent limits set through the API always
// win. In managed mode Gextto owns the seed policy, so this only runs
// standalone.

import (
	"strconv"
	"strings"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

const (
	maxRatioEnabledKey       = "max-ratio-enabled"
	maxRatioKey              = "max-ratio"
	maxRatioActKey           = "max-ratio-act"
	maxSeedingTimeEnabledKey = "max-seeding-time-enabled"
	maxSeedingTimeKey        = "max-seeding-time"
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

// seedLimitReached reports whether a seeding torrent passed its effective
// limit, and whether the action is removal (max-ratio-act=1) rather than pausing.
func (d *Daemon) seedLimitReached(meta *torrentMeta, stats torrent.Stats) (remove, reached bool) {
	remove = d.settingInt(maxRatioActKey, 0) == 1

	// Share ratio: the per-torrent value wins; -1 = global, 0 = never stop.
	ratio := meta.SeedRatio
	if ratio < 0 {
		if d.settingBool(maxRatioEnabledKey, false) {
			ratio = d.settingFloat(maxRatioKey, -1)
		} else {
			ratio = 0
		}
	}
	if ratio > 0 && stats.Bytes.Total > 0 {
		if float64(stats.Bytes.Uploaded) >= ratio*float64(stats.Bytes.Total) {
			return remove, true
		}
	}

	// Seeding time: per-torrent days; -1 = global minutes, 0 = never stop.
	var seconds int64
	switch {
	case meta.SeedDays > 0:
		seconds = meta.SeedDays * 86400
	case meta.SeedDays < 0:
		if d.settingBool(maxSeedingTimeEnabledKey, false) {
			seconds = int64(d.settingInt(maxSeedingTimeKey, -1)) * 60
		}
	}
	if seconds > 0 && int64(stats.SeededFor.Seconds()) >= seconds {
		return remove, true
	}
	return false, false
}
