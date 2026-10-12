package main

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/rss"
)

// BenchmarkFeedArchiveSearch measures a search over a large archive (100k
// releases): the Archive page and the rule preview walk it on every query.
func BenchmarkFeedArchiveSearch(b *testing.B) {
	archive, err := openFeedArchive(filepath.Join(b.TempDir(), feedArchiveFile))
	if err != nil {
		b.Fatal(err)
	}
	defer archive.close()
	base := time.Unix(1_700_000_000, 0)
	for batch := 0; batch < 100; batch++ {
		items := make([]rss.Item, 1000)
		for i := range items {
			n := batch*1000 + i
			items[i] = rss.Item{Title: fmt.Sprintf("Show.Number.%d.S%02dE%02d.1080p.WEB-DL.ITA.ENG-GRP", n%500, n%9+1, n%20+1), Link: fmt.Sprintf("https://example.org/%d.torrent", n), Size: int64(n) << 20, Seeders: n % 300}
		}
		if _, err := archive.record("feed", items, base.Add(time.Duration(batch)*time.Minute)); err != nil {
			b.Fatal(err)
		}
	}
	q := parseArchiveQuery("number 42 1080p -cam", "", false)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := archive.search(q, "new", 1, archivePageSize); err != nil {
			b.Fatal(err)
		}
	}
}
