package torrent

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/urldownloader"
)

// TestWebseedRetryDelay: a source parked after 429/503 waits as long as the
// server asked (the downloader caps it at an hour); every other failure gets
// the usual minute.
func TestWebseedRetryDelay(t *testing.T) {
	if got := webseedRetryDelay(errors.New("connection reset")); got != time.Minute {
		t.Errorf("generic error: delay = %v, want %v", got, time.Minute)
	}
	if got := webseedRetryDelay(&urldownloader.HTTPStatusError{Code: 429, RetryAfter: 5 * time.Minute}); got != 5*time.Minute {
		t.Errorf("429 with Retry-After: delay = %v, want 5m", got)
	}
	if got := webseedRetryDelay(&urldownloader.HTTPStatusError{Code: 503}); got != time.Minute {
		t.Errorf("503 without Retry-After: delay = %v, want 1m", got)
	}
	wrapped := fmt.Errorf("web seed: %w", &urldownloader.HTTPStatusError{Code: 429, RetryAfter: 2 * time.Minute})
	if got := webseedRetryDelay(wrapped); got != 2*time.Minute {
		t.Errorf("wrapped 429: delay = %v, want 2m", got)
	}
}
