package gextto

// torrent_engine_select.go turns the `torrent_backend` setting into a live
// engine, with the mandatory preflight that protects the filesystem: a backend
// is never activated when a path it may be asked to use cannot be translated.
//
// `gx-torrent` is the default (DefaultTorrentBackend); qBittorrent is the
// alternative selected by the fallback chain. Selecting qBittorrent is opt-in
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

// selectTorrentEngine builds the configured backend. The second return value is
// a human-readable note (empty on the happy path), and an error means activation
// was refused.
func selectTorrentEngine(cfg *Config, notifier *Notifier) (TorrentEngine, string, error) {
	if TorrentBackendName(cfg) != BackendGxTorrent {
		gxStopLeftoverDaemon(cfg)
	}
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
	default:
		// gx-torrent is the only other backend, and the default.
		engine, err := newGxTorrentEngine(cfg, notifier)
		if err != nil {
			return nil, "", err
		}
		if !engine.ping() {
			note := "gx-torrent is not reachable at startup; the adapter stays installed and will retry"
			logging.Warn(note, "url", engine.settings.BaseURL)
			return engine, note, nil
		}
		return engine, "", nil
	}
}

// requiredBackendPaths lists the Gextto paths a backend may be asked to use.
func requiredBackendPaths(cfg *Config) []string {
	paths := []string{cfg.DownloadDir}
	if cfg.DownloadTempDir != nil {
		paths = append(paths, *cfg.DownloadTempDir)
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
// resources (the managed qBittorrent process, the gx-torrent daemon adoption).
func ShutdownTorrentEngine(s *AppState) error {
	engine := s.activeEngine()
	if closer, ok := engine.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// ConfigureTorrentEngine installs the engine selected by the current settings.
// It is called at startup and whenever the backend settings change.
func ConfigureTorrentEngine(s *AppState, cfg *Config) error {
	if cfg != nil {
		if raw := strings.ToLower(strings.TrimSpace(cfg.Settings["torrent_backend"])); raw != "" && !isKnownTorrentBackend(raw) {
			logging.Warn("unknown torrent backend saved in the settings; using gx-torrent", "backend", raw)
		}
	}
	engine, note, err := selectTorrentEngine(cfg, s.notifier)
	if err != nil {
		return err
	}
	// The eD2k plane is optional and lives next to the torrent engine. It is
	// installed whenever the provider is enabled: when the daemon is down the
	// adapter simply returns no rows, and the torrent plane is unaffected.
	if ed2kEnabled(cfg) {
		if adapter, adapterErr := newEd2kEngine(cfg); adapterErr == nil {
			engine = newMultiEngine(engine, adapter, nil)
		} else {
			logging.Warn("eD2k transfer plane not installed", "error", adapterErr)
		}
	}
	s.setActiveEngine(engine)
	if note != "" {
		logging.Warn("torrent backend note", "note", note)
	}
	logging.Debug("torrent engine selected", "engine", TorrentBackendName(cfg))
	return nil
}
