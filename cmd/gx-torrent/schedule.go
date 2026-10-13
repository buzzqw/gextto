package main

// schedule.go is the standalone bandwidth scheduler: an optional daily window
// with alternate global speed limits, like qBittorrent's scheduler. Outside the
// window the configured limits apply. The engine changes the global limits in place,
// so switching never reopens the session or drops peers.

import (
	"fmt"
	"strconv"
	"strings"
	"time"
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

// scheduledSpeedLimits returns the alternate limits and whether the window is
// active at now. A disabled or invalid schedule is never active; the window
// applies only on the selected weekdays.
func (d *Daemon) scheduledSpeedLimits(now time.Time) (download, upload int64, active bool) {
	settings := d.opts.Settings
	if settings == nil {
		return 0, 0, false
	}
	if strings.ToLower(strings.TrimSpace(settings.Get(scheduleEnabledKey, "false"))) != "true" {
		return 0, 0, false
	}
	if !scheduleDaysSet(settings.Get(scheduleDaysKey, ""))[int(now.Weekday())] {
		return 0, 0, false
	}
	start, okStart := parseClock(settings.Get(scheduleStartKey, ""))
	end, okEnd := parseClock(settings.Get(scheduleEndKey, ""))
	if !okStart || !okEnd || start == end {
		return 0, 0, false
	}
	minute := now.Hour()*60 + now.Minute()
	if start < end {
		active = minute >= start && minute < end
	} else { // wraps midnight
		active = minute >= start || minute < end
	}
	if !active {
		return 0, 0, false
	}
	return int64FromSetting(settings.Get(scheduleDLKey, "0")), int64FromSetting(settings.Get(scheduleULKey, "0")), true
}

// parseClock reads "HH:MM" into minutes since midnight.
func parseClock(value string) (int, bool) {
	parts := strings.SplitN(strings.TrimSpace(value), ":", 2)
	if len(parts) != 2 {
		return 0, false
	}
	var hour, minute int
	if _, err := fmt.Sscanf(strings.TrimSpace(parts[0]), "%d", &hour); err != nil {
		return 0, false
	}
	if _, err := fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &minute); err != nil {
		return 0, false
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, false
	}
	return hour*60 + minute, true
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
	if download == d.appliedDL && upload == d.appliedUL {
		return
	}
	if d.session != nil {
		d.session.SetSpeedLimits(download, upload)
	}
	d.appliedDL, d.appliedUL = download, upload
	logf("effective speed limits %d/%d KiB/s (%s)", download, upload, reason)
}
