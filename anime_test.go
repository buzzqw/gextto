package gextto

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

func TestParseAbsoluteEpisode(t *testing.T) {
	cases := map[string]struct {
		series string
		number int64
	}{
		"[SubsPlease] One Piece - 1071 (1080p) [ABCDEF12].mkv":        {"One Piece", 1071},
		"[Erai-raws] Jujutsu Kaisen - 05v2 [720p][Multiple Subtitle]": {"Jujutsu Kaisen", 5},
		"Frieren - 12 (1080p)":                   {"Frieren", 12},
		"One Piece Ep 1071 SUB ITA":              {"One Piece", 1071},
		"One.Piece.1071.SUB.ITA.1080p.WEB-DL":    {"One Piece", 1071},
		"[Fansub] Detective Conan Episodio 1120": {"Detective Conan", 1120},
	}
	for title, want := range cases {
		release := ParseRelease(title, "magnet:?xt=urn:btih:"+"0123456789abcdef0123456789abcdef01234567", "test")
		if release == nil || release.AbsoluteEpisode == nil || release.AbsoluteSeries == nil {
			t.Errorf("%q: no absolute number parsed", title)
			continue
		}
		if *release.AbsoluteSeries != want.series || *release.AbsoluteEpisode != want.number {
			t.Errorf("%q: got %q %d, want %q %d", title, *release.AbsoluteSeries, *release.AbsoluteEpisode, want.series, want.number)
		}
		if release.Kind != "movie" || release.Season != nil {
			t.Errorf("%q: must stay unclassified until matched to an anime series", title)
		}
	}
	for _, title := range []string{"Dune Part Two 2024 ITA 1080p", "Show S01E02 1080p", "Blade Runner 2049 (2017) 1080p"} {
		release := ParseRelease(title, "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567", "test")
		if release != nil && release.AbsoluteEpisode != nil && release.Season == nil {
			t.Errorf("%q: unexpected absolute number %d", title, *release.AbsoluteEpisode)
		}
	}
}

func TestAnimeNumberingMapsAcrossSeasons(t *testing.T) {
	numbering := newAnimeNumbering(map[int64]int64{0: 5, 1: 12, 2: 12, 3: 24})
	for absolute, want := range map[int64][2]int64{1: {1, 1}, 12: {1, 12}, 13: {2, 1}, 30: {3, 6}, 60: {3, 36}} {
		season, episode, ok := numbering.seasonEpisode(absolute)
		if !ok || season != want[0] || episode != want[1] {
			t.Errorf("absolute %d → S%dE%d, want S%dE%d", absolute, season, episode, want[0], want[1])
		}
	}
	if absolute, ok := numbering.absolute(3, 6); !ok || absolute != 30 {
		t.Errorf("S03E06 → %d, want 30", absolute)
	}
	unknown := newAnimeNumbering(nil)
	if season, episode, ok := unknown.seasonEpisode(1071); !ok || season != 1 || episode != 1071 {
		t.Errorf("without TMDB data the number is an episode of season 1, got S%dE%d", season, episode)
	}
}

func TestResolveAnimeReleasesOnlyForAnimeSeries(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Series = []SeriesConfig{
		{Name: "One Piece", Enabled: true, Anime: true},
		{Name: "Frieren", Enabled: true},
	}
	parse := func(title string) models.Release {
		return *ParseRelease(title, "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567", "test")
	}
	releases := resolveAnimeReleases(t.Context(), &cfg, []models.Release{
		parse("[SubsPlease] One Piece - 1071 (1080p)"),
		parse("[SubsPlease] Frieren - 12 (1080p)"),
	})
	anime := releases[0]
	if anime.Kind != "series" || anime.Series == nil || *anime.Series != "One Piece" ||
		*anime.Season != 1 || *anime.Episode != 1071 || anime.IsPack {
		t.Fatalf("anime release not mapped: %+v", anime)
	}
	if releases[1].Kind != "movie" {
		t.Fatal("a series not marked as anime must not be touched")
	}
}

// The gap filling reads the local release archive too. A row whose only source
// is a `.torrent` link on a switched-off indexer cannot be downloaded, so it
// must never become a candidate.
func TestSearchArchiveForEpisodeSkipsLinksOfDisabledIndexers(t *testing.T) {
	archive := openTestArchive(t)
	ctx := context.Background()
	disabled := "http://192.168.1.161:9117/dl/1.torrent?jackett_api=1&path=a"
	enabled := "http://192.168.1.161:8788/dl/2.torrent?apikey=b"
	activeFeedLink := "https://rss.example.org/rss/download/2/b4ae63ba/Show.S01E02.torrent"
	removedFeedLink := "https://rss.old.example.org/rss/download/1/a4ae63ba/Show.S01E02.torrent"
	activeFeed := "https://rss.example.org/rss"
	removedFeed := "https://rss.old.example.org/rss"
	releases := []models.Release{
		{Title: "Show S01E02 1080p", Source: "Jackett RSS - AnIndexer", TorrentURL: &disabled,
			Kind: "series", DiscoveredAt: time.Now().UTC(), Seeders: -1, Peers: -1},
		{Title: "Show S01E02 720p", Source: "indexarr:ExtTo", TorrentURL: &enabled,
			Kind: "series", DiscoveredAt: time.Now().UTC(), Seeders: -1, Peers: -1},
		{Title: "Show S01E02 2160p", Source: "a feed",
			Magnet: "magnet:?xt=urn:btih:" + strings.Repeat("c", 40),
			Kind:   "series", DiscoveredAt: time.Now().UTC(), Seeders: -1, Peers: -1},
		{Title: "Show S01E02 1080p", Source: "ExampleFeed", FeedURL: activeFeed, TorrentURL: &activeFeedLink,
			Kind: "series", DiscoveredAt: time.Now().UTC(), Seeders: -1, Peers: -1},
		{Title: "Show S01E02 1080p", Source: "OldFeed", FeedURL: removedFeed, TorrentURL: &removedFeedLink,
			Kind: "series", DiscoveredAt: time.Now().UTC(), Seeders: -1, Peers: -1},
	}
	if err := archive.SaveBatch(releases, &Config{}); err != nil {
		t.Fatalf("SaveBatch: %v", err)
	}
	cfg := &Config{
		FeedURLs: []string{activeFeed},
		Indexers: []IndexerConfig{
			{Name: "jackett", URL: "http://192.168.1.161:9117", Enabled: false},
			{Name: "indexarr", URL: "http://192.168.1.161:8788", Enabled: true},
		},
	}
	items, err := searchArchiveForEpisode(ctx, cfg, archive, nil, "Show", 1, 2)
	if err != nil {
		t.Fatalf("searchArchiveForEpisode: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3: %+v", len(items), items)
	}
	kept := false
	for _, item := range items {
		if strings.Contains(item.Magnet, "192.168.1.161:9117") {
			t.Fatalf("a listing of a disabled indexer was proposed: %+v", item)
		}
		if strings.Contains(item.Magnet, "rss.old.example.org") {
			t.Fatalf("a listing of a removed feed was proposed: %+v", item)
		}
		if item.Magnet == activeFeedLink {
			kept = true
		}
	}
	if !kept {
		t.Fatal("the listing of the feed still in Sources was not proposed")
	}
}
