package gextto

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// guardRun feeds the guard one sample per probe interval with a latency that
// may depend on the caps in force (a line model), and returns the caps after
// the last step.
type guardRun struct {
	g    *latencyGuard
	now  time.Time
	line lineGuess
}

func newGuardRun() *guardRun {
	return &guardRun{g: newLatencyGuard(), now: time.Unix(1_800_000_000, 0)}
}

// step advances one probe interval. latency gets the caps in force before the
// step (0 = none) and returns the latency to record.
func (r *guardRun) step(latency func(downCap int64) time.Duration, downKiB int64) (int64, int64) {
	r.now = r.now.Add(latencyGuardProbeEvery)
	downCap, _ := r.g.caps()
	rate := downKiB
	if downCap > 0 && downCap < rate {
		rate = downCap
	}
	r.g.probe.Record(r.now, latency(downCap))
	d, u, _ := r.g.update(r.now, true, r.line, uint64(rate*1024), 10*1024)
	return d, u
}

// ownQueue is a line whose queue grows with Gextto's own rate: 60 ms uncapped,
// 20 ms as soon as the controller backs off.
func ownQueue(downCap int64) time.Duration {
	if downCap > 0 {
		return 20 * time.Millisecond
	}
	return 60 * time.Millisecond
}

// othersQueue is a line someone else keeps full: 60 ms whatever Gextto does,
// until others leave (then 20 ms).
func othersQueue(leftAt *bool) func(int64) time.Duration {
	return func(int64) time.Duration {
		if *leftAt {
			return 20 * time.Millisecond
		}
		return 60 * time.Millisecond
	}
}

// TestLatencyGuardControlsItsOwnQueue: with no router information at all, a
// high ping is enough to cap; the controller then keeps the cap while the ping
// is high and ramps up when it is good. The old guard only capped for others.
func TestLatencyGuardControlsItsOwnQueue(t *testing.T) {
	r := newGuardRun()
	controlled := false
	for i := 0; i < 30; i++ {
		if d, _ := r.step(ownQueue, 9000); d > 0 {
			controlled = true
		}
	}
	if !controlled {
		t.Fatal("the guard never capped its own queue despite the high ping")
	}
}

// TestLatencyGuardRampsUpWhileThePingIsGood: starting from a low cap the
// controller climbs, and it cuts when the ping degrades. This is the behaviour
// that replaces the old 64 KiB/s sawtooth.
func TestLatencyGuardRampsUpWhileThePingIsGood(t *testing.T) {
	r := newGuardRun()
	// The line is good up to 4000 KiB/s and high above it.
	line := func(downCap int64) time.Duration {
		if downCap > 4000 {
			return 60 * time.Millisecond
		}
		return 20 * time.Millisecond
	}
	for i := 0; i < 8; i++ { // trial and reference
		r.step(line, 9000)
	}
	peak := int64(0)
	for i := 0; i < 60; i++ {
		d, _ := r.step(line, 9000)
		peak = max(peak, d)
	}
	if peak < 3000 {
		t.Fatalf("the cap never ramped toward the line: peak %d KiB/s", peak)
	}
	if st := r.g.status(); st.LatencyMs > 40 {
		t.Fatalf("the controller settled with a bad ping: %+v", st)
	}
}

// TestLatencyGuardYieldsToOthersAndReleases: with the router source, someone
// else keeps the queue full while Gextto backs off, so the guard yields; once
// the others leave the caps climb and are lifted.
func TestLatencyGuardYieldsToOthersAndReleases(t *testing.T) {
	r := newGuardRun()
	r.g.routerOnly = true
	r.line = lineGuess{known: true, others: true}
	r.g.rest, r.g.restAt = 20*time.Millisecond, r.now
	r.g.probe.Record(r.now, 20*time.Millisecond)
	left := false
	queue := othersQueue(&left)
	var d int64
	for i := 0; i < 8; i++ {
		d, _ = r.step(queue, 9000)
	}
	if st := r.g.status(); st.Mode != "control" || d == 0 {
		t.Fatalf("after the trial: %+v cap %d, want control with a cap", st, d)
	}
	first := d
	for i := 0; i < 6; i++ {
		d, _ = r.step(queue, 9000)
	}
	if d >= first {
		t.Fatalf("the cap did not go down while the others kept the line full: %d → %d", first, d)
	}
	left = true
	r.line.others = false
	for i := 0; i < 400 && d != 0; i++ {
		d, _ = r.step(queue, 9000)
	}
	if d != 0 || r.g.status().Mode != "free" {
		t.Fatalf("not released after the others left: cap %d mode %s", d, r.g.status().Mode)
	}
}

// TestLatencyGuardSourceModes: with "ping"/"both" the latency decides even
// when the router sees nobody; with "router" a high ping and no other traffic
// caps nothing, and another device does. This is the option the user asked
// for: Google/Cloudflare ping alongside, or instead of, the router.
func TestLatencyGuardSourceModes(t *testing.T) {
	high := func(int64) time.Duration { return 60 * time.Millisecond }
	for _, tc := range []struct {
		name       string
		routerOnly bool
		others     bool
		wantCapped bool
	}{
		{"both/no others", false, false, true},
		{"ping/no others", false, false, true},
		{"router/no others", true, false, false},
		{"router/others", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newGuardRun()
			r.g.routerOnly = tc.routerOnly
			r.line = lineGuess{known: true, others: tc.others}
			r.g.rest, r.g.restAt = 20*time.Millisecond, r.now
			r.g.probe.Record(r.now, 20*time.Millisecond)
			d, _ := r.step(high, 9000)
			if capped := d > 0; capped != tc.wantCapped {
				t.Fatalf("cap %d, want capped=%v", d, tc.wantCapped)
			}
		})
	}
}

// TestLatencyGuardIgnoresAnIdleLineAndNonAuto: Gextto barely transferring is
// never capped, and leaving Auto drops every cap at once.
func TestLatencyGuardIgnoresAnIdleLineAndNonAuto(t *testing.T) {
	r := newGuardRun()
	for i := 0; i < 10; i++ {
		if d, u := r.step(ownQueue, 100); d != 0 || u != 0 {
			t.Fatalf("capped an idle line: %d/%d", d, u)
		}
	}
	for i := 0; i < 3; i++ {
		r.step(ownQueue, 9000)
	}
	if d, _ := r.g.caps(); d == 0 {
		t.Fatal("expected the trial cap before leaving Auto")
	}
	if d, u, changed := r.g.update(r.now, false, lineGuess{}, 0, 0); d != 0 || u != 0 || !changed {
		t.Fatalf("leaving Auto: %d/%d changed=%v, want 0/0 true", d, u, changed)
	}
}

// TestLatencyGuardFirstCapLogIsAdoptable: the guard exposes the first-cap line
// it wrote (time and caps) so the periodic speed logger can adopt it, and
// leaving Auto clears it: a later session must not adopt a stale line.
func TestLatencyGuardFirstCapLogIsAdoptable(t *testing.T) {
	var nilGuard *latencyGuard
	if at, _, _ := nilGuard.firstCapLog(); !at.IsZero() {
		t.Fatal("a missing guard reported a first-cap line")
	}
	g := newLatencyGuard()
	if at, _, _ := g.firstCapLog(); !at.IsZero() {
		t.Fatal("a fresh guard reported a first-cap line")
	}
	at := time.Unix(1_800_000_000, 0)
	g.mu.Lock()
	g.capLoggedAt, g.capLoggedDown, g.capLoggedUp = at, 1932, 0
	g.mu.Unlock()
	gotAt, down, up := g.firstCapLog()
	if !gotAt.Equal(at) || down != 1932 || up != 0 {
		t.Fatalf("first cap line = %v %d/%d, want %v 1932/0", gotAt, down, up, at)
	}
	g.update(at, false, lineGuess{}, 0, 0)
	if gotAt, _, _ := g.firstCapLog(); !gotAt.IsZero() {
		t.Fatalf("leaving Auto kept the first-cap line: %v", gotAt)
	}
}

// TestEffectiveSpeedLimitsUsesTheGuardOnlyOnAuto pins that the guard's caps
// replace "unlimited" only for an Auto source with the option on.
func TestEffectiveSpeedLimitsUsesTheGuardOnlyOnAuto(t *testing.T) {
	g := newLatencyGuard()
	g.downCap, g.upCap = 4000, 0
	auto := &Config{Settings: map[string]string{"torrent_speed_auto": "true"}}
	if down, up, source, _ := effectiveSpeedLimits(auto, g); down != 4000 || up != 0 || source != speedSourceAuto {
		t.Fatalf("auto: %d/%d %s, want 4000/0 auto", down, up, source)
	}
	auto.Settings[latencyGuardKey] = "false"
	if down, _, _, _ := effectiveSpeedLimits(auto, g); down != 0 {
		t.Fatalf("guard off: download %d, want unlimited", down)
	}
	manual := &Config{Settings: map[string]string{}}
	manual.Transfer.DownloadLimitKib = 3000
	if down, _, source, _ := effectiveSpeedLimits(manual, g); down != 3000 || source != speedSourceBase {
		t.Fatalf("manual: %d %s, want 3000 base", down, source)
	}
}

// TestGuardDirectionRampsAndCuts pins the direction controller: it climbs
// from a low cap while the ping is good and cuts when it is high.
func TestGuardDirectionRampsAndCuts(t *testing.T) {
	var d guardDirection
	d.start(128)
	now := time.Unix(1_800_000_000, 0)
	for i := 0; i < 20; i++ {
		now = now.Add(3 * time.Second)
		d.step(now, false, 9000, latencyGuardMaxDownKiB, latencyGuardMinFloorDownKiB, latencyGuardRiseDownKiB)
	}
	ramped := d.clamp.Value
	if ramped <= 128 {
		t.Fatalf("the cap did not ramp up: %d", ramped)
	}
	now = now.Add(3 * time.Second)
	cut := d.step(now, true, 90000, latencyGuardMaxDownKiB, latencyGuardMinFloorDownKiB, latencyGuardRiseDownKiB)
	if cut >= ramped {
		t.Fatalf("the cap did not cut on a high ping: %d → %d", ramped, cut)
	}
}

// TestLatencyGuardHasNoHardCap: 0 is the "no hard cap" sentinel, so even a
// multi-gigabit value is passed through instead of being folded to unlimited.
func TestLatencyGuardHasNoHardCap(t *testing.T) {
	g := newLatencyGuard()
	g.downCap, g.upCap = 2_000_000, 2_000_000
	if d, u := g.caps(); d != 2_000_000 || u != 2_000_000 {
		t.Fatalf("caps reported as %d/%d, want the values unchanged (no hard cap)", d, u)
	}
	if st := g.status(); !st.Active {
		t.Fatalf("a real cap reported as inactive: %+v", st)
	}
}

// TestV2DownloadsShowsTheLatencyGuard pins the badge: while the guard caps the
// transfers the downloads page says so, with the latency and the caps.
func TestV2DownloadsShowsTheLatencyGuard(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	state.latency_guard.mu.Lock()
	state.latency_guard.downCap, state.latency_guard.current = 4200, 61*time.Millisecond
	state.latency_guard.rest, state.latency_guard.restAt = 20*time.Millisecond, time.Now()
	state.latency_guard.mu.Unlock()
	resp, err := http.Get(server.URL + "/?view=downloads")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("downloads page -> %d", resp.StatusCode)
	}
	for _, want := range []string{`role="status"`, "61/20 ms", "DL 4200 KiB/s"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("downloads page lacks %q", want)
		}
	}
}
