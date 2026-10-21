package gextto

import (
	"strings"

	"github.com/buzzqw/gextto/internal/constants"
)

// restart_cause.go tells why the daemon started again.
//
// The guided tuning never reads restart counts as a measure of hardware health,
// and this is the reason it can afford to: most restarts are ordinary events —
// an update or rebuild installed a new binary, or the operator restarted the
// service — and must never be read as instability. A restart that changed the
// build identity is an update by construction, so only a restart with the same
// binary and an unclean stop could ever be a stability signal, and even then
// only to the jobs that choose to look (there are none today).

// RestartCause classifies a start relative to the previous run.
type RestartCause string

const (
	// RestartFirstStart is the very first run: nothing to compare.
	RestartFirstStart RestartCause = "first-start"
	// RestartUpdate means the binary changed (version, build or commit): an
	// update/rebuild, never instability.
	RestartUpdate RestartCause = "update"
	// RestartUnclean means the previous run left no stop time (crash, kill -9).
	RestartUnclean RestartCause = "unclean"
	// RestartManual is a clean stop and start of the same binary.
	RestartManual RestartCause = "restart"
)

// restartsMeaningInstability is intentionally empty: no tuning decision keys on
// restarts. It documents the contract — an update restart is not instability —
// so a future stability gate reads it here instead of re-deriving it wrongly.
func restartsMeaningInstability(cause RestartCause) bool {
	return cause == RestartUnclean
}

// ClassifyRestart tells an update/rebuild restart apart from an unclean stop and
// an ordinary clean restart.
func ClassifyRestart(prev runState) RestartCause {
	if prev.Runs <= 0 {
		return RestartFirstStart
	}
	if restartChangedBinary(prev) {
		return RestartUpdate
	}
	if strings.TrimSpace(prev.StoppedAt) == "" {
		return RestartUnclean
	}
	return RestartManual
}

// restartChangedBinary reports whether the previous run was a different build,
// which is what makes a restart an update rather than a stability event.
func restartChangedBinary(prev runState) bool {
	if value := strings.TrimSpace(prev.Version); value != "" && value != strings.TrimSpace(constants.AppVersion()) {
		return true
	}
	if value := strings.TrimSpace(prev.Build); value != "" && value != strings.TrimSpace(constants.Build) {
		return true
	}
	previous, current := shortCommit(prev.Commit), shortCommit(constants.Commit)
	return previous != "" && current != "" && previous != current
}

// shortCommit normalises a commit id (the daemon stores the raw one, the banner
// prints eight characters).
func shortCommit(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 8 {
		return value[:8]
	}
	return value
}
