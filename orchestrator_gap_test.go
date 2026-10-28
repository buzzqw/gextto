package gextto

import (
	"time"

	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func TestReleaseMatchesConfiguredEpisodeRequiresExactIdentity(t *testing.T) {
	series := &SeriesConfig{Name: "Example Show", Aliases: []string{"Example Alias"}, Enabled: true}
	name := "Example Alias"
	season := int64(2)
	episode := int64(3)
	release := &models.Release{
		Kind:         "series",
		Series:       &name,
		Season:       &season,
		Episode:      &episode,
		EpisodeRange: []int64{3},
	}
	if !releaseMatchesConfiguredEpisode(release, series, 2, 3) {
		t.Fatal("matching alias and episode was rejected")
	}
	if releaseMatchesConfiguredEpisode(release, series, 2, 4) {
		t.Fatal("different episode was accepted as a gap hit")
	}
	other := "Different Show"
	release.Series = &other
	if releaseMatchesConfiguredEpisode(release, series, 2, 3) {
		t.Fatal("different series was accepted as a gap hit")
	}
}

func TestCompleteSeasonPackCoversEveryGapInItsSeason(t *testing.T) {
	series := "Example Show"
	season := int64(2)
	pack := &models.Release{Series: &series, Season: &season, EpisodeRange: []int64{0}}
	targets := map[gapTarget]struct{}{
		{Series: series, Season: season, Episode: 1}: {},
		{Series: series, Season: season, Episode: 3}: {},
		{Series: series, Season: 3, Episode: 1}:      {},
	}
	if got := gapEpisodesForRelease(pack, targets); len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("complete pack covers %v, want [1 3]", got)
	}
	configured := &SeriesConfig{Name: series, Enabled: true}
	pack.Kind = "series"
	if !releaseMatchesConfiguredEpisode(pack, configured, season, 3) {
		t.Fatal("complete pack does not match its season gap")
	}
}

func TestPromoteTorrentURLMagnetKeepsDirectMagnetAcquisition(t *testing.T) {
	url := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Example"
	release := &models.Release{Title: "Example", TorrentURL: &url}
	if !promoteTorrentURLMagnet(release) {
		t.Fatal("magnet stored as a torrent URL was not promoted")
	}
	if release.TorrentURL != nil || release.Magnet == "" {
		t.Fatalf("release = %+v, want magnet with no torrent URL", release)
	}
}

func TestDeepPassSearchesTheGapsWaitingLongestFirst(t *testing.T) {
	now := time.Now().UTC()
	series := func(name string) *SeriesConfig { return &SeriesConfig{Name: name, Enabled: true} }
	waiting := []waitingGap{
		{candidate: liveCandidate{Series: series("9-1-1"), Season: 10, Episode: 1}, last: now.Add(-24 * time.Hour), searched: true},
		{candidate: liveCandidate{Series: series("Agenzia Roman"), Season: 2, Episode: 10}, last: now.Add(-30 * time.Hour), searched: true},
		{candidate: liveCandidate{Series: series("FBI: International"), Season: 3, Episode: 1}},
		{candidate: liveCandidate{Series: series("New Tricks"), Season: 1, Episode: 1}, last: now.Add(-20 * 24 * time.Hour), searched: true},
	}
	orderWaitingGaps(waiting)
	got := []string{}
	for _, entry := range waiting {
		got = append(got, entry.candidate.Series.Name)
	}
	want := []string{"FBI: International", "New Tricks", "Agenzia Roman", "9-1-1"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}
