package gextto

// "In progress" season packs (the Italian sites write "[IN CORSO][02/08]")
// are listings whose torrent grows as episodes air. The title is updated
// before the .torrent is: a pack that declares two episodes can still hold
// only the first one. Downloading it wastes the whole pack to import nothing,
// because the episode it was chosen for is not inside it. This file reads the
// .torrent file list — already downloaded to resolve the link — and compares
// it with the episode count the title declares, so the download never starts.

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
	"github.com/zeebo/bencode"
)

// torrentFileList decodes the file names inside a .torrent without touching
// the payload. A single-file torrent yields its own name.
func torrentFileList(torrentPath string) ([]string, error) {
	data, err := os.ReadFile(torrentPath)
	if err != nil {
		return nil, err
	}
	var meta struct {
		Info struct {
			Name  string `bencode:"name"`
			Files []struct {
				Path []string `bencode:"path"`
			} `bencode:"files"`
		} `bencode:"info"`
	}
	if err := bencode.NewDecoder(bytes.NewReader(data)).Decode(&meta); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(meta.Info.Files))
	for _, file := range meta.Info.Files {
		if len(file.Path) == 0 {
			continue
		}
		names = append(names, path.Join(file.Path...))
	}
	if len(names) == 0 && strings.TrimSpace(meta.Info.Name) != "" {
		names = append(names, meta.Info.Name)
	}
	return names, nil
}

// torrentPackEpisodes returns the episodes the .torrent actually carries for
// the declared season, plus how many video files it holds.
func torrentPackEpisodes(torrentPath string, season int64) (map[int64]struct{}, int, error) {
	names, err := torrentFileList(torrentPath)
	if err != nil {
		return nil, 0, err
	}
	return packEpisodesFromPaths(names, season)
}

// packEpisodesFromPaths maps file names to the episodes they carry for the
// declared season. It works both on a .torrent file list and on the files the
// engine reports for a live torrent, so the pre-download and in-download
// checks share one parser. Multi-episode names ("Show.S01E01-E02.mkv")
// contribute every episode they cover.
func packEpisodesFromPaths(names []string, season int64) (map[int64]struct{}, int, error) {
	episodePattern, err := utils.CachedRegex(
		`(?i)(?:s(?P<season>\d{1,2})e|(?P<nseason>\d{1,2})x)(?P<episode>\d{1,4})`,
	)
	if err != nil {
		return nil, 0, err
	}
	rangeSuffix, err := utils.CachedRegex(`(?i)^[ ._-]*(?:-|–)[ ._-]*(?:e|ep)?[ ._-]*0*(\d{1,4})\b`)
	if err != nil {
		return nil, 0, err
	}
	multiSuffix, err := utils.CachedRegex(`(?i)^[ ._-]*(?:e|ep)[ ._-]*0*(\d{1,4})\b`)
	if err != nil {
		return nil, 0, err
	}
	// Some packs name the files just "01.mkv", "02.mkv" inside the season
	// folder: with no other identity, the number is the episode.
	bareEpisode, err := utils.CachedRegex(`(?i)^(?:e(?:p(?:isodio)?)?[ ._-]*)?0*(\d{1,4})$`)
	if err != nil {
		return nil, 0, err
	}
	found := map[int64]struct{}{}
	videos := 0
	for _, name := range names {
		if !isVideoExtension(name) {
			continue
		}
		videos++
		base := path.Base(name)
		match := episodePattern.FindStringSubmatch(base)
		if match == nil {
			if bare := bareEpisode.FindStringSubmatch(strings.TrimSuffix(base, path.Ext(base))); bare != nil {
				if value, err := strconv.ParseInt(bare[1], 10, 64); err == nil {
					found[value] = struct{}{}
				}
			}
			continue
		}
		seasonText := submatchNamed(episodePattern, match, "season")
		if seasonText == "" {
			seasonText = submatchNamed(episodePattern, match, "nseason")
		}
		if seasonText == "" {
			continue
		}
		parsedSeason, err := strconv.ParseInt(seasonText, 10, 64)
		if err != nil || parsedSeason != season {
			continue
		}
		first, err := strconv.ParseInt(submatchNamed(episodePattern, match, "episode"), 10, 64)
		if err != nil {
			continue
		}
		found[first] = struct{}{}
		// The rest of the name can carry the end of a range
		// ("S01E01-E02", "S01E01E02"): count those episodes too.
		if loc := episodePattern.FindStringIndex(base); loc != nil {
			rest := base[loc[1]:]
			for _, pattern := range []*regexp.Regexp{rangeSuffix, multiSuffix} {
				if extra := pattern.FindStringSubmatch(rest); extra != nil {
					if value, err := strconv.ParseInt(extra[1], 10, 64); err == nil {
						found[value] = struct{}{}
					}
					break
				}
			}
		}
	}
	return found, videos, nil
}

// packEpisodeDecision is the verdict for one episode inside a pack. Keeping
// the per-episode detail (not only the boolean) is what lets the logs say why
// a pack was kept or thrown away instead of just reporting a count.
type packEpisodeDecision struct {
	Episode  int64
	Keep     bool
	Reason   string
	Existing models.ArchiveQuality
	OnDisk   bool
}

// packEpisodeDecisions reports, episode by episode, whether the pack carries
// something worth downloading: a gap, an episode missing from the library, or
// one the release would really upgrade. A pack whose files are all already
// better on disk is useless even when its title promises the whole season.
func packEpisodeDecisions(
	release *models.Release,
	found map[int64]struct{},
	needed map[int64]struct{},
	archiveIndex *models.ArchiveQualityIndex,
	releaseScore, upgradeMinScoreDiff, upgradeUntilScore int64,
	forbidUpgrade bool,
	settings map[string]string,
) []packEpisodeDecision {
	if release == nil || release.Season == nil || len(found) == 0 {
		return nil
	}
	season := *release.Season
	episodes := make([]int64, 0, len(found))
	for episode := range found {
		episodes = append(episodes, episode)
	}
	sort.Slice(episodes, func(i, j int) bool { return episodes[i] < episodes[j] })

	decisions := make([]packEpisodeDecision, 0, len(episodes))
	for _, episode := range episodes {
		decision := packEpisodeDecision{Episode: episode}
		if _, ok := needed[episode]; ok {
			decision.Keep = true
			decision.Reason = "missing from the library"
			decisions = append(decisions, decision)
			continue
		}
		if forbidUpgrade {
			decision.Reason = "upgrades disabled for this title"
			decisions = append(decisions, decision)
			continue
		}
		existing, onDisk := archiveIndex.BestFor(season, episode)
		decision.Existing = existing
		decision.OnDisk = onDisk
		if !onDisk {
			decision.Keep = true
			decision.Reason = "not in the library"
			decisions = append(decisions, decision)
			continue
		}
		if upgradeUntilScore > 0 && existing.Score >= upgradeUntilScore {
			decision.Reason = fmt.Sprintf("score at or above the upgrade limit (%d)", upgradeUntilScore)
			decisions = append(decisions, decision)
			continue
		}
		// Use the same semantic rules as the acquisition decision: a DV/HDR
		// release or a WEB-DL over a WEBRip is an upgrade even when the numeric
		// score delta is small, while size, group and subtitles alone are not.
		if reason := upgradeReasonUntil(&release.Quality, &existing.Quality, releaseScore, existing.Score, upgradeMinScoreDiff, upgradeUntilScore, settings); reason != "" {
			decision.Keep = true
			decision.Reason = "upgrade: " + reason
			decisions = append(decisions, decision)
			continue
		}
		decision.Reason = "already equal or better"
		decisions = append(decisions, decision)
	}
	return decisions
}

// packEpisodesUseful reports whether at least one of the decisions keeps an
// episode: it is the compact form used where only the verdict matters.
func packEpisodesUseful(
	release *models.Release,
	found map[int64]struct{},
	needed map[int64]struct{},
	archiveIndex *models.ArchiveQualityIndex,
	releaseScore, upgradeMinScoreDiff, upgradeUntilScore int64,
	forbidUpgrade bool,
	settings map[string]string,
) bool {
	for _, decision := range packEpisodeDecisions(release, found, needed, archiveIndex, releaseScore, upgradeMinScoreDiff, upgradeUntilScore, forbidUpgrade, settings) {
		if decision.Keep {
			return true
		}
	}
	return false
}

// packDecisionDetails renders the per-episode comparison for the log, e.g.
// "S01E03 1080p h265 AC3 (1880) vs 1080p h264 AC3 (1550): already equal or
// better". limit caps the list so a 24-episode pack cannot flood the log.
func packDecisionDetails(season int64, releaseQuality models.Quality, releaseScore int64, decisions []packEpisodeDecision, limit int) string {
	if len(decisions) == 0 {
		return ""
	}
	if limit <= 0 || limit > len(decisions) {
		limit = len(decisions)
	}
	parts := make([]string, 0, limit+1)
	for index, decision := range decisions {
		if index >= limit {
			parts = append(parts, fmt.Sprintf("+%d more", len(decisions)-limit))
			break
		}
		label := fmt.Sprintf("S%02dE%02d", season, decision.Episode)
		if decision.Keep {
			if !decision.OnDisk {
				parts = append(parts, label+" "+decision.Reason)
				continue
			}
			parts = append(parts, fmt.Sprintf("%s %s (score %d) → %s (score %d): %s",
				label, qualityDetailText(decision.Existing.Quality), decision.Existing.Score,
				qualityDetailText(releaseQuality), releaseScore, decision.Reason))
			continue
		}
		if decision.OnDisk {
			parts = append(parts, fmt.Sprintf("%s %s (score %d) vs %s (score %d): %s",
				label, qualityDetailText(decision.Existing.Quality), decision.Existing.Score,
				qualityDetailText(releaseQuality), releaseScore, decision.Reason))
			continue
		}
		parts = append(parts, label+" "+decision.Reason)
	}
	return strings.Join(parts, " · ")
}

// incompletePackReason reports why an in-progress pack must not be downloaded.
// A pack is downloaded for specific episodes: as long as the .torrent holds at
// least one of them, the title's count is irrelevant and the pack is kept. It
// is skipped only when nothing inside is useful — the case of a listing that
// announces 8 episodes while the .torrent still holds only the first one,
// which the library already has at a better quality.
//
// neededEpisodes are the gaps the release would fill. archiveIndex is the real
// quality already on disk, so an episode inside the pack that would be a real
// upgrade is useful too. An episode with no archive entry is a gap in practice.
func incompletePackReason(
	torrentPath string,
	release *models.Release,
	neededEpisodes []int64,
	archiveIndex *models.ArchiveQualityIndex,
	releaseScore, upgradeMinScoreDiff, upgradeUntilScore int64,
	forbidUpgrade bool,
	settings map[string]string,
) (string, error) {
	if release == nil || release.Season == nil || release.InProgressEpisodes <= 0 {
		return "", nil
	}
	found, videos, err := torrentPackEpisodes(torrentPath, *release.Season)
	if err != nil {
		return "", err
	}
	if len(found) == 0 {
		if videos == 0 {
			return "the torrent holds no video file", nil
		}
		// File names without a season/episode marker cannot be matched: leave
		// the verdict to the post-download check rather than risk rejecting a
		// pack that is actually usable.
		return "", nil
	}
	needed := make(map[int64]struct{}, len(neededEpisodes))
	for _, episode := range neededEpisodes {
		needed[episode] = struct{}{}
	}
	decisions := packEpisodeDecisions(release, found, needed, archiveIndex, releaseScore, upgradeMinScoreDiff, upgradeUntilScore, forbidUpgrade, settings)
	for _, decision := range decisions {
		if decision.Keep {
			return "", nil
		}
	}
	var reason string
	if len(neededEpisodes) > 0 {
		reason = fmt.Sprintf("the torrent holds none of the missing episodes (%s): it has %d of %d declared, already present in equal or better quality",
			episodeList(neededEpisodes), len(found), release.InProgressEpisodes)
	} else {
		reason = fmt.Sprintf("no useful episode: the torrent holds %d of %d declared, already present in equal or better quality",
			len(found), release.InProgressEpisodes)
	}
	if details := packDecisionDetails(*release.Season, release.Quality, releaseScore, decisions, 8); details != "" {
		reason += " · " + details
	}
	return reason, nil
}

// episodeList renders a short list of episodes for the log.
func episodeList(episodes []int64) string {
	parts := make([]string, 0, len(episodes))
	for _, episode := range episodes {
		parts = append(parts, strconv.FormatInt(episode, 10))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ",")
}
