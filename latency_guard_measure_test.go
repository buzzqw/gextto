package gextto

import (
	"context"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/autospeed"
)

// TestMeasureLatencyGuard measures the latency guard on a real line. It is
// opt-in: GX_MEASURE_LATENCY_GUARD is the URL of a gx-torrent already
// downloading at full line rate (a separate standalone instance, see
// scripts/gx-core-test.sh), GX_MEASURE_COMPETITOR_URL a large HTTP file that
// plays "another device on the line". GX_MEASURE_PHASE_SECS sets the length of
// each phase (default 90). The phases:
//
//	alone        Auto, guard on, no competitor: the line is Gextto's
//	others_off   competitor, guard off: what the other device gets today
//	others_on    competitor, guard on, router counters
//	no_router    competitor, guard on, router counters ignored (trials only)
//	others_leave the competitor stops after a minute: the guard must release
//
// GX_MEASURE_PHASES picks phases by name (comma-separated).
//
// Each phase logs the median and p90 latency, Gextto's and the competitor's
// mean rates and the share of time the guard was capping.
func TestMeasureLatencyGuard(t *testing.T) {
	gxURL := os.Getenv("GX_MEASURE_LATENCY_GUARD")
	competitor := os.Getenv("GX_MEASURE_COMPETITOR_URL")
	if gxURL == "" || competitor == "" {
		t.Skip("set GX_MEASURE_LATENCY_GUARD and GX_MEASURE_COMPETITOR_URL")
	}
	phase := 90 * time.Second
	if raw := os.Getenv("GX_MEASURE_PHASE_SECS"); raw != "" {
		if d, err := time.ParseDuration(raw + "s"); err == nil {
			phase = d
		}
	}
	cfg := &Config{DataDir: t.TempDir(), StateDir: t.TempDir(), Settings: map[string]string{
		"gxtorrent_url":              gxURL,
		"gxtorrent_poll_interval_ms": "1000",
		"torrent_speed_auto":         "true",
	}}
	engine, err := newGxTorrentEngine(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = engine.SetGlobalSpeedLimits(0, 0) }()
	if _, err := engine.SetGlobalSpeedLimits(0, 0); err != nil {
		t.Fatal(err)
	}

	type result struct {
		latencies           []time.Duration
		own, other          float64
		samples, capped, ok int
		othersSeen          float64
		modes               map[string]int
	}
	run := func(name string, guardOn, router, withCompetitor bool, competitorFor, length time.Duration) result {
		cfg.Settings[latencyGuardKey] = map[bool]string{true: "true", false: "false"}[guardOn]
		guard := newLatencyGuard()
		saved := discoverWANCounters
		if !router {
			discoverWANCounters = func() wanCounters { return nil }
		}
		defer func() { discoverWANCounters = saved }()
		_, _ = engine.SetGlobalSpeedLimits(0, 0)

		ctx, cancel := context.WithTimeout(context.Background(), length)
		defer cancel()
		competitorCtx, stopCompetitor := context.WithTimeout(ctx, competitorFor)
		defer stopCompetitor()
		var competitorBytes atomic.Int64
		var releasedAfter time.Duration
		if withCompetitor {
			go func() {
				for competitorCtx.Err() == nil {
					req, _ := http.NewRequestWithContext(competitorCtx, http.MethodGet, competitor, nil)
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						return
					}
					buf := make([]byte, 64<<10)
					for {
						n, err := resp.Body.Read(buf)
						competitorBytes.Add(int64(n))
						if err != nil {
							break
						}
					}
					resp.Body.Close()
				}
			}()
		}
		r := result{modes: map[string]int{}}
		ownMeter := newLatencyGuard()
		ticker := time.NewTicker(latencyGuardProbeEvery)
		defer ticker.Stop()
		start := time.Now()
		for {
			select {
			case <-ctx.Done():
				elapsed := time.Since(start).Seconds()
				r.other = float64(competitorBytes.Load()) / elapsed
				if r.samples > 0 {
					r.own /= float64(r.samples)
				}
				sort.Slice(r.latencies, func(i, j int) bool { return r.latencies[i] < r.latencies[j] })
				p50, p90 := time.Duration(0), time.Duration(0)
				if n := len(r.latencies); n > 0 {
					p50, p90 = r.latencies[n/2], r.latencies[n*9/10]
				}
				st := guard.status()
				if releasedAfter > 0 {
					t.Logf("%-10s guard released %s after the other device stopped", name, releasedAfter.Round(time.Second))
				}
				t.Logf("%-10s latency p50 %3d ms p90 %3d ms (reference %d ms) · Gextto %5.0f KiB/s · other device %5.0f KiB/s · capped %3d%% · modes %v · router %v, others seen %4.0f KiB/s",
					name, p50.Milliseconds(), p90.Milliseconds(), st.BaselineMs, r.own/1024, r.other/1024, 100*r.capped/max(r.samples, 1), r.modes, st.RouterCounters, r.othersSeen/float64(max(r.samples, 1)))
				return r
			case now := <-ticker.C:
				_ = engine.sync()
				if guardOn {
					guard.tick(now, cfg, engine)
				} else if rtt, ok := latencyGuardMeasureOnce(cfg); ok {
					guard.probe.Record(now, rtt)
				}
				current, _, _, ok := guard.probe.State(now, 1)
				if ok {
					r.latencies = append(r.latencies, current)
				}
				// Gextto's real rate, from the engine's wire counters: the
				// reported rates are averaged over tens of seconds.
				down, _ := ownMeter.ownRates(now, engine)
				r.own += float64(down)
				r.samples++
				if d, u := guard.caps(); d > 0 || u > 0 {
					r.capped++
				}
				st := guard.status()
				r.modes[st.Mode]++
				if withCompetitor && competitorCtx.Err() != nil && releasedAfter == 0 && !st.Active {
					releasedAfter = time.Since(start) - competitorFor
				}
				r.othersSeen += float64(st.OthersDownKib)
				// The peak the guard infers from is learned like the policy
				// tick does: the highest uncapped rate.
				if d, _ := guard.caps(); d == 0 && int64(down/1024) > speedPeakKib(cfg, autotunePeakDownKey) {
					cfg.Settings[autotunePeakDownKey] = strconv.FormatInt(int64(down/1024), 10)
				}
			}
		}
	}
	phases := os.Getenv("GX_MEASURE_PHASES")
	want := func(name string) bool { return phases == "" || strings.Contains(","+phases+",", ","+name+",") }
	if want("alone") {
		run("alone", true, true, false, 0, phase)
	}
	if want("others_off") {
		run("others_off", false, true, true, phase, phase)
	}
	if want("others_on") {
		run("others_on", true, true, true, phase, phase)
	}
	if want("no_router") {
		run("no_router", true, false, true, phase, phase)
	}
	// The other device stops after a minute: the guard must give the line
	// back to Gextto on its own.
	if want("others_leave") {
		run("others_leave", true, false, true, time.Minute, 5*time.Minute)
	}
}

// latencyGuardMeasureOnce probes the latency like the guard does, for the
// phases where the guard itself is off.
func latencyGuardMeasureOnce(cfg *Config) (time.Duration, bool) {
	return autospeed.MeasureBest(autospeed.Candidates(cfg.Settings[latencyGuardTargetKey], latencyGuardTargets))
}
