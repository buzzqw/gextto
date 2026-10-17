package autospeed

import (
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
