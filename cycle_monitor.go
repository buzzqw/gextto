package gextto

// cycle_monitor.go watches the search-cycle history and warns when a source
// keeps failing or when no cycle has completed cleanly for too long. It reads
// the same cycle_history the Salute "Ricerche" panel shows, so the data the
// operator sees and the data the warnings fire on cannot diverge.
//
// Like health_monitor.go it logs the warning on the transition (and with an
// hourly heartbeat) instead of every check, and records the recovery, so a
// persistent problem does not flood the log or the notifications.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

const (
	cycleMonitorFirstPass = 2 * time.Minute
	cycleMonitorPeriod    = 30 * time.Minute
	cycleMonitorHeartbeat = time.Hour
	// cycleMonitorHistory is how many recent cycles the warnings look at.
	cycleMonitorHistory = 12
	// cycleSourceFailStreak is how many consecutive cycles a single source must
	// fail before it is worth a warning.
	cycleSourceFailStreak = 3
	// cycleSlowWarning is a cycle duration long enough to point at a runaway or
	// very slow search (the default interval is six hours).
	cycleSlowWarning = 3 * time.Hour
	// cycleStaleFactor multiplies the configured search interval: after this
	// many intervals without a clean cycle the search is considered stuck, but
	// never before cycleStaleFloor (so a short interval still tolerates one bad
	// cycle).
	cycleStaleFactor = 3
	cycleStaleFloor  = 2 * time.Hour
)

// cycleWarningLogState folds consecutive observations into the log action to
// take, so the monitor logs transitions (plus an hourly heartbeat) instead of
// every check.
type cycleWarningLogState struct {
	lastReason string
	lastLogged time.Time
}

// observe returns "warning" when the problem must be logged, "recovered" when a
// previously warned search is clean again, or "" when nothing changed.
func (s *cycleWarningLogState) observe(reason string, now time.Time) string {
	action := ""
	switch {
	case reason != "":
		changed := reason != s.lastReason
		heartbeat := !s.lastLogged.IsZero() && now.Sub(s.lastLogged) >= cycleMonitorHeartbeat
		if changed || heartbeat {
			action = "warning"
			s.lastLogged = now
		}
	case s.lastReason != "":
		action = "recovered"
		s.lastLogged = now
	}
	s.lastReason = reason
	return action
}

// cycleWarningReason describes what the cycle history shows as a problem, or ""
// when the searches look healthy. Cycles are newest first.
func cycleWarningReason(cycles []models.CycleHistoryEntry, interval time.Duration, now time.Time) string {
	if len(cycles) == 0 {
		return ""
	}
	var problems []string

	// A source that failed in the last cycleSourceFailStreak cycles in a row.
	streaks := map[string]int{}
	for _, source := range cycles[0].Sources {
		if source.Fail == 0 {
			continue
		}
		streak := 0
		for _, cycle := range cycles {
			stat, ok := cycleSource(cycle, source.Kind, source.Name)
			if !ok || stat.Fail == 0 {
				break
			}
			streak++
		}
		if streak >= cycleSourceFailStreak {
			streaks[source.Name] = streak
		}
	}
	names := make([]string, 0, len(streaks))
	for name := range streaks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		problems = append(problems, fmt.Sprintf("la sorgente %s non risponde da %d cicli", name, streaks[name]))
	}

	// A single cycle that took far too long.
	if cycles[0].DurationSeconds > int(cycleSlowWarning.Seconds()) {
		problems = append(problems, fmt.Sprintf("l'ultima ricerca ha impiegato %s",
			logging.HumanDuration(int64(cycles[0].DurationSeconds))))
	}

	// Too long since a cycle completed without errors.
	if threshold := cycleStaleThreshold(interval); threshold > 0 {
		var lastSuccess, oldest time.Time
		for _, cycle := range cycles {
			at := cycleTime(cycle)
			if at.IsZero() {
				continue
			}
			if oldest.IsZero() || at.Before(oldest) {
				oldest = at
			}
			if cycle.Errors == 0 && at.After(lastSuccess) {
				lastSuccess = at
			}
		}
		since := lastSuccess
		if since.IsZero() {
			// No clean cycle in the window: use the oldest as a lower bound.
			since = oldest
		}
		if !since.IsZero() && now.Sub(since) > threshold {
			problems = append(problems, fmt.Sprintf("nessuna ricerca senza errori da %s",
				logging.HumanDuration(int64(now.Sub(since).Seconds()))))
		}
	}
	return strings.Join(problems, "; ")
}

// cycleStaleThreshold is how long the search may go without a clean cycle
// before it is considered stuck.
func cycleStaleThreshold(interval time.Duration) time.Duration {
	threshold := interval * cycleStaleFactor
	if threshold < cycleStaleFloor {
		threshold = cycleStaleFloor
	}
	return threshold
}

// cycleSource finds one source in a cycle by kind and name.
func cycleSource(cycle models.CycleHistoryEntry, kind, name string) (models.CycleSourceStat, bool) {
	for _, source := range cycle.Sources {
		if source.Kind == kind && source.Name == name {
			return source, true
		}
	}
	return models.CycleSourceStat{}, false
}

// cycleTime is when a stored cycle ran: the row time (UTC), falling back to the
// cycle's own start time.
func cycleTime(cycle models.CycleHistoryEntry) time.Time {
	if at, err := time.ParseInLocation("2006-01-02 15:04:05", cycle.At, time.UTC); err == nil {
		return at
	}
	if cycle.LastStartedAt != nil {
		return *cycle.LastStartedAt
	}
	return time.Time{}
}

// cycleHealthWorker runs the cycle-history check periodically and warns, with a
// reason, when a source keeps failing or the search is stuck or too slow.
func cycleHealthWorker(state *AppState) {
	if !state.SleepBackground(cycleMonitorFirstPass) {
		return
	}
	var logState cycleWarningLogState
	for {
		reason := ""
		cfg := latestConfig(state)
		if cfg.Active && state.db != nil {
			interval := time.Duration(cfg.RefreshSecs) * time.Second
			if cycles, err := state.db.RecentCycleStats(cycleMonitorHistory); err == nil {
				reason = cycleWarningReason(cycles, interval, time.Now())
			}
		}
		now := time.Now()
		switch logState.observe(reason, now) {
		case "warning":
			logging.Warn("search needs attention", "reason", reason)
			if state.notifier != nil {
				_ = state.notifier.NotifyEvent("cycle_warning", map[string]any{"reason": reason})
			}
		case "recovered":
			logging.Info("search healthy again")
			if state.notifier != nil {
				_ = state.notifier.NotifyEvent("cycle_recovered", map[string]any{})
			}
		}
		if !state.SleepBackground(cycleMonitorPeriod) {
			return
		}
	}
}
