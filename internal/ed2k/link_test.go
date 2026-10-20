package ed2k

import (
	"testing"
)

func TestParseFileLink(t *testing.T) {
	cases := []struct {
		raw  string
		want FileLink
	}{
		{
			raw:  "ed2k://|file|The%20Matrix%20(1999)%201080p.mkv|123456789|0123456789abcdef0123456789abcdef|/",
			want: FileLink{Name: "The Matrix (1999) 1080p.mkv", Size: 123456789, Hash: "0123456789abcdef0123456789abcdef"},
		},
		{
			raw:  "ED2K://|file|A+B.mkv|10|ABCDEF0123456789ABCDEF0123456789|/",
			want: FileLink{Name: "A+B.mkv", Size: 10, Hash: "abcdef0123456789abcdef0123456789"},
		},
	}
	for _, tc := range cases {
		got, err := ParseFileLink(tc.raw)
		if err != nil {
			t.Fatalf("ParseFileLink(%q): %v", tc.raw, err)
		}
		if got != tc.want {
			t.Fatalf("ParseFileLink(%q) = %+v, want %+v", tc.raw, got, tc.want)
		}
	}
}

func TestParseFileLinkRejects(t *testing.T) {
	bad := []string{
		"",
		"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
		"ed2k://|file|name|0|0123456789abcdef0123456789abcdef|/",
		"ed2k://|file|name|-1|0123456789abcdef0123456789abcdef|/",
		"ed2k://|file|name|10|nothex|/",
		"ed2k://|file||10|0123456789abcdef0123456789abcdef|/",
		"ed2k://|server|127.0.0.1|4661|/",
	}
	for _, raw := range bad {
		if _, err := ParseFileLink(raw); err == nil {
			t.Errorf("ParseFileLink(%q) succeeded, want error", raw)
		}
	}
}

func TestFileLinkStringRoundTrip(t *testing.T) {
	link := FileLink{Name: "Serie | S01E01 [ITA].mkv", Size: 42, Hash: "0123456789abcdef0123456789abcdef"}
	parsed, err := ParseFileLink(link.String())
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if parsed != link {
		t.Fatalf("round trip = %+v, want %+v", parsed, link)
	}
}

func TestParseServerLink(t *testing.T) {
	address, err := ParseServerLink("ed2k://|server|192.0.2.10|4661|/")
	if err != nil || address != "192.0.2.10:4661" {
		t.Fatalf("ParseServerLink = %q, %v", address, err)
	}
	if _, err := ParseServerLink("ed2k://|server|example.org|4661|/"); err == nil {
		t.Fatal("hostname accepted as server IP")
	}
}

func TestNormalizeServerList(t *testing.T) {
	got := NormalizeServerList("192.0.2.1:4661, 192.0.2.1:4661\n192.0.2.2:4661;bad")
	if len(got) != 2 || got[0] != "192.0.2.1:4661" || got[1] != "192.0.2.2:4661" {
		t.Fatalf("NormalizeServerList = %#v", got)
	}
}

func TestMatchesQuery(t *testing.T) {
	if !MatchesQuery("The.Matrix.1999.1080p.mkv", "Matrix 1080p") {
		t.Fatal("expected match")
	}
	if MatchesQuery("The.Matrix.1999.720p.mkv", "Matrix 1080p") {
		t.Fatal("unexpected match")
	}
	if !MatchesQuery("Matrix.1999.1080p.mkv", "The Matrix 1999") {
		t.Fatal("stop words must not hide a match")
	}
}

func TestMatchesQueryEpisodeVariants(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  bool
	}{
		{"Tracker.2024.S04E01.1080p.mkv", "Tracker S04E01", true},
		{"Tracker.2024.S04.E01.1080p.mkv", "Tracker S04E01", true},
		{"Tracker.2024.4x01.1080p.mkv", "Tracker S04E01", true},
		{"Tracker.2024.04x01.1080p.mkv", "Tracker S04E01", true},
		{"Tracker.2024.S04E02.1080p.mkv", "Tracker S04E01", false},
		{"Another.Show.S04E01.mkv", "Tracker S04E01", false},
	}
	for _, tc := range cases {
		if got := MatchesQuery(tc.name, tc.query); got != tc.want {
			t.Errorf("MatchesQuery(%q, %q) = %v, want %v", tc.name, tc.query, got, tc.want)
		}
	}
}

func TestQueryHasEpisode(t *testing.T) {
	if !queryHasEpisode("Tracker S04E01") || !queryHasEpisode("Tracker 4x01") {
		t.Fatal("episode query not recognized")
	}
	if queryHasEpisode("The Matrix 1999 1080p") {
		t.Fatal("year/quality must not count as an episode")
	}
}
