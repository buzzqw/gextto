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
// raising them, then lifting them, once the latency is back at rest. Both
// directions are capped, and the upload one starts from a much lower load
// because home uplinks are asymmetric and fill up first. When the router's WAN
// counters answer (line_usage.go) their verdict on "someone else" comes first,
// which also sees users of little bandwidth (a call, a game) that a trial
// cannot. The controller is internal/autospeed, shared with the standalone
// daemon.

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
	// latencyGuardSourceKey chooses what the controller listens to:
	// "ping" (Google/Cloudflare only, the router is not read), "both"
	// (ping decides, the router is shown as a diagnostic) or "router" (the
	// old behaviour: yield only to other devices the router sees).
	latencyGuardSourceKey = "torrent_auto_latency_source"
	// latencyGuardLineMaxDownKey/UpKey hold the line's maximum in KiB/s as
	// entered by the user (0 = unknown). When known, the controller uses it as
	// the ceiling and finds the knee faster; it is not a hard cap in the code,
	// it is the number the user knows about the line.
	latencyGuardLineMaxDownKey = "torrent_line_max_down_kib"
	latencyGuardLineMaxUpKey   = "torrent_line_max_up_kib"
	// latencySourcePing/Both/Router are the values of that setting.
	latencySourcePing   = "ping"
	latencySourceBoth   = "both"
	latencySourceRouter = "router"

	// latencyGuardProbeEvery is the probe cadence; latencyGuardWindow how far
	// back the probe keeps samples.
	latencyGuardProbeEvery = 2 * time.Second
	latencyGuardWindow     = 10 * time.Minute
	// latencyGuardRecent is how many samples the current latency is the
	// median of, so one late answer does not move the caps. Ten seconds at the
	// two-second probe cadence: a single spike must not cut the line.
	latencyGuardRecent = 5

	// latencyGuardTrial is how long a trial backs Gextto off;
	// latencyGuardTrialSettle the part of it left to the queue to drain
	// before its latency counts; latencyGuardTrialShare the share of the
	// current rate Gextto keeps during it.
	latencyGuardTrial       = 8 * time.Second
	latencyGuardTrialSettle = 3 * time.Second
	latencyGuardTrialShare  = 0.6
	// latencyGuardCooldown is the wait after a release before another probe of
	// the line. A confirmed latency rise is not blocked by it: the controller
	// must react as soon as the ping degrades.
	latencyGuardCooldown = time.Minute
	// latencyGuardRestAge is how long a measured idle reference is kept. Only
	// a lower measure replaces it before then: a trial disturbed by someone
	// else's light traffic must not raise the reference and blind the guard.
	latencyGuardRestAge = 6 * time.Hour

	// latencyGuardMinLoadKiB: a direction below this is not loading the line.
	latencyGuardMinLoadKiB = 256
	// latencyGuardMinLoadUpKiB is the same floor for upload. It is much lower
	// because home lines are asymmetric: 256 KiB/s on an 800 KiB/s uplink is
	// already more than a quarter of the capacity, and the provider's queue
	// starts adding latency well before that. With the line maximum known the
	// threshold follows it (a tenth, between 32 and 256 KiB/s), so a saturated
	// upload is capped even when the download is the direction moving the most
	// bytes.
	latencyGuardMinLoadUpKiB = 64
	// latencyGuardMaxDownKiB/UpKiB = 0 means no hard cap in the code. The
	// controller ramps while the ping is good and stops at twice the rate the
	// line is actually delivering (a dynamic governor, not a fixed ceiling), so
	// a gigabit or 10-gigabit line is never limited by a constant chosen here.
	// The ping, not a number, decides where the cap settles.
	latencyGuardMaxDownKiB = 0
	latencyGuardMaxUpKiB   = 0
	// latencyGuardRiseDownKiB/UpKiB is the additive part of the upward step.
	// The controller raises the cap while the ping is good and cuts it when the
	// ping degrades; this is how it finds the line instead of sitting on a low
	// peak measured during a slow swarm.
	latencyGuardRiseDownKiB = 512
	latencyGuardRiseUpKiB   = 64
	// latencyGuardMinFloorDownKiB/UpKiB is the lowest cap the controller may
	// settle on. The old 64 KiB/s was indistinguishable from a stall and was
	// the visible sawtooth; the floor only applies while the cap is active.
	latencyGuardMinFloorDownKiB = 128
	latencyGuardMinFloorUpKiB   = 64
	// latencyGuardIdleKiB: below this in both directions nothing is
	// transferring. The DHT and the peer keep-alives never go fully silent, so
	// "zero" would never release a cap left over from the last download.
	latencyGuardIdleKiB = 16
)

// latencyGuardTargets are probed when no target is configured: Google and
// Cloudflare, reached through the provider's queue (the gateway does not show
// it). They are anycast and stable, and the controller takes the lowest of the
// answers, so one server being far, slow or unreachable cannot fake a bad line.
var latencyGuardTargets = []string{"8.8.8.8:443", "8.8.4.4:443", "1.1.1.1:443", "1.0.0.1:443"}

// guardMode is where the guard stands.
type guardMode int

const (
	guardFree    guardMode = iota // no caps: the ping is good at full speed
	guardTrial                    // backed off for a few seconds, measuring
	guardControl                  // cap active: ramp up while the ping is good
)

func (m guardMode) String() string {
	return [...]string{"free", "trial", "control"}[m]
}

// guardDirection is the guard's cap for download or upload.
type guardDirection struct {
	clamp  autospeed.Clamp
	active bool
	// knee is the cap at which the latency last degraded (0 = not seen yet).
	// The quick ramp stops below it, the cap then climbs by riseKiB to a
	// plateau just under it and stays there, trying to pass it once every
	// latencyGuardKneeProbe. The quarter step jumped straight back over the
	// knee after every cut and kept the line queued most of the time.
	knee int64
	// cutAt and riseAt are the last moves near the knee, probeAt the last try
	// above the plateau. The current latency is a median of several probes,
	// so it shows a move only some seconds later: until it can, the cap does
	// not move again.
	cutAt, riseAt, probeAt time.Time
}

// latencyGuardCutSettle is how long the median of the recent probes takes to
// reflect a move of the cap: more than half of its samples must be taken
// after the queue had a probe interval to follow it.
const latencyGuardCutSettle = (latencyGuardRecent/2 + 2) * latencyGuardProbeEvery

// latencyGuardKneeProbe is how often the cap tries to pass a known knee: the
// line may have more room now (someone else stopped, the swarm grew).
const latencyGuardKneeProbe = time.Minute

// start caps the direction at value.
func (d *guardDirection) start(value int64) {
	*d = guardDirection{active: true}
	d.clamp.Value = value
}

// step moves an active cap: up while the ping is good, down when it degrades.
// It returns the cap (0 = none). ceilingKiB is the maximum the controller may
// reach: the line maximum the user entered, or (0) twice the rate the line is
// actually delivering. Using the current rate as the ceiling was the lock that
// kept the cap at a slow sample; the 2x governor still lets it grow when the
// rate grows, while a value entered by the user is respected exactly.
func (d *guardDirection) step(now time.Time, high bool, rateKiB, ceilingKiB, floorKiB, riseKiB int64) int64 {
	if !d.active {
		return 0
	}
	floor := max(floorKiB, 1)
	ceiling := ceilingKiB
	if ceiling <= 0 {
		ceiling = max(d.clamp.Value, rateKiB*2)
	}
	loading := rateKiB >= d.clamp.Value/2
	high = high && loading
	before := d.clamp.Value
	if now.Sub(d.cutAt) < latencyGuardCutSettle {
		return before
	}
	if d.knee > 0 && before > d.knee && !high && now.Sub(d.riseAt) >= latencyGuardCutSettle {
		// A try above the knee held: the line has more room, ramp again.
		d.knee = 0
	}
	approach, plateau := d.knee*85/100, d.knee*9/10
	near := d.knee > 0 && before >= approach
	if near && !high {
		if now.Sub(d.riseAt) < latencyGuardCutSettle {
			return before
		}
		if before >= plateau && now.Sub(d.probeAt) < latencyGuardKneeProbe {
			return before
		}
	}
	var value int64
	if near {
		// Near the knee the step is a twentieth of it, so a slow line is not
		// pushed over its knee by a step sized for a fast one.
		rise := max(d.knee/20, 1)
		if riseKiB < rise {
			rise = riseKiB
		}
		value = d.clamp.StepBy(now, high, ceiling, floor, rise)
		if value > plateau && before < plateau {
			value = plateau
			d.clamp.Value = value
		}
	} else {
		value = d.clamp.StepRamp(now, high, ceiling, floor, riseKiB)
		if d.knee > 0 && value > approach {
			value = max(approach, before)
			d.clamp.Value = value
		}
	}
	switch {
	case high && value < before:
		d.knee, d.cutAt = before, now
	case value > before:
		d.riseAt = now
		if near && before >= plateau {
			d.probeAt = now
		}
	}
	return value
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
	trialHigh      bool          // whether that latency was already high
	trialSamples   []time.Duration
	cooldownUntil  time.Time
	haveSample     bool
	auto           bool
	othersOnLine   bool
	// routerOnly selects the legacy policy: cap only when the router confirms
	// other traffic. With "ping"/"both" the latency decides and the router is
	// only a diagnostic.
	routerOnly bool
	// controlStarted is set when the controller applies its first cap of a
	// session; tick logs it once, with the values and the ping.
	controlStarted bool
	// capLoggedAt/Down/Up are that first-cap line: when it was written and the
	// caps it named. The periodic speed line adopts it as its own last line,
	// so it does not repeat the same numbers a few seconds later.
	capLoggedAt                time.Time
	capLoggedDown, capLoggedUp int64
	// lineMaxDown/Up are the user's line maxima in KiB/s (0 = unknown).
	lineMaxDown, lineMaxUp int64

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

// latencyGuardSource reads the source setting: "ping" ignores the router,
// "both" uses the ping for control and the router as a diagnostic, "router"
// keeps the legacy policy. An unknown or empty value is "both".
func latencyGuardSource(cfg *Config) string {
	if cfg == nil {
		return latencySourceBoth
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Settings[latencyGuardSourceKey])) {
	case latencySourcePing:
		return latencySourcePing
	case latencySourceRouter:
		return latencySourceRouter
	default:
		return latencySourceBoth
	}
}

// latencyGuardRouterEnabled reports whether the router counters are read. In
// "ping" they are not, so a router that is absent or wrong cannot affect the
// caps.
func latencyGuardRouterEnabled(cfg *Config) bool {
	return latencyGuardSource(cfg) != latencySourcePing
}

// latencyGuardRouterOnly reports whether the legacy router-only policy is in
// force.
func latencyGuardRouterOnly(cfg *Config) bool {
	return latencyGuardSource(cfg) == latencySourceRouter
}

// latencyGuardLineMaxKib reads the line maxima entered by the user (0 = not
// known). They are a ceiling for the controller, not a hard-coded limit: when
// both are 0 the guard uses the 2x-rate governor.
func latencyGuardLineMaxKib(cfg *Config) (down, up int64) {
	if cfg == nil {
		return 0, 0
	}
	down = max(settingsParseInt(cfg, latencyGuardLineMaxDownKey, 0), 0)
	up = max(settingsParseInt(cfg, latencyGuardLineMaxUpKey, 0), 0)
	return down, up
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

// update runs one controller step with the current rates (bytes/s) and
// returns the caps in force (0 = none) and whether they changed.
//
// The decision is taken from the latency to Google/Cloudflare alone. The
// router's counters are only a diagnostic: they can be missing, stale or
// wrong, and they must never decide how much bandwidth Gextto uses. line is
// kept for the status badge, not for the control loop.
func (g *latencyGuard) update(now time.Time, auto bool, line lineGuess, downBytes, upBytes uint64) (downCap, upCap int64, changed bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	prevDown, prevUp := g.downCap, g.upCap
	g.auto = auto
	g.othersOnLine = line.known && line.others
	if !auto {
		g.mode = guardFree
		g.down, g.up = guardDirection{}, guardDirection{}
		g.downCap, g.upCap = 0, 0
		g.haveSample, g.othersOnLine = false, false
		g.capLoggedAt, g.capLoggedDown, g.capLoggedUp = time.Time{}, 0, 0
		return 0, 0, prevDown != 0 || prevUp != 0
	}
	current, windowMin, _, ok := g.probe.State(now, latencyGuardRecent)
	g.current, g.haveSample = current, ok
	if !ok {
		// No measurement: hold what we have. Capping on a failed probe would
		// throttle the line, and releasing would risk the bufferbloat the guard
		// exists to prevent.
		return g.downCap, g.upCap, false
	}
	baseline, trusted := g.baselineLocked(now, windowMin)
	high := isHigh(current, baseline)
	rateDown, rateUp := int64(downBytes/1024), int64(upBytes/1024)
	uploadLoad := guardUploadLoadThreshold(g.lineMaxUp)
	loadDown, loadUp := rateDown >= latencyGuardMinLoadKiB, rateUp >= uploadLoad

	switch g.mode {
	case guardFree:
		if !(loadDown || loadUp) {
			break
		}
		if g.routerOnly && line.known {
			// Legacy policy: the router says who else is on the line. We cap
			// only for them; our own queue, when nobody else is there, is not
			// a reason to slow down.
			if high && line.others {
				g.controlLocked(rateDown, rateUp, loadDown, loadUp, 0.85)
			}
			break
		}
		if now.Before(g.cooldownUntil) {
			break
		}
		if high || !trusted {
			g.startTrialLocked(now, current, high, rateDown, rateUp, loadDown, loadUp)
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
		if !dropped && !g.trialHigh {
			// The trial only checked a stale reference: the ping was good at
			// full speed and backing off did not lower it, so there is no queue
			// at all. That latency is the line at rest, and the line goes back
			// to Gextto. Reading "did not fall" as "still high" here was what
			// cut a good line to the floor and held it there.
			rest := measured
			if g.trialLoaded < rest {
				rest = g.trialLoaded
			}
			if g.rest == 0 || !trusted || rest < g.rest {
				g.rest, g.restAt = rest, now
			}
			g.releaseLocked(now)
			break
		}
		// The trial measured the line at a reduced rate. From here the
		// controller ramps up while the ping stays good and cuts when it
		// degrades; it no longer releases the line just because the latency
		// fell (that is exactly the moment the line can be pushed again).
		g.mode = guardControl
	case guardControl:
		if !trusted {
			// The idle reference was never seen: use the latency measured
			// during the trial as the loaded reference and treat a clear fall
			// as the new rest. A latency that is not even high against the
			// window's minimum is never high.
			high = high && !fellFrom(current, g.trialLoaded)
			if !high {
				g.rest, g.restAt = current, now
			}
		}
		if rateDown < latencyGuardIdleKiB && rateUp < latencyGuardIdleKiB {
			// Nothing is transferring: there is no queue to protect and no
			// reason to keep a stale cap for the next download.
			g.releaseLocked(now)
			break
		}
		if g.routerOnly && line.known && !line.others && !high {
			// In the legacy policy the router is the authority: the others are
			// gone and the ping is back to rest, so the line returns to Gextto.
			g.releaseLocked(now)
			break
		}
		// A direction that was idle when the trial started has no cap yet. If
		// it starts loading while the ping is still high, cap it now instead
		// of leaving it unlimited until the next trial: this is how a high
		// upload, which fills its narrower pipe, gets its own limit even when
		// the download was the direction that triggered the trial.
		if (high || !trusted) && loadDown && !g.down.active {
			g.downCap = max(int64(float64(rateDown)*latencyGuardTrialShare), latencyGuardMinFloorDownKiB)
			g.down.start(g.downCap)
		}
		if (high || !trusted) && loadUp && !g.up.active {
			g.upCap = max(int64(float64(rateUp)*latencyGuardTrialShare), latencyGuardMinFloorUpKiB)
			g.up.start(g.upCap)
		}
		g.downCap = g.down.step(now, high, rateDown, g.lineMaxDown, latencyGuardMinFloorDownKiB, latencyGuardRiseDownKiB)
		g.upCap = g.up.step(now, high, rateUp, g.lineMaxUp, latencyGuardMinFloorUpKiB, latencyGuardRiseUpKiB)
		if !g.down.active && !g.up.active {
			g.releaseLocked(now)
		}
	}
	return g.downCap, g.upCap, g.downCap != prevDown || g.upCap != prevUp
}

// guardUploadLoadThreshold is the upload rate at which a direction counts as
// loading the line. It follows the configured uplink (a tenth of it, between
// 32 and 256 KiB/s) because an asymmetric home line can be queued by an upload
// far smaller than the download threshold.
func guardUploadLoadThreshold(lineMaxUpKiB int64) int64 {
	if lineMaxUpKiB <= 0 {
		return int64(latencyGuardMinLoadUpKiB)
	}
	threshold := lineMaxUpKiB / 10
	if threshold < 32 {
		threshold = 32
	}
	if threshold > latencyGuardMinLoadKiB {
		threshold = latencyGuardMinLoadKiB
	}
	return threshold
}

// fellFrom reports a latency clearly below a loaded one: by 30 % or by more
// than the high margin.
func fellFrom(latency, loaded time.Duration) bool {
	return latency < loaded*7/10 || latency+autospeed.HighMargin < loaded
}

// startTrialLocked backs the loading directions off to latencyGuardTrialShare.
func (g *latencyGuard) startTrialLocked(now time.Time, loaded time.Duration, high bool, rateDown, rateUp int64, loadDown, loadUp bool) {
	g.mode = guardTrial
	g.trialUntil = now.Add(latencyGuardTrial)
	g.trialLoaded = loaded
	g.trialHigh = high
	g.trialSamples = g.trialSamples[:0]
	g.controlLocked(rateDown, rateUp, loadDown, loadUp, latencyGuardTrialShare)
	g.mode = guardTrial
}

// controlLocked starts a control phase, capping the loading directions at
// share of their current rate. The cap then ramps up while the ping is good
// and is cut when it degrades.
func (g *latencyGuard) controlLocked(rateDown, rateUp int64, loadDown, loadUp bool, share float64) {
	g.mode = guardControl
	g.controlStarted = true
	g.down, g.up = guardDirection{}, guardDirection{}
	g.downCap, g.upCap = 0, 0
	if loadDown {
		g.downCap = max(int64(float64(rateDown)*share), latencyGuardMinFloorDownKiB)
		g.down.start(g.downCap)
	}
	if loadUp {
		g.upCap = max(int64(float64(rateUp)*share), latencyGuardMinFloorUpKiB)
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

// guardCapForEngine turns a finite controller maximum into "unlimited" (0).
// maxKiB <= 0 means there is no hard cap at all, so the value is passed through.
func guardCapForEngine(capKiB, maxKiB int64) int64 {
	if maxKiB > 0 && capKiB >= maxKiB {
		return 0
	}
	return capKiB
}

// caps returns the caps in force (0 = none).
func (g *latencyGuard) caps() (downCap, upCap int64) {
	if g == nil {
		return 0, 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return guardCapForEngine(g.downCap, latencyGuardMaxDownKiB), guardCapForEngine(g.upCap, latencyGuardMaxUpKiB)
}

// firstCapLog is the last first-cap line the guard wrote itself: when it did,
// and the caps it named. The periodic speed logger adopts it as its own last
// line, so the same numbers are not printed twice.
func (g *latencyGuard) firstCapLog() (at time.Time, downCap, upCap int64) {
	if g == nil {
		return time.Time{}, 0, 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.capLoggedAt, g.capLoggedDown, g.capLoggedUp
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
	downKib := guardCapForEngine(g.downCap, latencyGuardMaxDownKiB)
	upKib := guardCapForEngine(g.upCap, latencyGuardMaxUpKiB)
	return latencyGuardStatus{
		Active:         downKib > 0 || upKib > 0,
		Measuring:      g.auto && g.haveSample,
		Mode:           g.mode.String(),
		LatencyMs:      g.current.Milliseconds(),
		BaselineMs:     baseline.Milliseconds(),
		DownloadKib:    downKib,
		UploadKib:      upKib,
		RouterCounters: known,
		OthersOnLine:   g.othersOnLine,
		OthersDownKib:  int64(othersDown / 1024),
		OthersUpKib:    int64(othersUp / 1024),
	}
}

// effectiveSpeedLimits is speedPolicy with the latency guard applied: on an
// Auto source the guard's caps replace "unlimited" while it is acting.
func effectiveSpeedLimits(cfg *Config, guard *latencyGuard) (download, upload int64, source string) {
	download, upload, source = speedPolicy(cfg)
	if !speedSourceIsAuto(source) || !latencyGuardEnabled(cfg) {
		return download, upload, source
	}
	downCap, upCap := guard.caps()
	return downCap, upCap, source
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
	_, _, source := speedPolicy(cfg)
	if !speedSourceIsAuto(source) || !latencyGuardEnabled(cfg) {
		g.probe.Reset()
		g.usage.reset()
		g.mu.Lock()
		g.wireAt = time.Time{}
		g.mu.Unlock()
		g.update(now, false, lineGuess{}, 0, 0)
		return
	}
	// Google and Cloudflare, several targets, best RTT: a single server can be
	// far away, rate-limited or unreachable, and the router can be wrong or
	// missing. The minimum of the answers is the line, not the server.
	if rtt, ok := autospeed.MeasureBest(autospeed.Candidates(cfg.Settings[latencyGuardTargetKey], latencyGuardTargets)); ok {
		g.probe.Record(now, rtt)
	}
	down, up := g.ownRates(now, engine)
	g.usage.observe(now, down, up)
	var line lineGuess
	if latencyGuardRouterEnabled(cfg) {
		others, known := g.usage.read(now)
		line = lineGuess{known: known, others: others}
	} else {
		// "ping": the router is not read at all, so an absent or lying router
		// cannot influence the caps.
		g.usage.reset()
	}
	g.mu.Lock()
	g.routerOnly = latencyGuardRouterOnly(cfg)
	g.lineMaxDown, g.lineMaxUp = latencyGuardLineMaxKib(cfg)
	g.mu.Unlock()
	downCap, upCap, changed := g.update(now, true, line, down, up)
	downCap = guardCapForEngine(downCap, latencyGuardMaxDownKiB)
	upCap = guardCapForEngine(upCap, latencyGuardMaxUpKiB)
	// The first cap of a session is logged at once, with the values and the
	// ping, even if it is the only line the controller ever prints: the user
	// asked to see when autotune starts and at what speed.
	g.mu.Lock()
	started := g.controlStarted
	g.controlStarted = false
	if started {
		g.capLoggedAt, g.capLoggedDown, g.capLoggedUp = now, downCap, upCap
	}
	g.mu.Unlock()
	if started {
		st := g.status()
		logging.Info("🎚️ Autotune: starting with " + speedPairText(downCap, upCap) +
			bg_speedDetailSuffix(bg_speedMaxText(cfg), bg_pingText(st)))
	}
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
