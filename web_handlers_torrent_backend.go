package gextto

// Torrent backend abstraction and the qBittorrent connectivity endpoints.
//
// The daemon is normally driven by the embedded libtorrent engine (the
// official backend). `torrent_backend=qbittorrent` switches the transfer plane
// to qBittorrent-nox through the adapter in qbittorrent_engine.go, while Gextto
// keeps ownership of the queue, the automation and the post-processing.
//
// These endpoints expose the active backend, the explicit capability matrix and
// the mandatory activation preflight (connectivity + path mapping).

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/qbittorrent"
)

// TorrentBackend is the minimal identity contract (name + capabilities). The
// full operational contract is TorrentEngine.
type TorrentBackend interface {
	Name() string
	Capabilities() map[string]bool
}

// ActiveTorrentBackend returns the backend currently driving the daemon.
func ActiveTorrentBackend(s *AppState) TorrentBackend {
	return s.activeEngine()
}

// TorrentBackendName returns the configured backend, defaulting to the embedded
// libtorrent engine. Anything other than the known backends is normalised to
// embedded.
func TorrentBackendName(cfg *Config) string {
	value := BackendEmbedded
	if cfg != nil {
		if raw, ok := cfg.Settings["torrent_backend"]; ok {
			value = strings.ToLower(strings.TrimSpace(raw))
		}
	}
	switch value {
	case BackendQbittorrent:
		return BackendQbittorrent
	case BackendAnacrolix:
		return BackendAnacrolix
	default:
		return BackendEmbedded
	}
}

// TorrentBackendStatus implements `torrent_backend_status`.
func TorrentBackendStatus(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	active := ActiveTorrentBackend(s)
	configured := TorrentBackendName(cfg)
	payload := map[string]any{
		"ok":                true,
		"backend":           active.Name(),
		"configured":        configured,
		"capabilities":      active.Capabilities(),
		"capability_matrix": CapabilityMatrix(active.Name()),
		"capability_parity": CapabilityParity(),
		"qbittorrent_url":   settingsOr(cfg, "qbittorrent_url", ""),
		"anacrolix_built":   newAnacrolixEngine != nil,
	}
	if engine, ok := active.(*qbittorrentEngine); ok {
		payload["sync"] = engine.SyncStats()
	}
	jsonResponse(w, payload)
}

// TorrentBackendTest implements `torrent_backend_test`: it logs into the
// configured qBittorrent-nox and reports its versions and torrent count without
// changing anything.
func TorrentBackendTest(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	settings, err := qbittorrentSettingsFromConfig(cfg)
	if err != nil {
		jsonStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	clientCfg := settings.Client
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

// TorrentBackendPreflight implements `torrent_backend_preflight`: it validates
// the path mappings and the connectivity of the configured backend before it
// is ever activated. A path-mapping failure is blocking.
func TorrentBackendPreflight(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	configured := TorrentBackendName(cfg)
	if configured == BackendEmbedded {
		jsonResponse(w, QbittorrentPreflight{OK: true, Connected: true})
		return
	}
	if configured == BackendAnacrolix {
		if newAnacrolixEngine == nil {
			jsonStatus(w, http.StatusConflict, map[string]any{
				"ok":    false,
				"error": "torrent_backend=anacrolix requires a build with the `anacrolix` tag",
			})
			return
		}
		jsonResponse(w, map[string]any{"ok": true, "backend": BackendAnacrolix})
		return
	}
	result := PreflightQbittorrent(cfg)
	if err := validateBackendMappings(result.Mappings, requiredBackendPaths(cfg)); err != nil {
		result.Errors = append(result.Errors, err.Error())
		result.OK = false
	}
	if !result.OK {
		jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "preflight": result, "error": firstOr(result.Errors, "preflight failed")})
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "preflight": result})
}

// TorrentBackendActivate implements `POST /api/torrent-backend`: it re-reads
// the settings and installs the configured engine. The activation is refused
// when the preflight fails, leaving the previously active engine in place.
func TorrentBackendActivate(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	if err := ConfigureTorrentEngine(s, cfg); err != nil {
		jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonResponse(w, map[string]any{
		"ok":         true,
		"backend":    ActiveTorrentBackend(s).Name(),
		"configured": TorrentBackendName(cfg),
	})
}

func firstOr(values []string, fallback string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return fallback
}
