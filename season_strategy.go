package gextto

import (
	"strings"

	"github.com/buzzqw/gextto/internal/models"
)

const (
	seasonStrategyAuto        = "auto"
	seasonStrategySingles     = "singles"
	seasonStrategyWaitForPack = "wait-for-pack"
)

func normalizeSeasonStrategy(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", seasonStrategyAuto:
		return seasonStrategyAuto, true
	case seasonStrategySingles:
		return seasonStrategySingles, true
	case seasonStrategyWaitForPack:
		return seasonStrategyWaitForPack, true
	default:
		return "", false
	}
}

func storedSeasonStrategy(raw string) string {
	if strategy, ok := normalizeSeasonStrategy(raw); ok {
		return strategy
	}
	return seasonStrategyAuto
}

func seriesSeasonStrategy(series *SeriesConfig) string {
	if series == nil {
		return seasonStrategyAuto
	}
	return storedSeasonStrategy(series.SeasonStrategy)
}

func completeSeasonPack(release *models.Release) bool {
	if release == nil || !release.IsPack {
		return false
	}
	for _, episode := range release.EpisodeRange {
		if episode == 0 {
			return true
		}
	}
	return false
}

// seasonStrategyAllowsRelease is the automatic-acquisition policy. Manual
// downloads deliberately bypass it, like the other automatic filters.
func seasonStrategyAllowsRelease(series *SeriesConfig, release *models.Release) bool {
	switch seriesSeasonStrategy(series) {
	case seasonStrategySingles:
		return release != nil && !release.IsPack
	case seasonStrategyWaitForPack:
		return completeSeasonPack(release)
	default:
		return true
	}
}
