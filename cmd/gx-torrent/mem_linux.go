//go:build linux

package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// memoryTotal reads MemTotal from /proc/meminfo (0 when unknown).
func memoryTotal() int64 {
	return meminfoKib("MemTotal:") * 1024
}

// memoryAvailable reads MemAvailable (reclaimable memory) or falls back to
// MemFree. MemAvailable is the right signal: it accounts for the page cache
// that can be dropped under pressure, so the cache grows only when memory is
// genuinely free.
func memoryAvailable() int64 {
	if v := meminfoKib("MemAvailable:"); v > 0 {
		return v * 1024
	}
	return meminfoKib("MemFree:") * 1024
}

func meminfoKib(key string) int64 {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == key {
			var kib int64
			if _, err := fmt.Sscanf(fields[1], "%d", &kib); err == nil {
				return kib
			}
		}
	}
	return 0
}
