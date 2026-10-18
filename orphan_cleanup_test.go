package gextto

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func TestFindAndCleanOrphanedDownloads(t *testing.T) {
	root := t.TempDir()
	temp := filepath.Join(root, "temp")
	if err := os.MkdirAll(temp, 0o755); err != nil {
		t.Fatal(err)
	}
	trash := filepath.Join(root, "trash")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		t.Fatal(err)
	}
	library := t.TempDir()
	libraryFile := filepath.Join(library, "Show - S01E01 - [1080p][h265][AC3 5.1].mkv")
	if err := os.WriteFile(libraryFile, []byte("library"), 0o644); err != nil {
		t.Fatal(err)
	}

	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO series(name, enabled) VALUES('Show', 1)"); err != nil {
		t.Fatal(err)
	}
	var seriesID int64
	if err := db.db.QueryRow("SELECT id FROM series WHERE name='Show'").Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,quality_score,archive_path,downloaded_at) VALUES(?1,1,1,4000,?2,datetime('now'))", seriesID, libraryFile); err != nil {
		t.Fatal(err)
	}
	// Same-size copy with a lower score: the content is the same file, so the
	// guided cleanup may treat it as a duplicate.
	librarySame := filepath.Join(library, "Show - S01E04 - [1080p][h265][AC3 5.1].mkv")
	if err := os.WriteFile(librarySame, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,quality_score,archive_path,downloaded_at) VALUES(?1,1,4,1000,?2,datetime('now'))", seriesID, librarySame); err != nil {
		t.Fatal(err)
	}

	safeFile := filepath.Join(root, "Show.S01E01.1080p.mkv")
	if err := os.WriteFile(safeFile, []byte("safe"), 0o644); err != nil {
		t.Fatal(err)
	}
	unsafeFile := filepath.Join(root, "Show.S01E02.1080p.mkv")
	if err := os.WriteFile(unsafeFile, []byte("unsafe"), 0o644); err != nil {
		t.Fatal(err)
	}
	ownedFile := filepath.Join(root, "Show.S01E03.1080p.mkv")
	if err := os.WriteFile(ownedFile, []byte("owned"), 0o644); err != nil {
		t.Fatal(err)
	}
	sameSizeFile := filepath.Join(root, "Show.S01E04.1080p.mkv")
	if err := os.WriteFile(sameSizeFile, []byte("src!"), 0o644); err != nil {
		t.Fatal(err)
	}

	engine := &packInspectEngine{
		torrents: []models.TorrentView{{Hash: "abc123", Name: "Show.S01E03.1080p.mkv", SavePath: root}},
	}
	cfg := DefaultConfig()
	cfg.DownloadDir = root
	cfg.DownloadTempDir = &temp
	cfg.TrashPath = &trash
	cfg.Series = []SeriesConfig{{Name: "Show", Enabled: true}}

	items := FindOrphanedDownloads(&cfg, db, engine)
	byName := map[string]OrphanDownload{}
	for _, item := range items {
		byName[item.Name] = item
	}
	if item, ok := byName["Show.S01E01.1080p.mkv"]; !ok || !item.Safe {
		t.Fatalf("the safe leftover was not proposed: %+v", items)
	}
	if item, ok := byName["Show.S01E02.1080p.mkv"]; !ok || item.Safe {
		t.Fatalf("the unprotected leftover was marked safe: %+v", items)
	}
	if item, ok := byName["Show.S01E04.1080p.mkv"]; !ok || !item.Safe {
		t.Fatalf("a same-size library duplicate was not marked safe: %+v", items)
	}
	if _, ok := byName["Show.S01E03.1080p.mkv"]; ok {
		t.Fatal("a file owned by a live torrent was proposed")
	}

	moved, movedBytes, err := CleanOrphanedDownloads(&cfg, db, engine)
	if err != nil {
		t.Fatalf("clean: %v", err)
	}
	if moved != 2 || movedBytes <= 0 {
		t.Fatalf("moved %d entries / %d bytes, want the two safe ones", moved, movedBytes)
	}
	if _, err := os.Stat(safeFile); !os.IsNotExist(err) {
		t.Fatal("the safe source was not moved")
	}
	if _, err := os.Stat(sameSizeFile); !os.IsNotExist(err) {
		t.Fatal("the same-size duplicate was not moved")
	}
	if _, err := os.Stat(unsafeFile); err != nil {
		t.Fatal("the unsafe source must stay")
	}
}
