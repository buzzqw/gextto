package gextto

import (
	"os"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/tvmaze"
)

// TestMain shortens the waits that only pace real servers and processes:
// retry back-offs, per-site gaps, polls. The test servers answer at once, and
// waiting the production seconds made the suite spend half its time asleep.
// A test about one of these waits sets the variable it checks itself.
func TestMain(m *testing.M) {
	feedRetryUnit = 5 * time.Millisecond
	feedRateLimitPenalty = 20 * time.Millisecond
	feedHostMaxPenalty = 200 * time.Millisecond
	hostMinInterval = 5 * time.Millisecond
	notifierRetryUnit = 5 * time.Millisecond
	telegramMinGap = 5 * time.Millisecond
	tmdbRetryUnit = 5 * time.Millisecond
	comicsRetryUnit = 5 * time.Millisecond
	gxAdoptedPollInterval = 20 * time.Millisecond
	// No test reaches the real TVmaze: a test that wants it installs its own
	// fake server (series_end_status_test.go).
	tvmazePacing.gap = 0
	newSeriesTvmazeClient = func() *tvmaze.Client {
		client := tvmaze.New()
		client.BaseURL = "http://127.0.0.1:1"
		return client
	}
	os.Exit(m.Run())
}
