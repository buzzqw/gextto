package gextto

import (
	"net/url"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/bwschedule"
)

// The per-day editor's "Auto" mode stores the AutoLimit sentinel instead of a
// number; a "Limite" day keeps its two values, and the older two-checkbox form
// ("on"/"auto") is still read.
func TestWeekValueAutoHandsTheWindowToTheTuning(t *testing.T) {
	form := url.Values{
		"day-mon-mode":  {"auto"},
		"day-mon-start": {"00:00"},
		"day-mon-end":   {"23:59"},
		"day-tue-mode":  {"limit"},
		"day-tue-start": {"23:00"},
		"day-tue-end":   {"08:00"},
		"day-tue-dl":    {"3000"},
		"day-tue-ul":    {"200"},
	}
	raw, problem := uiWeekValue(form)
	if problem != "" {
		t.Fatalf("uiWeekValue: %s", problem)
	}
	week, ok := bwschedule.Parse(raw)
	if !ok {
		t.Fatalf("stored week unreadable: %q", raw)
	}
	if !week[time.Monday].Auto() {
		t.Fatalf("monday must be auto: %+v", week[time.Monday])
	}
	if week[time.Tuesday].Auto() || week[time.Tuesday].Download != 3000 {
		t.Fatalf("tuesday must stay fixed: %+v", week[time.Tuesday])
	}
}

// The older two-checkbox form ("on"/"auto") is still read while a page loaded
// before the change saves.
func TestWeekValueReadsTheOlderTwoCheckboxForm(t *testing.T) {
	form := url.Values{
		"day-wed-on":    {"1"},
		"day-wed-start": {"22:00"},
		"day-wed-end":   {"06:00"},
		"day-wed-auto":  {"1"},
	}
	raw, problem := uiWeekValue(form)
	if problem != "" {
		t.Fatalf("uiWeekValue: %s", problem)
	}
	week, ok := bwschedule.Parse(raw)
	if !ok || !week[time.Wednesday].Auto() {
		t.Fatalf("wednesday must be auto from the legacy form: %q", raw)
	}
}

// A schedule window marked Auto lifts the limit (unlimited) so the guided tuning
// can measure the line there; a fixed window keeps its numbers.
func TestSpeedPolicyAutotuneWindowIsUnlimited(t *testing.T) {
	now := time.Now()
	window := func(download, upload int64) string {
		var week bwschedule.Week
		week[int(now.Weekday())] = bwschedule.Day{On: true, Start: 0, End: 23*60 + 59, Download: download, Upload: upload}
		return week.String()
	}

	cfg := DefaultConfig()
	cfg.Settings = map[string]string{
		"torrent_sched_enabled": "true",
		"torrent_sched_week":    window(bwschedule.AutoLimit, bwschedule.AutoLimit),
	}
	download, upload, source, _ := speedPolicy(&cfg)
	if source != speedSourceScheduleAuto || download != 0 || upload != 0 {
		t.Fatalf("auto window = %d/%d source %q, want 0/0 %q", download, upload, source, speedSourceScheduleAuto)
	}

	cfg.Settings["torrent_sched_week"] = window(3000, 200)
	download, upload, source, _ = speedPolicy(&cfg)
	if source != speedSourceSchedule || download != 3000 || upload != 200 {
		t.Fatalf("fixed window = %d/%d source %q, want 3000/200 %q", download, upload, source, speedSourceSchedule)
	}
}
