//go:build windows

package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// replaceDirLink makes link point at dest. Windows symlinks need the
// SeCreateSymbolicLinkPrivilege (or Developer Mode), so a directory junction is
// used instead: it needs no privilege and is read back transparently by
// os.Readlink/os.Lstat as a symlink. A previous link is removed first, because
// renaming over an existing directory is not allowed on Windows.
func replaceDirLink(link, dest string) error {
	if info, err := os.Lstat(link); err == nil && info.Mode()&os.ModeSymlink != 0 {
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

// setJunction writes the mount-point reparse point that turns the existing
// empty directory link into a junction to dest.
func setJunction(link, dest string) error {
	target := `\??\` + filepath.Clean(dest)
	if !strings.HasSuffix(target, `\`) {
		target += `\`
	}
	name, err := windows.UTF16FromString(target) // includes the NUL terminator
	if err != nil {
		return err
	}
	nameBytes := len(name) * 2
	const (
		headerLen = 8
		mountLen  = 8 // SubstituteName/PrintName offsets and lengths
	)
	buffer := make([]byte, headerLen+mountLen+nameBytes)
	binary.LittleEndian.PutUint32(buffer[0:4], uint32(windows.IO_REPARSE_TAG_MOUNT_POINT))
	binary.LittleEndian.PutUint16(buffer[4:6], uint16(mountLen+nameBytes)) // ReparseDataLength
	binary.LittleEndian.PutUint16(buffer[6:8], 0)                          // Reserved
	binary.LittleEndian.PutUint16(buffer[8:10], 0)                         // SubstituteNameOffset
	binary.LittleEndian.PutUint16(buffer[10:12], uint16(nameBytes))        // SubstituteNameLength
	binary.LittleEndian.PutUint16(buffer[12:14], uint16(nameBytes))        // PrintNameOffset
	binary.LittleEndian.PutUint16(buffer[14:16], 0)                        // PrintNameLength
	for i, unit := range name {
		binary.LittleEndian.PutUint16(buffer[16+i*2:], unit)
	}
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
