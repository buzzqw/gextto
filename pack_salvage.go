package gextto

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

// Salvage of an abandoned season pack: when a pack stalls for good, the
// episodes whose files are already complete (every piece verified) are imported
// into the library like those of a finished pack, before the torrent and its
// partial data are removed. Only the missing episodes are searched again.

// torrentFileLister is implemented by every engine (TorrentEngine.Files); the
// stall monitor only holds the narrower TorrentSession.
type torrentFileLister interface {
	Files(hash string) ([]models.FileView, bool, error)
}

// tev_salvagePackEpisodes imports the complete episodes of the abandoned pack
// torrent and returns how many reached the library, and whether previous
// library copies of the other episodes were restored from the upgrade backup.
// Any failure leaves the usual give-up path untouched (nothing is salvaged).
func tev_salvagePackEpisodes(cfg *Config, torrents TorrentSession, db *Database, torrent models.TorrentView, meta *models.TorrentMeta) (int, bool) {
	if meta == nil {
		return 0, false
	}
	release := meta.Release
	if release.Kind != "series" || !release.IsPack || release.Series == nil || release.Season == nil {
		return 0, false
	}
	lister, ok := torrents.(torrentFileLister)
	if !ok {
		return 0, false
	}
	files, _, err := lister.Files(torrent.Hash)
	if err != nil {
		logging.Debug("pack salvage: file list unavailable", "hash", torrent.Hash, "error", err)
		return 0, false
	}
	complete := completeTorrentFiles(torrent.SavePath, files)
	if len(complete) == 0 {
		return 0, false
	}
	normalizeReleaseSeries(cfg, &release)
	if _, hasArchive := ConfiguredDestinationFor(&release, cfg); !hasArchive {
		// Without a library folder a finished pack stays in the download
		// folder, which the give-up is about to delete: nothing to keep it in.
		return 0, false
	}
	destination, _ := DestinationFor(&release, cfg)
	source := CompletionPath(&models.TorrentEvent{Hash: torrent.Hash, Name: torrent.Name, SavePath: torrent.SavePath})
	matching, err := MatchingPackFiles(source, &release)
	if err != nil {
		logging.Debug("pack salvage: cannot list the pack files", "hash", torrent.Hash, "error", err)
		return 0, false
	}
	ready := []PackSourceFile{}
	for _, file := range matching {
		if _, ok := complete[filepath.Clean(file.Path)]; ok {
			ready = append(ready, file)
		}
	}
	if len(ready) == 0 {
		return 0, false
	}
	guard := AcquireArchiveImport(release.Series)
	defer guard.Release()
	tmdb := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())
	destinationFiles, _ := VideoFiles(destination)
	score := cfg.ReleaseScore(&release)
	entries := []PackEpisode{}
	for index := range ready {
		file := ready[index]
		placed, ok, err := StagePackFile(&file, source, destination, destinationFiles, cfg, score)
		if err != nil {
			logging.Warn("pack salvage: episode not imported", "hash", torrent.Hash, "file", filepath.Base(file.Path), "error", err)
			continue
		}
		if !ok {
			continue
		}
		processed, err := ProcessPackFiles(context.Background(), []PackInput{{Path: placed, Source: file}}, &release, cfg, tmdb)
		if err != nil {
			logging.Warn("pack salvage: episode not imported", "hash", torrent.Hash, "file", filepath.Base(file.Path), "error", err)
			continue
		}
		for _, item := range processed {
			if item.Discarded {
				continue
			}
			entries = append(entries, PackEpisode{Episode: item.Episode, Path: item.Path, SizeBytes: item.SizeBytes, Score: item.QualityScore})
		}
	}
	if len(entries) == 0 {
		return 0, false
	}
	restored, err := db.SalvagePackEpisodes(torrent.Hash, &release, entries)
	if err != nil {
		logging.Warn("pack salvage: the imported episodes could not be recorded", "hash", torrent.Hash, "error", err)
		return 0, false
	}
	logging.Info(fmt.Sprintf("🛟 «%s»: %s already complete, added to the library before giving up (%s)",
		torrent.Name, countLabel(len(entries), "episode was", "episodes were"), salvagedEpisodeCodes(*release.Season, entries)),
		"hash", torrent.Hash)
	return len(entries), restored
}

// completeTorrentFiles returns the absolute paths of the files whose bytes are
// all verified. Engines report paths relative to the save path.
func completeTorrentFiles(savePath string, files []models.FileView) map[string]struct{} {
	complete := map[string]struct{}{}
	for _, file := range files {
		if file.Size <= 0 || file.Downloaded < file.Size || strings.TrimSpace(file.Path) == "" {
			continue
		}
		relative := filepath.FromSlash(file.Path)
		if filepath.IsAbs(relative) || hasParentComponent(relative) {
			continue
		}
		complete[filepath.Clean(filepath.Join(savePath, relative))] = struct{}{}
	}
	return complete
}

// salvagedEpisodeCodes lists the salvaged episodes as S01E03, S01E04, …
func salvagedEpisodeCodes(season int64, entries []PackEpisode) string {
	episodes := make([]int64, 0, len(entries))
	for _, entry := range entries {
		episodes = append(episodes, entry.Episode)
	}
	sort.Slice(episodes, func(i, j int) bool { return episodes[i] < episodes[j] })
	codes := make([]string, 0, len(episodes))
	for _, episode := range episodes {
		codes = append(codes, fmt.Sprintf("S%02dE%02d", season, episode))
	}
	return strings.Join(codes, ", ")
}

// SalvagePackEpisodes records the episodes imported from an abandoned pack.
// Upgrade backups are restored only for the episodes that were NOT salvaged
// (their previous library copy is still the valid one); the salvaged ones keep
// the new file. The backup row is consumed, so the RestoreUpgrade of the
// give-up that follows has nothing left to undo, and MarkTorrentError then
// clears only the placeholders of the episodes still missing. It reports
// whether any previous copy was restored.
func (d *Database) SalvagePackEpisodes(hash string, release *models.Release, episodes []PackEpisode) (bool, error) {
	if release.Series == nil || release.Season == nil {
		return false, errors.New("pack salvage needs series and season")
	}
	normalized := strings.ToLower(strings.TrimSpace(hash))
	tx, err := d.db.Begin()
	if err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var seriesID int64
	if err := tx.QueryRow("SELECT id FROM series WHERE name=?1", *release.Series).Scan(&seriesID); err != nil {
		return false, err
	}
	restored := false
	season := *release.Season
	salvaged := map[int64]struct{}{}
	for _, episode := range episodes {
		salvaged[episode.Episode] = struct{}{}
	}
	var payload sql.NullString
	backupErr := tx.QueryRow("SELECT payload_json FROM upgrade_backup WHERE new_hash=?1", normalized).Scan(&payload)
	if backupErr != nil && !errors.Is(backupErr, sql.ErrNoRows) {
		return false, backupErr
	}
	if backupErr == nil && payload.Valid {
		backups, err := decodeUpgradeBackups(payload.String)
		if err != nil {
			return false, err
		}
		for _, backup := range backups {
			if backup.Kind == "series" && backup.Season != nil && *backup.Season == season && backup.Episode != nil {
				if _, kept := salvaged[*backup.Episode]; kept {
					continue
				}
			}
			if err := restoreUpgradeBackupTx(tx, backup); err != nil {
				return false, err
			}
			restored = true
		}
		if _, err := tx.Exec("DELETE FROM upgrade_backup WHERE new_hash=?1", normalized); err != nil {
			return false, err
		}
	}
	now := nowSQLite()
	for _, episode := range episodes {
		if _, err := tx.Exec(`INSERT INTO episodes(series_id,season,episode,title,quality_score,downloaded_at,archive_path,size_bytes)
			VALUES (?1,?2,?3,?4,?5,?6,?7,?8)
			ON CONFLICT(series_id,season,episode) DO UPDATE SET downloaded_at=excluded.downloaded_at,archive_path=excluded.archive_path,
			size_bytes=excluded.size_bytes,quality_score=MAX(excluded.quality_score,episodes.quality_score)`,
			seriesID, season, episode.Episode, release.Title, episode.Score, now, episode.Path, episode.SizeBytes); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	committed = true
	return restored, nil
}
