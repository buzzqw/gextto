// Package backoff implements the escalating provider backoff, based on
// Sonarr's EscalationBackOff.
//
// A source (RSS feed, indexer, web engine) that keeps failing is temporarily
// disabled for a growing interval instead of being retried on every cycle.
// The first success steps the level back down, so recovery is automatic.
package backoff

// Periods are the disable intervals in seconds, indexed by escalation level.
// Level 0 means "not disabled"; the last entry is the cap.
var Periods = [10]int64{0, 60, 300, 900, 1800, 3600, 10800, 21600, 43200, 86400}

// MaxLevel returns the highest escalation level.
func MaxLevel() int64 { return int64(len(Periods) - 1) }

// PeriodSecs returns the seconds a provider at level stays disabled.
func PeriodSecs(level int64) int64 {
	level = clamp(level, 0, MaxLevel())
	return Periods[level]
}

// NextLevel returns the next escalation level after a failure. A failure only
// escalates when the previous one is older than the current period; rapid
// consecutive failures keep the same level (Sonarr's grace behaviour).
func NextLevel(level int64, secondsSincePreviousFailure *int64) int64 {
	level = clamp(level, 0, MaxLevel())
	if level == 0 {
		return 1
	}
	if secondsSincePreviousFailure != nil && *secondsSincePreviousFailure < PeriodSecs(level) {
		return level
	}
	return clamp(level+1, 1, MaxLevel())
}

// SuccessLevel returns the level after a success: step down one notch.
func SuccessLevel(level int64) int64 {
	if level-1 < 0 {
		return 0
	}
	return level - 1
}

func clamp(value, low, high int64) int64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
