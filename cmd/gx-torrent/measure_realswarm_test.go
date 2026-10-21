package main

// measure_realswarm_test.go validates the uTP transport and its adaptive RTO on
// a real swarm: it downloads a public torrent with the DHT, the trackers and
// uTP enabled, sampling the uTP socket counters while peers come and go. Opt-in,
// outside CI:
//
//	GX_MEASURE_REAL=/path/to/public.torrent \
//	  go test ./cmd/gx-torrent/ -run MeasureRealSwarm -v -count=1 -timeout 20m
//
// GX_MEASURE_REAL takes a comma-separated list of torrents too: they run
// together, which is the case the session budget of the automatic peer cap is
// for (many active torrents), and the CPU of the process over the window is
// reported with the bytes.
// GX_MEASURE_REAL_SECONDS sets the measurement window (default 120).
// GX_MEASURE_MAXCONN sets the per-torrent peer cap (default: the daemon's own,
// no cap): the controlled half of the cap measurement, where the same swarm is
// run with and without a cap to see what it costs in CPU and what it changes in
// throughput. The point is not the throughput (a public swarm is noisy) but
// that real uTP peers connect and that the retransmission counters stay sane: a
// healthy swarm must not show a resend storm, and the adaptive backoff must not
// fire on clean paths.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/queue"
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

	d := newTestDaemonWith(t, NetworkOptions{
		PortBegin: 45800, PortEnd: 45819,
		Encryption: 1, DHT: true, UTP: true, PEX: true,
	})
	if os.Getenv("GX_MEASURE_AUTO") == "1" {
		// The daemon's automatic cap (connauto.go): same swarm, machine-sized.
		d.state.Config.ConnAuto = queue.BoolPtr(true)
	}
	if raw := os.Getenv("GX_MEASURE_MAXCONN"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			d.state.Config.ConnAuto = queue.BoolPtr(false)
			d.state.Config.MaxConnectionsPerTorrent = n
		}
	}
	// Every torrent runs: the queue would keep all but three paused, and the
	// point of a list is many active torrents at once.
	paths := strings.Split(torrentPath, ",")
	d.state.Config.ActiveDownloads = len(paths)
	d.state.Config.ActiveLimit = 2 * len(paths)
	d.state.Config.DynamicQueue = false
	dst := filepath.Join(t.TempDir(), "dl")
	var hashes []string
	for _, path := range paths {
		data, err := os.ReadFile(strings.TrimSpace(path))
		if err != nil {
			t.Fatal(err)
		}
		hash, _, err := d.add(addRequest{TorrentData: data, Destination: dst, SeedRatio: -1, SeedDays: -1})
		if err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, hash)
	}
	cpuStart := processCPU()
	started := time.Now()

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

	cpu := processCPU() - cpuStart
	elapsed := time.Since(started)
	st := d.session.Stats()
	var done int64
	for _, hash := range hashes {
		info, _ := findInfo(d, hash)
		done += info.TotalDone
		t.Logf("real swarm %s: state=%s progress=%.1f%% downloaded=%d B", info.Name, info.State, info.Progress, info.TotalDone)
	}
	t.Logf("%d torrents over %v · automatic cap: %t · max connections per torrent: %d",
		len(hashes), window, queue.BoolOn(d.state.Config.ConnAuto), d.state.Config.MaxConnectionsPerTorrent)
	t.Logf("total: downloaded %d MB · CPU %.1f s (%.1f%% of a core) · %.2f s per 100 MB",
		done>>20, cpu.Seconds(), 100*cpu.Seconds()/elapsed.Seconds(), cpu.Seconds()/max(float64(done)/(100<<20), 0.01))
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
