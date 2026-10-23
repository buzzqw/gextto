package gextto

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/bwschedule"
)

// TestBGSpeedJumpedUsesTenPercent pins the log throttle: a 5 % step is not a
// new line, an 11 % step is, and a move from or to unlimited always is.
func TestBGSpeedJumpedUsesTenPercent(t *testing.T) {
	cases := []struct {
		prev, next int64
		want       bool
	}{
		{1000, 1050, false},
		{1000, 1099, false},
		{1000, 1100, true}, // exactly 10 % counts
		{1000, 900, true},  // exactly 10 % counts in both directions
		{1000, 890, true},
		{0, 1000, true},
		{1000, 0, true},
		{0, 0, false},
	}
	for _, tc := range cases {
		if got := bg_speedJumped(tc.prev, tc.next); got != tc.want {
			t.Errorf("bg_speedJumped(%d, %d) = %v, want %v", tc.prev, tc.next, got, tc.want)
		}
	}
}

// TestBGSpeedWorthLoggingConsidersTheTrend: a reversal of only 11 % is noise
// and waits, while a change in the same direction (or a large reversal) is
// worth a line.
func TestBGSpeedWorthLoggingConsidersTheTrend(t *testing.T) {
	cases := []struct {
		name                             string
		prevLogged, current, prevApplied int64
		want                             bool
	}{
		{"first jump", 0, 1000, 0, true},
		{"same direction", 1000, 1110, 1000, true},
		{"below threshold", 1000, 1099, 1000, false},
		{"exactly ten", 1000, 900, 1000, true},
		{"small reversal", 1000, 1110, 900, false},
		{"continuation", 1000, 1250, 1110, true},
		{"large reversal", 1000, 800, 1000, true},
	}
	for _, tc := range cases {
		if got := bg_speedWorthLogging(tc.prevLogged, tc.current, tc.prevApplied); got != tc.want {
			t.Errorf("%s: bg_speedWorthLogging(%d, %d, %d) = %v, want %v", tc.name, tc.prevLogged, tc.current, tc.prevApplied, got, tc.want)
		}
	}
}

// TestBGAdoptsTheGuardsFirstCapLine: the guard prints the session's first cap
// itself, at once. The periodic line must adopt that line as its last one
// instead of repeating the same numbers (and reading "from unlimited") a few
// seconds later.
func TestBGAdoptsTheGuardsFirstCapLine(t *testing.T) {
	last := time.Unix(1_800_000_000, 0)
	if at, ok := bg_adoptFirstCap(last, time.Time{}); ok || !at.Equal(last) {
		t.Fatalf("no guard line: adopted=%v at=%v", ok, at)
	}
	if _, ok := bg_adoptFirstCap(last, last); ok {
		t.Fatal("adopted the line the logger already printed")
	}
	guardAt := last.Add(14 * time.Second)
	at, ok := bg_adoptFirstCap(last, guardAt)
	if !ok || !at.Equal(guardAt) {
		t.Fatalf("guard line after the last one: adopted=%v at=%v", ok, at)
	}
	if _, ok := bg_adoptFirstCap(guardAt.Add(time.Second), guardAt); ok {
		t.Fatal("adopted an older guard line")
	}
}

// TestBGSpeedDeltaText pins the direction shown in the autotune line.
func TestBGSpeedDeltaText(t *testing.T) {
	for _, tc := range []struct {
		prev, next int64
		want       string
	}{
		{1000, 1110, "+11%"},
		{1000, 900, "-10%"},
		{0, 1000, "from unlimited"},
		{1000, 0, "to unlimited"},
		{1000, 1000, "="},
	} {
		if got := bg_speedDeltaText(tc.prev, tc.next); got != tc.want {
			t.Errorf("bg_speedDeltaText(%d, %d) = %q, want %q", tc.prev, tc.next, got, tc.want)
		}
	}
}

// TestBGSpeedMaxTextShowsTheConfiguredCeiling: the autotune line names the
// maximum the user configured, not the controller's internal ceiling.
func TestBGSpeedMaxTextShowsTheConfiguredCeiling(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Settings = map[string]string{"torrent_speed_auto": "1"}
	if got := bg_speedMaxText(&cfg); got != "no line max" {
		t.Fatalf("auto max text = %q, want no line max", got)
	}
	cfg.Settings["torrent_line_max_down_kib"] = "20000"
	cfg.Settings["torrent_line_max_up_kib"] = "2000"
	if got := bg_speedMaxText(&cfg); got != "line max 19.5 MB/s down, 2.0 MB/s up" {
		t.Fatalf("line max text = %q", got)
	}
	cfg.Settings = map[string]string{}
	cfg.Transfer.DownloadLimitKib, cfg.Transfer.UploadLimitKib = 8000, 1000
	if got := bg_speedMaxText(&cfg); got != "" {
		t.Fatalf("fixed limits should not repeat the maximum, got %q", got)
	}
}

func TestAutotuneDigestAllUnlimited(t *testing.T) {
	var g bg_autotuneDigest
	start := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	if !g.add(start, 0, 0, "free") {
		t.Fatal("the first step was not logged")
	}
	g.add(start.Add(time.Minute), 0, 0, "free")
	line, ok := g.summary(start.Add(15*time.Minute), 0, 0)
	if !ok {
		t.Fatal("no digest")
	}
	want := "🎚️ Autotune (last 15m 00s): 1 adjustment, no speed limit the whole time."
	if line != want {
		t.Fatalf("digest = %q, want %q", line, want)
	}
}

func TestAutotuneStepTextDoesNotRepeatUnlimited(t *testing.T) {
	if got := bg_autotuneStepText(0, 0, []string{"upload to unlimited"}, "", ""); got != "🎚️ Autotune: no speed limit" {
		t.Fatalf("both unlimited = %q", got)
	}
	if got := bg_autotuneStepText(128, 0, []string{"download -78%"}, "", ""); got != "🎚️ Autotune: download 128 KB/s, upload unlimited · download -78%" {
		t.Fatalf("download step = %q", got)
	}
	if got := bg_autotuneStepText(0, 71, []string{"upload from unlimited"}, "", ""); got != "🎚️ Autotune: download unlimited, upload 71 KB/s · upload from unlimited" {
		t.Fatalf("upload step = %q", got)
	}
}

func TestSpeedStatusTextSpeaksPlainly(t *testing.T) {
	if got := bg_speedStatusText(0, 0, "Auto", "no line max", ""); got != "🚦 Speed: no speed limit (Auto)" {
		t.Fatalf("unlimited speed line = %q", got)
	}
	if got := bg_speedStatusText(19170, 214, "Auto", "", "ping 15 ms (baseline 13 ms)"); got != "🚦 Speed: download 18.7 MB/s, upload 214 KB/s (Auto) · ping 15 ms (baseline 13 ms)" {
		t.Fatalf("capped speed line = %q", got)
	}
	if got := bg_speedStatusText(500, 0, "scheduled limit", "line max 19.5 MB/s down, 2.0 MB/s up", ""); got != "🚦 Speed: download 500 KB/s, upload unlimited (scheduled limit) · line max 19.5 MB/s down, 2.0 MB/s up" {
		t.Fatalf("scheduled speed line = %q", got)
	}
}

// TestBGPingTextRendersLatency: the speed line carries the guard's latency and
// its idle reference.
func TestBGPingTextRendersLatency(t *testing.T) {
	if got := bg_pingText(latencyGuardStatus{LatencyMs: 23, BaselineMs: 14}); got != "ping 23 ms (baseline 14 ms)" {
		t.Fatalf("ping text = %q", got)
	}
	if got := bg_pingText(latencyGuardStatus{}); got != "" {
		t.Fatalf("missing ping text = %q, want no detail", got)
	}
}

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
	download, upload, source := speedPolicy(&cfg)
	if source != speedSourceScheduleAuto || download != 0 || upload != 0 {
		t.Fatalf("auto window = %d/%d source %q, want 0/0 %q", download, upload, source, speedSourceScheduleAuto)
	}

	cfg.Settings["torrent_sched_week"] = window(3000, 200)
	download, upload, source = speedPolicy(&cfg)
	if source != speedSourceSchedule || download != 3000 || upload != 200 {
		t.Fatalf("fixed window = %d/%d source %q, want 3000/200 %q", download, upload, source, speedSourceSchedule)
	}
}

// TestAutotuneDigest: the first step of a mode is logged at once, the following
// steps are gathered and summed up once per window, with unlimited as the top
// of the range.
func TestAutotuneDigest(t *testing.T) {
	var g bg_autotuneDigest
	start := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	if !g.add(start, 400, 0, "control") {
		t.Fatal("the first step was not logged")
	}
	for i, down := range []int64{128, 718, 128, 1057, 0} {
		if g.add(start.Add(time.Duration(i+1)*time.Minute), down, 0, "control") {
			t.Fatalf("step %d logged at info level", i)
		}
	}
	if _, ok := g.summary(start.Add(10*time.Minute), 128, 0); ok {
		t.Fatal("digest before its window")
	}
	line, ok := g.summary(start.Add(15*time.Minute), 128, 0)
	if !ok {
		t.Fatal("no digest after the window")
	}
	for _, want := range []string{"5 adjustments", "Download 128 KB/s–unlimited", "upload no limit", "now download 128 KB/s, upload unlimited"} {
		if !strings.Contains(line, want) {
			t.Errorf("digest %q lacks %q", line, want)
		}
	}
	if _, ok := g.summary(start.Add(31*time.Minute), 128, 0); ok {
		t.Fatal("digest without steps")
	}
	if !g.add(start.Add(32*time.Minute), 0, 0, "free") {
		t.Fatal("a new mode was not logged at once")
	}
}
