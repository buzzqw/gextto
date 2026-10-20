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
	// Burn a moment of CPU so the delta is measurable, then sample again.
	deadline := time.Now().Add(20 * time.Millisecond)
	for time.Now().Before(deadline) {
		_ = os.Getpid()
	}
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
