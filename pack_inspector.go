package gextto

// pack_inspector.go inspects a pack while it is downloading. The pre-download
// check can read the file list only when the release carries a `.torrent` URL;
// a magnet has no metadata until the engine has talked to its peers. This
// worker fills that gap: as soon as the file list exists it asks whether any
// episode inside is actually wanted (a gap, an episode missing from the
// library, or a real upgrade) and, when nothing is, removes the torrent with
// its partial data and blocklists it so it is not downloaded again.
//
// It is off by default and enabled with the `pack_inspection` setting: a
// misjudgement here throws away bytes already downloaded, so the operator
// chooses when to accept that trade.

import (
	"fmt"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

const (
	// packInspectionSetting enables the in-download inspection.
	packInspectionSetting = "pack_inspection"
	// packInspectionPrioritizeSetting makes a partially useful pack download
	// only the files that are actually wanted (0 priority on the rest).
	packInspectionPrioritizeSetting = "pack_inspection_prioritize"
	// packInspectionMetadataWait is how long a torrent without its file list
	// yet is skipped before asking the engine again.
	packInspectionMetadataWait = 30 * time.Second
	// packInspectionSettled is how long an inspected torrent is skipped. The
	// file list of a torrent never changes, so one verdict is enough.
	packInspectionSettled = 6 * time.Hour
)

// packInspectionCheck remembers until when a torrent needs no further look.
type packInspectionCheck struct {
	until time.Time
}

// packInspectionEnabled reports whether the operator enabled the setting.
func packInspectionEnabled(cfg *Config) bool {
	return cfg != nil && settingsBool(cfg, packInspectionSetting, false)
}

// packInspectionPrioritizeEnabled reports whether a useful pack should download
// only the files that carry a wanted episode.
func packInspectionPrioritizeEnabled(cfg *Config) bool {
	return packInspectionEnabled(cfg) && settingsBool(cfg, packInspectionPrioritizeSetting, false)
}

// InspectActivePacks checks the packs currently downloading against what the
// library already has. checked, when not nil, caches the verdicts so the 750 ms
// tick does not call the engine and the database for every settled pack.
func InspectActivePacks(cfg *Config, torrents TorrentEngine, db *Database, checked map[string]packInspectionCheck) {
	if cfg == nil || db == nil || torrents == nil || checked == nil || !packInspectionEnabled(cfg) || cfg.DryRun {
		return
	}
	now := time.Now()
	list := torrents.List()
	live := make(map[string]struct{}, len(list))
	for _, torrent := range list {
		live[torrent.Hash] = struct{}{}
	}
	for hash := range checked {
		if _, ok := live[hash]; !ok {
			delete(checked, hash)
		}
	}

	// Collect the candidates first: the gap query and the quality index are
	// paid only when at least one new pack needs inspecting.
	type candidate struct {
		torrent models.TorrentView
		release models.Release
	}
	candidates := make([]candidate, 0)
	for _, torrent := range list {
		if seen, ok := checked[torrent.Hash]; ok && now.Before(seen.until) {
			continue
		}
		settled := packInspectionCheck{until: now.Add(packInspectionSettled)}
		if torrent.Progress >= 99.99 {
			// The completion path decides; there is nothing left to abort.
			checked[torrent.Hash] = settled
			continue
		}
		meta, err := db.TorrentMeta(torrent.Hash)
		if err != nil || meta == nil {
			checked[torrent.Hash] = settled
			continue
		}
		release := meta.Release
		if release.Kind != "series" || !release.IsPack || release.Season == nil {
			checked[torrent.Hash] = settled
			continue
		}
		candidates = append(candidates, candidate{torrent: torrent, release: release})
	}
	if len(candidates) == 0 {
		return
	}

	gapCache := map[string]map[int64]struct{}{}
	for _, candidate := range candidates {
		files, ok, err := torrents.Files(candidate.torrent.Hash)
		if err != nil || !ok || len(files) == 0 {
			// Metadata has not arrived yet: ask again later.
			checked[candidate.torrent.Hash] = packInspectionCheck{until: now.Add(packInspectionMetadataWait)}
			continue
		}
		names := make([]string, 0, len(files))
		for _, file := range files {
			names = append(names, file.Path)
		}
		found, _, err := packEpisodesFromPaths(names, *candidate.release.Season)
		if err != nil || len(found) == 0 {
			// No season/episode identity in the names: the post-download check
			// is the only safe verdict.
			checked[candidate.torrent.Hash] = packInspectionCheck{until: now.Add(packInspectionSettled)}
			continue
		}
		seriesName := ""
		if candidate.release.Series != nil {
			seriesName = *candidate.release.Series
		}
		cacheKey := fmt.Sprintf("%s\x00%d", seriesName, *candidate.release.Season)
		needed, cached := gapCache[cacheKey]
		if !cached {
			needed = seriesGapsForSeason(db, seriesName, *candidate.release.Season)
			gapCache[cacheKey] = needed
		}
		index := db.SeriesQualityIndex(seriesName, *candidate.release.Season)
		forbidUpgrade := false
		if series := cfg.FindSeriesMatch(seriesName, candidate.release.Season); series != nil {
			forbidUpgrade = series.DisableUpgrades
		}
		score := cfg.AcquisitionScore(&candidate.release)
		if packEpisodesUseful(&candidate.release, found, needed, index, score, cfg.UpgradeMinScoreDiff, cfg.UpgradeUntilScore, forbidUpgrade) {
			if packInspectionPrioritizeEnabled(cfg) {
				if !applyPackFilePriorities(cfg, db, torrents, candidate.torrent.Hash, files, &candidate.release, needed, index, score, forbidUpgrade) {
					// Let the engine settle after the priority change before
					// asking again: a failed call is retried on the next pass.
					checked[candidate.torrent.Hash] = packInspectionCheck{until: now.Add(packInspectionMetadataWait)}
					continue
				}
			}
			checked[candidate.torrent.Hash] = packInspectionCheck{until: now.Add(packInspectionSettled)}
			continue
		}

		reason := fmt.Sprintf("nessun episodio utile: ne contiene %d, già presenti in libreria in qualità uguale o migliore", len(found))
		logging.Warn(fmt.Sprintf("🗑️ Pack in download scartato — «%s» · %s", candidate.release.Title, reason),
			"hash", candidate.torrent.Hash, "name", candidate.torrent.Name)
		_ = db.MarkTorrentError(candidate.torrent.Hash, reason)
		_ = db.Blocklist(&candidate.release, "pack_inspection_useless")
		if candidate.release.TorrentURL != nil {
			_ = db.QuarantineTorrentLink(*candidate.release.TorrentURL, reason, now, 7*24*time.Hour)
		}
		_ = db.ForgetRemovedTorrent(candidate.torrent.Hash)
		removed, removeErr := torrents.Remove(candidate.torrent.Hash, true)
		if removeErr != nil {
			logging.Warn("could not remove the useless pack", "hash", candidate.torrent.Hash, "error", removeErr.Error())
		} else if removed {
			logging.Info("useless pack removed during download",
				"hash", candidate.torrent.Hash, "name", candidate.torrent.Name,
				"downloaded_bytes", candidate.torrent.TotalDone)
		}
		delete(checked, candidate.torrent.Hash)
	}
}

// applyPackFilePriorities keeps only the files that carry a wanted episode: the
// engine skips the others (priority 0), so a pack that is useful for two
// episodes does not download the other seven. Sidecars and files whose names
// carry no episode identity stay at normal priority: they are small and a
// misnamed episode must not be lost.
func applyPackFilePriorities(
	cfg *Config,
	db *Database,
	torrents TorrentEngine,
	hash string,
	files []models.FileView,
	release *models.Release,
	needed map[int64]struct{},
	index *models.ArchiveQualityIndex,
	score int64,
	forbidUpgrade bool,
) bool {
	if cfg == nil || release == nil || release.Season == nil || len(files) == 0 {
		return true
	}
	priorities := make([]int32, len(files))
	skippedEpisodes := make([]int64, 0)
	skipped := 0
	var savedBytes int64
	for i, file := range files {
		priorities[i] = 1
		if !isVideoExtension(file.Path) {
			continue
		}
		found, _, err := packEpisodesFromPaths([]string{file.Path}, *release.Season)
		if err != nil || len(found) == 0 {
			continue
		}
		if packEpisodesUseful(release, found, needed, index, score, cfg.UpgradeMinScoreDiff, cfg.UpgradeUntilScore, forbidUpgrade) {
			continue
		}
		priorities[i] = 0
		skipped++
		savedBytes += file.Size
		for episode := range found {
			skippedEpisodes = append(skippedEpisodes, episode)
		}
	}
	if skipped == 0 {
		return true
	}
	ok, err := torrents.SetFilePriorities(hash, priorities)
	if err != nil {
		logging.Warn("could not set the pack file priorities", "hash", hash, "error", err.Error())
		return false
	}
	if !ok {
		return false
	}
	// The completion path reloads the release from the database: record there
	// which episodes are absent on purpose, or the pack identity check would
	// reject the files that were downloaded.
	if db != nil {
		if err := db.SetTorrentSkippedEpisodes(hash, skippedEpisodes); err != nil {
			logging.Warn("could not record the skipped pack episodes", "hash", hash, "error", err.Error())
		}
	}
	logging.Info(fmt.Sprintf("📥 Pack parzialmente utile: %d file su %d · risparmio %s",
		len(files)-skipped, len(files), logging.HumanBytesI64(savedBytes)), "hash", hash)
	return true
}

// seriesGapsForSeason returns the known missing episodes of one season.
func seriesGapsForSeason(db *Database, seriesName string, season int64) map[int64]struct{} {
	needed := map[int64]struct{}{}
	if db == nil || seriesName == "" {
		return needed
	}
	gaps, err := db.ArchiveGapsForSeries(seriesName)
	if err != nil {
		return needed
	}
	for _, gap := range gaps {
		if gap[0] == season {
			needed[gap[1]] = struct{}{}
		}
	}
	return needed
}
