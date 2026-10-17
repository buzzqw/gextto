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

	"github.com/buzzqw/gextto/internal/autospeed"
)

const (
	autospeedEnabledKey = "autospeed-enabled"
	autospeedTargetKey  = "autospeed-target"
	autospeedMinKey     = "autospeed-min"

	// autoSpeedDefaultMinKiB is the lowest upload the controller may settle on.
	autoSpeedDefaultMinKiB = 50

	// autoSpeedProbeInterval is how often the latency is sampled.
	autoSpeedProbeInterval = 1 * time.Second
	// autoSpeedWindow is how far back the idle baseline is remembered.
	autoSpeedWindow = 60 * time.Second
)

// autoSpeed is the standalone controller: the shared latency probe and the
// one upload cap it moves (internal/autospeed). The zero value is ready to use.
type autoSpeed struct {
	mu    sync.Mutex
	probe autospeed.Probe
	clamp autospeed.Clamp
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

// reset forgets the samples and the settled ceiling (on disable).
func (a *autoSpeed) reset() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.probeLocked().Reset()
	a.clamp.Reset()
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
		a.clamp.Value = 0
		return ceilingKiB, false
	}
	_, _, high, ok := a.probeLocked().State(now, 1)
	if !ok {
		return ceilingKiB, false
	}
	return a.clamp.Step(now, high, ceilingKiB, floorKiB), true
}

// snapshot reports the last latency, the idle baseline and the settled ceiling
// for the settings page. have is false until a sample exists.
func (a *autoSpeed) snapshot(now time.Time) (rtt, baseline time.Duration, clampKiB int64, have bool) {
	if a == nil {
		return 0, 0, 0, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	rtt, baseline, _, have = a.probeLocked().State(now, 1)
	if !have {
		return 0, 0, 0, false
	}
	return rtt, baseline, a.clamp.Value, true
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
	return autospeed.Measure(latencyCandidates(target, gateway))
}

// latencyCandidates lists the host:port endpoints to try, in order.
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
