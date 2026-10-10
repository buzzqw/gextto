package main

// cache.go sizes rain's disk cache. Gextto pushes the manual value and the
// daemon retunes itself on a coarse cadence (see adaptCacheLocked); the fork
// applies a new read-cache and write-buffer size to the running session, so no
// reopen is needed.
//
// rain's own defaults (256 MiB read cache, 1 GiB write buffer) are fixed and do
// not consider the machine. Here the values follow the available RAM and the
// live workload: a bigger write buffer when several torrents download at once
// (more coalescing, less starving peers) and a bigger read cache while seeding
// (reads served from RAM instead of disk). Both are caps: rain allocates piece
// buffers on demand, so the process stays small when the load is light.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/buzzqw/gextto/internal/engine/torrent"
	"github.com/buzzqw/gextto/internal/queue"
)

const mib = 1 << 20

const (
	cacheBudgetMin  = 128 << 20
	cacheBudgetMax  = 2 << 30
	cacheReadMin    = 32 << 20
	cacheReadMax    = 512 << 20
	cacheWriteMin   = 96 << 20
	cacheWriteMax   = 1536 << 20
	cacheCheckEvery = 3 * time.Minute  // how often the target is recomputed
	cacheApplyEvery = 10 * time.Minute // minimum time between adaptive retunes
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

func clamp64(value, low, high int64) int64 {
	return max(low, min(value, high))
}

// cacheInputs are the live signals the adaptive policy reacts to.
type cacheInputs struct {
	ram             int64
	available       int64
	activeDownloads int
	activeSeeds     int
	downloadRate    int64
	class           string // "ssd", "hdd", "network" or "unknown"
}

// cacheSizes is the static sizing: RAM/32 read (32..512 MiB) and RAM/16 write
// (64 MiB..1 GiB). It is the fallback when the live signals are missing.
func cacheSizes(cfg queue.Config, ram int64) (read, write int64, auto bool) {
	if cfg.CacheMB > 0 {
		size := cfg.CacheMB * mib
		return size, size, false
	}
	if ram <= 0 {
		return 64 * mib, 128 * mib, true
	}
	return clamp64(ram/32, 32*mib, 512*mib), clamp64(ram/16, 64*mib, 1024*mib), true
}

// adaptiveCache returns the read cache and the write buffer for the current
// workload. cfg.CacheMB > 0 is a manual override and wins.
func adaptiveCache(cfg queue.Config, in cacheInputs) (read, write int64, reason string) {
	if cfg.CacheMB > 0 {
		size := cfg.CacheMB * mib
		return size, size, "manual"
	}
	if cfg.Auto != nil && !*cfg.Auto {
		// Self-management disabled: static sizing from the RAM.
		read, write, _ := cacheSizes(queue.Config{}, in.ram)
		return read, write, "static"
	}
	avail := in.available
	if avail <= 0 {
		avail = in.ram
	}
	if avail <= 0 {
		r, w, _ := cacheSizes(cfg, in.ram)
		return r, w, "fallback"
	}
	// Budget from reclaimable memory, capped hard so we never fight the system.
	budget := clamp64(avail/8, cacheBudgetMin, cacheBudgetMax)
	if in.ram > 0 {
		budget = min(budget, max(in.ram/4, cacheBudgetMin))
	}
	slow := in.class == "network" || in.class == "hdd"

	// Write buffer: grows with concurrent downloads (more in-flight pieces to
	// keep fed and to write back in order), doubled on slow storage.
	downloads := max(in.activeDownloads, 1)
	write = cacheWriteMin * int64(downloads)
	if in.downloadRate > 25<<20 {
		write += cacheWriteMin // a fast transfer benefits from more buffering
	}
	if slow {
		write *= 2
	}
	write = clamp64(write, cacheWriteMin, min(cacheWriteMax, budget))

	// Read cache: grows with active seeds (reads served from RAM), doubled on
	// network storage where a disk read is a round-trip.
	read = cacheReadMin + int64(in.activeSeeds)*(cacheReadMin/2)
	if in.class == "network" {
		read *= 2
	}
	read = clamp64(read, cacheReadMin, min(cacheReadMax, budget))

	// Never reserve more than 1.5x the budget in total.
	if limit := budget * 3 / 2; read+write > limit {
		read = max(cacheReadMin, limit-write)
	}
	reason = fmt.Sprintf("auto:%s downloads=%d seeds=%d", in.class, in.activeDownloads, in.activeSeeds)
	return read, write, reason
}

// storageClass classifies where downloads are written, so the policy can lean
// larger on slow storage. Network filesystems are detected from statfs; local
// disks from their rotational flag.
func storageClass(path string) string {
	if strings.TrimSpace(path) == "" {
		return "unknown"
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err == nil {
		switch uint64(st.Type) {
		case 0x6969, 0xFF534D42, 0xFE534D42: // NFS, CIFS/SMB, SMB2
			return "network"
		}
	}
	if majmin := mountMajorMinor(path); majmin != "" {
		if rotationalDevice(majmin) {
			return "hdd"
		}
		return "ssd"
	}
	return "unknown"
}

// refreshStorageClass recomputes the download storage class when due and
// returns it. It must be called WITHOUT holding d.mu: statfs on an
// unresponsive network mount can block for a long time.
func (d *Daemon) refreshStorageClass(now time.Time) string {
	d.classMu.Lock()
	defer d.classMu.Unlock()
	if d.classCached == "" || d.classCheckedAt.IsZero() || now.Sub(d.classCheckedAt) >= cacheCheckEvery {
		d.classCached = storageClass(d.downloadDir())
		d.classCheckedAt = now
	}
	return d.classCached
}

// cachedStorageClass returns the last classified storage, or "" when the
// download dir has not been classified yet.
func (d *Daemon) cachedStorageClass() string {
	d.classMu.Lock()
	defer d.classMu.Unlock()
	return d.classCached
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

// applyCache fills rain's cache settings from the daemon's current target.
func (d *Daemon) applyCache(cfg *torrent.Config) {
	if d.cacheRead <= 0 || d.cacheWrite <= 0 {
		d.cacheClass = d.cachedStorageClass()
		if d.cacheClass == "" {
			// First classification (tests, or a session opened before the first
			// tick); in normal operation the tick refreshes it outside d.mu.
			d.cacheClass = storageClass(d.downloadDir())
		}
		read, write, reason := adaptiveCache(d.state.Config, cacheInputs{
			ram: memoryTotal(), available: memoryAvailable(), class: d.cacheClass,
		})
		d.cacheRead, d.cacheWrite, d.cacheReason = read, write, reason
	}
	cfg.ReadCacheSize = d.cacheRead
	cfg.WriteCacheSize = d.cacheWrite
	cfg.ReadCacheTTL = time.Duration(d.state.Config.CacheTTLSecs) * time.Second
	cfg.Preallocate = d.state.Config.Preallocate
}

func (d *Daemon) downloadDir() string {
	if d.opts.DownloadDir != "" {
		return d.opts.DownloadDir
	}
	return d.opts.DataDir
}

// adaptCacheLocked recomputes the cache target from the live workload and
// applies it to the running session when it moved enough (the fork resizes
// the read cache and the write buffer in place). Hysteresis and a coarse
// cadence keep it from changing at every queue move.
func (d *Daemon) adaptCacheLocked(now time.Time, activeDownloads, activeSeeds int, downloadRate int64) {
	if !d.cacheCheckedAt.IsZero() && now.Sub(d.cacheCheckedAt) < cacheCheckEvery {
		return
	}
	d.cacheCheckedAt = now
	class := d.cachedStorageClass()
	if class == "" {
		// Never classified yet (tests, or a session opened before the first
		// tick): in normal operation the tick sets it outside d.mu.
		class = storageClass(d.downloadDir())
	}
	read, write, reason := adaptiveCache(d.state.Config, cacheInputs{
		ram:             memoryTotal(),
		available:       memoryAvailable(),
		activeDownloads: activeDownloads,
		activeSeeds:     activeSeeds,
		downloadRate:    downloadRate,
		class:           class,
	})
	d.cacheClass = class
	if d.cacheRead == 0 {
		// First evaluation.
		d.applyCacheSizesLocked(read, write, reason, now)
		return
	}
	// A manual size (CacheMB > 0) or a static policy is not a workload
	// estimate: any change the operator asked for takes effect at once, even
	// below the hysteresis threshold. Only the adaptive policy is damped, so
	// its workload-driven swings do not retune the cache at every tick.
	if !strings.HasPrefix(reason, "auto:") {
		if read == d.cacheRead && write == d.cacheWrite {
			d.cacheReason = reason
			return
		}
		d.applyCacheSizesLocked(read, write, reason, now)
		logf("cache retuned: read=%dMiB write=%dMiB (%s)", read/mib, write/mib, reason)
		return
	}
	if !cacheMoved(d.cacheRead, read) && !cacheMoved(d.cacheWrite, write) {
		d.cacheReason = reason
		return
	}
	if !d.cacheAppliedAt.IsZero() && now.Sub(d.cacheAppliedAt) < cacheApplyEvery {
		return
	}
	d.applyCacheSizesLocked(read, write, reason, now)
	logf("cache retuned: read=%dMiB write=%dMiB (%s)", read/mib, write/mib, reason)
}

// applyCacheSizesLocked resizes the running session's caches.
func (d *Daemon) applyCacheSizesLocked(read, write int64, reason string, now time.Time) {
	d.cacheRead, d.cacheWrite, d.cacheReason = read, write, reason
	d.cacheAppliedAt = now
	if d.session != nil {
		d.session.SetCacheSizes(read, write)
	}
}

// cacheMoved reports a change larger than 25%.
func cacheMoved(current, target int64) bool {
	if current <= 0 {
		return true
	}
	delta := target - current
	if delta < 0 {
		delta = -delta
	}
	return delta*4 > current
}
