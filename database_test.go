package gextto

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

func testRelease() models.Release {
	return models.Release{
		TorrentURL: nil,
		Title:      "Example.S01E01.1080p",
		Magnet:     "magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
		Source:     "rss",
		Quality: models.Quality{
			Resolution: "1080p",
		},
		Kind:         "series",
		Series:       stringPtr("Example"),
		Season:       int64Ptr(1),
		Episode:      int64Ptr(1),
		IsPack:       false,
		EpisodeRange: []int64{1},
		Year:         nil,
		SizeBytes:    0,
		Seeders:      -1,
		Peers:        -1,
	}
}

func stringPtr(value string) *string { return &value }

func int64Ptr(value int64) *int64 { return &value }

func newTestDB(t *testing.T) *Database {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gextto.db")
	db, err := OpenDatabase(path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.db.Close() })
	return db
}

func TestStallWatchPersistence(t *testing.T) {
	db := newTestDB(t)
	stalledAt := time.Date(2026, time.October, 5, 10, 0, 0, 0, time.UTC)
	want := StallWatch{
		lastProgressAt:    stalledAt.Add(-time.Hour),
		lastDone:          12345,
		stalledSince:      &stalledAt,
		nextRetryAt:       stalledAt.Add(time.Hour),
		nextRetryNoticeAt: stalledAt.Add(3 * time.Hour),
		retryNoticeStep:   2,
	}
	if err := db.SaveStallWatch("ABC123", want); err != nil {
		t.Fatalf("save stall watch: %v", err)
	}
	watches, err := db.LoadStallWatches()
	if err != nil {
		t.Fatalf("load stall watches: %v", err)
	}
	got, ok := watches["abc123"]
	if !ok {
		t.Fatal("saved stall watch was not restored")
	}
	if got.lastDone != want.lastDone || got.retryNoticeStep != want.retryNoticeStep || got.stalledSince == nil || !got.stalledSince.Equal(stalledAt) || !got.nextRetryAt.Equal(want.nextRetryAt) || !got.nextRetryNoticeAt.Equal(want.nextRetryNoticeAt) {
		t.Fatalf("restored stall watch = %#v, want %#v", got, want)
	}
	if err := db.DeleteStallWatch("abc123"); err != nil {
		t.Fatalf("delete stall watch: %v", err)
	}
	watches, err = db.LoadStallWatches()
	if err != nil || len(watches) != 0 {
		t.Fatalf("watches after delete = %#v, %v", watches, err)
	}
}

func TestMarkTorrentCompletedUnarchivedIsIdempotent(t *testing.T) {
	db := newTestDB(t)
	first, err := db.MarkTorrentCompletedUnarchived("ABC123", "Manual.Movie.mkv")
	if err != nil || !first {
		t.Fatalf("first completion = (%v, %v), want (true, nil)", first, err)
	}
	second, err := db.MarkTorrentCompletedUnarchived("abc123", "Manual.Movie.mkv")
	if err != nil || second {
		t.Fatalf("duplicate completion = (%v, %v), want (false, nil)", second, err)
	}
	var name, status string
	if err := db.db.QueryRow("SELECT name,status FROM torrent_meta WHERE hash='abc123'").Scan(&name, &status); err != nil {
		t.Fatalf("read completed torrent: %v", err)
	}
	if name != "Manual.Movie.mkv" || status != "completed" {
		t.Fatalf("stored torrent = (%q, %q), want name and completed status", name, status)
	}
}

func assertTrue(t *testing.T, value bool, message string) {
	t.Helper()
	if !value {
		t.Fatalf("%s", message)
	}
}

func assertEqual(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func mustHash(t *testing.T, magnet string) string {
	t.Helper()
	hash, ok := utils.MagnetHash(magnet)
	if !ok {
		t.Fatalf("invalid magnet: %s", magnet)
	}
	return hash
}

func TestReconcilesTorrentsMissingFromSession(t *testing.T) {
	db := newTestDB(t)
	release := testRelease()
	digest := mustHash(t, release.Magnet)
	if err := db.RegisterTorrent(&release); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO series(id,name) VALUES (1,'Example')"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,quality_score,magnet_hash,magnet_link) VALUES (1,1,1,'Example.S01E01.1080p',100,?1,?2)", digest, release.Magnet); err != nil {
		t.Fatal(err)
	}

	// Sessione vuota: il torrent è orfano → marcato error e placeholder rimosso.
	removed, err := db.ReconcileMissingTorrents(map[string]struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, removed, 1)
	status, err := db.TorrentStatus(digest)
	if err != nil {
		t.Fatal(err)
	}
	if status == nil || *status != "error" {
		t.Fatalf("expected status error, got %v", status)
	}
	var leftover int64
	if err := db.db.QueryRow("SELECT COUNT(*) FROM episodes WHERE magnet_hash=?1", digest).Scan(&leftover); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, leftover, int64(0))

	// Con il torrent vivo nella sessione non viene toccato.
	if err := db.RegisterTorrent(&release); err != nil {
		t.Fatal(err)
	}
	live := map[string]struct{}{digest: {}}
	removed, err = db.ReconcileMissingTorrents(live)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, removed, 0)
	status, err = db.TorrentStatus(digest)
	if err != nil {
		t.Fatal(err)
	}
	if status == nil || *status != "queued" {
		t.Fatalf("expected status queued, got %v", status)
	}
}

func TestTagsManualTorrentsCreatingMissingRows(t *testing.T) {
	db := newTestDB(t)
	hash := "f9d1b5f8b88ed1132723a6ce5a6d51e67267ab95"

	// Torrent aggiunto a mano: nessuna riga in torrent_meta.
	if err := db.SetTorrentTag(hash, "Manuale"); err != nil {
		t.Fatal(err)
	}
	tags, err := db.TorrentTags()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(tags), 1)
	assertEqual(t, tags[0][0], hash)
	assertEqual(t, tags[0][1], "Manuale")

	// Un secondo assegnamento aggiorna la riga esistente senza duplicarla.
	if err := db.SetTorrentTag(upper(hash), "Film"); err != nil {
		t.Fatal(err)
	}
	tags, err = db.TorrentTags()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(tags), 1)
	assertEqual(t, tags[0][1], "Film")

	// Una riga legacy con hash in maiuscolo viene aggiornata, non duplicata.
	legacy := "0123456789abcdef0123456789abcdef01234567"
	if _, err := db.db.Exec("INSERT INTO torrent_meta(hash,tag,status,created_at,updated_at) VALUES (?1,'','queued',datetime('now'),datetime('now'))", upper(legacy)); err != nil {
		t.Fatal(err)
	}
	if err := db.SetTorrentTag(legacy, "Serie TV"); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.db.QueryRow("SELECT COUNT(*) FROM torrent_meta WHERE lower(hash)=?1", legacy).Scan(&count); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, count, int64(1))
}

func upper(value string) string {
	out := []rune(value)
	for index, r := range out {
		if r >= 'a' && r <= 'z' {
			out[index] = r - 32
		}
	}
	return string(out)
}

func TestPurgeSeriesRemovesEveryTrace(t *testing.T) {
	db := newTestDB(t)
	archivePath := filepath.Join(t.TempDir(), "e1.mkv")
	if err := os.WriteFile(archivePath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO series(name) VALUES ('Gone')"); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := db.db.QueryRow("SELECT id FROM series WHERE name='Gone'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,archive_path) VALUES (?1,1,1,'Gone.S01E01',?2)", id, archivePath); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO pending_downloads(series_id,season,episode,status) VALUES (?1,1,1,'pending')", id); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"INSERT INTO series_metadata(series_name,season,episode_count,updated_at) VALUES ('Gone',1,1,datetime('now'))",
		"INSERT INTO episode_metadata(series_name,season,episode,air_date,updated_at) VALUES ('Gone',1,1,'',datetime('now'))",
		"INSERT INTO ignored_episodes(series_name,season,episode) VALUES ('Gone',1,2)",
		"INSERT INTO gap_search_log(series_name,season,episode,last_searched_at) VALUES ('Gone',1,1,datetime('now'))",
		"INSERT INTO series_status(series_name,status,last_air_date,updated_at) VALUES ('Gone','','',datetime('now'))",
	} {
		if _, err := db.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}

	targets, err := db.MediaInfoBackfillTargets(10)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(targets), 1)

	removed, err := db.PurgeSeries("Gone")
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, removed > 0, "la serie rimossa deve restituire righe eliminate")

	targets, err = db.MediaInfoBackfillTargets(10)
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, len(targets) == 0, "i target del backfill devono sparire")
	for _, table := range []string{"series", "episodes", "pending_downloads", "series_metadata", "episode_metadata", "ignored_episodes", "gap_search_log", "series_status"} {
		var count int64
		if err := db.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		assertEqual(t, count, int64(0))
	}
}

func TestTorrentAuxBulkMatchesPerHashLookups(t *testing.T) {
	db := newTestDB(t)
	now := nowSQLite()
	for _, row := range []struct {
		hash, processed, source, reason string
	}{
		{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "/nas/a.mkv", "rss", "series"},
		{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "", "indexer", "movie"},
	} {
		if _, err := db.db.Exec("INSERT INTO torrent_meta(hash,processed_path,source,reason,updated_at) VALUES (?1,?2,?3,?4,?5)", row.hash, row.processed, row.source, row.reason, now); err != nil {
			t.Fatal(err)
		}
	}
	aux, err := db.TorrentAuxBulk([]string{
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"cccccccccccccccccccccccccccccccccccccccc",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, aux["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"], [3]string{"/nas/a.mkv", "rss", "series"})
	assertEqual(t, aux["bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"], [3]string{"", "indexer", "movie"})
	if _, ok := aux["cccccccccccccccccccccccccccccccccccccccc"]; ok {
		t.Fatal("unexpected aux entry")
	}
	single, err := db.TorrentAux("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, single, aux["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"])
}

func TestSeriesSeasonCountsBulkMatchesSingleLookup(t *testing.T) {
	db := newTestDB(t)
	now := nowSQLite()
	for _, row := range []struct {
		name   string
		season int64
		count  int64
	}{{"A", 1, 10}, {"A", 2, 8}, {"B", 1, 3}} {
		if _, err := db.db.Exec("INSERT INTO series_metadata(series_name,season,episode_count,updated_at) VALUES (?1,?2,?3,?4)", row.name, row.season, row.count, now); err != nil {
			t.Fatal(err)
		}
	}
	bulk, err := db.SeriesSeasonCountsBulk()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, bulk["A"], [][2]int64{{1, 10}, {2, 8}})
	assertEqual(t, bulk["B"], [][2]int64{{1, 3}})
	single, err := db.SeriesSeasonCounts("A")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, single, bulk["A"])
	empty, err := db.SeriesSeasonCounts("C")
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, len(empty) == 0, "serie inesistente deve essere vuota")
	if _, ok := bulk["C"]; ok {
		t.Fatal("unexpected bulk entry")
	}
}

func TestReconcilesPackPlaceholdersWhenTorrentSeasonDiffers(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO series(id,name) VALUES (1,'Example')"); err != nil {
		t.Fatal(err)
	}
	advertised := testRelease()
	advertised.Title = "Example.S06E01-06.1080p"
	advertised.Season = int64Ptr(6)
	advertised.IsPack = true
	advertised.EpisodeRange = []int64{1, 2, 3, 4, 5, 6}
	hash := mustHash(t, advertised.Magnet)
	if err := db.RegisterTorrent(&advertised); err != nil {
		t.Fatal(err)
	}
	for episode := int64(1); episode <= 6; episode++ {
		var episodeHash any
		if episode == 1 {
			episodeHash = hash
		}
		if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,quality_score,magnet_hash,magnet_link) VALUES (1,6,?1,?2,100,?3,?4)", episode, advertised.Title, episodeHash, advertised.Magnet); err != nil {
			t.Fatal(err)
		}
	}
	actual := advertised
	actual.Title = "Example.S05E01-06.1080p"
	actual.Season = int64Ptr(5)
	cfg := DefaultConfig()
	if err := db.ReconcilePackRelease(hash, &actual, &cfg); err != nil {
		t.Fatal(err)
	}
	meta, err := db.TorrentMeta(hash)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, *meta.Release.Season, int64(5))
	var oldCount, newCount int64
	if err := db.db.QueryRow("SELECT COUNT(*) FROM episodes WHERE season=6").Scan(&oldCount); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRow("SELECT COUNT(*) FROM episodes WHERE season=5").Scan(&newCount); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, oldCount, int64(0))
	assertEqual(t, newCount, int64(6))
}

func TestFailedReleaseClearsPlaceholdersToAllowFallback(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO series(id,name) VALUES (1,'Example')"); err != nil {
		t.Fatal(err)
	}
	pack := models.Release{
		Title:        "Example.S01E01-08.2160p",
		Magnet:       "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Source:       "rss",
		Quality:      models.Quality{Resolution: "2160p"},
		Kind:         "series",
		Series:       stringPtr("Example"),
		Season:       int64Ptr(1),
		Episode:      int64Ptr(1),
		IsPack:       true,
		EpisodeRange: []int64{1, 2, 3, 4, 5, 6, 7, 8},
		Seeders:      -1,
		Peers:        -1,
	}
	if err := db.RegisterTorrent(&pack); err != nil {
		t.Fatal(err)
	}
	digest := mustHash(t, pack.Magnet)
	for episode := int64(1); episode <= 8; episode++ {
		var episodeHash any
		if episode == 1 {
			episodeHash = digest
		}
		if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,quality_score,magnet_hash,magnet_link) VALUES (1,1,?1,?2,1880,?3,?4)", episode, pack.Title, episodeHash, pack.Magnet); err != nil {
			t.Fatal(err)
		}
	}

	if err := db.MarkTorrentError(digest, "stalled download"); err != nil {
		t.Fatal(err)
	}
	var leftover int64
	if err := db.db.QueryRow("SELECT COUNT(*) FROM episodes WHERE series_id=1").Scan(&leftover); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, leftover, int64(0))

	fallback := models.Release{
		Title:        "Example.S01.1080p",
		Magnet:       "magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Source:       "rss",
		Quality:      models.Quality{Resolution: "1080p"},
		Kind:         "series",
		Series:       stringPtr("Example"),
		Season:       int64Ptr(1),
		Episode:      int64Ptr(0),
		IsPack:       true,
		EpisodeRange: []int64{0},
		Seeders:      -1,
		Peers:        -1,
	}
	fallbackHash := mustHash(t, fallback.Magnet)
	score := fallback.Quality.Score()
	approved, reason, err := db.checkSeriesPack(&fallback, fallbackHash, score, 200, &models.ApprovalContext{}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, approved, "il 1080p deve essere approvato: "+reason)
}

func TestArchiveIndexBlocksReleaseAlreadyPresentOnDisk(t *testing.T) {
	db := newTestDB(t)
	release := testRelease()
	score := release.Quality.Score()

	context := &models.ApprovalContext{
		Archive: &models.ArchiveQualityIndex{Best: map[[2]int64]models.ArchiveQuality{
			{1, 1}: {Quality: models.Quality{Resolution: "2160p"}, Score: 2000},
		}},
		Live: &models.LiveDownloads{},
	}
	approved, reason, err := db.CheckSeriesScored(&release, score, 200, context)
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, !approved, "atteso duplicate dal disco, ottenuto "+reason)

	approved, _, err = db.CheckSeriesScored(&release, score, 200, &models.ApprovalContext{})
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, approved, "senza indice il candidato deve essere approvato")
}

func TestLiveSessionBlocksEpisodeAlreadyDownloading(t *testing.T) {
	db := newTestDB(t)
	release := testRelease()
	score := release.Quality.Score()

	live := &models.LiveDownloads{Episodes: map[models.LiveEpisodeKey]struct{}{
		{Series: NormalizeSeriesName("Example"), Season: 1, Episode: 1}: {},
	}}
	context := &models.ApprovalContext{Archive: &models.ArchiveQualityIndex{}, Live: live}
	approved, reason, err := db.CheckSeriesScored(&release, score, 200, context)
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, !approved, "atteso active_episode, ottenuto "+reason)
	assertEqual(t, reason, "active_episode")

	liveHashes := &models.LiveDownloads{Hashes: map[string]struct{}{
		mustHash(t, release.Magnet): {},
	}}
	context = &models.ApprovalContext{Archive: &models.ArchiveQualityIndex{}, Live: liveHashes}
	approved, reason, err = db.CheckSeriesScored(&release, score, 200, context)
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, !approved, "atteso active_episode per hash, ottenuto "+reason)
}

func TestExcludesIgnoredSeasonsFromSeriesEpisodes(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO series(id,name,ignored_seasons) VALUES (1,'Show','[2]')"); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ season, count int64 }{{1, 2}, {2, 3}} {
		if _, err := db.db.Exec("INSERT INTO series_metadata(series_name,season,episode_count,updated_at) VALUES ('Show',?1,?2,datetime('now'))", row.season, row.count); err != nil {
			t.Fatal(err)
		}
	}
	items, err := db.EpisodesForSeries("Show", nil)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(items), 2)
	for _, item := range items {
		assertEqual(t, item.Season, int64(1))
	}
}

func TestExcludesConfigSeasonsPassedAsExtraIgnored(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO series(id,name) VALUES (1,'Show')"); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ season, count int64 }{{1, 2}, {2, 3}} {
		if _, err := db.db.Exec("INSERT INTO series_metadata(series_name,season,episode_count,updated_at) VALUES ('Show',?1,?2,datetime('now'))", row.season, row.count); err != nil {
			t.Fatal(err)
		}
	}
	items, err := db.EpisodesForSeries("Show", []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(items), 3)
	for _, item := range items {
		assertEqual(t, item.Season, int64(2))
	}
}

func TestTorrentNoRenameFlagRoundTripsCaseInsensitively(t *testing.T) {
	db := newTestDB(t)
	value, err := db.TorrentNoRename("ABCdef")
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, !value, "default false")
	if err := db.SetTorrentNoRename("ABCdef", true); err != nil {
		t.Fatal(err)
	}
	value, err = db.TorrentNoRename("abcdef")
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, value, "deve essere true")
	if err := db.SetTorrentNoRename("abcdef", false); err != nil {
		t.Fatal(err)
	}
	value, err = db.TorrentNoRename("ABCDEF")
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, !value, "deve essere false")
}

func TestTorrentMetadataRoundTripsAndCompletionUpdatesNormalizedState(t *testing.T) {
	db := newTestDB(t)
	release := testRelease()
	if _, _, err := db.CheckSeries(&release); err != nil {
		t.Fatal(err)
	}
	later := release
	later.Episode = int64Ptr(3)
	later.EpisodeRange = []int64{3}
	later.Title = "Example.S01E03.1080p"
	later.Magnet = "magnet:?xt=urn:btih:abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	if _, _, err := db.CheckSeries(&later); err != nil {
		t.Fatal(err)
	}
	gaps, err := db.ArchiveGaps()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, gaps, []SeriesGap{{Series: "Example", Season: 1, Episode: 2}})
	if err := db.SaveSeriesMetadata("Example", [][2]int64{{1, 5}}); err != nil {
		t.Fatal(err)
	}
	gaps, err = db.ArchiveGaps()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, gaps, []SeriesGap{
		{Series: "Example", Season: 1, Episode: 2},
		{Series: "Example", Season: 1, Episode: 4},
		{Series: "Example", Season: 1, Episode: 5},
	})
	searched, err := db.GapRecentlySearched("Example", 1, 2, 23)
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, !searched, "non ancora cercato")
	if err := db.MarkGapSearched("Example", 1, 2); err != nil {
		t.Fatal(err)
	}
	searched, err = db.GapRecentlySearched("Example", 1, 2, 23)
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, searched, "appena cercato")
	if err := db.RegisterTorrent(&release); err != nil {
		t.Fatal(err)
	}
	hash := mustHash(t, release.Magnet)
	meta, err := db.TorrentMeta(hash)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, meta.Release.Title, release.Title)
	if err := db.MarkTorrentCompleted(hash, "/nas/example", 42); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.db.QueryRow("SELECT status FROM torrent_meta WHERE hash=?1", hash).Scan(&status); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, status, "completed")
	var size int64
	if err := db.db.QueryRow("SELECT size_bytes FROM episodes WHERE magnet_hash=?1", hash).Scan(&size); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, size, int64(42))
}

func TestManualMissingSearchUsesArchivePresenceNotClientState(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO series(id,name) VALUES (1,'Example')"); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSeriesMetadata("Example", [][2]int64{{1, 3}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,archive_path) VALUES (1,1,1,'Example S01E01','')"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,archive_path) VALUES (1,1,2,'Example S01E02','/nas/Example S01E02.mkv')"); err != nil {
		t.Fatal(err)
	}
	missing, err := db.UnarchivedEpisodesForSeries("Example", nil)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, missing, [][2]int64{{1, 1}, {1, 3}})
}

func TestMissingArchivedEpisodeIsEligibleForRecovery(t *testing.T) {
	db := newTestDB(t)
	release := testRelease()
	if _, _, err := db.CheckSeries(&release); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "deleted-outside-gextto.mkv")
	if _, err := db.db.Exec("UPDATE episodes SET downloaded_at=?1,archive_path=?2,size_bytes=123 WHERE series_id=(SELECT id FROM series WHERE name=?3) AND season=1 AND episode=1", nowSQLite(), missing, *release.Series); err != nil {
		t.Fatal(err)
	}
	approved, reason, err := db.CheckSeries(&release)
	if err != nil {
		t.Fatal(err)
	}
	if !approved || reason != "upgrade" {
		t.Fatalf("missing archived file recovery = approved:%v reason:%q", approved, reason)
	}
	var archivePath string
	if err := db.db.QueryRow("SELECT COALESCE(archive_path,'') FROM episodes WHERE series_id=(SELECT id FROM series WHERE name=?1) AND season=1 AND episode=1", *release.Series).Scan(&archivePath); err != nil {
		t.Fatal(err)
	}
	if archivePath != "" {
		t.Fatalf("stale archive path was retained: %q", archivePath)
	}
}

func TestArchiveScanKeepsBetterExistingFilePath(t *testing.T) {
	db := newTestDB(t)
	dir := t.TempDir()
	better := filepath.Join(dir, "Show.S01E01.1080p.WEB-DL.mkv")
	inferior := filepath.Join(dir, "Show.S01E01.720p.HDTV.mkv")
	if err := os.WriteFile(better, []byte("better"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inferior, []byte("inferior"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := db.SyncArchiveFileScored("Show", 1, 1, filepath.Base(better), better, 6, 1200); err != nil {
		t.Fatal(err)
	}
	if err := db.SyncArchiveFileScored("Show", 1, 1, filepath.Base(inferior), inferior, 8, 500); err != nil {
		t.Fatal(err)
	}
	var path string
	if err := db.db.QueryRow("SELECT archive_path FROM episodes WHERE series_id=(SELECT id FROM series WHERE name='Show') AND season=1 AND episode=1").Scan(&path); err != nil {
		t.Fatal(err)
	}
	if path != better {
		t.Fatalf("inferior scan replaced better path: %q", path)
	}
	if err := os.Remove(better); err != nil {
		t.Fatal(err)
	}
	if err := db.SyncArchiveFileScored("Show", 1, 1, filepath.Base(inferior), inferior, 8, 500); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRow("SELECT archive_path FROM episodes WHERE series_id=(SELECT id FROM series WHERE name='Show') AND season=1 AND episode=1").Scan(&path); err != nil {
		t.Fatal(err)
	}
	if path != inferior {
		t.Fatalf("missing better path was not repaired: %q", path)
	}
}

func TestSeasonPackRegistersAllEpisodesAndRollsBackAsOneRelease(t *testing.T) {
	db := newTestDB(t)
	pack := testRelease()
	pack.Title = "Example.S01.Pack"
	pack.Magnet = "magnet:?xt=urn:btih:abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	pack.Episode = int64Ptr(1)
	pack.IsPack = true
	pack.EpisodeRange = []int64{1, 2, 3}
	approved, reason, err := db.CheckSeries(&pack)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, approved, true)
	assertEqual(t, reason, "approved")
	gaps, err := db.ArchiveGaps()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(gaps), 0)
	if err := db.RollbackRelease(&pack); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.db.QueryRow("SELECT COUNT(*) FROM episodes").Scan(&count); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, count, int64(0))
}

func TestCompletePackSuppressesMetadataGapsOnlyWhileQueuedOrCompleted(t *testing.T) {
	db := newTestDB(t)
	pack := testRelease()
	pack.Title = "Example.S01.COMPLETE.1080p"
	pack.Magnet = "magnet:?xt=urn:btih:abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	pack.Episode = int64Ptr(0)
	pack.IsPack = true
	pack.EpisodeRange = []int64{0}
	approved, reason, err := db.CheckSeries(&pack)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, approved, true)
	assertEqual(t, reason, "approved")
	if err := db.SaveSeriesMetadata("Example", [][2]int64{{1, 3}}); err != nil {
		t.Fatal(err)
	}
	gaps, err := db.ArchiveGaps()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(gaps), 3)
	if err := db.RegisterTorrent(&pack); err != nil {
		t.Fatal(err)
	}
	gaps, err = db.ArchiveGaps()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(gaps), 0)
	hash := mustHash(t, pack.Magnet)
	if err := db.MarkTorrentError(hash, "failed"); err != nil {
		t.Fatal(err)
	}
	gaps, err = db.ArchiveGaps()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(gaps), 3)
}

func TestMigratesLegacyTorrentMetadataBeforeCreatingStatusIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec("CREATE TABLE torrent_meta (hash TEXT PRIMARY KEY, tag TEXT DEFAULT '', source TEXT DEFAULT '', updated_at TEXT NOT NULL);"); err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	legacy.Close()

	db, err := OpenDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.db.Close()
	var hasStatus, hasIndex bool
	if err := db.db.QueryRow("SELECT EXISTS(SELECT 1 FROM pragma_table_info('torrent_meta') WHERE name='status')").Scan(&hasStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRow("SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='index' AND name='idx_torrent_meta_status')").Scan(&hasIndex); err != nil {
		t.Fatal(err)
	}
	assertTrue(t, hasStatus, "status column must be added")
	assertTrue(t, hasIndex, "status index must exist")
}

func TestRestoresSoftDeletedMovieByMagnetHash(t *testing.T) {
	db := newTestDB(t)
	release := models.Release{
		Title:   "Example Movie",
		Magnet:  "magnet:?xt=urn:btih:abcdefabcdefabcdefabcdefabcdefabcdefabcd",
		Source:  "rss",
		Quality: models.Quality{Resolution: "1080p"},
		Kind:    "movie",
		Year:    int64Ptr(2024),
		Seeders: -1,
		Peers:   -1,
	}
	approved, reason, err := db.CheckMovie(&release)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, approved, true)
	assertEqual(t, reason, "approved")
	if _, err := db.db.Exec("UPDATE movies SET removed_at=datetime('now') WHERE magnet_hash=?1", mustHash(t, release.Magnet)); err != nil {
		t.Fatal(err)
	}
	approved, reason, err = db.CheckMovie(&release)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, approved, true)
	assertEqual(t, reason, "restored")
	var removedIsNull bool
	if err := db.db.QueryRow("SELECT removed_at IS NULL FROM movies WHERE magnet_hash=?1", mustHash(t, release.Magnet)).Scan(&removedIsNull); err != nil {
		t.Fatal(err)
	}
	assertTrue(t, removedIsNull, "removed_at must be cleared")
}

func TestMovieRemuxUpgradeIsAllowedWithSmallScoreDelta(t *testing.T) {
	db := newTestDB(t)
	old := models.Release{
		Title:   "Example Movie",
		Magnet:  "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Source:  "rss",
		Quality: models.Quality{Resolution: "2160p", Source: "webdl"},
		Kind:    "movie",
		Year:    int64Ptr(2024),
		Seeders: -1,
		Peers:   -1,
	}
	approved, reason, err := db.CheckMovie(&old)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, approved, true)
	assertEqual(t, reason, "approved")
	if err := db.RegisterTorrent(&old); err != nil {
		t.Fatal(err)
	}
	upgrade := old
	upgrade.Magnet = "magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	upgrade.Quality.Source = "remux"
	approved, reason, err = db.CheckMovieScored(&upgrade, upgrade.Quality.Score(), 200)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, approved, true)
	assertEqual(t, reason, "upgrade")
}

func TestRollbackReleaseRestoresMovieUpgrade(t *testing.T) {
	db := newTestDB(t)
	old := models.Release{
		Title:   "Example Movie",
		Magnet:  "magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
		Source:  "rss",
		Quality: models.Quality{Resolution: "1080p", Source: "webdl"},
		Kind:    "movie",
		Year:    int64Ptr(2024),
		Seeders: -1,
		Peers:   -1,
	}
	if approved, _, err := db.CheckMovie(&old); err != nil || !approved {
		t.Fatalf("initial movie approval: approved=%v err=%v", approved, err)
	}
	if err := db.RegisterTorrent(&old); err != nil {
		t.Fatal(err)
	}
	upgrade := old
	upgrade.Magnet = "magnet:?xt=urn:btih:fedcba9876543210fedcba9876543210fedcba98"
	upgrade.Quality.Source = "remux"
	if approved, reason, err := db.CheckMovieScored(&upgrade, upgrade.Quality.Score(), 200); err != nil || !approved || reason != "upgrade" {
		t.Fatalf("upgrade approval: approved=%v reason=%q err=%v", approved, reason, err)
	}
	if err := db.RollbackRelease(&upgrade); err != nil {
		t.Fatal(err)
	}
	var hash, title string
	var count int
	if err := db.db.QueryRow("SELECT magnet_hash,title FROM movies WHERE name='Example Movie' AND year=2024 AND removed_at IS NULL").Scan(&hash, &title); err != nil {
		t.Fatal(err)
	}
	if hash != "0123456789012345678901234567890123456789" || title != old.Title {
		t.Fatalf("old movie was not restored: hash=%q title=%q", hash, title)
	}
	if err := db.db.QueryRow("SELECT COUNT(*) FROM upgrade_backup").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("upgrade backup remained after rollback: %d", count)
	}
}

func TestUpgradeBackupAccumulatesSeasonPackRows(t *testing.T) {
	db := newTestDB(t)
	tx, err := db.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for _, backup := range []upgradeBackup{
		{Kind: "series", RowID: 1, Title: "Show S01E01"},
		{Kind: "series", RowID: 2, Title: "Show S01E02"},
	} {
		if err := db.saveUpgradeBackup(tx, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", backup); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := db.db.QueryRow("SELECT payload_json FROM upgrade_backup WHERE new_hash=?1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	backups, err := decodeUpgradeBackups(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 2 || backups[0].RowID != 1 || backups[1].RowID != 2 {
		t.Fatalf("season pack backups = %+v", backups)
	}
}

func TestRecentDownloadsMergesSeriesAndMovies(t *testing.T) {
	db := newTestDB(t)
	release := testRelease()
	if _, _, err := db.CheckSeries(&release); err != nil {
		t.Fatal(err)
	}
	hash := mustHash(t, release.Magnet)
	if err := db.RegisterTorrent(&release); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkTorrentCompleted(hash, "/nas/example", 42); err != nil {
		t.Fatal(err)
	}
	movie := models.Release{
		Title:   "Example Movie",
		Magnet:  "magnet:?xt=urn:btih:1234567890abcdef1234567890abcdef12345678",
		Source:  "rss",
		Quality: models.Quality{Resolution: "1080p"},
		Kind:    "movie",
		Year:    int64Ptr(2024),
		Seeders: -1,
		Peers:   -1,
	}
	if _, _, err := db.CheckMovie(&movie); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("UPDATE movies SET downloaded_at=datetime('now'), size_bytes=99 WHERE magnet_hash=?1", mustHash(t, movie.Magnet)); err != nil {
		t.Fatal(err)
	}
	downloads, err := db.RecentDownloads(10)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(downloads), 2)
	foundSeries, foundMovie := false, false
	for _, item := range downloads {
		if item.Kind == "series" && item.SizeBytes == 42 {
			foundSeries = true
		}
		if item.Kind == "movie" && item.SizeBytes == 99 {
			foundMovie = true
		}
	}
	assertTrue(t, foundSeries && foundMovie, "entrambi i download devono comparire")
}

func TestRecordsAndGroupsSeenMoviesAndSeries(t *testing.T) {
	db := newTestDB(t)
	movie := func(title, resolution string) models.Release {
		return models.Release{
			Title:   title,
			Magnet:  "magnet:?xt=urn:btih:" + utils.StableID(title),
			Source:  "rss",
			Quality: models.Quality{Resolution: resolution},
			Kind:    "movie",
			Year:    int64Ptr(2024),
			Seeders: -1,
			Peers:   -1,
		}
	}
	seriesRelease := testRelease()
	seriesRelease.Series = stringPtr("Example Show")
	cfg := DefaultConfig()
	if err := db.RecordSeenBatch([]models.Release{
		movie("The.Veil.2024.1080p.BluRay", "1080p"),
		movie("The.Veil.2024.2160p.WEB-DL", "2160p"),
		seriesRelease,
	}, &cfg); err != nil {
		t.Fatal(err)
	}
	groups, total, err := db.MoviesSeenGrouped(0, 50, "")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, total, int64(1))
	assertEqual(t, groups[0].GroupName, "The Veil")
	assertEqual(t, groups[0].Count, int64(2))
	assertEqual(t, groups[0].BestResolution, "2160p")
	entries, err := db.SeenByGroup("movie", groups[0].GroupKey, 50)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(entries), 2)
	assertEqual(t, entries[0].Resolution, "2160p")
	seriesGroups, seriesTotal, err := db.SeriesSeenGrouped(0, 50, "")
	if err != nil {
		t.Fatal(err)
	}
	filtered, filteredTotal, err := db.MoviesSeenGrouped(0, 50, "veil")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, filteredTotal, int64(1))
	assertEqual(t, filtered[0].Count, int64(2))
	none, noneTotal, err := db.MoviesSeenGrouped(0, 50, "inesistente")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, noneTotal, int64(0))
	assertEqual(t, len(none), 0)
	assertEqual(t, seriesTotal, int64(1))
	assertEqual(t, seriesGroups[0].GroupName, "Example Show")
	assertEqual(t, seriesGroups[0].Season, int64(1))
	if _, err := db.db.Exec("UPDATE movie_feed_seen SET found_at='2000-01-01T00:00:00+00:00'"); err != nil {
		t.Fatal(err)
	}
	pruned, err := db.PruneSeenOlderThan(30)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, pruned, 2)
	movieCount, seriesCount, err := db.SeenCounts()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, movieCount, int64(0))
	assertEqual(t, seriesCount, int64(1))
	pruned, err = db.PruneSeenOlderThan(0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, pruned, 0)
}

func TestPrunePreviewCountsWithoutDeleting(t *testing.T) {
	db := newTestDB(t)
	for i := 0; i < 5; i++ {
		stats := models.CycleStats{}
		if err := db.SaveCycle(&stats); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.db.Exec("INSERT INTO torrent_meta(hash,status,updated_at) VALUES ('h1','error', datetime('now','-30 days')),('h2','error', datetime('now','-1 days'))"); err != nil {
		t.Fatal(err)
	}
	preview, err := db.PrunePreview(2, 7, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, preview.OldCyclesToRemove, 3)
	assertEqual(t, preview.StaleTorrentsToRemove, 1)
	var cycles int64
	if err := db.db.QueryRow("SELECT COUNT(*) FROM cycle_history").Scan(&cycles); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, cycles, int64(5))
	report, err := db.Cleanup(2, 7)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, report.OldCyclesRemoved, 3)
	assertEqual(t, report.StaleTorrentsRemoved, 1)
}

func TestExtractsRenamedTitleFromArchivePath(t *testing.T) {
	assertEqual(t, renamedFileTitle("/home/user/SerieTV/Show - S01E01 - Pilot - [1080p].mkv"), "Show - S01E01 - Pilot - [1080p]")
	assertEqual(t, renamedFileTitle("/home/user/SerieTV/Show/"), "")
	assertEqual(t, renamedFileTitle(""), "")
}

func TestSeasonPackInferiorToExistingEpisodesIsRejected(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO series(id,name) VALUES (1,'Reacher')"); err != nil {
		t.Fatal(err)
	}
	for episode := int64(1); episode <= 3; episode++ {
		if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,quality_score,downloaded_at,archive_path) VALUES (1,4,?1,?2,1510,datetime('now'),'/x')", episode, "Reacher.S04E0"+itoa(episode)+".ITA.ENG.2160p.AMZN.WEB-DL.DDP5.1.DV.HDR.H.265-MeM"); err != nil {
			t.Fatal(err)
		}
	}
	packTitle := "Reacher - Season 04 (2026) [1080p H265 ITA ENG EAC3 SUB ITA ENG WEB-DL]"
	pack := models.Release{
		Title:        packTitle,
		Magnet:       "magnet:?xt=urn:btih:1111111111111111111111111111111111111111",
		Source:       "ExtTo",
		Quality:      ParseQuality(packTitle),
		Kind:         "series",
		Series:       stringPtr("Reacher"),
		Season:       int64Ptr(4),
		Episode:      int64Ptr(0),
		IsPack:       true,
		EpisodeRange: []int64{0},
		Year:         int64Ptr(2026),
		Seeders:      -1,
		Peers:        -1,
	}
	score := pack.Quality.Score()
	assertTrue(t, score < 1510, "il pack 1080p deve avere score inferiore")
	approved, reason, err := db.CheckSeriesScored(&pack, score, 50, &models.ApprovalContext{})
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, !approved, "pack inferiore non deve essere approvato")
	assertEqual(t, reason, "duplicate")

	gap := pack
	gap.Season = int64Ptr(5)
	gap.Title = "Reacher - Season 05 (2027) [1080p H265 ITA ENG]"
	gap.Magnet = "magnet:?xt=urn:btih:2222222222222222222222222222222222222222"
	approvedGap, _, err := db.CheckSeriesScored(&gap, score, 50, &models.ApprovalContext{})
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, approvedGap, "pack per stagione vuota deve essere approvato")
}

func itoa(value int64) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

func TestManualPackCanReplaceUnarchivedHigherQualityPlaceholders(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO series(id,name) VALUES (1,'Neagley')"); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSeriesMetadata("Neagley", [][2]int64{{1, 2}}); err != nil {
		t.Fatal(err)
	}
	for episode := int64(1); episode <= 2; episode++ {
		if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,quality_score) VALUES (1,1,?1,?2,1880)", episode, "Neagley.S01E0"+itoa(episode)+".2160p.DV.HDR.H.265"); err != nil {
			t.Fatal(err)
		}
	}
	packTitle := "Neagley.S01E01-02.1080p.AMZN.WEB-DL.ITA.ENG.DDP5.1.H.264-G66"
	pack := models.Release{
		Title:        packTitle,
		Magnet:       "magnet:?xt=urn:btih:abababababababababababababababababababab",
		Source:       "ExtTo",
		Quality:      ParseQuality(packTitle),
		Kind:         "series",
		Series:       stringPtr("Neagley"),
		Season:       int64Ptr(1),
		Episode:      int64Ptr(1),
		IsPack:       true,
		EpisodeRange: []int64{1, 2},
		Seeders:      -1,
		Peers:        -1,
	}
	approved, reason, err := db.CheckSeriesManualScored(&pack, pack.Quality.Score(), 200, &models.ApprovalContext{})
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, approved, "un pack manuale deve poter sostituire placeholder non archiviati: "+reason)
}

func TestSeasonPackCanBeRetriedWhenPlaceholderNotDownloaded(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO series(id,name) VALUES (1,'Neagley')"); err != nil {
		t.Fatal(err)
	}
	hash := "3333333333333333333333333333333333333333"
	magnet := "magnet:?xt=urn:btih:" + hash
	for episode := int64(1); episode <= 2; episode++ {
		var episodeHash any
		if episode == 1 {
			episodeHash = hash
		}
		if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,quality_score,magnet_hash,magnet_link,downloaded_at) VALUES (1,1,?1,?2,1680,?3,?4,NULL)", episode, "Neagley.S01E0"+itoa(episode)+".2160p.DV.HDR.H.265-G66", episodeHash, magnet); err != nil {
			t.Fatal(err)
		}
	}
	packTitle := "Neagley.S01E01-02.2160p.AMZN.WEB-DL.ITA.ENG.DDP5.1.DV.HDR.H.265-G66"
	pack := models.Release{
		Title:        packTitle,
		Magnet:       magnet,
		Source:       "ExtTo",
		Quality:      ParseQuality(packTitle),
		Kind:         "series",
		Series:       stringPtr("Neagley"),
		Season:       int64Ptr(1),
		Episode:      int64Ptr(1),
		IsPack:       true,
		EpisodeRange: []int64{1, 2},
		Year:         int64Ptr(2026),
		Seeders:      -1,
		Peers:        -1,
	}
	score := pack.Quality.Score()
	approved, _, err := db.CheckSeriesScored(&pack, score, 50, &models.ApprovalContext{})
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, approved, "un placeholder non scaricato non deve bloccare il retry")
	if _, err := db.db.Exec("UPDATE episodes SET downloaded_at=datetime('now') WHERE magnet_hash=?1 OR magnet_link=?2", hash, magnet); err != nil {
		t.Fatal(err)
	}
	approvedAgain, reason, err := db.CheckSeriesScored(&pack, score, 50, &models.ApprovalContext{})
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, !approvedAgain, "ora deve essere duplicato")
	assertEqual(t, reason, "duplicate")
}

func TestRescoreNormalizesBaseScoresWithSettings(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO series(id,name) VALUES (1,'Show')"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO episodes(id,series_id,season,episode,title,quality_score,archive_path,downloaded_at) VALUES (1,1,1,1,'Prova di fiducia',980,'/nas/Show - S01E01 - Prova - [1080p][h265].mkv',datetime('now'))"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO episodes(id,series_id,season,episode,title,quality_score,downloaded_at) VALUES (2,1,1,2,'Show.S01E02.1080p.WEB-DL.H.265',1000,datetime('now'))"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO episodes(id,series_id,season,episode,title,quality_score) VALUES (3,1,1,3,'Episodio 3',500)"); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Settings["score_res_1080p"] = "1500"
	if _, err := db.Rescore(&cfg); err != nil {
		t.Fatal(err)
	}
	score := func(id int64) int64 {
		var value int64
		if err := db.db.QueryRow("SELECT quality_score FROM episodes WHERE id=?1", id).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	assertEqual(t, score(1), int64(1700))
	assertEqual(t, score(2), int64(1900))
	assertEqual(t, score(3), int64(500))
}

func TestHistorySearchFiltersAcrossNameTagAndPath(t *testing.T) {
	db := newTestDB(t)
	insert := func(hash, name, tag, path string) {
		if _, err := db.db.Exec("INSERT INTO torrent_meta(hash,name,tag,status,removed_at,progress,processed_path,updated_at) VALUES (?1,?2,?3,'completed',datetime('now'),1,?4,datetime('now'))", hash, name, tag, path); err != nil {
			t.Fatal(err)
		}
	}
	insert("aaaa", "Silo.S01E01.1080p", "nas", "/media/Silo/S01E01.mkv")
	insert("bbbb", "Altro.Film.2024", "temp", "/tmp/altro.mkv")
	insert("cccc", "Silo.S01E02.2160p", "nas", "/media/Silo/S01E02.mkv")

	search := func(query string) ([]StoredTorrent, int64) {
		items, total, err := db.CompletedTorrents(0, 10, query)
		if err != nil {
			t.Fatal(err)
		}
		return items, total
	}
	_, total := search("silo")
	assertEqual(t, total, int64(2))
	_, total = search("silo nas")
	assertEqual(t, total, int64(2))
	_, total = search("silo temp")
	assertEqual(t, total, int64(0))
	_, total = search("nas")
	assertEqual(t, total, int64(2))
	_, total = search("/tmp/altro")
	assertEqual(t, total, int64(1))
	page, total, err := db.CompletedTorrents(1, 1, "silo")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, total, int64(2))
	assertEqual(t, len(page), 1)
}

func TestCompletedHistoryKeepsOnlyTheLast30Days(t *testing.T) {
	db := newTestDB(t)
	timestamp := func(modifier string) string {
		var value string
		if err := db.db.QueryRow("SELECT datetime('now', ?1)", modifier).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	insert := func(hash, name, completedAt string) {
		if _, err := db.db.Exec("INSERT INTO torrent_meta(hash,name,status,removed_at,completed_at,progress,updated_at) VALUES (?1,?2,'completed',?3,?3,1,?3)", hash, name, completedAt); err != nil {
			t.Fatal(err)
		}
	}
	insert("recent", "Recent download", timestamp("-29 days"))
	insert("old", "Old download", timestamp("-31 days"))
	items, total, err := db.CompletedTorrents(0, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, total, int64(1))
	assertEqual(t, len(items), 1)
	assertEqual(t, items[0].Name, "Recent download")
}

func TestMovieIdentityRenamePreservesDownloadState(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO movies(name,year,title,quality_score,magnet_hash,magnet_link,downloaded_at,size_bytes) VALUES ('Titolo Vecchio',2020,'Titolo.Vecchio.2020',1234,'hash1','magnet:?xt=urn:btih:hash1',datetime('now'),999)"); err != nil {
		t.Fatal(err)
	}
	updated, err := db.RenameMovieIdentity("Titolo Vecchio", "2020", "Titolo Nuovo", "2021")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, updated, 1)
	var name string
	var year, score, size int64
	var downloaded sql.NullString
	if err := db.db.QueryRow("SELECT name,year,quality_score,size_bytes,downloaded_at FROM movies WHERE magnet_hash='hash1'").Scan(&name, &year, &score, &size, &downloaded); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, name, "Titolo Nuovo")
	assertEqual(t, year, int64(2021))
	assertEqual(t, score, int64(1234))
	assertEqual(t, size, int64(999))
	assertTrue(t, downloaded.Valid, "lo stato di download resta intatto")
	updated, err = db.RenameMovieIdentity("Titolo Nuovo", "1900", "X", "1901")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, updated, 0)
}

func TestEpisodeAirDatesPersistForUnmaterializedEpisodes(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO series(id,name) VALUES (1,'Show')"); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSeriesMetadata("Show", [][2]int64{{1, 2}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,quality_score) VALUES (1,1,1,'Show.S01E01',900)"); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveEpisodeAirDates("Show", []EpisodeAirDate{
		{Season: 1, Episode: 1, AirDate: "2024-01-01"},
		{Season: 1, Episode: 2, AirDate: "2024-01-08"},
	}); err != nil {
		t.Fatal(err)
	}
	items, err := db.EpisodesForSeries("Show", nil)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(items), 2)
	byEpisode := func(episode int64) string {
		for _, item := range items {
			if item.Episode == episode {
				return item.AirDate
			}
		}
		t.Fatalf("episode %d not found", episode)
		return ""
	}
	assertEqual(t, byEpisode(1), "2024-01-01")
	assertEqual(t, byEpisode(2), "2024-01-08")
	dates, err := db.EpisodeAirDates()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(dates), 2)
}

func TestHousekeepingTrimsBoundedTables(t *testing.T) {
	db := newTestDB(t)
	for index := 1; index <= 5; index++ {
		if _, err := db.db.Exec("INSERT INTO cycle_history(at,payload_json) VALUES (?1,'{}')", "2020-01-0"+itoa(int64(index))+"T00:00:00+00:00"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.db.Exec("INSERT INTO gap_search_log(series_name,season,episode,last_searched_at) VALUES ('A',1,1,'2000-01-01T00:00:00+00:00')"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO gap_search_log(series_name,season,episode,last_searched_at) VALUES ('A',1,2,?1)", nowSQLite()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO upgrade_backup(new_hash,payload_json,created_at) VALUES ('h1','{}','2000-01-01T00:00:00+00:00')"); err != nil {
		t.Fatal(err)
	}
	report, err := db.Housekeeping(HousekeepingParams{
		RetainCycles:      1,
		ErrorAgeDays:      7,
		SeenDays:          0,
		GapLogDays:        30,
		UpgradeBackupDays: 30,
		HistoryDays:       0,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, report.OldCyclesRemoved, 4)
	assertEqual(t, report.GapLogsRemoved, 1)
	assertEqual(t, report.UpgradeBackupsRemoved, 1)
	var remaining int64
	if err := db.db.QueryRow("SELECT COUNT(*) FROM gap_search_log").Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, remaining, int64(1))
}

func TestRescoreUsesStoredMediaInfo(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	var seriesID int64
	if err := db.db.QueryRow("SELECT id FROM series WHERE name='Show'").Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	media := MediaInfo{HDR: "HDR10", VideoCodec: "hevc"}
	payload, err := json.Marshal(media)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,quality_score,downloaded_at,media_info_json) VALUES (?1,1,1,'Show.S01E01.1080p.WEB-DL',0,'2024-01-01T00:00:00+00:00',?2)", seriesID, string(payload)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,quality_score,downloaded_at) VALUES (?1,1,2,'Show.S01E02.1080p.WEB-DL',0,'2024-01-01T00:00:00+00:00')", seriesID); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	if _, err := db.Rescore(&cfg); err != nil {
		t.Fatal(err)
	}
	score := func(episode int64) int64 {
		var value int64
		if err := db.db.QueryRow("SELECT quality_score FROM episodes WHERE season=1 AND episode=?1", episode).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	assertTrue(t, score(1) > score(2), "stored MediaInfo must raise the archived score")
}

func TestMediaInfoBackfillTargetsSkipAlreadyProbedRows(t *testing.T) {
	db := newTestDB(t)
	archivePath := filepath.Join(t.TempDir(), "e1.mkv")
	if err := os.WriteFile(archivePath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	var seriesID int64
	if err := db.db.QueryRow("SELECT id FROM series WHERE name='Show'").Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,archive_path) VALUES (?1,1,1,'Show.S01E01',?2)", seriesID, archivePath); err != nil {
		t.Fatal(err)
	}
	targets, err := db.MediaInfoBackfillTargets(10)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(targets), 1)
	assertEqual(t, targets[0].Kind, "series")
	assertEqual(t, targets[0].Path, archivePath)
	if _, err := db.SetEpisodeMediaInfo("Show", 1, 1, "{}"); err != nil {
		t.Fatal(err)
	}
	targets, err = db.MediaInfoBackfillTargets(10)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(targets), 0)
}

func TestMediaInfoBackfillTargetsIgnoreMissingFiles(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	var seriesID int64
	if err := db.db.QueryRow("SELECT id FROM series WHERE name='Show'").Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "existing.mkv")
	if err := os.WriteFile(archivePath, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,archive_path) VALUES (?1,1,1,'Show.S01E01','/missing/e1.mkv'),(?1,1,2,'Show.S01E02',?2)", seriesID, archivePath); err != nil {
		t.Fatal(err)
	}
	targets, err := db.MediaInfoBackfillTargets(10)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(targets), 1)
	assertEqual(t, targets[0].Path, archivePath)
}

func TestMediaInfoRoundtripsForEpisodesAndMovies(t *testing.T) {
	db := newTestDB(t)
	info := MediaInfo{VideoCodec: "hevc", BitDepth: 10, HDR: "HDR10", Width: 1920, Height: 1080}
	if _, err := db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	var seriesID int64
	if err := db.db.QueryRow("SELECT id FROM series WHERE name='Show'").Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title) VALUES (?1,1,2,'Show.S01E02')", seriesID); err != nil {
		t.Fatal(err)
	}
	release := testRelease()
	release.Series = stringPtr("Show")
	release.Season = int64Ptr(1)
	release.Episode = int64Ptr(2)
	if err := db.SetMediaInfo(&release, &info); err != nil {
		t.Fatal(err)
	}
	stored, err := db.EpisodeMediaInfo("Show", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, stored["hdr"], "HDR10")
	assertEqual(t, stored["bit_depth"], float64(10))

	movie := release
	movie.Kind = "movie"
	movie.Title = "The Film"
	movie.Year = int64Ptr(2026)
	movie.Series = nil
	movie.Season = nil
	movie.Episode = nil
	if _, err := db.db.Exec("INSERT INTO movies(name,year,title) VALUES ('The Film',2026,'The Film')"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetMediaInfo(&movie, &info); err != nil {
		t.Fatal(err)
	}
	stored, err = db.MovieMediaInfo("The Film", int64Ptr(2026))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, stored["video_codec"], "hevc")
}

func TestDelayPendingWaitsForTheDueTime(t *testing.T) {
	db := newTestDB(t)
	series := testRelease()
	series.Series = stringPtr("Show")
	series.Season = int64Ptr(1)
	series.Episode = int64Ptr(1)

	if err := db.QueuePendingScored(&series, 0, 100); err != nil {
		t.Fatal(err)
	}
	ready, err := db.ReadyPending()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(ready), 1)
	if err := db.RemovePending("Show", 1, 1); err != nil {
		t.Fatal(err)
	}
	ready, err = db.ReadyPending()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(ready), 0)

	if err := db.QueuePendingScored(&series, 60, 100); err != nil {
		t.Fatal(err)
	}
	ready, err = db.ReadyPending()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(ready), 0)
	better := series
	better.Title = "Show.S01E01.better"
	better.Magnet = "magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := db.QueuePendingScored(&better, 60, 500); err != nil {
		t.Fatal(err)
	}
	var storedScore int64
	if err := db.db.QueryRow("SELECT best_quality_score FROM pending_downloads WHERE season=1 AND episode=1 AND status='pending'").Scan(&storedScore); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, storedScore, int64(500))

	movie := testRelease()
	movie.Kind = "movie"
	movie.Title = "The Film"
	movie.Year = int64Ptr(2026)
	if err := db.QueuePendingMovieScored(&movie, 0, 200); err != nil {
		t.Fatal(err)
	}
	readyMovies, err := db.ReadyPendingMovies()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(readyMovies), 1)
	if err := db.RemovePendingMovie("The Film", int64Ptr(2026)); err != nil {
		t.Fatal(err)
	}
	readyMovies, err = db.ReadyPendingMovies()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(readyMovies), 0)
}

func TestLaterArchivedRankReportsTheBestFollowingEpisode(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	var seriesID int64
	if err := db.db.QueryRow("SELECT id FROM series WHERE name='Show'").Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		episode int64
		title   string
	}{{5, "Show.S01E05.1080p.WEB-DL"}, {6, "Show.S01E06.720p.WEB-DL"}} {
		if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,downloaded_at,archive_path) VALUES (?1,1,?2,?3,'2024-01-01T00:00:00+00:00','/nas/f.mkv')", seriesID, row.episode, row.title); err != nil {
			t.Fatal(err)
		}
	}
	rank, err := db.LaterArchivedMaxResolutionRank("Show", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rank == nil || *rank != 5 {
		t.Fatalf("expected rank 5, got %v", rank)
	}
	rank, err = db.LaterArchivedMaxResolutionRank("Show", 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	if rank == nil || *rank != 4 {
		t.Fatalf("expected rank 4, got %v", rank)
	}
	rank, err = db.LaterArchivedMaxResolutionRank("Show", 1, 6)
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, rank == nil, "nessun episodio successivo")
}

func TestDisableUpgradesBlocksReplacement(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	var seriesID int64
	if err := db.db.QueryRow("SELECT id FROM series WHERE name='Show'").Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,quality_score,downloaded_at,archive_path) VALUES (?1,1,1,'Show.S01E01.1080p',1000,'2024-01-01T00:00:00+00:00','/nas/e1.mkv')", seriesID); err != nil {
		t.Fatal(err)
	}
	candidate := testRelease()
	candidate.Series = stringPtr("Show")
	candidate.Season = int64Ptr(1)
	candidate.Episode = int64Ptr(1)
	candidate.IsPack = false
	candidate.Quality = models.Quality{Resolution: "2160p", Source: "webdl"}
	score := candidate.Quality.Score()

	index := &models.ArchiveQualityIndex{Best: map[[2]int64]models.ArchiveQuality{
		{1, 1}: {Quality: models.Quality{Resolution: "1080p"}, Score: 1000},
	}}
	allowedContext := &models.ApprovalContext{Archive: index, Live: &models.LiveDownloads{}}
	approved, reason, err := db.CheckSeriesScored(&candidate, score, 200, allowedContext)
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, approved, "expected upgrade, got "+reason)

	if _, err := db.db.Exec("UPDATE episodes SET title='Show.S01E01.1080p', quality_score=1000, magnet_hash=NULL, downloaded_at='2024-01-01T00:00:00+00:00', archive_path='/nas/e1.mkv' WHERE series_id=?1 AND season=1 AND episode=1", seriesID); err != nil {
		t.Fatal(err)
	}
	cutoffContext := &models.ApprovalContext{Archive: index, Live: &models.LiveDownloads{}, ForbidUpgrade: true}
	approved, reason, err = db.CheckSeriesScored(&candidate, score, 200, cutoffContext)
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, !approved, "expected upgrades_disabled")
	assertEqual(t, reason, "upgrades_disabled")
}

func TestSmartEpisodeIsAlwaysOnButExemptsGapsAndUpgrades(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO series(name) VALUES ('Show')"); err != nil {
		t.Fatal(err)
	}
	var seriesID int64
	if err := db.db.QueryRow("SELECT id FROM series WHERE name='Show'").Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,quality_score,downloaded_at,archive_path) VALUES (?1,1,5,'Show.S01E05.1080p',1000,'2024-01-01T00:00:00+00:00','/nas/e5.mkv')", seriesID); err != nil {
		t.Fatal(err)
	}
	clearEpisode := func() {
		if _, err := db.db.Exec("DELETE FROM episodes WHERE series_id=?1 AND season=1 AND episode=1", seriesID); err != nil {
			t.Fatal(err)
		}
	}
	candidate := testRelease()
	candidate.Series = stringPtr("Show")
	candidate.Season = int64Ptr(1)
	candidate.Episode = int64Ptr(1)
	candidate.IsPack = false
	candidate.Quality = models.Quality{Resolution: "1080p", Source: "webdl"}
	score := candidate.Quality.Score()

	approved, reason, err := db.CheckSeriesScored(&candidate, score, 200, &models.ApprovalContext{})
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, !approved, "atteso smart_episode")
	assertEqual(t, reason, "smart_episode")

	gapContext := &models.ApprovalContext{GapEpisode: true}
	approved, reason, err = db.CheckSeriesScored(&candidate, score, 200, gapContext)
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, approved, "expected gap approval, got "+reason)
	clearEpisode()

	candidate.Quality = models.Quality{Resolution: "2160p", Source: "webdl"}
	approved, reason, err = db.CheckSeriesScored(&candidate, candidate.Quality.Score(), 200, &models.ApprovalContext{})
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, approved, "expected upgrade approval, got "+reason)
}

func TestProviderBackoffEscalatesAndRecovers(t *testing.T) {
	db := newTestDB(t)
	blocked, err := db.ProviderBlocked("indexer", "Prowlarr")
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, !blocked, "provider non bloccato all'inizio")
	providers, err := db.BlockedProviders()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(providers), 0)

	if err := db.ProviderFailure("indexer", "Prowlarr", "HTTP 503"); err != nil {
		t.Fatal(err)
	}
	blocked, err = db.ProviderBlocked("indexer", "Prowlarr")
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, blocked, "provider bloccato dopo il fallimento")
	providers, err = db.BlockedProviders()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := providers[[2]string{"indexer", "Prowlarr"}]; !ok {
		t.Fatal("provider bloccato non elencato")
	}
	statuses, err := db.ProviderStatuses()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(statuses), 1)
	assertEqual(t, statuses[0].Level, int64(1))
	assertEqual(t, statuses[0].LastError, "HTTP 503")

	if err := db.ProviderSuccess("indexer", "Prowlarr"); err != nil {
		t.Fatal(err)
	}
	blocked, err = db.ProviderBlocked("indexer", "Prowlarr")
	if err != nil {
		t.Fatal(err)
	}
	assertTrue(t, !blocked, "provider sbloccato dopo il successo")
	statuses, err = db.ProviderStatuses()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(statuses), 0)

	if err := db.ProviderFailure("feed", "https://example.test/rss", "timeout"); err != nil {
		t.Fatal(err)
	}
	if err := db.ClearProviderStatus(nil); err != nil {
		t.Fatal(err)
	}
	statuses, err = db.ProviderStatuses()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(statuses), 0)
}

func TestCheckMovieScoredMatchesYearWithinTolerance(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO movies(name,year,title,quality_score) VALUES ('Example',2020,'Example',100)"); err != nil {
		t.Fatal(err)
	}
	release := models.Release{
		Title:   "Example",
		Kind:    "movie",
		Year:    int64Ptr(2021),
		Magnet:  "magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
		Quality: models.Quality{Resolution: "1080p"},
	}
	approved, action, err := db.CheckMovieScored(&release, 200, 50)
	if err != nil {
		t.Fatalf("CheckMovieScored: %v", err)
	}
	if !approved || action != "upgrade" {
		t.Fatalf("approved=%v action=%q, want upgrade within ±1 year", approved, action)
	}
}

func TestCheckMovieScoredIgnoresYearOutsideTolerance(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO movies(name,year,title,quality_score) VALUES ('Example',2020,'Example',100)"); err != nil {
		t.Fatal(err)
	}
	release := models.Release{
		Title:   "Example",
		Kind:    "movie",
		Year:    int64Ptr(2023),
		Magnet:  "magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
		Quality: models.Quality{Resolution: "1080p"},
	}
	approved, action, err := db.CheckMovieScored(&release, 200, 50)
	if err != nil {
		t.Fatalf("CheckMovieScored: %v", err)
	}
	if !approved || action != "approved" {
		t.Fatalf("approved=%v action=%q, want a new approved download", approved, action)
	}
	var score int64
	if err := db.db.QueryRow("SELECT quality_score FROM movies WHERE name='Example' AND year=2020").Scan(&score); err != nil {
		t.Fatal(err)
	}
	if score != 100 {
		t.Fatalf("existing 2020 score = %d, want 100 (untouched)", score)
	}
}

func TestCheckMovieScoredPrefersExactYear(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO movies(name,year,title,quality_score) VALUES ('Example',2020,'Example',100),('Example',2021,'Example',150)"); err != nil {
		t.Fatal(err)
	}
	release := models.Release{
		Title:   "Example",
		Kind:    "movie",
		Year:    int64Ptr(2021),
		Magnet:  "magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
		Quality: models.Quality{Resolution: "1080p"},
	}
	if _, action, err := db.CheckMovieScored(&release, 200, 50); err != nil || action != "upgrade" {
		t.Fatalf("action=%q err=%v, want upgrade", action, err)
	}
	var score2020, score2021 int64
	if err := db.db.QueryRow("SELECT quality_score FROM movies WHERE name='Example' AND year=2020").Scan(&score2020); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRow("SELECT quality_score FROM movies WHERE name='Example' AND year=2021").Scan(&score2021); err != nil {
		t.Fatal(err)
	}
	if score2021 != 200 || score2020 != 100 {
		t.Fatalf("scores 2020=%d 2021=%d, want 100/200 (exact year replaced)", score2020, score2021)
	}
}
