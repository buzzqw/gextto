package main

// footprint_optin_test.go is the opt-in footprint harness (docs/gx-torrent-evoluto.md
// §8.2). It is skipped unless GX_FOOTPRINT=1: it starts the built daemon and
// measures RSS at rest, thread count, startup time and idle CPU, comparing them
// with a committed baseline. Run it by hand:
//
//	GX_FOOTPRINT=1 go test ./cmd/gx-torrent/ -run TestFootprint -v
//	GX_UPDATE_FOOTPRINT=1 GX_FOOTPRINT=1 ... -run TestFootprint   # update baseline
//	GX_QBT=/path/to/qbittorrent-nox GX_FOOTPRINT=1 ...            # also measure qBittorrent
//
// It is not part of the default suite: it is slow and hardware-dependent.

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

type footprint struct {
	RSSMB      int64 `json:"rss_mb"`
	Threads    int64 `json:"threads"`
	StartupMS  int64 `json:"startup_ms"`
	CPUPercent int64 `json:"cpu_percent"`
}

const footprintBaseline = "testdata/footprint.json"

func gxBinary(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{"../../bin/gx-torrent", "gx-torrent"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	t.Skip("gx-torrent binary not found (build with `make gx-torrent`)")
	return ""
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

func procField(pid int, key string) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, key+":"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

func procRSSMB(pid int) int64 {
	value := strings.TrimSuffix(procField(pid, "VmRSS"), " kB")
	n, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return n / 1024
}

func procThreads(pid int) int64 {
	n, _ := strconv.ParseInt(procField(pid, "Threads"), 10, 64)
	return n
}

// procCPUTicks returns the process CPU time (user+system) in clock ticks.
func procCPUTicks(pid int) int64 {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0
	}
	text := string(data)
	close := strings.LastIndexByte(text, ')')
	if close < 0 {
		return 0
	}
	fields := strings.Fields(text[close+1:])
	// After the comm field: state is 0, so utime is 11, stime is 12.
	if len(fields) < 13 {
		return 0
	}
	utime, _ := strconv.ParseInt(fields[11], 10, 64)
	stime, _ := strconv.ParseInt(fields[12], 10, 64)
	return utime + stime
}

func waitHealthy(t *testing.T, url string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// measureDaemon starts the daemon once and returns its idle footprint.
func measureDaemon(t *testing.T, binary string, extra ...string) footprint {
	t.Helper()
	data := t.TempDir()
	port := freePort(t)
	args := append([]string{
		"-mode", "standalone", "-data", data, "-listen", "127.0.0.1:" + port,
		"-download-dir", filepath.Join(data, "dl"), "-peer-ports", "0",
		"-no-dht", "-no-lsd", "-no-upnp", "-no-natpmp", "-no-utp",
	}, extra...)
	cmd := exec.Command(binary, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	start := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", binary, err)
	}
	defer func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_, _ = cmd.Process.Wait()
	}()
	url := "http://127.0.0.1:" + port + "/api/v1/health"
	if !waitHealthy(t, url, 30*time.Second) {
		t.Fatalf("%s did not become healthy", binary)
	}
	startup := time.Since(start)

	// Idle window for RSS and CPU.
	time.Sleep(3 * time.Second)
	rss := procRSSMB(cmd.Process.Pid)
	threads := procThreads(cmd.Process.Pid)
	ticksBefore := procCPUTicks(cmd.Process.Pid)
	const window = 5 * time.Second
	time.Sleep(window)
	ticksAfter := procCPUTicks(cmd.Process.Pid)
	hz := int64(100) // sysconf(_SC_CLK_TCK) is 100 on Linux
	cpu := (ticksAfter - ticksBefore) * 100 / (hz * int64(window.Seconds()))
	return footprint{RSSMB: rss, Threads: threads, StartupMS: startup.Milliseconds(), CPUPercent: cpu}
}

func TestFootprint(t *testing.T) {
	if os.Getenv("GX_FOOTPRINT") != "1" {
		t.Skip("opt-in: set GX_FOOTPRINT=1")
	}
	binary := gxBinary(t)
	got := measureDaemon(t, binary)
	t.Logf("gx-torrent idle footprint: RSS=%d MB threads=%d startup=%d ms cpu=%d%%",
		got.RSSMB, got.Threads, got.StartupMS, got.CPUPercent)

	if qbt := strings.TrimSpace(os.Getenv("GX_QBT")); qbt != "" {
		qbtFootprint := measureDaemon(t, qbt)
		t.Logf("qbittorrent-nox idle footprint: RSS=%d MB threads=%d startup=%d ms cpu=%d%%",
			qbtFootprint.RSSMB, qbtFootprint.Threads, qbtFootprint.StartupMS, qbtFootprint.CPUPercent)
	}

	if os.Getenv("GX_UPDATE_FOOTPRINT") == "1" {
		raw, _ := json.MarshalIndent(got, "", "  ")
		if err := os.MkdirAll(filepath.Dir(footprintBaseline), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(footprintBaseline, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("baseline written to %s", footprintBaseline)
		return
	}

	raw, err := os.ReadFile(footprintBaseline)
	if err != nil {
		t.Skipf("no baseline at %s: run with GX_UPDATE_FOOTPRINT=1 to create it", footprintBaseline)
	}
	var baseline footprint
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	check := func(name string, got, want int64) {
		if want <= 0 {
			return
		}
		if got > want*5/4 { // 25% tolerance
			t.Errorf("%s regressed: got %d, baseline %d", name, got, want)
		}
	}
	check("rss_mb", got.RSSMB, baseline.RSSMB)
	check("threads", got.Threads, baseline.Threads)
	check("startup_ms", got.StartupMS, baseline.StartupMS)
	check("cpu_percent", got.CPUPercent, baseline.CPUPercent)
}
