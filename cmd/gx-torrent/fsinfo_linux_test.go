//go:build linux

package main

import "testing"

func TestDiskFreeBytesReportsAVolume(t *testing.T) {
	free, total := diskFreeBytes(t.TempDir())
	if total <= 0 {
		t.Fatalf("total = %d, want a positive size", total)
	}
	if free < 0 || free > total {
		t.Fatalf("free = %d, total = %d: free must be within [0,total]", free, total)
	}
	// diskFree is the daemon-facing wrapper and must agree.
	if wf, wt := diskFree(t.TempDir()); wt != total || wf != free {
		// Different temp dirs can sit on different volumes in exotic setups;
		// only require the wrapper to report a plausible volume.
		if wt <= 0 || wf < 0 || wf > wt {
			t.Fatalf("diskFree = %d/%d, want a plausible volume", wf, wt)
		}
	}
}

func TestNetworkFilesystemLocalDir(t *testing.T) {
	if networkFilesystem(t.TempDir()) {
		t.Fatal("a local temp dir must not look like a network filesystem")
	}
	if networkFilesystem("") {
		t.Fatal("an empty path must not look like a network filesystem")
	}
}
