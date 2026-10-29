package gextto

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// The same infohash is downloaded again when the archived copy has gone (a film
// deleted from the library, an episode removed by hand) or when a release is
// proposed once more after a failure. Its row must not keep saying "completed":
// the completion handler trusts that state, and the first storage move of the
// new attempt — the RAM-disk admission, with nothing downloaded yet — was taken
// for the end of the download, so a file full of zeros was quarantined.
const repeatDownloadHash = "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"

func repeatDownloadRelease() *models.Release {
	year := int64(2026)
	return &models.Release{
		Title:  "LEGO.Star.Wars.the.Mandalorian.2026.iTALiAN.MULTi.1080p.WEB.H264-NTROPiC",
		Magnet: "magnet:?xt=urn:btih:" + repeatDownloadHash,
		Kind:   "movie",
		Year:   &year,
	}
}

func repeatDownloadDB(t *testing.T) *Database {
	t.Helper()
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "gextto.db"))
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestReRegisteringACompletedReleaseStartsTheAttemptAgain is the database half
// of the bug: a release whose archived copy is gone goes back to "queued".
func TestReRegisteringACompletedReleaseStartsTheAttemptAgain(t *testing.T) {
	db := repeatDownloadDB(t)
	release := repeatDownloadRelease()
	if err := db.RegisterTorrentScored(release, 1150); err != nil {
		t.Fatalf("RegisterTorrentScored: %v", err)
	}
	// The previous attempt completed and its copy is no longer on disk.
	gone := filepath.Join(t.TempDir(), "film", "LEGO Star Wars The Mandalorian (2026) [1080p].mkv")
	if err := db.MarkTorrentCompleted(repeatDownloadHash, gone, 4096); err != nil {
		t.Fatalf("MarkTorrentCompleted: %v", err)
	}
	if status, _ := db.TorrentStatus(repeatDownloadHash); status == nil || *status != "completed" {
		t.Fatalf("setup: status = %v, want completed", status)
	}

	if err := db.RegisterTorrentScored(release, 1150); err != nil {
		t.Fatalf("RegisterTorrentScored: %v", err)
	}

	status, err := db.TorrentStatus(repeatDownloadHash)
	if err != nil {
		t.Fatalf("TorrentStatus: %v", err)
	}
	if status == nil || *status != "queued" {
		t.Fatalf("status after registering a new download = %v, want queued", status)
	}
	processed, err := db.TorrentProcessed(repeatDownloadHash)
	if err != nil {
		t.Fatalf("TorrentProcessed: %v", err)
	}
	if processed != nil && strings.TrimSpace(*processed) != "" {
		t.Fatalf("processed path of the previous attempt must be cleared, got %q", *processed)
	}
	times, err := db.TorrentTimes(repeatDownloadHash)
	if err != nil || times == nil {
		t.Fatalf("TorrentTimes: %v %v", times, err)
	}
	if times.CompletedAt != nil {
		t.Fatalf("completed_at of the previous attempt must be cleared, got %q", *times.CompletedAt)
	}
}

// TestReRegisteringKeepsTheCompletionOfACopyStillInTheLibrary pins the other
// side: a duplicate add of something already archived keeps its completion, so
// the repeated completion is still recognised and the library file is not
// imported a second time.
func TestReRegisteringKeepsTheCompletionOfACopyStillInTheLibrary(t *testing.T) {
	db := repeatDownloadDB(t)
	release := repeatDownloadRelease()
	library := filepath.Join(t.TempDir(), "film")
	if err := os.MkdirAll(library, 0o755); err != nil {
		t.Fatal(err)
	}
	archived := filepath.Join(library, "LEGO Star Wars The Mandalorian (2026) [1080p].mkv")
	if err := os.WriteFile(archived, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := db.RegisterTorrentScored(release, 1150); err != nil {
		t.Fatalf("RegisterTorrentScored: %v", err)
	}
	if err := db.MarkTorrentCompleted(repeatDownloadHash, archived, 5); err != nil {
		t.Fatalf("MarkTorrentCompleted: %v", err)
	}

	if err := db.RegisterTorrentScored(release, 1150); err != nil {
		t.Fatalf("RegisterTorrentScored: %v", err)
	}

	status, _ := db.TorrentStatus(repeatDownloadHash)
	if status == nil || *status != "completed" {
		t.Fatalf("status = %v, want completed for a copy still in the library", status)
	}
	processed, _ := db.TorrentProcessed(repeatDownloadHash)
	if processed == nil || *processed != archived {
		t.Fatalf("processed path = %v, want %q", processed, archived)
	}
}

// TestStorageMoveOfARepeatedDownloadIsNotACompletion reproduces the 29 October
// incident: the engine moves a just-started download onto the RAM disk, so its
// payload is a preallocated file of zeros. The storage move must not be taken
// for the end of the download, and the file must not be quarantined.
func TestStorageMoveOfARepeatedDownloadIsNotACompletion(t *testing.T) {
	db := repeatDownloadDB(t)
	release := repeatDownloadRelease()
	if err := db.RegisterTorrentScored(release, 1150); err != nil {
		t.Fatalf("RegisterTorrentScored: %v", err)
	}
	gone := filepath.Join(t.TempDir(), "film", "LEGO Star Wars The Mandalorian (2026) [1080p].mkv")
	if err := db.MarkTorrentCompleted(repeatDownloadHash, gone, 4096); err != nil {
		t.Fatalf("MarkTorrentCompleted: %v", err)
	}
	// Gextto queues the release again: the row restarts from scratch.
	if err := db.RegisterTorrentScored(release, 1150); err != nil {
		t.Fatalf("RegisterTorrentScored: %v", err)
	}

	name := "LEGO.Star.Wars.the.Mandalorian.2026.iTALiAN.MULTi.1080p.WEB.H264-NTROPiC.mkv"
	ramdisk := filepath.Join(t.TempDir(), "ramdisk")
	if err := os.MkdirAll(ramdisk, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(ramdisk, name)
	if err := os.WriteFile(payload, make([]byte, 128*1024), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.Settings = map[string]string{}
	// The quarantine must land inside the test's own folders, never in the
	// checkout's data directory: DefaultConfig points there.
	trash := filepath.Join(t.TempDir(), "trash")
	cfg.TrashPath = &trash
	session := &stubTorrentSession{list: []models.TorrentView{{
		Hash:      repeatDownloadHash,
		Name:      name,
		SavePath:  ramdisk,
		State:     "downloading",
		Progress:  0,
		TotalSize: 128 * 1024,
		TotalDone: 0,
	}}}
	event := models.TorrentEvent{
		Kind:     "storage_moved",
		Hash:     repeatDownloadHash,
		Name:     name,
		SavePath: ramdisk,
	}
	processed, err := HandleTorrentEvent(&cfg, session, db, map[string]struct{}{}, map[string]struct{}{},
		map[string]StorageMoveRetry{}, event, NewTmdbClientWithLanguage(nil, "it-IT"), FromConfig(&cfg))
	if err != nil {
		t.Fatalf("storage move before completion must be ignored, got error: %v", err)
	}
	if processed {
		t.Fatal("a storage move with nothing downloaded must not be processed as a completion")
	}
	if _, err := os.Stat(payload); err != nil {
		t.Fatalf("the download must stay where it is: %v", err)
	}
	if _, err := os.Stat(payload + ".corrupt"); err == nil {
		t.Fatal("the still empty payload was quarantined")
	}
	status, _ := db.TorrentStatus(repeatDownloadHash)
	if status == nil || *status == "error" {
		t.Fatalf("status = %v, want the download to go on", status)
	}
}
