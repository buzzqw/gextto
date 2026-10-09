package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestIPFilterBytesCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "filter.dat")
	if err := os.WriteFile(path, []byte("1.2.3.0/24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{}
	data, stamp, err := d.ipFilterBytes(path)
	if err != nil || string(data) != "1.2.3.0/24\n" || stamp == "" {
		t.Fatalf("first read = %q/%q/%v", data, stamp, err)
	}

	// A matching stamp means the cached bytes are reused: seed a sentinel and
	// confirm it is returned instead of re-reading the file.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	d.ipFilterData = []byte("CACHED")
	d.ipFilterStamp = fmt.Sprintf("%s|%d|%d", path, info.Size(), info.ModTime().UnixNano())
	cached, _, err := d.ipFilterBytes(path)
	if err != nil || string(cached) != "CACHED" {
		t.Fatalf("cache was not reused: %q/%v", cached, err)
	}
}
