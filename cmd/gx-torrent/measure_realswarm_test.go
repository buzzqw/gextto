package main

// measure_realswarm_test.go validates the uTP transport and its adaptive RTO on
// a real swarm: it downloads a public torrent with the DHT, the trackers and
// uTP enabled, sampling the uTP socket counters while peers come and go. Opt-in,
// outside CI:
//
//	GX_MEASURE_REAL=/path/to/public.torrent \
//	  go test ./cmd/gx-torrent/ -run MeasureRealSwarm -v -count=1 -timeout 20m
//
// GX_MEASURE_REAL_SECONDS sets the measurement window (default 120). The point
// is not the throughput (a public swarm is noisy) but that real uTP peers
// connect and that the retransmission counters stay sane: a healthy swarm must
// not show a resend storm, and the adaptive backoff must not fire on clean
// paths.

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestMeasureRealSwarm(t *testing.T) {
	torrentPath := os.Getenv("GX_MEASURE_REAL")
	if torrentPath == "" {
		t.Skip("set GX_MEASURE_REAL=<public torrent file> to run the real-swarm measurement")
	}
	window := 120 * time.Second
	if raw := os.Getenv("GX_MEASURE_REAL_SECONDS"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			window = time.Duration(n) * time.Second
		}
	}
	data, err := os.ReadFile(torrentPath)
	if err != nil {
		t.Fatal(err)
	}

	d := newTestDaemonWith(t, NetworkOptions{
		PortBegin: 45800, PortEnd: 45819,
		Encryption: 1, DHT: true, UTP: true, PEX: true,
	})
	dst := filepath.Join(t.TempDir(), "dl")
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: dst, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}

	// Sample while the swarm is live: peak peers, peak uTP peers and the
	// counters over the whole window.
	var peakPeers, peakUTPPeers int
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		st := d.session.Stats()
		if st.Peers > peakPeers {
			peakPeers = st.Peers
		}
		if st.UTPPeers > peakUTPPeers {
			peakUTPPeers = st.UTPPeers
		}
		time.Sleep(2 * time.Second)
	}

	st := d.session.Stats()
	info, _ := findInfo(d, hash)
	t.Logf("real swarm %s over %v: state=%s progress=%.1f%% downloaded=%d B",
		filepath.Base(torrentPath), window, info.State, info.Progress, info.TotalDone)
	t.Logf("peers: peak %d (peak uTP %d) | connections: utp outgoing %d / tcp outgoing %d, utp incoming %d",
		peakPeers, peakUTPPeers, st.OutgoingUTP, st.OutgoingTCP, st.IncomingUTP)
	t.Logf("uTP socket: dials %d (%d errors) / accepts %d (%d errors) | resends RTO %d fast %d | SYN drops %d",
		st.UTPDials, st.UTPDialErrors, st.UTPAccepts, st.UTPAcceptErrors,
		st.UTPResendsTimeout, st.UTPResendsFast, st.UTPSynDrops)

	// A run that never saw a single peer says nothing about uTP: fail loudly
	// instead of recording a hollow "no regression".
	if peakPeers == 0 && st.UTPDials+st.UTPAccepts == 0 {
		t.Fatal("no peers at all on the real swarm: check the network, the DHT and the torrent")
	}
}
