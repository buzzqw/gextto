package gextto

// Helpers for the user-facing log lines (INFO and WARN). These lines are read
// by people, not tools: short sentences, rounded numbers with units, titles
// instead of internal identifiers. Technical detail goes to DEBUG.

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

// logInteger is any integer type a log line may count.
type logInteger interface {
	~int | ~int32 | ~int64 | ~uint | ~uint8 | ~uint32 | ~uint64
}

// plural picks the singular or plural form for count.
func plural[T logInteger](count T, singular, pluralForm string) string {
	if count == 1 {
		return singular
	}
	return pluralForm
}

// countLabel formats a count with its noun, e.g. "1 download", "3,612 releases".
func countLabel[T logInteger](count T, singular, pluralForm string) string {
	return logCount(count) + " " + plural(count, singular, pluralForm)
}

// logCount formats an integer with thousands separators ("3,612").
func logCount[T logInteger](value T) string {
	count := int64(value)
	negative := count < 0
	if negative {
		count = -count
	}
	digits := strconv.FormatInt(count, 10)
	var out strings.Builder
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(digit)
	}
	if negative {
		return "-" + out.String()
	}
	return out.String()
}

// logPercent rounds a progress percentage for display: "9%", "99.5%".
func logPercent(progress float64) string {
	if progress >= 99 && progress < 100 {
		return strconv.FormatFloat(math.Floor(progress*10)/10, 'f', -1, 64) + "%"
	}
	return strconv.FormatFloat(math.Floor(progress), 'f', 0, 64) + "%"
}

// logDuration formats a duration in plain words: "45 seconds", "4 minutes",
// "2 hours 34 minutes".
func logDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		seconds := int64(math.Round(d.Seconds()))
		return countLabel(seconds, "second", "seconds")
	}
	if d < time.Hour {
		minutes := int64(math.Round(d.Minutes()))
		return countLabel(minutes, "minute", "minutes")
	}
	hours := int64(d / time.Hour)
	minutes := int64((d % time.Hour) / time.Minute)
	text := countLabel(hours, "hour", "hours")
	if minutes > 0 {
		text += " " + countLabel(minutes, "minute", "minutes")
	}
	return text
}

// stallReasonForLog explains in plain words why a download stopped receiving
// data. DiagnoseTorrent returns Italian UI texts; the log is English.
func stallReasonForLog(code string, torrent *models.TorrentView) string {
	switch code {
	case "metadata":
		return "it is still waiting for the list of files from other users"
	case "error":
		return "the torrent engine reported an error: " + strings.TrimSpace(torrent.Error)
	case "dead_swarm":
		return "nobody currently sharing it has the complete file"
	case "no_connected_seed":
		return "users with the complete file exist but none could be reached (check the listening port and firewall)"
	case "no_peers":
		return "no other users sharing it were found"
	default:
		peers := int64(torrent.NumPeers)
		if peers > 0 {
			return fmt.Sprintf("connected to %s but no data has arrived", countLabel(peers, "user", "users"))
		}
		return "no data has arrived"
	}
}

// speedLimitLabel shows a KB/s limit, with 0 meaning no limit.
func speedLimitLabel(kib int64) string {
	if kib <= 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%d KB/s", kib)
}

// logTarget names what a release is for, the way a person would say it:
// "Silo S03E04", "From season 1", "Slow Horses S01E01-E06", "Dune (2021)".
func logTarget(release *models.Release) string {
	if release == nil {
		return ""
	}
	if release.Kind == "series" && release.Series != nil {
		name := *release.Series
		if release.Season == nil {
			return name
		}
		season := *release.Season
		episodes := make([]int64, 0, len(release.EpisodeRange))
		complete := false
		for _, episode := range release.EpisodeRange {
			if episode == 0 {
				complete = true
			} else {
				episodes = append(episodes, episode)
			}
		}
		switch {
		case complete || (release.Episode != nil && *release.Episode == 0 && len(episodes) == 0):
			return fmt.Sprintf("%s season %d", name, season)
		case len(episodes) > 1:
			first, last := episodes[0], episodes[0]
			for _, episode := range episodes {
				if episode < first {
					first = episode
				}
				if episode > last {
					last = episode
				}
			}
			return fmt.Sprintf("%s S%02dE%02d-E%02d", name, season, first, last)
		case release.Episode != nil:
			return fmt.Sprintf("%s S%02dE%02d", name, season, *release.Episode)
		default:
			return fmt.Sprintf("%s season %d", name, season)
		}
	}
	if release.Year != nil && *release.Year > 0 {
		return fmt.Sprintf("%s (%d)", release.Title, *release.Year)
	}
	return release.Title
}

// friendlyQuality summarises the quality a person cares about: resolution,
// source, HDR and language ("2160p WEB-DL Dolby Vision ITA").
func friendlyQuality(quality models.Quality) string {
	sources := map[string]string{
		"webdl": "WEB-DL", "webrip": "WEBRip", "bluray": "BluRay", "remux": "REMUX",
		"hdtv": "HDTV", "dvdrip": "DVDRip", "dvd": "DVD", "web": "WEB",
	}
	parts := make([]string, 0, 4)
	if value := strings.TrimSpace(quality.Resolution); value != "" {
		parts = append(parts, value)
	}
	if value := strings.TrimSpace(quality.Source); value != "" {
		if label, ok := sources[strings.ToLower(value)]; ok {
			value = label
		}
		parts = append(parts, value)
	}
	if quality.IsDV {
		parts = append(parts, "Dolby Vision")
	} else if value := strings.TrimSpace(quality.HDR); value != "" {
		parts = append(parts, strings.ToUpper(value))
	}
	if value := strings.TrimSpace(quality.Language); value != "" {
		parts = append(parts, strings.ToUpper(value))
	}
	if len(parts) == 0 {
		return "unknown quality"
	}
	return strings.Join(parts, " ")
}

// skipReasonText explains why a candidate was not downloaded.
func skipReasonText(approvalReason string) string {
	switch approvalReason {
	case "blocklisted":
		return "it is on the blocklist"
	case "smart_episode":
		return "later episodes are already in the library and this one is not a known gap"
	case "upgrades_disabled":
		return "upgrades are turned off for this title"
	case "active_pack":
		return "a pack for this season is already downloading"
	case "active_episode":
		return "it is already downloading"
	case "duplicate":
		return "the library already has it in the same or better quality"
	case "":
		return "no reason given"
	default:
		return approvalReason
	}
}

// cycleModeLabel describes which titles a search cycle covers.
func cycleModeLabel(mode string) string {
	switch mode {
	case "series":
		return "series only"
	case "movies":
		return "movies only"
	case "comics":
		return "comics only"
	default:
		return "all titles"
	}
}

// cycleReportText is the closing summary of a search cycle.
func cycleReportText(duration string, stats *models.CycleStats, upgrades, newItems int) string {
	downloads := "nothing new to download"
	if stats.DownloadsStarted > 0 {
		var kinds []string
		if newItems > 0 {
			kinds = append(kinds, countLabel(int64(newItems), "new", "new"))
		}
		if upgrades > 0 {
			kinds = append(kinds, countLabel(int64(upgrades), "upgrade", "upgrades"))
		}
		if stats.GapsFilled > 0 {
			kinds = append(kinds, fmt.Sprintf("%s missing %s",
				logCount(stats.GapsFilled), plural(stats.GapsFilled, "episode filled", "episodes filled")))
		}
		downloads = countLabel(stats.DownloadsStarted, "download", "downloads") + " started"
		if len(kinds) > 0 {
			downloads += " (" + strings.Join(kinds, ", ") + ")"
		}
	}
	return fmt.Sprintf("📊 Search finished in %s: %s checked, %s matched your titles, %s%s",
		duration, countLabel(stats.Scraped, "release", "releases"), logCount(stats.Candidates),
		downloads, cycleErrorsSuffix(stats.Errors))
}

// cycleErrorsSuffix reports the cycle errors, pointing to the warnings above.
func cycleErrorsSuffix(errors int) string {
	if errors <= 0 {
		return ""
	}
	return fmt.Sprintf("; %s (see the warnings above)", countLabel(errors, "problem", "problems"))
}

// torrentErrorText explains a torrent engine error event.
func torrentErrorText(kind, message string) string {
	message = strings.TrimSpace(message)
	var what string
	switch kind {
	case "file_error":
		what = "a file could not be read or written"
	case "metadata_failed":
		what = "the list of files received was invalid"
	case "resume_save_failed":
		what = "its progress could not be saved"
	default:
		what = "the torrent engine reported an error"
	}
	if message == "" {
		return what
	}
	return what + " (" + message + ")"
}

// hashFailureAlertThreshold is how many damaged pieces a torrent may receive
// before the user is warned: a few are normal and repaired automatically.
const hashFailureAlertThreshold = 50

var hashFailures = struct {
	sync.Mutex
	counts map[string]int
}{counts: map[string]int{}}

// recordHashFailure counts a damaged piece for a torrent and reports whether
// this one crosses the alert threshold (true once per torrent).
func recordHashFailure(hash string) (int, bool) {
	key := strings.ToLower(hash)
	hashFailures.Lock()
	defer hashFailures.Unlock()
	if len(hashFailures.counts) > 1000 {
		hashFailures.counts = map[string]int{}
	}
	hashFailures.counts[key]++
	count := hashFailures.counts[key]
	return count, count == hashFailureAlertThreshold
}
