package gextto

import (
	"os"
	"testing"
	"time"
)

// TestMain shortens the waits that only pace real servers and processes:
// retry back-offs, per-site gaps, polls. The test servers answer at once, and
// waiting the production seconds made the suite spend half its time asleep.
// A test about one of these waits sets the variable it checks itself.
func TestMain(m *testing.M) {
	feedRetryUnit = 5 * time.Millisecond
	feedRateLimitPenalty = 20 * time.Millisecond
	hostMinInterval = 5 * time.Millisecond
	notifierRetryUnit = 5 * time.Millisecond
	telegramMinGap = 5 * time.Millisecond
	tmdbRetryUnit = 5 * time.Millisecond
	comicsRetryUnit = 5 * time.Millisecond
	gxAdoptedPollInterval = 20 * time.Millisecond
	os.Exit(m.Run())
}
