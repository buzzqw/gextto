package gextto

import (
	"sync"

	"github.com/buzzqw/gextto/internal/logging"
)

// NewAppState builds the shared daemon state. It exists so `cmd/gexttod` (a
// different package) can construct the state whose fields stay unexported for
// the in-package web handlers.
func NewAppState(
	cfg *Config,
	configPath string,
	i18n *I18nDb,
	db *Database,
	archive *Archive,
	comics *ComicsDb,
	engine *Engine,
	torrents *LibtorrentClient,
	notifier *Notifier,
	tmdb *TmdbClient,
) *AppState {
	state := &AppState{
		cfg:             cfg,
		config_path:     configPath,
		i18n:            i18n,
		db:              db,
		archive:         archive,
		comics:          comics,
		engine:          engine,
		torrents:        torrents,
		torrent_events:  &EventLog{},
		notifier:        notifier,
		tmdb:            tmdb,
		last_cycle:      &CycleState{},
		cycle_lock:      &sync.Mutex{},
		rename_progress: &RenameProgress{},
		config_cache:    &ConfigCache{},
		bgStop:          make(chan struct{}),
	}
	// Install the configured transfer backend. A refused activation (invalid
	// path mappings, an unavailable build tag, a port conflict) falls back to
	// the embedded libtorrent engine instead of aborting the daemon: improving
	// without breaking.
	if err := ConfigureTorrentEngine(state, cfg); err != nil {
		logging.Warn("torrent backend activation refused; using embedded libtorrent", "error", err)
		state.setActiveEngine(nil)
		// The embedded session was suppressed to avoid two engines owning the
		// same files. If the alternative backend then failed to activate, start
		// the embedded session now so the daemon still transfers.
		if !cfg.DryRun && cfg.LibtorrentEnabled && alternativeBackendActive(cfg) {
			fallback := *cfg
			settings := make(map[string]string, len(cfg.Settings))
			for key, value := range cfg.Settings {
				settings[key] = value
			}
			settings["torrent_backend"] = BackendEmbedded
			fallback.Settings = settings
			if client, clientErr := NewLibtorrentClient(&fallback); clientErr == nil {
				if state.torrents != nil {
					_ = state.torrents.Shutdown(cfg)
				}
				state.torrents = client
				logging.Info("embedded libtorrent session started as fallback")
			} else {
				logging.Error("embedded libtorrent fallback failed", "error", clientErr)
			}
		}
	}
	// A pending migration manifest for the active backend is re-imported here:
	// re-adding is idempotent and the manifest is marked completed on success.
	if ActiveTorrentBackend(state).Name() != BackendEmbedded {
		if imported, warnings, importErr := ImportMigrationManifest(state, cfg); importErr != nil {
			logging.Warn("migration import failed", "error", importErr)
		} else {
			for _, warning := range warnings {
				logging.Warn("migration import warning", "detail", warning)
			}
			if imported > 0 {
				logging.Info("migration import completed", "imported", imported)
			}
		}
	}
	return state
}
