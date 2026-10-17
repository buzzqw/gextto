//go:build !windows && !freebsd

package torrent

import (
	"syscall"
	"testing"
)

// TestSetNoFileNeverLowers: asking for fewer descriptors than the process has
// must leave both limits alone (the old form set them both to the value and so
// capped a 65536-descriptor service at 10240), and asking for more raises the
// soft limit within the hard one.
func TestSetNoFileNeverLowers(t *testing.T) {
	var before syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &before); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &before) }()
	if before.Cur < 64 {
		t.Skipf("descriptor limit too low to test: %d", before.Cur)
	}

	if err := setNoFile(before.Cur / 2); err != nil {
		t.Fatal(err)
	}
	var after syscall.Rlimit
	_ = syscall.Getrlimit(syscall.RLIMIT_NOFILE, &after)
	if after.Cur != before.Cur || after.Max != before.Max {
		t.Fatalf("limits lowered: %+v -> %+v", before, after)
	}

	// Lower the soft limit only, then ask for more: it goes back up.
	low := syscall.Rlimit{Cur: before.Cur / 2, Max: before.Max}
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &low); err != nil {
		t.Fatal(err)
	}
	if err := setNoFile(before.Cur); err != nil {
		t.Fatal(err)
	}
	_ = syscall.Getrlimit(syscall.RLIMIT_NOFILE, &after)
	if after.Cur != before.Cur || after.Max != before.Max {
		t.Fatalf("soft limit not raised: %+v, want Cur %d Max %d", after, before.Cur, before.Max)
	}
}
