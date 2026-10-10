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
// the configured roots.
func (d *Daemon) validateDestination(dest string) (string, error) {
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return d.opts.DownloadDir, nil
	}
	if !filepath.IsAbs(dest) {
		return "", fmt.Errorf("destination must be an absolute path: %q", dest)
	}
	dest = filepath.Clean(dest)
	if len(d.opts.AllowedRoots) == 0 {
		return dest, nil
	}
	for _, root := range d.opts.AllowedRoots {
		if pathWithin(dest, root) {
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

// pointLink makes DataDir/<id> point at dest, replacing an older link. A real
// directory left there by an older layout is never replaced. The link itself is
// platform-specific (replaceDirLink): a symlink on Unix, a junction on Windows
// (which needs no symlink privilege and is read back transparently by
// os.Readlink/Lstat).
func (d *Daemon) pointLink(id, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(d.opts.LinkDir, 0o755); err != nil {
		return err
	}
	link := d.linkPath(id)
	if info, err := os.Lstat(link); err == nil && info.Mode()&os.ModeSymlink == 0 {
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
	if info.Mode()&os.ModeSymlink == 0 {
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
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
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
func payloadPath(savePath, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || strings.ContainsRune(name, os.PathSeparator) {
		return "", fmt.Errorf("unsafe torrent name %q", name)
	}
	return filepath.Join(savePath, name), nil
}

// deletePayload removes the torrent's own file or directory, never the save
// path itself.
func deletePayload(savePath, name string) error {
	target, err := payloadPath(savePath, name)
	if err != nil {
		return err
	}
	if filepath.Clean(target) == filepath.Clean(savePath) {
		return fmt.Errorf("refusing to delete the save path itself")
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
func discardEmptyPayload(savePath, dest, name string) error {
	source, err := payloadPath(savePath, name)
	if err != nil {
		return err
	}
	target, err := payloadPath(dest, name)
	if err != nil {
		return err
	}
	if filepath.Clean(source) != filepath.Clean(target) {
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
		return os.Symlink(link, target)
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
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
