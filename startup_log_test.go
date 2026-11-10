package gextto

import (
	"strings"
	"testing"
	"time"
)

func TestStartupBannerColdRestartAndUnclean(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

	details := startupDetails{WebAddr: "0.0.0.0:5000", Mode: "downloads are enabled", Engine: "gx-torrent"}

	cold := startupBanner(runState{}, now, details)
	for _, want := range []string{"started for the first time", "web interface at http://0.0.0.0:5000", "downloads are enabled", "engine gx-torrent"} {
		if !strings.Contains(cold, want) {
			t.Fatalf("cold start banner = %q, want %q", cold, want)
		}
	}

	restart := startupBanner(runState{Runs: 2, StoppedAt: now.Add(-90 * time.Second).Format(time.RFC3339)}, now, details)
	for _, want := range []string{"restarted", "run #3", "it was down for 1m 30s"} {
		if !strings.Contains(restart, want) {
			t.Fatalf("restart banner = %q, want %q", restart, want)
		}
	}

	unclean := startupBanner(runState{Runs: 1}, now, details)
	if !strings.Contains(unclean, "the previous stop was not recorded") {
		t.Fatalf("banner without a stop time = %q, want a note about the missing stop time", unclean)
	}
}

func TestStartupSeparatorMarksOnlyRestarts(t *testing.T) {
	if got := startupSeparator(runState{}); got != "" {
		t.Fatalf("the first start must not be preceded by a divider: %q", got)
	}
	if got := startupSeparator(runState{Runs: 2}); got != restartDivider {
		t.Fatalf("restart separator = %q, want the divider", got)
	}
	if !strings.Contains(restartDivider, "━") {
		t.Fatal("the restart divider must be a visible line")
	}
}

func TestRunStateCounterAndShutdown(t *testing.T) {
	dir := t.TempDir()

	logStartupBanner(dir, startupDetails{})
	logStartupBanner(dir, startupDetails{})

	s := loadRunState(dir)
	if s.Runs != 2 {
		t.Fatalf("runs = %d, want 2", s.Runs)
	}
	if s.StartedAt == "" || s.PID == 0 {
		t.Fatalf("run not recorded: %+v", s)
	}

	recordShutdown(dir, "test signal")
	if got := loadRunState(dir); got.StoppedAt == "" {
		t.Fatalf("stopped_at not recorded: %+v", got)
	}
	if got := loadRunState(dir); got.Runs != 2 {
		t.Fatalf("shutdown changed runs = %d, want 2", got.Runs)
	}
}

func TestShutdownBannerUptime(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	line := shutdownBanner(runState{StartedAt: now.Add(-2 * time.Hour).Format(time.RFC3339)}, now, "")
	if line != "🛑 Gextto stopped after 2h 0m" {
		t.Fatalf("shutdown banner = %q", line)
	}
	if line := shutdownBanner(runState{StartedAt: now.Add(-time.Hour).Format(time.RFC3339)}, now, "fatal error: disk full"); line != "🛑 Gextto stopped after 1h 0m: fatal error: disk full" {
		t.Fatalf("fatal shutdown banner = %q", line)
	}
}

// A restart inside the search interval says when the next search runs in the
// start line itself; an elapsed interval, no previous cycle or the automatic
// cycle off leave the part out.
func TestStartupBannerCarriesTheNextSearch(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.Local)
	last := now.Add(-20 * time.Minute)
	due, ok := resumedCycleDue(true, 3600, last, true, now)
	if !ok || !due.Equal(now.Add(40*time.Minute)) {
		t.Fatalf("due = %v ok=%v, want in 40 minutes", due, ok)
	}
	note := nextSearchNote(now, due)
	if note != "next search at 12:40 (in 40 minutes)" {
		t.Fatalf("note = %q", note)
	}
	line := startupBanner(runState{Runs: 1}, now, startupDetails{Engine: "gx-torrent", NextSearch: note})
	if !strings.HasSuffix(line, "engine gx-torrent · next search at 12:40 (in 40 minutes)") {
		t.Fatalf("start line = %q", line)
	}
	for name, c := range map[string]struct {
		active, have bool
		last         time.Time
	}{
		"elapsed":     {true, true, now.Add(-2 * time.Hour)},
		"no previous": {true, false, time.Time{}},
		"cycle off":   {false, true, last},
	} {
		if _, ok := resumedCycleDue(c.active, 3600, c.last, c.have, now); ok {
			t.Fatalf("%s: a due time was reported", name)
		}
	}
}
