package gextto

// Handlers for the libtorrent controls that were surfaced after the deeper
// integration pass: per-torrent upload/share mode, per-torrent connection
// flags, tracker scrape / DHT announce, and the session counters.
//
// Kept in a dedicated file so the rest of the web groups stay unchanged.

import (
	"net/http"
	"strings"
)

// TorrentToggleInput is the body of the boolean per-torrent toggles.
type TorrentToggleInput struct {
	Enabled bool `json:"enabled"`
}

// TorrentFlagInput is the body of `set_torrent_flag`.
type TorrentFlagInput struct {
	Flag    int  `json:"flag"`
	Enabled bool `json:"enabled"`
}

// SetTorrentUploadMode implements POST /api/torrents/{hash}/upload-mode.
func SetTorrentUploadMode(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	var input TorrentToggleInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	ok, err := s.torrents.SetUploadMode(hash, input.Enabled)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !ok && !s.cfg.DryRun {
		jsonError(w, http.StatusConflict, "torrent unavailable")
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "upload_mode": input.Enabled})
}

// SetTorrentShareMode implements POST /api/torrents/{hash}/share-mode.
func SetTorrentShareMode(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	var input TorrentToggleInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	ok, err := s.torrents.SetShareMode(hash, input.Enabled)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !ok && !s.cfg.DryRun {
		jsonError(w, http.StatusConflict, "torrent unavailable")
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "share_mode": input.Enabled})
}

// SetTorrentFlag implements POST /api/torrents/{hash}/flags.
func SetTorrentFlag(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	var input TorrentFlagInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	switch input.Flag {
	case TorrentFlagApplyIPFilter, TorrentFlagDisableDHT, TorrentFlagDisablePEX, TorrentFlagDisableLSD:
	default:
		jsonError(w, http.StatusBadRequest, "unknown torrent flag")
		return
	}
	ok, err := s.torrents.SetTorrentFlag(hash, input.Flag, input.Enabled)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !ok && !s.cfg.DryRun {
		jsonError(w, http.StatusConflict, "torrent unavailable")
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "flag": input.Flag, "enabled": input.Enabled})
}

// ScrapeTorrent implements POST /api/torrents/{hash}/scrape.
func ScrapeTorrent(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	ok, err := s.torrents.ScrapeTracker(hash)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !ok && !s.cfg.DryRun {
		jsonError(w, http.StatusConflict, "torrent unavailable")
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true})
}

// TorrentDhtAnnounce implements POST /api/torrents/{hash}/dht-announce.
func TorrentDhtAnnounce(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	ok, err := s.torrents.ForceDhtAnnounce(hash)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !ok && !s.cfg.DryRun {
		jsonError(w, http.StatusConflict, "torrent unavailable")
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true})
}

// TorrentWhy implements GET /api/torrents/{hash}/why: it explains the current
// situation of one torrent (why it is or is not downloading), which makes
// extreme cases such as a swarm with no seeders explicit.
func TorrentWhy(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	for _, torrent := range s.torrents.List() {
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

// LibtorrentSessionStats implements GET /api/libtorrent/session-stats.
func LibtorrentSessionStats(w http.ResponseWriter, r *http.Request, s *AppState) {
	values, err := s.torrents.SessionStats()
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "stats": values})
}
