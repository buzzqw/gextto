package gextto

import (
	"fmt"
	"strings"
	"testing"
)

// benchFeedBody builds a Torznab-style response with count items, the shape
// indexarr and the other indexers return for one page of a search.
func benchFeedBody(count int) string {
	var out strings.Builder
	out.WriteString(`<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel><title>indexer</title>`)
	for i := 0; i < count; i++ {
		fmt.Fprintf(&out, `<item><title>Example.Show.S01E%02d.2160p.WEB-DL.DDP5.1.HDR.H.265-GRUPPO.mkv</title>`+
			`<pubDate>Mon, 05 Oct 2026 10:17:44 +0000</pubDate>`+
			`<guid isPermaLink="false">0123456789abcdef0123456789abcdef012345%02d</guid>`+
			`<link>magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef012345%02d&amp;dn=Example.Show.S01E%02d</link>`+
			`<enclosure url="magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef012345%02d&amp;dn=Example.Show" length="1234567890" type="application/x-bittorrent"/>`+
			`<description>Example Show S01E%02d 2160p WEB-DL DDP5.1 HDR H.265 | Size: 2.5 GB | Seeders: 12 | Leechers: 3</description>`+
			`<torznab:attr name="seeders" value="12"/><torznab:attr name="peers" value="3"/>`+
			`<torznab:attr name="size" value="2684354560"/><torznab:attr name="category" value="5000"/>`+
			`<torznab:attr name="infohash" value="0123456789abcdef0123456789abcdef012345%02d"/></item>`,
			i%100, i%100, i%100, i%100, i%100, i%100, i%100)
	}
	out.WriteString(`</channel></rss>`)
	return out.String()
}

func BenchmarkParseFeedBody(b *testing.B) {
	body := benchFeedBody(100)
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := parseFeedBody(body, "bench"); err != nil {
			b.Fatal(err)
		}
	}
}
