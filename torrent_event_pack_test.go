package gextto

// torrent_event_pack_test.go pins the season-pack import of HandleTorrentEvent:
// the episodes of the declared season reach the library, a pack whose files
// belong to another season is rejected, and an episode the library already
// holds in a better copy is discarded while the others are kept.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

const packHash = "3333333333333333333333333333333333333333"

type packEnv struct {
	*behaviourEnv
	library string
	folder  string
}

// newPackEnv registers a season-1 pack of Series A whose torrent folder holds
// the given episode files.
func newPackEnv(t *testing.T, files ...string) *packEnv {
	t.Helper()
	e := newBehaviourEnv(t)
	library := filepath.Join(e.library, "Series A")
	if err := os.MkdirAll(library, 0o755); err != nil {
		t.Fatal(err)
	}
	e.cfg.Series = []SeriesConfig{{Name: "Series A", ArchivePath: library, Enabled: true}}
	show, season := "Series A", int64(1)
	release := &models.Release{Title: "Series.A.S01.1080p.WEB.H264-GROUP", Magnet: "magnet:?xt=urn:btih:" + packHash,
		Kind: "series", Series: &show, Season: &season, IsPack: true}
	if err := e.db.RegisterTorrentScored(release, 500); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(e.downloads, release.Title)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, name := range files {
		payload := []byte("\x1a\x45\xdf\xa3 episode payload " + strings.Repeat("x", 4096+i))
		if err := os.WriteFile(filepath.Join(folder, name), payload, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e.session.list = []models.TorrentView{{Hash: packHash, Name: release.Title, SavePath: e.downloads, State: "seeding",
		Progress: 100, TotalSize: 8192, TotalDone: 8192, HasMetadata: true}}
	return &packEnv{behaviourEnv: e, library: library, folder: folder}
}

func (p *packEnv) finish(t *testing.T) bool {
	t.Helper()
	event := models.TorrentEvent{Kind: "torrent_finished", Hash: packHash, Name: filepath.Base(p.folder), SavePath: p.downloads}
	processed, err := HandleTorrentEvent(p.cfg, p.session, p.db, p.moves, p.postSeed, p.retries, event,
		NewTmdbClientWithLanguage(nil, "it-IT"), FromConfig(p.cfg))
	if err != nil {
		t.Fatalf("pack completion: %v", err)
	}
	return processed
}

func libraryVideos(t *testing.T, dir string) []string {
	t.Helper()
	files, err := VideoFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// Every episode of the declared season is imported, the torrent keeps its
// folder to seed and nothing is moved.
func TestPackCompletionImportsTheSeason(t *testing.T) {
	p := newPackEnv(t, "Series.A.S01E01.1080p.WEB.H264-GROUP.mkv", "Series.A.S01E02.1080p.WEB.H264-GROUP.mkv")
	if !p.finish(t) {
		t.Fatal("the pack was not processed")
	}
	if got := libraryVideos(t, p.library); len(got) != 2 {
		t.Fatalf("library holds %v, want two episodes", got)
	}
	if videos := libraryVideos(t, p.folder); len(videos) != 2 {
		t.Fatalf("the torrent folder lost its files: %v", videos)
	}
	if p.session.moves != 0 {
		t.Fatal("a pack was moved out of its torrent folder")
	}
	status, _ := p.db.TorrentStatus(packHash)
	if status == nil || *status != "completed" {
		t.Fatalf("status = %v, want completed", status)
	}
}

// A pack whose files belong to another season is a wrong identity: rejected,
// nothing imported.
func TestPackCompletionRejectsAnotherSeason(t *testing.T) {
	p := newPackEnv(t, "Series.A.S02E01.1080p.WEB.H264-GROUP.mkv", "Series.A.S02E02.1080p.WEB.H264-GROUP.mkv")
	if p.finish(t) {
		t.Fatal("a pack of another season was processed")
	}
	if got := libraryVideos(t, p.library); len(got) != 0 {
		t.Fatalf("library holds %v, want nothing", got)
	}
	status, _ := p.db.TorrentStatus(packHash)
	if status == nil || *status != "error" {
		t.Fatalf("status = %v, want error", status)
	}
}

// With no library folder for the series the pack completes where it is.
func TestPackCompletionWithoutLibraryStaysInPlace(t *testing.T) {
	p := newPackEnv(t, "Series.A.S01E01.1080p.WEB.H264-GROUP.mkv")
	p.cfg.Series = nil
	p.cfg.ArchiveRoot = nil
	if !p.finish(t) {
		t.Fatal("the pack was not processed")
	}
	archived, _ := p.db.TorrentProcessed(packHash)
	if archived == nil || *archived != p.folder {
		t.Fatalf("recorded path = %v, want the torrent folder %q", archived, p.folder)
	}
}

// An episode the library already holds in a better copy is discarded, the
// others are imported; when every episode is worse the whole pack is rejected.
func TestPackCompletionKeepsTheBetterCopies(t *testing.T) {
	better := func(t *testing.T, p *packEnv, episode string) string {
		t.Helper()
		path := filepath.Join(p.library, "Series.A.S01"+episode+".2160p.BluRay.REMUX.HDR.DTS-HD.MA.5.1-GROUP.mkv")
		if err := os.WriteFile(path, []byte("\x1a\x45\xdf\xa3 better copy "+strings.Repeat("y", 9000)), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	p := newPackEnv(t, "Series.A.S01E01.1080p.WEB.H264-GROUP.mkv", "Series.A.S01E02.1080p.WEB.H264-GROUP.mkv")
	kept := better(t, p, "E01")
	if !p.finish(t) {
		t.Fatal("a pack with one new episode was not processed")
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("the better copy of E01 was replaced: %v", err)
	}
	videos := libraryVideos(t, p.library)
	if len(videos) != 2 {
		t.Fatalf("library holds %v, want the better E01 and the new E02", videos)
	}

	all := newPackEnv(t, "Series.A.S01E01.1080p.WEB.H264-GROUP.mkv")
	better(t, all, "E01")
	if all.finish(t) {
		t.Fatal("a pack with nothing better was processed")
	}
	status, _ := all.db.TorrentStatus(packHash)
	if status == nil || *status != "error" {
		t.Fatalf("status = %v, want error for a pack inferior to the library", status)
	}
}
