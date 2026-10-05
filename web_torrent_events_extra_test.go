package gextto

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func TestPackFileNames(t *testing.T) {
	if got := tev_packFileNames(nil); got != "none" {
		t.Fatalf("empty = %q, want none", got)
	}
	items := []PackFileResult{
		{Path: "/nas/Show/S01E01.mkv"},
		{Path: "/nas/Show/S01E02.mkv"},
	}
	if got := tev_packFileNames(items); got != "S01E01.mkv, S01E02.mkv" {
		t.Fatalf("names = %q", got)
	}
	many := make([]PackFileResult, 10)
	for index := range many {
		many[index] = PackFileResult{Path: fmt.Sprintf("/nas/Show/S01E%02d.mkv", index+1)}
	}
	got := tev_packFileNames(many)
	if !strings.Contains(got, "… and 2 more") {
		t.Fatalf("truncated list = %q, want '… and 2 more'", got)
	}
}

func TestBgTorrentNeedsArchiveImportValidatesPayload(t *testing.T) {
	db := newTestDB(t)
	root := t.TempDir()
	cfg := &Config{ArchiveRoot: &root}

	release := models.Release{
		Title:   "Example",
		Kind:    "movie",
		Year:    int64Ptr(2020),
		Magnet:  "magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
		Quality: models.Quality{Resolution: "1080p"},
	}
	if err := db.RegisterTorrentScored(&release, 100); err != nil {
		t.Fatal(err)
	}
	hash := "0123456789012345678901234567890123456789"
	if _, err := db.MarkTorrentCompletedUnarchived(hash, "Example.2020.mkv"); err != nil {
		t.Fatal(err)
	}
	torrent := &models.TorrentView{Hash: hash, Name: "Example.2020.mkv", SavePath: root, HasMetadata: true, TotalSize: 1024}

	if bg_torrentNeedsArchiveImport(cfg, db, torrent) {
		t.Fatal("missing file should not need archive import")
	}

	target := filepath.Join(root, "Example.2020.mkv")
	if err := os.WriteFile(target, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if bg_torrentNeedsArchiveImport(cfg, db, torrent) {
		t.Fatal("empty file should not need archive import")
	}
	if err := os.WriteFile(target, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !bg_torrentNeedsArchiveImport(cfg, db, torrent) {
		t.Fatal("non-empty file should need archive import")
	}

	folderTorrent := &models.TorrentView{Hash: hash, Name: "Example.Folder", SavePath: root, HasMetadata: true, TotalSize: 1024}
	folder := filepath.Join(root, "Example.Folder")
	if err := os.MkdirAll(filepath.Join(folder, "Stagione 01"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "Stagione 01", "S01E01.mkv"), []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !bg_torrentNeedsArchiveImport(cfg, db, folderTorrent) {
		t.Fatal("directory with a regular file should need archive import")
	}

	// A season subfolder without any regular file must be rejected too.
	emptyFolderTorrent := &models.TorrentView{Hash: hash, Name: "Example.Empty", SavePath: root, HasMetadata: true, TotalSize: 1024}
	if err := os.MkdirAll(filepath.Join(root, "Example.Empty", "Stagione 01"), 0o755); err != nil {
		t.Fatal(err)
	}
	if bg_torrentNeedsArchiveImport(cfg, db, emptyFolderTorrent) {
		t.Fatal("directory without regular files should not need archive import")
	}
}
