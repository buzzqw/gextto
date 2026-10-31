package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestAutoSpeedTrialCapsBothDirections checks that a loading line with no
// trusted baseline enters the trial and caps download and upload at 60 %.
func TestAutoSpeedTrialCapsBothDirections(t *testing.T) {
	a := &autoSpeed{}
	base := time.Unix(1_700_000_000, 0)
	a.record(base, 20*time.Millisecond)

	down, up := a.update(base, 0, 0, 0, 0, 50, 1000*1024, 500*1024)
	if down != 600 || up != 300 {
		t.Fatalf("trial caps = %d/%d, want 600/300", down, up)
	}
	mode, _, _, _, _, have := a.status(base)
	if mode != autoSpeedModeTrial || !have {
		t.Fatalf("mode = %v have=%v, want trial true", mode, have)
	}
}

// TestAutoSpeedCutsAndRecovers checks the controller lowers both caps when the
// latency is high and raises them again when it falls back to rest.
func TestAutoSpeedCutsAndRecovers(t *testing.T) {
	a := &autoSpeed{}
	base := time.Unix(1_700_000_000, 0)
	a.record(base, 20*time.Millisecond)
	a.record(base.Add(time.Second), 100*time.Millisecond)

	// A high latency with no trusted baseline starts a trial at 60 %.
	down, up := a.update(base.Add(2*time.Second), 0, 0, 0, 0, 50, 1000*1024, 500*1024)
	if down != 600 || up != 300 {
		t.Fatalf("trial caps = %d/%d, want 600/300", down, up)
	}
	// The trial does not lower the latency: the queue is ours, so control starts.
	a.record(base.Add(10*time.Second), 100*time.Millisecond)
	down, up = a.update(base.Add(10*time.Second), 0, 0, 0, 0, 50, 1000*1024, 500*1024)
	if down == 0 || up == 0 {
		t.Fatalf("control must cap both directions, got %d/%d", down, up)
	}
	firstDown, firstUp := down, up
	// Still high after the settle time: each direction is cut by 15 %.
	a.record(base.Add(15*time.Second), 100*time.Millisecond)
	down, up = a.update(base.Add(16*time.Second), 0, 0, 0, 0, 50, 1000*1024, 500*1024)
	if down >= firstDown || up >= firstUp {
		t.Fatalf("high latency must cut the caps: %d/%d from %d/%d", down, up, firstDown, firstUp)
	}
	cutDown, cutUp := down, up
	// Back at rest: enough low samples to move the median, then the caps rise.
	for i := 20; i <= 22; i++ {
		a.record(base.Add(time.Duration(i)*time.Second), 20*time.Millisecond)
	}
	down, up = a.update(base.Add(23*time.Second), 0, 0, 0, 0, 50, 1000*1024, 500*1024)
	if down <= cutDown || up <= cutUp {
		t.Fatalf("rest must raise the caps: %d/%d from %d/%d", down, up, cutDown, cutUp)
	}
}

// TestAutoSpeedFloorsAtMinimum checks the upload floor is respected.
func TestAutoSpeedFloorsAtMinimum(t *testing.T) {
	a := &autoSpeed{}
	base := time.Unix(1_700_000_000, 0)
	a.record(base, 20*time.Millisecond)
	a.record(base.Add(time.Second), 500*time.Millisecond)
	if _, up := a.update(base.Add(2*time.Second), 0, 0, 0, 0, 900, 0, 1000*1024); up != 900 {
		t.Fatalf("upload cap = %d, want the floor 900", up)
	}
}

// TestAutoSpeedNoSamples checks the controller is inactive without a
// measurement.
func TestAutoSpeedNoSamples(t *testing.T) {
	a := &autoSpeed{}
	now := time.Unix(1_700_000_000, 0)
	if down, up := a.update(now, 0, 0, 0, 0, 50, 1000*1024, 500*1024); down != 0 || up != 0 {
		t.Fatalf("without samples = %d/%d, want 0/0", down, up)
	}
}

// TestAutoSpeedIdleReleases checks a finished transfer gives the line back.
func TestAutoSpeedIdleReleases(t *testing.T) {
	a := &autoSpeed{}
	base := time.Unix(1_700_000_000, 0)
	a.record(base, 20*time.Millisecond)
	a.record(base.Add(time.Second), 100*time.Millisecond)
	a.update(base.Add(2*time.Second), 0, 0, 0, 0, 50, 1000*1024, 500*1024)
	a.record(base.Add(10*time.Second), 100*time.Millisecond)
	a.update(base.Add(10*time.Second), 0, 0, 0, 0, 50, 1000*1024, 500*1024)
	if down, up := a.update(base.Add(11*time.Second), 0, 0, 0, 0, 50, 0, 0); down != 0 || up != 0 {
		t.Fatalf("idle line = %d/%d, want 0/0", down, up)
	}
}

// TestAutoSpeedReset checks a disable forgets the samples.
func TestAutoSpeedReset(t *testing.T) {
	a := &autoSpeed{}
	base := time.Unix(1_700_000_000, 0)
	a.record(base, 20*time.Millisecond)
	a.update(base, 0, 0, 0, 0, 50, 1000*1024, 500*1024)
	a.reset()
	if _, _, _, _, _, have := a.status(base); have {
		t.Fatal("reset must drop the samples")
	}
}

// TestAutoSpeedCeiling checks the line maximum and the manual limit combine
// into the highest value the controller may reach.
func TestAutoSpeedCeiling(t *testing.T) {
	cases := []struct{ configured, lineMax, want int64 }{
		{0, 0, 0},
		{5000, 0, 5000},
		{0, 9000, 9000},
		{5000, 9000, 5000},
		{9000, 5000, 5000},
	}
	for _, c := range cases {
		if got := autoSpeedCeiling(c.configured, c.lineMax); got != c.want {
			t.Errorf("autoSpeedCeiling(%d, %d) = %d, want %d", c.configured, c.lineMax, got, c.want)
		}
	}
}

// TestEffectiveSpeedLimit checks a manual limit is a ceiling, never a value
// automatic speed can raise.
func TestEffectiveSpeedLimit(t *testing.T) {
	if got := effectiveSpeedLimit(1000, 800); got != 800 {
		t.Fatalf("cap below limit = %d, want 800", got)
	}
	if got := effectiveSpeedLimit(1000, 1200); got != 1000 {
		t.Fatalf("cap above limit = %d, want 1000", got)
	}
	if got := effectiveSpeedLimit(1000, 0); got != 1000 {
		t.Fatalf("no cap = %d, want 1000", got)
	}
	if got := effectiveSpeedLimit(0, 800); got != 800 {
		t.Fatalf("unlimited configured = %d, want 800", got)
	}
}

// TestProbeLatency checks a reachable target yields a positive round-trip.
func TestProbeLatency(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	rtt, ok := probeLatency(listener.Addr().String(), nil)
	if !ok || rtt <= 0 {
		t.Fatalf("probe = %v ok=%v, want a positive round-trip", rtt, ok)
	}
}

// TestLatencyCandidates checks the target parsing and the gateway fallback.
func TestLatencyCandidates(t *testing.T) {
	if got := latencyCandidates("router.local:8080", nil); len(got) != 1 || got[0] != "router.local:8080" {
		t.Fatalf("explicit host:port = %v", got)
	}
	if got := latencyCandidates("192.168.1.1", nil); len(got) != 2 || got[0] != "192.168.1.1:80" || got[1] != "192.168.1.1:443" {
		t.Fatalf("bare host = %v", got)
	}
	gateway := net.IPv4(10, 0, 0, 1)
	got := latencyCandidates("", gateway)
	if len(got) != 3 || got[0] != "10.0.0.1:80" || got[2] != "10.0.0.1:53" {
		t.Fatalf("gateway candidates = %v", got)
	}
	if got := latencyCandidates("", nil); got != nil {
		t.Fatalf("no gateway must yield no candidates, got %v", got)
	}
}

// TestApplyEffectiveSpeedLimitsAutoSpeed checks the controller lowers the
// effective limits when it is enabled and the latency is high.
func TestApplyEffectiveSpeedLimitsAutoSpeed(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.autoSpeed = &autoSpeed{}
	d.opts.Settings.Set(autospeedEnabledKey, "true")
	d.opts.Settings.Set(autospeedMinKey, "100")

	d.mu.Lock()
	d.state.Config.SpeedLimitUpload = 1000
	d.rateDown = 1000 * 1024
	d.rateUp = 500 * 1024
	base := time.Now()
	// No measurement yet: the configured limit is applied unchanged.
	d.applyEffectiveSpeedLimitsLocked(base)
	if d.appliedUL != 1000 || d.appliedDL != 0 {
		d.mu.Unlock()
		t.Fatalf("without samples applied = %d/%d, want 0/1000", d.appliedDL, d.appliedUL)
	}
	d.autoSpeed.record(base, 20*time.Millisecond)
	d.autoSpeed.record(base.Add(time.Second), 100*time.Millisecond)
	d.applyEffectiveSpeedLimitsLocked(base.Add(2 * time.Second))
	gotDown, gotUp := d.appliedDL, d.appliedUL
	d.mu.Unlock()
	if gotDown != 600 || gotUp != 300 {
		t.Fatalf("with high latency applied = %d/%d, want 600/300", gotDown, gotUp)
	}
}

// TestUISettingsSavesAutoSpeed checks the settings page writes and shows the
// automatic speed options, including the line maxima, with no upload ceiling.
func TestUISettingsSavesAutoSpeed(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	page, err := http.Get(srv.URL + "/ui/settings")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()
	for _, field := range []string{"autospeed-enabled", "autospeed-target", "autospeed-min", "autospeed-line-down", "autospeed-line-up"} {
		if !strings.Contains(string(body), `name="`+field+`"`) {
			t.Errorf("settings page is missing %s", field)
		}
	}
	if strings.Contains(string(body), "{{AUTOSPEED_") {
		t.Fatalf("unreplaced automatic speed placeholder in the page")
	}

	resp, err := http.PostForm(srv.URL+"/ui/settings", url.Values{
		"speed-upload":        {"0"},
		"autospeed-enabled":   {"1"},
		"autospeed-target":    {"192.168.1.1:80"},
		"autospeed-min":       {"100"},
		"autospeed-line-down": {"9000"},
		"autospeed-line-up":   {"800"},
		"schedule-enabled":    {"0"},
		"schedule-download":   {"0"},
		"schedule-upload":     {"0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	store := d.opts.Settings
	if store.Get(autospeedEnabledKey, "") != "true" ||
		store.Get(autospeedTargetKey, "") != "192.168.1.1:80" ||
		store.Get(autospeedMinKey, "") != "100" ||
		store.Get(autospeedLineDownKey, "") != "9000" ||
		store.Get(autospeedLineUpKey, "") != "800" {
		t.Fatalf("automatic speed not saved: %q %q %q %q %q",
			store.Get(autospeedEnabledKey, ""), store.Get(autospeedTargetKey, ""),
			store.Get(autospeedMinKey, ""), store.Get(autospeedLineDownKey, ""),
			store.Get(autospeedLineUpKey, ""))
	}
}
