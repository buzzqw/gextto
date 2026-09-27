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
	// BoolValue, TrueValue and FalseValue are set only for Kind=="bool": they
	// keep the original spelling (yes/no, 1/0, on/off, true/false) so saving the
	// select never turns a "yes" into a "true" that strict readers would reject.
	BoolValue  bool
	TrueValue  string
	FalseValue string
}

// uiBoolPairs maps the boolean spellings accepted by the backend to their pair,
// so the settings select preserves the value style the daemon already reads.
func uiBoolValues(value string) (bool, string, string) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes":
		return true, "yes", "no"
	case "no":
		return false, "yes", "no"
	case "1":
		return true, "1", "0"
	case "0":
		return false, "1", "0"
	case "on":
		return true, "on", "off"
	case "off":
		return false, "on", "off"
	case "true":
		return true, "true", "false"
	case "false":
		return false, "true", "false"
	}
	return false, "true", "false"
}

type uiSettingsTabRef struct {
	ID     string
	Label  string
	Active bool
	Count  int
}

type uiSettingsPage struct {
	Tabs            []uiSettingsTabRef
	ActiveID        string
	Fields          []uiSettingField
	ShowSources     bool
	ShowLibrary     bool
	ShowEditors     bool
	ShowI18n        bool
	Editors         []uiJSONEditor
	SearchIndexJSON string
}

// uiJSONEditor is a generic JSON editor for a structured configuration
// endpoint: GET to load, POST to save. Some endpoints return a wrapper object
// ({"items":[...]}) but expect a bare array on POST, or expect a wrapped object;
// Unwrap extracts the payload from the GET response and Wrap rewraps it before
// POST so the round-trip is lossless.
type uiJSONEditor struct {
	Label    string
	GetPath  string
	PostPath string
	Hint     string
	Unwrap   string
	Wrap     string
}

var uiJSONEditors = []uiJSONEditor{
	{Label: "Filtri per sorgente", GetPath: "/api/config/source-filters", PostPath: "/api/config/source-filters", Unwrap: "filters", Wrap: "filters"},
	{Label: "Regole tag → cartella", GetPath: "/api/tag-dir-rules", PostPath: "/api/tag-dir-rules", Unwrap: "items"},
	{Label: "Event hook", GetPath: "/api/event-hooks", PostPath: "/api/event-hooks", Unwrap: "items"},
	{Label: "Cartelle osservate", GetPath: "/api/watched-folders", PostPath: "/api/watched-folders", Unwrap: "items"},
}

type uiSearchEntry struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Tab   string `json:"tab"`
}

// uiSettingsPageFrom builds one settings tab from the generated index and the
// live values, so every key the classic UI exposes stays editable. Only the
// active tab is rendered, which keeps the page small and usable on mobile.
func uiSettingsPageFrom(s *AppState, activeTab string) uiSettingsPage {
	cfg := latestConfig(s)

	labels := map[string]string{}
	order := []string{}
	for _, tab := range uiSettingsTabs {
		labels[tab.ID] = tab.Label
		order = append(order, tab.ID)
	}
	fieldsByTab := map[string][]uiSettingField{}
	for _, def := range uiSettingsIndex {
		if _, ok := labels[def.Tab]; !ok {
			continue
		}
		value := ""
		if raw, present := cfg.Settings[def.Key]; present {
			value = raw
		}
		fieldsByTab[def.Tab] = append(fieldsByTab[def.Tab], uiSettingFieldFor(def.Key, def.Label, value))
	}

	// Any persisted setting not covered by the generated index (score groups,
	// backup, integrations, ...) is still editable, grouped under "Altro".
	indexed := map[string]struct{}{}
	for _, def := range uiSettingsIndex {
		indexed[def.Key] = struct{}{}
	}
	keys := make([]string, 0, len(cfg.Settings))
	for key := range cfg.Settings {
		if _, ok := indexed[key]; ok {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		target := "altro"
		if strings.HasPrefix(strings.ToLower(key), "score") {
			target = "scores"
		}
		fieldsByTab[target] = append(fieldsByTab[target], uiSettingFieldFor(key, key, cfg.Settings[key]))
	}
	if len(fieldsByTab["altro"]) > 0 {
		labels["altro"] = "Altro"
		order = append(order, "altro")
	}
	labels["libreria"] = "Libreria"
	order = append(order, "libreria")

	special := map[string]bool{"sources": true, "advanced": true, "libreria": true, "i18n": true}
	tabs := make([]uiSettingsTabRef, 0, len(order))
	for _, id := range order {
		if len(fieldsByTab[id]) == 0 && !special[id] {
			continue
		}
		tabs = append(tabs, uiSettingsTabRef{ID: id, Label: labels[id], Count: len(fieldsByTab[id])})
	}
	if len(tabs) == 0 {
		tabs = append(tabs, uiSettingsTabRef{ID: "daemon", Label: "Daemon"})
	}

	active := strings.TrimSpace(activeTab)
	valid := false
	for _, tab := range tabs {
		if tab.ID == active {
			valid = true
			break
		}
	}
	if !valid {
		active = tabs[0].ID
		for _, tab := range tabs {
			if tab.ID == "daemon" {
				active = "daemon"
				break
			}
		}
	}
	for index := range tabs {
		tabs[index].Active = tabs[index].ID == active
	}

	page := uiSettingsPage{
		Tabs:     tabs,
		ActiveID: active,
		Fields:   fieldsByTab[active],
		Editors:  uiJSONEditors,
	}
	page.ShowSources = active == "sources"
	page.ShowLibrary = active == "libreria"
	page.ShowEditors = active == "advanced"
	page.ShowI18n = active == "i18n"

	entries := make([]uiSearchEntry, 0, len(cfg.Settings))
	for _, def := range uiSettingsIndex {
		entries = append(entries, uiSearchEntry{Key: def.Key, Label: def.Label, Tab: def.Tab})
	}
	for _, key := range keys {
		tab := "altro"
		if strings.HasPrefix(strings.ToLower(key), "score") {
			tab = "scores"
		}
		entries = append(entries, uiSearchEntry{Key: key, Label: key, Tab: tab})
	}
	// Special editors have no single setting key: point the search at their tab.
	for _, entry := range []uiSearchEntry{
		{Key: "", Label: "Feed RSS", Tab: "sources"},
		{Key: "", Label: "Indexer Torznab (Jackett / Prowlarr)", Tab: "sources"},
		{Key: "", Label: "Filtri per sorgente", Tab: "advanced"},
		{Key: "", Label: "Regole tag → cartella", Tab: "advanced"},
		{Key: "", Label: "Event hook", Tab: "advanced"},
		{Key: "", Label: "Cartelle osservate", Tab: "advanced"},
		{Key: "", Label: "Libreria (serie e film)", Tab: "libreria"},
		{Key: "", Label: "Traduzioni", Tab: "i18n"},
	} {
		entries = append(entries, entry)
	}
	page.SearchIndexJSON = uiJSON(entries)
	return page
}

func uiSettingFieldFor(key, label, value string) uiSettingField {
	field := uiSettingField{Key: key, Label: label, Value: value, Kind: uiSettingKind(key, value)}
	if field.Kind == "bool" {
		field.BoolValue, field.TrueValue, field.FalseValue = uiBoolValues(value)
	}
	return field
}

func uiSettingKind(key, value string) string {
	lowered := strings.ToLower(key)
	for _, secret := range []string{"password", "token", "api_key", "secret"} {
		if strings.Contains(lowered, secret) {
			return "secret"
		}
	}
	// A structured value (e.g. the `indexers` JSON) can embed credentials even
	// when its key does not: never render those in clear.
	loweredValue := strings.ToLower(value)
	for _, secret := range []string{`"api_key"`, `"password"`, `"token"`, `"secret"`} {
		if strings.Contains(loweredValue, secret) {
			return "secret"
		}
	}
	for _, area := range []string{"mappings", "extra_settings", "blacklist", "content_filters", "dht_bootstrap_nodes"} {
		if strings.Contains(lowered, area) {
			return "area"
		}
	}
	if strings.Contains(value, "\n") {
		return "area"
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
			Title:    "Archivio",
			Endpoint: "/api/archive",
			ItemsKey: "items",
			ColumnsJSON: uiJSON([]uiColumn{
				{Key: "title", Label: "Titolo"},
				{Key: "source", Label: "Sorgente"},
				{Key: "quality_score", Label: "Punteggio", Format: "number"},
				{Key: "added_at", Label: "Aggiunto"},
			}),
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
				{Key: "tag_url", Label: "Sorgente", Format: "getcomics"},
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
