//go:build !windows

package main

import (
	"errors"
	"os"
	"syscall"
)

// isCrossDevice reports whether err is the "rename across filesystems" error,
// so the caller falls back to a copy.
func isCrossDevice(err error) bool {
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return errors.Is(linkErr.Err, syscall.EXDEV)
	}
	return errors.Is(err, syscall.EXDEV)
}
