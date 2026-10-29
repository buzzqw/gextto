package main

// lifetime.go keeps the all-time transfer totals shown on the Statistics page.
// The engine's counters are per session — they start at zero when the daemon
// starts — while qBittorrent keeps the totals of every session; the deltas are
// folded into a small JSON file under the data dir, written on a coarse cadence
// and on shutdown. A missing or unreadable file starts fresh: the statistics
// must never keep the daemon from starting.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// lifetimeFlushEvery is how often the totals reach the disk. The in-memory
// value is always current, so a crash can lose at most this much transfer.
const lifetimeFlushEvery = 60 * time.Second

// lifetimeTotals is the persisted all-time transfer (bytes).
type lifetimeTotals struct {
	Downloaded int64     `json:"downloaded_bytes"`
	Uploaded   int64     `json:"uploaded_bytes"`
	Since      time.Time `json:"since"`
	// seenDown/seenUp remember the session counters already folded in, so each
	// delta is added exactly once. They are not persisted: at load the engine's
	// counters are the reference again.
	seenDown int64 `json:"-"`
	seenUp   int64 `json:"-"`
}

// lifetimeStats is the read model of the totals for the API and the page.
type lifetimeStats struct {
	Downloaded int64     `json:"downloaded_bytes"`
	Uploaded   int64     `json:"uploaded_bytes"`
	Ratio      float64   `json:"ratio"`
	Since      time.Time `json:"since"`
	Days       int       `json:"days"`
}

// lifetimePath is the totals file ("" when the daemon has no data dir, as in a
// bare test Daemon: nothing is persisted then).
func (d *Daemon) lifetimePath() string {
	if d.opts.DataDir == "" {
		return ""
	}
	return filepath.Join(d.opts.DataDir, "lifetime.json")
}

// loadLifetime reads the persisted totals.
func (d *Daemon) loadLifetime() {
	path := d.lifetimePath()
	if path == "" {
		d.lifetime = lifetimeTotals{Since: time.Now()}
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logfError("cannot read lifetime totals: %v", err)
		}
		d.lifetime = lifetimeTotals{Since: time.Now()}
		return
	}
	var loaded lifetimeTotals
	if err := json.Unmarshal(data, &loaded); err != nil {
		logfError("corrupted lifetime totals %s: %v", path, err)
		d.lifetime = lifetimeTotals{Since: time.Now()}
		return
	}
	if loaded.Since.IsZero() {
		loaded.Since = time.Now()
	}
	d.lifetime = loaded
}

// accumulateLifetimeLocked folds the engine's session counters into the all-time
// totals. A counter lower than the one already seen means the engine restarted
// its session: it is adopted without adding anything (the delta would otherwise
// be negative). It reports whether the totals should be flushed.
func (d *Daemon) accumulateLifetimeLocked(now time.Time, down, up int64) bool {
	l := &d.lifetime
	if down < l.seenDown {
		l.seenDown = down
	}
	if up < l.seenUp {
		l.seenUp = up
	}
	if delta := down - l.seenDown; delta > 0 {
		l.Downloaded += delta
		l.seenDown = down
	}
	if delta := up - l.seenUp; delta > 0 {
		l.Uploaded += delta
		l.seenUp = up
	}
	if l.Since.IsZero() {
		l.Since = now
	}
	if !d.lifetimeSaveAt.IsZero() && now.Sub(d.lifetimeSaveAt) < lifetimeFlushEvery {
		return false
	}
	d.lifetimeSaveAt = now
	return true
}

// saveLifetime writes the totals atomically. Called outside d.mu (it does file
// I/O): the snapshot is taken under the lock.
func (d *Daemon) saveLifetime() {
	path := d.lifetimePath()
	if path == "" {
		return
	}
	d.mu.Lock()
	data, err := json.MarshalIndent(d.lifetime, "", "  ")
	d.mu.Unlock()
	if err != nil {
		logfError("cannot encode lifetime totals: %v", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		logfError("cannot write lifetime totals: %v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		// Never leave the temporary behind when the replace fails.
		_ = os.Remove(tmp)
		logfError("cannot replace lifetime totals: %v", err)
	}
}

// lifetimeLocked is the read model (call with d.mu held).
func (d *Daemon) lifetimeLocked() lifetimeStats {
	out := lifetimeStats{
		Downloaded: d.lifetime.Downloaded,
		Uploaded:   d.lifetime.Uploaded,
		Since:      d.lifetime.Since,
	}
	if out.Downloaded > 0 {
		out.Ratio = float64(out.Uploaded) / float64(out.Downloaded)
	}
	if !out.Since.IsZero() {
		out.Days = int(time.Since(out.Since).Hours() / 24)
	}
	return out
}
