//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// memoryStatusEx mirrors Windows' MEMORYSTATUSEX.
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

var (
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	globalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
)

func readMemoryStatus() (total, available int64) {
	var status memoryStatusEx
	status.Length = uint32(unsafe.Sizeof(status))
	ret, _, _ := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if ret == 0 {
		return 0, 0
	}
	return int64(status.TotalPhys), int64(status.AvailPhys)
}

// memoryTotal is the physical memory of the machine (0 when unknown).
func memoryTotal() int64 {
	total, _ := readMemoryStatus()
	return total
}

// memoryAvailable is the free physical memory (0 when unknown). Windows does
// not expose a reclaimable-cache figure like Linux' MemAvailable, so this is
// the plain free count: the adaptive policy still reacts to real pressure.
func memoryAvailable() int64 {
	_, available := readMemoryStatus()
	return available
}
