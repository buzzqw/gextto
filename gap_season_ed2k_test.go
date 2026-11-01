package gextto

import (
	"fmt"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// seasonED2KRelease builds a copy found by a season search: the episode and the
// resolution are all the distribution looks at.
func seasonED2KRelease(episode int64, resolution string) models.Release {
	season := int64(2)
	name := "Walker"
	return models.Release{
		Title:   fmt.Sprintf("Walker 2x%02d %s", episode, resolution),
		Kind:    "series",
		Series:  &name,
		Season:  &season,
		Episode: &episode,
		Quality: models.Quality{Resolution: resolution, Language: "ita", Languages: []string{"ita"}},
	}
}

// TestDistributeSeasonED2KFillsEveryGapOfTheSeason pins why one eD2k query
// answers for a whole season: the copies for the episodes the pass did not pick
// are used too, because the query already paid for them.
func TestDistributeSeasonED2KFillsEveryGapOfTheSeason(t *testing.T) {
	cfg := DefaultConfig()
	series := &SeriesConfig{Name: "Walker", Quality: "720p-1080p", Language: "ita"}
	candidate := gapTarget{Series: "Walker", Season: 2, Episode: 5}
	candidateIndex := map[gapTarget]int{candidate: 0}
	foundResults := make([][]models.Release, 1)
	lastResorts := make([][]models.Release, 1)

	found := []models.Release{
		seasonED2KRelease(5, "1080p"), // the gap this pass picked
		seasonED2KRelease(6, "1080p"), // another gap of the same season
		seasonED2KRelease(9, "1080p"), // not a gap any more
	}
	extra, accepted := distributeSeasonED2K(&cfg, series, 2, []int64{5, 6}, found, candidateIndex, foundResults, lastResorts)

	if accepted != 2 {
		t.Fatalf("accepted = %d, want 2 (one candidate, one extra)", accepted)
	}
	if len(foundResults[0]) != 1 || *foundResults[0][0].Episode != 5 {
		t.Fatalf("candidate results = %+v, want episode 5", foundResults[0])
	}
	if len(extra) != 1 || *extra[0].Episode != 6 {
		t.Fatalf("extra = %+v, want episode 6", extra)
	}
	if len(lastResorts[0]) != 0 {
		t.Fatalf("last resorts = %+v, want none", lastResorts[0])
	}
}

// TestDistributeSeasonED2KKeepsTheLastResortForTheCandidatesOnly: a copy
// outside the resolution is a last resort only for a gap the pass picked. The
// others are not started this cycle, so promising them one would go nowhere.
func TestDistributeSeasonED2KKeepsTheLastResortForTheCandidatesOnly(t *testing.T) {
	cfg := DefaultConfig()
	series := &SeriesConfig{Name: "Walker", Quality: "720p-1080p", Language: "ita"}
	candidate := gapTarget{Series: "Walker", Season: 2, Episode: 5}
	candidateIndex := map[gapTarget]int{candidate: 0}
	foundResults := make([][]models.Release, 1)
	lastResorts := make([][]models.Release, 1)

	found := []models.Release{
		seasonED2KRelease(5, "480p"), // outside the profile, and a candidate
		seasonED2KRelease(6, "480p"), // outside the profile, not a candidate
	}
	extra, accepted := distributeSeasonED2K(&cfg, series, 2, []int64{5, 6}, found, candidateIndex, foundResults, lastResorts)

	if accepted != 0 {
		t.Fatalf("accepted = %d, want 0", accepted)
	}
	if len(extra) != 0 {
		t.Fatalf("extra = %+v, want none", extra)
	}
	if len(foundResults[0]) != 0 {
		t.Fatalf("candidate results = %+v, want none", foundResults[0])
	}
	if len(lastResorts[0]) != 1 || *lastResorts[0][0].Episode != 5 || !lastResorts[0][0].LastResort {
		t.Fatalf("last resorts = %+v, want episode 5 marked", lastResorts[0])
	}
}

// TestDistributeSeasonED2KIgnoresOtherEpisodes guards the distribution boundary:
// a copy for an episode that is not a gap of this season must not reach the
// results.
func TestDistributeSeasonED2KIgnoresOtherEpisodes(t *testing.T) {
	cfg := DefaultConfig()
	series := &SeriesConfig{Name: "Walker", Quality: "720p-1080p", Language: "ita"}
	candidateIndex := map[gapTarget]int{}
	foundResults := make([][]models.Release, 0)
	lastResorts := make([][]models.Release, 0)

	otherSeason := seasonED2KRelease(5, "1080p")
	season := int64(3)
	otherSeason.Season = &season

	extra, accepted := distributeSeasonED2K(&cfg, series, 2, []int64{5}, []models.Release{otherSeason}, candidateIndex, foundResults, lastResorts)
	if accepted != 0 || len(extra) != 0 {
		t.Fatalf("accepted = %d extra = %+v, want nothing outside season 2", accepted, extra)
	}
}

// TestDistributeSeasonED2KTakesACopyWithoutAResolutionAsLastResort is the
// Walker case: the only copy of an episode is named "ITA DLMux x264" with no
// resolution at all, so it reaches the profile decision and is kept as a last
// resort instead of being discarded for not declaring its quality.
func TestDistributeSeasonED2KTakesACopyWithoutAResolutionAsLastResort(t *testing.T) {
	cfg := DefaultConfig()
	series := &SeriesConfig{Name: "Walker", Quality: "720p-1080p", Language: "ita"}
	candidate := gapTarget{Series: "Walker", Season: 2, Episode: 5}
	candidateIndex := map[gapTarget]int{candidate: 0}
	foundResults := make([][]models.Release, 1)
	lastResorts := make([][]models.Release, 1)

	found := []models.Release{seasonED2KRelease(5, "")}
	_, accepted := distributeSeasonED2K(&cfg, series, 2, []int64{5}, found, candidateIndex, foundResults, lastResorts)

	if accepted != 0 {
		t.Fatalf("accepted = %d, want 0 (it is outside the profile)", accepted)
	}
	if len(lastResorts[0]) != 1 || !lastResorts[0][0].LastResort {
		t.Fatalf("last resorts = %+v, want the copy without a resolution", lastResorts[0])
	}
}
