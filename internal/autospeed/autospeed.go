// Package autospeed is the latency auto-speed controller (anti-bufferbloat),
// shared by the standalone gx-torrent daemon and by Gextto. A Probe keeps the
// recent round-trip latencies to a target; when the latest ones rise above the
// idle baseline, a Clamp lowers a rate cap, and it raises the cap again when
// the latency is back at rest. The configured (or learned) ceiling is never
// exceeded.
package autospeed

import (
	"net"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	// ProbeTimeout bounds a single latency measurement.
	ProbeTimeout = 1 * time.Second
	// StepInterval is the minimum time between two moves of a Clamp.
	StepInterval = 3 * time.Second
	// HighFactor and HighMargin decide when the line looks saturated: the
	// latency is "high" only when it is above both the baseline times the
	// factor and the baseline plus the margin.
	HighFactor = 1.5
	HighMargin = 20 * time.Millisecond
	// reduceNum/reduceDen scale a cap down on a spike (x0.85).
	reduceNum = 85
	reduceDen = 100
	// riseDivisor is the additive step on recovery: ceiling/riseDivisor.
	riseDivisor = 20
)

type sample struct {
	at  time.Time
	rtt time.Duration
}

// Probe keeps the latency samples of the last Window. The zero value with a
// Window set is ready to use; it is safe for concurrent use.
type Probe struct {
	// Window is how far back the idle baseline is remembered.
	Window time.Duration

	mu      sync.Mutex
	samples []sample
}

// Record adds a measured latency. A non-positive value is ignored.
func (p *Probe) Record(now time.Time, rtt time.Duration) {
	if p == nil || rtt <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.samples = append(p.samples, sample{at: now, rtt: rtt})
	p.trimLocked(now)
}

// Reset forgets every sample.
func (p *Probe) Reset() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.samples = nil
	p.mu.Unlock()
}

func (p *Probe) trimLocked(now time.Time) {
	cut := now.Add(-p.Window)
	i := 0
	for i < len(p.samples) && p.samples[i].at.Before(cut) {
		i++
	}
	if i > 0 {
		p.samples = p.samples[i:]
	}
}

// State reports the current latency (the median of the last recent samples,
// at least one), the idle baseline (the lowest latency in the window) and
// whether the line looks saturated. ok is false without samples.
func (p *Probe) State(now time.Time, recent int) (current, baseline time.Duration, high, ok bool) {
	if p == nil {
		return 0, 0, false, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.trimLocked(now)
	if len(p.samples) == 0 {
		return 0, 0, false, false
	}
	baseline = p.samples[0].rtt
	for _, s := range p.samples[1:] {
		baseline = min(baseline, s.rtt)
	}
	recent = max(recent, 1)
	tail := p.samples[max(len(p.samples)-recent, 0):]
	values := make([]time.Duration, len(tail))
	for i, s := range tail {
		values[i] = s.rtt
	}
	slices.Sort(values)
	current = values[len(values)/2]
	high = current > time.Duration(float64(baseline)*HighFactor) && current > baseline+HighMargin
	return current, baseline, high, true
}

// Clamp is one rate cap the controller moves, in KiB/s. The zero value is
// ready to use; it is not safe for concurrent use.
type Clamp struct {
	// Value is the cap settled on; 0 means none.
	Value    int64
	lastStep time.Time
}

// Reset forgets the settled cap.
func (c *Clamp) Reset() {
	*c = Clamp{}
}

// Step moves the cap at most once per StepInterval: down by 15 % (not below
// floorKiB) when high, up by ceilingKiB/20 (not above ceilingKiB) otherwise.
// Without a settled value, or with one above a shrunk ceiling, it starts
// from the ceiling. It returns the cap to apply.
func (c *Clamp) Step(now time.Time, high bool, ceilingKiB, floorKiB int64) int64 {
	if floorKiB < 1 {
		floorKiB = 1
	}
	if c.Value <= 0 || c.Value > ceilingKiB {
		c.Value = ceilingKiB
	}
	if now.Sub(c.lastStep) >= StepInterval {
		c.lastStep = now
		if high {
			reduced := max(c.Value*reduceNum/reduceDen, floorKiB)
			c.Value = min(c.Value, reduced)
		} else if c.Value < ceilingKiB {
			c.Value = min(c.Value+max(ceilingKiB/riseDivisor, 1), ceilingKiB)
		}
	}
	c.Value = min(c.Value, ceilingKiB)
	if c.Value < 1 {
		c.Value = ceilingKiB
	}
	return c.Value
}

// Measure times a TCP connect to the first answering candidate. A failed
// connect is not a measurement.
func Measure(candidates []string) (time.Duration, bool) {
	for _, address := range candidates {
		start := time.Now()
		conn, err := net.DialTimeout("tcp", address, ProbeTimeout)
		if err != nil {
			continue
		}
		rtt := time.Since(start)
		_ = conn.Close()
		return rtt, true
	}
	return 0, false
}

// Candidates lists the host:port endpoints to probe: the configured target
// (a bare host gets ports 80 and 443), or else the fallback list as given.
func Candidates(target string, fallback []string) []string {
	target = strings.TrimSpace(target)
	if target != "" {
		if _, _, err := net.SplitHostPort(target); err == nil {
			return []string{target}
		}
		return []string{net.JoinHostPort(target, "80"), net.JoinHostPort(target, "443")}
	}
	return fallback
}
