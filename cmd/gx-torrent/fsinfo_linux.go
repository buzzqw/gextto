//go:build linux

package main

import "syscall"

// diskFreeBytes returns the free (available to the user) and total bytes of the
// filesystem holding path. Zeroes when the path cannot be stat'ed.
func diskFreeBytes(path string) (free, total int64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	block := int64(st.Bsize)
	return int64(st.Bavail) * block, int64(st.Blocks) * block
}

// networkFilesystem reports whether path sits on a network filesystem, so the
// cache policy can lean larger. NFS, CIFS/SMB and SMB2 are recognised by their
// f_type.
func networkFilesystem(path string) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false
	}
	switch uint64(st.Type) {
	case 0x6969, 0xFF534D42, 0xFE534D42: // NFS, CIFS/SMB, SMB2
		return true
	}
	return false
}
