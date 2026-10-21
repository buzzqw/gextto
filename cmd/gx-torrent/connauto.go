package main

// connauto.go sizes the per-torrent connection cap from the machine, at run
// time, instead of using a fixed number.
//
// Why: the CPU of the engine grows with the number of established peers (about
// 0.1 % of a core each, see docs/measurements.md) and very little with the
// bytes when the line is the limit. A fixed cap is wrong at both ends: too
// high on a small machine, too low on a big one, and it keeps dead peers
// connected. The cap here is the number of peers one torrent may hold:
//
//   - the machine allowance is ~10 peers per core plus a flat 15, bounded to
//     15..200, halved on a host with less than 4 GiB of RAM and again when less
//     than 768 MiB are available (a few hundred KiB per connection);
//   - the configured max_connections_per_torrent (when above zero) stays the
//     ceiling, and a torrent's own limit (Limits tab) always wins;
//   - a torrent with at least 10 peers and no progress for two minutes is
//     trimmed to half: those connections are not buying anything, and the
//     freed slots let the engine try the queued addresses.
//
// The cap is a live per-torrent value (SetMaxConnections): no session restart,
// no re-announce, and the engine closes the excess peers itself.

import (
	"fmt"
	"runtime"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
	"github.com/buzzqw/gextto/internal/queue"
)

const (
	// connAutoEvery is how often the cap is recomputed. Slower than the queue
	// tick and the stats sampling: the numbers it depends on move slowly.
	connAutoEvery = 30 * time.Second
	// connAutoStallAfter is how long a torrent with plenty of peers and no
	// progress keeps its full allowance before it is trimmed.
	connAutoStallAfter = 2 * time.Minute
	// connAutoStallPeers is the peer count above which a silent torrent is
	// considered worth trimming.
	connAutoStallPeers = 10
	// connAutoMinPeers is the floor: below this the swarm cannot be sampled.
	connAutoMinPeers = 15
	// connAutoMaxPeers is the ceiling of the automatic allowance.
	connAutoMaxPeers = 200
	// connAutoBasePerCore and connAutoBaseFlat shape the machine allowance.
	connAutoBasePerCore = 10
	connAutoBaseFlat    = 15
)

// autoConnectionsPerTorrent is the machine allowance for one torrent.
func autoConnectionsPerTorrent(cores int, memTotal, memAvailable int64) int {
	if cores < 1 {
		cores = 1
	}
	limit := connAutoBaseFlat + connAutoBasePerCore*cores
	if memTotal > 0 && memTotal < 4<<30 {
		limit = min(limit, 30)
	}
	if memAvailable > 0 && memAvailable < 768<<20 {
		limit = max(connAutoMinPeers, limit/2)
	}
	return min(max(limit, connAutoMinPeers), connAutoMaxPeers)
}

// connAutoProgress remembers what a torrent delivered since the last pass, so a
// silent one can be recognised.
type connAutoProgress struct {
	completed    int64
	lastProgress time.Time
}

// autoConnectionsBase returns the allowance for this pass, or 0 when the
// automatic policy is off or the pass is not due yet. The memory reads happen
// outside d.mu.
func (d *Daemon) autoConnectionsBase(now time.Time) int {
	d.mu.Lock()
	auto := queue.BoolOn(d.state.Config.ConnAuto)
	ceiling := d.state.Config.MaxConnectionsPerTorrent
	due := now.Sub(d.connAutoAt) >= connAutoEvery
	if due {
		d.connAutoAt = now
	}
	d.mu.Unlock()
	if !auto || !due {
		return 0
	}
	limit := autoConnectionsPerTorrent(runtime.NumCPU(), memoryTotal(), memoryAvailable())
	if ceiling > 0 && limit > ceiling {
		limit = ceiling
	}
	return limit
}

// applyAutoConnectionsLocked writes the automatic cap onto every torrent that
// has no limit of its own. A change smaller than a tenth (and two peers) is
// skipped, so the cap cannot flap between two adjacent samples. Every change
// and every new machine allowance is logged: an operator must be able to see
// why the peer count moved.
func (d *Daemon) applyAutoConnectionsLocked(now time.Time, torrents []*torrent.Torrent, stats map[string]torrent.Stats, base int) {
	if d.connAutoState == nil {
		d.connAutoState = make(map[string]*connAutoProgress, len(torrents))
	}
	if base != d.connAutoBase {
		d.connAutoBase = base
		logf("automatic peer cap: %d connections per torrent (%d cores, %d MiB available)",
			base, runtime.NumCPU(), memoryAvailable()>>20)
	}
	for _, t := range torrents {
		id := t.ID()
		meta := d.state.Torrents[id]
		if meta == nil || meta.MaxConnections != nil {
			// The operator chose a number for this torrent: leave it alone.
			continue
		}
		sample, ok := stats[id]
		if !ok {
			continue
		}
		progress := d.connAutoState[id]
		if progress == nil {
			progress = &connAutoProgress{completed: sample.Bytes.Completed, lastProgress: now}
			d.connAutoState[id] = progress
		}
		if sample.Bytes.Completed > progress.completed || sample.Speed.Download+sample.Speed.Upload > 0 {
			progress.lastProgress = now
		}
		progress.completed = sample.Bytes.Completed

		target := base
		reason := "machine allowance"
		if sample.Peers.Total >= connAutoStallPeers && now.Sub(progress.lastProgress) >= connAutoStallAfter {
			target = max(connAutoMinPeers, base/2)
			reason = fmt.Sprintf("no progress with %d peers", sample.Peers.Total)
		}
		current := t.MaxConnections()
		if current == target {
			continue
		}
		step := target - current
		if step < 0 {
			step = -step
		}
		if current > 0 && step < max(2, target/10) {
			continue
		}
		t.SetMaxConnections(target)
		logf("peer cap for «%s»: %d → %d (%s)", t.Name(), current, target, reason)
	}
	// Forget the torrents that left the session.
	if len(d.connAutoState) > 2*len(torrents)+8 {
		live := make(map[string]bool, len(torrents))
		for _, t := range torrents {
			live[t.ID()] = true
		}
		for id := range d.connAutoState {
			if !live[id] {
				delete(d.connAutoState, id)
			}
		}
	}
}
