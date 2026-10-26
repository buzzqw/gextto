package gextto

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
	// The line is good up to 4000 KiB/s and high above it (and uncapped).
	line := func(downCap int64) time.Duration {
		if downCap == 0 || downCap > 4000 {
			return 60 * time.Millisecond
		}
		return 20 * time.Millisecond
	}
	// The line was seen at rest before the download started: without any
	// idle sample a flat 60 ms is just how the line is, not a queue.
	r.g.probe.Record(r.now, 20*time.Millisecond)
	for i := 0; i < 8; i++ { // trial and reference
		r.step(line, 9000)
	}
	peak := int64(0)
	bad := 0
	for i := 0; i < 60; i++ {
		d, _ := r.step(line, 9000)
		peak = max(peak, d)
		// Around the knee the cap probes above it and is cut back, so a
		// single instant can be bad: what counts is where it spends its time.
		if i >= 30 && r.g.status().LatencyMs > 40 {
			bad++
		}
	}
	if peak < 3000 {
		t.Fatalf("the cap never ramped toward the line: peak %d KiB/s", peak)
	}
	if bad >= 8 {
		t.Fatalf("the controller settled with a bad ping: %d of the last 30 steps: %+v", bad, r.g.status())
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
	if down, up, source := effectiveSpeedLimits(auto, g); down != 4000 || up != 0 || source != speedSourceAuto {
		t.Fatalf("auto: %d/%d %s, want 4000/0 auto", down, up, source)
	}
	auto.Settings[latencyGuardKey] = "false"
	if down, _, _ := effectiveSpeedLimits(auto, g); down != 0 {
		t.Fatalf("guard off: download %d, want unlimited", down)
	}
	manual := &Config{Settings: map[string]string{}}
	manual.Transfer.DownloadLimitKib = 3000
	if down, _, source := effectiveSpeedLimits(manual, g); down != 3000 || source != speedSourceBase {
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

// TestGuardUploadThresholdFollowsTheUplink guards the upload rule: the
// threshold is a tenth of the uplink, between 32 and 256 KiB/s, so a loaded
// upload is capped even when the download moves more bytes.
func TestGuardUploadThresholdFollowsTheUplink(t *testing.T) {
	cases := []struct{ lineMax, want int64 }{
		{0, latencyGuardMinLoadUpKiB},
		{100, 32},
		{800, 80},
		{9000, latencyGuardMinLoadKiB},
	}
	for _, test := range cases {
		if got := guardUploadLoadThreshold(test.lineMax); got != test.want {
			t.Errorf("line %d KiB/s: threshold %d, want %d", test.lineMax, got, test.want)
		}
	}
}

// TestLatencyGuardCapsAnUploadThatStartsLater: the trial begins while the
// upload is idle, so its cap stays off; when the upload becomes loaded during
// the control phase, the guard must cap it without waiting for a new trial.
func TestLatencyGuardCapsAnUploadThatStartsLater(t *testing.T) {
	r := newGuardRun()
	r.g.lineMaxUp = 800
	line := func(downCap int64) time.Duration {
		if downCap > 0 {
			return 20 * time.Millisecond
		}
		return 60 * time.Millisecond
	}
	for i := 0; i < 10; i++ {
		r.step(line, 2000) // the helper sends 10 KiB/s upload: below the threshold
	}
	for i := 0; i < 6; i++ {
		r.now = r.now.Add(latencyGuardProbeEvery)
		r.g.probe.Record(r.now, 60*time.Millisecond)
		downCap, _ := r.g.caps()
		rateDown := int64(2000)
		if downCap > 0 && downCap < rateDown {
			rateDown = downCap
		}
		if _, upCap, _ := r.g.update(r.now, true, r.line, uint64(rateDown*1024), 200*1024); upCap > 0 {
			return
		}
	}
	t.Fatal("the guard never capped an upload that became loaded during control")
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

// TestLatencyGuardDoesNotThrottleAGoodLine: with no fresh idle reference the
// guard runs a trial even though the ping is good at full speed. The trial
// cannot make the latency fall (there is no queue to drain), and that must not
// be read as "still high": the line goes back to Gextto instead of being cut
// to the floor and held there while the ping stays at rest.
func TestLatencyGuardDoesNotThrottleAGoodLine(t *testing.T) {
	r := newGuardRun()
	good := func(int64) time.Duration { return 15 * time.Millisecond }
	minCap := int64(-1)
	for i := 0; i < 150; i++ {
		if d, _ := r.step(good, 5000); d > 0 && (minCap < 0 || d < minCap) {
			minCap = d
		}
	}
	if d, _ := r.g.caps(); d != 0 {
		t.Fatalf("a good line is still capped at %d KiB/s (lowest %d): %+v", d, minCap, r.g.status())
	}
	if minCap >= 0 && minCap < 2000 {
		t.Fatalf("a good line was cut to %d KiB/s", minCap)
	}
}

// TestLatencyGuardReleasesWhenOnlyTheDHTIsTalking: under a cap, the DHT's
// background traffic is not a transfer and must not keep the cap alive.
func TestLatencyGuardReleasesWhenOnlyTheDHTIsTalking(t *testing.T) {
	r := newGuardRun()
	for i := 0; i < 12; i++ {
		r.step(ownQueue, 9000)
	}
	if d, _ := r.g.caps(); d == 0 {
		t.Fatal("setup: the guard did not cap its own queue")
	}
	r.now = r.now.Add(latencyGuardProbeEvery)
	r.g.probe.Record(r.now, 20*time.Millisecond)
	if d, u, _ := r.g.update(r.now, true, r.line, 3*1024, 2*1024); d != 0 || u != 0 {
		t.Fatalf("DHT chatter kept the caps at %d/%d KiB/s", d, u)
	}
}

// TestLatencyGuardSitsBelowTheKnee: on a line that queues above its capacity
// the cap settles just under it instead of climbing back over it after every
// cut. Over ten minutes the ping is bad for a small share of the time and the
// line is still used almost fully. GX_LOG_GUARD_KNEE=1 prints the figures
// recorded in docs/measurements.md.
func TestLatencyGuardSitsBelowTheKnee(t *testing.T) {
	for _, capacity := range []int64{800, 4000, 20000} {
		r := newGuardRun()
		line := func(downCap int64) time.Duration {
			if downCap == 0 || downCap > capacity {
				return 60 * time.Millisecond
			}
			return 20 * time.Millisecond
		}
		r.g.probe.Record(r.now, 20*time.Millisecond)
		bad, used := 0, int64(0)
		const warmup, steps = 30, 300
		for i := 0; i < warmup+steps; i++ {
			d, _ := r.step(line, 30000)
			if i < warmup {
				continue
			}
			if r.g.status().LatencyMs > 40 {
				bad++
			}
			if d == 0 || d > capacity {
				d = capacity
			}
			used += d
		}
		share := used * 100 / (capacity * steps)
		if os.Getenv("GX_LOG_GUARD_KNEE") != "" {
			t.Logf("line %d KiB/s: ping high %d of %d steps, line used %d%%", capacity, bad, steps, share)
		}
		if bad > steps/4 {
			t.Errorf("line %d KiB/s: the ping was high %d of %d steps", capacity, bad, steps)
		}
		if share < 90 {
			t.Errorf("line %d KiB/s: only %d%% of the line used", capacity, share)
		}
	}
}
