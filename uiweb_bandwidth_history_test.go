package gextto

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestRateHistoryRingKeepsNewest checks the bounded ring: once full it drops the
// oldest sample and keeps the order (oldest first).
func TestRateHistoryRingKeepsNewest(t *testing.T) {
	history := newRateHistory(3)
	base := time.Unix(0, 0)
	for i := 0; i < 5; i++ {
		history.add(rateSample{At: base.Add(time.Duration(i) * time.Second), Down: uint64(i), Up: uint64(i * 10)})
	}
	snapshot := history.snapshot()
	if len(snapshot) != 3 {
		t.Fatalf("snapshot length = %d, want 3", len(snapshot))
	}
	if history.len() != 3 {
		t.Fatalf("len = %d, want 3", history.len())
	}
	if snapshot[0].Down != 2 || snapshot[2].Down != 4 {
		t.Fatalf("oldest samples not dropped: %+v", snapshot)
	}
	if !snapshot[0].At.Before(snapshot[2].At) {
		t.Fatal("snapshot must be oldest first")
	}
}

// TestRateHistoryNilSafe: the AppState built by hand in some tests leaves the
// history nil; neither the add nor the readers may panic.
func TestRateHistoryNilSafe(t *testing.T) {
	var history *rateHistory
	history.add(rateSample{Down: 1})
	if got := history.snapshot(); got != nil {
		t.Fatalf("nil history snapshot = %v, want nil", got)
	}
	if history.len() != 0 {
		t.Fatalf("nil history len = %d, want 0", history.len())
	}
}

// trafficTestState builds a state whose effective config is fixed in the cache,
// so the view sees the limit set here instead of reloading defaults from an
// empty config path.
func trafficTestState(cfg *Config) *AppState {
	return &AppState{
		cfg:          cfg,
		config_cache: &ConfigCache{cfg: cfg, generation: ConfigGeneration(), valid: true},
		rate_history: newRateHistory(rateHistoryLen),
	}
}

// TestV2TrafficViewBuildsPointsAndLimit builds the chart from a few samples and
// a configured cap: the curves get coordinates and the limit line lands inside
// the canvas.
func TestV2TrafficViewBuildsPointsAndLimit(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Settings == nil {
		cfg.Settings = map[string]string{}
	}
	cfg.Transfer.DownloadLimitKib = 1024 // 1 MiB/s
	state := trafficTestState(&cfg)

	now := time.Now()
	for i := 0; i < 12; i++ {
		state.rate_history.add(rateSample{
			At:   now.Add(time.Duration(i-12) * time.Second),
			Down: uint64(i) * 1024,
			Up:   uint64(i) * 512,
		})
	}

	view := v2TrafficViewFrom(state)
	if !view.HasData {
		t.Fatal("expected HasData with 12 samples")
	}
	if view.DownPoints == "" || view.UpPoints == "" || view.DownArea == "" {
		t.Fatalf("missing coordinates: down=%q up=%q area=%q", view.DownPoints, view.UpPoints, view.DownArea)
	}
	if !view.HasLimit {
		t.Fatal("expected the configured limit line")
	}
	y, err := strconv.ParseFloat(view.LimitY, 64)
	if err != nil || y < 0 || y > trafficChartHeight {
		t.Fatalf("limit Y = %q (%v), want within [0,%d]", view.LimitY, err, trafficChartHeight)
	}
	if view.PeakDown == "" || view.ScaleLabel == "" || view.CurrentDown == "" {
		t.Fatal("expected formatted peak/scale/current labels")
	}
}

// TestV2TrafficViewAreaStartsAtTheFirstSample: history that starts mid-window
// leaves the earlier part empty, instead of a ramp from the left edge; samples
// older than the window are not drawn.
func TestV2TrafficViewAreaStartsAtTheFirstSample(t *testing.T) {
	cfg := DefaultConfig()
	state := trafficTestState(&cfg)
	now := time.Now()
	state.rate_history.add(rateSample{At: now.Add(-rateHistoryWindow - time.Minute), Down: 9999})
	for i := 0; i < 6; i++ {
		state.rate_history.add(rateSample{At: now.Add(time.Duration(i-6) * 10 * time.Second), Down: 4 << 20})
	}
	view := v2TrafficViewFrom(state)
	first := strings.SplitN(view.DownArea, " ", 2)[0]
	x, err := strconv.ParseFloat(strings.SplitN(first, ",", 2)[0], 64)
	if err != nil || x < trafficChartWidth*0.9 {
		t.Fatalf("area starts at %q, want under the first sample near the right edge", first)
	}
	if strings.HasPrefix(view.DownPoints, "0,") {
		t.Fatalf("a sample older than the window was drawn: %q", view.DownPoints)
	}
}

// TestV2TrafficViewAutoHasNoLimit: Auto policies lift the cap, so the chart must
// not draw a limit line (the same precedence the daemon applies).
func TestV2TrafficViewAutoHasNoLimit(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Settings == nil {
		cfg.Settings = map[string]string{}
	}
	cfg.Transfer.DownloadLimitKib = 1024
	cfg.Settings["torrent_speed_auto"] = "true"
	state := trafficTestState(&cfg)
	now := time.Now()
	for i := 0; i < 5; i++ {
		state.rate_history.add(rateSample{At: now.Add(time.Duration(i) * time.Second), Down: 1024, Up: 512})
	}
	view := v2TrafficViewFrom(state)
	if view.HasLimit {
		t.Fatal("Auto speed must not show a limit line")
	}
	if !view.Auto {
		t.Fatal("Auto speed must be reported, not shown as no limit")
	}
}

// TestV2LiveTrafficAutoBadge checks the badge the session panel shows when the
// speed is on Auto: it says Auto instead of "Nessun limite".
func TestV2LiveTrafficAutoBadge(t *testing.T) {
	var buffer bytes.Buffer
	if err := v2Templates.ExecuteTemplate(&buffer, "v2_live_traffic", v2TrafficView{
		Width:       trafficChartWidth,
		Height:      trafficChartHeight,
		Auto:        true,
		CurrentDown: "0 B/s",
		CurrentUp:   "0 B/s",
	}); err != nil {
		t.Fatalf("render v2_live_traffic: %v", err)
	}
	out := buffer.String()
	if !strings.Contains(out, `title="Auto attivo: Gextto misura la linea e adatta la velocità, senza un tetto fisso">Auto</span>`) {
		t.Fatalf("Auto badge missing in:\n%s", out)
	}
	if strings.Contains(out, "Nessun limite") {
		t.Fatalf("Auto speed shown as no limit in:\n%s", out)
	}
}

// TestV2TrafficViewNeedsTwoSamples: a single point cannot form a line, so the
// chart falls back to the "collecting" state.
func TestV2TrafficViewNeedsTwoSamples(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Settings == nil {
		cfg.Settings = map[string]string{}
	}
	state := trafficTestState(&cfg)
	state.rate_history.add(rateSample{At: time.Now(), Down: 1024, Up: 512})
	if view := v2TrafficViewFrom(state); view.HasData {
		t.Fatal("a single sample must not draw a curve")
	}
}

// TestV2TrafficViewTreatsSubKibRatesAsZero: protocol chatter under 1 KiB/s is
// idle for the session panel, so the curves sit on the baseline and the peak
// reads 0 B/s; the floor itself is kept.
func TestV2TrafficViewTreatsSubKibRatesAsZero(t *testing.T) {
	cfg := DefaultConfig()
	state := trafficTestState(&cfg)
	now := time.Now()
	for i := 0; i < 6; i++ {
		state.rate_history.add(rateSample{At: now.Add(time.Duration(i-6) * time.Second), Down: 300, Up: 900})
	}
	view := v2TrafficViewFrom(state)
	if !view.HasData {
		t.Fatal("expected a flat chart with six samples")
	}
	if got := v2RateOrZero(rateNoiseFloor - 1); got != 0 {
		t.Fatalf("v2RateOrZero(%d) = %d, want 0", rateNoiseFloor-1, got)
	}
	if got := v2RateOrZero(rateNoiseFloor); got != rateNoiseFloor {
		t.Fatalf("v2RateOrZero(%d) = %d, want it kept", rateNoiseFloor, got)
	}
	if view.PeakDown != "0 B/s" || view.PeakUp != "0 B/s" {
		t.Fatalf("sub-floor peaks = %q/%q, want 0 B/s", view.PeakDown, view.PeakUp)
	}
	for _, point := range strings.Fields(view.DownPoints) {
		if !strings.HasSuffix(point, ","+strconv.Itoa(trafficChartHeight)) {
			t.Fatalf("sub-floor point not on the baseline: %q in %q", point, view.DownPoints)
		}
	}

	state.rate_history.add(rateSample{At: now, Down: rateNoiseFloor, Up: rateNoiseFloor})
	view = v2TrafficViewFrom(state)
	if view.PeakDown != "1.0 KB/s" || view.PeakUp != "1.0 KB/s" {
		t.Fatalf("rates at the floor = %q/%q, want 1.0 KB/s", view.PeakDown, view.PeakUp)
	}
}

// TestV2LiveTrafficTemplateRendersChart renders the fragment the SSE stream
// pushes and checks the accessible SVG plus the limit line are present.
func TestV2LiveTrafficTemplateRendersChart(t *testing.T) {
	view := v2TrafficView{
		HasData:     true,
		Width:       trafficChartWidth,
		Height:      trafficChartHeight,
		DownPoints:  "0,160 600,0",
		UpPoints:    "0,160 600,80",
		DownArea:    "0,160 0,160 600,0 600,160",
		HasLimit:    true,
		LimitY:      "20",
		LimitLabel:  "1.0 MB/s",
		CurrentDown: "1.0 MB/s",
		CurrentUp:   "512 KB/s",
		PeakDown:    "2.0 MB/s",
		PeakUp:      "1.0 MB/s",
		ScaleLabel:  "2.0 MB/s",
	}
	var buffer bytes.Buffer
	if err := v2Templates.ExecuteTemplate(&buffer, "v2_live_traffic", view); err != nil {
		t.Fatalf("render v2_live_traffic: %v", err)
	}
	out := buffer.String()
	for _, want := range []string{
		`id="v2-live-traffic"`,
		`class="traffic-chart"`,
		`role="img"`,
		`aria-label="Grafico della banda di rete, ultimi 30 minuti"`,
		`traffic-line down`,
		`traffic-line up`,
		`traffic-limit`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("traffic fragment missing %q in:\n%s", want, out)
		}
	}
}

// TestV2TrafficViewFromNilState: the view builder never panics on a partially
// built state.
func TestV2TrafficViewFromNilState(t *testing.T) {
	view := v2TrafficViewFrom(&AppState{})
	if view.HasData || view.HasLimit {
		t.Fatal("empty state must not draw a chart")
	}
	if view.CurrentDown == "" || view.CurrentUp == "" {
		t.Fatal("current rates must be formatted even without history")
	}
}

// TestV2DashboardHostsTrafficPanel checks the real dashboard renders the panel
// the SSE stream refreshes.
func TestV2DashboardHostsTrafficPanel(t *testing.T) {
	state := newTestAppState(t)
	if err := CompleteSetup(state.cfg); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/", nil)
	if code != http.StatusOK {
		t.Fatalf("GET / -> %d", code)
	}
	for _, want := range []string{"dashboard-traffic-panel", `id="v2-live-traffic"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard missing %q", want)
		}
	}
}
