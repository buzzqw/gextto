package gextto

import (
	"fmt"
	"os"
	"strings"

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
