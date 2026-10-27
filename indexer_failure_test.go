package gextto

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// resetIndexerFailureNotices clears the throttle between tests.
func resetIndexerFailureNotices() {
	indexerFailureNotices.mu.Lock()
	indexerFailureNotices.last = map[string]time.Time{}
	indexerFailureNotices.mu.Unlock()
}

// TestIndexerFailureNoticeIsThrottledPerSource: the first break is news, the
// next cycle of the same broken login is not, a different source is
// independent, and a success re-arms the source at once.
func TestIndexerFailureNoticeIsThrottledPerSource(t *testing.T) {
	resetIndexerFailureNotices()
	defer resetIndexerFailureNotices()

	now := time.Unix(1_800_000_000, 0)
	if !claimIndexerFailureNotice("mircrew indexerr", now) {
		t.Fatal("the first notice was throttled")
	}
	if claimIndexerFailureNotice("mircrew indexerr", now.Add(time.Hour)) {
		t.Fatal("a second notice arrived inside the interval")
	}
	if !claimIndexerFailureNotice("prowlarr:tracker", now.Add(time.Hour)) {
		t.Fatal("another source was throttled by the first one")
	}
	if !claimIndexerFailureNotice("mircrew indexerr", now.Add(indexerFailureNotifyInterval)) {
		t.Fatal("the notice was not repeated after the interval")
	}
	clearIndexerFailureNotice("mircrew indexerr")
	if !claimIndexerFailureNotice("mircrew indexerr", now.Add(2*time.Hour)) {
		t.Fatal("a success did not re-arm the source")
	}
}

// TestIndexerFailureReasonIsBounded: a long reason stays one readable line and
// is cut on a rune boundary.
func TestIndexerFailureReasonIsBounded(t *testing.T) {
	got := indexerFailureReason("  a\n\tb " + strings.Repeat("è", indexerFailureReasonLimit+20))
	if strings.ContainsAny(got, "\n\t") {
		t.Fatalf("reason has a line break: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("long reason was not cut: %q", got)
	}
	if runes := []rune(got); len(runes) > indexerFailureReasonLimit+1 {
		t.Fatalf("reason is %d runes, want at most %d", len(runes), indexerFailureReasonLimit+1)
	}
	if !strings.HasPrefix(got, "a b ") {
		t.Fatalf("reason was not flattened: %q", got)
	}
}

// A transient answer (login, no magnet yet) still counts for the "indexer
// keeps failing" notice even though it does not quarantine the link.
func TestNoteTorrentLinkFailureCountsDistinctLinks(t *testing.T) {
	resetIndexerFailureNotices()
	defer resetIndexerFailureNotices()

	failures := map[string]map[string]struct{}{}
	err := errors.New("HTTP 502: MirCrew login failed")
	noteTorrentLinkFailure(failures, "mircrew indexerr", "u1", err, nil)
	noteTorrentLinkFailure(failures, "mircrew indexerr", "u1", err, nil)
	if got := len(failures["mircrew indexerr"]); got != 1 {
		t.Fatalf("the same link counted %d times", got)
	}
	noteTorrentLinkFailure(failures, "mircrew indexerr", "u2", err, nil)
	if got := len(failures["mircrew indexerr"]); got != 2 {
		t.Fatalf("two distinct links counted as %d", got)
	}
	noteTorrentLinkFailure(failures, "", "u3", err, nil)
	if len(failures) != 1 {
		t.Fatalf("a source-less failure was counted: %+v", failures)
	}
}

func TestAggregatedSourceNamesTheSite(t *testing.T) {
	manager, site, ok := aggregatedSource("indexarr:MirCrew")
	if !ok || manager != "indexarr" || site != "MirCrew" {
		t.Fatalf("aggregatedSource = %q, %q, %v", manager, site, ok)
	}
	for _, plain := range []string{"mircrew", "indexarr:", ":site", ""} {
		if _, _, ok := aggregatedSource(plain); ok {
			t.Errorf("%q split as an aggregated source", plain)
		}
	}
}
