package gextto

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/amule/amuletest"
	"github.com/buzzqw/gextto/internal/models"
)

// crossnetEngine is a gx-torrent stand-in: a list, the files of each torrent
// and a record of the external merges asked.
type crossnetEngine struct {
	nullEngine
	views  []models.TorrentView
	files  map[string][]models.FileView
	merged []string
}

func (e *crossnetEngine) Name() string                   { return BackendGxTorrent }
func (e *crossnetEngine) List() []models.TorrentView     { return e.views }
func (e *crossnetEngine) Snapshot() []models.TorrentView { return e.views }
func (e *crossnetEngine) Files(hash string) ([]models.FileView, bool, error) {
	return e.files[strings.ToLower(hash)], true, nil
}
func (e *crossnetEngine) MergeExternal(hash string, fileIndex int, path string) (bool, error) {
	e.merged = append(e.merged, fmt.Sprintf("%s#%d@%s", hash, fileIndex, path))
	return true, nil
}

func resetCrossnet() {
	crossnet.mu.Lock()
	crossnet.loaded = false
	crossnet.pairs = map[string]*crossnetPair{}
	crossnet.files = map[string][]models.FileView{}
	crossnet.stuckSince = map[string]time.Time{}
	crossnet.lastDone = map[string]int64{}
	crossnet.searchedAt = map[string]time.Time{}
	crossnet.tempDir = ""
	crossnet.mu.Unlock()
}

func episodeRelease(t *testing.T, title string) models.Release {
	t.Helper()
	series, season, episode := "Show", int64(1), int64(2)
	return models.Release{Title: title, Kind: "series", Series: &series, Season: &season, Episode: &episode}
}

// TestCrossnetPairsAndMergesAndRemovesTheSpareCopy follows one episode on
// both networks: paired by identity and exact size, the torrent reads
// aMule's .part file once aMule has moved on, and when the torrent finishes
// first the eD2k copy is removed from aMule.
func TestCrossnetPairsAndMergesAndRemovesTheSpareCopy(t *testing.T) {
	resetCrossnet()
	t.Cleanup(resetCrossnet)
	core, cfg := ed2kTestCore(t, nil)
	cfg.DataDir = t.TempDir()
	temp := t.TempDir()
	core.SetTempDir(temp)
	const size = 100 << 20
	ed2kHash := "0123456789abcdef0123456789abcdef"
	if err := os.WriteFile(filepath.Join(temp, "007.part"), make([]byte, 16), 0o644); err != nil {
		t.Fatal(err)
	}
	core.SetDownloads([]amuletest.File{{Hash: ed2kHash, Name: "Show.S01E02.mkv", Size: size, Done: 50 << 20, Status: 0, PartMetID: 7}})

	db := newTestDB(t)
	torrentHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tr := episodeRelease(t, "Show.S01E02.1080p")
	tr.Magnet = "magnet:?xt=urn:btih:" + torrentHash
	if err := db.RegisterTorrent(&tr); err != nil {
		t.Fatal(err)
	}
	er := episodeRelease(t, "Show.S01E02.mkv")
	er.Ed2kLink = fmt.Sprintf("ed2k://|file|Show.S01E02.mkv|%d|%s|/", size, ed2kHash)
	if err := db.RegisterTorrent(&er); err != nil {
		t.Fatal(err)
	}
	engine := &crossnetEngine{
		views: []models.TorrentView{{Hash: torrentHash, Name: "Show.S01E02.1080p", Progress: 40, State: "downloading"}},
		files: map[string][]models.FileView{torrentHash: {{Path: "Show/sample.mkv", Size: 1 << 20}, {Path: "Show/Show.S01E02.mkv", Size: size}}},
	}
	now := time.Now()
	crossnetTick(cfg, db, engine, nil, now)
	if len(engine.merged) != 1 || !strings.Contains(engine.merged[0], torrentHash+"#1@"+filepath.Join(temp, "007.part")) {
		t.Fatalf("merges = %v, want one read of the .part file for file 1", engine.merged)
	}
	// Nothing new from aMule: no second read.
	crossnetTick(cfg, db, engine, nil, now.Add(10*time.Minute))
	if len(engine.merged) != 1 {
		t.Fatalf("merged again without new eD2k data: %v", engine.merged)
	}
	// The torrent finishes first: the eD2k copy goes.
	engine.views[0].Progress = 100
	crossnetTick(cfg, db, engine, nil, now.Add(20*time.Minute))
	if left := core.Downloads(); len(left) != 0 {
		t.Fatalf("the eD2k copy was not removed: %+v", left)
	}
	pending, _ := db.PendingEd2kHashes()
	for _, hash := range pending {
		if hash == ed2kHash {
			t.Fatal("the absorbed eD2k download is still pending")
		}
	}
}

// TestCrossnetKeyNeedsTheSameEpisode: different episodes, packs and
// releases without numbers never pair.
func TestCrossnetKeyNeedsTheSameEpisode(t *testing.T) {
	a := episodeRelease(t, "a")
	b := episodeRelease(t, "b")
	if crossnetKey(&a) == "" || crossnetKey(&a) != crossnetKey(&b) {
		t.Fatal("the same episode must share a key")
	}
	other := int64(3)
	b.Episode = &other
	if crossnetKey(&a) == crossnetKey(&b) {
		t.Fatal("two episodes share a key")
	}
	b.Episode = a.Episode
	b.IsPack = true
	if crossnetKey(&b) != "" {
		t.Fatal("a pack must not pair")
	}
	if crossnetKey(&models.Release{Kind: "series"}) != "" {
		t.Fatal("a release without numbers must not pair")
	}
}

// TestEd2kLineShareIsBounded: the stored share is bound to 10..90 before it
// becomes an int, so a value from the settings cannot truncate into another
// share on a 32-bit build.
func TestEd2kLineShareIsBounded(t *testing.T) {
	cfg := DefaultConfig()
	for _, c := range []struct {
		stored string
		want   int
	}{
		{"", defaultEd2kLineShare},
		{"0", 10},
		{"5", 10},
		{"100", 90},
		{"4294967356", 90}, // 2^32+60 truncates into 60 on a 32-bit build
	} {
		cfg.Settings = map[string]string{ed2kLineShareSetting: c.stored}
		if got := ed2kLineShare(&cfg); got != c.want {
			t.Errorf("share for %q = %d, want %d", c.stored, got, c.want)
		}
	}
}

// TestSplitLineLendsTheIdleShare: both busy meet at the floors, an idle
// engine lends its floor, an unlimited line is not split.
func TestSplitLineLendsTheIdleShare(t *testing.T) {
	if gx, ed := splitLine(0, 1<<20, 1<<20, 60); gx != 0 || ed != 0 {
		t.Fatalf("unlimited split = %d/%d", gx, ed)
	}
	// Both using their floors (1000 KiB/s line: 600/400).
	if gx, ed := splitLine(1000, 600<<10, 400<<10, 60); gx != 600 || ed != 400 {
		t.Fatalf("both busy = %d/%d, want 600/400", gx, ed)
	}
	// eD2k idle: the torrents may take the whole line, eD2k keeps its floor.
	if gx, ed := splitLine(1000, 900<<10, 0, 60); gx != 1000 || ed != 400 {
		t.Fatalf("eD2k idle = %d/%d, want 1000/400", gx, ed)
	}
	// Torrents using 100 of their 600: eD2k may grow into the rest.
	if gx, ed := splitLine(1000, 100<<10, 400<<10, 60); gx != 600 || ed != 875 {
		t.Fatalf("torrents light = %d/%d, want 600/875", gx, ed)
	}
	// A tiny line keeps a minimum for each.
	if gx, ed := splitLine(10, 0, 0, 60); gx < lineSplitMinKib || ed < lineSplitMinKib {
		t.Fatalf("tiny line = %d/%d", gx, ed)
	}
}

// TestCrossnetEd2kFinishedFirstCompletesTheTorrent: aMule finishes first; its
// file goes to the torrent and the eD2k completion is not imported again.
func TestCrossnetEd2kFinishedFirstCompletesTheTorrent(t *testing.T) {
	resetCrossnet()
	t.Cleanup(resetCrossnet)
	_, cfg := ed2kTestCore(t, nil)
	cfg.DataDir = t.TempDir()
	incoming := t.TempDir()
	const size = 5 << 20
	ed2kHash := "fedcba9876543210fedcba9876543210"
	if err := os.WriteFile(filepath.Join(incoming, "Show.S01E02.mkv"), make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	db := newTestDB(t)
	torrentHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	tr := episodeRelease(t, "Show.S01E02.1080p")
	tr.Magnet = "magnet:?xt=urn:btih:" + torrentHash
	er := episodeRelease(t, "Show.S01E02.mkv")
	er.Ed2kLink = fmt.Sprintf("ed2k://|file|Show.S01E02.mkv|%d|%s|/", size, ed2kHash)
	for _, r := range []*models.Release{&tr, &er} {
		if err := db.RegisterTorrent(r); err != nil {
			t.Fatal(err)
		}
	}
	engine := &crossnetEngine{
		views: []models.TorrentView{{Hash: torrentHash, Progress: 30, State: "downloading"}},
		files: map[string][]models.FileView{torrentHash: {{Path: "Show.S01E02.mkv", Size: size}}},
	}
	event := models.TorrentEvent{Kind: "torrent_finished", Hash: ed2kHash, Name: "Show.S01E02.mkv", SavePath: incoming}
	if !crossnetAbsorbs(cfg, db, engine, event) {
		t.Fatal("the eD2k completion was not taken by the torrent")
	}
	if len(engine.merged) != 1 || !strings.HasSuffix(engine.merged[0], filepath.Join(incoming, "Show.S01E02.mkv")) {
		t.Fatalf("merges = %v", engine.merged)
	}
	// Without a torrent of the same episode the completion is imported as usual.
	resetCrossnet()
	engine.views = nil
	if crossnetAbsorbs(cfg, db, engine, models.TorrentEvent{Kind: "torrent_finished", Hash: "11111111111111111111111111111111", Name: "x.mkv", SavePath: incoming}) {
		t.Fatal("an unpaired eD2k completion was absorbed")
	}
}
