package gextto

// multiseason_pack_test.go pins a pack that covers several seasons: the
// approval fills the gaps of every season, the import files each episode in its
// own season folder, and a folder-shaped pack whose files are named only by
// episode is read through the season folders.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

const multiSeasonHash = "5555555555555555555555555555555555555555"

// multiSeasonRelease is "Series A S01-S02", a complete pack of two seasons.
func multiSeasonRelease() *models.Release {
	show := "Series A"
	first, last, zero := int64(1), int64(2), int64(0)
	return &models.Release{
		Title:        "Series.A.S01-S02.Complete.1080p.WEB.H264-GROUP",
		Magnet:       "magnet:?xt=urn:btih:" + multiSeasonHash,
		Kind:         "series",
		Series:       &show,
		Season:       &first,
		SeasonEnd:    last,
		Episode:      &zero,
		IsPack:       true,
		EpisodeRange: []int64{0},
	}
}

// The approval runs season by season: every covered season gets its own
// placeholder, so no season of the pack is left looking empty.
func TestMultiSeasonPackIsApprovedForEverySeason(t *testing.T) {
	e := newBehaviourEnv(t)
	release := multiSeasonRelease()
	approved, _, err := e.db.CheckSeriesScored(release, 500, DefaultUpgradeMinScoreDiff, &models.ApprovalContext{})
	if err != nil {
		t.Fatalf("approval: %v", err)
	}
	if !approved {
		t.Fatal("the multi-season pack was not approved")
	}
	rows, err := e.db.db.Query(`SELECT e.season FROM episodes e JOIN series s ON s.id=e.series_id WHERE s.name='Series A' AND e.episode=0 ORDER BY e.season`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seasons := []int64{}
	for rows.Next() {
		var season int64
		if err := rows.Scan(&season); err != nil {
			t.Fatal(err)
		}
		seasons = append(seasons, season)
	}
	if len(seasons) != 2 || seasons[0] != 1 || seasons[1] != 2 {
		t.Fatalf("placeholder seasons = %v, want [1 2]", seasons)
	}
}

// Each episode of the pack is filed in the folder of its own season.
func TestMultiSeasonPackFilesEverySeasonInItsFolder(t *testing.T) {
	e := newBehaviourEnv(t)
	library := filepath.Join(e.library, "Series A")
	if err := os.MkdirAll(library, 0o755); err != nil {
		t.Fatal(err)
	}
	e.cfg.Series = []SeriesConfig{{Name: "Series A", ArchivePath: library, Enabled: true, SeasonSubfolders: true}}
	release := multiSeasonRelease()
	if approved, _, err := e.db.CheckSeriesScored(release, 500, DefaultUpgradeMinScoreDiff, &models.ApprovalContext{}); err != nil || !approved {
		t.Fatalf("approval: %v (approved=%v)", err, approved)
	}
	if err := e.db.RegisterTorrentScored(release, 500); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(e.downloads, release.Title)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	for index, name := range []string{
		"Series.A.S01E01.1080p.WEB.H264-GROUP.mkv",
		"Series.A.S01E02.1080p.WEB.H264-GROUP.mkv",
		"Series.A.S02E01.1080p.WEB.H264-GROUP.mkv",
	} {
		payload := []byte("\x1a\x45\xdf\xa3 episode payload " + strings.Repeat("x", 4096+index))
		if err := os.WriteFile(filepath.Join(folder, name), payload, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e.session.list = []models.TorrentView{{
		Hash: multiSeasonHash, Name: release.Title, SavePath: e.downloads, State: "seeding",
		Progress: 100, TotalSize: 8192, TotalDone: 8192, HasMetadata: true,
	}}
	event := models.TorrentEvent{Kind: "torrent_finished", Hash: multiSeasonHash, Name: release.Title, SavePath: e.downloads}
	processed, err := HandleTorrentEvent(e.cfg, e.session, e.db, e.moves, e.postSeed, e.retries, event,
		NewTmdbClientWithLanguage(nil, "it-IT"), FromConfig(e.cfg))
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if !processed {
		t.Fatal("the multi-season pack was not processed")
	}
	seasonOne := libraryVideos(t, filepath.Join(library, "Stagione 01"))
	seasonTwo := libraryVideos(t, filepath.Join(library, "Stagione 02"))
	if len(seasonOne) != 2 {
		t.Fatalf("Stagione 01 holds %v, want two episodes", seasonOne)
	}
	if len(seasonTwo) != 1 || !strings.Contains(filepath.Base(seasonTwo[0]), "S02E01") {
		t.Fatalf("Stagione 02 holds %v, want S02E01", seasonTwo)
	}
	// The archive rows of every season exist, and the recorded archive path is
	// the series folder: a pack spanning two seasons has no single season
	// folder to point at.
	rows, err := e.db.db.Query(`SELECT e.season FROM episodes e JOIN series s ON s.id=e.series_id WHERE s.name='Series A' AND e.episode=0 ORDER BY e.season`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seasons := []int64{}
	for rows.Next() {
		var season int64
		if err := rows.Scan(&season); err != nil {
			t.Fatal(err)
		}
		seasons = append(seasons, season)
	}
	if len(seasons) != 2 || seasons[0] != 1 || seasons[1] != 2 {
		t.Fatalf("completed pack seasons = %v, want [1 2]", seasons)
	}
	processedPath, err := e.db.TorrentProcessed(multiSeasonHash)
	if err != nil {
		t.Fatal(err)
	}
	if processedPath == nil || !SamePath(*processedPath, library) {
		t.Fatalf("recorded archive path = %v, want the series folder %q", processedPath, library)
	}
}

// A pack whose files carry only the episode number is read through its season
// folders: "Season 2/01.mkv" is the first episode of season 2.
func TestMultiSeasonPackReadsBareFilesFromSeasonFolders(t *testing.T) {
	source := t.TempDir()
	for _, relative := range []string{"Season 1/01.mkv", "Season 1/02.mkv", "Season 2/01.mkv"} {
		full := filepath.Join(source, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("\x1a\x45\xdf\xa3 payload"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	matching, err := MatchingPackFiles(source, multiSeasonRelease())
	if err != nil {
		t.Fatal(err)
	}
	if len(matching) != 3 {
		t.Fatalf("matched %d files, want three: %#v", len(matching), matching)
	}
	want := [][2]int64{{1, 1}, {1, 2}, {2, 1}}
	for index, file := range matching {
		if file.Season != want[index][0] || file.Episode != want[index][1] {
			t.Fatalf("file %d = S%02dE%02d, want S%02dE%02d", index, file.Season, file.Episode, want[index][0], want[index][1])
		}
	}
}
