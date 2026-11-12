package torrent

import (
	"testing"
	"time"
)

// TestUnchokeRoundPausesWithTheTorrent: a stopped torrent's unchoke clock is
// silent, and starting it again brings the round back.
func TestUnchokeRoundPausesWithTheTorrent(t *testing.T) {
	tr := &torrent{unchokeTicker: time.NewTicker(time.Millisecond)}
	defer tr.unchokeTicker.Stop()

	tr.markStopped()
	if !tr.loopStopped.Load() {
		t.Fatal("a stopped torrent must tell the health check")
	}
	select {
	case <-tr.unchokeTicker.C:
	default:
	}
	select {
	case <-tr.unchokeTicker.C:
		t.Fatal("a paused unchoke round still ticks")
	case <-time.After(30 * time.Millisecond):
	}

	tr.markRunning()
	if tr.loopStopped.Load() {
		t.Fatal("a running torrent must be probed at the normal pace")
	}
	if got := len(tr.unchokeTicker.C); got != 0 {
		t.Fatalf("a resumed round must not fire at once, %d ticks pending", got)
	}

	// A torrent built without a run loop has no clock: both are no-ops.
	var bare torrent
	bare.markStopped()
	bare.markRunning()
}

// TestHealthProbeSkipsALoopThatJustAnswered: a command taken by the run loop
// stamps it alive, and the probe leaves alone a loop that answered within its
// interval; one that never answered, or long ago, is still probed.
func TestHealthProbeSkipsALoopThatJustAnswered(t *testing.T) {
	tr := &torrent{commandC: make(chan func(), 1), closeC: make(chan struct{})}
	now := time.Now()
	if answeredWithin(tr.answeredAt.Load(), now, 10*time.Second) {
		t.Fatal("a loop that never answered must be probed")
	}
	if !tr.sendCommand(func() {}) {
		t.Fatal("the command was not taken")
	}
	if !answeredWithin(tr.answeredAt.Load(), time.Now(), 10*time.Second) {
		t.Fatal("a loop that just took a command must not be probed")
	}
	if answeredWithin(tr.answeredAt.Load(), time.Now().Add(11*time.Second), 10*time.Second) {
		t.Fatal("a loop silent for longer than the interval must be probed")
	}
}

// TestHealthProbeSpacesOutAStoppedTorrent: the probe of a stopped torrent
// waits stoppedProbeFactor intervals, a running one the plain interval.
func TestHealthProbeSpacesOutAStoppedTorrent(t *testing.T) {
	if got := probeDelay(10*time.Second, false); got != 10*time.Second {
		t.Fatalf("running: %s", got)
	}
	if got := probeDelay(10*time.Second, true); got != time.Minute {
		t.Fatalf("stopped: %s", got)
	}
}

// TestUnchokeRoundSleepsWithoutPeers: a running torrent with nobody connected
// pauses its round, and the first peer brings it back.
func TestUnchokeRoundSleepsWithoutPeers(t *testing.T) {
	tr := &torrent{unchokeTicker: time.NewTicker(time.Millisecond)}
	defer tr.unchokeTicker.Stop()
	tr.markRunning()
	tr.idleUnchokeIfAlone()
	if !tr.unchokeIdle {
		t.Fatal("a torrent without peers must pause its round")
	}
	select {
	case <-tr.unchokeTicker.C:
	default:
	}
	select {
	case <-tr.unchokeTicker.C:
		t.Fatal("a paused round still ticks")
	case <-time.After(30 * time.Millisecond):
	}
	tr.wakeUnchoke()
	if tr.unchokeIdle {
		t.Fatal("the first peer must restart the round")
	}
	// A stopped torrent is not woken by a peer: start() turns the round on.
	tr.markStopped()
	tr.unchokeIdle = true
	tr.wakeUnchoke()
	if !tr.unchokeIdle {
		t.Fatal("a stopped torrent's round was restarted")
	}
}
