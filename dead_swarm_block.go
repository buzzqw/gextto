package gextto

// dead_swarm_block.go decides how long a release stays away when the swarm
// it belongs to is empty. The block is temporary because a swarm can revive;
// the period grows when the same release is refused again, so a release
// nobody is sharing stops coming back at every cycle.
//
// The refusal count is kept in its own table (`dead_swarm_strikes`): the
// blocklist row disappears when the block expires, while the count must
// outlive it.

import (
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

// deadSwarmStrikeRetention is how long the refusal count of a release is kept
// after its last refusal. It is the memory the longer periods are built on:
// a count from a year ago is not a reason to keep a release away for months.
const deadSwarmStrikeRetention = 180 * 24 * time.Hour

// deadSwarmPeriodCap is the longest period the escalation adds, whatever the
// refusal count. A base period the operator sets above it is used as it is.
const deadSwarmPeriodCap = 90 * 24 * time.Hour

// deadSwarmBlockPeriod returns how long a release refused for an empty swarm
// stays away, given the configured base period and how many times it has been
// refused (this one included): the base period the first time, four times as
// long the second, twelve times the third, never more than three months. A
// zero base keeps the release blocked forever, as the setting says.
func deadSwarmBlockPeriod(base time.Duration, strikes int) time.Duration {
	if base <= 0 {
		return 0
	}
	// The operator asked for a period longer than the escalation cap: use it
	// as it is (and multiply nothing, which would overflow).
	if base >= deadSwarmPeriodCap {
		return base
	}
	multiplier := 12
	switch {
	case strikes <= 1:
		multiplier = 1
	case strikes == 2:
		multiplier = 4
	}
	period := base * time.Duration(multiplier)
	if period > deadSwarmPeriodCap {
		period = deadSwarmPeriodCap
	}
	return period
}

// blockDeadSwarmRelease keeps a release the swarm gate refused for an empty
// swarm out of the next searches: evaluating it again at every cycle only
// repeats the same line in the log. The period follows
// `torrent_dead_swarm_blocklist_days` and grows when the release is refused
// again; zero keeps it blocked forever, like the operator's own entries.
func blockDeadSwarmRelease(db *Database, cfg *Config, release *models.Release) {
	if db == nil || cfg == nil || cfg.DryRun || release == nil {
		return
	}
	if _, err := db.BlocklistDeadSwarm(release, tevDeadSwarmBlocklistTTL(cfg)); err != nil {
		logging.Debug("could not blocklist a release nobody is sharing", "target", releaseTarget(release), "error", err)
	}
}
