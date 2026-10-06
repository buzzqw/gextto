package gextto

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestIsTransientCompletionError(t *testing.T) {
	transient := []error{
		&os.PathError{Op: "write", Path: "/mnt/nas/x", Err: syscall.EIO},
		fmt.Errorf("copy: %w", &os.PathError{Op: "write", Path: "/x", Err: syscall.ENOSPC}),
		&os.PathError{Op: "stat", Path: "/mnt/nas", Err: syscall.ESTALE},
		context.DeadlineExceeded,
		errors.New("database is locked (5) (SQLITE_BUSY)"),
	}
	for _, err := range transient {
		if !isTransientCompletionError(err) {
			t.Errorf("expected transient: %v", err)
		}
	}
	permanent := []error{
		nil,
		errors.New("season pack filenames do not match declared season"),
		&os.PathError{Op: "open", Path: "/x", Err: syscall.ENOENT},
		&os.PathError{Op: "open", Path: "/x", Err: syscall.EACCES},
	}
	for _, err := range permanent {
		if isTransientCompletionError(err) {
			t.Errorf("expected permanent: %v", err)
		}
	}
}

func TestCompletionRetryDelay(t *testing.T) {
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, 32 * time.Minute}
	for i, expected := range want {
		if got := completionRetryDelay(i + 1); got != expected {
			t.Errorf("attempt %d: got %s, want %s", i+1, got, expected)
		}
	}
}
