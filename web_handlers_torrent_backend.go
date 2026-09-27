package gextto

// Torrent backend abstraction and the qBittorrent connectivity endpoints.
//
// Today the daemon is always driven by the embedded libtorrent engine. This
// file exposes the selected/available backend and a read-only connectivity
// check for qBittorrent-nox, so the adapter (internal/qbittorrent) can be
// validated from the UI before it is ever made the active engine. Making
// qBittorrent the active transfer engine is a separate, larger refactor (see
// docs/aggiunta-qbittorrent-nox.md): nothing here changes how torrents run.

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/qbittorrent"
)

// TorrentBackend is the transfer-plane contract: Gextto keeps ownership of the
// queue, automation and post-processing, while the backend moves bytes.
type TorrentBackend interface {
	Name() string
	Capabilities() map[string]bool
}

type libtorrentBackend struct{}

func (libtorrentBackend) Name() string { return "embedded" }

func (libtorrentBackend) Capabilities() map[string]bool {
	return map[string]bool{
		"add": true, "list": true, "pause": true, "resume": true, "remove": true,
		"recheck": true, "move": true, "limits": true, "files": true, "peers": true,
		"trackers": true, "ramdisk": true, "fastresume": true, "sync": false,
	}
}

var _ TorrentBackend = libtorrentBackend{}

// ActiveTorrentBackend returns the backend currently driving the daemon.
func ActiveTorrentBackend(s *AppState) TorrentBackend {
	return libtorrentBackend{}
}

// TorrentBackendName returns the configured backend, defaulting to the embedded
// libtorrent engine. Only "embedded" is wired as an active engine today.
func TorrentBackendName(cfg *Config) string {
	if value, ok := cfg.Settings["torrent_backend"]; ok {
		if strings.EqualFold(strings.TrimSpace(value), "qbittorrent") {
			return "qbittorrent"
		}
	}
	return "embedded"
}

func qbittorrentConfigFromSettings(cfg *Config) qbittorrent.Config {
	timeout := 15 * time.Second
	if value, ok := cfg.Settings["qbittorrent_request_timeout_secs"]; ok {
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && parsed > 0 {
			timeout = time.Duration(parsed) * time.Second
		}
	}
	return qbittorrent.Config{
		BaseURL:        strings.TrimSpace(cfg.Settings["qbittorrent_url"]),
		Username:       strings.TrimSpace(cfg.Settings["qbittorrent_username"]),
		Password:       cfg.Settings["qbittorrent_password"],
		RequestTimeout: timeout,
	}
}

// TorrentBackendStatus implements `torrent_backend_status`.
func TorrentBackendStatus(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	active := ActiveTorrentBackend(s)
	jsonResponse(w, map[string]any{
		"ok":              true,
		"backend":         active.Name(),
		"configured":      TorrentBackendName(cfg),
		"capabilities":    active.Capabilities(),
		"qbittorrent_url": settingsOr(cfg, "qbittorrent_url", ""),
	})
}

// TorrentBackendTest implements `torrent_backend_test`: it logs into the
// configured qBittorrent-nox and reports its versions and torrent count without
// changing anything.
func TorrentBackendTest(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	clientCfg := qbittorrentConfigFromSettings(cfg)
	if clientCfg.BaseURL == "" {
		jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "qbittorrent_url is not configured"})
		return
	}
	client, err := qbittorrent.New(clientCfg)
	if err != nil {
		jsonStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), clientCfg.RequestTimeout+5*time.Second)
	defer cancel()

	appVersion, err := client.AppVersion(ctx)
	if err != nil {
		jsonStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	apiVersion, _ := client.WebAPIVersion(ctx)
	result := map[string]any{
		"ok":              true,
		"app_version":     appVersion,
		"web_api_version": apiVersion,
	}
	if torrents, err := client.Torrents(ctx); err == nil {
		result["torrents"] = len(torrents)
	}
	jsonResponse(w, result)
}
