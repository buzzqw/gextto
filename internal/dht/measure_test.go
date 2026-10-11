package dht

import (
	"crypto/rand"
	"os"
	"strconv"
	"testing"
	"time"
)

// TestMeasureDHTRealSwarm is an opt-in harness that runs the DHT against the
// public network and prints how the routing table fills and how queries fare:
// the numbers needed to decide on anti-Sybil enforcement (subnet concentration,
// insecure IDs) and on a multi-path lookup. It is skipped unless GX_MEASURE_DHT
// is set, so it never runs in the normal suite or in CI.
//
//	GX_MEASURE_DHT=5m           run for five minutes (or "1" for the default)
//	GX_MEASURE_DHT_INTERVAL=10s sampling period (default 10s)
//
// Run it with: make measure-dht
func TestMeasureDHTRealSwarm(t *testing.T) {
	raw := os.Getenv("GX_MEASURE_DHT")
	if raw == "" {
		t.Skip("set GX_MEASURE_DHT=<duration|1> to run the real-swarm DHT measurement")
	}
	dur := 2 * time.Minute
	if raw != "1" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			t.Fatalf("GX_MEASURE_DHT must be a duration like 5m or 1: %v", err)
		}
		dur = d
	}
	interval := 10 * time.Second
	if v := os.Getenv("GX_MEASURE_DHT_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			t.Fatalf("GX_MEASURE_DHT_INTERVAL must be a duration: %v", err)
		}
		interval = d
	}

	cfg := NewConfig()
	cfg.SaveRoutingTable = false // never write to ~/.taipeitorrent
	cfg.Port = 0
	if v := os.Getenv("GX_MEASURE_DHT_SUBNET_CAP"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			t.Fatalf("GX_MEASURE_DHT_SUBNET_CAP must be a non-negative integer: %v", err)
		}
		cfg.MaxNodesPerSubnet = n
	}
	// tableMetrics refreshes the Sybil counters on the cleanup tick; keep it
	// short so a measurement run sees them, without touching the default.
	cfg.CleanupPeriod = 30 * time.Second

	node, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := node.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer node.Stop()
	t.Logf("DHT started on UDP port %d, measuring for %s every %s (subnet cap %d)",
		node.Port(), dur, interval, cfg.MaxNodesPerSubnet)

	// A random infohash exercises the get_peers lookup path even though no peer
	// is expected to exist, so the counters reflect real lookup traffic.
	var ih [20]byte
	if _, err := rand.Read(ih[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	node.PeersRequest(string(ih[:]), false)

	deadline := time.Now().Add(dur)
	start := time.Now()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for time.Now().Before(deadline) {
		<-ticker.C
		st := Stats()
		t.Logf("t=%4s nodes=%4d subnet_max=%3d insecure=%3d capped=%4d q_out=%5d q_in=%4d replies=%5d drop=%4d obs=%d nat=%d bad_token=%d",
			time.Since(start).Round(time.Second), st["nodes"], st["subnet_share_max"], st["nodes_insecure_id"], st["nodes_capped"],
			st["queries_out"], st["queries_in"], st["replies_in"], st["dropped"],
			st["reflected_observations"], st["nat_mapping"], st["announce_bad_token"])
	}

	st := Stats()
	success := 0.0
	if st["queries_out"] > 0 {
		success = float64(st["replies_in"]) / float64(st["queries_out"]) * 100
	}
	t.Logf("SUMMARY nodes=%d subnet_max=%d insecure=%d capped=%d queries_out=%d queries_in=%d replies_in=%d success=%.1f%% dropped=%d reflected_obs=%d",
		st["nodes"], st["subnet_share_max"], st["nodes_insecure_id"], st["nodes_capped"],
		st["queries_out"], st["queries_in"], st["replies_in"], success, st["dropped"], st["reflected_observations"])
}
