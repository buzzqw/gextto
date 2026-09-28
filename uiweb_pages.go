package gextto

import (
	"encoding/json"
	"sort"
	"strconv"
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
	// Sortable renders a clickable header that sorts the client-side table.
	Sortable bool `json:"sortable,omitempty"`
}

type uiAction struct {
	Label   string `json:"label"`
	Class   string `json:"class,omitempty"`
	Method  string `json:"method"`
	Path    string `json:"path"`
	Body    string `json:"body,omitempty"`
	Confirm string `json:"confirm,omitempty"`
	// Kind selects a client-side action instead of a plain API call:
	// "library-toggle" (enable/disable) or "library-remove" (delete), both
	// implemented as a read-modify-write of /api/config/library.
	Kind string `json:"kind,omitempty"`
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
	// Filter adds a client-side text filter over the rendered rows.
	Filter bool
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
	Kind  string // text|bool|area|secret|tags
	// Hint is the descriptive tooltip shown on the label (ported from rextto).
	Hint string
	// BoolValue, TrueValue and FalseValue are set only for Kind=="bool": they
	// keep the original spelling (yes/no, 1/0, on/off, true/false) so saving the
	// select never turns a "yes" into a "true" that strict readers would reject.
	BoolValue  bool
	TrueValue  string
	FalseValue string
	// TagJSON is set for Kind=="tags": the value was a JSON array of scalars and
	// must be saved back as a JSON array (otherwise comma separated).
	TagJSON bool
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
	Groups          []uiSettingGroup
	ShowSources     bool
	ShowEditors     bool
	ShowI18n        bool
	ListEditors     []uiListEditor
	SearchIndexJSON string
}

// uiSettingGroup is one titled block of settings rows. rextto groups the fields
// of a tab into labelled panels instead of a grid of cards.
type uiSettingGroup struct {
	Title  string
	Fields []uiSettingField
}

// uiListField describes one column of a structured list editor.
type uiListField struct {
	Name        string
	Label       string
	Kind        string // text | number | bool | tags | secret
	Placeholder string
}

// uiListEditor edits a list of structured records with real form rows, so the
// interface never shows raw JSON. GetPath/Unwrap load the list; PostPath plus
// Wrap (or PostKey for a setting) save it back.
type uiListEditor struct {
	Title    string
	Hint     string
	GetPath  string
	PostPath string
	Unwrap   string
	Wrap     string
	// PostKey, when set, saves the list as the JSON value of that setting
	// through /api/config/settings (used for the indexers).
	PostKey string
	Fields  []uiListField
}

var uiIndexerEditor = uiListEditor{
	Title:    "Indexer Torznab",
	Hint:     "Jackett, Prowlarr o altri indexer Torznab. L'URL è la base (Gextto aggiunge il percorso Torznab).",
	GetPath:  "/api/config",
	Unwrap:   "indexers",
	PostPath: "/api/config/settings",
	PostKey:  "indexers",
	Fields: []uiListField{
		{Name: "name", Label: "Nome", Kind: "text", Placeholder: "jackett / prowlarr"},
		{Name: "url", Label: "URL base", Kind: "text", Placeholder: "http://127.0.0.1:9117"},
		{Name: "api_key", Label: "API key", Kind: "secret"},
		{Name: "enabled", Label: "Attivo", Kind: "bool"},
	},
}

var uiAdvancedEditors = []uiListEditor{
	{
		Title: "Filtri per sorgente", Hint: "Parole chiave da accettare o scartare per una sorgente.",
		GetPath: "/api/config/source-filters", Unwrap: "filters", PostPath: "/api/config/source-filters", Wrap: "filters",
		Fields: []uiListField{
			{Name: "source", Label: "Sorgente", Kind: "text"},
			{Name: "keywords", Label: "Parole chiave", Kind: "tags", Placeholder: "separate da virgola"},
			{Name: "enabled", Label: "Attivo", Kind: "bool"},
		},
	},
	{
		Title: "Regole tag → cartella", Hint: "Associa un tag del torrent a una cartella temporanea e finale.",
		GetPath: "/api/tag-dir-rules", Unwrap: "items", PostPath: "/api/tag-dir-rules",
		Fields: []uiListField{
			{Name: "tag", Label: "Tag", Kind: "text"},
			{Name: "temp_dir", Label: "Cartella temporanea", Kind: "text"},
			{Name: "final_dir", Label: "Cartella finale", Kind: "text"},
		},
	},
	{
		Title: "Event hook", Hint: "Esegue un programma su determinati eventi.",
		GetPath: "/api/event-hooks", Unwrap: "items", PostPath: "/api/event-hooks",
		Fields: []uiListField{
			{Name: "name", Label: "Nome", Kind: "text"},
			{Name: "enabled", Label: "Attivo", Kind: "bool"},
			{Name: "events", Label: "Eventi", Kind: "tags", Placeholder: "vuoto = tutti"},
			{Name: "program", Label: "Programma", Kind: "text"},
			{Name: "args", Label: "Argomenti", Kind: "text"},
			{Name: "timeout_secs", Label: "Timeout (s)", Kind: "number"},
		},
	},
	{
		Title: "Cartelle osservate", Hint: "Aggiunge automaticamente i .torrent trovati in queste cartelle.",
		GetPath: "/api/watched-folders", Unwrap: "items", PostPath: "/api/watched-folders",
		Fields: []uiListField{
			{Name: "path", Label: "Cartella", Kind: "text"},
			{Name: "enabled", Label: "Attiva", Kind: "bool"},
			{Name: "recursive", Label: "Ricorsiva", Kind: "bool"},
			{Name: "delete_after", Label: "Elimina dopo", Kind: "bool"},
		},
	},
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

	// Structured settings are edited with real forms (feed lines, indexer rows,
	// JSON editors turned into list editors); they are never repeated as raw
	// values.
	structured := map[string]struct{}{
		"url": {}, "indexers": {}, "source_filters": {},
		"tag_dir_rules": {}, "event_hooks": {}, "watched_folders": {},
	}
	indexed := map[string]struct{}{}
	for _, def := range uiSettingsIndex {
		indexed[def.Key] = struct{}{}
	}
	keys := make([]string, 0, len(cfg.Settings))
	for key := range cfg.Settings {
		if _, ok := indexed[key]; ok {
			continue
		}
		if _, ok := structured[key]; ok {
			continue
		}
		// Internal markers and settings with a dedicated editor are not repeated.
		if strings.HasPrefix(key, "_") || key == "download_tags" {
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

	special := map[string]bool{"sources": true, "advanced": true, "i18n": true}
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
	}
	page.Groups = uiSettingsGroups(active, page.Fields)
	page.ShowSources = active == "sources"
	page.ShowEditors = active == "advanced"
	page.ShowI18n = active == "i18n"
	switch active {
	case "sources":
		page.ListEditors = []uiListEditor{uiIndexerEditor}
	case "advanced":
		page.ListEditors = uiAdvancedEditors
	}

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
		{Key: "", Label: "Traduzioni", Tab: "i18n"},
	} {
		entries = append(entries, entry)
	}
	page.SearchIndexJSON = uiJSON(entries)
	return page
}

// uiSettingsGroups splits the fields of a tab into the titled panels rextto
// uses, keeping the original order both of the groups and of the fields.
func uiSettingsGroups(tab string, fields []uiSettingField) []uiSettingGroup {
	if len(fields) == 0 {
		return nil
	}
	order := []string{}
	grouped := map[string][]uiSettingField{}
	for _, field := range fields {
		title := uiSettingGroupTitle(tab, field.Key)
		if _, ok := grouped[title]; !ok {
			order = append(order, title)
		}
		grouped[title] = append(grouped[title], field)
	}
	groups := make([]uiSettingGroup, 0, len(order))
	for _, title := range order {
		groups = append(groups, uiSettingGroup{Title: title, Fields: grouped[title]})
	}
	return groups
}

// uiSettingGroupTitle assigns a setting to a labelled group. The libtorrent tab
// is the only one large enough to need sub-groups; the others render a single
// panel with the tab name.
func uiSettingGroupTitle(tab, key string) string {
	lowered := strings.ToLower(key)
	switch tab {
	case "daemon":
		return "Daemon"
	case "advanced":
		return "Avanzate"
	case "acquisition":
		return "Acquisizione automatica"
	case "notify":
		return "Notifiche"
	case "paths":
		return "Percorsi runtime"
	case "rename":
		return "Rinomina e pulizia"
	case "scores":
		return "Punteggi qualità"
	case "altro":
		return "Altre impostazioni"
	case "libtorrent":
		switch {
		case strings.Contains(lowered, "ramdisk") || strings.Contains(lowered, "port"):
			return "RAM disk e porte"
		case strings.Contains(lowered, "dl_limit") || strings.Contains(lowered, "ul_limit") || strings.Contains(lowered, "sched"):
			return "Velocità e programmazione"
		case strings.Contains(lowered, "extra_settings"):
			return "Impostazioni avanzate"
		case strings.Contains(lowered, "connections") || strings.Contains(lowered, "cache") ||
			strings.Contains(lowered, "aio") || strings.Contains(lowered, "alert") ||
			strings.Contains(lowered, "half_open") || strings.Contains(lowered, "upload_slots") ||
			strings.Contains(lowered, "max_"):
			return "Connessioni e prestazioni"
		case strings.Contains(lowered, "dht") || strings.Contains(lowered, "pex") ||
			strings.Contains(lowered, "lsd") || strings.Contains(lowered, "upnp") ||
			strings.Contains(lowered, "natpmp") || strings.Contains(lowered, "utp") ||
			strings.Contains(lowered, "encryption") || strings.Contains(lowered, "proxy") ||
			strings.Contains(lowered, "ipfilter") || strings.Contains(lowered, "interface") ||
			strings.Contains(lowered, "tracker") || strings.Contains(lowered, "announce"):
			return "Protocolli e rete"
		default:
			return "Generale"
		}
	}
	return "Impostazioni"
}

func uiSettingFieldFor(key, label, value string) uiSettingField {
	field := uiSettingField{Key: key, Label: label, Value: value, Kind: uiSettingKind(key, value), Hint: uiSettingTooltip(key)}
	if items, ok := uiJSONScalarList(value); ok {
		field.Kind = "tags"
		field.TagJSON = true
		field.Value = strings.Join(items, "\n")
		return field
	}
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		var parsed any
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			switch parsed.(type) {
			case map[string]any, []any:
				// A structured object (or array of objects) is never shown as
				// raw JSON; it is edited through its dedicated editor.
				field.Kind = "structured"
				return field
			}
		}
	}
	if field.Kind == "bool" {
		field.BoolValue, field.TrueValue, field.FalseValue = uiBoolValues(value)
	}
	return field
}

// uiJSONScalarList reports whether value is a JSON array of scalars and returns
// its items as strings.
func uiJSONScalarList(value string) ([]string, bool) {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "[") {
		return nil, false
	}
	var items []any
	if err := json.Unmarshal([]byte(trimmed), &items); err != nil {
		return nil, false
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		switch typed := item.(type) {
		case string:
			out = append(out, typed)
		case float64:
			out = append(out, strconv.FormatFloat(typed, 'f', -1, 64))
		case bool:
			out = append(out, strconv.FormatBool(typed))
		default:
			return nil, false
		}
	}
	return out, true
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
			Title:    "Serie monitorate",
			Endpoint: "/api/config/library",
			ItemsKey: "series",
			ColumnsJSON: uiJSON([]uiColumn{
				{Key: "name", Label: "Nome", Format: "series_link", Sortable: true},
				{Key: "seasons", Label: "Stagioni"},
				{Key: "quality", Label: "Qualità"},
				{Key: "language", Label: "Lingua"},
				{Key: "", Label: "Ep.", Format: "episodes"},
				{Key: "", Label: "Complet.", Format: "completion"},
				{Key: "last_downloaded_at", Label: "Ultimo"},
				{Key: "enabled", Label: "Stato", Format: "enabled"},
			}),
			ActionsJSON: uiJSON([]uiAction{
				{Label: "Pausa", Kind: "library-toggle", Method: "POST", Path: "", Body: `{"enabled":false}`, Confirm: ""},
				{Label: "Elimina", Class: "danger", Kind: "library-remove", Method: "POST", Path: "", Body: "{}", Confirm: "Eliminare questa serie dalla libreria?"},
			}),
			Empty:  "Nessuna serie monitorata.",
			Filter: true,
		}, true
	case "movies":
		return uiTableSpec{
			Title:    "Film monitorati",
			Endpoint: "/api/config/library",
			ItemsKey: "movies",
			ColumnsJSON: uiJSON([]uiColumn{
				{Key: "name", Label: "Nome", Format: "movie_link", Sortable: true},
				{Key: "year", Label: "Anno", Format: "number", Sortable: true},
				{Key: "quality", Label: "Qualità", Sortable: true},
				{Key: "language", Label: "Lingua", Sortable: true},
				{Key: "enabled", Label: "Stato", Format: "enabled"},
			}),
			ActionsJSON: uiJSON([]uiAction{
				{Label: "Pausa", Kind: "library-toggle", Method: "POST", Path: "", Body: `{"enabled":false}`},
				{Label: "Elimina", Class: "danger", Kind: "library-remove", Method: "POST", Path: "", Body: "{}", Confirm: "Eliminare questo film dalla libreria?"},
			}),
			Empty:  "Nessun film monitorizzato.",
			Filter: true,
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
			ActionsJSON: uiJSON([]uiAction{
				{Label: "Cerca", Kind: "gap-search", Class: "primary", Method: "POST", Path: "", Body: "{}"},
				{Label: "Ignora", Method: "POST", Path: "/api/episodes/{series}/{season}/{episode}/ignore", Body: `{"ignored":true,"reason":"ui"}`, Confirm: "Ignorare questo episodio mancante?"},
			}),
			Empty: "Nessun episodio mancante.",
		}, true
	case "archive":
		return uiTableSpec{
			Title:    "Archivio",
			Endpoint: "/api/archive",
			ItemsKey: "items",
			ColumnsJSON: uiJSON([]uiColumn{
				{Key: "title", Label: "Titolo", Format: "truncate"},
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
