package gextto

import (
	"strings"

	"github.com/buzzqw/gextto/internal/logging"
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
