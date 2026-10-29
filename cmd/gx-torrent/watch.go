package main

// watch.go is the standalone "watch folders" feature (qBittorrent's scan_dirs):
// the daemon scans the configured folders, adds the .torrent files it finds and
// removes them on success. It is off when no folder is configured, and the scan
// runs from the queue tick in a short-lived goroutine, so it costs nothing when
// unused. In managed mode Gextto owns the additions, so it never runs. A file
// that is not a valid .torrent is renamed to "<name>.invalid", so it is not
// retried at every scan; a file modified in the last few seconds waits for the
// next scan, because it may still be being written.

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

const (
	watchFoldersSettingKey = "watch-folders"
	// watchInterval is how often the folders are listed. qBittorrent reacts to
	// filesystem events; a 30 s poll is simpler and good enough for a drop
	// folder.
	watchInterval = 30 * time.Second
	// watchInvalidSuffix marks a dropped file that is not a valid .torrent.
	watchInvalidSuffix = ".invalid"
	// watchSettle is how old a dropped file must be before it is read: a
	// younger one may still be being copied.
	watchSettle = 5 * time.Second
)

// watchFolders returns the configured (folder, destination) pairs. Each line is
// "folder" (the default destination) or "folder=destination".
func (d *Daemon) watchFolders() [][2]string {
	if d.opts.Settings == nil {
		return nil
	}
	raw := strings.TrimSpace(d.opts.Settings.Get(watchFoldersSettingKey, ""))
	if raw == "" {
		return nil
	}
	var out [][2]string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		folder, dest := line, ""
		if idx := strings.Index(line, "="); idx >= 0 {
			folder = strings.TrimSpace(line[:idx])
			dest = strings.TrimSpace(line[idx+1:])
		}
		if folder != "" {
			out = append(out, [2]string{folder, dest})
		}
	}
	return out
}

// maybeScanWatchFolders triggers a scan at most every watchInterval, from the
// serial queue tick. It does nothing when no folder is configured.
func (d *Daemon) maybeScanWatchFolders(now time.Time) {
	if d.opts.Mode != ModeStandalone {
		return
	}
	folders := d.watchFolders()
	if len(folders) == 0 {
		return
	}
	if !d.watchLast.IsZero() && now.Sub(d.watchLast) < watchInterval {
		return
	}
	if !d.watchRunning.CompareAndSwap(false, true) {
		return
	}
	d.watchLast = now
	go d.scanWatchFolders(folders)
}

// scanWatchFolders adds the .torrent files found in the configured folders,
// removing each one after a successful add. A failed add leaves the file in
// place and is logged, so it is retried on the next scan.
func (d *Daemon) scanWatchFolders(folders [][2]string) {
	defer d.watchRunning.Store(false)
	for _, pair := range folders {
		folder, dest := pair[0], pair[1]
		if dest == "" {
			dest = d.downloadDir()
		}
		entries, err := os.ReadDir(folder)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".torrent") {
				continue
			}
			full := filepath.Join(folder, entry.Name())
			if fi, err := entry.Info(); err != nil || time.Since(fi.ModTime()) < watchSettle {
				continue
			}
			data, err := os.ReadFile(full)
			if err != nil || len(data) == 0 {
				continue
			}
			if err := torrent.ValidateTorrentFile(data); err != nil {
				if rerr := os.Rename(full, full+watchInvalidSuffix); rerr != nil {
					logfError("watch folder %s: %s is not a valid .torrent (%v) and cannot be set aside: %v", folder, entry.Name(), err, rerr)
				} else {
					logf("watch folder %s: %s is not a valid .torrent (%v): renamed to %s", folder, entry.Name(), err, entry.Name()+watchInvalidSuffix)
				}
				continue
			}
			if _, _, err := d.add(addRequest{TorrentData: data, Destination: dest}); err != nil {
				logfError("watch folder %s: cannot add %s: %v", folder, entry.Name(), err)
				continue
			}
			if err := os.Remove(full); err != nil {
				logfError("watch folder %s: added %s but cannot remove it: %v", folder, entry.Name(), err)
			} else {
				logf("watch folder %s: added %s", folder, entry.Name())
			}
		}
	}
}
