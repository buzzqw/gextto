package main

// selection.go: download only some files of a torrent (priority 0 = skip).
// Skipped files live in DATA/parts/<id>, never in the save path; changing
// the selection stops the torrent, moves the affected files between the two
// places and starts it again.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/cenkalti/rain/v2/torrent"
)

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

func (d *Daemon) setSelection(id string, priorities []int) {
	d.selMu.Lock()
	defer d.selMu.Unlock()
	if mask := skipMask(priorities); mask != nil {
		d.selection[id] = mask
	} else {
		delete(d.selection, id)
	}
}

// selectionFor is rain's FileSelection callback.
func (d *Daemon) selectionFor(id string) []bool {
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

// setFilePriorities applies a new selection (one priority per file).
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
	if skipMask(priorities) == nil {
		priorities = nil
	}
	meta.FilePriorities = priorities
	if slices.Equal(oldMask, newMask) {
		d.saveLocked()
		d.mu.Unlock()
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

	go d.finishSelection(id, t, paths, oldMask, newMask, savePath, wasRunning)
	return nil
}

func (d *Daemon) finishSelection(id string, t *torrent.Torrent, paths []string, oldMask, newMask []bool, savePath string, wasRunning bool) {
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
	d.setSelection(id, newMaskPriorities(newMask))

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

// newMaskPriorities turns a skip mask back into priorities for setSelection.
func newMaskPriorities(mask []bool) []int {
	if mask == nil {
		return nil
	}
	out := make([]int, len(mask))
	for i, skip := range mask {
		if !skip {
			out[i] = 4
		}
	}
	return out
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
