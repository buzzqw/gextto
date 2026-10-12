package main

// selection.go: per-file download priorities. A priority of 0 skips the file;
// 1..4 is normal, 5..7 is high (downloaded before the rest). Skipped files live
// in DATA/parts/<id>, never in the save path: changing which files are skipped
// stops the torrent, moves the affected files between the two places and starts
// it again, while changing only the levels is applied to the running torrent.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

// skipMask returns, per file, true when the priority means "do not download".
// nil when every file is wanted.
func skipMask(priorities []int) []bool {
	var mask []bool
	for i, priority := range priorities {
		if priority <= 0 {
			if mask == nil {
				mask = make([]bool, len(priorities))
			}
			mask[i] = true
		}
	}
	return mask
}

// normalFilePriority is the libtorrent default: a file with no explicit choice
// is downloaded with the rest.
const normalFilePriority = 4

// isAllNormal reports whether every file is normal (no skip, no high priority).
func isAllNormal(priorities []int) bool {
	return !slices.ContainsFunc(priorities, func(p int) bool { return p != normalFilePriority })
}

// setSelection stores the priorities the engine reads through prioritiesFor. A
// selection where every file is normal is the same as no selection, so the
// entry is dropped; the slice is copied so the engine never aliases the
// caller's.
func (d *Daemon) setSelection(id string, priorities []int) {
	d.selMu.Lock()
	defer d.selMu.Unlock()
	if isAllNormal(priorities) {
		delete(d.selection, id)
		return
	}
	d.selection[id] = append([]int(nil), priorities...)
}

// prioritiesFor is the engine's FilePriorities callback.
func (d *Daemon) prioritiesFor(id string) []int {
	d.selMu.RLock()
	defer d.selMu.RUnlock()
	return d.selection[id]
}

// filePriority reports the priority of one file for the API.
func filePriority(meta *torrentMeta, index int) int {
	if index < len(meta.FilePriorities) {
		if meta.FilePriorities[index] <= 0 {
			return 0
		}
		return meta.FilePriorities[index]
	}
	return 4
}

// setFilePriorities applies a new selection: one priority per file (0 = skip,
// 1..4 = normal, 5..7 = high).
func (d *Daemon) setFilePriorities(key string, priorities []int) error {
	d.mu.Lock()
	t, meta := d.findLocked(key)
	if t == nil {
		d.mu.Unlock()
		return errNotFound
	}
	id := t.ID()
	if d.moving[id] {
		d.mu.Unlock()
		return errors.New("the torrent is being moved")
	}
	files, err := t.Files()
	if err != nil {
		d.mu.Unlock()
		return errors.New("metadata not available yet")
	}
	if len(priorities) != len(files) {
		d.mu.Unlock()
		return fmt.Errorf("expected %d priorities, got %d", len(files), len(priorities))
	}
	if !slices.ContainsFunc(priorities, func(p int) bool { return p > 0 }) {
		d.mu.Unlock()
		return errors.New("at least one file must be wanted")
	}
	oldMask := skipMask(meta.FilePriorities)
	newMask := skipMask(priorities)
	stored := priorities
	if isAllNormal(stored) {
		stored = nil
	}
	meta.FilePriorities = stored
	if slices.Equal(oldMask, newMask) {
		// The files to skip did not change: only the priority levels did (or
		// nothing did). No file moves between the save path and the parts dir
		// and the torrent keeps running; the engine picks up the new levels.
		d.setSelection(id, stored)
		d.saveLocked()
		d.mu.Unlock()
		t.RefreshSelection()
		return nil
	}
	wasRunning := isRunning(t.Stats().Status)
	d.moving[id] = true
	if err := t.Stop(); err != nil {
		delete(d.moving, id)
		d.mu.Unlock()
		return err
	}
	paths := make([]string, len(files))
	for i, file := range files {
		paths[i] = file.Path()
	}
	savePath := meta.SavePath
	d.saveLocked()
	d.mu.Unlock()

	go d.finishSelection(id, t, paths, oldMask, newMask, stored, savePath, wasRunning)
	return nil
}

func (d *Daemon) finishSelection(id string, t *torrent.Torrent, paths []string, oldMask, newMask []bool, priorities []int, savePath string, wasRunning bool) {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && t.Stats().Status != torrent.Stopped {
		time.Sleep(100 * time.Millisecond)
	}
	skipped := func(mask []bool, i int) bool { return i < len(mask) && mask[i] }
	var moveErr error
	for i, rel := range paths {
		was, now := skipped(oldMask, i), skipped(newMask, i)
		if was == now {
			continue
		}
		real := filepath.Join(savePath, rel)
		part := filepath.Join(d.opts.PartsDir, id, rel)
		from, to := real, part
		if was {
			from, to = part, real
		}
		if err := relocateFile(from, to); err != nil && moveErr == nil {
			moveErr = err
		}
	}
	d.setSelection(id, priorities)

	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.moving, id)
	if meta, ok := d.state.Torrents[id]; ok && moveErr != nil {
		meta.Error = "file selection failed: " + moveErr.Error()
		logf("file selection of %s: %v", id, moveErr)
	}
	if wasRunning && moveErr == nil {
		if err := t.Start(); err != nil {
			logf("restart after file selection of %s: %v", id, err)
		}
	}
	d.saveLocked()
	d.poke()
}

// relocateFile moves one file (rename, or copy across filesystems). A
// missing source is not an error: nothing was downloaded yet.
func relocateFile(from, to string) error {
	if _, err := os.Stat(from); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if _, err := os.Stat(to); err == nil {
		return fmt.Errorf("%s already exists", to)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	if err := os.Rename(from, to); err == nil {
		return nil
	} else if !isCrossDevice(err) {
		return err
	}
	info, err := os.Stat(from)
	if err != nil {
		return err
	}
	if err := copyFile(from, to, info.Mode().Perm()); err != nil {
		_ = os.Remove(to)
		return err
	}
	return os.Remove(from)
}

// selectOnlyJob carries a BEP 53 magnet "so" selection from the tick loop
// (under d.mu) to applySelectOnly (outside d.mu).
type selectOnlyJob struct {
	id     string
	wanted []int
}

// applySelectOnly turns a BEP 53 magnet "so" selection into explicit file
// priorities once metadata is available, and drops the engine-side seed so the
// recorded selection is authoritative. It runs outside d.mu (setFilePriorities
// takes it) and must not block the tick for long: setFilePriorities stops the
// torrent and returns, with the relocation done in a goroutine.
func (d *Daemon) applySelectOnly(id string, wanted []int) {
	d.mu.Lock()
	t, _ := d.findLocked(id)
	d.mu.Unlock()
	if t == nil {
		return
	}
	files, err := t.Files()
	if err != nil || len(files) == 0 {
		// Metadata not ready after all: put the request back and retry.
		d.mu.Lock()
		if meta, ok := d.state.Torrents[id]; ok && len(meta.SelectOnly) == 0 {
			meta.SelectOnly = wanted
			d.dirty = true
		}
		d.mu.Unlock()
		return
	}
	want := make(map[int]struct{}, len(wanted))
	for _, i := range wanted {
		if i >= 0 {
			want[i] = struct{}{}
		}
	}
	priorities := make([]int, len(files))
	anyWanted := false
	for i := range files {
		if _, ok := want[i]; ok {
			priorities[i] = 4
			anyWanted = true
		}
	}
	if !anyWanted {
		logf("select-only %s: no valid file index in %v", id, wanted)
		t.ClearSelectOnly()
		return
	}
	if err := d.setFilePriorities(id, priorities); err != nil {
		logf("select-only %s: %v", id, err)
		return
	}
	t.ClearSelectOnly()
}
