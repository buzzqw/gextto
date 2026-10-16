package gextto

import "syscall"

// sysinfo.go reads what the machine is, for the guided tuning: the filesystem
// type (to tell a network share apart from a local disk) and the device id (to
// tell whether the download folder and the library are on the same filesystem).
// These are the only facts the settings page cannot already read elsewhere;
// memory and free space come from memory_info and FilesystemSpace.

// networkFilesystem reports whether path sits on a network filesystem, so the
// tuning can lean conservative (no preallocation, longer request timeout). NFS,
// CIFS/SMB and SMB2 are recognised by their f_type, the same check the gx-torrent
// daemon uses (cmd/gx-torrent/fsinfo_linux.go).
func networkFilesystem(path string) bool {
	var stats syscall.Statfs_t
	if err := syscall.Statfs(path, &stats); err != nil {
		return false
	}
	switch uint64(stats.Type) {
	case 0x6969, 0xFF534D42, 0xFE534D42: // NFS, CIFS/SMB, SMB2
		return true
	}
	return false
}

// sameFilesystemDevice reports whether two paths live on the same device, so
// the tuning can propose hardlinks when they do and a move/copy when they do
// not. A path that cannot be stat'ed is never "same".
func sameFilesystemDevice(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	var first, second syscall.Stat_t
	if err := syscall.Stat(a, &first); err != nil {
		return false
	}
	if err := syscall.Stat(b, &second); err != nil {
		return false
	}
	return first.Dev == second.Dev
}
