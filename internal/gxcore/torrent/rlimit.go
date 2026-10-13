//go:build !windows && !freebsd

package torrent

import "syscall"

// setNoFile raises the RLIMIT_NOFILE ceiling (current and max) so a session
// with many torrents and peers does not run out of file descriptors.
func setNoFile(value uint64) error {
	lim := syscall.Rlimit{Cur: value, Max: value}
	return syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim)
}
