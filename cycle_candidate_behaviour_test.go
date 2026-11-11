package gextto

// cycle_candidate_behaviour_test.go pins what the candidate pass decides for
// one feed release of a followed series: downloaded, held by a delay profile,
// refused by the blocklist, and refused because the library already holds a
// better copy. The feed is a local test server: no network.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

const candidateHash = "0123456789012345678901234567890123456789"

type candidateEnv struct {
	state    *AppState
	cfg      Config
	torrents *countingAddEngine
	archive  string
	release  *models.Release
}

func newCandidateEnv(t *testing.T) *candidateEnv {
	t.Helper()
	title := "Series.A.S01E01.1080p.WEB-DL.H264-GROUP"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<rss><channel><item><title>`+title+`</title><link>magnet:?xt=urn:btih:`+candidateHash+`</link></item></channel></rss>`)
	}))
	t.Cleanup(server.Close)
	state := newCycleState(t)
	torrents := &countingAddEngine{}
	state.setActiveEngine(torrents)
	cfg := *state.cfg
	archive := filepath.Join(t.TempDir(), "Series A")
	for _, dir := range []string{archive, cfg.DownloadDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg.Series = []SeriesConfig{{Name: "Series A", Enabled: true, ArchivePath: archive}}
	cfg.FeedURLs = []string{server.URL}
	cfg.Indexers = nil
	cfg.WebsearchEngines = nil
	cfg.DryRun = false
	cfg.Settings = map[string]string{}
	release := ParseRelease(title, "magnet:?xt=urn:btih:"+candidateHash, "test")
	if release == nil {
		t.Fatal("the test title does not parse")
	}
	return &candidateEnv{state: state, cfg: cfg, torrents: torrents, archive: archive, release: release}
}

func (e *candidateEnv) run(t *testing.T) *models.CycleStats {
	t.Helper()
	domain := "series"
	stats, err := RunCycleDomain(context.Background(), &e.cfg, e.state.engine, e.state.db, e.state.archive,
		e.state.comics, e.state.notifier, e.torrents, &domain)
	if err != nil {
		t.Fatalf("RunCycleDomain: %v", err)
	}
	return stats
}

func (e *candidateEnv) registered(t *testing.T) bool {
	t.Helper()
	meta, err := e.state.db.TorrentMeta(candidateHash)
	if err != nil {
		t.Fatal(err)
	}
	return meta != nil
}

// A new episode of a followed series is handed to the engine and recorded.
func TestCandidateIsDownloaded(t *testing.T) {
	e := newCandidateEnv(t)
	stats := e.run(t)
	if got := e.torrents.adds.Load(); got != 1 || stats.DownloadsStarted != 1 {
		t.Fatalf("adds %d, started %d; want the episode downloaded", got, stats.DownloadsStarted)
	}
	if !e.registered(t) {
		t.Fatal("the started download has no database row")
	}
	// The next cycle sees the same release again: it is not added twice.
	e.run(t)
	if got := e.torrents.adds.Load(); got != 1 {
		t.Fatalf("adds after a second cycle = %d, want 1", got)
	}
}

// A delay profile holds a release that neither fills a gap nor scores above
// the bypass: nothing reaches the engine yet.
func TestCandidateIsHeldByTheDelayProfile(t *testing.T) {
	e := newCandidateEnv(t)
	e.cfg.Settings[settingDelayTorrentMinutes] = "60"
	e.run(t)
	if got := e.torrents.adds.Load(); got != 0 {
		t.Fatalf("a delayed release was added %d time(s)", got)
	}
	if e.registered(t) {
		t.Fatal("a delayed release was recorded as a download")
	}
}

// A blocklisted release is refused.
func TestCandidateRefusedByTheBlocklist(t *testing.T) {
	e := newCandidateEnv(t)
	if err := e.state.db.Blocklist(e.release, "test"); err != nil {
		t.Fatal(err)
	}
	e.run(t)
	if got := e.torrents.adds.Load(); got != 0 {
		t.Fatalf("a blocklisted release was added %d time(s)", got)
	}
}

// A release no better than the copy in the library is refused.
func TestCandidateRefusedWhenTheLibraryHoldsBetter(t *testing.T) {
	e := newCandidateEnv(t)
	better := filepath.Join(e.archive, "Series.A.S01E01.2160p.BluRay.REMUX.HDR.DTS-HD.MA.5.1-GROUP.mkv")
	writeVideo(t, better, 9000)
	score := e.cfg.FileScore(better, "series", "Series A")
	if _, err := e.state.db.db.Exec("INSERT INTO series(name) VALUES ('Series A')"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.state.db.db.Exec(`INSERT INTO episodes(series_id,season,episode,title,downloaded_at,archive_path,quality_score)
		SELECT id,1,1,'Series.A.S01E01.2160p.BluRay.REMUX.HDR.DTS-HD.MA.5.1-GROUP','2026-01-01 00:00:00',?1,?2 FROM series WHERE name='Series A'`, better, score); err != nil {
		t.Fatal(err)
	}
	e.run(t)
	if got := e.torrents.adds.Load(); got != 0 {
		t.Fatalf("a worse release was added %d time(s) over the library's copy", got)
	}
}

// tryWithMirror runs one candidate through the pass directly: its `.torrent`
// link answers an error, and its mirror carries a magnet. infoHash is the hash
// the indexer declared for the link ("" for a link without one).
func (e *candidateEnv) tryWithMirror(t *testing.T, infoHash string) *models.CycleStats {
	t.Helper()
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusInternalServerError)
	}))
	t.Cleanup(dead.Close)
	link := dead.URL + "/download/1.torrent"
	release := *e.release
	mirror := release
	release.Magnet = ""
	release.TorrentURL = &link
	release.InfoHash = infoHash
	release.Source = "dead-indexer"
	mirror.Source = "mirror-indexer"
	release.Mirrors = []models.Release{mirror}
	stats := &models.CycleStats{}
	pass := &candidatePass{
		cfg: &e.cfg, engine: e.state.engine, db: e.state.db, archive: e.state.archive,
		notifier: e.state.notifier, torrents: e.torrents, stats: stats,
		blocklistedHashes: map[string]struct{}{}, blocklistedTitles: map[string]struct{}{},
		gapTargets: map[gapTarget]struct{}{}, quarantinedLinks: map[string]string{},
		readyPending:      map[string]struct{}{},
		liveDownloads:     &models.LiveDownloads{Hashes: map[string]struct{}{}, Episodes: map[models.LiveEpisodeKey]struct{}{}},
		stallAlternatives: map[models.LiveEpisodeKey]int64{},
		emptyArchiveIndex: &models.ArchiveQualityIndex{},
		archiveIndexCache: map[string]*models.ArchiveQualityIndex{},
		movieArchiveCache: map[string]*models.ArchiveQuality{},
		linkFailures:      map[string]map[string]struct{}{},
	}
	stop, err := pass.try(context.Background(), release)
	if err != nil || stop {
		t.Fatalf("try: stop %v, err %v", stop, err)
	}
	return stats
}

// A `.torrent` link that fails falls back to the same release from another
// source in the same cycle, whether the link declared its hash or not.
func TestCandidateFailedLinkFallsBackToTheMirror(t *testing.T) {
	for name, hash := range map[string]string{"declared hash": candidateHash, "no hash": ""} {
		t.Run(name, func(t *testing.T) {
			e := newCandidateEnv(t)
			e.tryWithMirror(t, hash)
			if got := e.torrents.adds.Load(); got != 1 {
				t.Fatalf("adds %d; want the mirror's magnet added once", got)
			}
			if !e.registered(t) {
				t.Fatal("the download started from the mirror has no database row")
			}
		})
	}
}
