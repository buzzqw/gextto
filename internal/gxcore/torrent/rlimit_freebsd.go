//go:build freebsd

package torrent

import "syscall"

// setNoFile raises the RLIMIT_NOFILE ceiling (current and max) so a session
// with many torrents and peers does not run out of file descriptors.
// FreeBSD's Rlimit fields are int64, hence the conversion.
func setNoFile(value uint64) error {
	lim := syscall.Rlimit{Cur: int64(value), Max: int64(value)}
	return syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim)
}
