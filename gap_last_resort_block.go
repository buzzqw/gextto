package gextto

// gap_last_resort_block.go keeps the last-resort pass honest: a copy the
// blocklist keeps out is not announced as taken, because nothing would start.
// The log says instead that the only copy of the gap is blocked, why and until
// when, so the operator can remove it from the blocklist or wait.

import (
	"fmt"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

// blockedLastResort is one copy the blocklist keeps out of the cycle, with the
// words the log uses for it.
type blockedLastResort struct {
	reason    string
	expiresAt string
}

// splitLastResorts separates the copies that miss only the profile into the
// ones this cycle can start and the ones the blocklist refuses. A copy with no
// hash cannot be matched against the blocklist and stays usable.
func splitLastResorts(db *Database, copies []models.Release, blockedHashes map[string]struct{}) (usable []models.Release, blocked []blockedLastResort) {
	if len(copies) == 0 {
		return nil, nil
	}
	usable = make([]models.Release, 0, len(copies))
	for i := range copies {
		release := copies[i]
		hash, ok := releaseHash(&release)
		if !ok {
			usable = append(usable, release)
			continue
		}
		if _, isBlocked := blockedHashes[strings.ToLower(hash)]; !isBlocked {
			usable = append(usable, release)
			continue
		}
		entry := blockedLastResort{reason: blocklistReasonText("")}
		if db != nil {
			if record, found := db.BlocklistRecordFor(hash); found {
				entry.reason = blocklistReasonText(record.Reason)
				entry.expiresAt = record.ExpiresAt
			}
		}
		blocked = append(blocked, entry)
	}
	return usable, blocked
}

// logBlockedLastResorts says why the only copies found for a gap outside the
// quality profile were not started. Without this line the cycle announced them
// as taken as a last resort and nothing ever downloaded, with no other line
// explaining it.
func logBlockedLastResorts(series string, season, episode int64, blocked []blockedLastResort) {
	if len(blocked) == 0 {
		return
	}
	subject := "the only copy in the right language is"
	tail := "remove it from the blocklist to try it again"
	verb := " and was not started"
	if len(blocked) > 1 {
		subject = fmt.Sprintf("the %s in the right language are", countLabel(int64(len(blocked)), "copy", "copies"))
		tail = "remove them from the blocklist to try them again"
		verb = " and none of them was started"
	}
	if when, ok := lastResortRetryDate(blocked); ok {
		tail = "it will be tried again on " + when
		if len(blocked) > 1 {
			tail = "they will be tried again on " + when
		}
	}
	logging.Info(fmt.Sprintf("🆘 Nothing within the quality profile for the missing %s S%02dE%02d: %s on the blocklist (%s)%s: %s",
		series, season, episode, subject, blockedReasonsText(blocked), verb, tail))
}

// blockedReasonsText lists the reasons once each: two copies blocked for the
// same reason read as one.
func blockedReasonsText(blocked []blockedLastResort) string {
	seen := map[string]struct{}{}
	reasons := make([]string, 0, len(blocked))
	for i := range blocked {
		reason := strings.TrimSpace(blocked[i].reason)
		if reason == "" {
			continue
		}
		if _, duplicate := seen[reason]; duplicate {
			continue
		}
		seen[reason] = struct{}{}
		reasons = append(reasons, reason)
	}
	return strings.Join(reasons, ", ")
}

// lastResortRetryDate is when the blocked copies come back: the earliest date,
// because that is when the gap gets its first chance again. A single copy
// blocked for good has no date, and the log says how to remove it instead.
func lastResortRetryDate(blocked []blockedLastResort) (string, bool) {
	earliest := time.Time{}
	for i := range blocked {
		raw := strings.TrimSpace(blocked[i].expiresAt)
		if raw == "" {
			return "", false
		}
		when, err := time.Parse("2006-01-02 15:04:05", raw)
		if err != nil {
			return "", false
		}
		if earliest.IsZero() || when.Before(earliest) {
			earliest = when
		}
	}
	if earliest.IsZero() {
		return "", false
	}
	return earliest.UTC().Format("2 January 2006"), true
}
