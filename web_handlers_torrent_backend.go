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
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
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

// TorrentBackendName returns the configured backend. A missing or empty
// `torrent_backend` selects DefaultTorrentBackend (gx-torrent for a fresh
// installation); a value that was saved but is unknown (a removed backend such
// as `anacrolix`) is normalised to the embedded libtorrent engine so existing
// installations keep transfering after an upgrade.
func TorrentBackendName(cfg *Config) string {
	value := DefaultTorrentBackend
	if cfg != nil {
		if raw, ok := cfg.Settings["torrent_backend"]; ok {
			value = strings.ToLower(strings.TrimSpace(raw))
			if value == "" {
				value = DefaultTorrentBackend
			}
		}
	}
	switch value {
	case BackendQbittorrent:
		return BackendQbittorrent
	case BackendGxTorrent:
		return BackendGxTorrent
	default:
		// embedded, or an unknown/removed backend saved by an older install.
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
	}
	if engine, ok := active.(*qbittorrentEngine); ok {
		payload["sync"] = engine.SyncStats()
	}
	if engine, ok := active.(*gxTorrentEngine); ok {
		payload["sync"] = engine.SyncStats()
		payload["gxtorrent_url"] = engine.settings.BaseURL
	}
	jsonResponse(w, payload)
}

// QbittorrentRuntimeUpdate reports the latest v2 static release and, on POST,
// downloads it into Gextto's private DATA_DIR/qbittorrent directory. It never
// touches a system qBittorrent installation.
func QbittorrentRuntimeUpdate(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	if cfg == nil || strings.TrimSpace(cfg.DataDir) == "" {
		jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "directory dati Gextto non configurata"})
		return
	}
	ctx := r.Context()
	if r.Method == http.MethodPost {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		prepared, err := managedQbittorrentConfig(cfg)
		if err != nil {
			jsonStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		settings, err := qbittorrentSettingsFromConfig(prepared)
		if err != nil {
			jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		// Prepare the private profile before replacing the binary. A malformed or
		// unwritable profile must never leave an existing executable half-updated.
		if err := writeManagedQbittorrentConfig(prepared, settings); err != nil {
			jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		status, err := installQbittorrentRuntime(ctx, prepared)
		if err != nil {
			jsonStatus(w, http.StatusBadGateway, map[string]any{
				"ok": false, "error": err.Error(), "source": qbittorrentGitHubRepo,
			})
			return
		}
		// Downloading the managed binary is an explicit opt-in to the private
		// runtime. The backend itself still changes only after a restart.
		for _, key := range []string{"qbittorrent_url", "qbittorrent_username", "qbittorrent_password", "qbittorrent_managed"} {
			if err := SaveSetting(cfg.DataDir, key, prepared.Settings[key]); err != nil {
				jsonStatus(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
				return
			}
		}
		status.Managed = true
		status.RestartRequired = true
		jsonResponse(w, map[string]any{"ok": true, "status": status, "message": fmt.Sprintf(
			"qBittorrent scaricato e installato in %s (%s); riavvia il servizio per usarlo.",
			status.Binary, status.LatestName)})
		return
	}
	status, err := getQbittorrentRuntimeStatus(ctx, cfg)
	if err != nil {
		// The settings page remains usable when GitHub is temporarily rate
		// limited/offline. The error is visible in the response and the next
		// refresh retries after the short cache expires.
		jsonResponse(w, map[string]any{"ok": false, "status": status, "error": err.Error(), "source": qbittorrentGitHubRepo})
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "status": status, "source": qbittorrentGitHubRepo, "message": qbittorrentRuntimeMessage(status)})
}

// qbittorrentRuntimeMessage describes the managed runtime and where it lives.
func qbittorrentRuntimeMessage(status qbittorrentRuntimeStatus) string {
	switch {
	case status.Installed && status.UpdateAvailable:
		return fmt.Sprintf("qBittorrent installato in %s (versione %s); aggiornamento disponibile: %s. Usa «Installa / Ottimizza qBittorrent» per aggiornarlo.",
			status.Binary, status.InstalledTag, status.LatestName)
	case status.Installed:
		return fmt.Sprintf("qBittorrent installato in %s (versione %s), già aggiornato.", status.Binary, status.InstalledTag)
	default:
		return fmt.Sprintf("qBittorrent non ancora installato. Verrà scaricato in %s: premi «Installa / Ottimizza qBittorrent» (oppure seleziona qBittorrent dalla tendina).", status.Binary)
	}
}

// TorrentBackendTest implements `torrent_backend_test`: it logs into the
// configured qBittorrent-nox and reports its versions and torrent count without
// changing anything.
func TorrentBackendTest(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	settings, err := qbittorrentSettingsFromConfig(cfg)
	if err != nil {
		logging.Info("torrent backend test", "backend", TorrentBackendName(cfg), "ok", false, "error", err.Error())
		jsonStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	clientCfg := settings.Client
	if clientCfg.BaseURL == "" {
		logging.Info("torrent backend test", "backend", TorrentBackendName(cfg), "ok", false, "error", "qbittorrent_url is not configured")
		jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "qbittorrent_url is not configured"})
		return
	}
	client, err := qbittorrent.New(clientCfg)
	if err != nil {
		logging.Info("torrent backend test", "backend", TorrentBackendName(cfg), "host", domain_of(clientCfg.BaseURL), "ok", false, "error", err.Error())
		jsonStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), clientCfg.RequestTimeout+5*time.Second)
	defer cancel()

	appVersion, err := client.AppVersion(ctx)
	if err != nil {
		logging.Info("torrent backend test", "backend", TorrentBackendName(cfg), "host", domain_of(clientCfg.BaseURL), "ok", false, "error", err.Error())
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
	logging.Info("torrent backend test", "backend", TorrentBackendName(cfg), "host", domain_of(clientCfg.BaseURL), "ok", true, "app_version", appVersion)
	jsonResponse(w, result)
}

// TorrentBackendPortCheck tests the peer port of the active engine: is it
// listening and does the router forward it (the eMule-style "test ports")?
// Only gx-torrent, whose daemon owns the router mapping, can answer.
func TorrentBackendPortCheck(w http.ResponseWriter, r *http.Request, s *AppState) {
	checker, ok := s.activeEngine().(interface {
		PortCheck() (map[string]any, error)
	})
	if !ok {
		jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "il test porte è disponibile solo con il motore gx-torrent"})
		return
	}
	result, err := checker.PortCheck()
	if err != nil {
		logging.Info("torrent backend port check", "backend", "gx-torrent", "ok", false, "error", err.Error())
		jsonStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	result["ok"] = true
	logging.Info("torrent backend port check", "backend", "gx-torrent", "open", result["open"], "port", result["port"])
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

// TorrentBackendActivate implements `POST /api/torrent-backend`: it validates
// the configured engine and reports whether a restart is needed. A running
// daemon never installs a second engine: switching the transfer plane changes
// who owns the files, so it is applied at startup after a clean shutdown
// (see the backend-specific implementation notes).
func TorrentBackendActivate(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	active := ActiveTorrentBackend(s).Name()
	configured := TorrentBackendName(cfg)

	// A backend different from the active one is only applied at restart.
	if configured != active {
		switch configured {
		case BackendQbittorrent:
			result := PreflightQbittorrent(cfg)
			if err := validateBackendMappings(result.Mappings, requiredBackendPaths(cfg)); err != nil {
				result.Errors = append(result.Errors, err.Error())
				result.OK = false
			}
			if !result.OK {
				jsonStatus(w, http.StatusConflict, map[string]any{
					"ok": false, "preflight": result,
					"error": firstOr(result.Errors, "preflight failed"),
				})
				return
			}
		}
	}
	message := "il motore attivo è già quello configurato"
	if configured != active {
		message = "riavvia Gextto per applicare il nuovo motore torrent"
	}
	jsonResponse(w, map[string]any{
		"ok":               true,
		"backend":          active,
		"configured":       configured,
		"restart_required": configured != active,
		"message":          message,
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
