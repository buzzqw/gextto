package gextto

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

func manualTorrentRemovalInfo(s *AppState, hash string) (name, state string, hasMetadata bool) {
	name = ""
	if s != nil && s.activeEngine() != nil {
		for _, torrent := range s.activeEngine().List() {
			if !strings.EqualFold(torrent.Hash, hash) {
				continue
			}
			name = strings.TrimSpace(torrent.Name)
			state = torrent.State
			hasMetadata = torrent.HasMetadata
			break
		}
	}
	if name == "" && s != nil && s.db != nil {
		if metadata, err := s.db.TorrentMeta(hash); err == nil && metadata != nil {
			name = strings.TrimSpace(metadata.Release.Title)
		}
	}
	if name == "" {
		name = hash
	}
	return name, state, hasMetadata
}

// logManualTorrentRemoval records user removals even when the engine has not
// received torrent metadata yet, which is otherwise easy to miss in the log.
// trashTarget is the trash location the files were moved to, empty when they
// were kept or deleted outright.
func logManualTorrentRemoval(hash string, deleteFiles bool, trashTarget, name, state string, hasMetadata bool) {
	files := "its files are kept"
	if trashTarget != "" {
		files = "its files were moved to the trash: " + trashTarget
	} else if deleteFiles {
		files = "its files were deleted"
	}
	logging.Info(fmt.Sprintf("🗑️ You removed «%s» from the download list; %s", name, files))
	logging.Debug("torrent removed by user", "hash", hash, "state", state, "metadata_available", hasMetadata)
}

// logAutomaticTorrentRemoval records a removal that gextto performed on its own
// as part of end-of-seed archiving or cleanup. It stays at DEBUG so the single
// friendly archive summary line remains the headline; the cleanup endpoint
// already reports its own removed/skipped totals at INFO.
func logAutomaticTorrentRemoval(hash, name, state string) {
	logging.Debug("completed download removed from the session", "hash", hash, "name", name, "state", state)
}

// SafeRemoveTorrent safely removes a torrent from the active engine.
// It ensures that:
//  1. Files in the permanent archive library (NAS) are NEVER deleted when the user
//     didn't explicitly request deletion, even if the torrent payload is marked disposable.
//  2. When file deletion is requested and trash is configured, files are moved
//     to the trash directory instead of being permanently unlinked.
//  3. Engine removal is properly recorded in the database.
func SafeRemoveTorrent(s *AppState, cfg *Config, hash string, requestedDeleteFiles bool) (bool, error) {
	if s == nil || s.activeEngine() == nil {
		return false, fmt.Errorf("torrent engine not available")
	}
	hash = strings.ToLower(strings.TrimSpace(hash))
	var targetTorrent *models.TorrentView
	for _, t := range s.activeEngine().List() {
		if strings.EqualFold(t.Hash, hash) {
			tCopy := t
			targetTorrent = &tCopy
			break
		}
	}

	deleteFiles := requestedDeleteFiles

	engineDeleteFiles := deleteFiles
	trashTarget := ""
	if targetTorrent != nil {
		source := CompletionPath(&models.TorrentEvent{
			Kind:     "torrent_finished",
			Hash:     targetTorrent.Hash,
			Name:     targetTorrent.Name,
			SavePath: targetTorrent.SavePath,
		})

		if !deleteFiles {
			engineDeleteFiles = false
		} else if cfg != nil && cfg.TrashPath != nil && strings.TrimSpace(*cfg.TrashPath) != "" && cfg.CleanupAction != "delete" {
			if _, statErr := os.Stat(source); statErr == nil {
				if target, moveErr := MoveToTrash(source, *cfg.TrashPath); moveErr != nil {
					logging.Error("could not move removed torrent source to trash",
						"hash", hash, "source", source, "error", moveErr.Error())
				} else {
					trashTarget = target
					logging.Debug("removed torrent source moved to trash",
						"hash", hash, "source", source, "trash", target)
				}
			}
			engineDeleteFiles = false
		}
	}

	removalName, removalState, removalHasMetadata := manualTorrentRemovalInfo(s, hash)
	removed, err := s.activeEngine().Remove(hash, engineDeleteFiles)
	if err == nil && removed {
		logManualTorrentRemoval(hash, deleteFiles, trashTarget, removalName, removalState, removalHasMetadata)
		if s.db != nil {
			_ = s.db.MarkTorrentRemoved(hash)
			_ = s.db.ForgetRemovedTorrent(hash)
		}
	}
	return removed, err
}

func moveDirAcrossDevices(source, target string) error {
	err := os.Rename(source, target)
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.EXDEV) {
		defer beginFileOperation(source)()
		if err := copyTree(source, target); err != nil {
			return err
		}
		return os.RemoveAll(source)
	}
	return err
}

// foreignTorrentAlreadyAnnounced reports whether a torrent without release
// metadata had its completion announced already: a foreign torrent is announced
// ("added manually") when it finishes downloading, and a comics torrent by the
// comics module, so archiving it is not a new completion.
func foreignTorrentAlreadyAnnounced(s *AppState, hash string) bool {
	if s.db != nil {
		if status, err := s.db.TorrentStatus(hash); err == nil && status != nil && *status == "completed" {
			return true
		}
	}
	if s.comics != nil {
		if comic, err := s.comics.Torrent(hash); err == nil && comic != nil {
			return true
		}
	}
	return false
}

func notifyArchivedTorrent(s *AppState, hash string, release *models.Release, finalPath string, sizeBytes int64, torrentName string) {
	if s == nil || s.notifier == nil {
		return
	}
	var durationSeconds *int64
	var averageSpeedBps *int64
	if s.db != nil {
		if times, err := s.db.TorrentTimes(hash); err == nil && times != nil {
			if created, ok := utils.ParseTimestamp(times.CreatedAt); ok {
				completed := time.Now().UTC()
				if times.CompletedAt != nil {
					if parsed, ok := utils.ParseTimestamp(*times.CompletedAt); ok {
						completed = parsed
					}
				}
				seconds := int64(completed.Sub(created).Seconds())
				if seconds < 1 {
					seconds = 1
				}
				speed := int64(float64(sizeBytes) / float64(seconds))
				durationSeconds = &seconds
				averageSpeedBps = &speed
			}
		}
	}
	title := torrentName
	var kindValue, seriesValue, seasonValue, episodeValue any
	if release != nil {
		title = release.Title
		kindValue = release.Kind
		if release.Series != nil {
			seriesValue = *release.Series
		}
		if release.Season != nil {
			seasonValue = *release.Season
		}
		if release.Episode != nil {
			episodeValue = *release.Episode
		}
	}
	var replacedTitleValue any
	if s.db != nil {
		if replacedName, ok := s.db.UpgradeReplacedInfo(hash); ok {
			replacedTitleValue = replacedName
		}
	}
	if err := s.notifier.NotifyEvent("torrent_completed", map[string]any{
		"hash":              hash,
		"name":              torrentName,
		"title":             title,
		"kind":              kindValue,
		"series":            seriesValue,
		"season":            seasonValue,
		"episode":           episodeValue,
		"path":              finalPath,
		"size_bytes":        sizeBytes,
		"duration_seconds":  durationSeconds,
		"average_speed_bps": averageSpeedBps,
		"replaced_title":    replacedTitleValue,
		"manual":            release == nil,
	}); err != nil {
		logging.Warn("archive completion notification failed", "hash", hash, "title", title, "error", err)
	} else {
		logging.Debug("completion notification sent", "hash", hash, "title", title)
	}

	if release != nil && s.db != nil && finalPath != "" {
		if info, probeErr := ProbeResult(finalPath); probeErr != nil {
			logging.Warn("MediaInfo probe failed for archived file", "title", release.Title, "path", finalPath, "error", probeErr)
		} else if err := s.db.SetMediaInfo(release, &info); err != nil {
			logging.Warn("could not save MediaInfo for archived file", "title", release.Title, "path", finalPath, "error", err)
		} else {
			logging.Debug("saved media details for archived file",
				"title", release.Title, "path", finalPath, "resolution", info.Resolution(), "hdr", info.HDR, "bit_depth", info.BitDepth)
		}
	}
}

// ArchiveAndRemoveTorrent stops a completed torrent from seeding, moves its
// files to the configured NAS archive destination (or default download dir if
// none is configured), performs filename renaming and quality checks, updates
// database records, and unloads the torrent from the engine.
func ArchiveAndRemoveTorrent(s *AppState, cfg *Config, hash string) (bool, error) {
	if s == nil || s.activeEngine() == nil {
		return false, fmt.Errorf("torrent engine not available")
	}
	hash = strings.ToLower(strings.TrimSpace(hash))
	var targetTorrent *models.TorrentView
	for _, t := range s.activeEngine().List() {
		if strings.EqualFold(t.Hash, hash) {
			tCopy := t
			targetTorrent = &tCopy
			break
		}
	}
	if targetTorrent == nil {
		return false, fmt.Errorf("torrent %s non trovato nella sessione attiva", hash)
	}

	// Guard against archiving an incomplete download
	isCompletedDB := false
	if s.db != nil {
		if st, _ := s.db.TorrentStatus(hash); st != nil && *st == "completed" {
			isCompletedDB = true
		}
	}
	done := (targetTorrent.Progress >= 99.99 && targetTorrent.TotalSize > 0 && targetTorrent.TotalDone >= targetTorrent.TotalSize) || isCompletedDB
	if !done {
		return false, fmt.Errorf("impossibile archiviare un download non completato (progresso: %.1f%%)", targetTorrent.Progress)
	}

	// Fetch release metadata from DB if present
	var release *models.Release
	if s.db != nil {
		if meta, err := s.db.TorrentMeta(hash); err == nil && meta != nil {
			relCopy := meta.Release
			release = &relCopy
			normalizeReleaseSeries(cfg, release)
		}
	}

	// Check if already completed and archived on disk. A post-seed relocation
	// can drop the completed file straight into the archive folder under its
	// original torrent name when the import step was interrupted (for example by
	// a restart). That copy was never renamed nor quality-checked, so it must not
	// take the "already archived" shortcut: fall through to the full
	// finalization, which renames it and runs the quality checks on the file
	// already in place.
	if s.db != nil {
		if processed, err := s.db.TorrentProcessed(hash); err == nil && processed != nil && strings.TrimSpace(*processed) != "" {
			if _, statErr := os.Stat(*processed); statErr == nil {
				finalized := true
				if release != nil && !release.IsPack && strings.EqualFold(filepath.Base(filepath.Clean(*processed)), filepath.Base(strings.TrimSpace(targetTorrent.Name))) {
					finalized = false
					logging.Debug("archived copy still has the download name; renaming and running quality checks",
						"hash", hash, "name", targetTorrent.Name, "path", *processed)
				}
				if finalized {
					source := CompletionPath(&models.TorrentEvent{
						Kind:     "torrent_finished",
						Hash:     targetTorrent.Hash,
						Name:     targetTorrent.Name,
						SavePath: targetTorrent.SavePath,
					})
					inRamdisk := false
					if ramdisk := cfg.RamdiskDir(); ramdisk != nil {
						inRamdisk = PathOnRamdisk(targetTorrent.SavePath, *ramdisk)
					}
					inTemp := cfg.LibtorrentTempDir != nil && SamePath(targetTorrent.SavePath, *cfg.LibtorrentTempDir)
					if (inRamdisk || inTemp) && !SamePath(source, *processed) {
						_ = os.RemoveAll(source)
					}
					// Already finalized on disk: its completion was announced when
					// the archive copy was made, so only the session entry goes.
					removalName, removalState, _ := manualTorrentRemovalInfo(s, hash)
					removed, err := s.activeEngine().Remove(hash, false)
					if err == nil && removed {
						logAutomaticTorrentRemoval(hash, removalName, removalState)
					}
					_ = s.db.MarkTorrentRemoved(hash)
					_ = s.db.ForgetRemovedTorrent(hash)
					return true, nil
				}
			}
		}
	}

	// Pause immediately to stop network activity/seeding
	_, _ = s.activeEngine().Pause(hash)

	source := CompletionPath(&models.TorrentEvent{
		Kind:     "torrent_finished",
		Hash:     targetTorrent.Hash,
		Name:     targetTorrent.Name,
		SavePath: targetTorrent.SavePath,
	})

	sourceInfo, err := os.Stat(source)
	if err != nil {
		return false, fmt.Errorf("file sorgente non trovato: %s: %w", source, err)
	}

	// Resolve destination: configured archive destination (NAS) or LibtorrentDir fallback
	destination := cfg.LibtorrentDir
	if release != nil {
		if dest, ok := DestinationFor(release, cfg); ok && strings.TrimSpace(dest) != "" {
			destination = dest
		}
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return false, fmt.Errorf("impossibile creare la cartella di destinazione: %w", err)
	}

	removalName, removalState, _ := manualTorrentRemovalInfo(s, hash)

	// Case 1: Season pack
	if release != nil && release.IsPack {
		matching, err := MatchingPackFiles(source, release)
		if err != nil {
			return false, err
		}
		if len(matching) == 0 {
			return false, fmt.Errorf("nessun file corrispondente alla stagione trovato nel season pack: %s", source)
		}
		guard := AcquireArchiveImport(release.Series)
		defer guard.Release()

		destinationFiles, _ := VideoFiles(destination)
		processed := []PackFileResult{}
		for index := range matching {
			file := matching[index]
			placed, ok, err := StagePackFile(&file, source, destination, destinationFiles, cfg, cfg.ReleaseScore(release))
			if err != nil {
				return false, err
			}
			if !ok {
				processed = append(processed, PackFileResult{
					Episode:   file.Episode,
					Path:      file.Path,
					Discarded: true,
				})
				continue
			}
			partial, err := ProcessPackFiles(context.Background(), []PackInput{{Path: placed, Source: file}}, release, cfg, s.tmdb)
			if err != nil {
				return false, err
			}
			processed = append(processed, partial...)
		}
		allDiscarded := tev_allPackFilesDiscarded(processed)
		if allDiscarded {
			if s.db != nil {
				_ = s.db.MarkTorrentError(hash, "season pack inferior to existing files")
			}
			tev_discardCompletedSource(cfg, s.db, s.activeEngine(), &models.TorrentEvent{
				Hash:     hash,
				Name:     targetTorrent.Name,
				SavePath: targetTorrent.SavePath,
			}, "season pack inferior to existing files")
			return false, fmt.Errorf("tutti gli episodi del season pack sono risultati inferiori ai file esistenti")
		}
		entries := []PackEpisode{}
		episodes := []any{}
		for _, item := range processed {
			if item.Discarded {
				continue
			}
			entries = append(entries, PackEpisode{
				Episode:   item.Episode,
				Path:      item.Path,
				SizeBytes: item.SizeBytes,
				Score:     item.QualityScore,
			})
			episodes = append(episodes, map[string]any{
				"series":  release.Series,
				"season":  release.Season,
				"episode": item.Episode,
				"path":    item.Path,
			})
		}
		size, _ := SizeOfPath(destination)
		if s.db != nil {
			_ = s.db.MarkPackCompleted(release, entries, destination, size)
		}
		archivedBytes := int64(0)
		for _, item := range entries {
			archivedBytes += item.SizeBytes
		}
		if archivedBytes <= 0 {
			archivedBytes = size
		}
		logging.Info(fmt.Sprintf("📁 Archived — «%s» · %d episodes · %s · saved to %s",
			release.Title, len(entries), logging.HumanBytesI64(archivedBytes), destination))
		if s.notifier != nil {
			discardedList := []any{}
			for _, item := range processed {
				if item.Discarded {
					discardedList = append(discardedList, map[string]any{
						"episode": item.Episode,
						"path":    item.Path,
						"name":    filepath.Base(item.Path),
					})
				}
			}
			if nErr := s.notifier.NotifyEvent("season_pack_completed", map[string]any{
				"series":          release.Series,
				"season":          release.Season,
				"title":           release.Title,
				"path":            destination,
				"size_bytes":      size,
				"new_count":       len(episodes),
				"discarded_count": len(discardedList),
				"episodes":        episodes,
				"discarded":       discardedList,
			}); nErr != nil {
				logging.Warn("archive pack completion notification failed", "hash", hash, "name", targetTorrent.Name, "error", nErr)
			} else {
				logging.Debug("season pack completion notification sent", "hash", hash, "name", targetTorrent.Name)
			}
		}
		// Clean up source on ramdisk/temp if needed
		inRamdisk := false
		if ramdisk := cfg.RamdiskDir(); ramdisk != nil {
			inRamdisk = PathOnRamdisk(targetTorrent.SavePath, *ramdisk)
		}
		inTemp := cfg.LibtorrentTempDir != nil && SamePath(targetTorrent.SavePath, *cfg.LibtorrentTempDir)
		if inRamdisk || inTemp {
			_ = os.RemoveAll(source)
		}
		removed, rErr := s.activeEngine().Remove(hash, false)
		if rErr == nil && removed {
			logAutomaticTorrentRemoval(hash, removalName, removalState)
		}
		if s.db != nil {
			_ = s.db.MarkTorrentRemoved(hash)
			_ = s.db.ForgetRemovedTorrent(hash)
		}
		return true, nil
	}

	// Case 2: Single Episode or Movie
	if release != nil && (release.Kind == "series" || release.Kind == "movie") {
		var video string
		if release.Kind == "movie" {
			v, err := resolveMovieCompletedFile(source)
			if err != nil {
				return false, err
			}
			video = v
		} else {
			v, err := tev_resolveEpisodeCompletedFile(source, release)
			if err != nil {
				return false, err
			}
			video = v
		}

		target := filepath.Join(destination, filepath.Base(video))
		if !SamePath(video, target) {
			if err := ValidateDestinationFrom(video, destination); err != nil {
				return false, err
			}
			if err := moveAcrossDevices(video, target); err != nil {
				return false, fmt.Errorf("spostamento del file nella destinazione non riuscito: %w", err)
			}
			if sourceInfo.IsDir() {
				ApplySidecarsTo(video, target, cfg)
				inRamdisk := false
				if ramdisk := cfg.RamdiskDir(); ramdisk != nil {
					inRamdisk = PathOnRamdisk(targetTorrent.SavePath, *ramdisk)
				}
				inTemp := cfg.LibtorrentTempDir != nil && SamePath(targetTorrent.SavePath, *cfg.LibtorrentTempDir)
				if inRamdisk || inTemp {
					_ = os.RemoveAll(source)
				}
			}
		}

		processedEvent := models.TorrentEvent{
			Kind:     "torrent_finished",
			Hash:     hash,
			Name:     filepath.Base(target),
			SavePath: destination,
		}
		processed, procErr := tev_completeTorrentOptions(cfg, s.db, s.activeEngine(), &processedEvent, release, s.tmdb, false)
		if procErr != nil {
			return false, fmt.Errorf("post-processing non riuscito: %w", procErr)
		}
		if !processed {
			logging.Warn("archive postprocessing did not keep release (inferior or discarded)", "hash", hash)
		} else {
			finalPath := target
			if s.db != nil {
				if p, err := s.db.TorrentProcessed(hash); err == nil && p != nil && strings.TrimSpace(*p) != "" {
					finalPath = *p
				}
			}
			size, _ := SizeOfPath(finalPath)
			notifyArchivedTorrent(s, hash, release, finalPath, size, targetTorrent.Name)
		}

		removed, rErr := s.activeEngine().Remove(hash, false)
		if rErr == nil && removed {
			logAutomaticTorrentRemoval(hash, removalName, removalState)
		}
		if s.db != nil {
			_ = s.db.MarkTorrentRemoved(hash)
			_ = s.db.ForgetRemovedTorrent(hash)
		}
		return true, nil
	}

	// Case 3: Foreign / unmonitored torrent (no release metadata)
	target := filepath.Join(destination, filepath.Base(source))
	if !SamePath(source, target) {
		if sourceInfo.IsDir() {
			if err := moveDirAcrossDevices(source, target); err != nil {
				return false, fmt.Errorf("spostamento della cartella non riuscito: %w", err)
			}
		} else {
			if err := moveAcrossDevices(source, target); err != nil {
				return false, fmt.Errorf("spostamento del file non riuscito: %w", err)
			}
		}
	}
	size, _ := SizeOfPath(target)
	alreadyAnnounced := foreignTorrentAlreadyAnnounced(s, hash)
	if s.db != nil {
		_ = s.db.MarkTorrentCompleted(hash, target, size)
	}
	if !alreadyAnnounced {
		notifyArchivedTorrent(s, hash, nil, target, size, targetTorrent.Name)
	}
	removed, rErr := s.activeEngine().Remove(hash, false)
	if rErr == nil && removed {
		logAutomaticTorrentRemoval(hash, removalName, removalState)
	}
	if s.db != nil {
		_ = s.db.MarkTorrentRemoved(hash)
		_ = s.db.ForgetRemovedTorrent(hash)
	}
	return true, nil
}
