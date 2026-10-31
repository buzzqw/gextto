package rss

import (
	"strings"
	"testing"
	"time"
)

func TestParseReadsFreeleechTristate(t *testing.T) {
	body := `<?xml version="1.0"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>
<item><title>Free.Factor.S01E01.1080p</title><enclosure url="magnet:?xt=urn:btih:1111111111111111111111111111111111111111" length="100000000"/><torznab:attr name="downloadvolumefactor" value="0"/></item>
<item><title>Paid.Factor.S01E02.1080p</title><enclosure url="magnet:?xt=urn:btih:2222222222222222222222222222222222222222" length="100000000"/><torznab:attr name="downloadvolumefactor" value="1"/></item>
<item><title>Unknown.Known.S01E03.1080p</title><enclosure url="magnet:?xt=urn:btih:3333333333333333333333333333333333333333" length="100000000"/><torznab:attr name="freeleechknown" value="0"/><torznab:attr name="downloadvolumefactor" value="0"/></item>
<item><title>Free.Known.S01E04.1080p</title><enclosure url="magnet:?xt=urn:btih:4444444444444444444444444444444444444444" length="100000000"/><torznab:attr name="freeleechknown" value="1"/><torznab:attr name="freeleech" value="1"/></item>
<item><title>Nothing.Stated.S01E05.1080p</title><enclosure url="magnet:?xt=urn:btih:5555555555555555555555555555555555555555" length="100000000"/></item>
</channel></rss>`
	items, err := Parse(strings.NewReader(body), "feed")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("got %d items, want 5", len(items))
	}
	want := []struct {
		free  bool
		known bool
	}{
		{true, true},
		{false, true},
		{false, false},
		{true, true},
		{false, false},
	}
	for i, item := range items {
		if item.Freeleech != want[i].free || item.FreeleechKnown != want[i].known {
			t.Errorf("%s: freeleech = (%v, known %v), want (%v, known %v)",
				item.Title, item.Freeleech, item.FreeleechKnown, want[i].free, want[i].known)
		}
	}
}

func TestCriteriaOnlyFreeleechRejectsUnknownAndPaid(t *testing.T) {
	criteria := Criteria{OnlyFreeleech: true}
	for _, item := range []struct {
		name  string
		state Item
	}{
		{"unknown", Item{Title: "Show S01E01 1080p"}},
		{"known paid", Item{Title: "Show S01E01 1080p", FreeleechKnown: true}},
	} {
		if criteria.Match(item.state, time.Now()) {
			t.Errorf("%s item matched a freeleech-only rule", item.name)
		}
	}
	if !criteria.Match(Item{Title: "Show S01E01 1080p", Freeleech: true, FreeleechKnown: true}, time.Now()) {
		t.Fatal("a known freeleech item did not match")
	}
}
