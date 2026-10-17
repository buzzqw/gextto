package main

// measure_files_test.go measures what a torrent with many files costs the
// daemon: open descriptors, peak RSS and the time to check data already on
// disk. It answers whether gx-core needs a file pool (an LRU of open files)
// instead of keeping every file of a running torrent open.
//
// Run it, bounded so the host stays usable, with:
//
//	GX_MEASURE_FILES=500,2000,5000 GOMAXPROCS=2 nice -n 19 ionice -c3 \
//	  systemd-run --user --scope -p MemoryMax=6G -p CPUQuota=200% \
//	  go test ./cmd/gx-torrent/ -run MeasureManyFiles -v -count=1
//
// Each step is a fresh daemon; the data is removed with the test's temp dir.

import (
	"crypto/sha1"
	"fmt"
	mrand "math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// openFDs counts this process's open descriptors.
func openFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skipf("no /proc/self/fd: %v", err)
	}
	return len(entries)
}

// procStatusKB reads one "Vm…" line of /proc/self/status in KiB.
func procStatusKB(t *testing.T, key string) int64 {
	t.Helper()
	raw, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, key+":") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				v, _ := strconv.ParseInt(fields[1], 10, 64)
				return v
			}
		}
	}
	return 0
}

func TestMeasureManyFiles(t *testing.T) {
	spec := os.Getenv("GX_MEASURE_FILES")
	if spec == "" {
		t.Skip("set GX_MEASURE_FILES=500,2000,5000 to run the many-files measurement")
	}
	const fileSize = 4096
	for _, part := range strings.Split(spec, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n <= 0 {
			t.Fatalf("bad file count %q", part)
		}
		t.Run(fmt.Sprintf("%d_files", n), func(t *testing.T) {
			d := newTestDaemon(t)
			dest := t.TempDir()
			files := make([]layoutFile, n)
			for i := range files {
				files[i] = layoutFile{path: fmt.Sprintf("d%03d/f%06d.bin", i%200, i), size: fileSize}
			}
			data := makeLayoutTorrent(t, filepath.Join(dest, "Many"), "Many", files)
			baseFDs := openFDs(t)
			baseRSS := procStatusKB(t, "VmRSS")

			start := time.Now()
			hash, _, err := d.add(addRequest{TorrentData: data, Destination: dest, SeedRatio: -1, SeedDays: -1})
			if err != nil {
				t.Fatal(err)
			}
			peakFDs := baseFDs
			deadline := time.Now().Add(10 * time.Minute)
			var checked time.Duration
			var state string
			for time.Now().Before(deadline) {
				peakFDs = max(peakFDs, openFDs(t))
				info, ok := findInfo(d, hash)
				if ok {
					state = info.State
					if info.Progress >= 100 {
						checked = time.Since(start)
						break
					}
					if info.State == "error" {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
			}
			// Seeding: the files stay open while the torrent runs.
			time.Sleep(500 * time.Millisecond)
			seedFDs := openFDs(t)
			peakFDs = max(peakFDs, seedFDs)
			hwm := procStatusKB(t, "VmHWM")

			// Stopped: the files are closed.
			if err := d.pause(hash); err != nil {
				t.Fatal(err)
			}
			time.Sleep(time.Second)
			stoppedFDs := openFDs(t)

			load, _ := os.ReadFile("/proc/loadavg")
			t.Logf("files=%d total=%d KiB state=%s check=%v fds: base=%d peak=%d seeding=%d (+%d) stopped=%d; RSS base=%d KiB peak(HWM)=%d KiB; load=%s",
				n, n*fileSize/1024, state, checked.Round(time.Millisecond), baseFDs, peakFDs, seedFDs, seedFDs-baseFDs, stoppedFDs,
				baseRSS, hwm, strings.TrimSpace(string(load)))
			if checked == 0 {
				t.Errorf("the torrent never completed its check (state %s)", state)
			}
		})
	}
}

// TestMeasureManyFilesDownload downloads a torrent of many files at full speed
// from a second daemon over loopback: the transfer path (piece writes spread
// over thousands of files, hashing, cache) rather than the check of data
// already on disk. Both daemons run in this process, so CPU and RSS are the
// two together. The data is generated on disk (not in memory) under
// GX_MEASURE_DIR, which must be a real disk (not tmpfs), and removed at the end.
//
//	GX_MEASURE_DL=6000:163840 GX_MEASURE_DIR=$HOME/gx-measure-dl GOMAXPROCS=2 \
//	  systemd-run --user --scope -p MemoryMax=6G -p CPUQuota=200% \
//	  nice -n 19 ionice -c3 go test ./cmd/gx-torrent/ -run MeasureManyFilesDownload -v -count=1 -timeout 60m
//
// GX_MEASURE_DL is files:average-size-in-bytes. The run stops early when the
// host's 1-minute load passes GX_MEASURE_MAX_LOAD (default 6) or its available
// memory drops under 2 GiB.
func TestMeasureManyFilesDownload(t *testing.T) {
	spec := os.Getenv("GX_MEASURE_DL")
	base := os.Getenv("GX_MEASURE_DIR")
	if spec == "" || base == "" {
		t.Skip("set GX_MEASURE_DL=files:avgsize and GX_MEASURE_DIR=<dir on a real disk>")
	}
	parts := strings.SplitN(spec, ":", 2)
	n, err1 := strconv.Atoi(parts[0])
	avg := 163840
	var err2 error
	if len(parts) == 2 {
		avg, err2 = strconv.Atoi(parts[1])
	}
	if err1 != nil || err2 != nil || n <= 0 || avg <= 0 {
		t.Fatalf("bad GX_MEASURE_DL %q", spec)
	}
	maxLoad := 6.0
	if v, err := strconv.ParseFloat(os.Getenv("GX_MEASURE_MAX_LOAD"), 64); err == nil && v > 0 {
		maxLoad = v
	}
	root, err := os.MkdirTemp(base, "dl-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	genStart := time.Now()
	data, total := writeManyFilesTorrent(t, filepath.Join(src, "Many"), "Many", n, avg)
	t.Logf("generated %d files, %.2f GiB, in %v", n, float64(total)/(1<<30), time.Since(genStart).Round(time.Second))

	seeder := newTestDaemonWith(t, NetworkOptions{PortBegin: 44500, PortEnd: 44599, Encryption: 1})
	hash, _, err := seeder.add(addRequest{TorrentData: data, Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	checkStart := time.Now()
	waitForLong(t, "the seeder to check its data", 20*time.Minute, func() bool { return stateOf(seeder, hash) == "seeding" })
	t.Logf("seeder check of %d files: %v", n, time.Since(checkStart).Round(time.Millisecond))

	leecher := newTestDaemonWith(t, NetworkOptions{PortBegin: 44600, PortEnd: 44699, Encryption: 1})
	if _, _, err := leecher.add(addRequest{TorrentData: data, Destination: dst, SeedRatio: -1, SeedDays: -1}); err != nil {
		t.Fatal(err)
	}
	waitForLong(t, "the leecher to start", time.Minute, func() bool {
		state := stateOf(leecher, hash)
		return state == "downloading" || state == "stalled"
	})
	leecher.mu.Lock()
	handle, _ := leecher.findLocked(hash)
	leecher.mu.Unlock()
	if err := handle.AddPeer(fmt.Sprintf("127.0.0.1:%d", seeder.peerPort)); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	cpu0 := procCPUTicks(os.Getpid())
	var peakRate int64
	var peakRSS int64
	peakFDs := 0
	lastLog := time.Now()
	for {
		time.Sleep(time.Second)
		info, _ := findInfo(leecher, hash)
		peakRate = max(peakRate, info.DownloadRate)
		peakRSS = max(peakRSS, procStatusKB(t, "VmRSS"))
		peakFDs = max(peakFDs, openFDs(t))
		load1, avail := hostLoad(t)
		if time.Since(lastLog) >= 10*time.Second {
			t.Logf("t=%v progress=%.1f%% rate=%.1f MB/s rss=%d MiB fds=%d load=%.2f avail=%d MiB",
				time.Since(start).Round(time.Second), info.Progress, float64(info.DownloadRate)/1e6,
				procStatusKB(t, "VmRSS")/1024, openFDs(t), load1, avail)
			lastLog = time.Now()
		}
		if load1 > maxLoad || avail < 2048 {
			t.Fatalf("stopped to protect the host: load %.2f, available %d MiB", load1, avail)
		}
		if info.Progress >= 100 {
			break
		}
		if time.Since(start) > 45*time.Minute {
			t.Fatalf("not complete after 45 minutes (%.1f%%)", info.Progress)
		}
	}
	elapsed := time.Since(start)
	cpu := float64(procCPUTicks(os.Getpid())-cpu0) / 100 // USER_HZ
	t.Logf("RESULT files=%d size=%.2f GiB time=%v avg=%.1f MB/s peak=%.1f MB/s cpu=%.1f s (%.0f%% of one core, both daemons) peakRSS=%d MiB peakFDs=%d",
		n, float64(total)/(1<<30), elapsed.Round(time.Millisecond), float64(total)/elapsed.Seconds()/1e6,
		float64(peakRate)/1e6, cpu, 100*cpu/elapsed.Seconds(), peakRSS/1024, peakFDs)
}

// waitForLong is waitFor with a caller-chosen deadline.
func waitForLong(t *testing.T, what string, limit time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// hostLoad is the 1-minute load and the available memory in MiB.
func hostLoad(t *testing.T) (float64, int64) {
	t.Helper()
	var load float64
	if raw, err := os.ReadFile("/proc/loadavg"); err == nil {
		load, _ = strconv.ParseFloat(strings.Fields(string(raw))[0], 64)
	}
	var avail int64 = 1 << 30
	if raw, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "MemAvailable:") {
				kb, _ := strconv.ParseInt(strings.Fields(line)[1], 10, 64)
				avail = kb / 1024
			}
		}
	}
	return load, avail
}

// writeManyFilesTorrent writes n files of pseudo-random data (sizes spread
// around avg) under dir and returns the .torrent and the total size. The data
// streams through the piece hasher: nothing of it is kept in memory.
func writeManyFilesTorrent(t *testing.T, dir, name string, n, avg int) ([]byte, int64) {
	t.Helper()
	const pieceLength = 512 << 10
	stream := mrand.NewChaCha8([32]byte{7})
	rng := mrand.New(stream)
	hasher := sha1.New()
	inPiece := 0
	var pieces []byte
	var total int64
	list := make([]any, 0, n)
	buf := make([]byte, 64<<10)
	for i := 0; i < n; i++ {
		size := avg/2 + rng.IntN(avg+1) // avg/2 .. 3*avg/2
		rel := fmt.Sprintf("s%02d/d%03d/f%06d.bin", i%10, i%300, i)
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(target)
		if err != nil {
			t.Fatal(err)
		}
		for left := size; left > 0; {
			chunk := buf[:min(left, len(buf))]
			_, _ = stream.Read(chunk)
			if _, err := f.Write(chunk); err != nil {
				t.Fatal(err)
			}
			for off := 0; off < len(chunk); {
				take := min(len(chunk)-off, pieceLength-inPiece)
				hasher.Write(chunk[off : off+take])
				inPiece += take
				off += take
				if inPiece == pieceLength {
					pieces = hasher.Sum(pieces)
					hasher.Reset()
					inPiece = 0
				}
			}
			left -= len(chunk)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		total += int64(size)
		parts := make([]any, 0, 3)
		for _, p := range strings.Split(rel, "/") {
			parts = append(parts, p)
		}
		list = append(list, map[string]any{"length": size, "path": parts})
	}
	if inPiece > 0 {
		pieces = hasher.Sum(pieces)
	}
	return bencode(map[string]any{"info": map[string]any{
		"name": name, "files": list, "piece length": pieceLength, "pieces": pieces,
	}}), total
}

// TestMeasureRealManyFiles downloads a public torrent with many files from the
// real swarm (trackers, DHT, uTP, web seeds) into GX_MEASURE_DIR on a real
// disk, at full speed, sampling the transfer and the host, then removes the
// data. It stops at completion, after GX_MEASURE_REAL_SECONDS (default 900) or
// when the host is under strain (load over GX_MEASURE_MAX_LOAD, default 6, or
// less than 2 GiB available).
//
//	GX_MEASURE_REAL_MANY=/path/to/public.torrent GX_MEASURE_DIR=$HOME/gx-measure-dl \
//	  GOMAXPROCS=2 systemd-run --user --scope -p MemoryMax=6G -p CPUQuota=200% \
//	  nice -n 19 ionice -c3 go test ./cmd/gx-torrent/ -run MeasureRealManyFiles -v -count=1 -timeout 20m
func TestMeasureRealManyFiles(t *testing.T) {
	torrentPath := os.Getenv("GX_MEASURE_REAL_MANY")
	base := os.Getenv("GX_MEASURE_DIR")
	if torrentPath == "" || base == "" {
		t.Skip("set GX_MEASURE_REAL_MANY=<public torrent> and GX_MEASURE_DIR=<dir on a real disk>")
	}
	window := 900 * time.Second
	if n, err := strconv.Atoi(os.Getenv("GX_MEASURE_REAL_SECONDS")); err == nil && n > 0 {
		window = time.Duration(n) * time.Second
	}
	maxLoad := 6.0
	if v, err := strconv.ParseFloat(os.Getenv("GX_MEASURE_MAX_LOAD"), 64); err == nil && v > 0 {
		maxLoad = v
	}
	data, err := os.ReadFile(torrentPath)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(base, "real-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	d := newTestDaemonWith(t, NetworkOptions{
		PortBegin: 45820, PortEnd: 45839,
		Encryption: 1, DHT: true, UTP: true, PEX: true,
	})
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: root, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	cpu0 := procCPUTicks(os.Getpid())
	var peakRate int64
	var peakRSS int64
	peakFDs, peakPeers := 0, 0
	var maxLoadSeen float64
	minAvail := int64(1 << 40)
	lastLog := time.Now()
	var info torrentInfo
	for time.Since(start) < window {
		time.Sleep(time.Second)
		info, _ = findInfo(d, hash)
		peakRate = max(peakRate, info.DownloadRate)
		peakRSS = max(peakRSS, procStatusKB(t, "VmRSS"))
		peakFDs = max(peakFDs, openFDs(t))
		peakPeers = max(peakPeers, info.NumPeers)
		load1, avail := hostLoad(t)
		maxLoadSeen = max(maxLoadSeen, load1)
		minAvail = min(minAvail, avail)
		if time.Since(lastLog) >= 15*time.Second {
			t.Logf("t=%v state=%s progress=%.1f%% rate=%.2f MB/s peers=%d rss=%d MiB fds=%d load=%.2f avail=%d MiB",
				time.Since(start).Round(time.Second), info.State, info.Progress, float64(info.DownloadRate)/1e6,
				info.NumPeers, procStatusKB(t, "VmRSS")/1024, openFDs(t), load1, avail)
			lastLog = time.Now()
		}
		if load1 > maxLoad || avail < 2048 {
			t.Fatalf("stopped to protect the host: load %.2f, available %d MiB", load1, avail)
		}
		if info.Progress >= 100 {
			break
		}
	}
	elapsed := time.Since(start)
	cpu := float64(procCPUTicks(os.Getpid())-cpu0) / 100
	t.Logf("RESULT state=%s progress=%.1f%% done=%.2f GiB of %.2f GiB time=%v avg=%.2f MB/s peak=%.2f MB/s peers(peak)=%d cpu=%.1f s (%.0f%% of one core) peakRSS=%d MiB peakFDs=%d hostLoad(max)=%.2f availMin=%d MiB",
		info.State, info.Progress, float64(info.TotalDone)/(1<<30), float64(info.TotalSize)/(1<<30), elapsed.Round(time.Second),
		float64(info.TotalDone)/elapsed.Seconds()/1e6, float64(peakRate)/1e6, peakPeers, cpu, 100*cpu/elapsed.Seconds(),
		peakRSS/1024, peakFDs, maxLoadSeen, minAvail)
}
