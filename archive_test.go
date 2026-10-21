package gextto

import (
	"bytes"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

// openTestArchive opens a fresh archive in a per-test temporary directory.
func openTestArchive(t *testing.T) *Archive {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gextto_archive.db")
	archive, err := OpenArchive(path)
	if err != nil {
		t.Fatalf("OpenArchive: %v", err)
	}
	t.Cleanup(func() { _ = archive.db.Close() })
	return archive
}

func TestRecentEntriesRespectTheLimit(t *testing.T) {
	archive := openTestArchive(t)
	year := int64(2026)
	releases := make([]models.Release, 0, 5)
	for index := 1; index <= 5; index++ {
		releases = append(releases, models.Release{
			Title:        fmt.Sprintf("Release %d", index),
			Magnet:       fmt.Sprintf("magnet:?xt=urn:btih:%040x", index),
			Source:       fmt.Sprintf("source-%d", index),
			Kind:         "movie",
			Year:         &year,
			DiscoveredAt: time.Now().UTC(),
			Seeders:      -1,
			Peers:        -1,
		})
	}
	if err := archive.SaveBatch(releases, &Config{}); err != nil {
		t.Fatalf("SaveBatch: %v", err)
	}
	recent, err := archive.RecentEntries(3)
	if err != nil {
		t.Fatalf("RecentEntries(3): %v", err)
	}
	if len(recent) != 3 {
		t.Fatalf("RecentEntries(3) = %d entries, want 3", len(recent))
	}
	all, err := archive.RecentEntries(100)
	if err != nil {
		t.Fatalf("RecentEntries(100): %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("RecentEntries(100) = %d entries, want 5", len(all))
	}
}

func TestDeleteMatchingRemovesAllTitleMatchesOnly(t *testing.T) {
	archive := openTestArchive(t)
	if _, err := archive.db.Exec(`INSERT INTO archive(title,magnet,magnet_key,source,added_at) VALUES
		('Adult Porno Release 1','magnet:porno-1',gextto_magnet_key('magnet:porno-1'),'feed','2026-01-01'),
		('Adult Porno Release 2','magnet:porno-2',gextto_magnet_key('magnet:porno-2'),'feed','2026-01-02'),
		('Family Movie','magnet:family',gextto_magnet_key('magnet:family'),'feed','2026-01-03')`); err != nil {
		t.Fatalf("insert archive fixtures: %v", err)
	}
	removed, err := archive.DeleteMatching("porno")
	if err != nil || removed != 2 {
		t.Fatalf("DeleteMatching = (%d, %v), want (2, nil)", removed, err)
	}
	remaining, err := archive.Count()
	if err != nil || remaining != 1 {
		t.Fatalf("remaining count = (%d, %v), want (1, nil)", remaining, err)
	}
	if _, err := archive.DeleteMatching("-porno"); err == nil {
		t.Fatal("DeleteMatching should refuse queries without a positive search term")
	}
}

func TestArchiveContentPrefiltersPreviewAndDeleteMatching(t *testing.T) {
	archive := openTestArchive(t)
	if _, err := archive.db.Exec(`INSERT INTO archive(title,magnet,magnet_key,source,added_at) VALUES
		('Adult Porno Movie','magnet:adult',gextto_magnet_key('magnet:adult'),'feed','2026-01-01'),
		('CJK 動画タイトル','magnet:cjk',gextto_magnet_key('magnet:cjk'),'feed','2026-01-02'),
		('Family Movie','magnet:family',gextto_magnet_key('magnet:family'),'feed','2026-01-03')`); err != nil {
		t.Fatalf("insert archive fixtures: %v", err)
	}

	page, err := archive.BrowseContentFilteredPage("", []string{"[cjk]"}, 1, 100)
	if err != nil || page.Total != 1 || page.Items[0].Magnet != "magnet:cjk" {
		t.Fatalf("CJK prefilter page = (%+v, %v), want only CJK title", page, err)
	}
	page, err = archive.BrowseContentFilteredPage("movie", []string{"[porno]"}, 1, 100)
	if err != nil || page.Total != 1 || page.Items[0].Magnet != "magnet:adult" {
		t.Fatalf("keyword plus adult prefilter page = (%+v, %v), want only adult movie", page, err)
	}
	removed, err := archive.DeleteContentFiltered("", []string{"[porno]"})
	if err != nil || removed != 1 {
		t.Fatalf("adult prefilter deletion = (%d, %v), want (1, nil)", removed, err)
	}
	remaining, err := archive.Count()
	if err != nil || remaining != 2 {
		t.Fatalf("remaining count = (%d, %v), want (2, nil)", remaining, err)
	}
}

func TestArchiveDeleteIDsBatchesAndDeduplicates(t *testing.T) {
	archive := openTestArchive(t)
	ids := make([]int64, 0, 501)
	for index := 0; index < 501; index++ {
		result, err := archive.db.Exec("INSERT INTO archive(title,magnet,magnet_key,source,added_at) VALUES (?1,?2,gextto_magnet_key(?2),'test',datetime('now'))", fmt.Sprintf("Batch %d", index), fmt.Sprintf("magnet:batch-%d", index))
		if err != nil {
			t.Fatalf("insert batch row %d: %v", index, err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatalf("read batch row id %d: %v", index, err)
		}
		ids = append(ids, id)
	}
	ids = append(ids, ids[0], 0, -1)
	removed, err := archive.DeleteIDs(ids)
	if err != nil || removed != 501 {
		t.Fatalf("DeleteIDs removed (%d, %v), want (501, nil)", removed, err)
	}
	count, err := archive.Count()
	if err != nil || count != 0 {
		t.Fatalf("archive count = (%d, %v), want (0, nil)", count, err)
	}
}

func TestRetainsDistinctHashesAndDeduplicatesTheSameMagnet(t *testing.T) {
	archive := openTestArchive(t)
	year := int64(2026)
	release := func(magnet, title string) models.Release {
		return models.Release{
			Title:        title,
			Magnet:       magnet,
			Source:       "test",
			Kind:         "movie",
			Year:         &year,
			DiscoveredAt: time.Now().UTC(),
			Seeders:      -1,
			Peers:        -1,
		}
	}
	first := release(
		"magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
		"Example Movie 1080p",
	)
	second := release(
		"magnet:?xt=urn:btih:abcdefabcdefabcdefabcdefabcdefabcdefabcd",
		"Example Movie 2160p",
	)
	if err := archive.SaveBatch([]models.Release{first, first, second}, &Config{}); err != nil {
		t.Fatalf("SaveBatch: %v", err)
	}
	count, err := archive.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 2 {
		t.Fatalf("Count = %d, want 2", count)
	}
	found, err := archive.Search("Example Movie")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("len(Search) = %d, want 2", len(found))
	}
	page, err := archive.BrowsePage("Example 1080p", 1, 100)
	if err != nil {
		t.Fatalf("BrowsePage(Example 1080p): %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("BrowsePage(Example 1080p).Total = %d, want 1", page.Total)
	}
	missing, err := archive.BrowsePage("Example Missing", 1, 100)
	if err != nil {
		t.Fatalf("BrowsePage(Example Missing): %v", err)
	}
	if missing.Total != 0 {
		t.Fatalf("BrowsePage(Example Missing).Total = %d, want 0", missing.Total)
	}
}

func TestSaveBatchDoesNotPersistConfiguredContentFilters(t *testing.T) {
	archive := openTestArchive(t)
	cfg := &Config{ContentFilters: []string{"[porno]"}}
	releases := []models.Release{
		{Title: "Adult.Porno.Movie.2026", Magnet: "magnet:?xt=urn:btih:adult"},
		{Title: "Family.Movie.2026", Magnet: "magnet:?xt=urn:btih:family"},
	}
	if err := archive.SaveBatch(releases, cfg); err != nil {
		t.Fatalf("SaveBatch: %v", err)
	}
	page, err := archive.BrowseContentFilteredPage("", []string{"[porno]"}, 1, 100)
	if err != nil {
		t.Fatalf("BrowseContentFilteredPage: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("filtered archive results = %d, want 0", page.Total)
	}
	count, err := archive.Count()
	if err != nil || count != 1 {
		t.Fatalf("archive count = (%d, %v), want (1, nil)", count, err)
	}
}

func TestSearchPrefersRecentMatchesWhenResultLimitApplies(t *testing.T) {
	archive := openTestArchive(t)
	releases := make([]models.Release, 0, 201)
	for index := 0; index < 201; index++ {
		releases = append(releases, models.Release{
			Title:  fmt.Sprintf("Example Show old release %03d", index),
			Magnet: fmt.Sprintf("magnet:?xt=urn:btih:%040x", index+1),
			Source: "test",
		})
	}
	if err := archive.SaveBatch(releases, &Config{}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.db.Exec("UPDATE archive SET title='Example Show latest release', added_at='2099-01-01 00:00:00' WHERE magnet=?1", releases[200].Magnet); err != nil {
		t.Fatal(err)
	}
	found, err := archive.Search("Example Show")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 200 {
		t.Fatalf("len(Search) = %d, want 200", len(found))
	}
	if found[0][0] != "Example Show latest release" {
		t.Fatalf("first search result = %q, want most recent release", found[0][0])
	}
}

func TestStoresAndCanonicalizesTorrentURLs(t *testing.T) {
	archive := openTestArchive(t)
	torrentURL := "http://jackett:9117/dl/test/?path=ZXhhbXBsZQ"
	series := "Example Show"
	season := int64(1)
	episode := int64(1)
	release := models.Release{
		TorrentURL:   &torrentURL,
		Title:        "Example.Show.S01E01.1080p",
		Magnet:       "",
		Source:       "Jackett RSS - Test",
		Kind:         "series",
		Series:       &series,
		Season:       &season,
		Episode:      &episode,
		EpisodeRange: []int64{1},
		DiscoveredAt: time.Now().UTC(),
		Seeders:      -1,
		Peers:        -1,
	}
	if err := archive.SaveBatch([]models.Release{release}, &Config{}); err != nil {
		t.Fatalf("SaveBatch: %v", err)
	}
	count, err := archive.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Fatalf("Count = %d, want 1", count)
	}
	magnet := "magnet:?xt=urn:btih:0123456789012345678901234567890123456789"
	if err := archive.CanonicalizeTorrentURL(torrentURL, magnet); err != nil {
		t.Fatalf("CanonicalizeTorrentURL: %v", err)
	}
	entries, err := archive.Search("Example Show")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("Search returned no entries")
	}
	if entries[0][1] != magnet {
		t.Fatalf("entry magnet = %q, want %q", entries[0][1], magnet)
	}
}

func TestArchiveDeduplicatesEphemeralTorrentURLsByGUID(t *testing.T) {
	archive := openTestArchive(t)
	const guid = "https://tracker.example/details/42"
	url1 := "http://jackett:9117/dl/test/?path=one"
	url2 := "http://jackett:9117/dl/test/?path=two"
	url3 := "http://jackett:9117/dl/test/?path=three"
	release := func(url, listing string) models.Release {
		return models.Release{
			Title:        "Example.Show.S01E01.1080p",
			TorrentURL:   &url,
			Source:       "jackett:Test",
			GUID:         listing,
			DiscoveredAt: time.Now().UTC(),
			Seeders:      -1,
			Peers:        -1,
		}
	}
	// The same listing, with the regenerated Jackett URL, is one row.
	if err := archive.SaveBatch([]models.Release{release(url1, guid)}, &Config{}); err != nil {
		t.Fatalf("SaveBatch first: %v", err)
	}
	if err := archive.SaveBatch([]models.Release{release(url2, guid)}, &Config{}); err != nil {
		t.Fatalf("SaveBatch second: %v", err)
	}
	if count, err := archive.Count(); err != nil || count != 1 {
		t.Fatalf("count after the same guid = %d, %v; want 1", count, err)
	}
	var stored string
	if err := archive.db.QueryRow("SELECT magnet FROM archive").Scan(&stored); err != nil || stored != url2 {
		t.Fatalf("stored URL = %q, %v; want the newest %q", stored, err, url2)
	}
	// A different listing of the same title stays a second row.
	if err := archive.SaveBatch([]models.Release{release(url1, "https://tracker.example/details/43")}, &Config{}); err != nil {
		t.Fatalf("SaveBatch other guid: %v", err)
	}
	if count, err := archive.Count(); err != nil || count != 2 {
		t.Fatalf("count after a new guid = %d, %v; want 2", count, err)
	}
	// Downloading the first listing replaces its URL with the magnet and keeps
	// the guid key, so another pass cannot append it again.
	magnet := "magnet:?xt=urn:btih:0123456789012345678901234567890123456789"
	first := release(url2, guid)
	if err := archive.CanonicalizeRelease(&first, magnet); err != nil {
		t.Fatalf("CanonicalizeRelease: %v", err)
	}
	var hash string
	if err := archive.db.QueryRow(
		"SELECT magnet, COALESCE(magnet_hash,'') FROM archive WHERE magnet_key=?1",
		archiveMagnetKeyString(archiveGUIDIdentity("jackett:Test", guid)),
	).Scan(&stored, &hash); err != nil {
		t.Fatalf("read canonical row: %v", err)
	}
	if stored != magnet || hash != "0123456789012345678901234567890123456789" {
		t.Fatalf("canonical row = %q, %q", stored, hash)
	}
	if err := archive.SaveBatch([]models.Release{release(url3, guid)}, &Config{}); err != nil {
		t.Fatalf("SaveBatch after canonicalization: %v", err)
	}
	if count, err := archive.Count(); err != nil || count != 2 {
		t.Fatalf("count after canonicalization = %d, %v; want 2", count, err)
	}
	if err := archive.db.QueryRow("SELECT magnet FROM archive WHERE magnet_key=?1", archiveMagnetKeyString(archiveGUIDIdentity("jackett:Test", guid))).Scan(&stored); err != nil || stored != magnet {
		t.Fatalf("canonical magnet was overwritten: %q, %v", stored, err)
	}
	// Deleting the other URL row by its exact link works even though its key
	// is the guid, not the URL digest.
	if removed, err := archive.Delete(url1); err != nil || !removed {
		t.Fatalf("Delete(url) = %v, %v; want true", removed, err)
	}
	if count, err := archive.Count(); err != nil || count != 1 {
		t.Fatalf("count after Delete = %d, %v; want 1", count, err)
	}
}

func TestApplyArchiveRetention(t *testing.T) {
	archive := openTestArchive(t)
	for index, row := range []struct {
		title string
		days  string
	}{
		{"Old One", "-400 days"},
		{"Old Two", "-399 days"},
		{"Old Three", "-398 days"},
		{"Recent One", "-10 days"},
		{"Recent Two", "-5 days"},
	} {
		magnet := fmt.Sprintf("magnet:?xt=urn:btih:retention-%d", index)
		if _, err := archive.db.Exec(
			"INSERT INTO archive(title,magnet,magnet_key,source,added_at) VALUES (?1,?2,gextto_magnet_key(?2),'feed',datetime('now',?3))",
			row.title, magnet, row.days,
		); err != nil {
			t.Fatalf("insert %s: %v", row.title, err)
		}
	}
	cfg := &Config{Settings: map[string]string{
		"archive_cleanup_enabled": "true",
		"archive_max_age_days":    "365",
		"archive_keep_min":        "0",
	}}
	removed, err := applyArchiveRetention(cfg, archive)
	if err != nil || removed != 3 {
		t.Fatalf("applyArchiveRetention = (%d, %v), want (3, nil)", removed, err)
	}
	if count, err := archive.Count(); err != nil || count != 2 {
		t.Fatalf("count after retention = %d, %v; want 2", count, err)
	}

	// Disabled, zero age and dry-run leave the archive alone.
	for _, disabled := range []*Config{
		{Settings: map[string]string{"archive_cleanup_enabled": "false", "archive_max_age_days": "365"}},
		{Settings: map[string]string{"archive_cleanup_enabled": "true", "archive_max_age_days": "0"}},
		{Settings: map[string]string{"archive_cleanup_enabled": "true", "archive_max_age_days": "365"}, DryRun: true},
	} {
		if removed, err := applyArchiveRetention(disabled, archive); err != nil || removed != 0 {
			t.Fatalf("disabled retention = (%d, %v), want (0, nil)", removed, err)
		}
	}

	// keep_min always preserves the newest rows, even when they are old.
	kept := openTestArchive(t)
	if _, err := kept.db.Exec(`INSERT INTO archive(title,magnet,magnet_key,source,added_at) VALUES
		('Old A','magnet:?xt=urn:btih:keep-a',gextto_magnet_key('magnet:?xt=urn:btih:keep-a'),'feed',datetime('now','-500 days')),
		('Old B','magnet:?xt=urn:btih:keep-b',gextto_magnet_key('magnet:?xt=urn:btih:keep-b'),'feed',datetime('now','-400 days')),
		('Old C','magnet:?xt=urn:btih:keep-c',gextto_magnet_key('magnet:?xt=urn:btih:keep-c'),'feed',datetime('now','-300 days'))`); err != nil {
		t.Fatal(err)
	}
	keepCfg := &Config{Settings: map[string]string{
		"archive_cleanup_enabled": "true",
		"archive_max_age_days":    "365",
		"archive_keep_min":        "2",
	}}
	if removed, err := applyArchiveRetention(keepCfg, kept); err != nil || removed != 1 {
		t.Fatalf("keep_min retention = (%d, %v), want (1, nil)", removed, err)
	}
	if count, err := kept.Count(); err != nil || count != 2 {
		t.Fatalf("count after keep_min = %d, %v; want 2", count, err)
	}
}

// TestArchiveMigratesToMagnetKey pins the one-off rebuild of an archive created
// before magnet_key: every row keeps its id and the FTS search still finds it,
// the exact-magnet dedup holds, and the unused title index is gone.
func TestArchiveMigratesToMagnetKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.db")
	legacy, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	const legacySchema = `CREATE TABLE archive (id INTEGER PRIMARY KEY, title TEXT NOT NULL, magnet TEXT NOT NULL UNIQUE, magnet_hash TEXT, source TEXT, quality_score INTEGER, added_at TEXT NOT NULL); CREATE INDEX idx_archive_title ON archive(title); CREATE INDEX idx_archive_added ON archive(added_at DESC); CREATE VIRTUAL TABLE archive_fts USING fts5(title, content='archive', content_rowid='id'); CREATE TRIGGER archive_fts_ai AFTER INSERT ON archive BEGIN INSERT INTO archive_fts(rowid,title) VALUES (new.id,new.title); END; CREATE TRIGGER archive_fts_ad AFTER DELETE ON archive BEGIN INSERT INTO archive_fts(archive_fts,rowid,title) VALUES('delete',old.id,old.title); END;`
	if _, err := legacy.Exec(legacySchema); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO archive(id,title,magnet,magnet_hash,source,added_at) VALUES
		(7,'Old Show S01E01','magnet:?xt=urn:btih:aaaa&tr=x','aaaa','feed','2026-01-01'),
		(9,'Old Show S01E02','magnet:?xt=urn:btih:bbbb','bbbb','feed','2026-01-02')`); err != nil {
		t.Fatal(err)
	}
	// A magnet longer than the stack buffer of archiveMagnetKeyString.
	long := "magnet:?xt=urn:btih:cccc" + strings.Repeat("&tr=udp%3A%2F%2Ftracker.example.org%3A1337", 60)
	if _, err := legacy.Exec("INSERT INTO archive(id,title,magnet,magnet_hash,source,added_at) VALUES (11,'Long Show S01E03',?1,'cccc','feed','2026-01-03')", long); err != nil {
		t.Fatal(err)
	}
	_ = legacy.Close()

	archive, err := OpenArchive(path)
	if err != nil {
		t.Fatalf("OpenArchive on a legacy archive: %v", err)
	}
	t.Cleanup(func() { _ = archive.Close() })
	columns, err := tableColumns(archive.db, "archive")
	if err != nil || !columns["magnet_key"] {
		t.Fatalf("magnet_key missing after the migration: %v %v", columns, err)
	}
	var titleIndex int
	if err := archive.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='idx_archive_title'").Scan(&titleIndex); err != nil || titleIndex != 0 {
		t.Fatalf("idx_archive_title kept: %d %v", titleIndex, err)
	}
	var id int64
	if err := archive.db.QueryRow("SELECT id FROM archive WHERE magnet='magnet:?xt=urn:btih:bbbb'").Scan(&id); err != nil || id != 9 {
		t.Fatalf("row id not kept: %d %v", id, err)
	}
	found, err := archive.Search("Old Show")
	if err != nil || len(found) != 2 {
		t.Fatalf("FTS after the migration = %v, %v; want 2 rows", found, err)
	}
	if !bytes.Equal(archiveMagnetKeyString(long), archiveMagnetKey([]byte(long))) {
		t.Fatal("the long-magnet key differs from the plain digest")
	}
	var longKey []byte
	if err := archive.db.QueryRow("SELECT magnet_key FROM archive WHERE id=11").Scan(&longKey); err != nil || !bytes.Equal(longKey, archiveMagnetKey([]byte(long))) {
		t.Fatalf("long magnet key = %x, %v", longKey, err)
	}
	// The same exact magnet is still refused, a different string accepted.
	if err := archive.SaveBatch([]models.Release{
		{Title: "Dup", Magnet: "magnet:?xt=urn:btih:aaaa&tr=x"},
		{Title: "Other trackers", Magnet: "magnet:?xt=urn:btih:aaaa&tr=y"},
	}, &Config{}); err != nil {
		t.Fatal(err)
	}
	if count, err := archive.Count(); err != nil || count != 4 {
		t.Fatalf("count after dedup = %d, %v; want 4", count, err)
	}
	if removed, err := archive.Delete("magnet:?xt=urn:btih:bbbb"); err != nil || !removed {
		t.Fatalf("Delete by magnet = %v, %v", removed, err)
	}
	// Reopening an already migrated archive is a no-op.
	_ = archive.Close()
	again, err := OpenArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = again.Close()
}

// benchArchiveMagnet is a realistic magnet: ~400 bytes with the tracker list.
func benchArchiveMagnet(n int) string {
	return fmt.Sprintf("magnet:?xt=urn:btih:%040x&dn=Some.Show.S01E%02d.1080p.WEB-DL.ITA.ENG&tr=udp%%3A%%2F%%2Ftracker.opentrackr.org%%3A1337%%2Fannounce&tr=udp%%3A%%2F%%2Fopen.stealth.si%%3A80%%2Fannounce&tr=udp%%3A%%2F%%2Ftracker.torrent.eu.org%%3A451%%2Fannounce&tr=udp%%3A%%2F%%2Fexodus.desync.com%%3A6969%%2Fannounce&tr=udp%%3A%%2F%%2Ftracker.tiny-vps.com%%3A6969", n, n%99)
}

// BenchmarkArchiveSaveBatch measures a feed pass archiving 1000 releases into
// an archive that already holds 50,000 (half of them already known), and the
// file size of those 50,000 rows.
func BenchmarkArchiveSaveBatch(b *testing.B) {
	path := filepath.Join(b.TempDir(), "gextto_archive.db")
	archive, err := OpenArchive(path)
	if err != nil {
		b.Fatal(err)
	}
	defer archive.Close()
	cfg := &Config{}
	seed := make([]models.Release, 50000)
	for i := range seed {
		seed[i] = models.Release{Title: fmt.Sprintf("Some Show S01E%02d %d", i%99, i), Magnet: benchArchiveMagnet(i)}
	}
	if err := archive.SaveBatch(seed, cfg); err != nil {
		b.Fatal(err)
	}
	// The file after the 50,000 seed rows: a fixed point, unlike the size at
	// the end, which grows with b.N.
	seededSize := int64(0)
	if info, err := os.Stat(path); err == nil {
		seededSize = info.Size()
	}
	next := 50000
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		batch := make([]models.Release, 1000)
		for j := range batch {
			n := next - 500 + j
			batch[j] = models.Release{Title: fmt.Sprintf("Some Show S01E%02d %d", n%99, n), Magnet: benchArchiveMagnet(n)}
		}
		next += 500
		if err := archive.SaveBatch(batch, cfg); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(seededSize)/(1<<20), "MiB-seeded")
}

// TestArchiveKeepsOneRowPerTorrent pins the infohash dedup: an archive with
// several listings of the same torrent keeps only the newest one, a later
// listing under another magnet refreshes that row, a base32 spelling is the
// same torrent as its hex one, and rows without a hash are never merged.
func TestArchiveKeepsOneRowPerTorrent(t *testing.T) {
	const hexHash = "0123456789abcdef0123456789abcdef01234567"
	raw, _ := hex.DecodeString(hexHash)
	b32 := base32.StdEncoding.EncodeToString(raw)
	path := filepath.Join(t.TempDir(), "archive.db")
	legacy, err := OpenArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.db.Exec("DROP INDEX idx_archive_hash"); err != nil {
		t.Fatal(err)
	}
	rows := []struct{ title, magnet, hash, added string }{
		{"Show S01E01 1080p", "magnet:?xt=urn:btih:" + hexHash + "&tr=a", hexHash, "2026-01-01 10:00:00"},
		{"Show S01E01 1080p [Prowlarr - Knaben]", "magnet:?xt=urn:btih:" + hexHash + "&tr=b", hexHash, "2026-01-03 10:00:00"},
		{"Show.S01E01.1080p", "magnet:?xt=urn:btih:" + b32, strings.ToLower(b32), "2026-01-02 10:00:00"},
		{"Url One", "http://jackett/dl/1", "", "2026-01-01 09:00:00"},
		{"Url One", "http://jackett/dl/2", "", "2026-01-01 09:00:00"},
	}
	for _, row := range rows {
		if _, err := legacy.db.Exec("INSERT INTO archive(title,magnet,magnet_key,magnet_hash,source,added_at) VALUES (?1,?2,gextto_magnet_key(?2),?3,'feed',?4)", row.title, row.magnet, row.hash, row.added); err != nil {
			t.Fatal(err)
		}
	}
	_ = legacy.Close()

	archive, err := OpenArchive(path)
	if err != nil {
		t.Fatalf("reopen with duplicates: %v", err)
	}
	t.Cleanup(func() { _ = archive.Close() })
	count := func() int64 {
		n, err := archive.Count()
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if got := count(); got != 3 {
		t.Fatalf("rows after the dedup = %d, want 3 (1 torrent + 2 URLs)", got)
	}
	var title string
	if err := archive.db.QueryRow("SELECT title FROM archive WHERE magnet_hash=?1", hexHash).Scan(&title); err != nil || title != "Show S01E01 1080p [Prowlarr - Knaben]" {
		t.Fatalf("kept row = %q, %v; want the newest listing", title, err)
	}
	// The exact same magnet is ignored; another magnet of it refreshes the row.
	cfg := &Config{}
	if err := archive.SaveBatch([]models.Release{{Title: "Ignored", Magnet: "magnet:?xt=urn:btih:" + hexHash + "&tr=b"}}, cfg); err != nil {
		t.Fatal(err)
	}
	if err := archive.SaveBatch([]models.Release{{Title: "Show S01E01 1080p WEB", Magnet: "magnet:?xt=urn:btih:" + b32 + "&tr=c"}}, cfg); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 3 {
		t.Fatalf("rows after re-listing = %d, want 3", got)
	}
	if err := archive.db.QueryRow("SELECT title FROM archive WHERE magnet_hash=?1", hexHash).Scan(&title); err != nil || title != "Show S01E01 1080p WEB" {
		t.Fatalf("refreshed row = %q, %v", title, err)
	}
	if found, err := archive.Search("Show"); err != nil || len(found) != 1 {
		t.Fatalf("search = %v, %v; want the one torrent", found, err)
	}
	// A URL resolved to a torrent already archived just goes away.
	if err := archive.CanonicalizeTorrentURL("http://jackett/dl/1", "magnet:?xt=urn:btih:"+hexHash+"&tr=z"); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 2 {
		t.Fatalf("rows after canonicalizing a known torrent = %d, want 2", got)
	}
	if removed, err := archive.RemoveHash(strings.ToLower(b32)); err != nil || removed != 1 {
		t.Fatalf("RemoveHash by base32 = %d, %v", removed, err)
	}
}

// TestArchiveSameGUIDAnotherTorrentKeepsBoth: a feed that publishes another
// torrent under the same guid (a REPACK replacing a release) must not rename
// the old row and drop the new torrent: the magnet, not the guid, is the
// identity of a release that has one.
func TestArchiveSameGUIDAnotherTorrentKeepsBoth(t *testing.T) {
	archive := openTestArchive(t)
	release := func(title, hash string) models.Release {
		return models.Release{
			Title:        title,
			Magnet:       "magnet:?xt=urn:btih:" + hash,
			Source:       "ExtTo",
			GUID:         "https://feed.example/item/1",
			DiscoveredAt: time.Now().UTC(),
			Seeders:      -1,
			Peers:        -1,
		}
	}
	first := release("Show.S01E01.1080p.WEB", "1111111111111111111111111111111111111111")
	repack := release("Show.S01E01.REPACK.2160p.WEB", "2222222222222222222222222222222222222222")
	for _, r := range []models.Release{first, repack, first} {
		if err := archive.SaveBatch([]models.Release{r}, &Config{}); err != nil {
			t.Fatalf("SaveBatch %s: %v", r.Title, err)
		}
	}
	rows, err := archive.db.Query("SELECT title, magnet_hash FROM archive ORDER BY title")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var title, hash string
		if err := rows.Scan(&title, &hash); err != nil {
			t.Fatal(err)
		}
		got[title] = hash
	}
	if len(got) != 2 || got[first.Title] != "1111111111111111111111111111111111111111" || got[repack.Title] != "2222222222222222222222222222222222222222" {
		t.Fatalf("rows = %v, want each title with its own torrent", got)
	}
}
