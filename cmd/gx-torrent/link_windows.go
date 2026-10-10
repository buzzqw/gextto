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
// used instead: it needs no privilege and os.Readlink reads it back. A previous
// link is removed first (removing a junction never touches its target),
// because renaming over an existing directory is not allowed on Windows.
func replaceDirLink(link, dest string) error {
	if info, err := os.Lstat(link); err == nil && isDirLink(link, info) {
		if err := os.Remove(link); err != nil {
			return err
		}
	}
	if err := os.Mkdir(link, 0o755); err != nil {
		return err
	}
	if err := setJunction(link, dest); err != nil {
		_ = os.Remove(link)
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
