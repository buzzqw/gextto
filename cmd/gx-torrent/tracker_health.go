package main

// tracker_health.go drops trackers that never work. A public tracker that
// returns a permanent error — e.g. "ip is blocked" — is announced to forever
// otherwise, wasting an announce round for every torrent. Only a tracker that
// has never answered successfully is dropped, and only after failing
// continuously for a while; a tracker that worked once is kept (a temporary
// outage must not become a permanent loss). The dropped URL is removed from the
// running torrents and remembered so newly added ones do not get it back for a
// cooldown.

import (
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

const (
	// trackerFailWindow is how long a tracker may keep failing before it is
	// dropped. Long enough to ride out a temporary outage of a good tracker.
	trackerFailWindow = time.Hour
	// trackerDisableFor is the cooldown before the tracker is allowed again.
	trackerDisableFor = 6 * time.Hour
)

// trackerHealth keeps the per-tracker failure clock and the disabled set.
type trackerHealth struct {
	mu       sync.Mutex
	worked   map[string]bool      // url -> it answered successfully at least once
	failing  map[string]time.Time // url -> since when it has been failing
	disabled map[string]time.Time // url -> disabled until
}

func newTrackerHealth() *trackerHealth {
	return &trackerHealth{
		worked:   map[string]bool{},
		failing:  map[string]time.Time{},
		disabled: map[string]time.Time{},
	}
}

// loadDisabled restores the persisted disabled set.
func (h *trackerHealth) loadDisabled(values map[string]int64) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for url, until := range values {
		if url == "" {
			continue
		}
		h.disabled[url] = time.Unix(until, 0)
	}
}

// disabledSnapshot returns the disabled set for persistence.
func (h *trackerHealth) disabledSnapshot() map[string]int64 {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.disabled) == 0 {
		return nil
	}
	out := make(map[string]int64, len(h.disabled))
	for url, until := range h.disabled {
		out[url] = until.Unix()
	}
	return out
}

// updateTrackerHealth advances the failure clocks and returns the currently
// disabled URLs. It must run on the queue-loop goroutine, outside d.mu.
func (d *Daemon) updateTrackerHealth(collected map[string]collectedTorrent, now time.Time) map[string]bool {
	if d.trackers == nil {
		return nil
	}
	newly := []string{}
	d.trackers.mu.Lock()
	for _, c := range collected {
		for _, tracker := range c.trackers {
			url := tracker.URL
			if url == "" {
				continue
			}
			if tracker.Status == torrent.Working {
				d.trackers.worked[url] = true
				delete(d.trackers.failing, url)
				delete(d.trackers.disabled, url)
				continue
			}
			if tracker.Status != torrent.NotWorking && tracker.Error == nil {
				continue
			}
			// A tracker that has answered at least once is only having a bad
			// spell: never drop it automatically, that would turn a temporary
			// outage into a permanent loss.
			if d.trackers.worked[url] {
				delete(d.trackers.failing, url)
				continue
			}
			since, known := d.trackers.failing[url]
			if !known {
				d.trackers.failing[url] = now
				continue
			}
			if now.Sub(since) < trackerFailWindow {
				continue
			}
			if until, disabled := d.trackers.disabled[url]; disabled && until.After(now) {
				continue
			}
			d.trackers.disabled[url] = now.Add(trackerDisableFor)
			delete(d.trackers.failing, url)
			newly = append(newly, url)
		}
	}
	for url, until := range d.trackers.disabled {
		if until.Before(now) {
			delete(d.trackers.disabled, url)
		}
	}
	disabled := make(map[string]bool, len(d.trackers.disabled))
	for url := range d.trackers.disabled {
		disabled[url] = true
	}
	d.trackers.mu.Unlock()

	for _, url := range newly {
		logf("tracker not working for %s: removed from the session for %s · %s",
			trackerFailWindow, trackerDisableFor, url)
	}
	if len(disabled) > 0 {
		d.stripDisabledTrackers(collected, disabled)
	}
	if len(newly) > 0 {
		d.mu.Lock()
		d.dirty = true
		d.mu.Unlock()
	}
	return disabled
}

// stripDisabledTrackers removes the disabled trackers that are still listed by
// the running torrents. New torrents are cleaned on the next tick.
func (d *Daemon) stripDisabledTrackers(collected map[string]collectedTorrent, disabled map[string]bool) {
	for hash, c := range collected {
		remaining := make([]string, 0, len(c.trackers))
		changed := false
		for _, tracker := range c.trackers {
			if disabled[tracker.URL] {
				changed = true
				continue
			}
			remaining = append(remaining, tracker.URL)
		}
		if changed {
			if err := d.setTrackers(strings.ToLower(hash), remaining); err != nil {
				logf("cannot drop a failing tracker: %v", err)
			}
		}
	}
}
