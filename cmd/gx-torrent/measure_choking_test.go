package main

// measure_choking_test.go measures the choking/seeding behaviour on a real
// swarm: it seeds a public torrent whose data is already on disk and reports how
// much real peers take from us and how many connect. It is the "campaign" gate
// for any change to the choking algorithm: without sustained upload to real
// peers there is nothing to tune. Opt-in, outside CI:
//
//	GX_MEASURE_CHOKING=<dir with the seeded file> GX_MEASURE_CHOKING_SECS=180 \
//	  go test ./cmd/gx-torrent/ -run MeasureChoking -v -count=1 -timeout 30m
//
// The torrent is GX_MEASURE_REAL (a .torrent whose data is already at
// <dir>/<name>); the seeder runs from <dir>.

import (
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMeasureChokingRealSwarm(t *testing.T) {
	dir := os.Getenv("GX_MEASURE_CHOKING")
	if dir == "" {
		t.Skip("set GX_MEASURE_CHOKING=<dir with the seeded file> to run the seeding campaign")
	}
	torrentPath := os.Getenv("GX_MEASURE_REAL")
	if torrentPath == "" {
		t.Skip("set GX_MEASURE_REAL=<public .torrent file> too")
	}
	secs := 180
	if v := os.Getenv("GX_MEASURE_CHOKING_SECS"); v != "" {
		if n, err := time.ParseDuration(v + "s"); err == nil {
			secs = int(n.Seconds())
		}
	}
	torrentData, err := os.ReadFile(torrentPath)
	if err != nil {
		t.Fatal(err)
	}

	d := newTestDaemonWith(t, NetworkOptions{PortBegin: 46200, PortEnd: 46250, Encryption: 1, PEX: true})
	hash, _, err := d.add(addRequest{TorrentData: torrentData, Destination: dir, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Minute)
	for stateOf(d, hash) != "seeding" && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if stateOf(d, hash) != "seeding" {
		t.Fatalf("the seeder did not verify its data (state %s)", stateOf(d, hash))
	}
	base := d.session.Stats()
	t.Logf("seeding %s from %s for %ds", filepath.Base(torrentPath), dir, secs)

	peakPeers := 0
	start := time.Now()
	for time.Since(start) < time.Duration(secs)*time.Second {
		time.Sleep(15 * time.Second)
		if info, ok := findInfo(d, hash); ok && info.NumPeers > peakPeers {
			peakPeers = info.NumPeers
		}
		st := d.session.Stats()
		t.Logf("t=%4s peers=%d (peak %d) uploaded=%.1f MiB (%.0f KiB/s) downloaded=%.1f MiB",
			time.Since(start).Round(time.Second), peerCount(d, hash), peakPeers,
			float64(st.BytesUploaded)/float64(1<<20), float64(st.BytesUploaded-base.BytesUploaded)/(1<<10)/time.Since(start).Seconds(),
			float64(st.BytesDownloaded)/float64(1<<20))
	}
	end := d.session.Stats()
	up := end.BytesUploaded - base.BytesUploaded
	elapsed := time.Since(start)
	size := int64(1)
	if info, ok := findInfo(d, hash); ok && info.TotalSize > 0 {
		size = info.TotalSize
	}
	t.Logf("SUMMARY seeded=%ds peers_peak=%d uploaded=%d bytes (%.2fx the file) upload_rate=%.0f KiB/s leechers_served=%v",
		secs, peakPeers, up, float64(up)/float64(size), float64(up)/(1<<10)/elapsed.Seconds(), up > 0)
}

// peerCount reports the connected peers of a torrent (0 when unknown).
func peerCount(d *Daemon, hash string) int {
	if info, ok := findInfo(d, hash); ok {
		return info.NumPeers
	}
	return 0
}

// TestMeasureChokingLibtorrent is the controlled half of the campaign: our
// seeder serves a real libtorrent leecher on loopback (no NAT), so the upload
// and the choking can be measured against an external client. Opt-in, outside
// CI (needs python3-libtorrent):
//
//	GX_MEASURE_CHOKING_LT=1 go test ./cmd/gx-torrent/ -run MeasureChokingLibtorrent -v
func TestMeasureChokingLibtorrent(t *testing.T) {
	if os.Getenv("GX_MEASURE_CHOKING_LT") != "1" {
		t.Skip("set GX_MEASURE_CHOKING_LT=1 (needs python3-libtorrent)")
	}
	content := make([]byte, 48<<20) // 48 MiB
	if _, err := rand.Read(content); err != nil {
		t.Fatal(err)
	}
	// A 256 KiB piece torrent keeps the transfer long enough to observe seeding.
	torrentData, hash := makeTorrentContent(t, "campaign.bin", content)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "campaign.bin"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	d := newTestDaemonWith(t, NetworkOptions{PortBegin: 46500, PortEnd: 46550, Encryption: 1, PEX: true})
	if _, _, err := d.add(addRequest{TorrentData: torrentData, Destination: dir, SeedRatio: -1, SeedDays: -1}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the seeder to verify its data", func() bool { return stateOf(d, hash) == "seeding" })

	torrentPath := filepath.Join(t.TempDir(), "campaign.torrent")
	if err := os.WriteFile(torrentPath, torrentData, 0o644); err != nil {
		t.Fatal(err)
	}
	leechDir := filepath.Join(t.TempDir(), "leech")
	if err := os.MkdirAll(leechDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base := d.session.Stats()
	cmd := exec.Command("python3", filepath.Join("..", "..", "scripts", "libtorrent-leech.py"), torrentPath, leechDir,
		"46601", fmt.Sprintf("127.0.0.1:%d", d.peerPort))
	out, err := cmd.CombinedOutput()
	end := d.session.Stats()
	up := end.BytesUploaded - base.BytesUploaded
	t.Logf("libtorrent leecher: %s", strings.TrimSpace(string(out)))
	t.Logf("our seeder uploaded %d bytes (%.0f%% of the file) to the external client",
		up, 100*float64(up)/float64(len(content)))
	if err != nil {
		t.Fatalf("libtorrent leecher failed: %v", err)
	}
	if up < int64(len(content))/2 {
		t.Fatalf("our seeder uploaded only %d of %d bytes", up, len(content))
	}
}
