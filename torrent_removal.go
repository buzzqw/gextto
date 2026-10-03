package gextto

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
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
func logManualTorrentRemoval(hash string, deleteFiles bool, name, state string, hasMetadata bool) {
	logging.Info("torrent removed by user", "hash", hash, "name", name, "state", state, "metadata_available", hasMetadata, "delete_files", deleteFiles)
}

// SafeRemoveTorrent safely removes a torrent from the active engine.
// It ensures that:
// 1. Files in the permanent archive library (NAS) are NEVER deleted when the user
//    didn't explicitly request deletion, even if the torrent payload is marked disposable.
// 2. When file deletion is requested and trash is configured, files are moved
//    to the trash directory instead of being permanently unlinked.
// 3. Engine removal is properly recorded in the database.
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
	if !deleteFiles && s.db != nil && targetTorrent != nil {
		if gh1_torrentFilesAreDisposable(s.db, hash) && tev_completedSourceDisposable(s.db, hash, targetTorrent.SavePath) {
			deleteFiles = true
		}
	}

	engineDeleteFiles := deleteFiles
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
					logging.Info("removed torrent source moved to trash",
						"hash", hash, "source", source, "trash", target)
				}
			}
			engineDeleteFiles = false
		}
	}

	removalName, removalState, removalHasMetadata := manualTorrentRemovalInfo(s, hash)
	removed, err := s.activeEngine().Remove(hash, engineDeleteFiles)
	if err == nil && removed {
		logManualTorrentRemoval(hash, deleteFiles, removalName, removalState, removalHasMetadata)
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
		if err := copyTree(source, target); err != nil {
			return err
		}
		return os.RemoveAll(source)
	}
	return err
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

	// Check if already completed and archived on disk
	if s.db != nil {
		if processed, err := s.db.TorrentProcessed(hash); err == nil && processed != nil && strings.TrimSpace(*processed) != "" {
			if _, statErr := os.Stat(*processed); statErr == nil {
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
				removalName, removalState, removalHasMetadata := manualTorrentRemovalInfo(s, hash)
				removed, err := s.activeEngine().Remove(hash, false)
				if err == nil && removed {
					logManualTorrentRemoval(hash, false, removalName, removalState, removalHasMetadata)
				}
				_ = s.db.MarkTorrentRemoved(hash)
				_ = s.db.ForgetRemovedTorrent(hash)
				return true, nil
			}
		}
	}

	// Pause immediately to stop network activity/seeding
	_, _ = s.activeEngine().Pause(hash)

	// Fetch release metadata from DB if present
	var release *models.Release
	if s.db != nil {
		if meta, err := s.db.TorrentMeta(hash); err == nil && meta != nil {
			relCopy := meta.Release
			release = &relCopy
			normalizeReleaseSeries(cfg, release)
		}
	}

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

	removalName, removalState, removalHasMetadata := manualTorrentRemovalInfo(s, hash)

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
		}
		size, _ := SizeOfPath(destination)
		if s.db != nil {
			_ = s.db.MarkPackCompleted(release, entries, destination, size)
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
			logManualTorrentRemoval(hash, false, removalName, removalState, removalHasMetadata)
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
				ApplySidecars(source, target, cfg)
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
		}

		removed, rErr := s.activeEngine().Remove(hash, false)
		if rErr == nil && removed {
			logManualTorrentRemoval(hash, false, removalName, removalState, removalHasMetadata)
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
	if s.db != nil {
		size, _ := SizeOfPath(target)
		_ = s.db.MarkTorrentCompleted(hash, target, size)
	}
	removed, rErr := s.activeEngine().Remove(hash, false)
	if rErr == nil && removed {
		logManualTorrentRemoval(hash, false, removalName, removalState, removalHasMetadata)
	}
	if s.db != nil {
		_ = s.db.MarkTorrentRemoved(hash)
		_ = s.db.ForgetRemovedTorrent(hash)
	}
	return true, nil
}
