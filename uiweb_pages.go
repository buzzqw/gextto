package gextto

import (
	"encoding/json"
	"sort"
	"strings"
)

// uiweb_pages.go defines the data-driven list pages and action pages of the new
// UI. They reuse the existing JSON APIs from the browser (small generic client)
// so every list keeps exactly the same data as before, and every action keeps
// the same endpoint and payload.

type uiColumn struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Format string `json:"format,omitempty"`
}

type uiAction struct {
	Label   string `json:"label"`
	Class   string `json:"class,omitempty"`
	Method  string `json:"method"`
	Path    string `json:"path"`
	Body    string `json:"body,omitempty"`
	Confirm string `json:"confirm,omitempty"`
}

type uiTableSpec struct {
	Title       string
	Endpoint    string
	ItemsKey    string
	ColumnsJSON string
	ActionsJSON string
	Empty       string
	Note        string
	Search      bool
	SearchParam string
	Query       string
	// Comics enables the extra panels of the comics page (download queue and
	// GetComics link finder) and the columns/actions of the queue table.
	Comics               bool
	DownloadsColumnsJSON string
	DownloadsActionsJSON string
}

// uiSearchPage drives the Esplora page: a query form, a generic result table and
// an "add" action that posts the selected release exactly like the classic UI.
type uiSearchPage struct {
	Title      string
	Endpoint   string
	ResultsKey string
	AddPath    string
}

type uiActionButton struct {
	Label   string
	Class   string
	Method  string
	Path    string
	Body    string
	Confirm string
	Hint    string
}

type uiActionSection struct {
	Label   string
	Hint    string
	Buttons []uiActionButton
}

type uiActionsPage struct {
	Title    string
	Sections []uiActionSection
}

// uiSettingField is one editable setting in the new settings page.
type uiSettingField struct {
	Key   string
	Label string
	Value string
	Kind  string // text|bool|area|secret
}

type uiSettingsTabData struct {
	Label  string
	Fields []uiSettingField
}

type uiSettingsPage struct {
	Tabs    []uiSettingsTabData
	Editors []uiJSONEditor
}

// uiJSONEditor is a generic JSON editor for a structured configuration
// endpoint: GET to load, POST to save. It keeps the new UI self-sufficient for
// configurations that the classic UI edited with bespoke widgets.
type uiJSONEditor struct {
	Label    string
	GetPath  string
	PostPath string
	Hint     string
}

var uiJSONEditors = []uiJSONEditor{
	{Label: "Filtri per sorgente", GetPath: "/api/config/source-filters", PostPath: "/api/config/source-filters"},
	{Label: "Regole tag → cartella", GetPath: "/api/tag-dir-rules", PostPath: "/api/tag-dir-rules"},
	{Label: "Event hook", GetPath: "/api/event-hooks", PostPath: "/api/event-hooks"},
	{Label: "Cartelle osservate", GetPath: "/api/watched-folders", PostPath: "/api/watched-folders"},
}

// uiSettingsPageFrom builds the settings page from the generated index and the
// live values, so every key the classic UI exposes stays editable.
func uiSettingsPageFrom(s *AppState) uiSettingsPage {
	cfg := latestConfig(s)
	tabIndex := map[string]int{}
	page := uiSettingsPage{}
	for _, tab := range uiSettingsTabs {
		tabIndex[tab.ID] = len(page.Tabs)
		page.Tabs = append(page.Tabs, uiSettingsTabData{Label: tab.Label})
	}
	for _, def := range uiSettingsIndex {
		position, ok := tabIndex[def.Tab]
		if !ok {
			continue
		}
		value := ""
		if raw, present := cfg.Settings[def.Key]; present {
			value = raw
		}
		page.Tabs[position].Fields = append(page.Tabs[position].Fields, uiSettingField{
			Key:   def.Key,
			Label: def.Label,
			Value: value,
			Kind:  uiSettingKind(def.Key, value),
		})
	}
	// Any persisted setting not covered by the generated index (score groups,
	// backup, integrations, ...) is still editable, grouped under "Altro".
	indexed := map[string]struct{}{}
	for _, def := range uiSettingsIndex {
		indexed[def.Key] = struct{}{}
	}
	other := uiSettingsTabData{Label: "Altro"}
	keys := make([]string, 0, len(cfg.Settings))
	for key := range cfg.Settings {
		if _, ok := indexed[key]; ok {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		other.Fields = append(other.Fields, uiSettingField{
			Key:   key,
			Label: key,
			Value: cfg.Settings[key],
			Kind:  uiSettingKind(key, cfg.Settings[key]),
		})
	}
	if len(other.Fields) > 0 {
		page.Tabs = append(page.Tabs, other)
	}
	page.Editors = uiJSONEditors
	return page
}

func uiSettingKind(key, value string) string {
	lowered := strings.ToLower(key)
	for _, secret := range []string{"password", "token", "api_key", "secret"} {
		if strings.Contains(lowered, secret) {
			return "secret"
		}
	}
	for _, area := range []string{"mappings", "extra_settings", "blacklist", "content_filters", "dht_bootstrap_nodes"} {
		if strings.Contains(lowered, area) {
			return "area"
		}
	}
	if value == "true" || value == "false" || value == "yes" || value == "no" {
		return "bool"
	}
	return "text"
}

func uiJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

// uiTableSpecFor returns the table definition of a list page.
func uiTableSpecFor(view string) (uiTableSpec, bool) {
	switch view {
	case "series":
		return uiTableSpec{
			Title:    "Serie TV",
			Endpoint: "/api/series",
			ItemsKey: "items",
			ColumnsJSON: uiJSON([]uiColumn{
				{Key: "name", Label: "Nome", Format: "series_link"},
				{Key: "seasons", Label: "Stagioni"},
				{Key: "quality", Label: "Qualità"},
				{Key: "language", Label: "Lingua"},
				{Key: "enabled", Label: "Attiva", Format: "bool"},
			}),
			ActionsJSON: uiJSON([]uiAction{
				{Label: "Cerca mancanti", Class: "primary", Method: "POST", Path: "/api/series/{name}/search-missing", Body: "{}"},
				{Label: "Scansiona", Method: "POST", Path: "/api/series/{name}/scan-archive", Body: "{}"},
				{Label: "TMDB", Method: "POST", Path: "/api/series/{name}/metadata", Body: "{}"},
			}),
			Empty: "Nessuna serie monitorizzata.",
		}, true
	case "movies":
		return uiTableSpec{
			Title:    "Film",
			Endpoint: "/api/movies",
			ItemsKey: "items",
			ColumnsJSON: uiJSON([]uiColumn{
				{Key: "name", Label: "Nome", Format: "movie_link"},
				{Key: "year", Label: "Anno"},
				{Key: "quality", Label: "Qualità"},
				{Key: "language", Label: "Lingua"},
				{Key: "enabled", Label: "Attivo", Format: "bool"},
			}),
			Empty: "Nessun film monitorizzato.",
		}, true
	case "gaps":
		return uiTableSpec{
			Title:    "Episodi mancanti",
			Endpoint: "/api/gaps",
			ItemsKey: "items",
			ColumnsJSON: uiJSON([]uiColumn{
				{Key: "series", Label: "Serie"},
				{Key: "season", Label: "Stagione", Format: "number"},
				{Key: "episode", Label: "Episodio", Format: "number"},
				{Key: "air_date", Label: "Data"},
			}),
			Empty: "Nessun episodio mancante.",
		}, true
	case "archive":
		return uiTableSpec{
			Title:       "Archivio",
			Endpoint:    "/api/archive",
			ItemsKey:    "items",
			Empty:       "Archivio vuoto.",
			Search:      true,
			SearchParam: "q",
		}, true
	case "blocklist":
		return uiTableSpec{
			Title:    "Blocklist",
			Endpoint: "/api/blocklist",
			ItemsKey: "items",
			ColumnsJSON: uiJSON([]uiColumn{
				{Key: "title", Label: "Titolo"},
				{Key: "reason", Label: "Motivo"},
				{Key: "kind", Label: "Tipo"},
				{Key: "created_at", Label: "Aggiunto"},
			}),
			ActionsJSON: uiJSON([]uiAction{
				{Label: "Rimuovi", Class: "danger", Method: "POST", Path: "/api/blocklist/{hash}/remove", Body: "{}", Confirm: "Rimuovere questa voce dalla blocklist?"},
			}),
			Empty: "Blocklist vuota.",
		}, true
	case "comics":
		return uiTableSpec{
			Title:    "Fumetti",
			Endpoint: "/api/comics",
			ItemsKey: "",
			ColumnsJSON: uiJSON([]uiColumn{
				{Key: "title", Label: "Titolo"},
				{Key: "publisher", Label: "Editore"},
				{Key: "from_date", Label: "Dal"},
				{Key: "enabled", Label: "Attivo", Format: "bool"},
				{Key: "latest_downloaded_title", Label: "Ultimo scaricato"},
				{Key: "tag_url", Label: "Sorgente", Format: "url"},
			}),
			ActionsJSON: uiJSON([]uiAction{
				{Label: "Attiva", Method: "POST", Path: "/api/comics/{id}/enabled", Body: `{"enabled":true}`},
				{Label: "Disattiva", Method: "POST", Path: "/api/comics/{id}/enabled", Body: `{"enabled":false}`},
				{Label: "Elimina", Class: "danger", Method: "DELETE", Path: "/api/comics/{id}", Body: "{}", Confirm: "Eliminare questo fumetto monitorizzato?"},
			}),
			Empty:  "Nessun fumetto monitorizzato.",
			Note:   "Tag dei download e pianificazione settimanale restano nella UI classica (/legacy).",
			Comics: true,
			DownloadsColumnsJSON: uiJSON([]uiColumn{
				{Key: "title", Label: "Titolo"},
				{Key: "method", Label: "Metodo"},
				{Key: "status", Label: "Stato"},
				{Key: "progress", Label: "Avanzamento", Format: "percent"},
				{Key: "downloaded_bytes", Label: "Scaricato", Format: "bytes"},
				{Key: "speed_bytes", Label: "Velocità", Format: "rate"},
				{Key: "tag", Label: "Tag"},
			}),
			DownloadsActionsJSON: uiJSON([]uiAction{
				{Label: "Pausa", Method: "POST", Path: "/api/comics/downloads/{id}/pause", Body: "{}"},
				{Label: "Riprendi", Method: "POST", Path: "/api/comics/downloads/{id}/resume", Body: "{}"},
				{Label: "Rimuovi", Class: "danger", Method: "POST", Path: "/api/comics/downloads/{id}/remove", Body: "{}", Confirm: "Rimuovere questo download?"},
			}),
		}, true
	default:
		return uiTableSpec{}, false
	}
}

// uiSearchPageFor defines the Esplora page.
func uiSearchPageFor(view string) (uiSearchPage, bool) {
	if view != "search" {
		return uiSearchPage{}, false
	}
	return uiSearchPage{
		Title:      "Esplora release",
		Endpoint:   "/api/search",
		ResultsKey: "results",
		AddPath:    "/api/search/add",
	}, true
}

// uiActionsPages are pages made of buttons that call existing endpoints.
func uiActionsPageFor(view string) (uiActionsPage, bool) {
	switch view {
	case "maintenance":
		return uiActionsPage{Title: "Manutenzione", Sections: []uiActionSection{
			{Label: "Database", Hint: "Operazioni sui database applicativi.", Buttons: []uiActionButton{
				{Label: "Ricalcola punteggi", Class: "primary", Method: "POST", Path: "/api/database/rescore", Body: "{}", Hint: "Ricalcola lo score delle release archiviate."},
				{Label: "Pulizia duplicati", Method: "POST", Path: "/api/maintenance/clean-duplicates", Body: "{}", Hint: "Rimuove le voci duplicate."},
				{Label: "Pota database", Method: "POST", Path: "/api/db/prune", Body: "{}", Hint: "Applica la retention configurata."},
				{Label: "VACUUM", Method: "POST", Path: "/api/db/action", Body: `{"action":"vacuum"}`, Hint: "Compatta i database."},
			}},
			{Label: "Archivio e rinomina", Buttons: []uiActionButton{
				{Label: "Scansiona archivi", Method: "POST", Path: "/api/scan-all-archives", Body: "{}"},
				{Label: "Rinomina tutto", Method: "POST", Path: "/api/rename-all", Body: "{}"},
			}},
			{Label: "Pulizie", Buttons: []uiActionButton{
				{Label: "Pulisci trash", Method: "POST", Path: "/api/maintenance/clean-trash", Body: "{}"},
				{Label: "Housekeeping", Method: "POST", Path: "/api/maintenance/housekeeping", Body: "{}"},
				{Label: "Backfill MediaInfo", Method: "POST", Path: "/api/maintenance/backfill-media-info", Body: "{}"},
			}},
			{Label: "Backup", Buttons: []uiActionButton{
				{Label: "Crea backup", Method: "POST", Path: "/api/backup", Body: "{}"},
			}},
		}}, true
	default:
		return uiActionsPage{}, false
	}
}

// uiIntegrationsPage carries the OAuth/PIN state and the media-server actions.
type uiIntegrationsPage struct {
	TraktConfigured    bool
	TraktAuthenticated bool
	SimklConfigured    bool
	SimklAuthenticated bool
	Sections           []uiActionSection
}

func uiIntegrationsPageFrom(s *AppState) uiIntegrationsPage {
	cfg := latestConfig(s)
	return uiIntegrationsPage{
		TraktConfigured:    settingsNonEmpty(cfg, "trakt_client_id"),
		TraktAuthenticated: settingsNonEmpty(cfg, "trakt_access_token"),
		SimklConfigured:    settingsNonEmpty(cfg, "simkl_client_id"),
		SimklAuthenticated: settingsNonEmpty(cfg, "simkl_access_token"),
		Sections: []uiActionSection{
			{Label: "Media server", Hint: "Verifica o aggiorna le librerie collegate.", Buttons: []uiActionButton{
				{Label: "Test Jellyfin", Method: "POST", Path: "/api/jellyfin/test", Body: "{}"},
				{Label: "Aggiorna Jellyfin", Method: "POST", Path: "/api/jellyfin/refresh", Body: "{}"},
				{Label: "Test Plex", Method: "POST", Path: "/api/plex/test", Body: "{}"},
				{Label: "Aggiorna Plex", Method: "POST", Path: "/api/plex/refresh", Body: "{}"},
			}},
			{Label: "Servizi", Buttons: []uiActionButton{
				{Label: "Test FlareSolverr", Method: "POST", Path: "/api/flaresolverr/test", Body: "{}"},
				{Label: "Notifica di test", Method: "POST", Path: "/api/test-notification", Body: "{}"},
			}},
		},
	}
}
