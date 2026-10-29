package main

// schedule.go is the standalone bandwidth scheduler: an optional window per
// weekday with alternate global speed limits, like qBittorrent's scheduler but
// with every day on its own times and limits (internal/bwschedule, shared with
// Gextto). Outside the window the configured limits apply. The engine changes the global limits in place,
// so switching never reopens the session or drops peers.

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/bwschedule"
)

const (
	scheduleEnabledKey = "schedule-enabled"
	scheduleStartKey   = "schedule-start"
	scheduleEndKey     = "schedule-end"
	scheduleDLKey      = "schedule-download"
	scheduleULKey      = "schedule-upload"
	// scheduleDaysKey holds the weekdays the window applies to, as a
	// comma-separated list of time.Weekday numbers (0=Sunday..6=Saturday). An
	// empty value means every day, so settings written before this option keep
	// working unchanged.
	scheduleDaysKey = "schedule-days"
	// scheduleWeekKey holds the per-day schedule (bwschedule's stored form):
	// each weekday with its own window and limits. When set it supersedes the
	// single window above, which stays as the fallback of older settings.
	scheduleWeekKey = "schedule-week"
	// altSpeedEnabledKey is the manual "alternative limits" toggle (qBittorrent's
	// speed-limit button): when on, altSpeedDLKey/altSpeedULKey apply outside the
	// scheduled window too.
	altSpeedEnabledKey = "alt-speed-limits-on"
	altSpeedDLKey      = "alt-speed-download"
	altSpeedULKey      = "alt-speed-upload"
)

// scheduleDaysSet parses the stored weekday list into a per-weekday flag. An
// empty (or missing) value means every day.
func scheduleDaysSet(raw string) [7]bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return [7]bool{true, true, true, true, true, true, true}
	}
	var days [7]bool
	for _, part := range strings.Split(raw, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && n >= 0 && n <= 6 {
			days[n] = true
		}
	}
	return days
}

// scheduleDaysSelected counts the days the schedule applies to.
func scheduleDaysSelected(days [7]bool) int {
	count := 0
	for _, on := range days {
		if on {
			count++
		}
	}
	return count
}

// formatScheduleDays canonicalizes the flags for storage: an all-days (or
// all-off) selection collapses to "", the compact "every day" value.
func formatScheduleDays(days [7]bool) string {
	parts := make([]string, 0, 7)
	for n := 0; n < 7; n++ {
		if days[n] {
			parts = append(parts, strconv.Itoa(n))
		}
	}
	if len(parts) == 7 {
		return ""
	}
	return strings.Join(parts, ",")
}

// scheduleWeek is the configured week: the per-day schedule when saved,
// otherwise the single window on the selected days (older settings). ok is
// false when neither is usable.
func (d *Daemon) scheduleWeek() (week bwschedule.Week, ok bool) {
	settings := d.opts.Settings
	if settings == nil {
		return week, false
	}
	if week, ok := bwschedule.Parse(settings.Get(scheduleWeekKey, "")); ok {
		return week, true
	}
	start, okStart := parseClock(settings.Get(scheduleStartKey, ""))
	end, okEnd := parseClock(settings.Get(scheduleEndKey, ""))
	if !okStart || !okEnd || start == end {
		return week, false
	}
	return bwschedule.Uniform(scheduleDaysSet(settings.Get(scheduleDaysKey, "")), start, end,
		int64FromSetting(settings.Get(scheduleDLKey, "0")), int64FromSetting(settings.Get(scheduleULKey, "0"))), true
}

// scheduledSpeedLimits returns the alternate limits and whether the window is
// active at now. A disabled or invalid schedule is never active; each weekday
// has its own window and limits (see bwschedule).
func (d *Daemon) scheduledSpeedLimits(now time.Time) (download, upload int64, active bool) {
	settings := d.opts.Settings
	if settings == nil {
		return 0, 0, false
	}
	if strings.ToLower(strings.TrimSpace(settings.Get(scheduleEnabledKey, "false"))) != "true" {
		return 0, 0, false
	}
	week, ok := d.scheduleWeek()
	if !ok {
		return 0, 0, false
	}
	download, upload, active = week.At(now)
	// An "Auto" window (the AutoLimit sentinel) has no cap: the line is left to
	// the auto-speed so it can measure it, exactly like Gextto's Auto windows.
	if active {
		if download == bwschedule.AutoLimit {
			download = 0
		}
		if upload == bwschedule.AutoLimit {
			upload = 0
		}
	}
	return download, upload, active
}

// parseClock reads "HH:MM" into minutes since midnight.
func parseClock(value string) (int, bool) { return bwschedule.ParseClock(value) }

// altSpeedSettings returns the manual alternative limits (KiB/s, 0 = unlimited)
// and whether the manual mode is on (qBittorrent's speed-limit button).
func (d *Daemon) altSpeedSettings() (download, upload int64, manual bool) {
	settings := d.opts.Settings
	if settings == nil {
		return 0, 0, false
	}
	manual = strings.EqualFold(strings.TrimSpace(settings.Get(altSpeedEnabledKey, "false")), "true")
	download = int64FromSetting(settings.Get(altSpeedDLKey, "0"))
	upload = int64FromSetting(settings.Get(altSpeedULKey, "0"))
	return download, upload, manual
}

// altSpeedActive reports whether the alternative limits are in force now: the
// scheduled window, or the manual toggle outside it.
func (d *Daemon) altSpeedActive(now time.Time) bool {
	if _, _, active := d.scheduledSpeedLimits(now); active {
		return true
	}
	_, _, manual := d.altSpeedSettings()
	return manual
}

func int64FromSetting(value string) int64 {
	var n int64
	if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &n); err != nil || n < 0 {
		return 0
	}
	return n
}

// applyEffectiveSpeedLimitsLocked pushes the effective global limits onto the
// running session: the scheduled ones inside the window, the configured ones
// otherwise. It applies only when they changed.
func (d *Daemon) applyEffectiveSpeedLimitsLocked(now time.Time) {
	download, upload := d.state.Config.SpeedLimitDownload, d.state.Config.SpeedLimitUpload
	reason := "limits"
	if sd, su, active := d.scheduledSpeedLimits(now); active {
		download, upload = sd, su
		reason = "schedule"
	} else if ad, au, manual := d.altSpeedSettings(); manual {
		download, upload = ad, au
		reason = "alt-speed"
	}
	// Latency auto-speed clamps the upload below the configured (or scheduled)
	// ceiling; it never raises it above. See autospeed.go.
	if enabled, _, floor := d.autoSpeedSettings(); enabled {
		if clamped, acting := d.autoSpeed.clampUpload(now, upload, floor); acting {
			if clamped != upload {
				reason = "auto-speed"
			}
			upload = clamped
		}
	}
	if d.session != nil {
		if lan := d.limitLANPeers(); d.session.LimitLocalPeers() != lan {
			d.session.SetLimitLocalPeers(lan)
		}
	}
	if download == d.appliedDL && upload == d.appliedUL {
		d.logSpeedLimitsLocked(now, reason)
		return
	}
	if d.session != nil {
		d.session.SetSpeedLimits(download, upload)
	}
	d.appliedDL, d.appliedUL = download, upload
	d.limitsChangedAt = now
	d.logSpeedLimitsLocked(now, reason)
}

// speedLimitLogSettle is how long a limit must stay put before it is logged.
// The autotune controller moves the cap every few seconds while it ramps: one
// line per step would be hundreds of lines a day. Gextto logs the autotune
// decisions on its own; here the log keeps the value the session really runs
// with.
const speedLimitLogSettle = 10 * time.Second

// logSpeedLimitsLocked writes the applied limits once they have settled, and
// whenever the reason changes (schedule, alternative limits, auto-speed).
func (d *Daemon) logSpeedLimitsLocked(now time.Time, reason string) {
	if !d.loggedAt.IsZero() {
		if d.appliedDL == d.loggedDL && d.appliedUL == d.loggedUL && reason == d.loggedReason {
			return
		}
		if now.Sub(d.limitsChangedAt) < speedLimitLogSettle {
			return
		}
	}
	d.loggedDL, d.loggedUL = d.appliedDL, d.appliedUL
	d.loggedReason, d.loggedAt = reason, now
	logfDebug("speed limits applied: %d KiB/s down, %d KiB/s up (%s)", d.appliedDL, d.appliedUL, reason)
}
