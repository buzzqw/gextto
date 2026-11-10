package gextto

import (
	"os"
	"path/filepath"
	"testing"
)

// appendCrashLog appends text to the daemon's crash log, like the Go runtime
// does on stderr.
func appendCrashLog(t *testing.T, path, text string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open crash log: %v", err)
	}
	if _, err := file.WriteString(text); err != nil {
		file.Close()
		t.Fatalf("write crash log: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close crash log: %v", err)
	}
}

// TestCrashReportOnlyCountsWhatThisRunWrote checks the boundary that makes the
// check useful: the crash log is append-only across runs, so what a previous
// daemon wrote (its own panic, included) must never be attributed to this one.
func TestCrashReportOnlyCountsWhatThisRunWrote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gx-torrent.crash.log")
	appendCrashLog(t, path, "panic: Torrent (id=old) does not respond.\ngoroutine 1 [running]:\n")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	process := &gxManagedProcess{crashLog: path, crashOffset: info.Size()}
	if reported, crashed := process.crashReport(); crashed {
		t.Fatalf("a panic written by a previous run was reported again: %s", reported)
	}

	appendCrashLog(t, path, "2026-11-10 12:00:00  INFO gx-core 1.1.334 is listening on [::]:8890\n")
	if _, crashed := process.crashReport(); crashed {
		t.Fatal("an ordinary log line was reported as a crash")
	}

	appendCrashLog(t, path, "gc 41 @845.413s 0%: 0.091+3.0+0.012 ms clock\n")
	if _, crashed := process.crashReport(); crashed {
		t.Fatal("runtime noise was reported as a crash")
	}

	appendCrashLog(t, path, "panic: Torrent (id=243b2bf) does not respond.\ngoroutine 26675 [running]:\n")
	got, crashed := process.crashReport()
	if !crashed {
		t.Fatal("the panic of this run was not reported")
	}
	if got != path {
		t.Fatalf("crash log path = %q, want %q", got, path)
	}
}

// TestCrashReportDetectsGoRuntimeFailures covers the other banner the runtime
// prints before dying, and the adopted daemon, for which no starting offset is
// known and so nothing is claimed.
func TestCrashReportDetectsGoRuntimeFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gx-torrent.crash.log")
	appendCrashLog(t, path, "fatal error: concurrent map writes\n")
	process := &gxManagedProcess{crashLog: path}
	if _, crashed := process.crashReport(); !crashed {
		t.Fatal("a Go runtime fatal error was not reported")
	}

	adopted := &gxManagedProcess{pid: 42}
	if _, crashed := adopted.crashReport(); crashed {
		t.Fatal("an adopted daemon must not report a crash it cannot attribute")
	}

	missing := &gxManagedProcess{crashLog: filepath.Join(t.TempDir(), "absent.log")}
	if _, crashed := missing.crashReport(); crashed {
		t.Fatal("a missing crash log must not report a crash")
	}
}
