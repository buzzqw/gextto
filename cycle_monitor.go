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
	// cycleSourceFailCount is how many times a single source may fail across the
	// recent cycles before it is worth a warning. Counting across the window
	// (not consecutive cycles) keeps the warning working when the provider
	// backoff skips a source for a cycle.
	cycleSourceFailCount = 3
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

// cycleProblem is one reason the search needs attention. key is stable across
// checks while the problem persists; text may carry the elapsed time and is
// what the operator reads.
type cycleProblem struct {
	key  string
	text string
}

// cycleWarningLogState folds consecutive observations into the log action to
// take, so the monitor logs transitions (plus an hourly heartbeat) instead of
// every check.
type cycleWarningLogState struct {
	lastKey    string
	lastLogged time.Time
}

// observe takes the stable problem key (not the text, whose elapsed time
// changes at every check) and returns "warning" when the problem must be
// logged, "recovered" when a previously warned search is clean again, or ""
// when nothing changed.
func (s *cycleWarningLogState) observe(key string, now time.Time) string {
	action := ""
	switch {
	case key != "":
		changed := key != s.lastKey
		heartbeat := !s.lastLogged.IsZero() && now.Sub(s.lastLogged) >= cycleMonitorHeartbeat
		if changed || heartbeat {
			action = "warning"
			s.lastLogged = now
		}
	case s.lastKey != "":
		action = "recovered"
		s.lastLogged = now
	}
	s.lastKey = key
	return action
}

// cycleSourceErrorLabel turns a source's last error into a short reason for
// the warning: a timeout says the source is slow, not down.
func cycleSourceErrorLabel(lastError string) string {
	lower := strings.ToLower(lastError)
	switch {
	case lower == "":
		return ""
	case strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout"):
		return "risposte troppo lente: richieste scadute"
	case len(lastError) > 80:
		return lastError[:80] + "…"
	default:
		return lastError
	}
}

// cycleProblems describes what the cycle history shows as a problem. Cycles
// are newest first.
func cycleProblems(cycles []models.CycleHistoryEntry, interval time.Duration, now time.Time) []cycleProblem {
	if len(cycles) == 0 {
		return nil
	}
	var problems []cycleProblem

	// A source that failed cycleSourceFailCount times across the recent cycles.
	type sourceTally struct {
		name      string
		attempts  int
		failures  int
		lastError string
	}
	tally := map[string]*sourceTally{}
	for _, cycle := range cycles {
		for _, source := range cycle.Sources {
			key := source.Kind + "\x01" + source.Name
			entry := tally[key]
			if entry == nil {
				entry = &sourceTally{name: source.Name}
				tally[key] = entry
			}
			entry.attempts++
			// A cycle counts as failed for a source only when most of its
			// requests failed: a manager answering 16 searches out of 29 is
			// working, slowly, not failing.
			if source.Fail > source.OK {
				entry.failures++
				if entry.lastError == "" {
					entry.lastError = source.LastError
				}
			}
		}
	}
	keys := make([]string, 0, len(tally))
	for key := range tally {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		entry := tally[key]
		if entry.failures >= cycleSourceFailCount {
			problem := fmt.Sprintf("la sorgente %s ha fallito in %d delle ultime %d ricerche",
				entry.name, entry.failures, entry.attempts)
			if reason := cycleSourceErrorLabel(entry.lastError); reason != "" {
				problem += " (" + reason + ")"
			}
			problems = append(problems, cycleProblem{key: "source:" + key, text: problem})
		}
	}

	// A single cycle that took far too long.
	if cycles[0].DurationSeconds > int(cycleSlowWarning.Seconds()) {
		problems = append(problems, cycleProblem{
			key: "slow",
			text: fmt.Sprintf("l'ultima ricerca ha impiegato %s",
				logging.HumanDuration(int64(cycles[0].DurationSeconds))),
		})
	}

	// Too long since a cycle completed without real errors.
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
			if cycleSearchErrors(cycle) == 0 && at.After(lastSuccess) {
				lastSuccess = at
			}
		}
		since := lastSuccess
		if since.IsZero() {
			// No clean cycle in the window: use the oldest as a lower bound.
			since = oldest
		}
		if !since.IsZero() && now.Sub(since) > threshold {
			problems = append(problems, cycleProblem{
				key: "stale",
				text: fmt.Sprintf("nessuna ricerca senza errori da %s",
					logging.HumanDuration(int64(now.Sub(since).Seconds()))),
			})
		}
	}
	return problems
}

// cycleSearchErrors counts the errors that mean the search itself failed. A
// dead .torrent link is a download-side problem: it has its own warning and
// the link quarantine, and it must not keep the search "not clean" for days.
func cycleSearchErrors(cycle models.CycleHistoryEntry) int {
	errors := cycle.Errors
	for category, count := range cycle.ErrorDetails {
		if category == "torrent_link" {
			errors -= count
		}
	}
	if errors < 0 {
		return 0
	}
	return errors
}

// cycleProblemsText joins the human reasons.
func cycleProblemsText(problems []cycleProblem) string {
	texts := make([]string, 0, len(problems))
	for _, problem := range problems {
		texts = append(texts, problem.text)
	}
	return strings.Join(texts, "; ")
}

// cycleProblemsKey joins the stable identities. It changes when a problem
// appears or disappears, not when its elapsed time is redisplayed.
func cycleProblemsKey(problems []cycleProblem) string {
	keys := make([]string, 0, len(problems))
	for _, problem := range problems {
		keys = append(keys, problem.key)
	}
	return strings.Join(keys, "; ")
}

// cycleWarningReason is the operator-facing description, or "" when the
// searches look healthy.
func cycleWarningReason(cycles []models.CycleHistoryEntry, interval time.Duration, now time.Time) string {
	return cycleProblemsText(cycleProblems(cycles, interval, now))
}

// cycleWarningKey is the stable identity used to decide whether a warning is
// new. It deliberately leaves the elapsed time out: the reason text changes at
// every check, the key changes only when the problem does.
func cycleWarningKey(cycles []models.CycleHistoryEntry, interval time.Duration, now time.Time) string {
	return cycleProblemsKey(cycleProblems(cycles, interval, now))
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
		key := ""
		cfg := latestConfig(state)
		if cfg.Active && state.db != nil {
			interval := time.Duration(cfg.RefreshSecs) * time.Second
			if cycles, err := state.db.RecentCycleStats(cycleMonitorHistory); err == nil {
				problems := cycleProblems(cycles, interval, time.Now())
				reason = cycleProblemsText(problems)
				key = cycleProblemsKey(problems)
			}
		}
		now := time.Now()
		switch logState.observe(key, now) {
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
