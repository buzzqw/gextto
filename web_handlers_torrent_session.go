package gextto

// Engine-neutral torrent endpoints: the "why" diagnosis and the session
// counters. The libtorrent-only controls (upload/share mode, per-torrent flags,
// scrape, DHT announce) went away with the embedded engine.

import (
	"net/http"
	"strings"
)

// TorrentWhy implements GET /api/torrents/{hash}/why: it explains the current
// situation of one torrent (why it is or is not downloading), which makes
// extreme cases such as a swarm with no seeders explicit.
func TorrentWhy(w http.ResponseWriter, r *http.Request, s *AppState) {
	engine := s.activeEngine()
	if engine == nil {
		jsonError(w, http.StatusServiceUnavailable, "no torrent engine is active")
		return
	}
	hash := pathParam(r, "hash")
	for _, torrent := range engine.List() {
		if !strings.EqualFold(torrent.Hash, hash) {
			continue
		}
		code, reason, hint := DiagnoseTorrent(&torrent)
		jsonResponse(w, map[string]any{
			"ok":      true,
			"code":    code,
			"reason":  reason,
			"hint":    hint,
			"torrent": torrent,
		})
		return
	}
	jsonError(w, http.StatusNotFound, "torrent not found")
}

// TorrentSessionStats implements GET /api/torrents/session-stats: the active
// engine's session counters, for every backend that exposes them.
func TorrentSessionStats(w http.ResponseWriter, r *http.Request, s *AppState) {
	type sessionStatter interface {
		SessionStats() (map[string]int64, error)
	}
	statter, ok := s.activeEngine().(sessionStatter)
	if !ok {
		jsonError(w, http.StatusConflict, "the active engine does not expose session statistics")
		return
	}
	values, err := statter.SessionStats()
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "stats": values})
}
