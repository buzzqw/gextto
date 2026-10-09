//go:build cgo

package gextto

import (
	"testing"
	"time"
	"unsafe"
)

// Shutdown must wait for session calls already running and refuse new ones,
// so no cgo call reaches a destroyed handle.
func TestSessionDrainWaitsForRunningCallsAndRefusesNewOnes(t *testing.T) {
	marker := 1
	client := &LibtorrentClient{session: unsafe.Pointer(&marker)}
	if !client.enterSession() {
		t.Fatal("enterSession refused with a live session")
	}
	drained := make(chan struct{})
	go func() {
		client.drainSession(5 * time.Second)
		close(drained)
	}()
	deadline := time.Now().Add(time.Second)
	for {
		client.sessionUse.Lock()
		closing := client.sessionClosing
		client.sessionUse.Unlock()
		if closing || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if client.enterSession() {
		t.Fatal("enterSession accepted a new call while shutting down")
	}
	select {
	case <-drained:
		t.Fatal("drain returned while a call was still running")
	case <-time.After(100 * time.Millisecond):
	}
	client.exitSession()
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("drain did not return after the running call ended")
	}
}

// A call stuck past the timeout must not hang the daemon's exit.
func TestSessionDrainGivesUpAfterTimeout(t *testing.T) {
	marker := 1
	client := &LibtorrentClient{session: unsafe.Pointer(&marker)}
	if !client.enterSession() {
		t.Fatal("enterSession refused with a live session")
	}
	start := time.Now()
	client.drainSession(100 * time.Millisecond)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("drain took %s, want about the timeout", elapsed)
	}
}

func TestSessionGuardWithoutSession(t *testing.T) {
	client := &LibtorrentClient{}
	if client.enterSession() {
		t.Fatal("enterSession accepted a call without a session")
	}
}

// A worker stuck in uninterruptible work (a library copy) must not block the
// shutdown past workerStopTimeout: systemd would kill the daemon before the
// torrent resume data is saved.
func TestStopBackgroundWorkersDoesNotWaitForever(t *testing.T) {
	previous := workerStopTimeout
	workerStopTimeout = 100 * time.Millisecond
	defer func() { workerStopTimeout = previous }()
	state := &AppState{bgStop: make(chan struct{})}
	release := make(chan struct{})
	defer close(release)
	state.bgWG.Add(1)
	go func() {
		defer state.bgWG.Done()
		<-release
	}()
	start := time.Now()
	stopBackgroundWorkers(state)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("shutdown waited %s for a stuck worker", elapsed)
	}
}
