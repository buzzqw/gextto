package torrent

// torrent_rename.go implements qBittorrent-compatible renames: a file, a folder
// or the whole torrent (display name and content root). The engine never
// changes the info dict (its hash must stay the original): a rename is planned
// here (pure), the caller performs the on-disk moves with the storage closed
// and persists the override with PersistRename; the next load applies it and
// reopens the files at the new paths.

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
	"github.com/buzzqw/gextto/internal/gxcore/internal/resumer/boltdbresumer"
	"go.etcd.io/bbolt"
)

// FileMove is one on-disk rename requested by a rename plan: Old and New are
// torrent-relative storage paths.
type FileMove struct {
	Old string
	New string
}

// RenamePlan is the result of planning a rename: the on-disk moves, the full new
// file-path list (matching info.Files, persisted by PersistRename) and the new
// display name for a torrent rename (empty otherwise).
type RenamePlan struct {
	Moves []FileMove
	Paths []string
	Name  string
}

// renameQuery carries a plan and its error across the run-loop query.
type renameQuery struct {
	plan RenamePlan
	err  error
}

// PlanRenameFile plans renaming a stored file, or every file under it when
// oldPath is a folder. It does not touch the disk or the resume.
func (t *Torrent) PlanRenameFile(oldPath, newPath string) (RenamePlan, error) {
	res := query(t.torrent, func() renameQuery {
		plan, err := t.torrent.planRename(oldPath, newPath)
		return renameQuery{plan: plan, err: err}
	})
	return res.plan, res.err
}

// PlanRenameFolder plans renaming a folder: same prefix semantics as
// PlanRenameFile.
func (t *Torrent) PlanRenameFolder(oldPath, newPath string) (RenamePlan, error) {
	return t.PlanRenameFile(oldPath, newPath)
}

// PlanRename plans renaming the torrent: its display name and the root
// component of every file.
func (t *Torrent) PlanRename(newName string) (RenamePlan, error) {
	res := query(t.torrent, func() renameQuery {
		plan, err := t.torrent.planRenameTorrent(newName)
		return renameQuery{plan: plan, err: err}
	})
	return res.plan, res.err
}

// planRename computes the moves and the new path list for a prefix rename.
func (t *torrent) planRename(oldPath, newPath string) (RenamePlan, error) {
	info := t.info()
	if info == nil {
		return RenamePlan{}, errors.New("torrent metadata not ready")
	}
	oldPath = normalizeRelPath(oldPath)
	newPath = normalizeRelPath(newPath)
	if err := validateRelPath(oldPath); err != nil {
		return RenamePlan{}, fmt.Errorf("invalid old path: %w", err)
	}
	if err := validateRelPath(newPath); err != nil {
		return RenamePlan{}, fmt.Errorf("invalid new path: %w", err)
	}
	if newPath == oldPath {
		return RenamePlan{}, nil
	}
	paths := make([]string, len(info.Files))
	moves := make([]FileMove, 0, len(info.Files))
	matched := false
	for i, f := range info.Files {
		if f.Path == oldPath || strings.HasPrefix(f.Path, oldPath+"/") {
			np := newPath + f.Path[len(oldPath):]
			paths[i] = np
			if !f.Padding {
				moves = append(moves, FileMove{Old: f.Path, New: np})
			}
			matched = true
			continue
		}
		paths[i] = f.Path
	}
	if !matched {
		return RenamePlan{}, fmt.Errorf("no file or folder at %q", oldPath)
	}
	if err := checkPathCollisions(info.Files, paths); err != nil {
		return RenamePlan{}, err
	}
	return RenamePlan{Moves: moves, Paths: paths}, nil
}

// checkPathCollisions refuses a path list where two files would share a path,
// or where a file would sit where another needs a folder (a/b next to a/b/c):
// on disk the second move would overwrite or fail half-way.
func checkPathCollisions(files []metainfo.File, paths []string) error {
	seen := make(map[string]struct{}, len(paths))
	for i, p := range paths {
		if files[i].Padding {
			continue
		}
		if _, dup := seen[p]; dup {
			return fmt.Errorf("%q is already used by another file of the torrent", p)
		}
		seen[p] = struct{}{}
	}
	for p := range seen {
		for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
			if _, clash := seen[dir]; clash {
				return fmt.Errorf("%q is a file of the torrent and cannot become a folder", dir)
			}
		}
	}
	return nil
}

// planRenameTorrent renames the display name and the content root.
func (t *torrent) planRenameTorrent(newName string) (RenamePlan, error) {
	info := t.info()
	if info == nil {
		return RenamePlan{}, errors.New("torrent metadata not ready")
	}
	newName = strings.TrimSpace(newName)
	if validateRelPath(newName) != nil || strings.Contains(newName, "/") {
		return RenamePlan{}, fmt.Errorf("invalid torrent name %q", newName)
	}
	// The on-disk root is the first component of the file paths, which is the
	// cleaned info name (may differ from info.Name when it held a separator).
	root := info.Files[0].Path
	if idx := strings.IndexByte(root, '/'); idx >= 0 {
		root = root[:idx]
	}
	plan, err := t.planRename(root, newName)
	if err != nil {
		return RenamePlan{}, err
	}
	plan.Name = newName
	return plan, nil
}

// PersistRename writes a torrent's file-path override (and, for a torrent
// rename, its display name) into an existing resume database, opening it
// directly. It is meant to run while no session owns the database: a rename
// closes the session, performs the on-disk moves, persists here and reopens.
func PersistRename(dbPath, torrentID string, paths []string, name string) error {
	db, err := bbolt.Open(dbPath, 0600, &bbolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return err
	}
	defer db.Close()
	res, err := boltdbresumer.New(db, torrentsBucket)
	if err != nil {
		return err
	}
	if err := res.WriteFilePaths(torrentID, paths); err != nil {
		return err
	}
	if name != "" {
		return res.WriteDisplayName(torrentID, name)
	}
	return nil
}

// normalizeRelPath cleans a torrent-relative path (backslashes become slashes).
func normalizeRelPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "\\", "/")
	return path.Clean(p)
}

// validateRelPath rejects a path that could escape the torrent storage.
func validateRelPath(p string) error {
	if p == "" || p == "." || p == "/" {
		return errors.New("empty path")
	}
	if path.IsAbs(p) || strings.HasPrefix(p, "/") {
		return errors.New("absolute path not allowed")
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return errors.New("path escapes the torrent")
		}
	}
	return nil
}
