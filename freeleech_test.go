package gextto

import (
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// TestTorznabFreeleechFlags covers the three spellings an indexer uses for a
// free download and the explicit "not known" marker indexarr sends so an
// unknown release is not mistaken for a free one.
func TestTorznabFreeleechFlags(t *testing.T) {
	for _, test := range []struct {
		name  string
		attrs string
		want  bool
	}{
		{"volume factor zero", `<torznab:attr name="downloadvolumefactor" value="0"/>`, true},
		{"volume factor paid", `<torznab:attr name="downloadvolumefactor" value="1"/>`, false},
		{"explicit freeleech", `<torznab:attr name="freeleech" value="1"/><torznab:attr name="freeleechknown" value="1"/>`, true},
		{"explicit paid", `<torznab:attr name="freeleech" value="0"/><torznab:attr name="freeleechknown" value="1"/>`, false},
		{"unknown", `<torznab:attr name="downloadvolumefactor" value="0"/><torznab:attr name="freeleechknown" value="0"/>`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := `<rss xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel><item>` +
				`<title>Some.Show.S01E01.1080p.WEB-DL</title>` +
				`<guid>some-show-s01e01</guid>` +
				`<enclosure url="magnet:?xt=urn:btih:0123456789012345678901234567890123456789" length="900000000" type="application/x-bittorrent"/>` +
				test.attrs + `</item></channel></rss>`
			releases, err := parse_feed_body(body, "test")
			if err != nil {
				t.Fatalf("parse_feed_body: %v", err)
			}
			if len(releases) != 1 {
				t.Fatalf("releases = %d, want 1", len(releases))
			}
			if releases[0].Freeleech != test.want {
				t.Fatalf("freeleech = %v, want %v", releases[0].Freeleech, test.want)
			}
		})
	}
}

// TestProwlarrFreeleechFlags reads the named flags Prowlarr exposes.
func TestProwlarrFreeleechFlags(t *testing.T) {
	for _, test := range []struct {
		flags []any
		want  bool
	}{
		{[]any{"freeleech"}, true},
		{[]any{"neutralleech"}, true},
		{[]any{"freeleech", "scene"}, true},
		{[]any{"scene"}, false},
		{nil, false},
	} {
		item := map[string]any{"indexerFlags": test.flags}
		if got := prowlarrFreeleech(item); got != test.want {
			t.Errorf("prowlarrFreeleech(%v) = %v, want %v", test.flags, got, test.want)
		}
	}
}

// TestFreeleechBonusDoesNotEnterStoredScore checks that freeleech is a
// preference among candidates, never a quality upgrade: the stored score is
// unchanged and only AcquisitionScore receives the bonus.
func TestFreeleechBonusDoesNotEnterStoredScore(t *testing.T) {
	release := models.Release{
		Title:     "Some.Show.S01E01.1080p.WEB-DL",
		Source:    "test",
		SizeBytes: 900_000_000,
		Seeders:   20,
		Quality:   models.Quality{Resolution: "1080p"},
	}
	cfg := &Config{Settings: map[string]string{"score_bonus_freeleech": "150"}}
	plain := cfg.ReleaseScore(&release)
	cfg.Settings["score_bonus_freeleech"] = "0"
	base := cfg.AcquisitionScore(&release)
	release.Freeleech = true
	if got := cfg.ReleaseScore(&release); got != plain {
		t.Fatalf("stored score changed with freeleech: %d -> %d", plain, got)
	}
	cfg.Settings["score_bonus_freeleech"] = "150"
	got := cfg.AcquisitionScore(&release)
	if got != base+150 {
		t.Fatalf("acquisition score = %d, want %d (base %d plus the bonus)", got, base+150, base)
	}
	cfg.Settings["score_bonus_freeleech"] = "0"
	if got := cfg.AcquisitionScore(&release); got != base {
		t.Fatalf("acquisition score with the bonus off = %d, want %d", got, base)
	}
}
