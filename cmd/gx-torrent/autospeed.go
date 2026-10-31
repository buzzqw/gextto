package main

// autospeed.go is the standalone automatic speed controller (autotune): the
// counterpart of Gextto's Auto speed. A probe measures the round-trip latency
// towards Google/Cloudflare (or a configured host, with the gateway as a
// fallback); when the line queues, the controller lowers the download and the
// upload, and raises them again while the latency stays at rest. The maximum
// of each direction is the line maximum entered by the user, or twice the rate
// the swarm is delivering, so a fast line is never limited by a constant.
//
// The configured (or scheduled) limits are ceilings: automatic speed never
// goes above them, and a limit of 0 lets it discover that direction. The
// option lives in settings.json (standalone mode); managed mode is driven by
// Gextto and does not read it.
//
// The probe and the rate step are internal/autospeed, the controller shared
// with Gextto's latency guard. This file keeps the standalone state machine,
// which has no router counters: the latency alone decides.

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/autospeed"
)

const (
	autospeedEnabledKey = "autospeed-enabled"
	autospeedTargetKey  = "autospeed-target"
	autospeedMinKey     = "autospeed-min"
	// autospeedLineDownKey/UpKey are the line maxima in KiB/s (0 = unknown).
	autospeedLineDownKey = "autospeed-line-down"
	autospeedLineUpKey   = "autospeed-line-up"

	// autoSpeedDefaultMinKiB is the lowest upload the controller may settle on.
	autoSpeedDefaultMinKiB = 50

	// autoSpeedProbeInterval is how often the latency is sampled and
	// autoSpeedWindow how far back the idle baseline is remembered.
	autoSpeedProbeInterval = 1 * time.Second
	autoSpeedWindow        = 10 * time.Minute
	// autoSpeedRecent is how many probes the current latency is the median of.
	autoSpeedRecent = 5

	// autoSpeedTrial is how long the controller backs off to check whether the
	// queue is its own; autoSpeedTrialSettle leaves the queue time to drain;
	// autoSpeedTrialShare is the share of the current rate kept during it.
	autoSpeedTrial       = 8 * time.Second
	autoSpeedTrialSettle = 3 * time.Second
	autoSpeedTrialShare  = 0.6
	// autoSpeedCooldown is the wait after releasing the line before another
	// trial. A confirmed latency rise is not blocked: the state machine acts
	// as soon as the ping degrades.
	autoSpeedCooldown = time.Minute
	// autoSpeedRestAge is how long a measured idle reference is kept.
	autoSpeedRestAge = 6 * time.Hour

	// autoSpeedMinLoadKiB is the download rate that counts as loading the
	// line; autoSpeedMinLoadUpKiB is the upload floor.
	autoSpeedMinLoadKiB   = 256
	autoSpeedMinLoadUpKiB = 64
	// autoSpeedMinFloorDownKiB/UpKiB are the lowest caps the controller may
	// settle on while active.
	autoSpeedMinFloorDownKiB = 128
	autoSpeedMinFloorUpKiB   = 64
	// autoSpeedRiseDownKiB/UpKiB are the additive part of the upward step.
	autoSpeedRiseDownKiB = 512
	autoSpeedRiseUpKiB   = 64
	// autoSpeedIdleKiB: below this in both directions nothing is transferring.
	autoSpeedIdleKiB = 16
	// autoSpeedKneeProbe is how often the cap tries to pass a known knee.
	autoSpeedKneeProbe = time.Minute
)

// autoSpeedCutSettle is how long the median of the recent probes takes to
// reflect a move of the cap: more than half of its samples must be taken after
// the queue had a probe interval to follow it.
const autoSpeedCutSettle = (autoSpeedRecent/2 + 2) * autoSpeedProbeInterval

// autoSpeedTargets are probed when no target is configured: Google and
// Cloudflare, reached through the provider's queue (the gateway does not show
// it). They are anycast and stable, and the controller takes the lowest of the
// answers, so one server being far, slow or unreachable cannot fake a bad line.
var autoSpeedTargets = []string{"8.8.8.8:443", "8.8.4.4:443", "1.1.1.1:443", "1.0.0.1:443"}

// autoSpeedMode is where the controller stands.
type autoSpeedMode int

const (
	autoSpeedModeFree    autoSpeedMode = iota // no caps: the ping is good at full speed
	autoSpeedModeTrial                        // backed off for a few seconds, measuring
	autoSpeedModeControl                      // cap active: ramp up while the ping is good
)

func (m autoSpeedMode) String() string {
	return [...]string{"free", "trial", "control"}[m]
}

// autoSpeedDirection is the controller's cap for download or upload. It
// mirrors the guard direction of Gextto's latency guard: the knee is the cap
// at which the latency last degraded, and the ramp stops just below it.
type autoSpeedDirection struct {
	clamp  autospeed.Clamp
	active bool
	// knee is the cap at which the latency last degraded (0 = not seen yet).
	knee int64
	// cutAt and riseAt are the last moves near the knee, probeAt the last try
	// above the plateau.
	cutAt, riseAt, probeAt time.Time
}

// start caps the direction at value.
func (d *autoSpeedDirection) start(value int64) {
	*d = autoSpeedDirection{active: true}
	d.clamp.Value = value
}

// step moves an active cap: up while the ping is good, down when it degrades.
// It returns the cap (0 = none). ceilingKiB is the maximum the controller may
// reach: the line maximum the user entered, the configured limit, or (0) twice
// the rate the line is actually delivering.
func (d *autoSpeedDirection) step(now time.Time, high bool, rateKiB, ceilingKiB, floorKiB, riseKiB int64) int64 {
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
	if now.Sub(d.cutAt) < autoSpeedCutSettle {
		return before
	}
	if d.knee > 0 && before > d.knee && !high && now.Sub(d.riseAt) >= autoSpeedCutSettle {
		// A try above the knee held: the line has more room, ramp again.
		d.knee = 0
	}
	approach, plateau := d.knee*85/100, d.knee*9/10
	near := d.knee > 0 && before >= approach
	if near && !high {
		if now.Sub(d.riseAt) < autoSpeedCutSettle {
			return before
		}
		if before >= plateau && now.Sub(d.probeAt) < autoSpeedKneeProbe {
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

// autoSpeed is the standalone controller: the shared latency probe and the
// one cap per direction. The zero value is ready to use.
type autoSpeed struct {
	mu    sync.Mutex
	probe autospeed.Probe
	mode  autoSpeedMode

	down, up       autoSpeedDirection
	downCap, upCap int64
	current        time.Duration
	baseline       time.Duration
	rest           time.Duration // idle reference learned by a trial
	restAt         time.Time
	trialUntil     time.Time
	trialLoaded    time.Duration // latency when the trial started
	trialHigh      bool          // whether that latency was already high
	trialSamples   []time.Duration
	cooldownUntil  time.Time
	haveSample     bool
	lineMaxDown    int64
	lineMaxUp      int64
}

func (a *autoSpeed) probeLocked() *autospeed.Probe {
	if a.probe.Window == 0 {
		a.probe.Window = autoSpeedWindow
	}
	return &a.probe
}

// record adds a measured latency. A non-positive value is ignored.
func (a *autoSpeed) record(now time.Time, rtt time.Duration) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.probeLocked().Record(now, rtt)
}

// reset forgets the samples and every cap (on disable).
func (a *autoSpeed) reset() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.probeLocked().Reset()
	a.mode = autoSpeedModeFree
	a.down, a.up = autoSpeedDirection{}, autoSpeedDirection{}
	a.downCap, a.upCap = 0, 0
	a.current, a.baseline = 0, 0
	a.rest, a.restAt = 0, time.Time{}
	a.trialUntil, a.trialLoaded, a.trialHigh = time.Time{}, 0, false
	a.trialSamples = nil
	a.cooldownUntil = time.Time{}
	a.haveSample = false
	a.lineMaxDown, a.lineMaxUp = 0, 0
}

// isHigh applies the shared rule of internal/autospeed to a reference.
func autoSpeedHigh(current, baseline time.Duration) bool {
	return current > time.Duration(float64(baseline)*autospeed.HighFactor) && current > baseline+autospeed.HighMargin
}

// fellFrom reports a latency clearly below a loaded one: by 30 % or by more
// than the high margin.
func autoSpeedFellFrom(latency, loaded time.Duration) bool {
	return latency < loaded*7/10 || latency+autospeed.HighMargin < loaded
}

func autoSpeedMedian(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), values...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return sorted[len(sorted)/2]
}

// baselineLocked is the idle reference: the trial's measure while it is
// fresh, else the lowest latency of the probe window.
func (a *autoSpeed) baselineLocked(now time.Time, windowMin time.Duration) (time.Duration, bool) {
	if a.rest > 0 && now.Sub(a.restAt) < autoSpeedRestAge {
		if windowMin < a.rest {
			return windowMin, true
		}
		return a.rest, true
	}
	return windowMin, false
}

// ceiling returns the maximum a direction may reach: the user's line maximum,
// the configured limit, or 0 for the dynamic twice-the-rate governor.
func autoSpeedCeiling(configuredKiB, lineMaxKiB int64) int64 {
	switch {
	case configuredKiB > 0 && lineMaxKiB > 0:
		return min(configuredKiB, lineMaxKiB)
	case configuredKiB > 0:
		return configuredKiB
	default:
		return lineMaxKiB
	}
}

// effectiveLimit applies a controller cap to the configured (or scheduled)
// limit: the cap can only lower it. A cap of 0 means "no cap from the
// controller", so the configured value stays.
func effectiveSpeedLimit(configuredKiB, capKiB int64) int64 {
	if capKiB <= 0 {
		return configuredKiB
	}
	if configuredKiB > 0 && capKiB > configuredKiB {
		return configuredKiB
	}
	return capKiB
}

// startTrialLocked backs the loading directions off to autoSpeedTrialShare.
func (a *autoSpeed) startTrialLocked(now time.Time, loaded time.Duration, high bool, rateDown, rateUp, floorUp int64, loadDown, loadUp bool) {
	a.mode = autoSpeedModeTrial
	a.trialUntil = now.Add(autoSpeedTrial)
	a.trialLoaded = loaded
	a.trialHigh = high
	a.trialSamples = a.trialSamples[:0]
	a.controlLocked(rateDown, rateUp, floorUp, loadDown, loadUp, autoSpeedTrialShare)
	a.mode = autoSpeedModeTrial
}

// controlLocked starts a control phase, capping the loading directions at
// share of their current rate. The cap then ramps up while the ping is good
// and is cut when it degrades.
func (a *autoSpeed) controlLocked(rateDown, rateUp, floorUp int64, loadDown, loadUp bool, share float64) {
	a.mode = autoSpeedModeControl
	a.down, a.up = autoSpeedDirection{}, autoSpeedDirection{}
	a.downCap, a.upCap = 0, 0
	if loadDown {
		a.downCap = max(int64(float64(rateDown)*share), autoSpeedMinFloorDownKiB)
		a.down.start(a.downCap)
	}
	if loadUp {
		a.upCap = max(int64(float64(rateUp)*share), max(floorUp, autoSpeedMinFloorUpKiB))
		a.up.start(a.upCap)
	}
}

// releaseLocked gives the line back and waits before the next trial.
func (a *autoSpeed) releaseLocked(now time.Time) {
	a.mode = autoSpeedModeFree
	a.down, a.up = autoSpeedDirection{}, autoSpeedDirection{}
	a.downCap, a.upCap = 0, 0
	a.cooldownUntil = now.Add(autoSpeedCooldown)
}

// uploadLoadThreshold is the upload rate at which a direction counts as
// loading the line. It follows the configured uplink (a tenth of it, between
// 32 and 256 KiB/s) because an asymmetric home line can be queued by an
// upload far smaller than the download threshold.
func autoSpeedUploadLoadThreshold(lineMaxUpKiB int64) int64 {
	if lineMaxUpKiB <= 0 {
		return autoSpeedMinLoadUpKiB
	}
	threshold := lineMaxUpKiB / 10
	if threshold < 32 {
		threshold = 32
	}
	if threshold > autoSpeedMinLoadKiB {
		threshold = autoSpeedMinLoadKiB
	}
	return threshold
}

// update runs one controller step with the current rates (bytes/s) and
// returns the caps in force (0 = none) for download and upload.
// downLimit/upLimit are the configured or scheduled ceilings (0 = unlimited),
// lineMaxDown/lineMaxUp the user's line maxima (0 = unknown), floorUp the
// lowest uplink the controller may settle on.
func (a *autoSpeed) update(now time.Time, downLimit, upLimit, lineMaxDown, lineMaxUp, floorUp int64, downBytes, upBytes int64) (downCap, upCap int64) {
	if a == nil {
		return 0, 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lineMaxDown, a.lineMaxUp = max(lineMaxDown, 0), max(lineMaxUp, 0)
	current, windowMin, _, ok := a.probeLocked().State(now, autoSpeedRecent)
	a.current, a.haveSample = current, ok
	if !ok {
		// No measurement: hold what we have. Capping on a failed probe would
		// throttle the line, and releasing would risk the bufferbloat the
		// controller exists to prevent.
		return a.downCap, a.upCap
	}
	baseline, trusted := a.baselineLocked(now, windowMin)
	a.baseline = baseline
	high := autoSpeedHigh(current, baseline)
	rateDown, rateUp := int64(downBytes/1024), int64(upBytes/1024)
	loadDown := rateDown >= autoSpeedMinLoadKiB
	loadUp := rateUp >= autoSpeedUploadLoadThreshold(a.lineMaxUp)

	switch a.mode {
	case autoSpeedModeFree:
		if !(loadDown || loadUp) {
			break
		}
		if now.Before(a.cooldownUntil) {
			break
		}
		if high || !trusted {
			a.startTrialLocked(now, current, high, rateDown, rateUp, floorUp, loadDown, loadUp)
		}
	case autoSpeedModeTrial:
		if now.Sub(a.trialUntil.Add(-autoSpeedTrial)) >= autoSpeedTrialSettle {
			a.trialSamples = append(a.trialSamples, current)
		}
		if now.Before(a.trialUntil) {
			break
		}
		measured := current
		if len(a.trialSamples) > 0 {
			measured = autoSpeedMedian(a.trialSamples)
		}
		dropped := autoSpeedFellFrom(measured, a.trialLoaded)
		if dropped && (a.rest == 0 || !trusted || measured < a.rest) {
			a.rest, a.restAt = measured, now
		}
		if !dropped && !a.trialHigh {
			// The trial only checked a stale reference: the ping was good at
			// full speed and backing off did not lower it, so there is no
			// queue at all. That latency is the line at rest, and the line
			// goes back to full speed.
			rest := measured
			if a.trialLoaded < rest {
				rest = a.trialLoaded
			}
			if a.rest == 0 || !trusted || rest < a.rest {
				a.rest, a.restAt = rest, now
			}
			a.releaseLocked(now)
			break
		}
		// The trial measured the line at a reduced rate. From here the
		// controller ramps up while the ping stays good and cuts when it
		// degrades.
		a.mode = autoSpeedModeControl
	case autoSpeedModeControl:
		if !trusted {
			// The idle reference was never seen: use the latency measured
			// during the trial as the loaded reference and treat a clear fall
			// as the new rest.
			high = high && !autoSpeedFellFrom(current, a.trialLoaded)
			if !high {
				a.rest, a.restAt = current, now
			}
		}
		if rateDown < autoSpeedIdleKiB && rateUp < autoSpeedIdleKiB {
			// Nothing is transferring: there is no queue to protect and no
			// reason to keep a stale cap for the next download.
			a.releaseLocked(now)
			break
		}
		// A direction that was idle when the trial started has no cap yet. If
		// it starts loading while the ping is still high, cap it now instead
		// of leaving it unlimited until the next trial: this is how a high
		// upload, which fills its narrower pipe, gets its own limit even when
		// the download was the direction that triggered the trial.
		if (high || !trusted) && loadDown && !a.down.active {
			a.downCap = max(int64(float64(rateDown)*autoSpeedTrialShare), autoSpeedMinFloorDownKiB)
			a.down.start(a.downCap)
		}
		if (high || !trusted) && loadUp && !a.up.active {
			a.upCap = max(int64(float64(rateUp)*autoSpeedTrialShare), autoSpeedMinFloorUpKiB)
			a.up.start(a.upCap)
		}
		ceilingDown := autoSpeedCeiling(downLimit, a.lineMaxDown)
		ceilingUp := autoSpeedCeiling(upLimit, a.lineMaxUp)
		a.downCap = a.down.step(now, high, rateDown, ceilingDown, autoSpeedMinFloorDownKiB, autoSpeedRiseDownKiB)
		a.upCap = a.up.step(now, high, rateUp, ceilingUp, max(floorUp, autoSpeedMinFloorUpKiB), autoSpeedRiseUpKiB)
		if !a.down.active && !a.up.active {
			a.releaseLocked(now)
		}
	}
	return a.downCap, a.upCap
}

// status is the controller state for the settings page.
func (a *autoSpeed) status(now time.Time) (mode autoSpeedMode, rtt, baseline time.Duration, downCap, upCap int64, have bool) {
	if a == nil {
		return autoSpeedModeFree, 0, 0, 0, 0, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.haveSample {
		return a.mode, 0, 0, a.downCap, a.upCap, false
	}
	return a.mode, a.current, a.baseline, a.downCap, a.upCap, true
}

// autoSpeedSettings reads the standalone settings. Managed mode (nil store) is
// always disabled.
func (d *Daemon) autoSpeedSettings() (enabled bool, target string, floorKiB, lineDown, lineUp int64) {
	if d.opts.Settings == nil {
		return false, "", autoSpeedDefaultMinKiB, 0, 0
	}
	store := d.opts.Settings
	enabled = strings.EqualFold(strings.TrimSpace(store.Get(autospeedEnabledKey, "false")), "true")
	target = strings.TrimSpace(store.Get(autospeedTargetKey, ""))
	floorKiB = int64FromSetting(store.Get(autospeedMinKey, strconv.Itoa(autoSpeedDefaultMinKiB)))
	if floorKiB < 1 {
		floorKiB = autoSpeedDefaultMinKiB
	}
	lineDown = max(int64FromSetting(store.Get(autospeedLineDownKey, "0")), 0)
	lineUp = max(int64FromSetting(store.Get(autospeedLineUpKey, "0")), 0)
	return enabled, target, floorKiB, lineDown, lineUp
}

// autoSpeedStatus renders the current latency and the effective caps for the
// settings page. Empty when the option is off; an em dash until the first
// measurement. 0 KiB/s is printed as ∞.
func (d *Daemon) autoSpeedStatus(now time.Time) string {
	enabled, _, _, _, _ := d.autoSpeedSettings()
	if !enabled || d.autoSpeed == nil {
		return ""
	}
	// Read the configured limits before the controller lock: the queue path
	// takes d.mu first and then the controller lock, and the other order would
	// deadlock.
	d.mu.Lock()
	configuredDown, configuredUp, _ := d.configuredSpeedLimits(now)
	d.mu.Unlock()
	_, rtt, _, downCap, upCap, have := d.autoSpeed.status(now)
	if !have {
		return "—"
	}
	down := effectiveSpeedLimit(configuredDown, downCap)
	up := effectiveSpeedLimit(configuredUp, upCap)
	return fmt.Sprintf("%d ms · ↓ %s · ↑ %s", rtt.Milliseconds(), autoSpeedCapText(down), autoSpeedCapText(up))
}

func autoSpeedCapText(kib int64) string {
	if kib <= 0 {
		return "∞"
	}
	return strconv.FormatInt(kib, 10) + " KiB/s"
}

// runAutoSpeed samples the latency while the option is enabled. It is started
// beside the queue loop and stops with it. The gateway is cached for a minute
// so probing does not resolve the default route every second.
func (d *Daemon) runAutoSpeed(stop <-chan struct{}) {
	ticker := time.NewTicker(autoSpeedProbeInterval)
	defer ticker.Stop()
	var gateway net.IP
	var gatewayAt time.Time
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		enabled, target, _, _, _ := d.autoSpeedSettings()
		if !enabled {
			d.autoSpeed.reset()
			gateway, gatewayAt = nil, time.Time{}
			continue
		}
		now := time.Now()
		if target == "" && (gateway == nil || now.Sub(gatewayAt) > time.Minute) {
			if resolved, err := defaultGateway(); err == nil {
				gateway, gatewayAt = resolved, now
			}
		}
		if rtt, ok := probeLatency(target, gateway); ok {
			d.autoSpeed.record(now, rtt)
		}
	}
}

// probeLatency measures the round-trip latency: the configured target, or
// Google/Cloudflare, with the default gateway as a last resort. A failed
// connect is not a measurement.
func probeLatency(target string, gateway net.IP) (time.Duration, bool) {
	if strings.TrimSpace(target) != "" {
		return autospeed.MeasureBest(autospeed.Candidates(target, nil))
	}
	if rtt, ok := autospeed.MeasureBest(autoSpeedTargets); ok {
		return rtt, true
	}
	return autospeed.Measure(latencyCandidates("", gateway))
}

// latencyCandidates lists the host:port endpoints to try for a configured
// target, or the gateway fallback.
func latencyCandidates(target string, gateway net.IP) []string {
	var fallback []string
	if gateway != nil {
		host := gateway.String()
		fallback = []string{
			net.JoinHostPort(host, "80"),
			net.JoinHostPort(host, "443"),
			net.JoinHostPort(host, "53"),
		}
	}
	return autospeed.Candidates(target, fallback)
}
