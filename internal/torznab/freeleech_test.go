package torznab

import (
	"strings"
	"testing"
)

func TestParseReadsFreeleechAttributes(t *testing.T) {
	body := `<?xml version="1.0"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>
<item><title>Free.Factor.S01E01.1080p</title><enclosure url="magnet:?xt=urn:btih:1111111111111111111111111111111111111111" length="100000000"/><torznab:attr name="downloadvolumefactor" value="0"/></item>
<item><title>Paid.Factor.S01E02.1080p</title><enclosure url="magnet:?xt=urn:btih:2222222222222222222222222222222222222222" length="100000000"/><torznab:attr name="downloadvolumefactor" value="1"/></item>
<item><title>Unknown.Known.S01E03.1080p</title><enclosure url="magnet:?xt=urn:btih:3333333333333333333333333333333333333333" length="100000000"/><torznab:attr name="freeleechknown" value="0"/><torznab:attr name="downloadvolumefactor" value="0"/></item>
<item><title>Free.Known.S01E04.1080p</title><enclosure url="magnet:?xt=urn:btih:4444444444444444444444444444444444444444" length="100000000"/><torznab:attr name="freeleechknown" value="1"/><torznab:attr name="freeleech" value="1"/></item>
</channel></rss>`
	items, err := Parse(strings.NewReader(body), "indexarr")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("got %d items, want 4", len(items))
	}
	want := []struct {
		free  bool
		known bool
	}{
		{true, true},
		{false, true},
		{false, false},
		{true, true},
	}
	for i, item := range items {
		if item.Freeleech != want[i].free || item.FreeleechKnown != want[i].known {
			t.Errorf("%s: freeleech = (%v, known %v), want (%v, known %v)",
				item.Title, item.Freeleech, item.FreeleechKnown, want[i].free, want[i].known)
		}
	}
}

func TestParseProwlarrReadsFreeleechFlags(t *testing.T) {
	body := `[
 {"title":"Free.Movie.2026.1080p","size":100000000,"seeders":4,"protocol":"torrent","magnetUrl":"magnet:?xt=urn:btih:1111111111111111111111111111111111111111","indexerFlags":["freeleech"]},
 {"title":"Paid.Movie.2026.1080p","size":100000000,"seeders":4,"protocol":"torrent","magnetUrl":"magnet:?xt=urn:btih:2222222222222222222222222222222222222222","indexerFlags":["scene"]},
 {"title":"Factor.Movie.2026.1080p","size":100000000,"seeders":4,"protocol":"torrent","magnetUrl":"magnet:?xt=urn:btih:3333333333333333333333333333333333333333","downloadVolumeFactor":0}
]`
	items, err := ParseProwlarr(strings.NewReader(body), "prowlarr")
	if err != nil {
		t.Fatalf("ParseProwlarr: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}
	if !items[0].Freeleech || !items[0].FreeleechKnown {
		t.Error("freeleech flag not read as known free")
	}
	if items[1].Freeleech || items[1].FreeleechKnown {
		t.Error("a scene-only flag must stay unknown, not become paid")
	}
	if !items[2].Freeleech || !items[2].FreeleechKnown {
		t.Error("downloadVolumeFactor 0 not read as known free")
	}
}
