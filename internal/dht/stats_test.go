package dht

import "testing"

// TestStatsExposesDiagnostics locks the counters Stats() promises: the daemon
// forwards every key to SessionStats.DHTStats and the "dht_<key>" session map,
// so a silent removal would break the DHT panel. Values are cumulative across
// tests, so only presence and non-negativity are checked.
func TestStatsExposesDiagnostics(t *testing.T) {
	s := Stats()
	for _, key := range []string{
		"nodes", "nodes_reached", "peers_found",
		"packets_in", "packets_out", "bytes_in", "bytes_out", "dropped",
		"queries_in", "queries_out", "replies_in",
		"blocked_hosts", "self_promotions", "dupe_references", "nodes_killed",
		"announce_ok_token", "announce_bad_token",
		"reflected_port",
		"reflected_observations", "nat_mapping",
		"nodes_capped",
		"subnet_share_max", "nodes_insecure_id",
	} {
		v, ok := s[key]
		if !ok {
			t.Errorf("Stats() missing key %q", key)
			continue
		}
		if v < 0 {
			t.Errorf("Stats()[%q] = %d, want >= 0", key, v)
		}
	}
}
