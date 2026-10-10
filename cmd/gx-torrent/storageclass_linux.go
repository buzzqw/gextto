//go:build linux

package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// localStorageClass tells a spinning disk from a solid-state one for the local
// filesystem backing path, reading the rotational flag of the whole disk.
func localStorageClass(path string) string {
	majmin := mountMajorMinor(path)
	if majmin == "" {
		return "unknown"
	}
	if rotationalDevice(majmin) {
		return "hdd"
	}
	return "ssd"
}

// mountMajorMinor returns the "major:minor" of the filesystem backing path.
func mountMajorMinor(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return ""
	}
	defer file.Close()
	best, bestLen := "", -1
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 5 {
			continue
		}
		mount := fields[4]
		if abs == mount || strings.HasPrefix(abs, strings.TrimSuffix(mount, "/")+"/") {
			if len(mount) > bestLen {
				best, bestLen = fields[2], len(mount)
			}
		}
	}
	return best
}

// rotationalDevice reports whether the device "major:minor" sits on a spinning
// disk. It resolves the partition symlink and reads the whole disk's flag.
func rotationalDevice(majmin string) bool {
	target, err := filepath.EvalSymlinks("/sys/dev/block/" + majmin)
	if err != nil {
		return false
	}
	parts := strings.Split(target, "/")
	for i, p := range parts {
		if p == "block" && i+1 < len(parts) {
			data, err := os.ReadFile("/sys/block/" + parts[i+1] + "/queue/rotational")
			return err == nil && strings.TrimSpace(string(data)) == "1"
		}
	}
	return false
}
