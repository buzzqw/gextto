package gextto

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func selectionRelease(id int, episodes []int64, score int64, remux bool) models.Release {
	series := "Example"
	season := int64(1)
	release := models.Release{
		Title:        fmt.Sprintf("r%d", id),
		Magnet:       fmt.Sprintf("m%d", id),
		Kind:         "series",
		Series:       &series,
		Season:       &season,
		EpisodeRange: episodes,
		IsPack:       len(episodes) != 1,
		SizeBytes:    score, // carries the test score, see selectionScore
	}
	if remux {
		release.Quality.Source = "remux"
	}
	return release
}

func selectionScore(release *models.Release) int64 { return release.SizeBytes }

func selectRun(releases []models.Release) string {
	var best []models.Release
	for _, release := range releases {
		best, _ = mergeSeriesCandidate(best, release, selectionScore(&release), selectionScore)
	}
	names := make([]string, 0, len(best))
	for _, release := range best {
		names = append(names, release.Title)
	}
	slices.Sort(names)
	return strings.Join(names, ",")
}

// The regression of L2: an episode and a same-score season pack produced a
// different selection depending on which one arrived first.
func TestCandidateSelectionEpisodeVersusPackAtEqualScore(t *testing.T) {
	episode := selectionRelease(1, []int64{1}, 100, false)
	pack := selectionRelease(2, []int64{0}, 100, false)
	first := selectRun([]models.Release{episode, pack})
	second := selectRun([]models.Release{pack, episode})
	if first != "r2" || second != "r2" {
		t.Fatalf("episode-first = %q, pack-first = %q; want only the pack in both", first, second)
	}
}

// Shuffling the same input must not change the selection.
func TestCandidateSelectionIgnoresArrivalOrder(t *testing.T) {
	ranges := [][]int64{{1}, {2}, {3}, {1, 2}, {2, 3}, {1, 2, 3}, {0}}
	scores := []int64{50, 100}
	random := rand.New(rand.NewSource(1))
	for trial := 0; trial < 2000; trial++ {
		count := 2 + random.Intn(4)
		input := make([]models.Release, 0, count)
		for i := 0; i < count; i++ {
			input = append(input, selectionRelease(i, ranges[random.Intn(len(ranges))],
				scores[random.Intn(len(scores))], random.Intn(4) == 0))
		}
		// Identical range and identical quality is a true tie: the first wins,
		// which is legitimately order dependent. Skip such inputs.
		if hasExactTie(input) {
			continue
		}
		want := selectRun(input)
		for shuffle := 0; shuffle < 10; shuffle++ {
			permuted := slices.Clone(input)
			random.Shuffle(len(permuted), func(i, j int) { permuted[i], permuted[j] = permuted[j], permuted[i] })
			if got := selectRun(permuted); got != want {
				t.Fatalf("selection depends on order:\n input %s -> %s\n input %s -> %s",
					describeSelection(input), want, describeSelection(permuted), got)
			}
		}
	}
}

func hasExactTie(releases []models.Release) bool {
	for i := range releases {
		for j := i + 1; j < len(releases); j++ {
			a, b := &releases[i], &releases[j]
			sameCover := slices.Equal(a.EpisodeRange, b.EpisodeRange) ||
				(hasCompleteRange(a) && hasCompleteRange(b))
			if sameCover && a.SizeBytes == b.SizeBytes && a.Quality.IsRemux() == b.Quality.IsRemux() {
				return true
			}
		}
	}
	return false
}

func describeSelection(releases []models.Release) string {
	parts := make([]string, 0, len(releases))
	for _, release := range releases {
		parts = append(parts, fmt.Sprintf("%s%v/%d/remux=%v", release.Title, release.EpisodeRange, release.SizeBytes, release.Quality.IsRemux()))
	}
	return strings.Join(parts, " ")
}

func TestSettingTruthyIsUniform(t *testing.T) {
	for _, value := range []string{"yes", "YES", " true ", "True", "1", "on", "On"} {
		if !settingTruthy(value) {
			t.Fatalf("settingTruthy(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"", "no", "false", "0", "off", "enabled"} {
		if settingTruthy(value) {
			t.Fatalf("settingTruthy(%q) = true, want false", value)
		}
	}
}
