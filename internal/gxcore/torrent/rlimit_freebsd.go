//go:build freebsd

package torrent

import "syscall"

// setNoFile makes sure at least value file descriptors are available, only
// ever raising the limits (see rlimit.go). FreeBSD's Rlimit fields are int64.
func setNoFile(value uint64) error {
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		return err
	}
	want := int64(value)
	if lim.Cur >= want {
		return nil
	}
	if lim.Max >= want {
		lim.Cur = want
		return syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim)
	}
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &syscall.Rlimit{Cur: want, Max: want}); err == nil {
		return nil
	}
	lim.Cur = lim.Max
	return syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim)
}
