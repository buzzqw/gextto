package dht

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempLeftovers(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "taipeitorrent") {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestSaveStoreLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	saveStore(dhtStore{path: dir, Port: 6881})
	if _, err := os.Stat(filepath.Join(dir, "dht-6881")); err != nil {
		t.Fatalf("state not written: %v", err)
	}
	if left := tempLeftovers(t, dir); len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
}

// When the new state cannot take the old one's place, the old one stays and
// the temporary file goes.
func TestFailedSaveStoreKeepsThePreviousStateAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, fmt.Sprintf("dht-%d", 6881))
	// A non-empty directory cannot be replaced by a rename, nor moved onto
	// another non-empty directory: both steps of the save fail.
	for _, d := range []string{target, target + ".old"} {
		if err := os.MkdirAll(filepath.Join(d, "keep"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	saveStore(dhtStore{path: dir, Port: 6881})
	if _, err := os.Stat(filepath.Join(target, "keep")); err != nil {
		t.Fatalf("the previous state was lost: %v", err)
	}
	if left := tempLeftovers(t, dir); len(left) != 0 {
		t.Fatalf("temporary files left after a failed save: %v", left)
	}
}

// A node whose Start failed never ran the loop that stops the throttle:
// Stop must end the throttle's cleanup goroutine anyway.
func TestStopEndsTheThrottleAfterAFailedStart(t *testing.T) {
	d := newTestDHT()
	d.stop = make(chan bool)
	d.clientThrottle = newClientThrottle(10, 10)
	d.Stop()
	select {
	case <-d.clientThrottle.stop:
	default:
		t.Fatal("the throttle cleanup is still running after Stop")
	}
}
