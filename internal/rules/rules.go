// Package rules implements the built-in, always-on release sanity rules.
//
// These replace the former user-configurable release rules / size envelopes:
// the useful checks are hard-coded with values derived from a real archive
// instead of asking the user to guess numbers.
//
// Two checks are implemented:
//
// - Hardcoded subtitles: a release that carries burned-in subs is refused.
// - Absurd size: a per-resolution sanity floor (MiB) with two tiers, a very
// low hard floor applied globally and a sane floor applied per title.
package rules

import (
	"fmt"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

// HardFloorMB is the very low absolute floor: only obvious fakes/truncated
// files. Applied globally, before a release is matched to a title.
func HardFloorMB(resolution string) int64 {
	switch resolution {
	case "2160p":
		return 300
	case "1080p":
		return 80
	case "720p":
		return 60
	case "576p":
		return 50
	case "480p":
		return 40
	default:
		return 40
	}
}

// SaneFloorMB is the sane per-resolution floor derived from a real archive.
func SaneFloorMB(resolution string) int64 {
	switch resolution {
	case "2160p":
		return 800
	case "1080p":
		return 120
	case "720p":
		return 100
	case "576p":
		return 60
	case "480p":
		return 60
	default:
		return 50
	}
}

// HardFloorBytes returns the hard floor in bytes.
func HardFloorBytes(resolution string) uint64 {
	mb := HardFloorMB(resolution)
	if mb < 0 {
		mb = 0
	}
	return uint64(mb) * 1_048_576
}

// SaneFloorBytes returns the sane floor in bytes.
func SaneFloorBytes(resolution string) uint64 {
	mb := SaneFloorMB(resolution)
	if mb < 0 {
		mb = 0
	}
	return uint64(mb) * 1_048_576
}

// EffectiveFloorMB returns the effective sane floor for a title. Lower-only: it
// can never exceed SaneFloorMB, and a series with no archived history falls back
// to the permissive hard floor.
func EffectiveFloorMB(resolution string, seriesMinMB *int64) int64 {
	if seriesMinMB != nil && *seriesMinMB > 0 {
		sane := SaneFloorMB(resolution)
		half := *seriesMinMB / 2
		if half < sane {
			return half
		}
		return sane
	}
	return HardFloorMB(resolution)
}

// DeniedReason reports a global reason a release is refused by a built-in rule
// (hardcoded subtitles or the hard size floor), or "" when it passes.
func DeniedReason(release *models.Release) string {
	if release.Quality.HardcodedSubs {
		return "hardcoded subtitles"
	}
	if release.SizeBytes > 0 {
		floor := HardFloorBytes(release.Quality.Resolution)
		if uint64(release.SizeBytes) < floor {
			return fmt.Sprintf("size %d MiB below the %s hard floor of %d MiB",
				release.SizeBytes/1_048_576,
				displayResolution(release.Quality.Resolution),
				HardFloorMB(release.Quality.Resolution))
		}
	}
	return ""
}

// SaneSizeDeniedReason reports the per-title sane size reason, using the
// adaptive floor. Returns "" when the size is unknown or acceptable.
func SaneSizeDeniedReason(release *models.Release, seriesMinMB *int64) string {
	if release.Quality.HardcodedSubs || release.SizeBytes <= 0 {
		return ""
	}
	floor := EffectiveFloorMB(release.Quality.Resolution, seriesMinMB)
	if floor < 0 {
		floor = 0
	}
	if uint64(release.SizeBytes) < uint64(floor)*1_048_576 {
		return fmt.Sprintf("size %d MiB below the %s sanity floor of %d MiB",
			release.SizeBytes/1_048_576,
			displayResolution(release.Quality.Resolution),
			floor)
	}
	return ""
}

func displayResolution(resolution string) string {
	if resolution == "" {
		return "unknown"
	}
	return resolution
}

// LogRejection logs a rejected release at DEBUG.
func LogRejection(release *models.Release, reason string) {
	logging.Debug("🚫 release rejected",
		"title", release.Title,
		"source", release.Source,
		"resolution", release.Quality.Resolution,
		"size_mb", release.SizeBytes/1_048_576,
		"season", release.Season,
		"episode", release.Episode,
		"reason", reason)
}

// SizeScoreBonus is a small preference for a healthier bitrate within the same
// resolution: +1 per 100 MiB above the sane floor, capped at 100.
func SizeScoreBonus(release *models.Release) int64 {
	if release.SizeBytes <= 0 {
		return 0
	}
	floor := SaneFloorBytes(release.Quality.Resolution)
	size := uint64(release.SizeBytes)
	if size <= floor {
		return 0
	}
	bonus := int64((size - floor) / (100 * 1_048_576))
	if bonus > 100 {
		return 100
	}
	return bonus
}
