package gextto

// health_monitor.go watches the overall health status and records in the log
// what is wrong, when it was detected and why. A degraded status is written on
// the transition (and whenever the reason changes) so a persistent problem does
// not flood the log; an hourly heartbeat records that it is still degraded, and
// a line records the recovery.

import (
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

const (
	healthMonitorFirstPass = 10 * time.Second
	healthMonitorPeriod    = 30 * time.Second
	healthMonitorHeartbeat = time.Hour
)

// healthLogState folds consecutive health snapshots into the log action to
// take, so the monitor logs transitions instead of every single check.
type healthLogState struct {
	lastStatus string
	lastReason string
	lastLogged time.Time
}

// observe returns "degraded" when the problem must be logged, "recovered" when
// a previously degraded service came back, or "" when nothing changed.
func (s *healthLogState) observe(status, reason string, now time.Time) string {
	action := ""
	switch {
	case status != "ok":
		changed := status != s.lastStatus || reason != s.lastReason
		heartbeat := !s.lastLogged.IsZero() && now.Sub(s.lastLogged) >= healthMonitorHeartbeat
		if changed || heartbeat {
			action = "degraded"
			s.lastLogged = now
		}
	case s.lastStatus != "" && s.lastStatus != "ok":
		action = "recovered"
		s.lastLogged = now
	}
	s.lastStatus = status
	s.lastReason = reason
	return action
}

// healthMonitorWorker runs the canonical health check periodically and logs a
// degraded status with its reason. The timestamp of the log line is the "when";
// the reason lists what failed and why.
func healthMonitorWorker(state *AppState) {
	if !state.SleepBackground(healthMonitorFirstPass) {
		return
	}
	var logState healthLogState
	for {
		paths := uiHealthPathsFrom(state)
		health := CheckWithPaths(paths)
		reason := healthStatusReason(health)
		switch logState.observe(health.Status, reason, time.Now()) {
		case "degraded":
			logging.Warn("health check degraded",
				"status", health.Status,
				"reason", reason,
				"data_dir", paths.DataDir,
				"data_dir_writable", health.DataDirWritable,
			)
			if state.notifier != nil {
				_ = state.notifier.NotifyEvent("health_degraded", map[string]any{
					"status": health.Status,
					"reason": reason,
				})
			}
		case "recovered":
			logging.Info("health check recovered", "status", health.Status)
			if state.notifier != nil {
				_ = state.notifier.NotifyEvent("health_recovered", map[string]any{"status": health.Status})
			}
		}
		if !state.SleepBackground(healthMonitorPeriod) {
			return
		}
	}
}
