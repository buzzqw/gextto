package gextto

// latency_guard.go keeps the line usable while the speed is on Auto, without
// taking it from Gextto when nobody else needs it. Auto (the base policy, an
// Auto schedule day or the temporary Auto) leaves the transfers uncapped so the
// line's peak can be learned; a saturated download then queues at the provider
// and adds tens of milliseconds for every device in the house (measured 19.8 →
// 62.5 ms). A queue Gextto builds for itself hurts no one, though: with nobody
// else on the line the whole line is Gextto's.
//
// So the guard finds out by trying. When the latency to a public host is high
// (or the idle reference is unknown or old) it backs Gextto off to 60 % for a
// few seconds and watches: if the latency falls, the queue was Gextto's own,
// the fallen latency becomes the idle reference and the line goes back to
// Gextto; if it stays high while Gextto left room, someone else is filling
// the line and the guard yields to them, lowering the caps step by step and
// raising them, then lifting them, once the latency is back at rest. When the
// router's WAN counters answer (line_usage.go) their verdict on "someone
// else" comes first, which also sees users of little bandwidth (a call, a
// game) that a trial cannot. The controller is internal/autospeed, shared with
// the standalone daemon.

import (
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/autospeed"
	"github.com/buzzqw/gextto/internal/logging"
)

const (
	// latencyGuardKey enables the guard (on by default); latencyGuardTargetKey
	// is the host[:port] to probe (empty: latencyGuardTargets).
	latencyGuardKey       = "torrent_auto_latency_guard"
	latencyGuardTargetKey = "torrent_auto_latency_target"

	// latencyGuardProbeEvery is the probe cadence; latencyGuardWindow how far
	// back the probe keeps samples.
	latencyGuardProbeEvery = 2 * time.Second
	latencyGuardWindow     = 10 * time.Minute
	// latencyGuardRecent is how many samples the current latency is the
	// median of, so one late answer does not move the caps.
	latencyGuardRecent = 3

	// latencyGuardTrial is how long a trial backs Gextto off;
	// latencyGuardTrialSettle the part of it left to the queue to drain
	// before its latency counts; latencyGuardTrialShare the share of the
	// current rate Gextto keeps during it.
	latencyGuardTrial       = 8 * time.Second
	latencyGuardTrialSettle = 3 * time.Second
	latencyGuardTrialShare  = 0.6
	// latencyGuardCooldown is the wait after a trial that found the line
	// Gextto's own (or after a release) before another one.
	latencyGuardCooldown = 10 * time.Minute
	// latencyGuardRestAge is how long a measured idle reference is kept. Only
	// a lower measure replaces it before then: a trial disturbed by someone
	// else's light traffic must not raise the reference and blind the guard.
	latencyGuardRestAge = 6 * time.Hour

	// latencyGuardRelease is the calm spell at the ceiling after which a cap
	// is lifted.
	latencyGuardRelease = time.Minute
	// latencyGuardMinLoadKiB: a direction below this is not loading the line.
	latencyGuardMinLoadKiB = 256
	// latencyGuardMinFloorKiB is the lowest cap the guard may set.
	latencyGuardMinFloorKiB = 64
)

// latencyGuardTargets are probed when no target is configured: public anycast
// resolvers, near on most lines, reached through the provider's queue (the
// gateway is not: it shows no queueing).
var latencyGuardTargets = []string{"9.9.9.9:443", "1.1.1.1:443"}

// guardMode is where the guard stands.
type guardMode int

const (
	guardFree  guardMode = iota // no caps: the line is Gextto's
	guardTrial                  // backed off for a few seconds, watching
	guardYield                  // capped for the others on the line
)

func (m guardMode) String() string {
	return [...]string{"free", "trial", "yield"}[m]
}

// guardDirection is the guard's cap for download or upload.
type guardDirection struct {
	clamp     autospeed.Clamp
	active    bool
	calmSince time.Time
}

// start caps the direction at value.
func (d *guardDirection) start(value int64) {
	*d = guardDirection{active: true}
	d.clamp.Value = value
}

// step moves an active cap: down while high (when the direction loads the
// line), up otherwise, lifted after a calm spell at the ceiling. It returns
// the cap (0 = none).
func (d *guardDirection) step(now time.Time, high bool, rateKiB, peakKiB int64) int64 {
	if !d.active {
		return 0
	}
	ceiling := max(peakKiB, rateKiB, d.clamp.Value)
	floor := max(ceiling/10, latencyGuardMinFloorKiB)
	loading := rateKiB >= d.clamp.Value/2
	capKiB := d.clamp.Step(now, high && loading, ceiling, floor)
	if high {
		d.calmSince = time.Time{}
		return capKiB
	}
	if d.calmSince.IsZero() {
		d.calmSince = now
	}
	if capKiB >= ceiling && now.Sub(d.calmSince) >= latencyGuardRelease {
		*d = guardDirection{}
		return 0
	}
	return capKiB
}

// latencyGuard is the Auto speed's latency controller. The zero value is not
// ready: use newLatencyGuard.
type latencyGuard struct {
	probe autospeed.Probe
	usage lineUsage

	mu             sync.Mutex
	mode           guardMode
	down, up       guardDirection
	downCap, upCap int64
	current        time.Duration
	rest           time.Duration // idle reference measured by a trial
	restAt         time.Time
	trialUntil     time.Time
	trialLoaded    time.Duration // the latency when the trial started
	trialSamples   []time.Duration
	cooldownUntil  time.Time
	haveSample     bool
	auto           bool
	othersOnLine   bool

	// The engine's cumulative wire counters at the previous tick, for an
	// instant rate (the reported rates are averaged over tens of seconds).
	wireDown, wireUp uint64
	wireAt           time.Time
}

// sessionCounters is the engine side of the instant rate: gx-torrent reports
// its cumulative wire bytes.
type sessionCounters interface {
	SessionStats() (map[string]int64, error)
}

// ownRates returns Gextto's transfer rates (bytes/s) now: from the engine's
// cumulative wire counters when it has them, else from the reported rates.
func (g *latencyGuard) ownRates(now time.Time, engine TorrentEngine) (down, up uint64) {
	if counters, ok := engine.(sessionCounters); ok {
		if stats, err := counters.SessionStats(); err == nil {
			wireDown := uint64(max(stats["peer_wire_downloaded"]+stats["dht_bytes_in"], 0))
			wireUp := uint64(max(stats["peer_wire_uploaded"]+stats["dht_bytes_out"], 0))
			g.mu.Lock()
			prevDown, prevUp, prevAt := g.wireDown, g.wireUp, g.wireAt
			g.wireDown, g.wireUp, g.wireAt = wireDown, wireUp, now
			g.mu.Unlock()
			if !prevAt.IsZero() && wireDown >= prevDown && wireUp >= prevUp {
				seconds := now.Sub(prevAt).Seconds()
				if seconds > 0 {
					return uint64(float64(wireDown-prevDown) / seconds), uint64(float64(wireUp-prevUp) / seconds)
				}
			}
		}
	}
	if engine == nil {
		return 0, 0
	}
	for _, view := range engine.List() {
		down += view.DownloadRate
		up += view.UploadRate
	}
	return down, up
}

func newLatencyGuard() *latencyGuard {
	return &latencyGuard{probe: autospeed.Probe{Window: latencyGuardWindow}}
}

// latencyGuardEnabled reads the option (on unless set to a false value).
func latencyGuardEnabled(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	value, ok := cfg.Settings[latencyGuardKey]
	return !ok || strings.TrimSpace(value) == "" || settingTruthy(value)
}

// speedSourceIsAuto reports the policy sources that leave the line uncapped.
func speedSourceIsAuto(source string) bool {
	return source == speedSourceAuto || source == speedSourceScheduleAuto || source == speedSourceTempAuto
}

// baselineLocked is the idle reference: the trial's measure while it is
// fresh, else the lowest latency of the probe window.
func (g *latencyGuard) baselineLocked(now time.Time, windowMin time.Duration) (time.Duration, bool) {
	if g.rest > 0 && now.Sub(g.restAt) < latencyGuardRestAge {
		if windowMin < g.rest {
			return windowMin, true
		}
		return g.rest, true
	}
	return windowMin, false
}

// isHigh applies the shared rule of internal/autospeed to a reference.
func isHigh(current, baseline time.Duration) bool {
	return current > time.Duration(float64(baseline)*autospeed.HighFactor) && current > baseline+autospeed.HighMargin
}

// lineGuess is what is known about the others on the line: from the router
// when its counters answer.
type lineGuess struct {
	known, others bool
}

// update runs one controller step with the current rates (bytes/s) and the
// router's verdict, and returns the caps in force (0 = none) and whether they
// changed.
func (g *latencyGuard) update(now time.Time, auto bool, line lineGuess, downBytes, upBytes uint64, peakDownKiB, peakUpKiB int64) (downCap, upCap int64, changed bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	prevDown, prevUp := g.downCap, g.upCap
	g.auto = auto
	if !auto {
		g.mode = guardFree
		g.down, g.up = guardDirection{}, guardDirection{}
		g.downCap, g.upCap = 0, 0
		g.haveSample, g.othersOnLine = false, false
		return 0, 0, prevDown != 0 || prevUp != 0
	}
	current, windowMin, _, ok := g.probe.State(now, latencyGuardRecent)
	g.current, g.haveSample = current, ok
	if !ok {
		return g.downCap, g.upCap, false
	}
	baseline, trusted := g.baselineLocked(now, windowMin)
	high := isHigh(current, baseline)
	rateDown, rateUp := int64(downBytes/1024), int64(upBytes/1024)
	loadDown, loadUp := rateDown >= latencyGuardMinLoadKiB, rateUp >= latencyGuardMinLoadKiB

	switch g.mode {
	case guardFree:
		g.othersOnLine = false
		if !(loadDown || loadUp) || now.Before(g.cooldownUntil) {
			break
		}
		if line.known && trusted {
			// The router tells who else is on the line: no trial needed.
			if high && line.others {
				g.yieldLocked(rateDown, rateUp, loadDown, loadUp, 0.85)
			}
			break
		}
		if high || !trusted {
			g.startTrialLocked(now, current, rateDown, rateUp, loadDown, loadUp)
		}
	case guardTrial:
		if now.Sub(g.trialUntil.Add(-latencyGuardTrial)) >= latencyGuardTrialSettle {
			g.trialSamples = append(g.trialSamples, current)
		}
		if now.Before(g.trialUntil) {
			break
		}
		measured := current
		if len(g.trialSamples) > 0 {
			measured = medianDuration(g.trialSamples)
		}
		dropped := fellFrom(measured, g.trialLoaded)
		if dropped && (g.rest == 0 || !trusted || measured < g.rest) {
			g.rest, g.restAt = measured, now
		}
		others := !dropped
		if line.known {
			others = line.others
		}
		if others {
			g.othersOnLine = true
			g.mode = guardYield
			// The trial caps are the starting point of the yield.
			break
		}
		g.releaseLocked(now)
	case guardYield:
		if !trusted {
			// Someone kept the line full since the trial, so its idle
			// latency was never seen: the line stays "high" until the latency
			// falls clearly below the loaded one, which is then the reference.
			high = !fellFrom(current, g.trialLoaded)
			if !high {
				g.rest, g.restAt = current, now
			}
		}
		if line.known {
			g.othersOnLine = line.others
			if !line.others {
				high = false // nobody else: climb back and release
			}
		}
		g.downCap = g.down.step(now, high, rateDown, peakDownKiB)
		g.upCap = g.up.step(now, high, rateUp, peakUpKiB)
		if !g.down.active && !g.up.active {
			g.releaseLocked(now)
		}
	}
	return g.downCap, g.upCap, g.downCap != prevDown || g.upCap != prevUp
}

// fellFrom reports a latency clearly below a loaded one: by 30 % or by more
// than the high margin.
func fellFrom(latency, loaded time.Duration) bool {
	return latency < loaded*7/10 || latency+autospeed.HighMargin < loaded
}

// startTrialLocked backs the loading directions off to latencyGuardTrialShare.
func (g *latencyGuard) startTrialLocked(now time.Time, loaded time.Duration, rateDown, rateUp int64, loadDown, loadUp bool) {
	g.mode = guardTrial
	g.trialUntil = now.Add(latencyGuardTrial)
	g.trialLoaded = loaded
	g.trialSamples = g.trialSamples[:0]
	g.yieldLocked(rateDown, rateUp, loadDown, loadUp, latencyGuardTrialShare)
	g.mode = guardTrial
}

// yieldLocked caps the loading directions at share of their current rate.
func (g *latencyGuard) yieldLocked(rateDown, rateUp int64, loadDown, loadUp bool, share float64) {
	g.mode = guardYield
	g.othersOnLine = true
	g.down, g.up = guardDirection{}, guardDirection{}
	g.downCap, g.upCap = 0, 0
	if loadDown {
		g.downCap = max(int64(float64(rateDown)*share), latencyGuardMinFloorKiB)
		g.down.start(g.downCap)
	}
	if loadUp {
		g.upCap = max(int64(float64(rateUp)*share), latencyGuardMinFloorKiB)
		g.up.start(g.upCap)
	}
}

// releaseLocked gives the line back to Gextto and waits before the next trial.
func (g *latencyGuard) releaseLocked(now time.Time) {
	g.mode = guardFree
	g.othersOnLine = false
	g.down, g.up = guardDirection{}, guardDirection{}
	g.downCap, g.upCap = 0, 0
	g.cooldownUntil = now.Add(latencyGuardCooldown)
}

// caps returns the caps in force (0 = none).
func (g *latencyGuard) caps() (downCap, upCap int64) {
	if g == nil {
		return 0, 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.downCap, g.upCap
}

// latencyGuardStatus is what the interface shows about the guard.
type latencyGuardStatus struct {
	Active      bool   `json:"active"`
	Measuring   bool   `json:"measuring"`
	Mode        string `json:"mode"`
	LatencyMs   int64  `json:"latency_ms"`
	BaselineMs  int64  `json:"baseline_ms"`
	DownloadKib int64  `json:"download_cap_kib"`
	UploadKib   int64  `json:"upload_cap_kib"`
	// RouterCounters: the router's WAN counters answer, so the others'
	// traffic below is measured; OthersOnLine is the guard's verdict.
	RouterCounters bool  `json:"router_counters"`
	OthersOnLine   bool  `json:"others_on_line"`
	OthersDownKib  int64 `json:"others_download_kib"`
	OthersUpKib    int64 `json:"others_upload_kib"`
}

func (g *latencyGuard) status() latencyGuardStatus {
	if g == nil {
		return latencyGuardStatus{}
	}
	othersDown, othersUp, known := g.usage.others()
	_, windowMin, _, ok := g.probe.State(time.Now(), latencyGuardRecent)
	g.mu.Lock()
	defer g.mu.Unlock()
	baseline := g.rest
	if ok {
		baseline, _ = g.baselineLocked(time.Now(), windowMin)
	}
	return latencyGuardStatus{
		Active:         g.downCap > 0 || g.upCap > 0,
		Measuring:      g.auto && g.haveSample,
		Mode:           g.mode.String(),
		LatencyMs:      g.current.Milliseconds(),
		BaselineMs:     baseline.Milliseconds(),
		DownloadKib:    g.downCap,
		UploadKib:      g.upCap,
		RouterCounters: known,
		OthersOnLine:   g.othersOnLine,
		OthersDownKib:  int64(othersDown / 1024),
		OthersUpKib:    int64(othersUp / 1024),
	}
}

// effectiveSpeedLimits is speedPolicy with the latency guard applied: on an
// Auto source the guard's caps replace "unlimited" while it is acting.
func effectiveSpeedLimits(cfg *Config, guard *latencyGuard) (download, upload int64, source string, tempShadowed bool) {
	download, upload, source, tempShadowed = speedPolicy(cfg)
	if !speedSourceIsAuto(source) || !latencyGuardEnabled(cfg) {
		return download, upload, source, tempShadowed
	}
	downCap, upCap := guard.caps()
	return downCap, upCap, source, tempShadowed
}

// latencyGuardWorker probes the latency while the speed is on Auto and the
// guard is on, and pushes the guard's caps to the engine as soon as they move.
// Outside Auto it probes nothing.
func latencyGuardWorker(state *AppState) {
	guard := state.latency_guard
	if guard == nil {
		return
	}
	ticker := time.NewTicker(latencyGuardProbeEvery)
	defer ticker.Stop()
	for {
		select {
		case <-state.bgStop:
			return
		case <-ticker.C:
		}
		guard.tick(time.Now(), latestConfig(state), state.activeEngine())
	}
}

// tick is one round of the guard: probe, read who else is on the line, step
// the caps and push them when they moved. Leaving Auto pushes nothing: the
// policy that replaced Auto sets its own limits.
func (g *latencyGuard) tick(now time.Time, cfg *Config, engine TorrentEngine) {
	_, _, source, _ := speedPolicy(cfg)
	if !speedSourceIsAuto(source) || !latencyGuardEnabled(cfg) {
		g.probe.Reset()
		g.usage.reset()
		g.mu.Lock()
		g.wireAt = time.Time{}
		g.mu.Unlock()
		g.update(now, false, lineGuess{}, 0, 0, 0, 0)
		return
	}
	if rtt, ok := autospeed.Measure(autospeed.Candidates(cfg.Settings[latencyGuardTargetKey], latencyGuardTargets)); ok {
		g.probe.Record(now, rtt)
	}
	down, up := g.ownRates(now, engine)
	g.usage.observe(now, down, up)
	others, known := g.usage.read(now)
	peakDown, peakUp := speedPeakKib(cfg, autotunePeakDownKey), speedPeakKib(cfg, autotunePeakUpKey)
	downCap, upCap, changed := g.update(now, true, lineGuess{known: known, others: others}, down, up, peakDown, peakUp)
	if !changed || engine == nil {
		return
	}
	if _, err := engine.SetGlobalSpeedLimits(downCap, upCap); err != nil {
		logging.Debug("latency guard apply failed", "error", err)
		return
	}
	st := g.status()
	logging.Debug("latency guard", "mode", st.Mode, "latency_ms", st.LatencyMs, "baseline_ms", st.BaselineMs, "others_on_line", st.OthersOnLine, "download_cap_kib", downCap, "upload_cap_kib", upCap)
}

// medianDuration is the median of values, which it leaves untouched.
func medianDuration(values []time.Duration) time.Duration {
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return sorted[len(sorted)/2]
}
