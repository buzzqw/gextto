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

// TestAutoSpeedClampsAndRecovers checks the controller lowers the upload while
// the latency is high and raises it again when the line is idle.
func TestAutoSpeedClampsAndRecovers(t *testing.T) {
	a := &autoSpeed{}
	base := time.Unix(1_700_000_000, 0)
	a.record(base, 20*time.Millisecond)
	a.record(base.Add(time.Second), 100*time.Millisecond)

	// First decision: from the ceiling 1000 KiB/s, high latency -> x0.85 = 850.
	if v, acting := a.clampUpload(base.Add(2*time.Second), 1000, 50); !acting || v != 850 {
		t.Fatalf("first clamp = %d acting=%v, want 850 true", v, acting)
	}
	// Still high after the step interval: another x0.85 = 722.
	a.record(base.Add(5*time.Second), 100*time.Millisecond)
	if v, _ := a.clampUpload(base.Add(6*time.Second), 1000, 50); v != 722 {
		t.Fatalf("second clamp = %d, want 722", v)
	}
	// Latency back to idle: the upload rises by ceiling/20 = 50 per step.
	a.record(base.Add(7*time.Second), 20*time.Millisecond)
	if v, _ := a.clampUpload(base.Add(9*time.Second), 1000, 50); v != 772 {
		t.Fatalf("recovery clamp = %d, want 772", v)
	}
}

// TestAutoSpeedFloorsAtMinimum checks the controller never goes below the floor.
func TestAutoSpeedFloorsAtMinimum(t *testing.T) {
	a := &autoSpeed{}
	base := time.Unix(1_700_000_000, 0)
	a.record(base, 20*time.Millisecond)
	a.record(base.Add(time.Second), 500*time.Millisecond)
	if v, _ := a.clampUpload(base.Add(2*time.Second), 1000, 900); v != 900 {
		t.Fatalf("clamp = %d, want the floor 900", v)
	}
}

// TestAutoSpeedNoCeilingOrSamples checks the controller is inactive without a
// positive ceiling or without any measurement.
func TestAutoSpeedNoCeilingOrSamples(t *testing.T) {
	a := &autoSpeed{}
	now := time.Unix(1_700_000_000, 0)
	if v, acting := a.clampUpload(now, 0, 50); v != 0 || acting {
		t.Fatalf("unlimited ceiling: %d acting=%v, want 0 false", v, acting)
	}
	if v, acting := a.clampUpload(now, 1000, 50); v != 1000 || acting {
		t.Fatalf("no samples: %d acting=%v, want 1000 false", v, acting)
	}
}

// TestAutoSpeedReset checks a disable forgets the samples.
func TestAutoSpeedReset(t *testing.T) {
	a := &autoSpeed{}
	base := time.Unix(1_700_000_000, 0)
	a.record(base, 20*time.Millisecond)
	a.clampUpload(base, 1000, 50)
	a.reset()
	if _, _, _, have := a.snapshot(base); have {
		t.Fatal("reset must drop the samples")
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

// TestApplyEffectiveSpeedLimitsAutoSpeed checks the controller clamps the
// effective upload when it is enabled and the latency is high.
func TestApplyEffectiveSpeedLimitsAutoSpeed(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.autoSpeed = &autoSpeed{}
	d.opts.Settings.Set(autospeedEnabledKey, "true")
	d.opts.Settings.Set(autospeedMinKey, "100")

	d.mu.Lock()
	d.state.Config.SpeedLimitUpload = 1000
	base := time.Now()
	// No measurement yet: the configured limit is applied unchanged.
	d.applyEffectiveSpeedLimitsLocked(base)
	if d.appliedUL != 1000 {
		d.mu.Unlock()
		t.Fatalf("without samples appliedUL = %d, want 1000", d.appliedUL)
	}
	d.autoSpeed.record(base, 20*time.Millisecond)
	d.autoSpeed.record(base.Add(time.Second), 100*time.Millisecond)
	d.applyEffectiveSpeedLimitsLocked(base.Add(2 * time.Second))
	got := d.appliedUL
	d.mu.Unlock()
	if got != 850 {
		t.Fatalf("with high latency appliedUL = %d, want 850", got)
	}
}

// TestUISettingsSavesAutoSpeed checks the settings page writes and shows the
// auto-speed option, and refuses to enable it without an upload ceiling.
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
	if page.StatusCode != http.StatusOK || !strings.Contains(string(body), `name="autospeed-enabled"`) || !strings.Contains(string(body), `name="autospeed-target"`) {
		t.Fatalf("settings page: status=%d", page.StatusCode)
	}
	if strings.Contains(string(body), "{{AUTOSPEED_") {
		t.Fatalf("unreplaced auto-speed placeholder in the page")
	}

	resp, err := http.PostForm(srv.URL+"/ui/settings", url.Values{
		"speed-upload":      {"512"},
		"autospeed-enabled": {"1"},
		"autospeed-target":  {"192.168.1.1:80"},
		"autospeed-min":     {"100"},
		"schedule-enabled":  {"0"},
		"schedule-download": {"0"},
		"schedule-upload":   {"0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	store := d.opts.Settings
	if store.Get(autospeedEnabledKey, "") != "true" || store.Get(autospeedTargetKey, "") != "192.168.1.1:80" || store.Get(autospeedMinKey, "") != "100" {
		t.Fatalf("auto-speed not saved: %q %q %q", store.Get(autospeedEnabledKey, ""), store.Get(autospeedTargetKey, ""), store.Get(autospeedMinKey, ""))
	}

	// Enabling without any upload ceiling must be refused.
	bad, err := http.PostForm(srv.URL+"/ui/settings", url.Values{
		"speed-upload":      {"0"},
		"autospeed-enabled": {"1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	badBody, _ := io.ReadAll(bad.Body)
	bad.Body.Close()
	if !strings.Contains(string(badBody), `class="err"`) {
		t.Fatalf("enabling without a ceiling must be reported: %s", badBody)
	}
}
