package main

// datalock.go keeps a second gx-torrent off a data folder already in use. Two
// daemons on the same folder (a service plus a copy started by hand on
// another port) both load state.json and both save it, and the one that
// cannot open its port still saves while it closes: each overwrites the other's
// torrents. The lock is held by the kernel for the life of the process, so a
// crash never leaves it behind; the file itself is never removed, or two
// processes in a restart race could each lock a different inode.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// dataLockFile is the name of the lock inside the data folder.
const dataLockFile = "gx-torrent.lock"

// dataLockWait is how long a start waits for the folder to be released. A
// managed restart stops the old daemon and waits for it, so the lock is
// normally free at once; the wait covers an old process still closing its
// storage when the new one is started.
var dataLockWait = 30 * time.Second

// errDataLocked is what tryLockFile returns when another process holds the
// lock.
var errDataLocked = errors.New("data folder locked")

// acquireDataLock takes the lock of dataDir and returns its release.
func acquireDataLock(dataDir string) (func(), error) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return func() {}, nil
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, dataLockFile)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("cannot open the lock %s: %w", path, err)
	}
	deadline := time.Now().Add(dataLockWait)
	announced := false
	for {
		err = tryLockFile(file)
		if err == nil {
			break
		}
		if !errors.Is(err, errDataLocked) {
			_ = file.Close()
			return nil, fmt.Errorf("cannot lock the data folder %s: %w", dataDir, err)
		}
		if !time.Now().Before(deadline) {
			_ = file.Close()
			return nil, fmt.Errorf("another gx-torrent is already using the data folder %s: stop it before starting this one", dataDir)
		}
		if !announced {
			logf("another gx-torrent is still using the data folder %s: waiting up to %s for it to close", dataDir, dataLockWait.Round(time.Second))
			announced = true
		}
		time.Sleep(250 * time.Millisecond)
	}
	// The PID is for a person looking at the folder; the lock is what keeps
	// the second process out, so a failed write does not refuse the start.
	_ = file.Truncate(0)
	_, _ = file.WriteAt([]byte(fmt.Sprintf("%d\n", os.Getpid())), 0)
	return func() {
		unlockFile(file)
		_ = file.Close()
	}, nil
}
