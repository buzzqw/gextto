package main

import (
	"strings"
	"testing"
	"time"
)

// TestDataLockRefusesASecondDaemon pins that a second gx-torrent on a data
// folder in use is refused with a sentence naming the folder, and that the
// folder is free again once the first one releases it.
func TestDataLockRefusesASecondDaemon(t *testing.T) {
	saved := dataLockWait
	dataLockWait = 300 * time.Millisecond
	t.Cleanup(func() { dataLockWait = saved })
	dir := t.TempDir()
	release, err := acquireDataLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireDataLock(dir); err == nil || !strings.Contains(err.Error(), "another gx-torrent is already using the data folder") {
		t.Fatalf("second lock on a folder in use: err = %v, want the refusal", err)
	}
	release()
	again, err := acquireDataLock(dir)
	if err != nil {
		t.Fatalf("the folder stays locked after its release: %v", err)
	}
	again()
}

// TestDataLockWaitsForTheOldDaemonToClose pins the restart handover: a new
// daemon started while the old one is still closing waits for the folder
// instead of failing.
func TestDataLockWaitsForTheOldDaemonToClose(t *testing.T) {
	saved := dataLockWait
	dataLockWait = 5 * time.Second
	t.Cleanup(func() { dataLockWait = saved })
	dir := t.TempDir()
	release, err := acquireDataLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(400 * time.Millisecond)
		release()
	}()
	started := time.Now()
	next, err := acquireDataLock(dir)
	if err != nil {
		t.Fatalf("the new daemon did not wait for the old one: %v", err)
	}
	next()
	if waited := time.Since(started); waited < 300*time.Millisecond {
		t.Fatalf("the lock was taken after %s, while the old daemon still held it", waited)
	}
}
