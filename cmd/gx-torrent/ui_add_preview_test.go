package main

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

// TestHandleUIAddPreviewListsFiles proves the add dialog receives the files of
// the chosen .torrent, with a checkbox each, before the torrent is added.
func TestHandleUIAddPreviewListsFiles(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	dir := t.TempDir()
	for _, name := range []string{"keep.bin", "skip.bin"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Repeat("x", 40<<10)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	data, err := torrent.CreateTorrent(dir, torrent.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("torrent", "sample.torrent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "http://127.0.0.1:8890/ui/add-preview", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.RemoteAddr = "127.0.0.1:1234"
	d.handleUIAddPreview(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	out := rec.Body.String()
	for _, want := range []string{"keep.bin", "skip.bin", `data-file-index="0"`, `data-file-index="1"`, `type="checkbox"`} {
		if !strings.Contains(out, want) {
			t.Errorf("preview lacks %q: %s", want, out)
		}
	}
}

// TestAddAppliesFilePriorities proves the selection from the add dialog reaches
// the torrent metadata before the queue starts it.
func TestAddAppliesFilePriorities(t *testing.T) {
	d := newTestDaemonWith(t, NetworkOptions{PortBegin: 42600, PortEnd: 42699, Encryption: 1})
	src := filepath.Join(t.TempDir(), "src")
	data := makeMultiTorrent(t, src, "Season", []int{50_000, 70_000, 40_000})
	hash, _, err := d.add(addRequest{
		TorrentData:    data,
		Destination:    filepath.Join(t.TempDir(), "dst"),
		Paused:         true,
		FilePriorities: []int{4, 0, 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	_, meta := d.findLocked(hash)
	d.mu.Unlock()
	if meta == nil || len(meta.FilePriorities) != 3 || meta.FilePriorities[1] != 0 {
		t.Fatalf("file priorities not stored: %+v", meta)
	}
	if got := d.prioritiesFor(meta.ID); len(got) != 3 || got[1] != 0 {
		t.Fatalf("engine selection = %v, want the skipped middle file", got)
	}
}

// TestAddRejectsAnEmptySelection stops a dialog with every box unchecked from
// creating a torrent that can never download anything.
func TestAddRejectsAnEmptySelection(t *testing.T) {
	d := newTestDaemonWith(t, NetworkOptions{PortBegin: 42700, PortEnd: 42799, Encryption: 1})
	src := filepath.Join(t.TempDir(), "src")
	data := makeMultiTorrent(t, src, "Season", []int{10_000})
	if _, _, err := d.add(addRequest{
		TorrentData:    data,
		Destination:    filepath.Join(t.TempDir(), "dst"),
		Paused:         true,
		FilePriorities: []int{0},
	}); err == nil {
		t.Fatal("an all-skipped selection must be refused")
	}
}
