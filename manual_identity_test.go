package gextto

// manual_identity_test.go pins what happens to a torrent that arrives without a
// release — a magnet pasted in «Aggiungi torrent», an uploaded .torrent, the
// qBittorrent-compatible add — whose completion used to end in the download
// folder whatever it was: the torrent's own name, or the names of the files it
// carries, say what it is and the download reaches the library like the others.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// newHandAddedEnv is the environment of a torrent added by hand: it has no
// release in the database, its payload is complete in the download folder and
// Series A is in the library.
func newHandAddedEnv(t *testing.T, name string) *episodeEnv {
	t.Helper()
	e := newBehaviourEnv(t)
	if _, err := e.db.db.Exec("DELETE FROM torrent_meta"); err != nil {
		t.Fatal(err)
	}
	library := filepath.Join(e.library, "Series A")
	if err := os.MkdirAll(library, 0o755); err != nil {
		t.Fatal(err)
	}
	e.cfg.Series = []SeriesConfig{{Name: "Series A", ArchivePath: library, Enabled: true}}
	writeVideo(t, filepath.Join(e.downloads, name), 5000)
	e.session.list = []models.TorrentView{{Hash: episodeHash, Name: name, SavePath: e.downloads, State: "seeding",
		Progress: 100, TotalSize: 5000, TotalDone: 5000, HasMetadata: true}}
	return &episodeEnv{behaviourEnv: e, library: library, name: name}
}

// The name of a pasted magnet is enough: the episode is identified and, at the
// end, filed into the library of its series instead of staying in the download
// folder.
func TestHandAddedTorrentIsIdentifiedAndFiledIntoTheLibrary(t *testing.T) {
	p := newHandAddedEnv(t, "Series.A.S01E03.1080p.WEB.H264-GROUP.mkv")
	if status, _ := p.db.TorrentStatus(episodeHash); status != nil {
		t.Fatalf("the torrent already has a status: %v", status)
	}
	if !p.finish(t) {
		t.Fatal("the identified episode was not imported")
	}
	meta, err := p.db.TorrentMeta(episodeHash)
	if err != nil || meta == nil {
		t.Fatalf("TorrentMeta: %v (err %v)", meta, err)
	}
	release := meta.Release
	if release.Kind != "series" || release.Series == nil || *release.Series != "Series A" ||
		release.Season == nil || *release.Season != 1 || release.Episode == nil || *release.Episode != 3 {
		t.Fatalf("identified as %+v, want Series A S01E03", release)
	}
	if reason, _ := p.db.TorrentReason(episodeHash); reason == nil || *reason != manualReason {
		t.Fatalf("reason = %v, want %q", reason, manualReason)
	}
	archived, _ := p.db.TorrentProcessed(episodeHash)
	if archived == nil || !strings.HasPrefix(*archived, p.library) {
		t.Fatalf("archived path = %v, want inside %q", archived, p.library)
	}
	if got := libraryVideos(t, p.library); len(got) != 1 {
		t.Fatalf("library holds %v, want the episode", got)
	}
}

// A pack whose name says nothing is read from the episodes inside it.
func TestHandAddedPackNamedLikeNothingIsIdentifiedFromItsFiles(t *testing.T) {
	e := newBehaviourEnv(t)
	if _, err := e.db.db.Exec("DELETE FROM torrent_meta"); err != nil {
		t.Fatal(err)
	}
	library := filepath.Join(e.library, "Series A")
	if err := os.MkdirAll(library, 0o755); err != nil {
		t.Fatal(err)
	}
	e.cfg.Series = []SeriesConfig{{Name: "Series A", ArchivePath: library, Enabled: true}}
	pack := filepath.Join(e.downloads, "S01")
	if err := os.MkdirAll(pack, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, episode := range []string{"Series.A.S01E01.1080p.WEB.H264-GROUP.mkv", "Series.A.S01E02.1080p.WEB.H264-GROUP.mkv"} {
		writeVideo(t, filepath.Join(pack, episode), 1000)
	}
	event := models.TorrentEvent{Kind: "torrent_finished", Hash: episodeHash, Name: "S01", SavePath: e.downloads}
	release := tevManualReleaseIdentity(e.cfg, e.session, event)
	if release == nil {
		t.Fatal("a pack named «S01» was not recognised from its files")
	}
	if release.Kind != "series" || release.Series == nil || *release.Series != "Series A" {
		t.Fatalf("identified as %+v, want Series A", release)
	}
	if !release.IsPack || release.Season == nil || *release.Season != 1 || len(release.EpisodeRange) != 2 {
		t.Fatalf("identified as %+v, want the two episodes of season 1", release)
	}
}

// Nothing to recognise — no episode, no film, no video inside — and the
// torrent keeps the behaviour it had: completed where it was downloaded.
func TestHandAddedTorrentWithNothingToRecogniseStaysWhereItIs(t *testing.T) {
	p := newHandAddedEnv(t, "backup-of-someday.zip")
	if p.finish(t) {
		t.Fatal("a torrent with nothing to recognise was filed into the library")
	}
	if status, _ := p.db.TorrentStatus(episodeHash); status == nil || *status != "completed" {
		t.Fatalf("status = %v, want completed", status)
	}
	if meta, _ := p.db.TorrentMeta(episodeHash); meta != nil && strings.TrimSpace(meta.Release.Title) != "" {
		t.Fatalf("an identity was invented: %q", meta.Release.Title)
	}
	if archived, _ := p.db.TorrentProcessed(episodeHash); archived != nil && strings.TrimSpace(*archived) != "" {
		t.Fatalf("an archive path was recorded: %q", *archived)
	}
	if got := libraryVideos(t, p.library); len(got) != 0 {
		t.Fatalf("library holds %v, want nothing", got)
	}
	if _, err := os.Stat(filepath.Join(p.downloads, p.name)); err != nil {
		t.Fatalf("the download left the download folder: %v", err)
	}
}
