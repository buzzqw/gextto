package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

// TestSelectionDropsEmptyFiles: a selection change on a torrent that has not
// verified a single piece must not move its files. Moving them between a NAS
// download folder and the local parts folder copied gigabytes of zeros (a
// hundred-gigabyte pack with a few files kept) and the selection only came into
// force once the copy ended.
func TestSelectionDropsEmptyFiles(t *testing.T) {
	dir := t.TempDir()
	savePath := filepath.Join(dir, "downloads")
	partsDir := filepath.Join(dir, "parts", "id")
	rel := filepath.Join("Season.S01.2160p", "Season.S01E01.mkv")
	from := filepath.Join(savePath, rel)
	to := filepath.Join(partsDir, rel)

	// The engine allocates the whole size up front: the file exists, empty.
	if err := os.MkdirAll(filepath.Dir(from), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(from, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(from, 8<<30); err != nil {
		t.Fatal(err)
	}

	if err := dropEmptyFile(from, to); err != nil {
		t.Fatalf("dropEmptyFile: %v", err)
	}
	if _, err := os.Stat(from); !os.IsNotExist(err) {
		t.Fatalf("the empty file is still at the save path: %v", err)
	}
	if _, err := os.Stat(to); err == nil {
		t.Fatal("dropping an empty file must not create it at the destination")
	}
	if info, err := os.Stat(filepath.Dir(to)); err != nil || !info.IsDir() {
		t.Fatalf("the destination folder must exist for the engine: %v", err)
	}
	// A missing source is not an error: the same pack is inspected again while
	// it downloads and the file is already gone.
	if err := dropEmptyFile(from, to); err != nil {
		t.Fatalf("dropEmptyFile on a missing source: %v", err)
	}
}

// TestEmptySelectionOnlyForAFreshDownload pins when the files may be dropped
// instead of moved: only a running download whose piece table says that
// nothing at all has been verified. A stopped torrent may hold data that was
// never checked, and a finished one must never lose it.
func TestEmptySelectionOnlyForAFreshDownload(t *testing.T) {
	fresh := torrent.Stats{}
	fresh.Status = torrent.Downloading
	if !selectionDropsEmpty(fresh) {
		t.Fatal("a downloading torrent with no verified piece must drop its empty files")
	}
	onePiece := torrent.Stats{}
	onePiece.Status = torrent.Downloading
	onePiece.Pieces.Have = 1
	withBytes := torrent.Stats{}
	withBytes.Status = torrent.Downloading
	withBytes.Bytes.Completed = 1 << 20
	stopped := torrent.Stats{}
	stopped.Status = torrent.Stopped
	verifying := torrent.Stats{}
	verifying.Status = torrent.Verifying
	for _, tc := range []struct {
		name  string
		stats torrent.Stats
	}{
		{"stopped", stopped},
		{"verifying (data not checked yet)", verifying},
		{"one verified piece", onePiece},
		{"bytes already written", withBytes},
	} {
		if selectionDropsEmpty(tc.stats) {
			t.Fatalf("%s: the files must be moved, not dropped", tc.name)
		}
	}
}
