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
	"errors"
	"fmt"
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
	// watchMaxPerScan bounds one scan: a folder holding a huge drop must not
	// keep the scan goroutine busy and the rest of the folders waiting.
	watchMaxPerScan = 256
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
		if err := d.checkWatchFolder(folder); err != nil {
			d.noteWatchProblem(folder, err.Error())
			continue
		}
		d.clearWatchProblem(folder)
		entries, err := os.ReadDir(folder)
		if err != nil {
			d.noteWatchProblem(folder, err.Error())
			continue
		}
		handled := 0
		for _, entry := range entries {
			// A link can point anywhere: only a real .torrent file is read.
			if entry.IsDir() || !entry.Type().IsRegular() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".torrent") {
				continue
			}
			if handled >= watchMaxPerScan {
				d.noteWatchProblem(folder, "too many .torrent files in one scan")
				break
			}
			handled++
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

// checkWatchFolder keeps a drop folder from being a door to the daemon's own
// files: it must be a real folder (not a link), not writable by others, and
// outside the internal folders the daemon writes itself.
func (d *Daemon) checkWatchFolder(folder string) error {
	info, err := os.Lstat(folder)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("not a real folder")
	}
	if info.Mode().Perm()&0o002 != 0 {
		return errors.New("the folder is writable by others")
	}
	canonical, err := canonicalPath(folder)
	if err != nil {
		return err
	}
	for _, internal := range []string{d.opts.DataDir, d.opts.LinkDir, d.opts.PartsDir} {
		if internal != "" && pathWithin(canonical, canonicalMust(internal)) {
			return fmt.Errorf("the folder is inside the daemon's data (%s)", internal)
		}
	}
	return nil
}

// noteWatchProblem logs a watch-folder problem on transition only: the scan
// runs every half minute and a broken folder would otherwise fill the log.
func (d *Daemon) noteWatchProblem(folder, reason string) {
	d.watchWarnMu.Lock()
	if d.watchWarn == nil {
		d.watchWarn = map[string]string{}
	}
	previous := d.watchWarn[folder]
	d.watchWarn[folder] = reason
	d.watchWarnMu.Unlock()
	if previous != reason {
		logfError("watch folder %s: %s", folder, reason)
	}
}

func (d *Daemon) clearWatchProblem(folder string) {
	d.watchWarnMu.Lock()
	delete(d.watchWarn, folder)
	d.watchWarnMu.Unlock()
}
