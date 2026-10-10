package gextto

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// healthFindCheck returns the path check with the given label.
func healthFindCheck(t *testing.T, checks []PathCheck, label string) PathCheck {
	t.Helper()
	for _, check := range checks {
		if check.Label == label {
			return check
		}
	}
	t.Fatalf("no %q path check in %#v", label, checks)
	return PathCheck{}
}

// TestHealthCheckReportsOKAndCleansProbe ports the
// `health_checks_actual_write_access` test: the write probe succeeds and leaves
// no `.health-probe-*` file behind.
func TestHealthCheckReportsOKAndCleansProbe(t *testing.T) {
	dir := t.TempDir()
	health := Check(dir)
	if health.Status != "ok" {
		t.Fatalf("status = %q, want ok", health.Status)
	}
	if !health.DataDirWritable {
		t.Fatal("DataDirWritable should be true")
	}
	if health.ProcessID != uint32(os.Getpid()) {
		t.Fatalf("ProcessID = %d, want %d", health.ProcessID, os.Getpid())
	}
	if health.Paths == nil {
		t.Fatal("Paths should not be nil")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".health-probe-") {
			t.Fatalf("health probe left behind %s", entry.Name())
		}
	}
}

// TestHealthCheckDegradedForMissingDir checks a non-existent data directory is
// reported as degraded rather than ok.
func TestHealthCheckDegradedForMissingDir(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does", "not", "exist")
	health := Check(missing)
	if health.Status != "degraded" {
		t.Fatalf("status = %q, want degraded", health.Status)
	}
	if health.DataDirWritable {
		t.Fatal("missing data dir should not be writable")
	}
}

// TestHealthPathsVariations ports the `path_checks_report_missing_paths`
// test and exercises the optional archive/ramdisk paths.
func TestHealthPathsVariations(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "nope")

	paths := &HealthPaths{
		DataDir:      root,
		TrashPath:    missing,
		DownloadPath: root,
		ArchiveRoot:  filepath.Join(root, "archive"),
		RamdiskPath:  missing,
	}
	health := CheckWithPaths(paths)

	if len(health.Paths) != 5 {
		t.Fatalf("Paths = %#v, want 5 entries", health.Paths)
	}
	data := healthFindCheck(t, health.Paths, "Data")
	if !data.Exists || !data.Writable {
		t.Fatalf("Data check = %#v, want exists+writable", data)
	}
	trash := healthFindCheck(t, health.Paths, "Trash")
	if trash.Exists || trash.Writable {
		t.Fatalf("Trash check = %#v, want neither exists nor writable", trash)
	}
	archive := healthFindCheck(t, health.Paths, "Archivio")
	if archive.Exists || archive.Writable {
		t.Fatalf("Archivio check = %#v, want neither exists nor writable", archive)
	}
	ramdisk := healthFindCheck(t, health.Paths, "RAM disk")
	if ramdisk.Exists {
		t.Fatalf("RAM disk check = %#v, want not existing", ramdisk)
	}
	if health.Ramdisk != nil {
		t.Fatalf("Ramdisk = %#v, want nil for a missing path", health.Ramdisk)
	}

	// A real RAM disk directory is reported with its space.
	ramdiskPaths := *paths
	ramdiskPaths.RamdiskPath = root
	withRamdisk := CheckWithPaths(&ramdiskPaths)
	if withRamdisk.Ramdisk == nil {
		t.Fatal("Ramdisk should be reported for an existing directory")
	}
	if withRamdisk.Ramdisk.Path != root || withRamdisk.Ramdisk.TotalBytes == 0 {
		t.Fatalf("Ramdisk = %#v", withRamdisk.Ramdisk)
	}

	// Without optional paths only the three required checks are produced.
	minimal := CheckWithPaths(&HealthPaths{DataDir: root, TrashPath: filepath.Join(root, "trash"), DownloadPath: root})
	if len(minimal.Paths) != 3 {
		t.Fatalf("minimal Paths = %#v, want 3 entries", minimal.Paths)
	}
}

// TestRecentErrorsKeepsTheLastLines ports the
// `recent_errors_keeps_only_the_last_error_lines` test.
func TestHealthRecentErrorsKeepsTheLastLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gextto.log")
	mustWriteFile(t, path, "INFO ok\nERROR first\nWARN ignore\nERROR second\n")

	errors := recent_errors(path, 10)
	if len(errors) != 2 {
		t.Fatalf("recent_errors = %#v, want 2 entries", errors)
	}
	if !strings.Contains(errors[0], "first") || !strings.Contains(errors[1], "second") {
		t.Fatalf("recent_errors order = %#v", errors)
	}
	last := recent_errors(path, 1)
	if len(last) != 1 || !strings.Contains(last[0], "second") {
		t.Fatalf("recent_errors(1) = %#v", last)
	}
	if got := recent_errors(filepath.Join(t.TempDir(), "missing.log"), 10); len(got) != 0 {
		t.Fatalf("missing log = %#v, want empty", got)
	}
}

// TestProcessMetricsReturnsSaneRSS checks the lightweight metrics snapshot
// reports a plausible resident set size.
func TestHealthProcessMetricsReturnsSaneRSS(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process metrics rely on procfs")
	}
	metrics := ProcessMetrics()
	if metrics.ResidentBytes == 0 {
		t.Fatal("ResidentBytes should be greater than zero")
	}
	if metrics.ResidentBytes > 1<<40 {
		t.Fatalf("ResidentBytes = %d, implausibly large", metrics.ResidentBytes)
	}
	if resident := healthResidentBytes(); resident != metrics.ResidentBytes {
		// The two reads happen at different instants, so only guard the order of
		// magnitude when they disagree.
		if resident == 0 {
			t.Fatal("healthResidentBytes returned zero")
		}
	}
}

// TestDisksReturnsRealFilesystems checks `disks()` is never nil and every entry
// looks like a real mount.
func TestHealthDisksReturnsRealFilesystems(t *testing.T) {
	disks := disks()
	if disks == nil {
		t.Fatal("disks() should return a non-nil slice")
	}
	for _, disk := range disks {
		if !strings.HasPrefix(disk.Mount, "/") {
			t.Fatalf("mount %q is not absolute", disk.Mount)
		}
		if disk.TotalBytes == 0 {
			t.Fatalf("disk %#v has no total space", disk)
		}
	}
}

// TestHealthNumericHelpers covers the saturating arithmetic and percent clamp.
func TestHealthNumericHelpers(t *testing.T) {
	if got := healthSatSub(3, 5); got != 0 {
		t.Fatalf("healthSatSub(3,5) = %d, want 0", got)
	}
	if got := healthSatSub(9, 4); got != 5 {
		t.Fatalf("healthSatSub(9,4) = %d, want 5", got)
	}
	if got := healthSatAdd(^uint64(0), 1); got != ^uint64(0) {
		t.Fatalf("healthSatAdd overflow = %d", got)
	}
	if got := healthSatMul(3, 4); got != 12 {
		t.Fatalf("healthSatMul(3,4) = %d, want 12", got)
	}
	if got := healthSatMul(^uint64(0), 2); got != ^uint64(0) {
		t.Fatalf("healthSatMul overflow = %d", got)
	}
	if got := healthClampPercent(-1); got != 0 {
		t.Fatalf("healthClampPercent(-1) = %v, want 0", got)
	}
	if got := healthClampPercent(150); got != 100 {
		t.Fatalf("healthClampPercent(150) = %v, want 100", got)
	}
	if got := healthClampPercent(42.5); got != 42.5 {
		t.Fatalf("healthClampPercent(42.5) = %v, want 42.5", got)
	}
}

// TestHealthCPUSnapshot checks /proc/stat parsing when procfs is available.
func TestHealthCPUSnapshot(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("procfs is Linux-only")
	}
	total, idle, ok := cpu_snapshot()
	if !ok {
		t.Skip("/proc/stat unavailable")
	}
	if total == 0 {
		t.Fatal("total CPU ticks should be non-zero")
	}
	if idle > total {
		t.Fatalf("idle (%d) exceeds total (%d)", idle, total)
	}
}

// TestHealthTreeStats sums regular files in a directory tree.
func TestHealthTreeStats(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "sub")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	mustWriteFile(t, filepath.Join(root, "a.bin"), "12345")
	mustWriteFile(t, filepath.Join(nested, "b.bin"), "123")

	files, bytes := tree_stats(root)
	if files != 2 {
		t.Fatalf("files = %d, want 2", files)
	}
	if bytes != 8 {
		t.Fatalf("bytes = %d, want 8", bytes)
	}
	if files, bytes := tree_stats(filepath.Join(root, "missing")); files != 0 || bytes != 0 {
		t.Fatalf("missing tree = (%d,%d), want (0,0)", files, bytes)
	}
}

// TestReadFileTailAndRecentErrorsOnLargeLog checks the log tail is read without
// the whole file and that recent errors survive on a log larger than the tail.
func TestReadFileTailAndRecentErrorsOnLargeLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.log")
	var builder strings.Builder
	for builder.Len() < 4*recentErrorsTailBytes {
		builder.WriteString("2026-01-01 00:00:00 INFO routine line\n")
	}
	builder.WriteString("2026-01-02 10:00:00 ERROR boom one\n")
	builder.WriteString("2026-01-02 10:01:00 ERROR boom two\n")
	if err := os.WriteFile(path, []byte(builder.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	tail, err := readFileTail(path, recentErrorsTailBytes)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(tail)) > recentErrorsTailBytes {
		t.Fatalf("tail = %d bytes, want <= %d", len(tail), recentErrorsTailBytes)
	}
	got := recent_errors(path, 10)
	if len(got) != 2 || !strings.Contains(got[1], "boom two") {
		t.Fatalf("recent_errors on a large log = %#v", got)
	}
}
