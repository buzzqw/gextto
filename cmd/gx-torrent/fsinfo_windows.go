//go:build windows

package main

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// diskFreeBytes returns the free (available to the user) and total bytes of the
// volume holding path. Zeroes when the volume cannot be queried.
func diskFreeBytes(path string) (free, total int64) {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0
	}
	var freeToCaller, totalBytes, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(ptr, &freeToCaller, &totalBytes, &totalFree); err != nil {
		return 0, 0
	}
	return int64(freeToCaller), int64(totalBytes)
}

// networkFilesystem reports whether path sits on a network drive (a mapped
// share or UNC path), so the cache policy can lean larger.
func networkFilesystem(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	volume := filepath.VolumeName(abs)
	if volume == "" {
		return false
	}
	ptr, err := windows.UTF16PtrFromString(volume + `\`)
	if err != nil {
		return false
	}
	return windows.GetDriveType(ptr) == windows.DRIVE_REMOTE
}
