package gextto

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Health is the full health report exposed by the dashboard/health endpoints.
type Health struct {
	Status               string       `json:"status"`
	UnixTime             uint64       `json:"unix_time"`
	ProcessID            uint32       `json:"process_id"`
	ResidentBytes        uint64       `json:"resident_bytes"`
	MemoryTotalBytes     uint64       `json:"memory_total_bytes"`
	MemoryAvailableBytes uint64       `json:"memory_available_bytes"`
	DataDirWritable      bool         `json:"data_dir_writable"`
	DiskTotalBytes       uint64       `json:"disk_total_bytes"`
	DiskFreeBytes        uint64       `json:"disk_free_bytes"`
	UptimeSeconds        uint64       `json:"uptime_seconds"`
	ProcessUptimeSeconds uint64       `json:"process_uptime_seconds"`
	LoadAverage          *float64     `json:"load_average"`
	CPUPercent           *float64     `json:"cpu_percent"`
	ProcessCPUPercent    *float64     `json:"process_cpu_percent"`
	TrashFileCount       uint64       `json:"trash_file_count"`
	TrashBytes           uint64       `json:"trash_bytes"`
	Disks                []DiskInfo   `json:"disks"`
	Paths                []PathCheck  `json:"paths"`
	Ramdisk              *RamDiskInfo `json:"ramdisk"`
	LastErrors           []string     `json:"last_errors"`
}

// ProcessMetricsSnapshot holds the lightweight process-only metrics used by the
// always-visible UI status bar. It is the Go implementation of the `ProcessMetrics`
// struct; the type is named differently because Go does not allow a struct and
// the `ProcessMetrics()` function to share one package-level identifier.
type ProcessMetricsSnapshot struct {
	ResidentBytes     uint64   `json:"resident_bytes"`
	ProcessCPUPercent *float64 `json:"process_cpu_percent"`
	// Go runtime figures help distinguish the daemon's own footprint from the
	// embedded libtorrent session, whose cache grows during transfers.
	GoHeapBytes      uint64 `json:"go_heap_bytes"`
	GoHeapInuseBytes uint64 `json:"go_heap_inuse_bytes"`
	GoGoroutines     int    `json:"go_goroutines"`
	GoGCCount        uint32 `json:"go_gc_count"`
}

// DiskInfo describes a real mounted filesystem.
type DiskInfo struct {
	Mount      string `json:"mount"`
	Filesystem string `json:"filesystem"`
	TotalBytes uint64 `json:"total_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
}

// PathCheck reports existence and writability of an operational path.
type PathCheck struct {
	Label    string `json:"label"`
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	Writable bool   `json:"writable"`
}

// RamDiskInfo describes the optional libtorrent RAM disk.
type RamDiskInfo struct {
	Path       string `json:"path"`
	TotalBytes uint64 `json:"total_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
}

// CpuSample is the last system CPU reading from /proc/stat.
type CpuSample struct {
	total   uint64
	idle    uint64
	at      time.Time
	percent *float64
}

var (
	cpuSampleMu sync.Mutex
	cpuSample   *CpuSample
)

// ProcessCpuSample is the last process CPU reading from /proc/self/stat.
type ProcessCpuSample struct {
	ticks   uint64
	at      time.Time
	percent *float64
}

var (
	processCPUSampleMu sync.Mutex
	processCPUSample   *ProcessCpuSample
)

// healthClockTicks mirrors libc sysconf(_SC_CLK_TCK). On Linux USER_HZ is a
// fixed kernel constant of 100 across every supported architecture.
func healthClockTicks() int64 { return 100 }

// healthPageSize mirrors libc sysconf(_SC_PAGESIZE).max(0).
func healthPageSize() uint64 {
	size := os.Getpagesize()
	if size < 0 {
		return 0
	}
	return uint64(size)
}

// currentCPUFrequency returns the current frequency of the first logical CPU
// in a compact label suitable for the always-visible UI chrome. Linux exposes
// a live cpufreq value in kHz; /proc/cpuinfo is the fallback used by systems
// without the cpufreq sysfs driver.
func currentCPUFrequency() string {
	if raw, err := os.ReadFile("/sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq"); err == nil {
		if value, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64); err == nil && value > 0 {
			return fmt.Sprintf("%.2f GHz", value/1_000_000)
		}
	}
	if raw, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 || !strings.EqualFold(strings.TrimSpace(parts[0]), "cpu MHz") {
				continue
			}
			value, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
			if err == nil && value > 0 {
				return fmt.Sprintf("%.2f GHz", value/1_000)
			}
		}
	}
	return ""
}

func healthSatSub(a, b uint64) uint64 {
	if a < b {
		return 0
	}
	return a - b
}

func healthSatAdd(a, b uint64) uint64 {
	sum := a + b
	if sum < a {
		return ^uint64(0)
	}
	return sum
}

func healthSatMul(a, b uint64) uint64 {
	if a == 0 || b == 0 {
		return 0
	}
	if a > ^uint64(0)/b {
		return ^uint64(0)
	}
	return a * b
}

func healthClampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func healthFloatPtr(value float64) *float64 { return &value }

// cpu_snapshot reads `(total, idle)` from `/proc/stat`.
func cpu_snapshot() (uint64, uint64, bool) {
	contents, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	lines := strings.Split(string(contents), "\n")
	if len(lines) == 0 {
		return 0, 0, false
	}
	parts := strings.Fields(lines[0])
	if len(parts) == 0 || parts[0] != "cpu" {
		return 0, 0, false
	}
	values := make([]uint64, 0, len(parts)-1)
	for _, value := range parts[1:] {
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			continue
		}
		values = append(values, parsed)
	}
	if len(values) < 4 {
		return 0, 0, false
	}
	var total uint64
	for _, value := range values {
		total = healthSatAdd(total, value)
	}
	idle := values[3]
	if len(values) > 4 {
		idle = healthSatAdd(idle, values[4])
	}
	return total, idle, true
}

// system_cpu_percent is the system CPU usage percent computed from the delta of
// two consecutive /proc/stat readings. Close calls reuse the previous value; the
// very first call performs a short measurement so it never returns nil.
func system_cpu_percent() *float64 {
	total, idle, ok := cpu_snapshot()
	if !ok {
		return nil
	}
	now := time.Now()
	cpuSampleMu.Lock()
	if previous := cpuSample; previous != nil {
		elapsed := now.Sub(previous.at).Seconds()
		deltaTotal := float64(healthSatSub(total, previous.total))
		deltaIdle := float64(healthSatSub(idle, previous.idle))
		var result *float64
		if elapsed >= 0.2 && deltaTotal > 0.0 {
			result = healthFloatPtr(healthClampPercent((deltaTotal - deltaIdle) / deltaTotal * 100.0))
		} else {
			result = previous.percent
		}
		cpuSample = &CpuSample{total: total, idle: idle, at: now, percent: result}
		cpuSampleMu.Unlock()
		return result
	}
	cpuSampleMu.Unlock()
	// First sample: measure over a short window so a value is available at once.
	time.Sleep(150 * time.Millisecond)
	totalLater, idleLater, ok := cpu_snapshot()
	if !ok {
		return nil
	}
	deltaTotal := float64(healthSatSub(totalLater, total))
	deltaIdle := float64(healthSatSub(idleLater, idle))
	var result *float64
	if deltaTotal > 0.0 {
		result = healthFloatPtr(healthClampPercent((deltaTotal - deltaIdle) / deltaTotal * 100.0))
	}
	cpuSampleMu.Lock()
	cpuSample = &CpuSample{
		total:   totalLater,
		idle:    idleLater,
		at:      time.Now(),
		percent: result,
	}
	cpuSampleMu.Unlock()
	return result
}

// process_cpu_percent is the CPU percent of the current process, computed from
// its utime+stime ticks. The value refers to a single core (it can exceed 100%
// with multiple threads).
func process_cpu_percent() *float64 {
	contents, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return nil
	}
	text := string(contents)
	commandEnd := strings.LastIndex(text, ")")
	if commandEnd < 0 || commandEnd+2 > len(text) {
		return nil
	}
	fields := strings.Fields(text[commandEnd+2:])
	if len(fields) < 13 {
		return nil
	}
	// After pid and comm, fields[0] is state (field 3); utime/stime are procfs
	// fields 14/15, hence indexes 11/12 here.
	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return nil
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return nil
	}
	ticks := healthSatAdd(utime, stime)
	now := time.Now()
	clockTicks := float64(healthClockTicks())
	if clockTicks < 1 {
		clockTicks = 1
	}
	processCPUSampleMu.Lock()
	previous := processCPUSample
	if previous == nil {
		processCPUSample = &ProcessCpuSample{ticks: ticks, at: now, percent: nil}
		processCPUSampleMu.Unlock()
		return nil
	}
	elapsed := now.Sub(previous.at).Seconds()
	var percent *float64
	if elapsed > 0.0 {
		value := float64(healthSatSub(ticks, previous.ticks)) / clockTicks / elapsed * 100.0
		if value < 0 {
			value = 0
		}
		percent = healthFloatPtr(value)
	} else {
		percent = previous.percent
	}
	processCPUSample = &ProcessCpuSample{ticks: ticks, at: now, percent: percent}
	processCPUSampleMu.Unlock()
	return percent
}

// process_uptime_seconds returns seconds since the current process started,
// using procfs start ticks.
func process_uptime_seconds() uint64 {
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0
	}
	text := string(data)
	commandEnd := strings.LastIndex(text, ")")
	if commandEnd < 0 || commandEnd+2 > len(text) {
		return 0
	}
	fields := strings.Fields(text[commandEnd+2:])
	if len(fields) < 20 {
		return 0
	}
	// After pid and comm, fields[0] is state; starttime is procfs field 22,
	// therefore index 19 in this slice.
	startTicks, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0
	}
	clockTicks := healthClockTicks()
	if clockTicks <= 0 {
		return 0
	}
	systemUptime := 0.0
	if uptimeData, err := os.ReadFile("/proc/uptime"); err == nil {
		parts := strings.Fields(string(uptimeData))
		if len(parts) > 0 {
			if parsed, err := strconv.ParseFloat(parts[0], 64); err == nil {
				systemUptime = parsed
			}
		}
	}
	value := systemUptime - float64(startTicks)/float64(clockTicks)
	if value < 0 {
		value = 0
	}
	return uint64(value)
}

// HealthPaths is the context of the paths inspected by the health check.
type HealthPaths struct {
	DataDir      string
	TrashPath    string
	DownloadPath string
	ArchiveRoot  string
	RamdiskPath  string
}

// Check runs the health check for the given data directory.
func Check(dataDir string) Health {
	trash := filepath.Join(dataDir, "trash")
	return CheckWithPaths(&HealthPaths{
		DataDir:      dataDir,
		TrashPath:    trash,
		DownloadPath: dataDir,
	})
}

// ProcessMetrics returns the lightweight process-only metrics for the
// always-visible UI status bar. Unlike CheckWithPaths it does not inspect disks,
// paths or trash.
func ProcessMetrics() ProcessMetricsSnapshot {
	resident := healthResidentBytes()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	return ProcessMetricsSnapshot{
		ResidentBytes:     resident,
		ProcessCPUPercent: process_cpu_percent(),
		GoHeapBytes:       memory.HeapAlloc,
		GoHeapInuseBytes:  memory.HeapInuse,
		GoGoroutines:      runtime.NumGoroutine(),
		GoGCCount:         memory.NumGC,
	}
}

func healthResidentBytes() uint64 {
	contents, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(contents))
	if len(fields) < 2 {
		return 0
	}
	value, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return healthSatMul(value, healthPageSize())
}

// CheckWithPaths runs the full health check over the supplied operational paths.
func CheckWithPaths(paths *HealthPaths) Health {
	dataDir := paths.DataDir
	trashPath := paths.TrashPath
	downloadPath := paths.DownloadPath
	info, statErr := os.Stat(dataDir)
	writable := statErr == nil && info.IsDir() && write_probe(dataDir)
	diskTotalBytes, diskFreeBytes := disk_space(downloadPath)
	resident := healthResidentBytes()
	uptimeSeconds := healthUptimeSeconds()
	loadAverage := healthLoadAverage()
	trashFileCount, trashBytes := tree_stats(trashPath)
	memoryTotalBytes, memoryAvailableBytes := memory_info()

	status := "degraded"
	if writable {
		status = "ok"
	}
	var ramdisk *RamDiskInfo
	if paths.RamdiskPath != "" {
		if healthIsDir(paths.RamdiskPath) {
			totalBytes, freeBytes := disk_space(paths.RamdiskPath)
			ramdisk = &RamDiskInfo{
				Path:       paths.RamdiskPath,
				TotalBytes: totalBytes,
				FreeBytes:  freeBytes,
			}
		}
	}
	return Health{
		Status:               status,
		UnixTime:             uint64(time.Now().Unix()),
		ProcessID:            uint32(os.Getpid()),
		ResidentBytes:        resident,
		MemoryTotalBytes:     memoryTotalBytes,
		MemoryAvailableBytes: memoryAvailableBytes,
		DataDirWritable:      writable,
		DiskTotalBytes:       diskTotalBytes,
		DiskFreeBytes:        diskFreeBytes,
		UptimeSeconds:        uptimeSeconds,
		ProcessUptimeSeconds: process_uptime_seconds(),
		LoadAverage:          loadAverage,
		CPUPercent:           system_cpu_percent(),
		ProcessCPUPercent:    process_cpu_percent(),
		TrashFileCount:       trashFileCount,
		TrashBytes:           trashBytes,
		Disks:                disks(),
		Paths:                path_checks(paths),
		Ramdisk:              ramdisk,
		LastErrors:           recent_errors(filepath.Join(dataDir, "gextto.log"), 10),
	}
}

func healthUptimeSeconds() uint64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	parts := strings.Fields(string(data))
	if len(parts) == 0 {
		return 0
	}
	value, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0
	}
	return uint64(value)
}

func healthLoadAverage() *float64 {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil
	}
	parts := strings.Fields(string(data))
	if len(parts) == 0 {
		return nil
	}
	value, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return nil
	}
	return &value
}

func healthIsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// path_checks verifies existence and writability of the operational paths. The
// write probe is limited to local paths so NAS mounts are not disturbed.
func path_checks(paths *HealthPaths) []PathCheck {
	checks := []PathCheck{}
	add := func(label, path string, probe bool) {
		exists := healthIsDir(path)
		writable := exists && (!probe || write_probe(path))
		checks = append(checks, PathCheck{
			Label:    label,
			Path:     path,
			Exists:   exists,
			Writable: writable,
		})
	}
	add("Data", paths.DataDir, true)
	add("Download", paths.DownloadPath, true)
	add("Trash", paths.TrashPath, true)
	if paths.ArchiveRoot != "" {
		add("Archivio", paths.ArchiveRoot, false)
	}
	if paths.RamdiskPath != "" {
		add("RAM disk", paths.RamdiskPath, false)
	}
	return checks
}

// recent_errors returns the last `limit` ERROR log lines, most recent last.
func recent_errors(logPath string, limit int) []string {
	contents, err := os.ReadFile(logPath)
	if err != nil {
		return []string{}
	}
	errors := []string{}
	for _, line := range strings.Split(string(contents), "\n") {
		if strings.Contains(line, "ERROR") {
			errors = append(errors, line)
		}
	}
	if len(errors) > limit {
		errors = errors[len(errors)-limit:]
	}
	return errors
}

// disks lists real mounted filesystems (pseudo-fs excluded), deduplicated by
// device, with total/free space. Used by the dashboard and the health page.
func disks() []DiskInfo {
	ignored := []string{
		"proc",
		"sysfs",
		"devtmpfs",
		"devpts",
		"tmpfs",
		"ramfs",
		"cgroup",
		"cgroup2",
		"pstore",
		"securityfs",
		"debugfs",
		"tracefs",
		"configfs",
		"fusectl",
		"mqueue",
		"hugetlbfs",
		"binfmt_misc",
		"autofs",
		"squashfs",
		"efivarfs",
		"bpf",
		"overlay",
	}
	contents, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return []DiskInfo{}
	}
	seenDevices := []string{}
	result := []DiskInfo{}
	for _, line := range strings.Split(string(contents), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		device := parts[0]
		mount := parts[1]
		filesystem := parts[2]
		if healthContains(ignored, filesystem) ||
			!strings.HasPrefix(mount, "/") ||
			healthContains(seenDevices, device) {
			continue
		}
		mount = strings.ReplaceAll(mount, `\040`, " ")
		totalBytes, freeBytes := disk_space(mount)
		if totalBytes == 0 {
			continue
		}
		seenDevices = append(seenDevices, device)
		result = append(result, DiskInfo{
			Mount:      mount,
			Filesystem: filesystem,
			TotalBytes: totalBytes,
			FreeBytes:  freeBytes,
		})
		if len(result) >= 12 {
			break
		}
	}
	return result
}

func healthContains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func memory_info() (uint64, uint64) {
	contents, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	field := func(name string) uint64 {
		for _, line := range strings.Split(string(contents), "\n") {
			if !strings.HasPrefix(line, name) {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) < 2 {
				return 0
			}
			value, err := strconv.ParseUint(parts[1], 10, 64)
			if err != nil {
				return 0
			}
			return healthSatMul(value, 1024)
		}
		return 0
	}
	return field("MemTotal:"), field("MemAvailable:")
}

func tree_stats(path string) (uint64, uint64) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0, 0
	}
	files := uint64(0)
	bytes := uint64(0)
	for _, entry := range entries {
		entryPath := filepath.Join(path, entry.Name())
		if healthIsDir(entryPath) {
			nestedFiles, nestedBytes := tree_stats(entryPath)
			files = healthSatAdd(files, nestedFiles)
			bytes = healthSatAdd(bytes, nestedBytes)
			continue
		}
		info, err := os.Stat(entryPath)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files++
		if linkInfo, err := entry.Info(); err == nil {
			size := linkInfo.Size()
			if size > 0 {
				bytes = healthSatAdd(bytes, uint64(size))
			}
		}
	}
	return files, bytes
}

func write_probe(dataDir string) bool {
	path := filepath.Join(dataDir, fmt.Sprintf(".health-probe-%d", os.Getpid()))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return false
	}
	syncErr := file.Sync()
	_ = file.Close()
	if syncErr != nil {
		return false
	}
	return os.Remove(path) == nil
}

func disk_space(path string) (uint64, uint64) {
	var stats syscall.Statfs_t
	if err := syscall.Statfs(path, &stats); err != nil {
		return 0, 0
	}
	blockSize := uint64(stats.Frsize)
	return stats.Blocks * blockSize, stats.Bavail * blockSize
}
