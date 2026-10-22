package gextto

import "github.com/buzzqw/gextto/internal/models"

// upgradeReasonUntil is Quality.UpgradeReason with the "upgrade until" cutoff
// (setting upgrade_until_score): once the existing copy scores at least until,
// it is good enough and no better release replaces it. A REPACK or PROPER is
// still accepted because it fixes a defective release rather than improving
// the quality. until <= 0 disables the cutoff.
//
// A "score" upgrade must also be a real quality gain: settings are the score
// weights (nil = the defaults), and the technical score of the candidate has to
// beat the existing copy by the same minimum difference. Size, subtitles and
// swarm health are part of the release score but never justify replacing a
// copy of the same quality on their own.
func upgradeReasonUntil(candidate, existing *models.Quality, newScore, oldScore, minScoreDiff, until int64, settings map[string]string) string {
	reason := upgradeReasonWith(candidate, existing, newScore, oldScore, minScoreDiff, settings)
	if reason == "" || !upgradeCutoffReached(oldScore, until) {
		return reason
	}
	if reason == "repack" || reason == "proper" {
		return reason
	}
	return ""
}

// upgradeReasonWith is Quality.UpgradeReason read with the configured weights
// (nil = the defaults). The WEB-DL-over-WEBRip/HDTV rule holds only while the
// configured source weight of the candidate is higher: whoever weighs WEB-DL
// and WEBRip the same treats them as the same quality, and the candidate is
// then judged as if it had the archived copy's source (HDR, a resolution step
// or a REPACK still count). A "score" upgrade must also be a technical gain.
func upgradeReasonWith(candidate, existing *models.Quality, newScore, oldScore, minScoreDiff int64, settings map[string]string) string {
	reason := sourceAwareUpgradeReason(candidate, existing, newScore, oldScore, minScoreDiff, settings)
	if reason == "score" && !technicalScoreGain(candidate, existing, minScoreDiff, settings) {
		return ""
	}
	return reason
}

// sourceAwareUpgradeReason is the source part of upgradeReasonWith alone. The
// cleaner uses it after the download: the incoming file is often already
// renamed without quality tags, so only the decision before the download can
// judge the technical gain.
func sourceAwareUpgradeReason(candidate, existing *models.Quality, newScore, oldScore, minScoreDiff int64, settings map[string]string) string {
	reason := candidate.UpgradeReason(existing, newScore, oldScore, minScoreDiff)
	if reason == "source" && sourceWeight(candidate.Source, settings) <= sourceWeight(existing.Source, settings) {
		sameSource := *candidate
		sameSource.Source = existing.Source
		reason = sameSource.UpgradeReason(existing, newScore, oldScore, minScoreDiff)
	}
	return reason
}

// sourceWeight is the configured score of a release source alone.
func sourceWeight(source string, settings map[string]string) int64 {
	quality := models.Quality{Source: source}
	return ScoreQuality(&quality, settings)
}

// technicalScoreGain reports whether the quality alone (resolution, source,
// codec, audio, HDR, release fixes and group, with the configured weights)
// improves by at least minScoreDiff, and by at least one point.
func technicalScoreGain(candidate, existing *models.Quality, minScoreDiff int64, settings map[string]string) bool {
	gain := ScoreQuality(candidate, settings) - ScoreQuality(existing, settings)
	return gain > 0 && gain >= minScoreDiff
}

func upgradeCutoffReached(oldScore, until int64) bool {
	return until > 0 && oldScore >= until
}
