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
	d, u, _ := r.g.update(r.now, true, r.line, uint64(rate*1024), 10*1024, 9400, 300)
	return d, u
}

// ownQueue is a line used by Gextto alone: the latency is 60 ms when Gextto
// runs uncapped and falls to 20 ms as soon as it backs off.
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

// TestLatencyGuardAloneKeepsTheLine: with nobody else on the line a trial
// shows the queue is Gextto's own (the latency falls while it backs off); the
// line goes back to Gextto, the fallen latency is the idle reference, and no
// cap follows for the cooldown, however high the latency.
func TestLatencyGuardAloneKeepsTheLine(t *testing.T) {
	r := newGuardRun()
	var trial bool
	for i := 0; i < 8; i++ {
		if d, _ := r.step(ownQueue, 9000); d > 0 {
			trial = true
		}
	}
	if !trial {
		t.Fatal("no trial on an unknown idle reference")
	}
	if st := r.g.status(); st.Mode != "free" || st.Active || st.BaselineMs != 20 {
		t.Fatalf("after the trial: %+v, want free, no caps, reference 20 ms", st)
	}
	for i := 0; i < int(latencyGuardCooldown/latencyGuardProbeEvery)-10; i++ {
		if d, u := r.step(ownQueue, 9000); d != 0 || u != 0 {
			t.Fatalf("capped %d/%d with nobody else on the line", d, u)
		}
	}
}

// TestLatencyGuardYieldsToOthersAndReleases: someone else keeps the queue full
// while Gextto backs off, so the guard yields (the caps go down while the
// latency stays high); once the others leave the caps climb and are lifted.
func TestLatencyGuardYieldsToOthersAndReleases(t *testing.T) {
	r := newGuardRun()
	left := false
	queue := othersQueue(&left)
	var d int64
	for i := 0; i < 8; i++ {
		d, _ = r.step(queue, 9000)
	}
	if st := r.g.status(); st.Mode != "yield" || !st.OthersOnLine || d == 0 {
		t.Fatalf("after the trial: %+v cap %d, want yield with a cap", st, d)
	}
	first := d
	for i := 0; i < 6; i++ {
		d, _ = r.step(queue, 9000)
	}
	if d >= first {
		t.Fatalf("the cap did not go down while the others kept the line full: %d → %d", first, d)
	}
	left = true
	for i := 0; i < 400 && d != 0; i++ {
		d, _ = r.step(queue, 9000)
	}
	if d != 0 || r.g.status().Mode != "free" {
		t.Fatalf("not released after the others left: cap %d mode %s", d, r.g.status().Mode)
	}
}

// TestLatencyGuardTrustsTheRouter: with the router's counters, a trusted
// reference and nobody else on the line, a high latency caps nothing; with
// someone else it yields at once, without a trial.
func TestLatencyGuardTrustsTheRouter(t *testing.T) {
	r := newGuardRun()
	r.line = lineGuess{known: true}
	for i := 0; i < 8; i++ { // the first trial measures the reference
		r.step(ownQueue, 9000)
	}
	r.g.mu.Lock()
	r.g.cooldownUntil = time.Time{}
	r.g.mu.Unlock()
	for i := 0; i < 20; i++ {
		if d, _ := r.step(func(int64) time.Duration { return 60 * time.Millisecond }, 9000); d != 0 {
			t.Fatalf("capped %d though the router sees nobody else", d)
		}
	}
	r.line.others = true
	if d, _ := r.step(func(int64) time.Duration { return 60 * time.Millisecond }, 9000); d == 0 || r.g.status().Mode != "yield" {
		t.Fatalf("no yield with someone else on the line: cap %d mode %s", d, r.g.status().Mode)
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
	if d, u, changed := r.g.update(r.now, false, lineGuess{}, 0, 0, 0, 0); d != 0 || u != 0 || !changed {
		t.Fatalf("leaving Auto: %d/%d changed=%v, want 0/0 true", d, u, changed)
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
