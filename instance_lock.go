package gextto

// A second Gextto on the same data folder would run a second acquisition
// cycle, write the same state files and start a second gx-torrent handover:
// the interface port alone does not catch it when the two instances listen on
// different addresses. The lock is an advisory flock on a file inside the data
// folder. The kernel releases it when the process exits, so a crash never
// leaves a stale lock behind.
//
// The file is not removed on purpose: deleting it would let two processes
// open different inodes during a restart race, each holding its own lock.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// InstanceLockFile is the name of the lock inside the data folder.
const InstanceLockFile = "gextto.instance.lock"

// AcquireInstanceLock takes the data-folder lock and returns the function that
// releases it. A second process gets a sentence naming the folder, not a raw
// EWOULDBLOCK.
func AcquireInstanceLock(dataDir string) (func(), error) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return nil, errors.New("the data folder is empty: cannot guard against a second instance")
	}
	path := filepath.Join(dataDir, InstanceLockFile)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("cannot open the instance lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, fmt.Errorf("another Gextto is already using the data folder %s: stop it before starting", dataDir)
		}
		return nil, fmt.Errorf("cannot lock the data folder %s: %w", dataDir, err)
	}
	// The PID is diagnostic only; the flock is what keeps the second process
	// out. A failed write must not refuse a start.
	_ = file.Truncate(0)
	if _, err := file.WriteAt([]byte(fmt.Sprintf("%d\n", os.Getpid())), 0); err == nil {
		_ = file.Sync()
	}
	release := func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}
	return release, nil
}
