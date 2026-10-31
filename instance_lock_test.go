package gextto

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstanceLockRefusesASecondInstance(t *testing.T) {
	dir := t.TempDir()
	release, err := AcquireInstanceLock(dir)
	if err != nil {
		t.Fatalf("first AcquireInstanceLock: %v", err)
	}
	defer release()

	if _, err := AcquireInstanceLock(dir); err == nil {
		t.Fatal("second AcquireInstanceLock succeeded on the same data folder")
	} else if !strings.Contains(err.Error(), dir) || !strings.Contains(err.Error(), "already using") {
		t.Fatalf("second lock error does not name the folder and the cause: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, InstanceLockFile))
	if err != nil {
		t.Fatalf("lock file missing: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("lock file carries no PID")
	}
}

func TestInstanceLockIsReleased(t *testing.T) {
	dir := t.TempDir()
	release, err := AcquireInstanceLock(dir)
	if err != nil {
		t.Fatalf("first AcquireInstanceLock: %v", err)
	}
	release()

	again, err := AcquireInstanceLock(dir)
	if err != nil {
		t.Fatalf("AcquireInstanceLock after release: %v", err)
	}
	again()
}

func TestInstanceLockRequiresADataFolder(t *testing.T) {
	if _, err := AcquireInstanceLock("  "); err == nil {
		t.Fatal("AcquireInstanceLock accepted an empty data folder")
	}
}
