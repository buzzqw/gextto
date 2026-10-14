package gextto

import (
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/bwschedule"
)

// speedScheduleWeek is the configured bandwidth schedule: the per-day table
// (libtorrent_sched_week) when saved, otherwise the single window
// (libtorrent_sched_start/end and the two limits) on the selected days of
// libtorrent_sched_days (0 = Monday … 6 = Sunday). The week format and the
// evaluation are shared with gx-torrent (internal/bwschedule).
func speedScheduleWeek(cfg *Config) bwschedule.Week {
	if week, ok := bwschedule.Parse(cfg.Settings["libtorrent_sched_week"]); ok {
		return week
	}
	clock := func(key string, fallback int) int {
		if minutes, ok := bwschedule.ParseClock(cfg.Settings[key]); ok {
			return minutes
		}
		return fallback
	}
	var days [7]bool
	for _, day := range strings.Split(cfg.Settings["libtorrent_sched_days"], ",") {
		if parsed, err := strconv.Atoi(strings.TrimSpace(day)); err == nil && parsed >= 0 && parsed <= 6 {
			// Monday=0 here, time.Weekday has Sunday=0.
			days[(parsed+1)%7] = true
		}
	}
	return bwschedule.Uniform(days, clock("libtorrent_sched_start", 23*60), clock("libtorrent_sched_end", 8*60),
		gh6_parseSettingInt(cfg, "libtorrent_sched_dl_limit"), gh6_parseSettingInt(cfg, "libtorrent_sched_ul_limit"))
}

// scheduledSpeedLimitsAt returns the scheduled limits in force at now and
// whether the schedule is active.
func scheduledSpeedLimitsAt(cfg *Config, now time.Time) (int64, int64, bool) {
	if cfg == nil || !settingTruthy(cfg.Settings["libtorrent_sched_enabled"]) {
		return 0, 0, false
	}
	return speedScheduleWeek(cfg).At(now)
}
