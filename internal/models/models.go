// Package models defines the shared domain types of gextto.
package models

import (
	"strings"
	"time"
)

// Quality is the parsed technical quality of a release.
type Quality struct {
	Resolution        string   `json:"resolution"`
	Source            string   `json:"source"`
	Codec             string   `json:"codec"`
	Audio             string   `json:"audio"`
	AudioChannels     string   `json:"audio_channels,omitempty"`
	AudioObjects      string   `json:"audio_objects,omitempty"`
	HDR               string   `json:"hdr"`
	Group             string   `json:"group"`
	IsIta             bool     `json:"is_ita"`
	IsDV              bool     `json:"is_dv"`
	IsRepack          bool     `json:"is_repack"`
	IsProper          bool     `json:"is_proper"`
	IsReal            bool     `json:"is_real"`
	Language          string   `json:"language"`
	Languages         []string `json:"languages"`
	HasSubtitle       bool     `json:"has_subtitle"`
	SubtitleLanguages []string `json:"subtitle_languages"`
	// HardcodedSubs marks burned-in subtitles (HC/hardcoded): refused by a
	// built-in rule.
	HardcodedSubs bool `json:"hardcoded_subs"`
}

// ScoreBreakdownItem is one labelled component of the quality score.
type ScoreBreakdownItem struct {
	Label string
	Value int64
}

// ScoreBreakdown splits the base score by category (also used by the UI score
// simulator).
func (q *Quality) ScoreBreakdown() []ScoreBreakdownItem {
	resolution := int64(0)
	switch q.Resolution {
	case "2160p":
		resolution = 2000
	case "1080p":
		resolution = 1000
	case "720p":
		resolution = 400
	case "576p":
		resolution = 80
	case "480p":
		resolution = 40
	case "360p":
		resolution = 20
	}
	source := int64(0)
	switch q.Source {
	case "bluray":
		source = 300
	case "remux":
		// A REMUX is a lossless copy of the disc, so it must rank above a
		// BluRay re-encode. It used to be 280 (< bluray), a leftover that never
		// mattered while ParseQuality could not emit this source.
		source = 400
	case "webdl":
		source = 200
	case "webrip":
		source = 150
	case "hdtv":
		source = 50
	case "dvdrip":
		source = 20
	}
	codec := int64(0)
	switch q.Codec {
	case "h265", "x265", "hevc":
		codec = 200
	case "h264", "x264", "avc":
		codec = 50
	}
	audio := audioCodecPoints(q.Audio) + audioChannelPoints(q.AudioChannelLayout()) + audioObjectPoints(q.AudioObjectMetadata())
	hdr := int64(0)
	// HDR is additive with Dolby Vision. The parser tags a DV release with
	// HDR="DV", so a Dolby Vision release gets both the HDR and the DV bonus
	// unless the operator zeroes one of the two keys in the Punteggi tab.
	if q.HDR != "" {
		hdr = 100
	}
	dv := int64(0)
	if q.IsDV {
		dv = 300
	}
	proper := int64(0)
	if q.IsProper {
		proper = 75
	}
	repack := int64(0)
	if q.IsRepack {
		repack = 100
	}
	real := int64(0)
	if q.IsReal {
		real = 100
	}
	return []ScoreBreakdownItem{
		{"Risoluzione", resolution},
		{"Sorgente", source},
		{"Codec", codec},
		{"Audio", audio},
		{"HDR", hdr},
		{"Dolby Vision", dv},
		{"Proper", proper},
		{"Repack", repack},
		{"Real", real},
	}
}

// Score returns the total quality score.
func (q *Quality) Score() int64 {
	var total int64
	for _, item := range q.ScoreBreakdown() {
		total += item.Value
	}
	return total
}

// ScoreBreakdownWithSettings reports the same configured technical score used
// for decisions. The final item carries overrides and custom release groups,
// which cannot be represented by the fixed base categories alone.
func (q *Quality) ScoreBreakdownWithSettings(settings map[string]string) []ScoreBreakdownItem {
	items := q.ScoreBreakdown()
	if delta := q.ScoreWithSettings(settings) - q.Score(); delta != 0 {
		items = append(items, ScoreBreakdownItem{Label: "Modificatori configurati", Value: delta})
	}
	return items
}

// HasHDR reports any recognised HDR flavour (Dolby Vision included).
func (q *Quality) HasHDR() bool { return q.IsDV || q.HDR != "" }

// IsRemux reports whether the source is a full-quality REMUX.
func (q *Quality) IsRemux() bool { return q.Source == "remux" }

// ResolutionRank returns the numeric resolution rank.
func (q *Quality) ResolutionRank() int {
	switch q.Resolution {
	case "2160p":
		return 6
	case "1080p":
		return 5
	case "720p":
		return 4
	case "576p":
		return 3
	case "480p":
		return 2
	case "360p":
		return 1
	default:
		return 0
	}
}

// SourceRank returns the numeric source rank.
func (q *Quality) SourceRank() int {
	switch q.Source {
	case "bluray", "remux":
		return 5
	case "webdl":
		return 4
	case "webrip":
		return 3
	case "dvdrip":
		return 2
	case "hdtv":
		return 1
	default:
		return 0
	}
}

// AudioTier groups codecs by perceptual level: lossless, lossy high, lossy
// standard, compressed and low. The tier drives the per-title audio minimum.
func (q *Quality) AudioTier() int {
	value := strings.ToLower(strings.TrimSpace(q.Audio))
	switch {
	case strings.Contains(value, "dts-hd-hra"), strings.Contains(value, "dtshd-hra"):
		return 4
	case strings.Contains(value, "truehd"), strings.Contains(value, "dts-hd"),
		strings.Contains(value, "dtshd"), strings.Contains(value, "flac"),
		strings.Contains(value, "pcm"), strings.Contains(value, "lpcm"):
		return 5
	case strings.Contains(value, "dts"), strings.Contains(value, "ddp"),
		strings.Contains(value, "eac3"):
		return 4
	case strings.Contains(value, "ac3"), value == "5.1", value == "7.1":
		return 3
	case strings.Contains(value, "aac"), strings.Contains(value, "opus"):
		return 2
	case strings.Contains(value, "mp3"):
		return 1
	default:
		return 0
	}
}

// AudioChannelLayout returns the channel layout from the dedicated field, with
// a fallback for quality metadata written before the field existed.
func (q *Quality) AudioChannelLayout() string {
	if value := strings.TrimSpace(q.AudioChannels); value != "" && !strings.EqualFold(value, "unknown") {
		return value
	}
	value := strings.ToLower(q.Audio)
	switch {
	case strings.Contains(value, "7.1"):
		return "7.1"
	case strings.Contains(value, "5.1"):
		return "5.1"
	case strings.Contains(value, "2.0"):
		return "2.0"
	}
	return ""
}

// AudioObjectMetadata returns the dedicated object-audio field, with a
// fallback for quality metadata written before that field existed.
func (q *Quality) AudioObjectMetadata() string {
	if value := strings.TrimSpace(q.AudioObjects); value != "" {
		return value
	}
	return q.Audio
}

// AudioChannelRank orders channel layouts. 0 means unknown.
func (q *Quality) AudioChannelRank() int {
	switch q.AudioChannelLayout() {
	case "7.1":
		return 3
	case "5.1":
		return 2
	case "2.0":
		return 1
	default:
		return 0
	}
}

// AudioRank combines codec, channels and object audio. A candidate with a lower
// rank cannot win through a generic score delta: size, release group or swarm
// health must not hide an audio downgrade.
func (q *Quality) AudioRank() int {
	rank := q.AudioTier()*10 + q.AudioChannelRank()
	if audioObjectPoints(q.AudioObjectMetadata()) > 0 {
		rank++
	}
	return rank
}

func audioGroupName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case strings.Contains(value, "dts-hd-hra"), strings.Contains(value, "dtshd-hra"):
		return "high"
	case strings.Contains(value, "truehd"), strings.Contains(value, "dts-hd"),
		strings.Contains(value, "dtshd"), strings.Contains(value, "flac"),
		strings.Contains(value, "pcm"), strings.Contains(value, "lpcm"):
		return "lossless"
	case strings.Contains(value, "dts"), strings.Contains(value, "ddp"),
		strings.Contains(value, "eac3"):
		return "high"
	case strings.Contains(value, "ac3"), strings.Contains(value, "5.1"),
		strings.Contains(value, "7.1"):
		return "standard"
	case strings.Contains(value, "aac"), strings.Contains(value, "opus"):
		return "compressed"
	case strings.Contains(value, "mp3"):
		return "mp3"
	default:
		return ""
	}
}

func audioGroupPoints(group string) int64 {
	switch group {
	case "lossless":
		return 150
	case "high":
		return 90
	case "standard":
		return 50
	case "compressed":
		return 40
	case "mp3":
		return 10
	default:
		return 0
	}
}

// audioLegacyKeys returns the old per-codec keys still read when the grouped
// key has never been saved. This keeps existing custom weights working while the
// UI exposes one control per group.
func audioLegacyKeys(value string) []string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch audioGroupName(value) {
	case "lossless":
		switch {
		case strings.Contains(value, "truehd"):
			return []string{"score_audio_truehd"}
		case strings.Contains(value, "dts-hd"), strings.Contains(value, "dtshd"):
			return []string{"score_audio_dts-hd"}
		case strings.Contains(value, "flac"):
			return []string{"score_audio_flac"}
		case strings.Contains(value, "pcm"), strings.Contains(value, "lpcm"):
			return []string{"score_audio_pcm"}
		}
	case "high":
		switch {
		case strings.Contains(value, "hra"):
			return []string{"score_audio_dts-hd-hra"}
		case strings.Contains(value, "dts"):
			return []string{"score_audio_dts"}
		case strings.Contains(value, "ddp"), strings.Contains(value, "eac3"):
			return []string{"score_audio_ddp"}
		}
	case "standard":
		switch {
		case strings.Contains(value, "ac3"):
			return []string{"score_audio_ac3"}
		case strings.Contains(value, "7.1"):
			return []string{"score_audio_7.1"}
		case strings.Contains(value, "5.1"):
			return []string{"score_audio_5.1"}
		}
	case "compressed":
		switch {
		case strings.Contains(value, "aac"):
			return []string{"score_audio_aac"}
		case strings.Contains(value, "opus"):
			return []string{"score_audio_opus"}
		}
	case "mp3":
		return []string{"score_audio_mp3"}
	}
	return nil
}

func audioCodecPoints(value string) int64 {
	return audioGroupPoints(audioGroupName(value))
}

func audioChannelPoints(layout string) int64 {
	switch {
	case strings.Contains(layout, "7.1"):
		return 20
	case strings.Contains(layout, "5.1"):
		return 10
	default:
		return 0
	}
}

func audioObjectPoints(objects string) int64 {
	value := strings.ToLower(objects)
	if strings.Contains(value, "atmos") || strings.Contains(value, "dtsx") || strings.Contains(value, "dts:x") {
		return 20
	}
	return 0
}

// UpgradeReason explains why q may replace old, or "" when the change is not
// worth a re-download.
func (q *Quality) UpgradeReason(old *Quality, newScore, oldScore, minScoreDiff int64) string {
	newRes := q.ResolutionRank()
	oldRes := old.ResolutionRank()
	if newRes > oldRes {
		return "resolution"
	}
	// A resolution upgrade can justify a different audio mix; at the same (or
	// lower) resolution a worse audio codec is a downgrade, not an upgrade,
	// whatever size or release-group bonus the score carries.
	if q.AudioRank() < old.AudioRank() {
		return ""
	}
	if q.IsRemux() && !old.IsRemux() && newRes >= oldRes && newScore >= oldScore {
		return "remux"
	}
	// WEB-DL is a reliable source upgrade over the two broadcast/re-encode
	// variants even though WEBRip -> WEB-DL is only a 50-point score delta.
	// Do not treat WEBRip over HDTV as an upgrade: it is commonly a lossy
	// re-encode and its higher source rank alone is not sufficient evidence.
	if q.Source == "webdl" && (old.Source == "hdtv" || old.Source == "webrip") && newRes >= oldRes {
		return "source"
	}
	if q.HasHDR() && !old.HasHDR() && newRes >= oldRes {
		return "hdr"
	}
	if q.IsRepack && !old.IsRepack && newRes >= oldRes && q.SourceRank() >= old.SourceRank() {
		return "repack"
	}
	// A PROPER re-release fixes a defective release; treat it like a repack so
	// it is not rejected only because its score delta is below the threshold.
	if q.IsProper && !old.IsProper && newRes >= oldRes && q.SourceRank() >= old.SourceRank() {
		return "proper"
	}
	if old.Source == "unknown" && q.Source != "unknown" && q.sameNonSourceQuality(old) {
		return ""
	}
	if newScore > oldScore && newScore-oldScore >= minScoreDiff {
		return "score"
	}
	return ""
}

// sameNonSourceQuality compares the quality fields that are not the source.
func (q *Quality) sameNonSourceQuality(other *Quality) bool {
	return q.Resolution == other.Resolution &&
		q.Codec == other.Codec &&
		q.Audio == other.Audio &&
		q.AudioChannels == other.AudioChannels &&
		q.AudioObjects == other.AudioObjects &&
		q.HDR == other.HDR &&
		q.IsDV == other.IsDV &&
		q.IsRepack == other.IsRepack &&
		q.IsProper == other.IsProper &&
		q.IsReal == other.IsReal
}

// ScoreWithSettings applies user score overrides from the settings map.
func (q *Quality) ScoreWithSettings(settings map[string]string) int64 {
	score := q.Score()
	adjust := func(def int64, active bool, keys ...string) {
		if !active {
			return
		}
		// The first key is canonical. Later keys are legacy aliases and are
		// considered only when the canonical key was never set; this avoids a
		// DTS-HD release receiving a second DTS modifier.
		for _, key := range keys {
			if value, ok := settings[key]; ok {
				parsed, err := parseInt64(value)
				if err != nil {
					continue
				}
				score += parsed - def
				return
			}
		}
	}
	for _, item := range []struct {
		res string
		def int64
	}{
		{"2160p", 2000}, {"1080p", 1000}, {"720p", 400}, {"576p", 80},
		{"480p", 40}, {"360p", 20},
	} {
		adjust(item.def, q.Resolution == item.res, "score_res_"+item.res)
	}
	for _, item := range []struct {
		src string
		def int64
	}{
		{"bluray", 300}, {"remux", 400}, {"webdl", 200}, {"webrip", 150},
		{"hdtv", 50}, {"dvdrip", 20},
	} {
		adjust(item.def, q.Source == item.src, "score_source_"+item.src)
	}
	// One configurable key per parsed token. The parser normalizes the codec to
	// h265/h264, so x265/hevc/x264/avc are accepted spellings of the same token
	// but they no longer have their own keys.
	adjust(200, q.Codec == "h265" || q.Codec == "x265" || q.Codec == "hevc", "score_codec_h265")
	adjust(50, q.Codec == "h264" || q.Codec == "x264" || q.Codec == "avc", "score_codec_h264")

	// Audio has one configurable weight per group, plus separate channel and
	// object metadata weights. The legacy per-codec keys are still read when the
	// grouped key has never been saved, so existing custom weights survive the
	// transition without appearing as extra controls.
	if group := audioGroupName(q.Audio); group != "" {
		keys := append([]string{"score_audio_" + group}, audioLegacyKeys(q.Audio)...)
		adjust(audioGroupPoints(group), true, keys...)
	}
	switch q.AudioChannelLayout() {
	case "7.1":
		adjust(20, true, "score_channels_7.1")
	case "5.1":
		adjust(10, true, "score_channels_5.1")
	}
	objects := strings.ToLower(q.AudioObjectMetadata())
	if strings.Contains(objects, "atmos") || strings.Contains(objects, "dtsx") || strings.Contains(objects, "dts:x") {
		adjust(20, true, "score_audio_objects", "score_audio_atmos", "score_audio_dtsx")
	}
	// HDR and Dolby Vision are additive; a DV release carries HDR="DV", so it
	// receives both bonuses.
	adjust(300, q.IsDV, "score_bonus_dv")
	adjust(100, q.HDR != "", "score_bonus_hdr")
	adjust(75, q.IsProper, "score_bonus_proper")
	adjust(100, q.IsRepack, "score_bonus_repack")
	adjust(100, q.IsReal, "score_bonus_real")
	if group := strings.ToLower(strings.TrimSpace(q.Group)); group != "" && group != "unknown" {
		groupKey := "score_group_" + group
		if value, ok := settings[groupKey]; ok {
			if parsed, err := parseInt64(value); err == nil {
				score += parsed
			}
		}
	}
	return score
}

// ArchiveQualityIndex holds the best quality found on disk for each
// (season, episode) in a series folder.
type ArchiveQualityIndex struct {
	Best map[[2]int64]ArchiveQuality
}

// ArchiveQuality pairs a Quality with its score.
type ArchiveQuality struct {
	Quality Quality
	Score   int64
}

// BestFor returns the best archived quality for a season/episode.
func (a *ArchiveQualityIndex) BestFor(season, episode int64) (ArchiveQuality, bool) {
	if a == nil || a.Best == nil {
		return ArchiveQuality{}, false
	}
	value, ok := a.Best[[2]int64{season, episode}]
	return value, ok
}

// IsEmpty reports whether the index holds no entries.
func (a *ArchiveQualityIndex) IsEmpty() bool { return a == nil || len(a.Best) == 0 }

// LiveDownloads describes the torrents currently in the libtorrent session.
type LiveDownloads struct {
	Hashes   map[string]struct{}
	Episodes map[LiveEpisodeKey]struct{}
}

// LiveEpisodeKey is the (series, season, episode) key used by LiveDownloads.
type LiveEpisodeKey struct {
	Series  string
	Season  int64
	Episode int64
}

// IsEmpty reports whether nothing is currently downloading.
func (l *LiveDownloads) IsEmpty() bool {
	return l == nil || (len(l.Hashes) == 0 && len(l.Episodes) == 0)
}

// ApprovalContext carries extra data for the approval decision.
type ApprovalContext struct {
	Archive *ArchiveQualityIndex
	// MovieArchive is the best matching movie already on the archive volume.
	// Series use Archive; this covers a movie whose database row is missing.
	MovieArchive  *ArchiveQuality
	Live          *LiveDownloads
	ForbidUpgrade bool
	// UpgradeUntilScore stops upgrades once the existing copy scores at least
	// this much (0 = never stop); only a REPACK/PROPER of it is still taken.
	UpgradeUntilScore int64
	// ScoreSettings are the configured score weights (nil = the defaults): a
	// "score" upgrade must improve the quality score computed with them, not
	// only the size, subtitle or swarm-health parts of the release score.
	ScoreSettings map[string]string
	GapEpisode    bool
	// DryRun evaluates the decision without writing placeholders, torrent rows
	// or upgrades to the database. Used by the automatic cycle in dry-run mode.
	DryRun bool
	// StallAlternatives maps an episode whose download is stuck (parked for a
	// while) to the score of the stuck release. When present, a candidate may
	// be accepted even though the episode is already downloading, as long as it
	// stays within StallScoreDrop of the stuck release. This lets the search
	// try an alternative without removing the stuck torrent.
	StallAlternatives map[LiveEpisodeKey]int64
	// StallScoreDrop is how many points below the stuck release an alternative
	// may score (0 disables the tolerance).
	StallScoreDrop int64
}

// stallTolerance returns the stuck release score for key, if any.
func (c *ApprovalContext) stallTolerance(key LiveEpisodeKey) (int64, bool) {
	if c == nil || c.StallScoreDrop <= 0 || c.StallAlternatives == nil {
		return 0, false
	}
	score, ok := c.StallAlternatives[key]
	return score, ok
}

// AllowsStallAlternative reports whether a candidate with the given score may
// be started for an episode whose current download is stuck.
func (c *ApprovalContext) AllowsStallAlternative(key LiveEpisodeKey, score int64) bool {
	stuck, ok := c.stallTolerance(key)
	return ok && score >= stuck-c.StallScoreDrop
}

// StallAlternative applies the stall tolerance to a minimum score difference:
// while an episode is stuck, an upgrade may replace with fewer extra points.
func (c *ApprovalContext) StallAdjustedMinDiff(minScoreDiff int64, key LiveEpisodeKey) int64 {
	if _, ok := c.stallTolerance(key); !ok {
		return minScoreDiff
	}
	adjusted := minScoreDiff - c.StallScoreDrop
	if adjusted < 0 {
		adjusted = 0
	}
	return adjusted
}

// Release is a candidate release discovered from a source.
type Release struct {
	Title      string  `json:"title"`
	Magnet     string  `json:"magnet"`
	TorrentURL *string `json:"torrent_url"`
	// GUID is the stable identifier the indexer gives to a listing (the RSS or
	// Torznab <guid>, the Prowlarr guid). It is not a torrent infohash: the
	// archive uses it to recognise the same listing again when the download URL
	// is regenerated at every request and no magnet is exposed.
	GUID string `json:"guid,omitempty"`
	// InfoHash is the infohash an indexer declared for a release kept as a
	// `.torrent` link (the file carries the announce with the passkey, so no
	// magnet is built). It is only an identity, used to recognise the same
	// torrent from another source: the transfer still uses TorrentURL.
	InfoHash string `json:"info_hash,omitempty"`
	// Mirrors are other copies of the same torrent from other sources, folded
	// into this release by the duplicate merge. The acquisition falls back to
	// them when the link of this copy fails. Never persisted.
	Mirrors      []Release `json:"-"`
	Source       string    `json:"source"`
	Quality      Quality   `json:"quality"`
	Kind         string    `json:"kind"`
	Series       *string   `json:"series"`
	Season       *int64    `json:"season"`
	Episode      *int64    `json:"episode"`
	IsPack       bool      `json:"is_pack"`
	EpisodeRange []int64   `json:"episode_range"`
	// Network names the acquisition network: "" or "torrent" for BitTorrent,
	// "ed2k" for a file found on Kad/eD2k. The provider sets it so the UI can
	// label a result without guessing from the link.
	Network string `json:"network,omitempty"`
	// Ed2kLink is the `ed2k://|file|...` link of an eD2k result. A release has
	// exactly one transfer identity: Magnet, TorrentURL or Ed2kLink.
	Ed2kLink string `json:"ed2k_link,omitempty"`
	// Sources is the eD2k source count (the number of clients sharing the
	// file). It is deliberately not mapped onto Seeders: a Kad source count is
	// a different measure and must not feed the torrent score as if it were a
	// swarm.
	Sources int64 `json:"sources,omitempty"`
	// InProgressEpisodes is the episode count an "in progress" pack declares
	// ([IN CORSO][02/08] -> 2), 0 for every other release. Italian sites update
	// the listing before the .torrent, so the acquisition loop compares the two.
	InProgressEpisodes int64 `json:"in_progress_episodes,omitempty"`
	// SkippedEpisodes lists the episodes deliberately left out of the download
	// by the in-download inspection (file priority 0). The pack identity check
	// accepts the remaining files instead of demanding the whole declared range.
	SkippedEpisodes []int64 `json:"skipped_episodes,omitempty"`
	// AbsoluteEpisode/AbsoluteSeries hold an anime-style absolute number
	// ("[Group] Title - 1071") when the title has no season. The release stays
	// unclassified until it matches a series marked as anime, which turns the
	// number into season and episode.
	AbsoluteEpisode *int64  `json:"absolute_episode,omitempty"`
	AbsoluteSeries  *string `json:"absolute_series,omitempty"`
	Year            *int64  `json:"year"`
	// TmdbID is supplied by Prowlarr's native search response. Unlike a title
	// string, it identifies a movie unambiguously when the release name omits
	// its year.
	TmdbID       string    `json:"tmdb_id,omitempty"`
	DiscoveredAt time.Time `json:"discovered_at"`
	SizeBytes    int64     `json:"size_bytes"`
	Seeders      int64     `json:"seeders"`
	Peers        int64     `json:"peers"`
	// Score is the release score computed by the current configuration, filled
	// by the search endpoints so the UI can show and sort by it.
	Score int64 `json:"score,omitempty"`
}

// TorrentMeta stores the release associated with a torrent hash.
type TorrentMeta struct {
	Release Release `json:"release"`
}

// CycleSourceStat is how one source (a feed, an indexer or a web search engine)
// behaved in a search cycle. It is kept in the cycle history so the Salute page
// can show a source that keeps failing instead of only the live check.
type CycleSourceStat struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	OK        int    `json:"ok"`
	Fail      int    `json:"fail"`
	Results   int    `json:"results"`
	LastError string `json:"last_error,omitempty"`
}

// CycleStats summarises a scrape cycle.
type CycleStats struct {
	Scraped          int            `json:"scraped"`
	Candidates       int            `json:"candidates"`
	DownloadsStarted int            `json:"downloads_started"`
	GapsFilled       int            `json:"gaps_filled"`
	Errors           int            `json:"errors"`
	ErrorDetails     map[string]int `json:"error_details"`
	LastStartedAt    *time.Time     `json:"last_started_at"`
	// DurationSeconds is how long the cycle took, for the cycle history.
	DurationSeconds int `json:"duration_seconds"`
	// Sources is the per-source outcome of the cycle (feeds, indexers, engines).
	Sources []CycleSourceStat `json:"sources,omitempty"`
}

// CycleHistoryEntry is one stored cycle together with the wall-clock time the
// row was written (UTC, `2006-01-02 15:04:05`), as read back from the cycle
// history.
type CycleHistoryEntry struct {
	At string `json:"at"`
	CycleStats
}

// Error records a categorised cycle error.
func (c *CycleStats) Error(category string) {
	c.Errors++
	if c.ErrorDetails == nil {
		c.ErrorDetails = map[string]int{}
	}
	c.ErrorDetails[category]++
}

// TorrentView is the live status of a torrent in the session.
type TorrentView struct {
	Hash     string  `json:"hash"`
	Name     string  `json:"name"`
	Progress float64 `json:"progress"`
	State    string  `json:"state"`
	// Network is the acquisition network of this row: "" or "torrent" for a
	// torrent, "ed2k" for a file downloaded on eD2k. It is set by the
	// multi-engine router; a single torrent engine leaves it empty.
	Network string `json:"network,omitempty"`
	// Sources is the eD2k source count. It is not NumSeeds: eD2k sources are
	// clients sharing the whole file, not a BitTorrent swarm.
	Sources      int    `json:"sources,omitempty"`
	DownloadRate uint64 `json:"download_rate"`
	UploadRate   uint64 `json:"upload_rate"`
	// Total rates include protocol overhead; the fields above are payload-only.
	DownloadRateTotal uint64  `json:"download_rate_total"`
	UploadRateTotal   uint64  `json:"upload_rate_total"`
	SavePath          string  `json:"save_path"`
	DownloadLimit     int64   `json:"download_limit"`
	UploadLimit       int64   `json:"upload_limit"`
	AllTimeUpload     int64   `json:"all_time_upload"`
	AllTimeDownload   int64   `json:"all_time_download"`
	SeedingSeconds    int64   `json:"seeding_seconds"`
	QueuePosition     int     `json:"queue_position"`
	NumPeers          int     `json:"num_peers"`
	NumSeeds          int     `json:"num_seeds"`
	SeedRatio         float64 `json:"seed_ratio"`
	SeedDays          int64   `json:"seed_days"`
	HasMetadata       bool    `json:"has_metadata"`
	AutoManaged       bool    `json:"auto_managed"`
	TorrentVersion    string  `json:"torrent_version"`
	TotalSize         int64   `json:"total_size"`
	TotalDone         int64   `json:"total_done"`
	Stalled           bool    `json:"stalled"`
	// Pinned marks a torrent force-started outside the queue (libtorrent and
	// qBittorrent call it "force start").
	Pinned bool `json:"pinned"`
	// Rich status exposed by libtorrent that the UI and diagnostics can use.
	Error             string `json:"error"`
	CurrentTracker    string `json:"current_tracker"`
	NumComplete       int    `json:"num_complete"`
	NumIncomplete     int    `json:"num_incomplete"`
	NumConnections    int    `json:"num_connections"`
	ConnectCandidates int    `json:"connect_candidates"`
	FinishedSeconds   int64  `json:"finished_seconds"`
	ActiveSeconds     int64  `json:"active_seconds"`
	IsSeeding         bool   `json:"is_seeding"`
	Sequential        bool   `json:"sequential_download"`
	SuperSeeding      bool   `json:"super_seeding"`
	// Collections and Similar are the BEP 38 hints: the collections the torrent
	// belongs to, and the info hashes (hex) of torrents that might share files.
	Collections       []string `json:"collections,omitempty"`
	Similar           []string `json:"similar,omitempty"`
	UploadMode        bool     `json:"upload_mode"`
	ShareMode         bool     `json:"share_mode"`
	DistributedCopies float64  `json:"distributed_copies"`
	// Engine statistics the detail view surfaces. gx-torrent reports all of
	// them; qBittorrent carries ETA and the added/completed dates in its list.
	// EtaSeconds < 0 means "unknown" (seeding or stopped).
	EtaSeconds      int64 `json:"eta_seconds"`
	Private         bool  `json:"private"`
	FileCount       int   `json:"file_count"`
	PiecesTotal     int   `json:"pieces_total"`
	PiecesHave      int   `json:"pieces_have"`
	PiecesAvailable int   `json:"pieces_available"`
	PieceLength     int64 `json:"piece_length"`
	Wasted          int64 `json:"wasted"`
	Allocated       int64 `json:"allocated"`
	// AddedAt and CompletedAt are Unix seconds.
	AddedAt     int64 `json:"added_at"`
	CompletedAt int64 `json:"completed_at"`
	// Diagnosis is a short machine code explaining the torrent's situation
	// (downloading, metadata, dead_swarm, no_connected_seed, stalled, no_peers,
	// seeding, error). See GET /api/torrents/{hash}/why for the human reason.
	Diagnosis string `json:"diagnosis"`
}

// ProviderStatus is the escalating backoff state of one source.
type ProviderStatus struct {
	Provider          string `json:"provider"`
	Kind              string `json:"kind"`
	Level             int64  `json:"level"`
	DisabledTill      string `json:"disabled_till"`
	MostRecentFailure string `json:"most_recent_failure"`
	LastError         string `json:"last_error"`
	// URL, when known, turns the provider name into a link to its own site.
	URL string `json:"url,omitempty"`
	// UserMessage and SuggestedAction turn a transport-level error into the
	// operational information shown in the Health page.
	UserMessage     string `json:"user_message,omitempty"`
	SuggestedAction string `json:"suggested_action,omitempty"`
}

// TorrentEvent is a lifecycle event emitted by the torrent session.
type TorrentEvent struct {
	Kind     string `json:"kind"`
	Hash     string `json:"hash"`
	Name     string `json:"name"`
	SavePath string `json:"save_path"`
	Message  string `json:"message,omitempty"`
}

// PeerView is one peer of a torrent.
type PeerView struct {
	Address       string  `json:"address"`
	Client        string  `json:"client"`
	DownloadRate  uint64  `json:"download_rate"`
	UploadRate    uint64  `json:"upload_rate"`
	Pieces        int     `json:"pieces"`
	Seed          bool    `json:"seed"`
	Progress      float64 `json:"progress"`
	TotalUpload   int64   `json:"total_upload"`
	TotalDownload int64   `json:"total_download"`
	Incoming      bool    `json:"incoming"`
	Encrypted     bool    `json:"encrypted"`
	Utp           bool    `json:"utp"`
}

// TrackerView is one tracker of a torrent.
type TrackerView struct {
	URL              string `json:"url"`
	Tier             int    `json:"tier"`
	Message          string `json:"message,omitempty"`
	Fails            int    `json:"fails"`
	NextAnnounce     int    `json:"next_announce"`
	Verified         bool   `json:"verified"`
	ScrapeIncomplete int    `json:"scrape_incomplete"`
	ScrapeComplete   int    `json:"scrape_complete"`
	ScrapeDownloaded int    `json:"scrape_downloaded"`
}

// FileView is one file of a torrent.
type FileView struct {
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	Downloaded int64  `json:"downloaded"`
	Priority   int    `json:"priority"`
}

func parseInt64(value string) (int64, error) {
	var result int64
	var negative bool
	started := false
	for i, r := range value {
		if i == 0 && r == '-' {
			negative = true
			continue
		}
		if r < '0' || r > '9' {
			return 0, errInvalid
		}
		result = result*10 + int64(r-'0')
		started = true
	}
	if !started {
		return 0, errInvalid
	}
	if negative {
		return -result, nil
	}
	return result, nil
}

type simpleErr string

func (e simpleErr) Error() string { return string(e) }

const errInvalid simpleErr = "invalid integer"
