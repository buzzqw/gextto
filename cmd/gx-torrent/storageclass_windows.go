//go:build windows

package main

import (
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	ioctlStorageQueryProperty        = 0x2D1400
	storageDeviceSeekPenaltyProperty = 7
)

// storagePropertyQuery mirrors Windows' STORAGE_PROPERTY_QUERY.
type storagePropertyQuery struct {
	PropertyID           uint32
	QueryType            uint32
	AdditionalParameters [8]byte
}

// deviceSeekPenaltyDescriptor mirrors Windows' DEVICE_SEEK_PENALTY_DESCRIPTOR.
type deviceSeekPenaltyDescriptor struct {
	Version           uint32
	Size              uint32
	IncursSeekPenalty bool
	_                 [3]byte
}

// localStorageClass tells a spinning disk from a solid-state one for the local
// volume backing path, via the device's seek-penalty property. It returns
// "unknown" when the property cannot be read, so the cache policy stays
// conservative instead of guessing.
func localStorageClass(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "unknown"
	}
	volume := filepath.VolumeName(abs)
	if volume == "" {
		return "unknown"
	}
	device, err := windows.UTF16PtrFromString(`\\.\` + volume)
	if err != nil {
		return "unknown"
	}
	handle, err := windows.CreateFile(device, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return "unknown"
	}
	defer windows.CloseHandle(handle)
	query := storagePropertyQuery{PropertyID: storageDeviceSeekPenaltyProperty}
	var descriptor deviceSeekPenaltyDescriptor
	var returned uint32
	err = windows.DeviceIoControl(
		handle,
		ioctlStorageQueryProperty,
		(*byte)(unsafe.Pointer(&query)), uint32(unsafe.Sizeof(query)),
		(*byte)(unsafe.Pointer(&descriptor)), uint32(unsafe.Sizeof(descriptor)),
		&returned, nil,
	)
	if err != nil {
		return "unknown"
	}
	if descriptor.IncursSeekPenalty {
		return "hdd"
	}
	return "ssd"
}
