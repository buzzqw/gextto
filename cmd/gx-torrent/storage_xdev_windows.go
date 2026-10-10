//go:build windows

package main

import (
	"errors"
	"os"
	"syscall"
)

// errnoNotSameDevice is Windows' ERROR_NOT_SAME_DEVICE (17): the error a
// cross-volume MoveFile reports. Go maps it to a syscall.Errno, not to EXDEV.
const errnoNotSameDevice = syscall.Errno(0x11)

// isCrossDevice reports whether err is the "rename across volumes" error, so
// the caller falls back to a copy.
func isCrossDevice(err error) bool {
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return errors.Is(linkErr.Err, syscall.EXDEV) || errors.Is(linkErr.Err, errnoNotSameDevice)
	}
	return errors.Is(err, syscall.EXDEV) || errors.Is(err, errnoNotSameDevice)
}
