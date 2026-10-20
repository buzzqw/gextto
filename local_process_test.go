package gextto

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestLocalProcessUsageForOwnPID(t *testing.T) {
	pid := os.Getpid()
	first, ok := localProcessUsageForPIDs([]int{pid})
	if !ok {
		t.Fatal("current process not sampled")
	}
	if first.RSSBytes == 0 || len(first.PIDs) != 1 || first.PIDs[0] != pid {
		t.Fatalf("first sample = %+v", first)
	}
	if first.CPUPercent != nil {
		t.Fatalf("the first sample must not report a CPU percentage: %v", *first.CPUPercent)
	}
	// A call right after the first one is inside the minimum window: it keeps
	// the first sample instead of measuring a tiny, noisy interval.
	again, ok := localProcessUsageForPIDs([]int{pid})
	if !ok || again.CPUPercent != nil {
		t.Fatalf("a sample inside the minimum window must reuse the previous one: %+v", again)
	}
	// Burn a moment of CPU, move the stored sample past the window, then
	// sample again.
	deadline := time.Now().Add(20 * time.Millisecond)
	for time.Now().Before(deadline) {
		_ = os.Getpid()
	}
	key := localProcessKey([]int{pid})
	localProcessMu.Lock()
	sample := localProcessSamples[key]
	sample.at = sample.at.Add(-localProcessMinWindow - time.Second)
	localProcessSamples[key] = sample
	localProcessMu.Unlock()
	second, ok := localProcessUsageForPIDs([]int{pid})
	if !ok {
		t.Fatal("second sample failed")
	}
	if second.CPUPercent == nil {
		t.Fatal("the second sample must report a CPU percentage")
	}
	if *second.CPUPercent < 0 || *second.CPUPercent > 100 {
		t.Fatalf("cpu = %v", *second.CPUPercent)
	}
}

func TestLocalProcessSamplesOfGoneProcessesArePruned(t *testing.T) {
	localProcessMu.Lock()
	localProcessSamples["pid:999999999"] = localProcessSample{at: time.Now().Add(-localProcessSampleTTL - time.Minute)}
	localProcessMu.Unlock()
	if _, ok := localProcessUsageForPIDs([]int{os.Getpid()}); !ok {
		t.Fatal("current process not sampled")
	}
	// A fresh sample of the own pid may sit inside the minimum window and not
	// prune; force a new one.
	key := localProcessKey([]int{os.Getpid()})
	localProcessMu.Lock()
	sample := localProcessSamples[key]
	sample.at = sample.at.Add(-localProcessMinWindow - time.Second)
	localProcessSamples[key] = sample
	localProcessMu.Unlock()
	localProcessUsageForPIDs([]int{os.Getpid()})
	localProcessMu.Lock()
	_, stale := localProcessSamples["pid:999999999"]
	localProcessMu.Unlock()
	if stale {
		t.Fatal("the sample of a process not read for a long time must be dropped")
	}
}

func TestLocalProcessUsageForUnknownName(t *testing.T) {
	if _, ok := localProcessUsageForName("gextto-no-such-process"); ok {
		t.Fatal("a missing process was reported as running")
	}
}

func TestLocalProcessLoadLabel(t *testing.T) {
	if got := localProcessLoadLabel(localProcessUsage{RSSBytes: 1024}); !strings.HasPrefix(got, "— · ") {
		t.Fatalf("label without a CPU sample = %q", got)
	}
	value := 1.25
	got := localProcessLoadLabel(localProcessUsage{RSSBytes: 1024, CPUPercent: &value})
	if !strings.HasPrefix(got, "1.2% · ") {
		t.Fatalf("label = %q", got)
	}
}

func TestHostIsLocal(t *testing.T) {
	hostname, _ := os.Hostname()
	cases := map[string]bool{
		"":            true,
		"localhost":   true,
		"127.0.0.1":   true,
		"::1":         true,
		"192.0.2.1":   false,
		"amule.other": false,
		hostname:      true,
	}
	for host, want := range cases {
		if got := hostIsLocal(host); got != want {
			t.Fatalf("hostIsLocal(%q) = %v, want %v", host, got, want)
		}
	}
}
