package autospeed

import (
	"net"
	"testing"
	"time"
)

// TestProbeStateMedianBaselineAndWindow pins the probe: the baseline is the
// lowest latency in the window, the current latency the median of the recent
// samples, and samples older than the window are forgotten.
func TestProbeStateMedianBaselineAndWindow(t *testing.T) {
	p := &Probe{Window: time.Minute}
	now := time.Unix(1_800_000_000, 0)
	if _, _, _, ok := p.State(now, 3); ok {
		t.Fatal("no samples must not be a state")
	}
	p.Record(now, 20*time.Millisecond)
	p.Record(now.Add(time.Second), 70*time.Millisecond)
	p.Record(now.Add(2*time.Second), 25*time.Millisecond)
	p.Record(now.Add(3*time.Second), 80*time.Millisecond)
	current, baseline, high, ok := p.State(now.Add(3*time.Second), 3)
	if !ok || baseline != 20*time.Millisecond || current != 70*time.Millisecond || !high {
		t.Fatalf("state = %v %v high=%v ok=%v, want 70ms 20ms high", current, baseline, high, ok)
	}
	if current, _, _, _ := p.State(now.Add(3*time.Second), 1); current != 80*time.Millisecond {
		t.Fatalf("recent=1 current = %v, want the latest 80ms", current)
	}
	// Past the window only the later samples count for the baseline.
	if _, baseline, _, _ := p.State(now.Add(time.Minute+1500*time.Millisecond), 1); baseline != 25*time.Millisecond {
		t.Fatalf("baseline after the window = %v, want 25ms", baseline)
	}
}

// TestClampStepsDownAndUpWithinBounds pins the cap moves: x0.85 when high, not
// below the floor; +ceiling/20 at rest, not above the ceiling; one move per
// StepInterval.
func TestClampStepsDownAndUpWithinBounds(t *testing.T) {
	var c Clamp
	now := time.Unix(1_800_000_000, 0)
	if v := c.Step(now, true, 1000, 50); v != 850 {
		t.Fatalf("first high step = %d, want 850", v)
	}
	if v := c.Step(now.Add(time.Second), true, 1000, 50); v != 850 {
		t.Fatalf("within the step interval = %d, want 850 unchanged", v)
	}
	if v := c.Step(now.Add(StepInterval), false, 1000, 50); v != 900 {
		t.Fatalf("rest step = %d, want 900", v)
	}
	if v := c.Step(now.Add(2*StepInterval), false, 920, 50); v != 920 {
		t.Fatalf("a shrunk ceiling = %d, want 920", v)
	}
	c.Reset()
	if v := c.Step(now, true, 1000, 900); v != 900 {
		t.Fatalf("floor = %d, want 900", v)
	}
}

// TestClampStepRampFindsTheLine: the ramp grows from a low cap by at least
// riseKiB, and by a quarter of the current value once that is larger, instead
// of the ceiling/20 step (tiny when the ceiling is the current rate).
func TestClampStepRampFindsTheLine(t *testing.T) {
	var c Clamp
	c.Value = 128
	now := time.Unix(1_800_000_000, 0)
	if v := c.StepRamp(now, false, 100000, 50, 512); v != 640 {
		t.Fatalf("first ramp step = %d, want 640", v)
	}
	now = now.Add(StepInterval)
	if v := c.StepRamp(now, false, 100000, 50, 512); v != 1152 {
		t.Fatalf("second ramp step = %d, want 1152", v)
	}
	now = now.Add(StepInterval)
	if v := c.StepRamp(now, true, 100000, 50, 512); v != 979 {
		t.Fatalf("high step = %d, want 979 (15%% cut of 1152)", v)
	}
}

// TestClampStepRampWithoutCeiling: ceilingKiB <= 0 is "no hard cap"; the cap
// still grows and the high-latency cut still works.
func TestClampStepRampWithoutCeiling(t *testing.T) {
	var c Clamp
	c.Value = 1000
	now := time.Unix(1_800_000_000, 0)
	if v := c.StepRamp(now, false, 0, 50, 512); v != 1512 {
		t.Fatalf("unbounded ramp step = %d, want 1512", v)
	}
	now = now.Add(StepInterval)
	if v := c.StepRamp(now, true, 0, 50, 512); v != 1285 {
		t.Fatalf("unbounded high step = %d, want 1285 (15%% cut of 1512)", v)
	}
}

// TestMeasureBestSkipsAFailingTarget: one unreachable target must not hide the
// answer of another; the guard uses several Google/Cloudflare addresses.
func TestMeasureBestSkipsAFailingTarget(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback listener")
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	rtt, ok := MeasureBest([]string{"127.0.0.1:1", ln.Addr().String()})
	if !ok || rtt <= 0 {
		t.Fatalf("MeasureBest = %v, %v; want a measurement from the second target", rtt, ok)
	}
}
