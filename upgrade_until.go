package gextto

import "github.com/buzzqw/gextto/internal/models"

// upgradeReasonUntil is Quality.UpgradeReason with the "upgrade until" cutoff
// (setting upgrade_until_score): once the existing copy scores at least until,
// it is good enough and no better release replaces it. A REPACK or PROPER is
// still accepted because it fixes a defective release rather than improving
// the quality. until <= 0 disables the cutoff.
func upgradeReasonUntil(candidate, existing *models.Quality, newScore, oldScore, minScoreDiff, until int64) string {
	reason := candidate.UpgradeReason(existing, newScore, oldScore, minScoreDiff)
	if reason == "" || !upgradeCutoffReached(oldScore, until) {
		return reason
	}
	if reason == "repack" || reason == "proper" {
		return reason
	}
	return ""
}

func upgradeCutoffReached(oldScore, until int64) bool {
	return until > 0 && oldScore >= until
}
