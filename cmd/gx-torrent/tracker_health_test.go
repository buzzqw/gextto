package main

import (
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/engine/torrent"
)

func newTestDaemonForTrackers() *Daemon {
	return &Daemon{
		selection: map[string][]bool{},
		runtime:   map[string]*runtimeInfo{},
		moving:    map[string]bool{},
		trackers:  newTrackerHealth(),
	}
}

func TestTrackerHealthDisablesAfterWindow(t *testing.T) {
	d := newTestDaemonForTrackers()
	now := time.Now()
	collected := map[string]collectedTorrent{
		"hash": {trackers: []torrent.Tracker{{URL: "http://bad.example/announce", Status: torrent.NotWorking}}},
	}

	// First observation starts the failure clock.
	if got := d.updateTrackerHealth(collected, now); len(got) != 0 {
		t.Fatalf("disabled too early: %v", got)
	}
	// Still inside the window: not disabled.
	if got := d.updateTrackerHealth(collected, now.Add(30*time.Minute)); len(got) != 0 {
		t.Fatalf("disabled inside the window: %v", got)
	}
	// Past the window: disabled.
	got := d.updateTrackerHealth(collected, now.Add(trackerFailWindow+time.Minute))
	if !got["http://bad.example/announce"] {
		t.Fatalf("tracker not disabled: %v", got)
	}

	// A tracker that works again clears the state.
	collected["hash"] = collectedTorrent{
		trackers: []torrent.Tracker{{URL: "http://bad.example/announce", Status: torrent.Working}},
	}
	if got := d.updateTrackerHealth(collected, now.Add(trackerFailWindow+2*time.Minute)); len(got) != 0 {
		t.Fatalf("a working tracker must not stay disabled: %v", got)
	}
}

// A tracker that answered at least once is only having a bad spell: it must not
// be dropped automatically (a temporary outage would become permanent).
func TestTrackerHealthKeepsTrackersThatWorkedOnce(t *testing.T) {
	d := newTestDaemonForTrackers()
	now := time.Now()
	url := "http://good.example/announce"
	d.updateTrackerHealth(map[string]collectedTorrent{
		"hash": {trackers: []torrent.Tracker{{URL: url, Status: torrent.Working}}},
	}, now)
	collected := map[string]collectedTorrent{
		"hash": {trackers: []torrent.Tracker{{URL: url, Status: torrent.NotWorking}}},
	}
	d.updateTrackerHealth(collected, now.Add(time.Minute))
	if got := d.updateTrackerHealth(collected, now.Add(trackerFailWindow+time.Hour)); len(got) != 0 {
		t.Fatalf("a tracker that worked once must not be disabled: %v", got)
	}
}

func TestTrackerHealthPersistence(t *testing.T) {
	health := newTrackerHealth()
	health.disabled["http://x.example/announce"] = time.Unix(1000, 0)
	snapshot := health.disabledSnapshot()
	if snapshot["http://x.example/announce"] != 1000 {
		t.Fatalf("snapshot = %v", snapshot)
	}
	restored := newTrackerHealth()
	restored.loadDisabled(snapshot)
	if until, ok := restored.disabled["http://x.example/announce"]; !ok || !until.Equal(time.Unix(1000, 0)) {
		t.Fatalf("restored disabled = %v", restored.disabled)
	}
}

func TestTrackerHealthNilSafe(t *testing.T) {
	var d Daemon
	if got := d.updateTrackerHealth(map[string]collectedTorrent{}, time.Now()); got != nil {
		t.Fatalf("nil tracker health = %v", got)
	}
}
