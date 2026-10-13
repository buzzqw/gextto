package main

// diskguard.go pauses the unfinished downloads of a storage folder whose free
// space drops below the queue's MinFreeMB, and lets the queue resume them once
// space returns. A full disk otherwise turns every piece write into an error
// (or, with -mmap, into a fault) and stops the torrents in error; pausing first
// keeps them clean, and seeding goes on because it writes nothing.
//
// With preallocation the files are reserved in full when the torrent starts,
// so a download needs no further space and the guard stays out of the way.

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// diskGuardInterval spaces the statfs calls: free space moves slowly, and on a
// network mount statfs can block.
const diskGuardInterval = 15 * time.Second

// diskGuardSlack is the hysteresis above the threshold needed to resume, so a
// folder hovering at the limit does not start and stop downloads every tick.
func diskGuardSlack(minFree int64) int64 { return max(minFree/10, 256<<20) }

// diskGuard holds, per storage folder, whether it is below the threshold.
type diskGuard struct {
	mu        sync.Mutex
	checkedAt time.Time
	low       map[string]bool
	free      map[string]int64
	// freeFn reads the free space of a folder (diskFree when nil); tests
	// replace it under mu.
	freeFn func(path string) (free, total int64)
}

// refresh re-reads the free space of the given folders (at most every
// diskGuardInterval). It must run WITHOUT holding d.mu: statfs can block.
func (g *diskGuard) refresh(now time.Time, paths []string, minFree int64, force bool) {
	g.mu.Lock()
	if !force && now.Sub(g.checkedAt) < diskGuardInterval {
		g.mu.Unlock()
		return
	}
	g.checkedAt = now
	prevLow := g.low
	freeFn := g.freeFn
	g.mu.Unlock()
	if freeFn == nil {
		freeFn = diskFree
	}

	low := make(map[string]bool, len(paths))
	free := make(map[string]int64, len(paths))
	if minFree > 0 {
		for _, path := range paths {
			if _, seen := free[path]; seen {
				continue
			}
			bytes, total := freeFn(existingDir(path))
			free[path] = bytes
			if total <= 0 {
				continue // unknown: never block on a folder we cannot measure
			}
			limit := minFree
			if prevLow[path] {
				limit += diskGuardSlack(minFree)
			}
			if bytes < limit {
				low[path] = true
				if !prevLow[path] {
					logf("disk guard: %s has %s free (below %s): downloads there are paused until space returns", path, humanBytes(bytes), humanBytes(minFree))
				}
			} else if prevLow[path] {
				logf("disk guard: %s has %s free again: downloads resume", path, humanBytes(bytes))
			}
		}
	}
	g.mu.Lock()
	g.low, g.free = low, free
	g.mu.Unlock()
}

// isLow reports whether downloads into path must wait for space.
func (g *diskGuard) isLow(path string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.low[path]
}

// existingDir walks up to the nearest existing folder, so a save path not
// created yet is measured on the filesystem it will live on.
func existingDir(path string) string {
	for p := filepath.Clean(path); ; {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return p
		}
		p = parent
	}
}

// diskGuardPaths returns the storage folders of the unfinished torrents and
// the threshold to apply (0 when the guard is off). It takes d.mu briefly.
func (d *Daemon) diskGuardPaths() ([]string, int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	cfg := d.state.Config
	if cfg.Preallocate || cfg.MinFree() <= 0 {
		return nil, 0
	}
	seen := map[string]bool{}
	var paths []string
	for _, meta := range d.state.Torrents {
		path := d.savePathOf(meta)
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	return paths, cfg.MinFree()
}

// savePathOf is the storage folder of a torrent, the download folder when the
// metadata has none.
func (d *Daemon) savePathOf(meta *torrentMeta) string {
	if meta != nil && meta.SavePath != "" {
		return meta.SavePath
	}
	return d.opts.DownloadDir
}

// humanBytes formats a byte count for the log (binary units).
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return strconv.FormatFloat(float64(n)/float64(div), 'f', 1, 64) + " " + string("KMGTPE"[exp]) + "iB"
}
