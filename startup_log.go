package gextto

// startup_log.go: makes startup and restart unmistakable in the log.
//
// A small marker in the data dir records each run, so the next start can say
// whether it is a cold start or a restart, how many runs there have been and how
// long the daemon was down. An unclean stop (kill -9, crash) leaves the marker
// without a stop time and is reported as such.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/constants"
	"github.com/buzzqw/gextto/internal/logging"
)

// runStateFile is the marker written on every start, next to the other gextto
// state in the data dir.
const runStateFile = ".gextto-run.json"

// runState is the persisted view of the previous/current run.
type runState struct {
	Runs      int    `json:"runs"`
	StartedAt string `json:"started_at"`
	StoppedAt string `json:"stopped_at,omitempty"`
	Version   string `json:"version,omitempty"`
	Build     string `json:"build,omitempty"`
	Commit    string `json:"commit,omitempty"`
	PID       int    `json:"pid,omitempty"`
}

func runStatePath(dataDir string) string { return filepath.Join(dataDir, runStateFile) }

// loadRunState reads the marker. A missing or unreadable file yields the zero
// value, which startupBanner treats as a cold start.
func loadRunState(dataDir string) runState {
	var s runState
	if data, err := os.ReadFile(runStatePath(dataDir)); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	return s
}

func saveRunState(dataDir string, s runState) {
	if data, err := json.Marshal(s); err == nil {
		_ = os.WriteFile(runStatePath(dataDir), data, 0o644)
	}
}

// buildIdentity renders the version stamped into the binary, with the short
// commit when the build provides one.
func buildIdentity() string {
	identity := constants.VersionLabel()
	if c := strings.TrimSpace(constants.Commit); c != "" {
		if len(c) > 8 {
			c = c[:8]
		}
		identity += " (commit " + c + ")"
	}
	return identity
}

// startupDetails is what the start line says besides the version: where the
// interface listens, whether downloads are enabled and which engine is used.
// They are the facts a person wants after a restart, not a second line.
type startupDetails struct {
	WebAddr string
	Mode    string
	Engine  string
}

// startupBanner is the log line for a start: one sentence with the version and
// what the user needs to know, or the restart marker with the run counter and
// how long the previous run was down. Pure so it is easy to test.
func startupBanner(prev runState, now time.Time, details startupDetails) string {
	parts := make([]string, 0, 4)
	if prev.Runs <= 0 {
		parts = append(parts, "🟢 Gextto started for the first time — "+constants.VersionLabel())
	} else {
		down := "the previous stop was not recorded"
		if t, err := time.Parse(time.RFC3339, prev.StoppedAt); err == nil && !t.IsZero() {
			down = "it was down for " + humanDuration(int64(now.Sub(t).Seconds()))
		}
		parts = append(parts, fmt.Sprintf("♻️ Gextto restarted (run #%d) — %s · %s",
			prev.Runs+1, constants.VersionLabel(), down))
	}
	if details.WebAddr != "" {
		parts = append(parts, "web interface at http://"+details.WebAddr)
	}
	if details.Mode != "" {
		parts = append(parts, details.Mode)
	}
	if details.Engine != "" {
		parts = append(parts, "engine "+details.Engine)
	}
	return strings.Join(parts, " · ")
}

// shutdownBanner is the line written on a clean stop.
func shutdownBanner(s runState, now time.Time, reason string) string {
	uptime := "0s"
	if t, err := time.Parse(time.RFC3339, s.StartedAt); err == nil && !t.IsZero() {
		uptime = humanDuration(int64(now.Sub(t).Seconds()))
	}
	return fmt.Sprintf("🛑 Gextto stopped (%s) after %s", reason, uptime)
}

// logStartupBanner logs the start line, records the run and returns the
// previous run (zero for a first start) so callers can notify on a restart.
func logStartupBanner(dataDir string, details startupDetails) runState {
	prev := loadRunState(dataDir)
	now := time.Now().UTC()
	logging.Info(startupBanner(prev, now, details))
	saveRunState(dataDir, runState{
		Runs:      prev.Runs + 1,
		StartedAt: now.Format(time.RFC3339),
		Version:   constants.AppVersion(),
		Build:     constants.Build,
		Commit:    constants.Commit,
		PID:       os.Getpid(),
	})
	return prev
}

// notifyRestart sends a restart notification when this is not the first start.
// A crash is reported here, at the next start: a dying process cannot notify.
func notifyRestart(state *AppState, prev runState) {
	if state == nil || state.notifier == nil || prev.Runs <= 0 {
		return
	}
	data := map[string]any{
		"run":     prev.Runs + 1,
		"version": constants.AppVersion(),
	}
	if t, err := time.Parse(time.RFC3339, prev.StoppedAt); err == nil && !t.IsZero() {
		data["down_seconds"] = int64(time.Since(t).Seconds())
	} else {
		data["unclean"] = true
	}
	notifier := state.notifier
	go func() {
		defer recoverGoroutine("restart notifier")
		if err := notifier.NotifyEvent("daemon_restarted", data); err != nil {
			logging.Warn("restart notification failed", "error", err)
		}
	}()
}

// notifyDaemonError reports a fatal shutdown just before the process exits.
func notifyDaemonError(state *AppState, err error) {
	if state == nil || state.notifier == nil || err == nil {
		return
	}
	if nErr := state.notifier.NotifyEvent("daemon_error", map[string]any{"error": err.Error()}); nErr != nil {
		logging.Warn("shutdown notification failed", "error", nErr)
	}
}

// recordShutdown stamps the run as cleanly stopped; the next start then shows
// how long the daemon was down. reason is the cause (signal, fatal error).
func recordShutdown(dataDir, reason string) {
	s := loadRunState(dataDir)
	if s.Runs <= 0 {
		return
	}
	now := time.Now().UTC()
	s.StoppedAt = now.Format(time.RFC3339)
	saveRunState(dataDir, s)
	logging.Info(shutdownBanner(s, now, reason))
}
