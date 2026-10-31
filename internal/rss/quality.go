package rss

import "strings"

// QualityRank extracts the video quality from a release title and gives it a
// rank, so a profile can order 720p before 1080p before 2160p and decide
// whether a release is an upgrade. It is deliberately small: the resolution
// tags found in release names, not a media probe.
func QualityRank(title string) (string, int) {
	lower := strings.ToLower(title)
	switch {
	case containsQuality(lower, "2160p") || containsQuality(lower, "4k") || containsQuality(lower, "uhd"):
		return "2160p", 6
	case containsQuality(lower, "1080p"):
		return "1080p", 5
	case containsQuality(lower, "720p"):
		return "720p", 4
	case containsQuality(lower, "576p"):
		return "576p", 3
	case containsQuality(lower, "480p"):
		return "480p", 2
	case containsQuality(lower, "360p"):
		return "360p", 1
	}
	return "", 0
}

// QualityRankOf maps a configured quality label to its rank. A range or a "+"
// takes the lowest rank it accepts, which is the safe choice for a cutoff.
func QualityRankOf(label string) int {
	label = strings.ToLower(strings.TrimSpace(label))
	if label == "" || label == "any" || label == "*" {
		return 0
	}
	if label == "2160p+" {
		return 6
	}
	if label == "1080p+" {
		return 5
	}
	if label == "720p+" {
		return 4
	}
	if left, right, ok := strings.Cut(label, "-"); ok {
		if rank := QualityRankOf(left); rank > 0 {
			return rank
		}
		return QualityRankOf(right)
	}
	_, rank := QualityRank(label)
	return rank
}

func containsQuality(lower, token string) bool {
	for i := 0; i+len(token) <= len(lower); i++ {
		if lower[i:i+len(token)] != token {
			continue
		}
		before := i == 0 || !isQualityChar(lower[i-1])
		after := i+len(token) == len(lower) || !isQualityChar(lower[i+len(token)])
		if before && after {
			return true
		}
	}
	return false
}

func isQualityChar(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9'
}
