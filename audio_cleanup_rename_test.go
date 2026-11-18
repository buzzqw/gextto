// audio_cleanup_rename_test.go pins the rename that follows the audio
// cleanup: the cleanup removes the foreign tracks and the file gets the name
// they leave behind, in the library and after an import. The name used to keep
// the old language tag forever, because the periodic library check compares
// only the prefix of the name.

package gextto

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newAudioCleanupRenameEnv configures Series A with renaming on and a real
// two-track episode on disk, named as if both languages were still there.
func newAudioCleanupRenameEnv(t *testing.T, tracks []testAudioTrack) (*AppState, string) {
	t.Helper()
	requireAudioTools(t)
	s := newTestAppState(t)
	s.config_path = t.TempDir() // latestConfig falls back to s.cfg
	cfg := s.cfg
	cfg.RenameEpisodes = true
	cfg.TmdbAPIKey = nil
	cfg.RenameFormat = "custom"
	cfg.RenameTemplate = "{Serie} - {Stagione}{Episodio} - {Titolo} - [{Risoluzione}][{Lingue}]"
	archive := filepath.Join(t.TempDir(), "Series A")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.Series = []SeriesConfig{{Name: "Series A", ArchivePath: archive, Enabled: true}}
	if _, err := s.db.db.Exec("INSERT INTO series(name) VALUES ('Series A')"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(archive, "Series A - S01E01 - Episodio 1 - [480p][IT+EN].mkv")
	makeAudioCleanupFixture(t, path, tracks)
	if _, err := s.db.db.Exec(`INSERT INTO episodes(series_id,season,episode,title,downloaded_at,archive_path)
		SELECT id,1,1,'Series.A.S01E01.480p.WEB.H264-GROUP','2026-01-01 00:00:00',?1 FROM series WHERE name='Series A'`, path); err != nil {
		t.Fatal(err)
	}
	return s, path
}

// After the cleanup the file says only the language that is left, and the
// database knows the new path.
func TestAudioCleanupRenameFollowsTheRemainingTracks(t *testing.T) {
	s, path := newAudioCleanupRenameEnv(t, []testAudioTrack{
		{language: "ita"},
		{language: "eng"},
	})
	if _, err := RunAudioCleanup(context.Background(), path, AudioCleanupOptions{Language: "ita", Policy: audioCleanupPolicyAll}); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	renamed, err := renameAfterAudioCleanup(s.cfg, s.db, path)
	if err != nil {
		t.Fatalf("rename after cleanup: %v", err)
	}
	if renamed == path || renamed == "" {
		t.Fatalf("file not renamed: %q", renamed)
	}
	if _, err := os.Stat(renamed); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("the old name is still there")
	}
	base := filepath.Base(renamed)
	if !strings.Contains(base, "[IT]") || strings.Contains(base, "EN") {
		t.Fatalf("name %q does not follow the remaining tracks", base)
	}
	var stored string
	if err := s.db.db.QueryRow(`SELECT e.archive_path FROM episodes e JOIN series s ON s.id=e.series_id
		WHERE s.name='Series A' AND season=1 AND episode=1`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != renamed {
		t.Fatalf("stored path = %q, want %q", stored, renamed)
	}
	// The same call on the new name has nothing left to do.
	again, err := renameAfterAudioCleanup(s.cfg, s.db, renamed)
	if err != nil {
		t.Fatalf("second rename: %v", err)
	}
	if again != renamed {
		t.Fatalf("second rename moved the file again: %q", again)
	}
}

// The files of a series that left the library keep the name they have: there
// is no template to compute a new one from.
func TestAudioCleanupRenameLeavesASeriesThatLeftTheLibrary(t *testing.T) {
	s, path := newAudioCleanupRenameEnv(t, []testAudioTrack{
		{language: "ita"},
		{language: "eng"},
	})
	s.cfg.Series = nil

	renamed, err := renameAfterAudioCleanup(s.cfg, s.db, path)
	if err != nil {
		t.Fatalf("rename after cleanup: %v", err)
	}
	if renamed != path {
		t.Fatalf("a series outside the library was renamed to %q", renamed)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the file was moved: %v", err)
	}
}

// A cleaned movie updates the path its torrent records.
func TestAudioCleanupRenameUpdatesAMoviePath(t *testing.T) {
	requireAudioTools(t)
	s := newTestAppState(t)
	s.config_path = t.TempDir()
	cfg := s.cfg
	cfg.RenameEpisodes = true
	cfg.TmdbAPIKey = nil
	cfg.RenameFormat = "custom"
	cfg.RenameMovieTemplate = "{Titolo} ({Anno}) - [{Risoluzione}][{Lingue}]"

	path := filepath.Join(t.TempDir(), "Example Movie (2024) - [2160p][IT+EN].mkv")
	makeAudioCleanupFixture(t, path, []testAudioTrack{{language: "ita"}, {language: "eng"}})
	if _, err := RunAudioCleanup(context.Background(), path, AudioCleanupOptions{Language: "ita", Policy: audioCleanupPolicyAll}); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := s.db.db.Exec(`INSERT INTO movies(name,year,magnet_hash,downloaded_at) VALUES ('Example Movie',2024,'abc123','2026-01-01 00:00:00')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.db.Exec(`INSERT INTO torrent_meta(hash,status,processed_path,updated_at,created_at) VALUES ('abc123','completed',?1,'2026-01-01 00:00:00','2026-01-01 00:00:00')`, path); err != nil {
		t.Fatal(err)
	}

	renamed, err := renameAfterAudioCleanup(cfg, s.db, path)
	if err != nil {
		t.Fatalf("rename after cleanup: %v", err)
	}
	if renamed == path || renamed == "" {
		t.Fatalf("movie not renamed: %q", renamed)
	}
	if _, err := os.Stat(renamed); err != nil {
		t.Fatalf("renamed movie missing: %v", err)
	}
	base := filepath.Base(renamed)
	if !strings.Contains(base, "[IT]") || strings.Contains(base, "EN") {
		t.Fatalf("movie name %q does not follow the remaining tracks", base)
	}
	var stored string
	if err := s.db.db.QueryRow("SELECT processed_path FROM torrent_meta WHERE hash='abc123'").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != renamed {
		t.Fatalf("processed path = %q, want %q", stored, renamed)
	}
}

// A torrent the operator excluded from renaming keeps its file's name: the
// cleanup changes the tracks, not the choice.
func TestAudioCleanupRenameRespectsNoRename(t *testing.T) {
	s, path := newAudioCleanupRenameEnv(t, []testAudioTrack{
		{language: "ita"},
		{language: "eng"},
	})
	if _, err := RunAudioCleanup(context.Background(), path, AudioCleanupOptions{Language: "ita", Policy: audioCleanupPolicyAll}); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := s.db.db.Exec(`UPDATE episodes SET magnet_hash='abc123' WHERE season=1 AND episode=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.db.Exec(`INSERT INTO torrent_meta(hash,no_rename,updated_at,created_at) VALUES ('abc123',1,'2026-01-01 00:00:00','2026-01-01 00:00:00')`); err != nil {
		t.Fatal(err)
	}

	renamed, err := renameAfterAudioCleanup(s.cfg, s.db, path)
	if err != nil {
		t.Fatalf("rename after cleanup: %v", err)
	}
	if renamed != path {
		t.Fatalf("a no-rename torrent was renamed to %q", renamed)
	}
}

// The library pass cleans the file and returns the name it has afterwards, so
// its progress is remembered under the real path.
func TestLibraryAudioCleanupRenamesTheFileItCleaned(t *testing.T) {
	s, path := newAudioCleanupRenameEnv(t, []testAudioTrack{
		{language: "ita"},
		{language: "eng"},
	})
	series := "Series A"

	outcome, cleaned, report := runLibraryAudioCleanupFile(context.Background(), s,
		audioLibraryCandidate{Path: path, Series: &series},
		AudioCleanupOptions{Language: "ita", Policy: audioCleanupPolicyAll})
	if outcome != "cleaned" || !report.Applied {
		t.Fatalf("outcome %q applied %v, want a cleaned file", outcome, report.Applied)
	}
	if cleaned == path || cleaned == "" {
		t.Fatalf("the library pass kept the old name %q", cleaned)
	}
	if _, err := os.Stat(cleaned); err != nil {
		t.Fatalf("cleaned file missing: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("the old name is still there")
	}
	if _, _, _, _, _, ok, err := s.db.ArchivedEpisodeForPath(cleaned); err != nil || !ok {
		t.Fatalf("the cleaned file is not the stored episode: %v", err)
	}
}
