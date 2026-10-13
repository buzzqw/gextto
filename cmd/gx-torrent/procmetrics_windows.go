//go:build windows

package main

// procmetrics_windows.go reads this process' own memory and CPU time for the
// status bar, with the Win32 process APIs (WorkingSetSize and the kernel+user
// times), so it works alongside the native Windows service.

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processMemoryCounters mirrors PROCESS_MEMORY_COUNTERS from psapi.h.
type processMemoryCounters struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

var (
	psapi                = windows.NewLazySystemDLL("psapi.dll")
	getProcessMemoryInfo = psapi.NewProc("GetProcessMemoryInfo")
)

// processMemoryBytes is the resident set size (WorkingSetSize) of this process.
func processMemoryBytes() int64 {
	var counters processMemoryCounters
	counters.CB = uint32(unsafe.Sizeof(counters))
	ret, _, _ := getProcessMemoryInfo.Call(
		uintptr(windows.CurrentProcess()),
		uintptr(unsafe.Pointer(&counters)),
		uintptr(counters.CB),
	)
	if ret == 0 {
		return 0
	}
	return int64(counters.WorkingSetSize)
}

// processCPUTime is the total user+system CPU time used by this process.
func processCPUTime() time.Duration {
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &creation, &exit, &kernel, &user); err != nil {
		return 0
	}
	return time.Duration(kernel.Nanoseconds() + user.Nanoseconds())
}
