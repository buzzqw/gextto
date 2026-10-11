package main

// autospeed.go is the latency auto-speed controller (anti-bufferbloat), like
// BiglyBT's Ping Auto-Speed. A small probe measures the round-trip latency to
// the default gateway (or to a configured host); when the latency rises
// because the upload saturates the last-mile queue, the controller lowers the
// effective upload below the configured limit and raises it again when the
// line is idle. It only ever lowers the upload: the configured (or scheduled)
// limit stays the ceiling.
//
// The option lives in settings.json (standalone mode, like the bandwidth
// scheduler); managed mode is driven by Gextto and does not read it.

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	autospeedEnabledKey = "autospeed-enabled"
	autospeedTargetKey  = "autospeed-target"
	autospeedMinKey     = "autospeed-min"

	// autoSpeedDefaultMinKiB is the lowest upload the controller may settle on.
	autoSpeedDefaultMinKiB = 50

	// autoSpeedProbeInterval is how often the latency is sampled.
	autoSpeedProbeInterval = 1 * time.Second
	// autoSpeedProbeTimeout bounds a single measurement.
	autoSpeedProbeTimeout = 1 * time.Second
	// autoSpeedWindow is how far back the idle baseline is remembered.
	autoSpeedWindow = 60 * time.Second
	// autoSpeedStepInterval is the minimum time between two controller moves.
	autoSpeedStepInterval = 3 * time.Second
	// autoSpeedHighFactor and autoSpeedHighMargin decide when the line looks
	// saturated: the current latency is "high" only when it is above both the
	// idle baseline times the factor and the baseline plus the margin.
	autoSpeedHighFactor = 1.5
	autoSpeedHighMargin = 20 * time.Millisecond
	// autoSpeedReduceNum/Den scale the upload down on a spike (x0.85).
	autoSpeedReduceNum = 85
	autoSpeedReduceDen = 100
	// autoSpeedRiseDivisor is the additive step on recovery: ceiling/divisor.
	autoSpeedRiseDivisor = 20
)

// autoSpeedSample is one measured round-trip latency.
type autoSpeedSample struct {
	at  time.Time
	rtt time.Duration
}

// autoSpeed keeps the recent latency samples and the current upload ceiling
// the controller has settled on. The zero value is ready to use.
type autoSpeed struct {
	mu       sync.Mutex
	samples  []autoSpeedSample
	clampKiB int64 // current upload ceiling in KiB/s; 0 = none settled
	lastStep time.Time
}

// record adds a measured latency. A non-positive value is ignored.
func (a *autoSpeed) record(now time.Time, rtt time.Duration) {
	if a == nil || rtt <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.samples = append(a.samples, autoSpeedSample{at: now, rtt: rtt})
	a.trimLocked(now)
}

// reset forgets the samples and the settled ceiling (on disable).
func (a *autoSpeed) reset() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.samples = nil
	a.clampKiB = 0
	a.lastStep = time.Time{}
}

// trimLocked drops samples older than the window.
func (a *autoSpeed) trimLocked(now time.Time) {
	cut := now.Add(-autoSpeedWindow)
	i := 0
	for i < len(a.samples) && a.samples[i].at.Before(cut) {
		i++
	}
	if i > 0 {
		a.samples = a.samples[i:]
	}
}

// baselineLocked is the lowest latency in the window, the idle reference.
func (a *autoSpeed) baselineLocked() time.Duration {
	baseline := a.samples[0].rtt
	for _, s := range a.samples[1:] {
		if s.rtt < baseline {
			baseline = s.rtt
		}
	}
	return baseline
}

// clampUpload returns the upload limit (KiB/s) to apply now and whether the
// controller is actually acting. A ceiling <= 0 (unlimited) or no measurement
// means "no clamp": the ceiling is returned unchanged and acting is false.
// floorKiB is the lowest value the controller may settle on.
func (a *autoSpeed) clampUpload(now time.Time, ceilingKiB, floorKiB int64) (int64, bool) {
	if a == nil {
		return ceilingKiB, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if ceilingKiB <= 0 {
		a.clampKiB = 0
		return ceilingKiB, false
	}
	if floorKiB < 1 {
		floorKiB = 1
	}
	a.trimLocked(now)
	if len(a.samples) == 0 {
		return ceilingKiB, false
	}
	current := a.samples[len(a.samples)-1].rtt
	baseline := a.baselineLocked()
	// First decision, or a ceiling that shrank: start from the ceiling.
	if a.clampKiB <= 0 || a.clampKiB > ceilingKiB {
		a.clampKiB = ceilingKiB
	}
	if now.Sub(a.lastStep) >= autoSpeedStepInterval {
		a.lastStep = now
		high := current > time.Duration(float64(baseline)*autoSpeedHighFactor) &&
			current > baseline+autoSpeedHighMargin
		if high {
			reduced := a.clampKiB * autoSpeedReduceNum / autoSpeedReduceDen
			if reduced < floorKiB {
				reduced = floorKiB
			}
			if reduced < a.clampKiB {
				a.clampKiB = reduced
			}
		} else if a.clampKiB < ceilingKiB {
			step := ceilingKiB / autoSpeedRiseDivisor
			if step < 1 {
				step = 1
			}
			a.clampKiB += step
			if a.clampKiB > ceilingKiB {
				a.clampKiB = ceilingKiB
			}
		}
	}
	if a.clampKiB > ceilingKiB {
		a.clampKiB = ceilingKiB
	}
	if a.clampKiB < 1 {
		a.clampKiB = ceilingKiB
	}
	return a.clampKiB, true
}

// snapshot reports the last latency, the idle baseline and the settled ceiling
// for the settings page. have is false until a sample exists.
func (a *autoSpeed) snapshot(now time.Time) (rtt, baseline time.Duration, clampKiB int64, have bool) {
	if a == nil {
		return 0, 0, 0, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.trimLocked(now)
	if len(a.samples) == 0 {
		return 0, 0, 0, false
	}
	rtt = a.samples[len(a.samples)-1].rtt
	return rtt, a.baselineLocked(), a.clampKiB, true
}

// autoSpeedSettings reads the standalone settings. Managed mode (nil store) is
// always disabled.
func (d *Daemon) autoSpeedSettings() (enabled bool, target string, floorKiB int64) {
	if d.opts.Settings == nil {
		return false, "", autoSpeedDefaultMinKiB
	}
	store := d.opts.Settings
	enabled = strings.EqualFold(strings.TrimSpace(store.Get(autospeedEnabledKey, "false")), "true")
	target = strings.TrimSpace(store.Get(autospeedTargetKey, ""))
	floorKiB = int64FromSetting(store.Get(autospeedMinKey, strconv.Itoa(autoSpeedDefaultMinKiB)))
	if floorKiB < 1 {
		floorKiB = autoSpeedDefaultMinKiB
	}
	return enabled, target, floorKiB
}

// autoSpeedStatus renders the current latency and the settled upload ceiling
// for the settings page. Empty when the option is off; an em dash until the
// first measurement.
func (d *Daemon) autoSpeedStatus(now time.Time) string {
	enabled, _, _ := d.autoSpeedSettings()
	if !enabled {
		return ""
	}
	rtt, _, clampKiB, have := d.autoSpeed.snapshot(now)
	if !have {
		return "—"
	}
	capText := "—"
	if clampKiB > 0 {
		capText = strconv.FormatInt(clampKiB, 10) + " KiB/s"
	}
	return fmt.Sprintf("%d ms / %s", rtt.Milliseconds(), capText)
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
		enabled, target, _ := d.autoSpeedSettings()
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

// probeLatency measures the round-trip latency with a TCP connect to the
// configured target (host or host:port) or, when empty, to the default
// gateway on a few common ports. A failed connect is not a measurement.
func probeLatency(target string, gateway net.IP) (time.Duration, bool) {
	for _, address := range latencyCandidates(target, gateway) {
		start := time.Now()
		conn, err := net.DialTimeout("tcp", address, autoSpeedProbeTimeout)
		if err != nil {
			continue
		}
		rtt := time.Since(start)
		_ = conn.Close()
		return rtt, true
	}
	return 0, false
}

// latencyCandidates lists the host:port endpoints to try, in order.
func latencyCandidates(target string, gateway net.IP) []string {
	target = strings.TrimSpace(target)
	if target != "" {
		if _, _, err := net.SplitHostPort(target); err == nil {
			return []string{target}
		}
		return []string{net.JoinHostPort(target, "80"), net.JoinHostPort(target, "443")}
	}
	if gateway == nil {
		return nil
	}
	host := gateway.String()
	return []string{
		net.JoinHostPort(host, "80"),
		net.JoinHostPort(host, "443"),
		net.JoinHostPort(host, "53"),
	}
}
