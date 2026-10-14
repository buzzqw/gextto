package main

// rename.go is the daemon side of the qBittorrent rename: it plans the rename
// with the engine, then closes the session (so the files are closed), performs
// the on-disk moves with a rollback on failure, persists the new paths and lets
// the session reopen and apply them. The engine never changes the info dict, so
// a rename does not touch the torrent's identity.

import (
	"os"
	"path/filepath"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

// absRename is one on-disk move with absolute paths.
type absRename struct {
	old, new string
}

// pendingRename is the rename waiting for the session restart hook.
type pendingRename struct {
	id     string
	dbPath string
	moves  []absRename
	paths  []string
	name   string
}

// renameFile renames a stored file or folder of the torrent.
func (d *Daemon) renameFile(hash, oldPath, newPath string) error {
	t, err := d.torrentHandle(hash)
	if err != nil {
		return err
	}
	plan, err := t.PlanRenameFile(oldPath, newPath)
	if err != nil {
		return err
	}
	return d.queueRename(t.ID(), plan)
}

// renameFolder renames a folder of the torrent.
func (d *Daemon) renameFolder(hash, oldPath, newPath string) error {
	t, err := d.torrentHandle(hash)
	if err != nil {
		return err
	}
	plan, err := t.PlanRenameFolder(oldPath, newPath)
	if err != nil {
		return err
	}
	return d.queueRename(t.ID(), plan)
}

// renameTorrent renames the torrent (display name and content root).
func (d *Daemon) renameTorrent(hash, newName string) error {
	t, err := d.torrentHandle(hash)
	if err != nil {
		return err
	}
	plan, err := t.PlanRename(newName)
	if err != nil {
		return err
	}
	return d.queueRename(t.ID(), plan)
}

// torrentHandle finds a live torrent by hash without holding d.mu during the
// engine call.
func (d *Daemon) torrentHandle(hash string) (*torrent.Torrent, error) {
	d.mu.Lock()
	t, _ := d.findLocked(hash)
	d.mu.Unlock()
	if t == nil {
		return nil, errNotFound
	}
	return t, nil
}

// queueRename records the rename and schedules the session restart that will
// close the storage, move the files and reopen at the new paths.
func (d *Daemon) queueRename(id string, plan torrent.RenamePlan) error {
	if len(plan.Paths) == 0 && plan.Name == "" {
		return nil // nothing to do (no metadata yet)
	}
	moves := d.absRenameMoves(id, plan.Moves)
	d.mu.Lock()
	d.pendingRename = &pendingRename{
		id:     id,
		dbPath: d.opts.DBPath,
		moves:  moves,
		paths:  plan.Paths,
		name:   plan.Name,
	}
	d.restartPending = true
	d.mu.Unlock()
	d.poke()
	return nil
}

// absRenameMoves resolves the torrent-relative moves against the storage roots
// (the torrent's symlink root and its parts root). A file lives in one of them.
func (d *Daemon) absRenameMoves(id string, moves []torrent.FileMove) []absRename {
	bases := []string{filepath.Join(d.opts.LinkDir, id)}
	if d.opts.PartsDir != "" {
		bases = append(bases, filepath.Join(d.opts.PartsDir, id))
	}
	out := make([]absRename, 0, len(moves))
	for _, m := range moves {
		for _, base := range bases {
			oldAbs := filepath.Join(base, filepath.FromSlash(m.Old))
			if _, err := os.Stat(oldAbs); err == nil {
				out = append(out, absRename{old: oldAbs, new: filepath.Join(base, filepath.FromSlash(m.New))})
				break
			}
		}
	}
	return out
}

// applyPendingRename performs the moves (with rollback) and persists the new
// paths. It runs after the session has closed its files.
func (d *Daemon) applyPendingRename() {
	pr := d.pendingRename
	d.pendingRename = nil
	if pr == nil {
		return
	}
	if err := executeRenameMoves(pr.moves); err != nil {
		logf("rename %s: %v", pr.id, err)
		return
	}
	if err := torrent.PersistRename(pr.dbPath, pr.id, pr.paths, pr.name); err != nil {
		logf("rename %s: cannot persist the new paths: %v", pr.id, err)
		return
	}
	logf("renamed %s", pr.id)
}

// executeRenameMoves renames every file, rolling back the ones already moved
// when one fails, so the storage stays consistent.
func executeRenameMoves(moves []absRename) error {
	done := make([]absRename, 0, len(moves))
	for _, m := range moves {
		if err := os.MkdirAll(filepath.Dir(m.new), 0o755); err != nil {
			rollbackRename(done)
			return err
		}
		if err := os.Rename(m.old, m.new); err != nil {
			rollbackRename(done)
			return err
		}
		done = append(done, m)
	}
	return nil
}

func rollbackRename(done []absRename) {
	for i := len(done) - 1; i >= 0; i-- {
		_ = os.Rename(done[i].new, done[i].old)
	}
}
