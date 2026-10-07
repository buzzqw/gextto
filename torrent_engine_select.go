package gextto

// torrent_engine_select.go turns the `torrent_backend` setting into a live
// engine, with the mandatory preflight that protects the filesystem: a backend
// is never activated when a path it may be asked to use cannot be translated.
//
// `embedded` (libtorrent) stays the default. Selecting qBittorrent is opt-in
// and non-destructive: if the connectivity preflight fails the adapter is still
// installed (it degrades to an empty/stale view), but a path-mapping failure
// refuses activation because it could relocate files to the wrong place.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/qbittorrent"
)

// alternativeBackendActive reports whether a non-embedded backend will be
// activated at startup. It mirrors selectTorrentEngine's validation (without
// the connectivity probe) so NewLibtorrentClient can suppress its session
// exactly when the alternative engine will really take over.
func alternativeBackendActive(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	switch TorrentBackendName(cfg) {
	case BackendQbittorrent:
		settings, err := qbittorrentSettingsFromConfig(cfg)
		if err != nil || settings.Client.BaseURL == "" {
			return false
		}
		return validateBackendMappings(settings.Mappings, requiredBackendPaths(cfg)) == nil
	case BackendGxTorrent:
		return true // Will add path mapping validation later if needed
	default:
		return false
	}
}

// selectTorrentEngine builds the configured backend. A nil engine means the
// embedded libtorrent adapter. The second return value is a human-readable
// note (empty on the happy path), and an error means activation was refused.
func selectTorrentEngine(cfg *Config) (TorrentEngine, string, error) {
	switch TorrentBackendName(cfg) {
	case BackendQbittorrent:
		settings, err := qbittorrentSettingsFromConfig(cfg)
		if err != nil {
			return nil, "", err
		}
		if settings.Client.BaseURL == "" {
			return nil, "", fmt.Errorf("torrent_backend=qbittorrent requires qbittorrent_url")
		}
		if err := validateBackendMappings(settings.Mappings, requiredBackendPaths(cfg)); err != nil {
			return nil, "", err
		}
		engine, err := newQbittorrentEngine(cfg)
		if err != nil {
			return nil, "", err
		}
		preflight := PreflightQbittorrentWith(settings)
		if !preflight.Connected {
			note := "qBittorrent is not reachable at startup; the adapter stays installed and will retry"
			logging.Warn(note, "url", settings.Client.BaseURL)
			return engine, note, nil
		}
		return engine, "", nil
	case BackendGxTorrent:
		engine, err := newGxTorrentEngine(cfg)
		if err != nil {
			return nil, "", err
		}
		// TODO: Add preflight check for gx-torrent
		return engine, "", nil
	default:
		return nil, "", nil
	}
}

// requiredBackendPaths lists the Gextto paths a backend may be asked to use.
func requiredBackendPaths(cfg *Config) []string {
	paths := []string{cfg.LibtorrentDir}
	if cfg.LibtorrentTempDir != nil {
		paths = append(paths, *cfg.LibtorrentTempDir)
	}
	if ramdisk := cfg.RamdiskDir(); ramdisk != nil {
		paths = append(paths, *ramdisk)
	}
	return paths
}

// validateBackendMappings enforces the path preflight. When explicit mappings
// are configured, every required path must be covered (otherwise a move could
// target the wrong directory). With no mappings, the paths are assumed shared
// and only their local existence is checked.
func validateBackendMappings(mappings []PathMapping, required []string) error {
	if len(mappings) == 0 {
		for _, path := range required {
			if strings.TrimSpace(path) == "" {
				continue
			}
			if info, err := os.Stat(path); err != nil || !info.IsDir() {
				return fmt.Errorf("backend path %q does not exist; configure qbittorrent_path_mappings", path)
			}
		}
		return nil
	}
	return ValidatePathMappings(mappings, required)
}

// QbittorrentPreflight is the result of the activation check surfaced to the UI.
type QbittorrentPreflight struct {
	OK              bool          `json:"ok"`
	Connected       bool          `json:"connected"`
	AppVersion      string        `json:"app_version,omitempty"`
	WebAPIVersion   string        `json:"web_api_version,omitempty"`
	DefaultSavePath string        `json:"default_save_path,omitempty"`
	Torrents        int           `json:"torrents"`
	Mappings        []PathMapping `json:"path_mappings"`
	Warnings        []string      `json:"warnings,omitempty"`
	Errors          []string      `json:"errors,omitempty"`
}

// PreflightQbittorrent runs the activation preflight from the current settings.
func PreflightQbittorrent(cfg *Config) QbittorrentPreflight {
	settings, err := qbittorrentSettingsFromConfig(cfg)
	if err != nil {
		return QbittorrentPreflight{Errors: []string{err.Error()}}
	}
	return PreflightQbittorrentWith(settings)
}

// PreflightQbittorrentWith runs connectivity + path checks for resolved settings.
func PreflightQbittorrentWith(settings qbittorrentSettings) QbittorrentPreflight {
	result := QbittorrentPreflight{Mappings: settings.Mappings}
	if settings.Client.BaseURL == "" {
		result.Errors = append(result.Errors, "qbittorrent_url is not configured")
		return result
	}
	client, err := qbittorrent.New(settings.Client)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result
	}
	timeout := settings.Client.RequestTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout+5*time.Second)
	defer cancel()
	appVersion, err := client.AppVersion(ctx)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result
	}
	result.Connected = true
	result.AppVersion = appVersion
	result.WebAPIVersion, _ = client.WebAPIVersion(ctx)
	result.DefaultSavePath, _ = client.DefaultSavePath(ctx)
	if torrents, err := client.Torrents(ctx); err == nil {
		result.Torrents = len(torrents)
	}
	if len(settings.Mappings) == 0 {
		result.Warnings = append(result.Warnings,
			"no qbittorrent_path_mappings configured: the paths must be identical for Gextto and qBittorrent")
	}
	result.OK = true
	return result
}

// ShutdownTorrentEngine releases the active engine when it owns process-level
// resources. The embedded engine is closed
// by the daemon through LibtorrentClient.Shutdown.
func ShutdownTorrentEngine(s *AppState) error {
	engine := s.activeEngine()
	if closer, ok := engine.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// ShutdownEmbedded closes the embedded libtorrent client currently held by the
// state. It must be used instead of a startup-local pointer because NewAppState
// may replace the client with a fallback when an alternative backend fails to
// activate.
func ShutdownEmbedded(s *AppState, cfg *Config) error {
	if s == nil || s.torrents == nil {
		return nil
	}
	return s.torrents.Shutdown(cfg)
}

// ConfigureTorrentEngine installs the engine selected by the current settings.
// It is called at startup and whenever the backend settings change.
func ConfigureTorrentEngine(s *AppState, cfg *Config) error {
	if cfg != nil && strings.EqualFold(strings.TrimSpace(cfg.Settings["torrent_backend"]), "anacrolix") {
		logging.Warn("torrent backend anacrolix has been removed; falling back to embedded libtorrent")
	}
	engine, note, err := selectTorrentEngine(cfg)
	if err != nil {
		return err
	}
	s.setActiveEngine(engine)
	if note != "" {
		logging.Warn("torrent backend note", "note", note)
	}
	active := TorrentBackendName(cfg)
	if engine == nil {
		// The configured name `embedded` is an implementation detail. The
		// actual engine is libtorrent, so expose the concrete backend and the
		// linked library version in the startup log.
		logging.Debug("torrent backend selected",
			"backend", "libtorrent",
			"version", LibtorrentVersion())
		return nil
	}
	logging.Info("Torrent engine: " + active)
	return nil
}
