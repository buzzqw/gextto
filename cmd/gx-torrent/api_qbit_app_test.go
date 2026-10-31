package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// TestQbitFreeSpaceAndDirectoryContent covers the two read-only application
// endpoints the alternative web interfaces use for their folder pickers.
func TestQbitFreeSpaceAndDirectoryContent(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file.bin"), []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	d.opts.DownloadDir = dir
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v2/app/getFreeSpace?path=" + url.QueryEscape(dir))
	if err != nil {
		t.Fatal(err)
	}
	var free int64
	if err := json.NewDecoder(resp.Body).Decode(&free); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || free <= 0 {
		t.Fatalf("free space: status=%d free=%d", resp.StatusCode, free)
	}

	resp, err = http.Get(srv.URL + "/api/v2/app/getDirectoryContent?dirPath=" + url.QueryEscape(dir))
	if err != nil {
		t.Fatal(err)
	}
	var entries []qbitDirEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("directory content: status=%d", resp.StatusCode)
	}
	var sawDir, sawFile bool
	for _, entry := range entries {
		switch entry.Name {
		case "sub":
			sawDir = entry.IsDir
		case "file.bin":
			sawFile = !entry.IsDir && entry.Size == int64(len("payload"))
		}
	}
	if !sawDir || !sawFile {
		t.Fatalf("directory content = %+v", entries)
	}
}
