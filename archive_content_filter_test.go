package gextto

import (
	"fmt"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

// TestBrowseContentFilteredPageKeepsTheRows guards the page that lists the
// archived releases matching a content filter. The scan reads only id and title
// of every row (the magnet column is over 100 MB on a full archive) and then
// loads the visible page separately, so the test fixes what the rows must still
// carry: the entry fields, the parsed release and the pagination count.
func TestBrowseContentFilteredPageKeepsTheRows(t *testing.T) {
	archive := openTestArchive(t)
	releases := []models.Release{
		{Title: "Show S01E01 1080p ita", Magnet: "magnet:?xt=urn:btih:" + fmt.Sprintf("%040x", 1), Source: "feed-one", Kind: "series", DiscoveredAt: time.Now().UTC(), Seeders: -1, Peers: -1},
		{Title: "Show S01E02 1080p eng", Magnet: "magnet:?xt=urn:btih:" + fmt.Sprintf("%040x", 2), Source: "feed-one", Kind: "series", DiscoveredAt: time.Now().UTC(), Seeders: -1, Peers: -1},
		{Title: "Film 2020 1080p ita", Magnet: "magnet:?xt=urn:btih:" + fmt.Sprintf("%040x", 3), Source: "feed-two", Kind: "movie", DiscoveredAt: time.Now().UTC(), Seeders: -1, Peers: -1},
	}
	if err := archive.SaveBatch(releases, &Config{}); err != nil {
		t.Fatalf("SaveBatch: %v", err)
	}

	page, err := archive.BrowseContentFilteredPage("", []string{"ita"}, 1, 1)
	if err != nil {
		t.Fatalf("BrowseContentFilteredPage: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("Total = %d, want 2 (the two Italian releases)", page.Total)
	}
	if page.Pages != 2 {
		t.Fatalf("Pages = %d, want 2 with a limit of 1", page.Pages)
	}
	if len(page.Items) != 1 {
		t.Fatalf("Items = %d, want the single row of page 1", len(page.Items))
	}
	assertContentFilteredEntry(t, page.Items[0])

	second, err := archive.BrowseContentFilteredPage("", []string{"ita"}, 2, 1)
	if err != nil {
		t.Fatalf("BrowseContentFilteredPage(page 2): %v", err)
	}
	if len(second.Items) != 1 {
		t.Fatalf("page 2 Items = %d, want 1", len(second.Items))
	}
	if second.Items[0].ID == page.Items[0].ID {
		t.Fatalf("page 2 repeats the row of page 1 (%d)", second.Items[0].ID)
	}
	assertContentFilteredEntry(t, second.Items[0])
}

// assertContentFilteredEntry checks that a row loaded after the id/title scan
// carries everything the page and the deletion need.
func assertContentFilteredEntry(t *testing.T, entry ArchiveEntry) {
	t.Helper()
	if entry.ID <= 0 {
		t.Fatalf("ID = %d, want a positive id", entry.ID)
	}
	if entry.Title == "" {
		t.Fatal("Title is empty")
	}
	if entry.Magnet == "" {
		t.Fatalf("%q: Magnet is empty", entry.Title)
	}
	if entry.Source == "" || entry.Source == "archive" {
		t.Fatalf("%q: Source = %q, want the stored source", entry.Title, entry.Source)
	}
	if entry.AddedAt == "" {
		t.Fatalf("%q: AddedAt is empty", entry.Title)
	}
	if entry.Release == nil {
		t.Fatalf("%q: Release was not parsed", entry.Title)
	}
}
