package gextto

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

func orphanTestSetup(t *testing.T) (*Config, string, string) {
	t.Helper()
	root := t.TempDir()
	temp := filepath.Join(root, "transmission-temp")
	trash := filepath.Join(root, "trash")
	if err := os.MkdirAll(temp, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{DataDir: root, LibtorrentTempDir: &temp, TrashPath: &trash, Settings: map[string]string{}}
	return cfg, temp, trash
}

// makeEntry creates temp/name/file.mkv with every timestamp set to age ago.
func makeEntry(t *testing.T, temp, name string, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(temp, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "file.mkv")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	for _, path := range []string{file, dir} {
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

func TestTrashOrphanedTempDataMovesOnlyOldUnownedEntries(t *testing.T) {
	cfg, temp, trash := orphanTestSetup(t)
	week := 7 * 24 * time.Hour
	orphan := makeEntry(t, temp, "Old.Orphan.S01", week+time.Hour)
	recent := makeEntry(t, temp, "Recent.Orphan.S01", 2*24*time.Hour)
	owned := makeEntry(t, temp, "Owned.Show.S01", 30*24*time.Hour)
	inside := makeEntry(t, temp, "Holder", 30*24*time.Hour)
	hidden := makeEntry(t, temp, ".Old.gextto-copy-x", 30*24*time.Hour)
	session := &stubTorrentSession{list: []models.TorrentView{
		{Hash: "a", Name: "Owned.Show.S01", SavePath: temp, HasMetadata: true},
		{Hash: "b", Name: "Movie.mkv", SavePath: filepath.Join(temp, "Holder"), HasMetadata: true},
	}}
	if moved := trashOrphanedTempData(cfg, session, nil, time.Now()); moved != 1 {
		t.Fatalf("moved = %d, want 1", moved)
	}
	if exists(orphan) || !exists(filepath.Join(trash, "Old.Orphan.S01", "file.mkv")) {
		t.Fatal("the week-old orphan should be in the trash, not deleted")
	}
	for _, kept := range []string{recent, owned, inside, hidden} {
		if !exists(kept) {
			t.Fatalf("%s must be kept", filepath.Base(kept))
		}
	}
}

func TestTrashOrphanedTempDataLooksInsideFolders(t *testing.T) {
	cfg, temp, _ := orphanTestSetup(t)
	dir := makeEntry(t, temp, "Old.Folder", 30*24*time.Hour)
	// A file written yesterday inside an old folder means it is still alive.
	fresh := filepath.Join(dir, "new.part")
	if err := os.WriteFile(fresh, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	session := &stubTorrentSession{list: []models.TorrentView{{Hash: "a", Name: "Other", SavePath: temp, HasMetadata: true}}}
	if moved := trashOrphanedTempData(cfg, session, nil, time.Now()); moved != 0 || !exists(dir) {
		t.Fatalf("a folder with a recent file must be kept (moved=%d)", moved)
	}
}

func TestTrashOrphanedTempDataSafetyGuards(t *testing.T) {
	cfg, temp, _ := orphanTestSetup(t)
	orphan := makeEntry(t, temp, "Old.Orphan", 30*24*time.Hour)
	known := []models.TorrentView{{Hash: "a", Name: "Other", SavePath: temp, HasMetadata: true}}

	if trashOrphanedTempData(cfg, &stubTorrentSession{}, nil, time.Now()) != 0 || !exists(orphan) {
		t.Fatal("an empty torrent list (session maybe not loaded) must move nothing")
	}
	noMetadata := append([]models.TorrentView{{Hash: "c", Name: "", SavePath: temp}}, known...)
	if trashOrphanedTempData(cfg, &stubTorrentSession{list: noMetadata}, nil, time.Now()) != 0 || !exists(orphan) {
		t.Fatal("a torrent still fetching metadata must stop the sweep")
	}
	cfg.Settings[tempOrphanEnabledSetting] = "false"
	if trashOrphanedTempData(cfg, &stubTorrentSession{list: known}, nil, time.Now()) != 0 || !exists(orphan) {
		t.Fatal("the sweep must be off when disabled")
	}
	cfg.Settings[tempOrphanEnabledSetting] = "true"
	cfg.Settings[tempOrphanMinAgeSetting] = "60"
	if trashOrphanedTempData(cfg, &stubTorrentSession{list: known}, nil, time.Now()) != 0 || !exists(orphan) {
		t.Fatal("temp_orphan_min_age_days must be honoured")
	}
	cfg.Settings[tempOrphanMinAgeSetting] = "20"
	if trashOrphanedTempData(cfg, &stubTorrentSession{list: known}, nil, time.Now()) != 1 || exists(orphan) {
		t.Fatal("with a 20-day threshold the 30-day orphan should be moved")
	}
}

func TestCleanerCopyFileKeepsSparseFilesSparse(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "partial.mkv")
	file, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	const size = 64 << 20 // 64 MiB declared, 2 MiB of real data
	data := bytes.Repeat([]byte{0xab}, 1<<20)
	for _, offset := range []int64{0, 30 << 20} {
		if _, err := file.WriteAt(data, offset); err != nil {
			t.Fatal(err)
		}
	}
	// A few non-zero bytes in an otherwise empty block, and a trailing hole.
	if _, err := file.WriteAt([]byte("tail"), 40<<20+12345); err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(size); err != nil {
		t.Fatal(err)
	}
	file.Close()

	target := filepath.Join(dir, "trash", "partial.mkv")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cleanerCopyFile(source, target, 0o644); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(source)
	copied, _ := os.ReadFile(target)
	if !bytes.Equal(original, copied) {
		t.Fatal("the sparse copy changed the content")
	}
	var stat syscall.Stat_t
	if err := syscall.Stat(target, &stat); err != nil {
		t.Fatal(err)
	}
	if allocated := stat.Blocks * 512; allocated > 8<<20 {
		t.Fatalf("the copy allocated %d bytes for 3 MiB of data: holes were written as zeros", allocated)
	}
}
