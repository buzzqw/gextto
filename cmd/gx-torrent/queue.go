package main

// queue.go is gx-torrent's own queue manager. rain has no queue, so the
// daemon decides every few seconds which torrents run, with the same policy
// Gextto configures on libtorrent:
//
//   - active_downloads / active_seeds slots, ordered by queue position;
//   - dont_count_slow: a running torrent that moves no payload for
//     slow_after_secs keeps running but frees its slot, so a dead swarm never
//     blocks healthy downloads;
//   - active_limit is a hard cap on running auto-managed torrents, slow ones
//     included. When it is hit, slow torrents are rotated out (moved to the
//     back of the queue) and get another turn after slow_rotate_secs: every
//     stuck download is retried periodically while healthy ones proceed;
//   - torrents the user paused, torrents Gextto parked as stalled, torrents
//     in error and torrents being moved are never started by the queue;
//   - pinned torrents and stall probes always run and take no slot;
//   - the optional dynamic queue widens or narrows active_downloads from the
//     measured throughput against the global download limit.

import (
	"sort"
	"time"
)

// QueueConfig is the queue policy, pushed by Gextto and persisted.
type QueueConfig struct {
	ActiveDownloads int   `json:"active_downloads"`
	ActiveSeeds     int   `json:"active_seeds"`
	ActiveLimit     int   `json:"active_limit"`
	DontCountSlow   bool  `json:"dont_count_slow"`
	SlowRate        int64 `json:"slow_rate"`
	SlowAfterSecs   int64 `json:"slow_after_secs"`
	SlowRotateSecs  int64 `json:"slow_rotate_secs"`
	DynamicQueue    bool  `json:"dynamic_queue"`
	DynamicMin      int   `json:"dynamic_min"`
	DynamicMax      int   `json:"dynamic_max"`
	// Global limits in KiB/s (0 = unlimited) and peer limits, applied to the
	// rain session.
	SpeedLimitDownload int64 `json:"speed_limit_download"`
	SpeedLimitUpload   int64 `json:"speed_limit_upload"`
	MaxPeerDial        int   `json:"max_peer_dial"`
	MaxPeerAccept      int   `json:"max_peer_accept"`
	// Disk cache: CacheMB <= 0 sizes it from the RAM (automatic), otherwise
	// it is the read cache and the write buffer in MiB. CacheTTLSecs keeps
	// read blocks; Preallocate reserves new files in full.
	CacheMB      int64 `json:"cache_mb"`
	CacheTTLSecs int64 `json:"cache_ttl_secs"`
	Preallocate  bool  `json:"preallocate"`
	// Sequential downloads new torrents in piece order (streaming) instead of
	// rarest-first. rain applies it when a torrent is added, so it affects new
	// additions only.
	Sequential bool `json:"sequential"`
}

func defaultQueueConfig() QueueConfig {
	return QueueConfig{
		ActiveDownloads: 3,
		ActiveSeeds:     3,
		ActiveLimit:     5,
		DontCountSlow:   true,
		SlowRate:        2048,
		SlowAfterSecs:   120,
		SlowRotateSecs:  1800,
		DynamicMin:      1,
		DynamicMax:      10,
		CacheMB:         -1,
		CacheTTLSecs:    300,
	}
}

// normalized clamps the configuration into a usable range.
func (c QueueConfig) normalized() QueueConfig {
	if c.ActiveDownloads < 1 {
		c.ActiveDownloads = 1
	}
	if c.ActiveSeeds < 1 {
		c.ActiveSeeds = 1
	}
	if c.ActiveLimit < 1 {
		c.ActiveLimit = 1
	}
	if c.SlowRate < 1 {
		c.SlowRate = 1
	}
	if c.SlowAfterSecs < 30 {
		c.SlowAfterSecs = 30
	}
	if c.SlowRotateSecs < 60 {
		c.SlowRotateSecs = 60
	}
	if c.DynamicMin < 1 {
		c.DynamicMin = 1
	}
	if c.DynamicMax < c.DynamicMin {
		c.DynamicMax = c.DynamicMin
	}
	if c.SpeedLimitDownload < 0 {
		c.SpeedLimitDownload = 0
	}
	if c.SpeedLimitUpload < 0 {
		c.SpeedLimitUpload = 0
	}
	if c.CacheMB <= 0 {
		c.CacheMB = -1
	}
	if c.CacheTTLSecs < 10 {
		c.CacheTTLSecs = 300
	}
	return c
}

// queueItem is the planner's view of one torrent.
type queueItem struct {
	ID       string
	Complete bool
	Running  bool
	// Managed torrents are started and stopped by the queue.
	Managed bool
	// Forced torrents (pinned, stall probes) always run.
	Forced bool
	// Slow: running, past its grace time, moving less than SlowRate.
	Slow bool
	// RotatedAt: when the queue last set it aside for being slow.
	RotatedAt time.Time
	Pos       int64
}

// queuePlan is the outcome of one planning round.
type queuePlan struct {
	Start  []string
	Stop   []string
	Rotate []string // stopped for being slow: move to the back of the queue
}

// effectiveLimits are the slot counts used for one round.
type effectiveLimits struct {
	Downloads int
	Seeds     int
	Limit     int
}

// planQueue decides which torrents run. It is a pure function: the daemon
// applies the plan.
func planQueue(items []queueItem, limits effectiveLimits, cfg QueueConfig, now time.Time) queuePlan {
	var plan queuePlan
	want := map[string]bool{}
	var downloads, seeds []queueItem
	for _, item := range items {
		switch {
		case item.Forced:
			want[item.ID] = true
		case item.Managed && item.Complete:
			seeds = append(seeds, item)
		case item.Managed:
			downloads = append(downloads, item)
		default:
			want[item.ID] = false
		}
	}
	byPos := func(list []queueItem) {
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].Pos != list[j].Pos {
				return list[i].Pos < list[j].Pos
			}
			return list[i].ID < list[j].ID
		})
	}
	byPos(downloads)
	byPos(seeds)

	rotateCooldown := time.Duration(cfg.SlowRotateSecs) * time.Second
	coolingDown := func(item queueItem) bool {
		return !item.Running && !item.RotatedAt.IsZero() && now.Sub(item.RotatedAt) < rotateCooldown
	}

	var wanted []queueItem
	fill := func(list []queueItem, slots int) {
		counted := 0
		for _, item := range list {
			if counted >= slots {
				want[item.ID] = false
				continue
			}
			want[item.ID] = true
			wanted = append(wanted, item)
			if !(cfg.DontCountSlow && item.Running && item.Slow) {
				counted++
			}
		}
	}
	fill(downloads, limits.Downloads)
	fill(seeds, limits.Seeds)

	// Hard cap. Eviction order: do not start a torrent that was just rotated
	// out; then stop slow seeds, slow downloads, new starts, and finally
	// healthy running torrents, always from the back of the queue.
	if excess := len(wanted) - limits.Limit; excess > 0 {
		rank := func(item queueItem) int {
			switch {
			case coolingDown(item):
				return 0
			case item.Running && item.Slow && item.Complete:
				return 1
			case item.Running && item.Slow:
				return 2
			case !item.Running && item.Complete:
				return 3
			case !item.Running:
				return 4
			case item.Complete:
				return 5
			default:
				return 6
			}
		}
		candidates := append([]queueItem(nil), wanted...)
		sort.SliceStable(candidates, func(i, j int) bool {
			ri, rj := rank(candidates[i]), rank(candidates[j])
			if ri != rj {
				return ri < rj
			}
			return candidates[i].Pos > candidates[j].Pos
		})
		for _, item := range candidates[:excess] {
			want[item.ID] = false
			if item.Running && item.Slow {
				plan.Rotate = append(plan.Rotate, item.ID)
			}
		}
	}

	for _, item := range items {
		run, decided := want[item.ID]
		if !decided {
			continue
		}
		switch {
		case run && !item.Running:
			plan.Start = append(plan.Start, item.ID)
		case !run && item.Running:
			plan.Stop = append(plan.Stop, item.ID)
		}
	}
	sort.Strings(plan.Start)
	sort.Strings(plan.Stop)
	sort.Strings(plan.Rotate)
	return plan
}

// dynamicQueue adapts active_downloads to the line, like the libtorrent
// bridge: narrow when the global limit is saturated for ten minutes, widen
// quickly when bandwidth is unused and torrents are waiting. Seeds drop to one
// while downloads are queued, so seeding never starves a download.
type dynamicQueue struct {
	downloads     int
	seeds         int
	samples       []rateSample
	saturation    int
	lastChange    time.Time
	lastIncrease  time.Time
	seedState     bool
	seedStateFrom time.Time
}

type rateSample struct {
	at   time.Time
	rate int64
}

// limits returns the slot counts for this round. aggregateRate is the total
// download rate of running downloads; queued counts the downloads waiting
// for a slot.
func (q *dynamicQueue) limits(cfg QueueConfig, aggregateRate int64, queued int, now time.Time) effectiveLimits {
	if !cfg.DynamicQueue {
		*q = dynamicQueue{}
		return effectiveLimits{Downloads: cfg.ActiveDownloads, Seeds: cfg.ActiveSeeds, Limit: cfg.ActiveLimit}
	}
	if q.downloads == 0 {
		q.downloads = clampInt(cfg.ActiveDownloads, cfg.DynamicMin, cfg.DynamicMax)
		q.seeds = cfg.ActiveSeeds
		q.seedStateFrom = now
	}
	q.downloads = clampInt(q.downloads, cfg.DynamicMin, cfg.DynamicMax)

	q.samples = append(q.samples, rateSample{at: now, rate: aggregateRate})
	for len(q.samples) > 0 && now.Sub(q.samples[0].at) > 10*time.Minute {
		q.samples = q.samples[1:]
	}
	decreaseReady := now.Sub(q.lastChange) >= 10*time.Minute
	increaseReady := now.Sub(q.lastIncrease) >= 90*time.Second
	globalLimit := cfg.SpeedLimitDownload * 1024
	switch {
	case globalLimit > 0 && len(q.samples) >= 3:
		var sum int64
		for _, sample := range q.samples {
			sum += sample.rate
		}
		ratio := float64(sum) / float64(len(q.samples)) / float64(globalLimit)
		switch {
		case ratio >= 0.9 && q.downloads > cfg.DynamicMin && decreaseReady:
			q.saturation++
			if q.saturation >= 3 {
				q.downloads--
				q.saturation = 0
				q.lastChange = now
			}
		case queued > 0 && ratio <= 0.7 && q.downloads < cfg.DynamicMax && increaseReady:
			q.saturation = 0
			step := 1
			if ratio <= 0.4 {
				step = 2
			}
			q.downloads = clampInt(q.downloads+step, cfg.DynamicMin, cfg.DynamicMax)
			q.lastIncrease = now
		default:
			q.saturation = 0
		}
	case globalLimit <= 0 && queued > 0 && q.downloads < cfg.DynamicMax && increaseReady:
		q.downloads++
		q.lastIncrease = now
	default:
		q.saturation = 0
	}

	waiting := queued > 0
	if waiting != q.seedState {
		q.seedState = waiting
		q.seedStateFrom = now
	} else if now.Sub(q.seedStateFrom) >= 5*time.Minute {
		if waiting {
			q.seeds = 1
		} else {
			q.seeds = cfg.ActiveSeeds
		}
	}
	if q.seeds < 1 {
		q.seeds = 1
	}
	limit := cfg.ActiveLimit
	if sum := q.downloads + q.seeds + 2; sum > limit {
		limit = sum
	}
	return effectiveLimits{Downloads: q.downloads, Seeds: q.seeds, Limit: limit}
}

func clampInt(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
