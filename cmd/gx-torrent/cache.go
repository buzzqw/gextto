package main

// cache.go sizes rain's disk cache. Automatic mode follows the RAM of the
// machine, like Gextto's "Ottimizza impostazioni" for libtorrent: rain's own
// defaults (256 MiB read cache, up to 1 GiB write buffer) are too much on a
// small box and too little on a big one.

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/rain/torrent"
)

const mib = 1 << 20

// memoryTotal reads MemTotal from /proc/meminfo (0 when unknown).
func memoryTotal() int64 {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kib, err := strconv.ParseInt(fields[1], 10, 64)
			if err == nil {
				return kib * 1024
			}
		}
	}
	return 0
}

func clamp64(value, low, high int64) int64 {
	return max(low, min(value, high))
}

// cacheSizes returns the read cache and the write buffer in bytes.
func cacheSizes(cfg QueueConfig, ram int64) (read, write int64, auto bool) {
	if cfg.CacheMB > 0 {
		size := cfg.CacheMB * mib
		return size, size, false
	}
	if ram <= 0 {
		// Unknown RAM: conservative values.
		return 64 * mib, 128 * mib, true
	}
	return clamp64(ram/32, 32*mib, 512*mib), clamp64(ram/16, 64*mib, 1024*mib), true
}

// applyCache fills rain's cache settings.
func (d *Daemon) applyCache(cfg *torrent.Config) {
	read, write, _ := cacheSizes(d.state.Config, memoryTotal())
	cfg.ReadCacheSize = read
	cfg.WriteCacheSize = write
	cfg.ReadCacheTTL = time.Duration(d.state.Config.CacheTTLSecs) * time.Second
	cfg.Preallocate = d.state.Config.Preallocate
}
