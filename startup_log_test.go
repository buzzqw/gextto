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
	line := shutdownBanner(runState{StartedAt: now.Add(-2 * time.Hour).Format(time.RFC3339)}, now, "SIGTERM/SIGINT")
	if !strings.Contains(line, "stopped") || !strings.Contains(line, "after 2h 0m") {
		t.Fatalf("shutdown banner = %q", line)
	}
}
