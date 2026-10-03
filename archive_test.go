package gextto

import (
	"fmt"
	"path/filepath"
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
	if _, err := archive.db.Exec(`INSERT INTO archive(title,magnet,source,added_at) VALUES
		('Adult Porno Release 1','magnet:porno-1','feed','2026-01-01'),
		('Adult Porno Release 2','magnet:porno-2','feed','2026-01-02'),
		('Family Movie','magnet:family','feed','2026-01-03')`); err != nil {
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
