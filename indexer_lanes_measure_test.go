package gextto

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

// TestMeasureManagerLanes compares, on the real Jackett and Prowlarr, the
// aggregate (with the 45 s and the 90 s limits) and the per-indexer lanes over
// the same titles, six at a time like the cycle. Opt-in: GX_MEASURE_LANES is
// the Gextto data directory (read only: the indexers and the series titles).
func TestMeasureManagerLanes(t *testing.T) {
	dataDir := os.Getenv("GX_MEASURE_LANES")
	if dataDir == "" {
		t.Skip("set GX_MEASURE_LANES to the Gextto data directory")
	}
	cfgDB, err := sql.Open("sqlite", "file:"+dataDir+"/gextto_config.db?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer cfgDB.Close()
	var raw string
	if err := cfgDB.QueryRow("SELECT value FROM settings WHERE key='indexers'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var all []IndexerConfig
	if err := json.Unmarshal([]byte(raw), &all); err != nil {
		t.Fatal(err)
	}
	var managers []IndexerConfig
	for _, indexer := range all {
		if indexer.Enabled && isManagerSource(indexer) {
			managers = append(managers, indexer)
		}
	}
	seriesDB, err := sql.Open("sqlite", "file:"+dataDir+"/gextto_series.db?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer seriesDB.Close()
	rows, err := seriesDB.Query("SELECT name FROM series WHERE enabled=1 ORDER BY name LIMIT 30")
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			titles = append(titles, name)
		}
	}
	rows.Close()
	cfg := &Config{Indexers: managers}

	run := func(name string, lanes bool, managerTimeout time.Duration) {
		savedLanes, savedTimeout := managerLanes, managerRequestTimeout
		managerLanes, managerRequestTimeout = lanes, managerTimeout
		defer func() { managerLanes, managerRequestTimeout = savedLanes, savedTimeout }()
		resetLaneState()
		logging.TakeSourceStats()
		var releases atomic.Int64
		durations := make([]time.Duration, len(titles))
		sem := make(chan struct{}, queryConcurrency)
		var wg sync.WaitGroup
		start := time.Now()
		for i, title := range titles {
			wg.Add(1)
			sem <- struct{}{}
			go func(i int, title string) {
				defer wg.Done()
				defer func() { <-sem }()
				began := time.Now()
				ctx, cancel := context.WithTimeout(context.Background(), automaticSearchTimeout)
				items := searchOneWithDBType(ctx, cfg, title, nil, nil, nil, true, searchTypeAuto, false)
				cancel()
				durations[i] = time.Since(began)
				releases.Add(int64(len(items)))
			}(i, title)
		}
		wg.Wait()
		wall := time.Since(start)
		sort.Slice(durations, func(a, b int) bool { return durations[a] < durations[b] })
		ok, fail := 0, 0
		for _, entry := range logging.TakeSourceStats() {
			ok += entry.Stats.OK
			fail += entry.Stats.Fail
			if entry.Stats.Fail > 0 {
				t.Logf("    %s: ok %d failed %d (%s)", entry.Name, entry.Stats.OK, entry.Stats.Fail, entry.Stats.LastError)
			}
		}
		t.Logf("%-14s %2d titles: wall %5.1fs · per title p50 %5.1fs max %5.1fs · requests ok %3d failed %3d · releases %d",
			name, len(titles), wall.Seconds(), durations[len(durations)/2].Seconds(), durations[len(durations)-1].Seconds(), ok, fail, releases.Load())
	}
	// The order matters: an aggregate that times out leaves its searches
	// running inside Prowlarr and slows whatever runs next, so the default
	// order measures the lanes first. GX_MEASURE_LANE_MODES picks and orders.
	modes := os.Getenv("GX_MEASURE_LANE_MODES")
	if modes == "" {
		modes = "lanes,aggregate90,aggregate45"
	}
	for _, mode := range strings.Split(modes, ",") {
		switch strings.TrimSpace(mode) {
		case "lanes":
			run("lanes", true, managerRequestTimeout)
		case "aggregate90":
			run("aggregate 90s", false, 90*time.Second)
		case "aggregate45":
			run("aggregate 45s", false, 45*time.Second)
		}
	}
}
