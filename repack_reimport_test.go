package gextto

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// repackEd2kRelease is an eD2k REPACK of one episode: the library name Gextto
// gives the file drops the REPACK marker.
func repackEd2kRelease(t *testing.T) models.Release {
	t.Helper()
	name := "Show.S03E04.Title.Repack.ITA.ENG.2160p.AMZN.WEB-DL.DDP5.1.DV.HDR.H.265-GRP.mkv"
	release := models.Release{
		Title:        name,
		Ed2kLink:     "ed2k://|file|" + name + "|6031566092|062504458548173fa0d1c962900210e1|/",
		Source:       "indexarr:example",
		Network:      networkEd2k,
		Kind:         "series",
		Series:       stringPtr("Show"),
		Season:       int64Ptr(3),
		Episode:      int64Ptr(4),
		EpisodeRange: []int64{4},
		Quality:      ParseQuality(name),
		Seeders:      -1,
		Peers:        -1,
	}
	if !release.Quality.IsRepack {
		t.Fatal("the test release must be a REPACK")
	}
	return release
}

// libraryFile writes a stand-in for the archived file and returns its path.
func libraryFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestImportedRepackIsNotDownloadedAgain reproduces a REPACK imported over an
// episode row that a library scan had recreated from the old file (old name,
// no hash): the completion must record the release on the row, so the next
// search does not take the same REPACK for an upgrade of itself.
func TestImportedRepackIsNotDownloadedAgain(t *testing.T) {
	db := newTestDB(t)
	release := repackEd2kRelease(t)
	libraryPath := libraryFile(t, "Show - S03E04 - Title - [WEB-DL][2160p][h265][DV HDR10][DDP][IT+EN].mkv")

	// The row as a library scan recreates it: the replaced 1080p copy's name
	// and score, and no hash.
	if err := db.SyncArchiveFileScored("Show", 3, 4, "Show - S03E04 - Title - [1080p][h265][AAC 5.1][IT+EN]", "/old/1080p.mkv", 800, 1256); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkReleaseCompleted(&release, libraryPath, 6031566092); err != nil {
		t.Fatal(err)
	}
	var hash, title string
	if err := db.db.QueryRow("SELECT COALESCE(magnet_hash,''), COALESCE(title,'') FROM episodes WHERE season=3 AND episode=4").Scan(&hash, &title); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, hash, "062504458548173fa0d1c962900210e1")
	assertEqual(t, title, release.Title)

	approved, reason, err := db.CheckSeriesScored(&release, release.Quality.Score(), 200, &models.ApprovalContext{})
	if err != nil {
		t.Fatal(err)
	}
	if approved || reason != "duplicate" {
		t.Fatalf("the REPACK already in the library was approved again: approved=%v reason=%q", approved, reason)
	}
}

// TestCompletedReleaseWithLibraryFileIsADuplicate covers a row that has already
// lost the hash: the completed download of the same release, whose library file
// is still there, is the copy we have. Once the file is gone (deleted on
// purpose), the release may be downloaded again.
func TestCompletedReleaseWithLibraryFileIsADuplicate(t *testing.T) {
	db := newTestDB(t)
	release := repackEd2kRelease(t)
	libraryPath := libraryFile(t, "Show - S03E04 - Title - [WEB-DL][2160p][h265][DV HDR10][DDP][IT+EN].mkv")

	if err := db.SyncArchiveFileScored("Show", 3, 4, "Show - S03E04 - Title - [1080p][h265][AAC 5.1][IT+EN]", libraryPath, 800, 1256); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO torrent_meta(hash,series_name,season,episode,status,processed_path,updated_at) VALUES ('062504458548173fa0d1c962900210e1','Show',3,4,'completed',?1,datetime('now'))", libraryPath); err != nil {
		t.Fatal(err)
	}
	context := &models.ApprovalContext{DryRun: true}
	approved, reason, err := db.CheckSeriesScored(&release, release.Quality.Score(), 200, context)
	if err != nil {
		t.Fatal(err)
	}
	if approved || reason != "duplicate" {
		t.Fatalf("an imported release was approved again: approved=%v reason=%q", approved, reason)
	}

	if err := os.Remove(libraryPath); err != nil {
		t.Fatal(err)
	}
	imported, err := db.releaseAlreadyImported("062504458548173fa0d1c962900210e1", "Show", 3, 4)
	if err != nil {
		t.Fatal(err)
	}
	if imported {
		t.Fatal("a release whose library file was deleted still counts as imported")
	}
}
