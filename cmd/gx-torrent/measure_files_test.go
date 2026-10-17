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
	"fmt"
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
