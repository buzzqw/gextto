//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// replaceDirLink makes link point at dest. A junction (mount point) can only
// target a local volume, so a destination on a network share (UNC or a mapped
// remote drive) is linked with a symlink instead: that needs
// SeCreateSymbolicLinkPrivilege (or Developer Mode), and a clear error is
// returned when it is not available. A local destination uses a junction, which
// needs no privilege (os.Readlink reads it back).
//
// Windows cannot atomically replace a directory junction (MoveFileEx with
// REPLACE_EXISTING does not overwrite a directory), so the new link is built
// complete under a temporary name first and only then swapped in: the window
// with no link is as short as a single rename, and a failure while building the
// new link leaves the old one untouched.
func replaceDirLink(link, dest string) error {
	if networkFilesystem(dest) {
		return replaceDirLinkSymlink(link, dest)
	}
	tmp := link + ".tmp"
	_ = os.RemoveAll(tmp)
	if err := os.Mkdir(tmp, 0o755); err != nil {
		return err
	}
	if err := setJunction(tmp, dest); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if info, err := os.Lstat(link); err == nil && isDirLink(link, info) {
		// Removing a junction never touches its target.
		if err := os.Remove(link); err != nil {
			_ = os.RemoveAll(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	return nil
}

// replaceDirLinkSymlink builds link as a symlink to a network destination, the
// only link kind that may target a share. A symlink to a directory needs the
// symlink privilege (or Developer Mode); without it the error names the network
// folder so the operator knows to use a local folder or enable symlinks.
func replaceDirLinkSymlink(link, dest string) error {
	tmp := link + ".tmp"
	_ = os.Remove(tmp)
	if err := os.Symlink(dest, tmp); err != nil {
		return fmt.Errorf("network folder %s: the link needs the symlink privilege (Developer Mode) or a local folder: %w", dest, err)
	}
	if info, err := os.Lstat(link); err == nil && isDirLink(link, info) {
		if err := os.Remove(link); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// isDirLink reports whether path (with its Lstat info) is a gx-torrent link.
// Since Go 1.23 os.Lstat reports a junction as ModeIrregular, not ModeSymlink,
// so a reparse point counts as a link when os.Readlink can read it: that holds
// only for symlinks and junctions.
func isDirLink(path string, info os.FileInfo) bool {
	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	if info.Mode()&os.ModeIrregular == 0 {
		return false
	}
	_, err := os.Readlink(path)
	return err == nil
}

// setJunction writes the mount-point reparse point that turns the existing
// empty directory link into a junction to dest. A junction target must be an
// absolute path.
func setJunction(link, dest string) error {
	abs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	buffer := mountPointReparseData(`\??\` + filepath.Clean(abs))
	ptr, err := windows.UTF16PtrFromString(link)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(
		ptr,
		windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	var returned uint32
	return windows.DeviceIoControl(
		handle,
		windows.FSCTL_SET_REPARSE_POINT,
		(*byte)(unsafe.Pointer(&buffer[0])), uint32(len(buffer)),
		nil, 0,
		&returned, nil,
	)
}
