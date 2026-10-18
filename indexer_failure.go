package gextto

// Repeated failures of one source's `.torrent` links. An expired login, a
// revoked passkey or a proxy that is down makes every magnet request fail with
// the source's own answer (mircrew says "MirCrew login failed: credenziali
// rifiutate o verifica richiesta"). One failure is transient; the same source
// failing on two different links in a cycle is the operator's cue to sign in
// again and, until it works, disable that indexer in the searches.
//
// The log line is written every cycle: the log is where the operator looks. The
// notification is throttled per source, so a login that stays broken for days
// cannot flood the chat; a link that resolves clears the source at once, so a
// new break is news again.

import (
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

const (
	// indexerFailureNotifyLimit is how many distinct `.torrent` links from the
	// same source must fail in one cycle. Two is already "repeated": a one-off
	// 502 or a removed torrent is not news, a broken login fails every link.
	indexerFailureNotifyLimit = 2
	// indexerFailureNotifyInterval is the minimum gap between two notices for
	// the same source.
	indexerFailureNotifyInterval = 12 * time.Hour
	// indexerFailureReasonLimit bounds the reason carried to the notification.
	indexerFailureReasonLimit = 300
)

var indexerFailureNotices = struct {
	mu   sync.Mutex
	last map[string]time.Time
}{last: map[string]time.Time{}}

// claimIndexerFailureNotice records a notice for the source and reports whether
// it is due: the first one, or one older than the interval.
func claimIndexerFailureNotice(source string, now time.Time) bool {
	source = strings.TrimSpace(source)
	if source == "" {
		source = "indexer"
	}
	indexerFailureNotices.mu.Lock()
	defer indexerFailureNotices.mu.Unlock()
	if at, ok := indexerFailureNotices.last[source]; ok && now.Sub(at) < indexerFailureNotifyInterval {
		return false
	}
	indexerFailureNotices.last[source] = now
	return true
}

// clearIndexerFailureNotice re-arms the source: a link resolved, so the next
// failure is news again.
func clearIndexerFailureNotice(source string) {
	source = strings.TrimSpace(source)
	if source == "" {
		return
	}
	indexerFailureNotices.mu.Lock()
	delete(indexerFailureNotices.last, source)
	indexerFailureNotices.mu.Unlock()
}

// noteTorrentLinkFailure counts one failed link per source and tells the user
// when a source keeps failing. A transient answer counts too: a login that
// keeps failing is exactly what the notice is for, even though it must not
// quarantine the link.
func noteTorrentLinkFailure(failures map[string]map[string]struct{}, source, rawURL string, err error, notifier *Notifier) {
	source = strings.TrimSpace(source)
	rawURL = strings.TrimSpace(rawURL)
	if source == "" || rawURL == "" {
		return
	}
	links := failures[source]
	if links == nil {
		links = map[string]struct{}{}
		failures[source] = links
	}
	links[rawURL] = struct{}{}
	if len(links) == indexerFailureNotifyLimit {
		notifyIndexerFailure(notifier, source, len(links), err.Error())
	}
}

// notifyIndexerFailure logs that a source keeps failing and, when the source's
// notice is due, sends the notification. The text names the action: sign in to
// the indexer again and disable it in the searches until it works.
func notifyIndexerFailure(notifier *Notifier, source string, failures int, reason string) {
	source = strings.TrimSpace(source)
	if source == "" {
		source = "indexer"
	}
	reason = indexerFailureReason(reason)
	logging.Warn("indexer failing repeatedly: re-login or disable it in the searches",
		"source", source, "failed_links", failures, "error", reason)
	if !claimIndexerFailureNotice(source, time.Now()) || notifier == nil {
		return
	}
	_ = notifier.NotifyEvent("indexer_failing", map[string]any{
		"source":       source,
		"failed_links": failures,
		"error":        reason,
	})
}

// indexerFailureReason flattens a reason to one bounded line.
func indexerFailureReason(reason string) string {
	return boundReason(strings.Join(strings.Fields(reason), " "), indexerFailureReasonLimit)
}

// boundReason truncates on rune boundaries and marks the cut, so a multibyte
// character is never split in two.
func boundReason(reason string, limit int) string {
	runes := []rune(reason)
	if limit <= 0 || len(runes) <= limit {
		return reason
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}
