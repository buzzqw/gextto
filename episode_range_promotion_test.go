package gextto

// episode_range_promotion_test.go pins what happens when a title says one
// episode but the torrent holds a run of them: an indexer that turns
// "S06E01-11" into "S06E01 11" loses the dash, so the release became its first
// episode while the folder held the whole run. The completion now reads the run
// from the files and files a pack; when the folder cannot be a run, the error
// says what is really in it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

const promotionHash = "4444444444444444444444444444444444444444"

type promotionEnv struct {
	*behaviourEnv
	library string
	folder  string
}

// newPromotionEnv registers a single-episode release of Series A whose torrent
// folder holds the given files.
func newPromotionEnv(t *testing.T, files ...string) *promotionEnv {
	t.Helper()
	e := newBehaviourEnv(t)
	library := filepath.Join(e.library, "Series A")
	if err := os.MkdirAll(library, 0o755); err != nil {
		t.Fatal(err)
	}
	e.cfg.Series = []SeriesConfig{{Name: "Series A", ArchivePath: library, Enabled: true}}
	show, season, episode := "Series A", int64(1), int64(1)
	title := "Series.A.S01E01.1080p.WEB.H264-GROUP"
	release := &models.Release{
		Title:        title,
		Magnet:       "magnet:?xt=urn:btih:" + promotionHash,
		Kind:         "series",
		Series:       &show,
		Season:       &season,
		Episode:      &episode,
		EpisodeRange: []int64{1},
	}
	if err := e.db.RegisterTorrentScored(release, 500); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(e.downloads, title)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	for index, name := range files {
		payload := []byte("\x1a\x45\xdf\xa3 episode payload " + strings.Repeat("x", 4096+index))
		if err := os.WriteFile(filepath.Join(folder, name), payload, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e.session.list = []models.TorrentView{{
		Hash: promotionHash, Name: title, SavePath: e.downloads, State: "seeding",
		Progress: 100, TotalSize: 8192, TotalDone: 8192, HasMetadata: true,
	}}
	return &promotionEnv{behaviourEnv: e, library: library, folder: folder}
}

func (p *promotionEnv) finish(t *testing.T) (bool, error) {
	t.Helper()
	event := models.TorrentEvent{
		Kind: "torrent_finished", Hash: promotionHash,
		Name: filepath.Base(p.folder), SavePath: p.downloads,
	}
	return HandleTorrentEvent(p.cfg, p.session, p.db, p.moves, p.postSeed, p.retries, event,
		NewTmdbClientWithLanguage(nil, "it-IT"), FromConfig(p.cfg))
}

// A folder holding a contiguous run that starts at the declared episode is the
// pack the title lost: every episode is filed, and when the pack carries two
// copies of the same episode the better one is kept.
func TestACompletedEpisodeRangeIsFiledAsAPack(t *testing.T) {
	p := newPromotionEnv(t,
		"Series.A.S01E01.1080p.WEB.AAC2.0.H264-GROUP.mkv",
		"Series.A.S01E01.1080p.WEB.DDP5.1.H264-GROUP.mkv",
		"Series.A.S01E02.1080p.WEB.DDP5.1.H264-GROUP.mkv",
		"Series.A.S01E03.1080p.WEB.DDP5.1.H264-GROUP.mkv",
	)
	if _, err := p.finish(t); err != nil {
		t.Fatalf("completion: %v", err)
	}
	videos := libraryVideos(t, p.library)
	if len(videos) != 3 {
		t.Fatalf("library holds %v, want three episodes", videos)
	}
	for _, file := range videos {
		if strings.Contains(filepath.Base(file), "S01E01") && !strings.Contains(filepath.Base(file), "DDP5.1") {
			t.Fatalf("the AAC copy of S01E01 was filed instead of the better one: %v", videos)
		}
	}
	status, _ := p.db.TorrentStatus(promotionHash)
	if status == nil || *status != "completed" {
		t.Fatalf("status = %v, want completed", status)
	}
}

// A folder whose episodes are not a run from the declared one is not a pack:
// the declared episode is filed when exactly one file is it, and the others
// stay in the download folder.
func TestAnEpisodeFolderThatIsNotARunFilesTheDeclaredEpisodeOnly(t *testing.T) {
	p := newPromotionEnv(t,
		"Series.A.S01E01.1080p.WEB.H264-GROUP.mkv",
		"Series.A.S01E03.1080p.WEB.H264-GROUP.mkv",
	)
	if _, err := p.finish(t); err != nil {
		t.Fatalf("completion: %v", err)
	}
	videos := libraryVideos(t, p.library)
	if len(videos) != 1 || !strings.Contains(filepath.Base(videos[0]), "S01E01") {
		t.Fatalf("library holds %v, want the declared S01E01 only", videos)
	}
}

// A folder with no file of the declared episode is refused, and the error says
// which episode was expected instead of counting "ambiguous" files.
func TestAnEpisodeFolderWithoutTheDeclaredEpisodeIsRefusedByName(t *testing.T) {
	p := newPromotionEnv(t,
		"Series.A.S01E02.1080p.WEB.H264-GROUP.mkv",
		"Series.A.S01E03.1080p.WEB.H264-GROUP.mkv",
	)
	_, err := p.finish(t)
	if err == nil {
		t.Fatal("a folder of unrelated episodes was processed")
	}
	if !strings.Contains(err.Error(), "none of them is S01E01") {
		t.Fatalf("error does not name the episode: %v", err)
	}
	if strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("the message still says ambiguous: %v", err)
	}
	if got := libraryVideos(t, p.library); len(got) != 0 {
		t.Fatalf("library holds %v, want nothing", got)
	}
}

// Two copies of the declared episode alone are not a run: the error says how
// many copies are there, which is what the operator has to decide about.
func TestTwoCopiesOfOneEpisodeAreNamedInTheError(t *testing.T) {
	p := newPromotionEnv(t,
		"Series.A.S01E01.1080p.WEB.AAC2.0.H264-GROUP.mkv",
		"Series.A.S01E01.1080p.WEB.DDP5.1.H264-GROUP.mkv",
	)
	_, err := p.finish(t)
	if err == nil {
		t.Fatal("a folder with two copies of one episode was processed")
	}
	if !strings.Contains(err.Error(), "2 of them are S01E01") {
		t.Fatalf("error does not say how many copies are there: %v", err)
	}
}
