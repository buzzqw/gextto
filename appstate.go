package gextto

import "sync"

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
	return &AppState{
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
	}
}
