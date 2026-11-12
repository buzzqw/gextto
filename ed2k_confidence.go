package gextto

import (
	"path/filepath"
	"regexp"
	"strings"
)

// ed2k_confidence.go scores how much an eD2k result can be trusted. eD2k has
// no tracker moderating what is shared, and fakes are common: a program named
// like an episode, a tiny file posing as a full one, one file renamed after
// many titles. The score warns and explains, it never hides a result: the
// page shows a badge with the reasons, and only the automatic fallback skips
// what is clearly not the file it looks for.

// Trust levels of an eD2k result.
const (
	ed2kTrustGood     = "good"
	ed2kTrustDoubtful = "doubtful"
	ed2kTrustSuspect  = "suspect"
)

// The reasons are catalog keys, shown in the tooltip of the badge.
const (
	ed2kReasonProgram   = "È un programma, non un video"
	ed2kReasonArchive   = "È un archivio: può contenere qualsiasi cosa"
	ed2kReasonTiny      = "Molto più piccolo di un video di questa qualità"
	ed2kReasonSmall     = "Più piccolo di quanto la qualità dichiarata richiede"
	ed2kReasonSameSize  = "La stessa dimensione esatta compare con titoli diversi: tipico dei falsi"
	ed2kReasonSpam      = "Il nome contiene pubblicità o parole tipiche dei falsi"
	ed2kReasonOneSource = "Una sola fonte: il download potrebbe non completarsi"
)

// ed2kCandidate is what the score reads of a result.
type ed2kCandidate struct {
	Name    string
	Size    int64
	Sources int64
}

// ed2kTrust is the verdict on one result.
type ed2kTrust struct {
	Level   string
	Reasons []string
}

var (
	ed2kProgramExt = map[string]bool{".exe": true, ".scr": true, ".bat": true, ".com": true, ".cmd": true, ".msi": true,
		".lnk": true, ".vbs": true, ".js": true, ".jar": true, ".apk": true, ".pif": true}
	ed2kArchiveExt = map[string]bool{".zip": true, ".rar": true, ".7z": true, ".ace": true, ".tar": true, ".gz": true}
	ed2kSpamRE     = regexp.MustCompile(`(?i)(www\.|\.com\b|\.net\b|\.org\b|https?:|password|passwd|codec|keygen|crack|serial|full\s*version|scarica\s*gratis|free\s*download|virus)`)
	ed2kEpisodeRE  = regexp.MustCompile(`(?i)\bs\d{1,2}\s*e\d{1,3}\b|\b\d{1,2}x\d{2,3}\b`)
	ed2kEpisodeKey = regexp.MustCompile(`(?i)s(\d{1,2})\s*e(\d{1,3})|(\d{1,2})x(\d{2,3})`)
)

// ed2kMinSize is the least size a genuine video of this name can have: by
// resolution, for an episode or a movie.
func ed2kMinSize(name string) int64 {
	lower := strings.ToLower(name)
	episode := ed2kEpisodeRE.MatchString(lower)
	var minEpisode, minMovie int64
	switch {
	case strings.Contains(lower, "2160p") || strings.Contains(lower, "4k") || strings.Contains(lower, "uhd"):
		minEpisode, minMovie = 700<<20, 1500<<20
	case strings.Contains(lower, "1080p") || strings.Contains(lower, "1080i"):
		minEpisode, minMovie = 250<<20, 700<<20
	case strings.Contains(lower, "720p"):
		minEpisode, minMovie = 150<<20, 400<<20
	default:
		minEpisode, minMovie = 70<<20, 200<<20
	}
	if episode {
		return minEpisode
	}
	return minMovie
}

// ed2kEpisodeOf returns "SxxEyy" of a name, or "".
func ed2kEpisodeOf(name string) string {
	m := ed2kEpisodeKey.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	season, episode := m[1], m[2]
	if season == "" {
		season, episode = m[3], m[4]
	}
	return strings.TrimLeft(season, "0") + "x" + strings.TrimLeft(episode, "0")
}

// ed2kConfidence scores one result; all are the results of the same search,
// read to find one size under several titles.
func ed2kConfidence(c ed2kCandidate, all []ed2kCandidate) ed2kTrust {
	level := ed2kTrustGood
	var reasons []string
	raise := func(to string, reason string) {
		reasons = append(reasons, reason)
		if to == ed2kTrustSuspect || (to == ed2kTrustDoubtful && level == ed2kTrustGood) {
			level = to
		}
	}
	name := strings.TrimSpace(c.Name)
	ext := strings.ToLower(filepath.Ext(name))
	switch {
	case ed2kProgramExt[ext]:
		raise(ed2kTrustSuspect, ed2kReasonProgram)
	case ed2kArchiveExt[ext]:
		raise(ed2kTrustDoubtful, ed2kReasonArchive)
	case isVideoExtension(name):
		if minimum := ed2kMinSize(name); c.Size > 0 && c.Size < minimum/4 {
			raise(ed2kTrustSuspect, ed2kReasonTiny)
		} else if c.Size > 0 && c.Size < minimum {
			raise(ed2kTrustDoubtful, ed2kReasonSmall)
		}
	}
	if ed2kSpamRE.MatchString(name) {
		raise(ed2kTrustDoubtful, ed2kReasonSpam)
	}
	// One exact size under titles of different episodes: a renamed fake.
	if episode := ed2kEpisodeOf(name); episode != "" && c.Size > 0 {
		for _, other := range all {
			if other.Size != c.Size || other.Name == c.Name {
				continue
			}
			if otherEpisode := ed2kEpisodeOf(other.Name); otherEpisode != "" && otherEpisode != episode {
				raise(ed2kTrustSuspect, ed2kReasonSameSize)
				break
			}
		}
	}
	if c.Sources == 1 {
		reasons = append(reasons, ed2kReasonOneSource)
	}
	return ed2kTrust{Level: level, Reasons: reasons}
}
