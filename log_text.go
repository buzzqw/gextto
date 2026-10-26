package gextto

// Helpers for the user-facing log lines (INFO and WARN). These lines are read
// by people, not tools: short sentences, rounded numbers with units, titles
// instead of internal identifiers. Technical detail goes to DEBUG.

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
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

// searchProgressText is the periodic line of a title search: it says how far
// the search has got without repeating the total twice.
func searchProgressText(completed, total int) string {
	if total <= 0 {
		return ""
	}
	percent := completed * 100 / total
	return fmt.Sprintf("🔎 Searching: %d%% done (%s of %s titles)",
		percent, logCount(completed), logCount(total))
}

// logPercent rounds a progress percentage for display: "9%", "99.5%".
// shortTorrentName trims a torrent display name for the one-line download
// summary: long release names would make the periodic status unreadable.
func shortTorrentName(name string) string {
	name = strings.TrimSpace(name)
	if len(name) <= 40 {
		return name
	}
	return strings.TrimSpace(name[:39]) + "…"
}

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

// speedLimitText shows a KB/s limit in the unit a person reads, 0 meaning no
// limit: "597 KB/s", "2.5 MB/s", "no limit".
func speedLimitText(kib int64) string {
	if kib <= 0 {
		return "no limit"
	}
	if kib >= 1024 {
		return fmt.Sprintf("%.1f MB/s", float64(kib)/1024)
	}
	return fmt.Sprintf("%d KB/s", kib)
}

// speedLimitLabel is the older spelling used where the sentence already says
// "unlimited"; new lines use speedLimitText.
func speedLimitLabel(kib int64) string {
	if kib <= 0 {
		return "unlimited"
	}
	return speedLimitText(kib)
}

// speedPairText describes a download/upload limit pair in words:
// "no speed limit", "download 2.5 MB/s, upload unlimited".
func speedPairText(down, up int64) string {
	switch {
	case down <= 0 && up <= 0:
		return "no speed limit"
	case down <= 0:
		return "download unlimited, upload " + speedLimitText(up)
	case up <= 0:
		return "download " + speedLimitText(down) + ", upload unlimited"
	default:
		return "download " + speedLimitText(down) + ", upload " + speedLimitText(up)
	}
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

// qualityDetailText is the fuller quality label used when the log has to
// explain a comparison: resolution, source, codec, audio, HDR and language.
// Unlike friendlyQuality it keeps the codec/audio fields, and it drops the
// parser's literal "unknown" placeholders instead of printing them twice.
func qualityDetailText(quality models.Quality) string {
	value := func(raw string) string {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.EqualFold(raw, "unknown") {
			return ""
		}
		return raw
	}
	parts := make([]string, 0, 6)
	for _, raw := range []string{quality.Resolution, quality.Source, quality.Codec, quality.Audio} {
		if field := value(raw); field != "" {
			parts = append(parts, field)
		}
	}
	if quality.IsDV {
		parts = append(parts, "Dolby Vision")
	} else if hdr := value(quality.HDR); hdr != "" {
		parts = append(parts, strings.ToUpper(hdr))
	}
	if language := value(quality.Language); language != "" {
		parts = append(parts, strings.ToUpper(language))
	}
	if len(parts) == 0 {
		return "unknown quality"
	}
	return strings.Join(parts, " ")
}

// episodeRangeLabel names a set of episodes compactly: "S01E01", "S01E01–S01E02"
// or "S01E01, S01E03". Consecutive episodes share one range, so a pack log line
// stays readable even when it covers a whole season.
func episodeRangeLabel(season int64, episodes []int64) string {
	if len(episodes) == 0 {
		return ""
	}
	ordered := append([]int64(nil), episodes...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	unique := ordered[:0]
	for _, episode := range ordered {
		if len(unique) == 0 || unique[len(unique)-1] != episode {
			unique = append(unique, episode)
		}
	}
	parts := make([]string, 0, len(unique))
	start, previous := unique[0], unique[0]
	flush := func() {
		if start == previous {
			parts = append(parts, fmt.Sprintf("S%02dE%02d", season, start))
			return
		}
		parts = append(parts, fmt.Sprintf("S%02dE%02d–S%02dE%02d", season, start, season, previous))
	}
	for _, episode := range unique[1:] {
		if episode == previous+1 {
			previous = episode
			continue
		}
		flush()
		start, previous = episode, episode
	}
	flush()
	return strings.Join(parts, ", ")
}

// upgradeSourceText describes what is already in the library for the episodes
// a release would replace, e.g. "S01E01–S01E02 480p h264 aac ITA (125)".
// It turns "better version than the one you have" into a statement that names
// both sides of the comparison.
func upgradeSourceText(archive *models.ArchiveQualityIndex, release *models.Release) string {
	if archive.IsEmpty() || release == nil || release.Kind != "series" || release.Season == nil {
		return ""
	}
	episodes := release.EpisodeRange
	if len(episodes) == 0 && release.Episode != nil && *release.Episode > 0 {
		episodes = []int64{*release.Episode}
	}
	if len(episodes) == 0 {
		return ""
	}
	order := make([]string, 0, len(episodes))
	groups := map[string][]int64{}
	for _, episode := range episodes {
		existing, onDisk := archive.BestFor(*release.Season, episode)
		label := "missing from the library"
		if onDisk {
			label = fmt.Sprintf("%s (score %d)", qualityDetailText(existing.Quality), existing.Score)
		}
		if _, seen := groups[label]; !seen {
			order = append(order, label)
		}
		groups[label] = append(groups[label], episode)
	}
	parts := make([]string, 0, len(order))
	for _, label := range order {
		parts = append(parts, episodeRangeLabel(*release.Season, groups[label])+" "+label)
		if len(parts) == 4 && len(order) > 4 {
			parts = append(parts, fmt.Sprintf("+%d more", len(order)-4))
			break
		}
	}
	return strings.Join(parts, ", ")
}

// archiveCoversRelease reports whether at least one episode of the release
// already has a copy in the library index. It separates a real upgrade from an
// episode that is simply missing, which the same approval path can carry.
func archiveCoversRelease(archive *models.ArchiveQualityIndex, release *models.Release) bool {
	if release == nil || release.Season == nil {
		return false
	}
	episodes := release.EpisodeRange
	if len(episodes) == 0 && release.Episode != nil && *release.Episode > 0 {
		episodes = []int64{*release.Episode}
	}
	for _, episode := range episodes {
		if _, ok := archive.BestFor(*release.Season, episode); ok {
			return true
		}
	}
	return false
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
	case "dead_swarm_empty":
		return "nobody is sharing it: no seeders and no peers"
	case "dead_swarm_upgrade":
		return "the upgrade has no seeders; the copy in the library stays"
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
		// A full cycle covers every kind: name them so the log shows the movie
		// scan too (searching the log for "movie" used to find nothing, which
		// made it look like movies were never scanned).
		return "series, movies and comics"
	}
}

// cycleReportText is the closing summary of a search cycle.
// cycleSkipCounts says why the matched releases of a cycle were not
// downloaded, for the end-of-cycle report.
type cycleSkipCounts struct {
	InLibrary   int // the library already has it in the same or better quality
	Downloading int // already in the torrent session
	Waiting     int // held by a delay profile, in case a better version appears
	Other       int // skipped for another reason (each one has its own INFO line)
}

func (c cycleSkipCounts) text() string {
	var parts []string
	if c.InLibrary > 0 {
		parts = append(parts, fmt.Sprintf("%s already in the library", logCount(c.InLibrary)))
	}
	if c.Downloading > 0 {
		parts = append(parts, fmt.Sprintf("%s already downloading", logCount(c.Downloading)))
	}
	if c.Waiting > 0 {
		parts = append(parts, fmt.Sprintf("%s waiting for a better version", logCount(c.Waiting)))
	}
	if c.Other > 0 {
		parts = append(parts, fmt.Sprintf("%s skipped (see above)", logCount(c.Other)))
	}
	return strings.Join(parts, ", ")
}

func cycleReportText(duration string, stats *models.CycleStats, upgrades, newItems int, skips cycleSkipCounts) string {
	downloads := "nothing new to download"
	if why := skips.text(); why != "" && stats.DownloadsStarted == 0 {
		downloads += ": " + why
	}
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
		if why := skips.text(); why != "" {
			downloads += "; the others: " + why
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

// renameItemLogText describes one change made by the library check: an
// episode renamed (old name → new name) or an inferior copy removed. Empty for
// a failed item, which has its own warning.
func renameItemLogText(series string, item map[string]any, cleanupAction string) string {
	if _, failed := item["error"]; failed {
		return ""
	}
	label := series
	season, hasSeason := item["season"].(int64)
	episode, hasEpisode := item["episode"].(int64)
	if hasSeason && hasEpisode {
		label = fmt.Sprintf("%s S%02dE%02d", series, season, episode)
	}
	from, _ := item["from"].(string)
	if discarded, _ := item["discarded"].(bool); discarded {
		action := "moved to the trash"
		if cleanupAction == "delete" {
			action = "deleted"
		}
		return fmt.Sprintf("🗑️ %s: «%s» %s — the library already has a better copy", label, filepath.Base(from), action)
	}
	to, _ := item["to"].(string)
	if to == "" {
		return ""
	}
	if filepath.Dir(from) != filepath.Dir(to) {
		return fmt.Sprintf("✏️ %s renamed: «%s» → «%s» (moved to %s)", label, filepath.Base(from), filepath.Base(to), filepath.Dir(to))
	}
	return fmt.Sprintf("✏️ %s renamed: «%s» → «%s»", label, filepath.Base(from), filepath.Base(to))
}

// renameRepairReportText summarises a periodic library check (rename to the
// naming scheme, inferior copies and duplicates removed).
func renameRepairReportText(series int, correct, renamed, discarded, duplicates, errs int64) string {
	checked := countLabel(series, "series", "series")
	if renamed+discarded+duplicates+errs == 0 {
		return fmt.Sprintf("🗂 Library check: %s, %s — all correctly named, no inferior or duplicate copies",
			checked, countLabel(correct, "episode", "episodes"))
	}
	var done []string
	if renamed > 0 {
		done = append(done, countLabel(renamed, "episode renamed", "episodes renamed"))
	}
	if discarded > 0 {
		done = append(done, countLabel(discarded, "inferior copy removed", "inferior copies removed"))
	}
	if duplicates > 0 {
		done = append(done, countLabel(duplicates, "duplicate removed", "duplicates removed"))
	}
	if errs > 0 {
		done = append(done, countLabel(errs, "error (see the warnings above)", "errors (see the warnings above)"))
	}
	return fmt.Sprintf("🗂 Library check: %s · %s", checked, strings.Join(done, " · "))
}

// torrentErrorNoticeCooldown is how long a torrent/file error is reported only
// once for the same torrent: libtorrent posts one alert per failed disk job,
// so a single problem (disk full, missing folder) arrives as a burst, and again
// after every resume or restart.
const torrentErrorNoticeCooldown = 6 * time.Hour

var torrentErrorNotices = struct {
	sync.Mutex
	last map[string]time.Time
}{last: map[string]time.Time{}}

// recordTorrentErrorNotice reports whether a torrent error of this kind should
// be logged as a warning and notified now (false while a previous one for the
// same torrent is still within the cooldown).
func recordTorrentErrorNotice(hash, kind string, now time.Time) bool {
	if strings.TrimSpace(hash) == "" {
		return true
	}
	key := strings.ToLower(hash) + "|" + kind
	torrentErrorNotices.Lock()
	defer torrentErrorNotices.Unlock()
	if last, ok := torrentErrorNotices.last[key]; ok && now.Sub(last) < torrentErrorNoticeCooldown {
		return false
	}
	if len(torrentErrorNotices.last) > 1000 {
		torrentErrorNotices.last = map[string]time.Time{}
	}
	torrentErrorNotices.last[key] = now
	return true
}

// indexerTestText is the user-facing line of the indexer test button: a
// sentence with the indexer's name, where it is and what it answered, instead
// of a trail of internal fields.
func indexerTestText(kind, name, host string, reachable, ok bool, status int, torznabError string) string {
	label := strings.TrimSpace(name)
	if label == "" {
		label = strings.TrimSpace(kind)
	}
	if label == "" {
		label = "indexer"
	}
	kindTag := ""
	if kind != "" && !strings.EqualFold(kind, "torznab") && !strings.EqualFold(kind, label) {
		kindTag = " (" + kind + ")"
	}
	where := ""
	if host = strings.TrimSpace(host); host != "" {
		where = " at " + host
	}
	switch {
	case !reachable:
		return fmt.Sprintf("⚠️ The indexer «%s»%s%s did not answer the test: the service is unreachable", label, kindTag, where)
	case torznabError != "":
		return fmt.Sprintf("⚠️ The indexer «%s»%s%s answered the test with a Torznab error", label, kindTag, where)
	case !ok:
		return fmt.Sprintf("⚠️ The indexer «%s»%s%s answered the test with HTTP %d", label, kindTag, where, status)
	default:
		return fmt.Sprintf("✅ The indexer «%s»%s%s answered the test (HTTP %d)", label, kindTag, where, status)
	}
}

// mediaInfoMismatchText explains a file whose real quality differs from the
// release title stored in the database. It is the line that makes a stale
// upgrade row visible: "the file says 480p h264, the recorded release says
// 1080p h265; using the file on disk". Empty when the two agree or the title
// carries no quality at all.
func mediaInfoMismatchText(name string, recorded, onDisk models.Quality) string {
	known := func(value string) bool {
		value = strings.TrimSpace(value)
		return value != "" && !strings.EqualFold(value, "unknown")
	}
	different := func(a, b string) bool {
		return known(a) && known(b) && !strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
	}
	if !known(recorded.Resolution) {
		return ""
	}
	if !different(recorded.Resolution, onDisk.Resolution) &&
		!different(recorded.Codec, onDisk.Codec) &&
		!different(recorded.Audio, onDisk.Audio) {
		return ""
	}
	return fmt.Sprintf("🔎 %s: the file on disk is %s, while the release was recorded as %s; using the file on disk",
		name, qualityDetailText(onDisk), qualityDetailText(recorded))
}

// mediaInfoBackfillText summarises a MediaInfo backfill run: which library
// files had their real quality read, and why that matters. Empty when there
// is nothing worth telling.
func mediaInfoBackfillText(names []string, failed, missing int) string {
	if len(names) == 0 && failed == 0 && missing == 0 {
		return ""
	}
	var text string
	if len(names) > 0 {
		const shown = 4
		listed := names
		more := ""
		if len(listed) > shown {
			more = fmt.Sprintf(" and %d more", len(listed)-shown)
			listed = listed[:shown]
		}
		text = fmt.Sprintf("🔬 Read the real quality of %s (%s%s): future upgrades will compare with what is actually on disk",
			countLabel(len(names), "library file", "library files"), strings.Join(listed, ", "), more)
	} else {
		text = "🔬 Library quality check"
	}
	var problems []string
	if failed > 0 {
		problems = append(problems, countLabel(failed, "file could not be read", "files could not be read"))
	}
	if missing > 0 {
		problems = append(problems, countLabel(missing, "file is no longer on disk", "files are no longer on disk"))
	}
	if len(problems) > 0 {
		if len(names) == 0 {
			return text + ": " + strings.Join(problems, ", ")
		}
		text += "; " + strings.Join(problems, ", ")
	}
	return text
}

// seedFinishedLead opens a line about a torrent whose seeding may have just
// ended: "«X» has finished seeding (ratio reached)" the first time, «X»
// otherwise. It clears *reason so the end of seeding is announced once.
func seedFinishedLead(name string, reason *string) string {
	if reason == nil || *reason == "" {
		return "«" + name + "»"
	}
	lead := fmt.Sprintf("«%s» has finished seeding (%s)", name, *reason)
	*reason = ""
	return lead
}
