package gextto

// web_handlers_torrent_stream.go redirects a player to the gx-torrent daemon's
// streaming endpoint, adding the daemon token server-side so the Gextto page
// never exposes it. The daemon downloads the pieces of the requested window
// first, so the player can start before the file is complete.

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// TorrentStream implements GET /api/torrents/{hash}/stream?file=N.
func TorrentStream(w http.ResponseWriter, r *http.Request, s *AppState) {
	if s.activeEngine().Name() != BackendGxTorrent {
		jsonError(w, http.StatusConflict, "lo streaming è disponibile solo con il motore gx-torrent")
		return
	}
	cfg := latestConfig(s)
	base := strings.TrimRight(strings.TrimSpace(cfg.Settings["gxtorrent_url"]), "/")
	if base == "" {
		jsonError(w, http.StatusConflict, "gxtorrent_url non configurato")
		return
	}
	hash := pathParam(r, "hash")
	file := strings.TrimSpace(r.URL.Query().Get("file"))
	if file == "" {
		jsonError(w, http.StatusBadRequest, "file mancante")
		return
	}
	target := fmt.Sprintf("%s/ui/stream?%s", base,
		url.Values{"hash": {hash}, "file": {file}}.Encode())
	if token := strings.TrimSpace(cfg.Settings["gxtorrent_token"]); token != "" {
		target += "&token=" + url.QueryEscape(token)
	}
	http.Redirect(w, r, target, http.StatusFound)
}
