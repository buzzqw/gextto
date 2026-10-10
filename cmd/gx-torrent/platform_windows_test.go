//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// These tests run on a real Windows runner (CI job windows-platform): they
// exercise the Win32 layer that the Linux suite only cross-compiles.

func TestWindowsMemoryTotalsArePositive(t *testing.T) {
	if total := memoryTotal(); total <= 0 {
		t.Fatalf("memoryTotal = %d, want a positive size", total)
	}
	if available := memoryAvailable(); available <= 0 {
		t.Fatalf("memoryAvailable = %d, want a positive size", available)
	}
}

func TestWindowsDefaultGatewayReturnsOrErrors(t *testing.T) {
	gateway, err := defaultGateway()
	if err != nil {
		t.Skipf("no default gateway on this runner: %v", err)
	}
	if gateway == nil || gateway.To4() == nil {
		t.Fatalf("gateway = %v, want an IPv4 address", gateway)
	}
}

func TestWindowsDefaultDataDirIsUnderGxTorrent(t *testing.T) {
	if dir := defaultDataDir(); !strings.Contains(dir, "gx-torrent") {
		t.Fatalf("defaultDataDir = %q", dir)
	}
}

func TestWindowsStorageClassIsKnownValue(t *testing.T) {
	switch class := localStorageClass(t.TempDir()); class {
	case "hdd", "ssd", "unknown":
	default:
		t.Fatalf("localStorageClass = %q", class)
	}
}

func TestWindowsCrossDeviceError(t *testing.T) {
	err := &os.LinkError{Op: "rename", Old: `C:\a`, New: `D:\a`, Err: syscall.Errno(17)}
	if !isCrossDevice(err) {
		t.Fatal("ERROR_NOT_SAME_DEVICE not recognised")
	}
	if isCrossDevice(&os.LinkError{Op: "rename", Err: syscall.Errno(5)}) {
		t.Fatal("ERROR_ACCESS_DENIED taken for a cross-device error")
	}
}

func TestWindowsJunctionIsALinkAndIsReplaced(t *testing.T) {
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
	if err != nil || !isDirLink(link, info) {
		t.Fatalf("junction not recognised as a link: %v %v", info, err)
	}
	if got, err := os.Readlink(link); err != nil || filepath.Clean(got) != dest {
		t.Fatalf("readlink = %q, %v; want %q", got, err, dest)
	}
	// Writing through the junction lands in dest.
	if err := os.WriteFile(filepath.Join(link, "payload.bin"), []byte("data"), 0o644); err != nil {
		t.Fatalf("write through junction: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "payload.bin")); err != nil {
		t.Fatalf("file not in dest: %v", err)
	}
	second := filepath.Join(dir, "dest2")
	if err := os.MkdirAll(second, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceDirLink(link, second); err != nil {
		t.Fatalf("replace existing junction: %v", err)
	}
	if got, _ := os.Readlink(link); filepath.Clean(got) != second {
		t.Fatalf("relinked to %q, want %q", got, second)
	}
	// Removing the junction never deletes the payload behind it.
	if err := os.RemoveAll(link); err != nil {
		t.Fatalf("remove junction: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "payload.bin")); err != nil {
		t.Fatalf("payload lost after removing the junction: %v", err)
	}
}

func TestWindowsRealDirectoryIsNotALink(t *testing.T) {
	dir := t.TempDir()
	info, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if isDirLink(dir, info) {
		t.Fatal("a plain directory taken for a link")
	}
}
