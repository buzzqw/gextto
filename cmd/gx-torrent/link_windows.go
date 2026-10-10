//go:build windows

package main

import (
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// replaceDirLink makes link point at dest. Windows symlinks need the
// SeCreateSymbolicLinkPrivilege (or Developer Mode), so a directory junction is
// used instead: it needs no privilege and os.Readlink reads it back.
//
// Windows cannot atomically replace a directory junction (MoveFileEx with
// REPLACE_EXISTING does not overwrite a directory), so the new junction is
// built complete under a temporary name first and only then swapped in: the
// window with no link is as short as a single rename, and a failure while
// building the new junction leaves the old link untouched.
func replaceDirLink(link, dest string) error {
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
