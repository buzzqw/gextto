package gextto

// v2_replacement.go remembers the releases refused because they are
// BitTorrent v2-only (gx-torrent cannot download them) and, when another
// release of the same episode or movie is downloaded instead, says so in the
// log.

import (
	"fmt"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

const v2SkipMemory = 30 * 24 * time.Hour

// v2NoticeEvery spaces the reminders for a v2 release still not replaced.
const v2NoticeEvery = 24 * time.Hour

type v2SkippedRelease struct {
	title    string
	label    string
	at       time.Time
	noticeAt time.Time
}

var v2Skipped = struct {
	sync.Mutex
	byTarget map[string]v2SkippedRelease
}{byTarget: map[string]v2SkippedRelease{}}

// rememberV2Skip records a v2-only release that would have been downloaded.
func rememberV2Skip(release *models.Release, now time.Time) {
	v2Skipped.Lock()
	defer v2Skipped.Unlock()
	target := releaseTarget(release)
	if entry, known := v2Skipped.byTarget[target]; known {
		// Another v2-only release of the same target: keep the first date so
		// the reminders and the 30-day limit still apply.
		entry.title = release.Title
		v2Skipped.byTarget[target] = entry
		return
	}
	v2Skipped.byTarget[target] = v2SkippedRelease{title: release.Title, label: logTarget(release), at: now}
}

// reportPendingV2Skips runs at the end of a search cycle: it says which
// v2-only releases still have no v1 or hybrid replacement (right after the
// cycle that refused them, then once a day) and when Gextto stops tracking
// them after 30 days.
func reportPendingV2Skips(now time.Time) {
	v2Skipped.Lock()
	defer v2Skipped.Unlock()
	for target, entry := range v2Skipped.byTarget {
		waited := now.Sub(entry.at)
		if waited > v2SkipMemory {
			logging.Warn(fmt.Sprintf("⌛ %s: no v1 or hybrid release found in %d days to replace «%s» (BitTorrent v2-only, not downloadable by gx-torrent); no longer tracked",
				entry.label, int(v2SkipMemory.Hours()/24), entry.title))
			delete(v2Skipped.byTarget, target)
			continue
		}
		if !entry.noticeAt.IsZero() && now.Sub(entry.noticeAt) < v2NoticeEvery {
			continue
		}
		if entry.noticeAt.IsZero() {
			logging.Warn(fmt.Sprintf("⏳ %s: «%s» is BitTorrent v2-only and no v1 or hybrid release is available yet; the search continues",
				entry.label, entry.title))
		} else {
			logging.Warn(fmt.Sprintf("⏳ %s: still no v1 or hybrid release to replace «%s» (BitTorrent v2-only), waiting for %s",
				entry.label, entry.title, logging.HumanDuration(int64(waited.Seconds()))))
		}
		entry.noticeAt = now
		v2Skipped.byTarget[target] = entry
	}
}

// reportV2Replacement logs, once, that a release replaced a v2-only one.
func reportV2Replacement(release *models.Release) {
	target := releaseTarget(release)
	v2Skipped.Lock()
	entry, ok := v2Skipped.byTarget[target]
	if ok {
		delete(v2Skipped.byTarget, target)
	}
	v2Skipped.Unlock()
	if !ok || entry.title == release.Title {
		return
	}
	logging.Info(fmt.Sprintf("🔁 %s: «%s» was a BitTorrent v2-only torrent that gx-torrent cannot download; «%s» (v1) is downloaded instead",
		logTarget(release), entry.title, release.Title))
}
