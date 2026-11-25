package gextto

import (
	"sort"
	"strings"

	"github.com/buzzqw/gextto/internal/models"
)

const (
	titlePriorityHigh   = "high"
	titlePriorityNormal = "normal"
	titlePriorityLow    = "low"
)

// normalizeTitlePriority keeps the stored vocabulary small and predictable.
// The empty value of an older configuration is the normal priority.
func normalizeTitlePriority(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", titlePriorityNormal:
		return titlePriorityNormal, true
	case titlePriorityHigh:
		return titlePriorityHigh, true
	case titlePriorityLow:
		return titlePriorityLow, true
	default:
		return "", false
	}
}

func storedTitlePriority(raw string) string {
	if priority, ok := normalizeTitlePriority(raw); ok {
		return priority
	}
	return titlePriorityNormal
}

func titlePriorityRank(priority string) int {
	switch priority {
	case titlePriorityHigh:
		return 0
	case titlePriorityLow:
		return 2
	default:
		return 1
	}
}

func releaseTitlePriority(cfg *Config, release *models.Release) string {
	if cfg == nil || release == nil {
		return titlePriorityNormal
	}
	if release.Kind == "series" {
		name := ""
		if release.Series != nil {
			name = *release.Series
		}
		if series := cfg.FindSeriesMatch(name, release.Season); series != nil {
			return series.Priority
		}
		return titlePriorityNormal
	}
	if movie := cfg.FindMovieMatchForRelease(release, false); movie != nil {
		return movie.Priority
	}
	return titlePriorityNormal
}

// sortReleasesByTitlePriority orders the start queue, not the quality choice:
// every candidate has already passed the normal filters and per-title merge.
// Within one priority, gaps go first, then the stronger acquisition score.
func sortReleasesByTitlePriority(cfg *Config, releases []models.Release, gaps map[gapTarget]struct{}) {
	sort.SliceStable(releases, func(i, j int) bool {
		left, right := &releases[i], &releases[j]
		leftRank := titlePriorityRank(releaseTitlePriority(cfg, left))
		rightRank := titlePriorityRank(releaseTitlePriority(cfg, right))
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if len(gaps) > 0 {
			leftGaps := gapEpisodesForRelease(left, gaps)
			rightGaps := gapEpisodesForRelease(right, gaps)
			if len(leftGaps) != len(rightGaps) {
				return len(leftGaps) > len(rightGaps)
			}
		}
		leftScore, rightScore := cfg.AcquisitionScore(left), cfg.AcquisitionScore(right)
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if left.Title != right.Title {
			return left.Title < right.Title
		}
		return releaseInfoHash(left) < releaseInfoHash(right)
	})
}

func sortSeriesGapsByTitlePriority(cfg *Config, gaps []SeriesGap) {
	sort.SliceStable(gaps, func(i, j int) bool {
		leftSeason, rightSeason := gaps[i].Season, gaps[j].Season
		left := cfg.FindSeriesMatch(gaps[i].Series, &leftSeason)
		right := cfg.FindSeriesMatch(gaps[j].Series, &rightSeason)
		leftRank, rightRank := titlePriorityRank(titlePriorityNormal), titlePriorityRank(titlePriorityNormal)
		if left != nil {
			leftRank = titlePriorityRank(left.Priority)
		}
		if right != nil {
			rightRank = titlePriorityRank(right.Priority)
		}
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if gaps[i].Series != gaps[j].Series {
			return gaps[i].Series < gaps[j].Series
		}
		if gaps[i].Season != gaps[j].Season {
			return gaps[i].Season < gaps[j].Season
		}
		return gaps[i].Episode < gaps[j].Episode
	})
}
