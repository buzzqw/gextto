//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestMemoryTotalsArePositive(t *testing.T) {
	if total := memoryTotal(); total <= 0 {
		t.Fatalf("memoryTotal = %d, want a positive size", total)
	}
	if available := memoryAvailable(); available <= 0 {
		t.Fatalf("memoryAvailable = %d, want a positive size", available)
	}
}

func TestDefaultGatewayReturnsOrErrors(t *testing.T) {
	gateway, err := defaultGateway()
	if err != nil {
		// A sandbox/container without a default route is fine.
		t.Skipf("no default gateway in this environment: %v", err)
	}
	if gateway == nil || gateway.To4() == nil {
		t.Fatalf("gateway = %v, want an IPv4 address", gateway)
	}
}

func TestDefaultDataDirIsUnderGxTorrent(t *testing.T) {
	if dir := defaultDataDir(); !strings.Contains(dir, "gx-torrent") {
		t.Fatalf("defaultDataDir = %q", dir)
	}
}

func TestReplaceDirLinkIsASymlinkAndIsReplaced(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "link")
	dest := filepath.Join(dir, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceDirLink(link, dest); err != nil {
		t.Fatalf("replaceDirLink: %v", err)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link is not a symlink: %v %v", info, err)
	}
	if got, err := os.Readlink(link); err != nil || got != dest {
		t.Fatalf("readlink = %q, %v; want %q", got, err, dest)
	}
	second := filepath.Join(dir, "dest2")
	if err := os.MkdirAll(second, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceDirLink(link, second); err != nil {
		t.Fatalf("replace existing link: %v", err)
	}
	if got, _ := os.Readlink(link); got != second {
		t.Fatalf("relinked to %q, want %q", got, second)
	}
}

func TestIsCrossDeviceMatchesEXDEV(t *testing.T) {
	if !isCrossDevice(&os.LinkError{Op: "rename", Err: syscall.EXDEV}) {
		t.Fatal("EXDEV must be detected as cross-device")
	}
	if isCrossDevice(&os.LinkError{Op: "rename", Err: syscall.EPERM}) {
		t.Fatal("EPERM is not cross-device")
	}
}
