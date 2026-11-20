package main

// storage.go keeps the on-disk layout safe.
//
// The engine stores every torrent under DataDir/<id> and, on removal, always runs
// os.RemoveAll(DataDir/<id>). gx-torrent therefore makes DataDir/<id> a
// symlink to the real save path chosen by Gextto: removing a torrent from the
// session only unlinks the symlink, and the payload is deleted only when the
// caller explicitly asks for it (deletePayload).

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// linkPath is the engine's data directory of one torrent.
func (d *Daemon) linkPath(id string) string {
	return filepath.Join(d.opts.LinkDir, id)
}

// validateDestination accepts only absolute paths, optionally restricted to
// the configured roots. Symlinks are resolved on the longest existing prefix,
// so a directory link cannot make a destination look inside a root it leaves;
// the daemon's own folders (links, parts, the data folder itself) are refused.
func (d *Daemon) validateDestination(dest string) (string, error) {
	dest = strings.TrimSpace(dest)
	if dest == "" {
		dest = d.opts.DownloadDir
	}
	if !filepath.IsAbs(dest) {
		return "", fmt.Errorf("destination must be an absolute path: %q", dest)
	}
	dest = filepath.Clean(dest)
	canonical, err := canonicalPath(dest)
	if err != nil {
		return "", fmt.Errorf("cannot resolve destination %q: %w", dest, err)
	}
	for _, internal := range []string{d.opts.LinkDir, d.opts.PartsDir} {
		if internal != "" && pathWithin(canonical, canonicalMust(internal)) {
			return "", fmt.Errorf("destination %q is inside the daemon's own data", dest)
		}
	}
	if canonical == canonicalMust(d.opts.DataDir) {
		return "", fmt.Errorf("destination %q is the daemon's data folder", dest)
	}
	if info, err := os.Stat(dest); err == nil && !info.IsDir() {
		return "", fmt.Errorf("destination %q is not a folder", dest)
	}
	if len(d.opts.AllowedRoots) == 0 {
		return dest, nil
	}
	for _, root := range d.opts.AllowedRoots {
		if pathWithin(canonical, canonicalMust(root)) {
			return dest, nil
		}
	}
	return "", fmt.Errorf("destination %q is outside the allowed roots", dest)
}

func pathWithin(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == root {
		return true
	}
	return strings.HasPrefix(path, strings.TrimRight(root, string(os.PathSeparator))+string(os.PathSeparator))
}

// canonicalPath resolves the symbolic links in the longest existing prefix of
// path and appends the part that does not exist yet. A destination chosen in
// the page often does not exist: comparing only the lexical path would let a
// symlinked parent point outside the allowed roots.
func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	suffix := ""
	for {
		resolved, err := filepath.EvalSymlinks(abs)
		if err == nil {
			if suffix == "" {
				return resolved, nil
			}
			return filepath.Join(resolved, suffix), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", err
		}
		suffix = filepath.Join(filepath.Base(abs), suffix)
		abs = parent
	}
}

// canonicalMust is canonicalPath for a configured folder whose absence is not a
// reason to skip the check: the lexical path is the fallback.
func canonicalMust(path string) string {
	if path == "" {
		return ""
	}
	if resolved, err := canonicalPath(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

// protectedTarget refuses to delete or serve one of the daemon's own files and
// folders, even when a save path overlaps the data folder or a torrent name
// happens to match a state file. The data folder itself and every ancestor of
// it are protected too: a payload never takes the session with it.
func (d *Daemon) protectedTarget(target string) error {
	target = filepath.Clean(target)
	canonical, err := canonicalPath(target)
	if err != nil {
		// A path that cannot be resolved is not a payload we can delete safely.
		return fmt.Errorf("cannot resolve %q: %w", target, err)
	}
	data := canonicalMust(d.opts.DataDir)
	if data != "" && (canonical == data || pathWithin(data, canonical)) {
		return fmt.Errorf("refusing to touch %q: it holds the daemon's data", target)
	}
	protected := []string{
		d.opts.LinkDir,
		d.opts.PartsDir,
		d.opts.DBPath,
		d.opts.StatePath,
		d.lifetimePath(),
		logFilePath,
	}
	if d.opts.DataDir != "" {
		protected = append(protected,
			filepath.Join(d.opts.DataDir, "settings.json"),
			filepath.Join(d.opts.DataDir, "ipfilter.dat"),
		)
	}
	for _, path := range protected {
		if path == "" {
			continue
		}
		guard := canonicalMust(path)
		if guard == "" {
			continue
		}
		if canonical == guard || pathWithin(canonical, guard) || pathWithin(guard, canonical) {
			return fmt.Errorf("refusing to touch %q: the daemon keeps its state there", target)
		}
	}
	// The download folder is legitimate payload content, but never the payload
	// itself: a torrent named like the folder must not take every download.
	if dest := d.opts.DownloadDir; dest != "" && canonical == canonicalMust(dest) {
		return fmt.Errorf("refusing to touch %q: it is the download folder", target)
	}
	// Hard links and renamed aliases have different paths but the same identity.
	if info, err := os.Lstat(target); err == nil {
		for _, path := range protected {
			if path == "" {
				continue
			}
			if other, err := os.Stat(path); err == nil && os.SameFile(info, other) {
				return fmt.Errorf("refusing to touch %q: it is the daemon's state", target)
			}
		}
	}
	return nil
}

// pointLink makes DataDir/<id> point at dest, replacing an older link. A real
// directory left there by an older layout is never replaced. The link itself is
// platform-specific (replaceDirLink, isDirLink): a symlink on Unix, a junction
// on Windows (which needs no symlink privilege; os.Readlink reads it back, but
// os.Lstat reports it as ModeIrregular, hence isDirLink).
func (d *Daemon) pointLink(id, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(d.opts.LinkDir, 0o755); err != nil {
		return err
	}
	link := d.linkPath(id)
	if info, err := os.Lstat(link); err == nil && !isDirLink(link, info) {
		return fmt.Errorf("%s is a real directory, not a gx-torrent link", link)
	}
	return replaceDirLink(link, dest)
}

// readLink returns the save path behind DataDir/<id>. A legacy real directory
// is its own save path.
func (d *Daemon) readLink(id string) (string, bool) {
	link := d.linkPath(id)
	info, err := os.Lstat(link)
	if err != nil {
		return "", false
	}
	if !isDirLink(link, info) {
		return link, true
	}
	target, err := os.Readlink(link)
	if err != nil {
		return "", false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(d.opts.LinkDir, target)
	}
	return filepath.Clean(target), true
}

// protectLegacyDir moves a real DataDir/<id> directory (older layout) out of
// the engine's reach before a removal, so the payload is never deleted implicitly.
func (d *Daemon) protectLegacyDir(id string) {
	link := d.linkPath(id)
	info, err := os.Lstat(link)
	if err != nil || isDirLink(link, info) {
		return
	}
	target := filepath.Join(d.opts.DataDir, "orphaned", id)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err == nil {
		if err := os.Rename(link, target); err == nil {
			logf("legacy data of %s kept in %s", id, target)
		}
	}
}

// payloadPath is the file or top directory of a torrent inside its save path.
// The name is one component: both separators are refused on every platform (on
// Windows "/" is a separator too, and a torrent name may come from an untrusted
// .torrent), and filepath.IsLocal rejects ".", "..", absolute paths and the
// Windows reserved names.
func payloadPath(savePath, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, `/\`) || !filepath.IsLocal(name) {
		return "", fmt.Errorf("unsafe torrent name %q", name)
	}
	return filepath.Join(savePath, name), nil
}

// deletePayload removes the torrent's own file or directory, never the save
// path itself. guard refuses the daemon's own state even when the save path
// overlaps the data folder.
func deletePayload(savePath, name string, guard func(string) error) error {
	target, err := payloadPath(savePath, name)
	if err != nil {
		return err
	}
	if filepath.Clean(target) == filepath.Clean(savePath) {
		return fmt.Errorf("refusing to delete the save path itself")
	}
	if guard != nil {
		if err := guard(target); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// movePayload moves savePath/name to dest/name: a rename on the same
// filesystem, a copy followed by removal across filesystems.
func movePayload(savePath, dest, name string) error {
	source, err := payloadPath(savePath, name)
	if err != nil {
		return err
	}
	target, err := payloadPath(dest, name)
	if err != nil {
		return err
	}
	if filepath.Clean(source) == filepath.Clean(target) {
		return nil
	}
	if _, err := os.Lstat(source); errors.Is(err, os.ErrNotExist) {
		// Nothing downloaded yet: only the link moves.
		return os.MkdirAll(dest, 0o755)
	}
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("destination already contains %q", name)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	if err := os.Rename(source, target); err == nil {
		return nil
	} else if !isCrossDevice(err) {
		return err
	}
	partial := target + ".gxpart"
	_ = os.RemoveAll(partial)
	if err := copyTree(source, partial); err != nil {
		_ = os.RemoveAll(partial)
		return err
	}
	if err := os.Rename(partial, target); err != nil {
		_ = os.RemoveAll(partial)
		return err
	}
	return os.RemoveAll(source)
}

// discardEmptyPayload drops the files of a torrent with nothing downloaded
// instead of moving them, and prepares the destination.
func discardEmptyPayload(savePath, dest, name string, guard func(string) error) error {
	source, err := payloadPath(savePath, name)
	if err != nil {
		return err
	}
	target, err := payloadPath(dest, name)
	if err != nil {
		return err
	}
	if filepath.Clean(source) != filepath.Clean(target) {
		if guard != nil {
			if err := guard(source); err != nil {
				return err
			}
		}
		if err := os.RemoveAll(source); err != nil {
			return err
		}
	}
	return os.MkdirAll(dest, 0o755)
}

// isCrossDevice is defined per platform (storage_xdev_*.go): Unix uses EXDEV,
// Windows maps ERROR_NOT_SAME_DEVICE as well.

// copyTree copies a file or a directory tree, preserving permissions.
func copyTree(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		link, err := os.Readlink(source)
		if err != nil {
			return err
		}
		if err := os.Symlink(link, target); err == nil {
			return nil
		} else if resolved, statErr := os.Stat(source); statErr == nil && resolved.Mode().IsRegular() {
			// Windows without the symlink privilege: keep the file's content
			// instead of failing the whole move.
			return copyFile(source, target, resolved.Mode().Perm())
		} else {
			return err
		}
	case info.IsDir():
		if err := os.MkdirAll(target, info.Mode().Perm()|0o700); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyTree(filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	default:
		return copyFile(source, target, info.Mode().Perm())
	}
}

func copyFile(source, target string, perm os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	n, err := copyFileData(out, in, info.Size())
	if err != nil {
		out.Close()
		_ = os.Remove(target)
		return fmt.Errorf("copy %s: %w", source, err)
	}
	// A file that shrinks while it is copied must not become the copy: the
	// rename into place would publish a truncated file as if it were whole.
	if n != info.Size() {
		out.Close()
		_ = os.Remove(target)
		return fmt.Errorf("copy %s: short copy, %d of %d bytes", source, n, info.Size())
	}
	if err := out.Sync(); err != nil {
		out.Close()
		_ = os.Remove(target)
		return err
	}
	return out.Close()
}

// copyFileData copies size bytes; tests replace it to inject a short copy.
var copyFileData = func(dst io.Writer, src io.Reader, size int64) (int64, error) {
	return io.CopyN(dst, src, size)
}
