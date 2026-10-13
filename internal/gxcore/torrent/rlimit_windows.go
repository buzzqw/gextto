//go:build windows

package torrent

// setNoFile is a no-op on Windows: the descriptor ceiling is managed by the
// C runtime, there is no RLIMIT_NOFILE equivalent to raise.
func setNoFile(value uint64) error {
	return nil
}
