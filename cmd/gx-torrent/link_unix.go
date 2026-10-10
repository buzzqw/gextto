//go:build !windows

package main

import "os"

// replaceDirLink makes link an atomic symlink to dest, replacing an existing
// one. A temporary link plus rename keeps a concurrent reader from seeing a
// half-written link.
func replaceDirLink(link, dest string) error {
	tmp := link + ".tmp"
	_ = os.Remove(tmp)
	if err := os.Symlink(dest, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// isDirLink reports whether path (with its Lstat info) is a gx-torrent link.
func isDirLink(_ string, info os.FileInfo) bool {
	return info.Mode()&os.ModeSymlink != 0
}
