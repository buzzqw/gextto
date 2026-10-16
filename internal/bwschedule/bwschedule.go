// Package bwschedule is the weekly bandwidth scheduler shared by Gextto and
// the gx-torrent daemon: every weekday has its own window (start/end) and its
// own download/upload limits, and can be switched on or off by itself.
//
// A window that crosses midnight belongs to the day it starts on: Friday
// 23:00–07:00 covers Friday night and Saturday until 07:00. When that tail
// overlaps the window of the following day, the following day wins.
//
// The stored form is one entry per weekday, separated by ";":
//
//	mon=23:00-07:00/0/500;-tue=23:00-07:00/0/500;…
//
// "<day>=<start>-<end>/<download KiB/s>/<upload KiB/s>", with a leading "-"
// for a day that is switched off (its values are kept for when it is turned
// back on). 0 means unlimited. Days missing from the text are off.
//
// A limit of AutoLimit (-1) marks the window as "let the caller decide": Gextto
// reads it as the guided tuning (it lifts the limit so the line can be measured),
// and the daemon, which has no such concept, treats it as an ordinary number.
package bwschedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// AutoLimit is the sentinel a limit can take instead of a sound number: it means
// "let the caller decide" (Gextto: the guided tuning). 0 means unlimited.
const AutoLimit int64 = -1

// Day is the schedule of one weekday.
type Day struct {
	On bool
	// Start and End are minutes since midnight; Start == End is no window.
	Start, End int
	// Download and Upload are the limits inside the window, in KiB/s (0 =
	// unlimited, AutoLimit = let the caller decide).
	Download, Upload int64
}

// Auto reports whether the window hands the limit to the caller (the guided
// tuning) instead of using a fixed number.
func (d Day) Auto() bool { return d.Download == AutoLimit || d.Upload == AutoLimit }

// Week is indexed by time.Weekday: 0 = Sunday … 6 = Saturday.
type Week [7]Day

// dayNames are the stored day names, indexed by time.Weekday.
var dayNames = [7]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// MondayFirst lists the weekdays in the order the pages show them.
var MondayFirst = [7]time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday}

// DayName is the stored (English, three letter) name of a weekday.
func DayName(day time.Weekday) string { return dayNames[int(day)%7] }

// Uniform builds the week of the older single-window settings: the same
// window and limits on every day in days (indexed by time.Weekday).
func Uniform(days [7]bool, start, end int, download, upload int64) Week {
	var week Week
	for n := range week {
		week[n] = Day{On: days[n], Start: start, End: end, Download: download, Upload: upload}
	}
	return week
}

// Parse reads the stored form. ok is false for an empty or unreadable value,
// so the caller can fall back to the older single-window settings.
func Parse(raw string) (Week, bool) {
	var week Week
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return week, false
	}
	seen := false
	for _, entry := range strings.Split(raw, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		on := true
		if strings.HasPrefix(entry, "-") {
			on, entry = false, entry[1:]
		}
		name, spec, found := strings.Cut(entry, "=")
		if !found {
			return Week{}, false
		}
		index := -1
		for n, dayName := range dayNames {
			if strings.EqualFold(strings.TrimSpace(name), dayName) {
				index = n
			}
		}
		if index < 0 {
			return Week{}, false
		}
		day, err := parseDay(spec)
		if err != nil {
			return Week{}, false
		}
		day.On = on
		week[index] = day
		seen = true
	}
	return week, seen
}

func parseDay(spec string) (Day, error) {
	parts := strings.Split(strings.TrimSpace(spec), "/")
	if len(parts) != 3 {
		return Day{}, fmt.Errorf("want start-end/download/upload")
	}
	startText, endText, found := strings.Cut(parts[0], "-")
	if !found {
		return Day{}, fmt.Errorf("want start-end")
	}
	start, okStart := ParseClock(startText)
	end, okEnd := ParseClock(endText)
	if !okStart || !okEnd {
		return Day{}, fmt.Errorf("bad time")
	}
	download, errDL := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	upload, errUL := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
	if errDL != nil || errUL != nil || download < AutoLimit || upload < AutoLimit {
		return Day{}, fmt.Errorf("bad limit")
	}
	return Day{Start: start, End: end, Download: download, Upload: upload}, nil
}

// String is the stored form, Monday first, with every day present.
func (w Week) String() string {
	parts := make([]string, 0, 7)
	for _, weekday := range MondayFirst {
		day := w[weekday]
		prefix := ""
		if !day.On {
			prefix = "-"
		}
		parts = append(parts, fmt.Sprintf("%s%s=%s-%s/%d/%d", prefix, dayNames[weekday],
			FormatClock(day.Start), FormatClock(day.End), day.Download, day.Upload))
	}
	return strings.Join(parts, ";")
}

// AnyOn reports whether at least one day has a usable window (an Auto day is
// usable even without "on").
func (w Week) AnyOn() bool {
	for _, day := range w {
		if (day.On || day.Auto()) && day.Start != day.End {
			return true
		}
	}
	return false
}

// At returns the scheduled limits in force at now, and whether a window is
// active. Today's window wins over the after-midnight tail of yesterday's.
func (w Week) At(now time.Time) (download, upload int64, active bool) {
	day, active := w.AtDay(now)
	return day.Download, day.Upload, active
}

// AtDay returns the window in force at now, so a caller can tell a fixed window
// from an Auto one (Day.Auto).
func (w Week) AtDay(now time.Time) (Day, bool) {
	// An Auto window is active by itself: ticking Auto is enough, the user does
	// not also have to tick "on".
	usable := func(day Day) bool { return (day.On || day.Auto()) && day.Start != day.End }
	minute := now.Hour()*60 + now.Minute()
	today := w[int(now.Weekday())]
	if usable(today) {
		if today.Start < today.End {
			if minute >= today.Start && minute < today.End {
				return today, true
			}
		} else if minute >= today.Start {
			return today, true
		}
	}
	yesterday := w[(int(now.Weekday())+6)%7]
	if usable(yesterday) && yesterday.Start > yesterday.End && minute < yesterday.End {
		return yesterday, true
	}
	return Day{}, false
}

// ParseClock reads "HH:MM" into minutes since midnight.
func ParseClock(value string) (int, bool) {
	hourText, minuteText, found := strings.Cut(strings.TrimSpace(value), ":")
	if !found {
		return 0, false
	}
	hour, errHour := strconv.Atoi(strings.TrimSpace(hourText))
	minute, errMinute := strconv.Atoi(strings.TrimSpace(minuteText))
	if errHour != nil || errMinute != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, false
	}
	return hour*60 + minute, true
}

// FormatClock writes minutes since midnight as "HH:MM".
func FormatClock(minutes int) string {
	minutes = ((minutes % 1440) + 1440) % 1440
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}
