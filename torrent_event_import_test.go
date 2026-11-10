package gextto

// torrent_event_import_test.go pins the library import of a single episode
// (tevCompleteTorrentOptions): the file is renamed in the library, a worse copy
// it upgrades leaves, a release no better than the library's copy is
// discarded, and a torrent marked no-rename keeps its name.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

const episodeHash = "4444444444444444444444444444444444444444"

type episodeEnv struct {
	*behaviourEnv
	library string
	name    string
}

// newEpisodeEnv registers Series A S01E03, downloaded and seeding from the
// download folder, with the series' library folder configured: the import
// copies it into the library and renames it there.
func newEpisodeEnv(t *testing.T) *episodeEnv {
	t.Helper()
	e := newBehaviourEnv(t)
	library := filepath.Join(e.library, "Series A")
	if err := os.MkdirAll(library, 0o755); err != nil {
		t.Fatal(err)
	}
	e.cfg.Series = []SeriesConfig{{Name: "Series A", ArchivePath: library, Enabled: true}}
	show, season, episode := "Series A", int64(1), int64(3)
	release := &models.Release{Title: "Series.A.S01E03.1080p.WEB.H264-GROUP", Magnet: "magnet:?xt=urn:btih:" + episodeHash,
		Kind: "series", Series: &show, Season: &season, Episode: &episode,
		Quality: models.Quality{Resolution: "1080p", Source: "webdl"}}
	if err := e.db.RegisterTorrentScored(release, 500); err != nil {
		t.Fatal(err)
	}
	name := release.Title + ".mkv"
	writeVideo(t, filepath.Join(e.downloads, name), 5000)
	e.session.list = []models.TorrentView{{Hash: episodeHash, Name: name, SavePath: e.downloads, State: "seeding",
		Progress: 100, TotalSize: 5000, TotalDone: 5000, HasMetadata: true}}
	return &episodeEnv{behaviourEnv: e, library: library, name: name}
}

func writeVideo(t *testing.T, path string, size int) {
	t.Helper()
	payload := append([]byte("\x1a\x45\xdf\xa3 video "), []byte(strings.Repeat("v", size))...)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (p *episodeEnv) finish(t *testing.T) bool {
	t.Helper()
	event := models.TorrentEvent{Kind: "torrent_finished", Hash: episodeHash, Name: p.name, SavePath: p.downloads}
	processed, err := HandleTorrentEvent(p.cfg, p.session, p.db, p.moves, p.postSeed, p.retries, event,
		NewTmdbClientWithLanguage(nil, "it-IT"), FromConfig(p.cfg))
	if err != nil {
		t.Fatalf("episode completion: %v", err)
	}
	return processed
}

// With renaming on, the episode is renamed in the library and recorded
// there, while the torrent keeps seeding its own copy.
func TestEpisodeImportRenamesInTheLibrary(t *testing.T) {
	p := newEpisodeEnv(t)
	p.cfg.RenameEpisodes = true
	if !p.finish(t) {
		t.Fatal("the episode was not imported")
	}
	archived, _ := p.db.TorrentProcessed(episodeHash)
	if archived == nil || !strings.HasPrefix(*archived, p.library) {
		t.Fatalf("archived path = %v, want inside %q", archived, p.library)
	}
	if filepath.Base(*archived) == p.name {
		t.Fatalf("the episode kept its release name %q", p.name)
	}
	if _, err := os.Stat(*archived); err != nil {
		t.Fatalf("the renamed episode is missing: %v", err)
	}
	if got := libraryVideos(t, p.library); len(got) != 1 {
		t.Fatalf("library holds %v, want one episode", got)
	}
	if _, err := os.Stat(filepath.Join(p.downloads, p.name)); err != nil {
		t.Fatalf("the seeding copy left the download folder: %v", err)
	}
}

// A torrent marked no-rename is imported under its own name, renaming on.
func TestEpisodeImportKeepsTheNameWhenAsked(t *testing.T) {
	p := newEpisodeEnv(t)
	p.cfg.RenameEpisodes = true
	if err := p.db.SetTorrentNoRename(episodeHash, true); err != nil {
		t.Fatal(err)
	}
	if !p.finish(t) {
		t.Fatal("the episode was not imported")
	}
	archived, _ := p.db.TorrentProcessed(episodeHash)
	if archived == nil || filepath.Base(*archived) != p.name {
		t.Fatalf("archived path = %v, want the release name %q", archived, p.name)
	}
}

// A worse copy already in the library leaves when the new one arrives.
func TestEpisodeImportUpgradesAWorseCopy(t *testing.T) {
	p := newEpisodeEnv(t)
	worse := filepath.Join(p.library, "Series.A.S01E03.480p.HDTV.x264-OLD.mkv")
	writeVideo(t, worse, 3000)
	if !p.finish(t) {
		t.Fatal("the upgrade was not imported")
	}
	if _, err := os.Stat(worse); err == nil {
		t.Fatal("the worse copy is still in the library")
	}
	if got := libraryVideos(t, p.library); len(got) != 1 {
		t.Fatalf("library holds %v, want only the new episode", got)
	}
}

// A release no better than the library's copy is discarded and the library's
// copy stays.
func TestEpisodeImportDiscardsAnInferiorRelease(t *testing.T) {
	p := newEpisodeEnv(t)
	better := filepath.Join(p.library, "Series.A.S01E03.2160p.BluRay.REMUX.HDR.DTS-HD.MA.5.1-GROUP.mkv")
	writeVideo(t, better, 9000)
	if p.finish(t) {
		t.Fatal("an inferior release was imported")
	}
	if _, err := os.Stat(better); err != nil {
		t.Fatalf("the better copy left the library: %v", err)
	}
	status, _ := p.db.TorrentStatus(episodeHash)
	if status == nil || *status != "error" {
		t.Fatalf("status = %v, want error for an inferior release", status)
	}
}
