package main

// connauto.go sizes the per-torrent connection cap from the machine, at run
// time, instead of using a fixed number.
//
// Why: the CPU of the engine grows with the number of established peers (about
// 0.1 % of a core each, see docs/measurements.md) and very little with the
// bytes when the line is the limit. A fixed cap is wrong at both ends: too
// high on a small machine, too low on a big one, and it keeps dead peers
// connected. Two numbers come from the machine:
//
//   - the allowance of one torrent: ~10 peers per core plus a flat 15,
//     bounded to 15..200, at most 30 on a host with less than 4 GiB of RAM and
//     halved when less than 768 MiB are available (a few hundred KiB per
//     connection);
//   - the budget of the whole session: 75 peers per core, with the same memory
//     rules. It is what keeps the CPU bounded with many active torrents: the
//     allowance alone would grow with the number of torrents.
//
// Each running torrent asks for its allowance (half while it seeds, since the
// upload line is the narrow one), half of that when it has held at least ten
// peers for two minutes without moving 256 KiB (those connections buy nothing,
// and the freed slots let the engine try the queued addresses), and the floor
// when it is a seed nobody downloads from. When the asks exceed the budget the
// budget is shared by weight — a torrent that is downloading counts twice — and
// a torrent never gets less than the floor or more than it asked: the slots a
// small ask leaves are shared again among the others. A single torrent always
// gets its full allowance, so downloading one thing is never slowed.
//
// The configured max_connections_per_torrent (when above zero) stays the
// ceiling, a torrent's own limit (Limits tab) always wins, and the peers of
// such a torrent are taken out of the budget first. The cap is a live
// per-torrent value (SetMaxConnections): no session restart, no re-announce,
// and the engine closes the excess peers itself.

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
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
	// connAutoProgressBytes is what a torrent must move (downloaded plus
	// uploaded) to count as alive: a trickle of a few bytes per second does
	// not justify a full set of connections.
	connAutoProgressBytes = 256 << 10
	// connAutoMinPeers is the floor: below this the swarm cannot be sampled.
	connAutoMinPeers = 15
	// connAutoMaxPeers is the ceiling of the automatic allowance.
	connAutoMaxPeers = 200
	// connAutoBasePerCore and connAutoBaseFlat shape the machine allowance.
	connAutoBasePerCore = 10
	connAutoBaseFlat    = 15
	// connAutoBudgetPerCore shapes the session budget.
	connAutoBudgetPerCore = 75
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

// autoConnectionsBudget is the machine budget for the whole session: the
// established peers of every running torrent together.
func autoConnectionsBudget(cores int, memTotal, memAvailable int64) int {
	if cores < 1 {
		cores = 1
	}
	budget := connAutoBudgetPerCore * cores
	if memTotal > 0 && memTotal < 4<<30 {
		budget = min(budget, 150)
	}
	if memAvailable > 0 && memAvailable < 768<<20 {
		budget /= 2
	}
	return max(budget, 4*connAutoMinPeers)
}

// connAutoLimits is what the machine allows in this pass.
type connAutoLimits struct {
	perTorrent int
	budget     int
}

// connAutoProgress remembers what a torrent delivered since it was last seen
// alive, so a silent one can be recognised.
type connAutoProgress struct {
	moved        int64
	lastProgress time.Time
}

// connAutoCores is the number of cores the daemon may use. GOMAXPROCS follows
// the CPU quota of the cgroup (container, systemd CPUQuota), NumCPU does not.
func connAutoCores() int {
	return min(runtime.GOMAXPROCS(0), runtime.NumCPU())
}

// autoConnectionsBase returns the limits for this pass, or a zero value when
// the automatic policy is off or the pass is not due yet. A change in the
// number of torrents makes it due at once: a torrent just added has no cap
// until the first pass, and would connect without limit for up to
// connAutoEvery. The memory reads happen outside d.mu.
func (d *Daemon) autoConnectionsBase(now time.Time, torrents int) connAutoLimits {
	d.mu.Lock()
	auto := queue.BoolOn(d.state.Config.ConnAuto)
	ceiling := d.state.Config.MaxConnectionsPerTorrent
	due := now.Sub(d.connAutoAt) >= connAutoEvery || torrents != d.connAutoTorrents
	if due {
		d.connAutoAt = now
		d.connAutoTorrents = torrents
	}
	d.mu.Unlock()
	if !auto || !due {
		return connAutoLimits{}
	}
	cores, total, available := connAutoCores(), memoryTotal(), memoryAvailable()
	limits := connAutoLimits{
		perTorrent: autoConnectionsPerTorrent(cores, total, available),
		budget:     autoConnectionsBudget(cores, total, available),
	}
	if ceiling > 0 && limits.perTorrent > ceiling {
		limits.perTorrent = ceiling
	}
	return limits
}

// connAutoAsk is what one torrent asks for in a pass.
type connAutoAsk struct {
	t      *torrent.Torrent
	ask    int
	weight int
	reason string
	target int
}

// shareConnections fills the targets: every ask is granted when the budget
// covers them; otherwise the budget is shared by weight, an ask smaller than
// its share is granted whole and the rest is shared again among the others
// (water filling). Nobody goes below the floor or above its ask; the caller
// passes a budget that covers the floor of every torrent.
func shareConnections(asks []connAutoAsk, budget int) {
	total := 0
	for _, a := range asks {
		total += a.ask
	}
	if total <= budget {
		for i := range asks {
			asks[i].target = asks[i].ask
		}
		return
	}
	open := make([]int, len(asks))
	for i := range open {
		open[i] = i
	}
	left := budget
	for len(open) > 0 {
		weights := 0
		for _, i := range open {
			weights += asks[i].weight
		}
		var waiting []int
		granted := 0
		for _, i := range open {
			if asks[i].ask <= left*asks[i].weight/weights {
				asks[i].target = asks[i].ask
				granted += asks[i].ask
				continue
			}
			waiting = append(waiting, i)
		}
		if granted == 0 {
			// Every remaining ask is above its share. A share under the
			// floor is raised to it first, and the others share what is
			// left; then each gets its share.
			for _, i := range waiting {
				if left*asks[i].weight/weights < connAutoMinPeers {
					asks[i].target = connAutoMinPeers
					granted += connAutoMinPeers
				}
			}
			if granted == 0 {
				for _, i := range waiting {
					asks[i].target = left * asks[i].weight / weights
				}
				return
			}
			waiting = slices.DeleteFunc(waiting, func(i int) bool { return asks[i].target > 0 })
		}
		left -= granted
		open = waiting
	}
}

// applyAutoConnectionsLocked writes the automatic cap onto every torrent that
// has no limit of its own. A change smaller than a tenth (and two peers) is
// skipped, so the cap cannot flap between two adjacent samples. Every change
// and every new machine allowance is logged: an operator must be able to see
// why the peer count moved.
func (d *Daemon) applyAutoConnectionsLocked(now time.Time, torrents []*torrent.Torrent, stats map[string]torrent.Stats, limits connAutoLimits) {
	if d.connAutoState == nil {
		d.connAutoState = make(map[string]*connAutoProgress, len(torrents))
	}
	base := limits.perTorrent
	if limits != d.connAutoLimits {
		d.connAutoLimits = limits
		logfDebug("automatic peer cap: %d connections per torrent, %d in all (%d cores, %d MiB available)",
			base, limits.budget, connAutoCores(), memoryAvailable()>>20)
	}
	budget := limits.budget
	asks := make([]connAutoAsk, 0, len(torrents))
	for _, t := range torrents {
		id := t.ID()
		meta := d.state.Torrents[id]
		sample, ok := stats[id]
		if meta == nil || !ok {
			continue
		}
		if meta.MaxConnections != nil {
			// The operator chose a number for this torrent: leave it alone,
			// but its peers use the machine as much as the others.
			budget -= sample.Peers.Total
			continue
		}
		if sample.Status == torrent.Stopping {
			continue
		}
		if sample.Status == torrent.Stopped {
			// A paused or queued torrent holds no peers: it takes nothing
			// from the budget, but it starts with its allowance instead of
			// no cap at all when the queue resumes it.
			if t.MaxConnections() == 0 {
				t.SetMaxConnections(base)
			}
			continue
		}
		moved := sample.Bytes.Completed + sample.Bytes.Uploaded
		progress := d.connAutoState[id]
		if progress == nil {
			progress = &connAutoProgress{moved: moved, lastProgress: now}
			d.connAutoState[id] = progress
		}
		if moved-progress.moved >= connAutoProgressBytes || moved < progress.moved {
			progress.moved = moved
			progress.lastProgress = now
		}
		stalled := sample.Peers.Total >= connAutoStallPeers && now.Sub(progress.lastProgress) >= connAutoStallAfter

		ask := connAutoAsk{t: t, ask: base, weight: 2, reason: "machine allowance"}
		seeding := sample.Status == torrent.Seeding
		switch {
		case seeding && stalled:
			ask.ask, ask.weight = connAutoMinPeers, 1
			ask.reason = fmt.Sprintf("seeding, nobody downloading with %d peers", sample.Peers.Total)
		case seeding:
			ask.ask, ask.weight = max(connAutoMinPeers, base/2), 1
			ask.reason = "seeding"
		case stalled:
			ask.ask, ask.weight = max(connAutoMinPeers, base/2), 1
			ask.reason = fmt.Sprintf("no progress with %d peers", sample.Peers.Total)
		}
		ask.ask = min(ask.ask, base)
		asks = append(asks, ask)
	}
	shareConnections(asks, max(budget, connAutoMinPeers*len(asks)))
	var changes []connAutoChange
	for _, a := range asks {
		reason := a.reason
		if a.target < a.ask {
			reason = fmt.Sprintf("%s, shared with %d running torrents", reason, len(asks))
		}
		current := a.t.MaxConnections()
		if current == a.target {
			continue
		}
		step := a.target - current
		if step < 0 {
			step = -step
		}
		if current > 0 && step < max(2, a.target/10) {
			continue
		}
		a.t.SetMaxConnections(a.target)
		changes = append(changes, connAutoChange{name: a.t.Name(), from: current, to: a.target, reason: reason})
	}
	logAutoConnectionChanges(changes)
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

// connAutoChange is one cap written by a pass.
type connAutoChange struct {
	name     string
	from, to int
	reason   string
}

// logAutoConnectionChanges logs the caps written by one pass. Changes to the
// same value for the same reason share one line — the budget moves every
// torrent at once when one is added or removed, and a line per torrent would
// fill the log — while a lone change keeps its own line with the old value.
func logAutoConnectionChanges(changes []connAutoChange) {
	type groupKey struct {
		to     int
		reason string
	}
	groups := map[groupKey][]connAutoChange{}
	var order []groupKey
	for _, c := range changes {
		key := groupKey{c.to, c.reason}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], c)
	}
	for _, key := range order {
		group := groups[key]
		if len(group) == 1 {
			c := group[0]
			logfDebug("peer cap for «%s»: %d → %d (%s)", c.name, c.from, c.to, c.reason)
			continue
		}
		names := make([]string, 0, 3)
		for _, c := range group[:min(3, len(group))] {
			names = append(names, "«"+c.name+"»")
		}
		more := ""
		if len(group) > 3 {
			more = fmt.Sprintf(" +%d", len(group)-3)
		}
		logfDebug("peer cap %d for %d torrents (%s): %s%s", key.to, len(group), key.reason, strings.Join(names, ", "), more)
	}
}
