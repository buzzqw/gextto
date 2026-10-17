//go:build !windows && !freebsd

package torrent

import "syscall"

// setNoFile makes sure at least value file descriptors are available. Every
// file of a running torrent stays open (the allocator opens them all), so the
// limit bounds the files of the running torrents together.
//
// It only ever raises: the soft limit goes up to value (to the hard limit when
// that is lower and the hard limit cannot be raised), and nothing already
// higher is touched. The Go runtime lifts the soft limit to the hard one at
// start, and the gextto service grants 65536: setting both limits to value
// would lower them, and a torrent with more files than value could not start.
func setNoFile(value uint64) error {
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		return err
	}
	if lim.Cur >= value {
		return nil
	}
	if lim.Max >= value {
		lim.Cur = value
		return syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim)
	}
	// The hard limit is lower: raise both if allowed (root), else take what
	// the hard limit gives.
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &syscall.Rlimit{Cur: value, Max: value}); err == nil {
		return nil
	}
	lim.Cur = lim.Max
	return syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim)
}
