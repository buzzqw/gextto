package main

// payload.go describes what of a save path belongs to a torrent. Normally that
// is one entry, the content root (a file, or the folder holding every file),
// handled as a whole (storage.go). With the NoSubfolder content layout a
// multi-file torrent has no folder of its own: its files sit directly in the
// save path, next to anything else there, so they are deleted and moved one by
// one and only the folders they leave empty are removed.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

// payload is a torrent's data under its save path: root, or files when the
// torrent has no folder of its own.
type payload struct {
	root  string
	files []string
}

// payloadOf returns the torrent's payload. name is the fallback root while the
// metadata is unknown.
func payloadOf(t *torrent.Torrent, meta *torrentMeta, name string) payload {
	if meta != nil && meta.ContentLayout == torrent.ContentLayoutNoSubfolder {
		if files, err := t.Files(); err == nil && len(files) > 1 {
			paths := make([]string, 0, len(files))
			for _, f := range files {
				paths = append(paths, f.Path())
			}
			return payload{files: paths}
		}
	}
	if root := t.ContentRoot(); root != "" {
		return payload{root: root}
	}
	return payload{root: name}
}

// contentPath is where the torrent's content is: the root under the save path,
// or the save path itself for loose files (qBittorrent's content_path).
func (p payload) contentPath(savePath string) string {
	if p.files != nil || p.root == "" {
		return savePath
	}
	return filepath.Join(savePath, p.root)
}

// filePath resolves one torrent-relative file under base, refusing a path that
// would leave it.
func payloadFilePath(base, rel string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(rel))
	if cleaned == "." || filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe torrent file path %q", rel)
	}
	return filepath.Join(base, cleaned), nil
}

// delete removes the torrent's own data, never the save path itself.
func (p payload) delete(savePath string) error {
	if p.files == nil {
		return deletePayload(savePath, p.root)
	}
	for _, rel := range p.files {
		target, err := payloadFilePath(savePath, rel)
		if err != nil {
			return err
		}
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	pruneEmptyDirs(savePath, p.files)
	return nil
}

// gone reports that none of the torrent's data is left in the save path.
func (p payload) gone(savePath string) bool {
	if p.files == nil {
		if p.root == "" {
			return false
		}
		_, err := os.Lstat(filepath.Join(savePath, p.root))
		return errors.Is(err, os.ErrNotExist)
	}
	for _, rel := range p.files {
		if target, err := payloadFilePath(savePath, rel); err == nil {
			if _, err := os.Lstat(target); err == nil {
				return false
			}
		}
	}
	return true
}

// move moves the data from savePath to dest: a rename on the same filesystem,
// a copy followed by removal across filesystems. Loose files are moved one by
// one; an existing target is never overwritten and a failure puts back the
// files already moved.
func (p payload) move(savePath, dest string) error {
	if p.files == nil {
		return movePayload(savePath, dest, p.root)
	}
	if filepath.Clean(savePath) == filepath.Clean(dest) {
		return nil
	}
	type step struct{ from, to string }
	steps := make([]step, 0, len(p.files))
	for _, rel := range p.files {
		from, err := payloadFilePath(savePath, rel)
		if err != nil {
			return err
		}
		to, err := payloadFilePath(dest, rel)
		if err != nil {
			return err
		}
		if _, err := os.Lstat(from); errors.Is(err, os.ErrNotExist) {
			continue // not downloaded yet
		}
		if _, err := os.Lstat(to); err == nil {
			return fmt.Errorf("destination already contains %q", rel)
		}
		steps = append(steps, step{from, to})
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	done := make([]step, 0, len(steps))
	undo := func() {
		for i := len(done) - 1; i >= 0; i-- {
			_ = os.Rename(done[i].to, done[i].from)
		}
	}
	for _, s := range steps {
		if err := os.MkdirAll(filepath.Dir(s.to), 0o755); err != nil {
			undo()
			return err
		}
		err := os.Rename(s.from, s.to)
		if err != nil && isCrossDevice(err) {
			err = copyThenRemove(s.from, s.to)
			if err == nil {
				continue // a copy cannot be undone by a rename: keep it
			}
		}
		if err != nil {
			undo()
			return err
		}
		done = append(done, s)
	}
	pruneEmptyDirs(savePath, p.files)
	return nil
}

// discard drops the files of a torrent with nothing downloaded instead of
// moving them, and prepares the destination.
func (p payload) discard(savePath, dest string) error {
	if p.files == nil {
		return discardEmptyPayload(savePath, dest, p.root)
	}
	if filepath.Clean(savePath) != filepath.Clean(dest) {
		if err := p.delete(savePath); err != nil {
			return err
		}
	}
	return os.MkdirAll(dest, 0o755)
}

// copyThenRemove copies one file across filesystems through a partial name,
// then removes the source.
func copyThenRemove(from, to string) error {
	partial := to + ".gxpart"
	_ = os.Remove(partial)
	if err := copyTree(from, partial); err != nil {
		_ = os.Remove(partial)
		return err
	}
	if err := os.Rename(partial, to); err != nil {
		_ = os.Remove(partial)
		return err
	}
	return os.Remove(from)
}

// pruneEmptyDirs removes the folders under base that held the files and are now
// empty, deepest first. base itself is never removed, and a folder holding
// anything else stays.
func pruneEmptyDirs(base string, files []string) {
	dirs := map[string]struct{}{}
	for _, rel := range files {
		for dir := filepath.Dir(filepath.FromSlash(rel)); dir != "." && dir != string(os.PathSeparator); dir = filepath.Dir(dir) {
			dirs[dir] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(dirs))
	for dir := range dirs {
		ordered = append(ordered, dir)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return strings.Count(ordered[i], string(os.PathSeparator)) > strings.Count(ordered[j], string(os.PathSeparator))
	})
	for _, dir := range ordered {
		if target, err := payloadFilePath(base, filepath.ToSlash(dir)); err == nil {
			_ = os.Remove(target) // fails, harmlessly, on a folder that is not empty
		}
	}
}
