package gextto

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	internalutils "github.com/buzzqw/gextto/internal/utils"
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
	Class       string
	Endpoint    string
	ItemsKey    string
	ColumnsJSON string
	ActionsJSON string
	Empty       string
	Note        string
	Search      bool
	SearchParam string
	SearchHint  string
	Query       string
	PageSize    int
	RefreshHint string
	// ManualOnly leaves the table idle until the user presses Aggiorna.
	ManualOnly bool
	Initial    string
	// Filter adds a client-side text filter over the rendered rows.
	Filter bool
	// Comics enables the extra panels of the comics page (download queue and
	// GetComics link finder) and the columns/actions of the queue table.
	Comics               bool
	DownloadsColumnsJSON string
	DownloadsActionsJSON string
	FooterForm           *uiFormSection
	FooterActions        []uiActionButton
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

// uiSettingField is one editable setting in the new settings page.
type uiSettingField struct {
	Key   string
	Label string
	Value string
	Kind  string // text|bool|area|secret|tags|select
	// Placeholder is the default value shown greyed out when the setting is
	// still unset, so the form mirrors rextto/extto instead of looking empty.
	Placeholder string
	// Managed marks a field the automatic optimization controls: it renders
	// read-only with the value "Auto" (like rextto) instead of an editable box.
	Managed bool
	// Disabled marks a field that does not apply to the selected torrent
	// engine: it renders read-only with an explanation, so the page never
	// suggests a control has an effect when the active engine ignores it.
	Disabled     bool
	DisabledNote string
	// DisabledEngine is the active engine named by DisabledNote, so the page
	// can render the note as a translatable sentence plus the engine name.
	DisabledEngine string
	// Options is set for Kind=="select": a fixed list to choose from.
	Options []uiFormOption
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
	// Unit, Zero and DependsOn come from uiSettingMetaByKey: the suffix of the
	// control, the meaning of the special value and the controlling switch.
	Unit      string
	Zero      string
	DependsOn string
	// Hidden hides a row whose controlling switch is off.
	Hidden bool
	// Default is the documented default in the field's own spelling (empty
	// when there is none worth restoring); Modified marks a saved value that
	// differs from it.
	Default  string
	Modified bool
	// Step is the step of a number control ("1" for integers, "any" otherwise).
	Step string
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
	Area   string
	Intro  string
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
	// Actions is an optional toolbar of API buttons shown at the top of the
	// tab (e.g. Ottimizza / Applica ora in Libtorrent).
	Actions *uiActionSection
	// EditorsFirst renders the structured editors (the feed editor) before the
	// plain fields, the order extto/rextto use on the Sorgenti tab.
	EditorsFirst bool
	// CheckboxGroups edits JSON-array settings as checkbox lists (Motori web,
	// Filtri contenuto) instead of textareas.
	CheckboxGroups []uiCheckboxGroup
	// FieldsGrid renders the group fields as a two-column grid of compact
	// fields (Punteggi), like rextto's score editor.
	FieldsGrid bool
	// Rename is the rename-composition editor of the Rinomina tab.
	Rename *uiRenameEditor
}

// uiRenameEditor composes the file-rename format: a preset, a custom template,
// the placeholders and a live preview.
type uiRenameEditor struct {
	Format   string
	Template string
	Formats  []uiFormOption
	Tokens   []uiRenameToken
}

// uiRenameToken is one placeholder button of the rename editor.
type uiRenameToken struct {
	Token string
	Label string
}

// uiSettingGroup is one titled block of settings rows. rextto groups the fields
// of a tab into labelled panels instead of a grid of cards.
type uiSettingGroup struct {
	Title  string
	Hint   string
	Fields []uiSettingField
	// Buttons are optional API actions rendered inside the group panel (the
	// qBittorrent-nox tile, for example).
	Buttons []uiActionButton
	// Grid renders the fields as a compact two-column grid (score weights).
	Grid bool
	// Collapsed renders the group closed: expert options, and the panels of
	// a torrent engine that is not the active one.
	Collapsed bool
}

// uiCheckboxOption is one checkbox of a uiCheckboxGroup.
type uiCheckboxOption struct {
	Value    string
	Label    string
	Selected bool
}

// uiCheckboxGroup edits a JSON-array setting as a list of checkboxes (Motori
// web, Filtri contenuto) instead of a raw textarea, exactly like rextto. Every
// change is saved immediately; when Custom is set the panel also offers an
// input to add a custom value.
type uiCheckboxGroup struct {
	Key               string
	Title             string
	Hint              string
	Options           []uiCheckboxOption
	Custom            bool
	CustomPlaceholder string
	// TestQuery, when set, shows a "test the selected values" toolbar: the
	// button queries /api/sources/health with this default term and reports
	// which sources responded. TestKind filters the health request and results
	// by `kind`.
	TestQuery string
	TestKind  string
	TestLabel string
}

// uiWebsearchEngines are the web search engines rextto exposes for gap filling.
var uiWebsearchEngines = []uiCheckboxOption{
	{Value: "bitsearch", Label: "BitSearch"},
	{Value: "tpb", Label: "The Pirate Bay"},
	{Value: "1337x", Label: "1337x"},
	{Value: "bt4g", Label: "BT4G"},
	{Value: "knaben", Label: "Knaben"},
	{Value: "nyaa", Label: "Nyaa"},
	{Value: "eztv", Label: "EZTV"},
	{Value: "btdig", Label: "BTDig"},
	{Value: "limetorrents", Label: "LimeTorrents"},
	{Value: "torrentz2", Label: "Torrentz2"},
	{Value: "torrentscsv", Label: "TorrentsCSV"},
}

// uiContentFilterOptions are the script/keyword filters rextto offers.
var uiContentFilterOptions = []uiCheckboxOption{
	{Value: "[non-latino]", Label: "Non latino (cirillico, arabo, CJK…)"},
	{Value: "[cjk]", Label: "CJK (cinese, giapponese, coreano)"},
	{Value: "[cirillico]", Label: "Cirillico"},
	{Value: "[arabo]", Label: "Arabo"},
	{Value: "[ebraico]", Label: "Ebraico"},
	{Value: "[thai]", Label: "Thai"},
	{Value: "[porno]", Label: "Porno / contenuti per adulti"},
}

// uiListField describes one column of a structured list editor.
// uiListFieldOption is one choice of a select field.
type uiListFieldOption struct {
	Value string
	Label string
}

type uiListField struct {
	Name        string
	Label       string
	Kind        string // text | number | bool | tags | secret | select
	Placeholder string
	// Options are the choices of a select field.
	Options []uiListFieldOption
	// Wide gives the field two grid tracks on wide screens (URLs, API keys).
	Wide bool
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
	// TestEndpoint, when set, renders a per-row "Testa" button that POSTs the
	// row's fields to that endpoint.
	TestEndpoint string
	Fields       []uiListField
}

var uiIndexerEditor = uiListEditor{
	Title:        "Indexer Torznab",
	Hint:         "Jackett, Prowlarr o altri indexer Torznab. L'URL è la base (Gextto aggiunge il percorso Torznab).",
	GetPath:      "/api/config",
	Unwrap:       "indexers",
	PostPath:     "/api/config/settings",
	PostKey:      "indexers",
	TestEndpoint: "/api/indexer/test",
	Fields: []uiListField{
		{Name: "name", Label: "Nome", Kind: "text", Placeholder: "jackett / prowlarr"},
		{Name: "url", Label: "URL base", Kind: "text", Placeholder: "http://127.0.0.1:9117", Wide: true},
		{Name: "api_key", Label: "API key", Kind: "secret", Wide: true},
		{Name: "manager", Label: "Tipo", Kind: "select", Options: []uiListFieldOption{
			{Value: "", Label: "Rilevato automaticamente"},
			{Value: "prowlarr", Label: "Prowlarr"},
			{Value: "jackett", Label: "Jackett"},
		}},
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
	Terms string `json:"terms,omitempty"`
}

// gxAutoManagedKey lists the settings the gx-torrent daemon controls by itself
// when self-management (gxtorrent_auto) is on: cache sizing and queue/slot
// tuning. They render as "Auto" instead of an editable value.
func gxAutoManagedKey(key string) bool {
	switch key {
	case "libtorrent_active_downloads", "libtorrent_active_seeds", "libtorrent_active_limit",
		"libtorrent_dont_count_slow_torrents",
		"libtorrent_dynamic_queue", "libtorrent_dynamic_queue_min", "libtorrent_dynamic_queue_max",
		"libtorrent_cache_size", "libtorrent_cache_expiry":
		return true
	}
	return false
}

// uiSettingsPageFrom builds one settings tab from the curated settings index
// and the live values. Legacy or dedicated-page-only keys are intentionally not
// copied into a catch-all tab. Only the active tab is rendered, which keeps the
// page small and usable on mobile.
func uiSettingsPageFrom(s *AppState, activeTab string) uiSettingsPage {
	cfg := latestConfig(s)

	labels := map[string]string{}
	order := []string{}
	for _, tab := range uiSettingsTabs {
		labels[tab.ID] = tab.Label
		order = append(order, tab.ID)
	}
	fieldsByTab := map[string][]uiSettingField{}
	activeBackend := uiActiveTorrentBackend(cfg)
	// The score weights lead the Qualità e upgrade tab; its other settings
	// (upgrade thresholds) follow them.
	for _, def := range uiScoreSettingDefs {
		fieldsByTab["scores"] = append(fieldsByTab["scores"], uiSettingFieldFor(def.Key, def.Label, cfg.Settings[def.Key]))
	}
	for _, def := range uiSettingsIndex {
		if _, ok := labels[def.Tab]; !ok {
			continue
		}
		value := ""
		if raw, present := cfg.Settings[def.Key]; present {
			value = raw
		}
		field := uiSettingFieldFor(def.Key, def.Label, value)
		if !uiBackendAllows(uiSettingAllowedBackends(def.Key), activeBackend) {
			field.Disabled = true
			field.DisabledNote = "Non attivo con il motore «" + uiBackendLabel(activeBackend) + "»."
			field.DisabledEngine = uiBackendLabel(activeBackend)
		}
		// With gx-torrent self-management on, the daemon owns the cache and the
		// queue tuning: show those settings as "Auto" (managed) instead of
		// editable. Turning gxtorrent_auto off makes them editable again.
		if activeBackend == BackendGxTorrent && settingsBool(cfg, "gxtorrent_auto", true) && gxAutoManagedKey(def.Key) {
			field.Managed = true
		}
		fieldsByTab[def.Tab] = append(fieldsByTab[def.Tab], field)
	}

	// Structured settings are edited with real forms (feed lines, indexer rows,
	// JSON editors turned into list editors); they are never repeated as raw
	// values.
	structured := map[string]struct{}{
		"url": {}, "indexers": {}, "source_filters": {},
		"tag_dir_rules": {}, "event_hooks": {}, "watched_folders": {},
		// Edited as checkbox groups (see uiSourcesCheckboxGroups).
		"websearch_engines": {}, "content_filters": {},
		// Edited by the rename-composition editor (see uiRenameEditor).
		"rename_format": {}, "rename_template": {},
	}
	indexed := map[string]struct{}{}
	for _, def := range uiSettingsIndex {
		indexed[def.Key] = struct{}{}
	}
	keys := make([]string, 0, len(cfg.Settings))
	for key := range cfg.Settings {
		// Legacy score_bonus_ita is not a supported scoring rule: language is
		// intentionally excluded from the quality score, so don't expose it as
		// an editable override in the Punteggi tab.
		if strings.EqualFold(key, "score_bonus_ita") {
			continue
		}
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
		// Unknown settings are normally legacy keys or values edited by a
		// dedicated page (backup, integrations, runtime controls, ...). Do not
		// expose them in a catch-all "Altro" tab: that made obsolete settings
		// such as aMule and aria2 look supported and duplicated current controls.
		// Quality score overrides are the one intentional exception; they are
		// grouped in the Punteggi tab and edited by ScoreEditor.
		if strings.HasPrefix(strings.ToLower(key), "score") {
			if uiScoreSettingKeys[key] || uiDeprecatedScoreSettingKeys[key] {
				continue
			}
			// Custom release groups have their own add/edit/delete interface in
			// the v2 score editor; don't expose them as anonymous key/value rows.
			if strings.HasPrefix(strings.ToLower(key), "score_group_") {
				continue
			}
			fieldsByTab["scores"] = append(fieldsByTab["scores"], uiSettingFieldFor(key, uiSettingAutoLabel(key), cfg.Settings[key]))
		}
	}

	special := map[string]bool{"sources": true, "scores": true, "system": true}
	tabs := make([]uiSettingsTabRef, 0, len(order))
	for _, tab := range uiSettingsTabs {
		if len(fieldsByTab[tab.ID]) == 0 && !special[tab.ID] {
			continue
		}
		tabs = append(tabs, uiSettingsTabRef{ID: tab.ID, Label: tab.Label, Area: tab.Area, Intro: tab.Intro, Count: len(fieldsByTab[tab.ID])})
	}
	if len(tabs) == 0 {
		tabs = append(tabs, uiSettingsTabRef{ID: "daemon", Label: "Daemon"})
	}

	active := uiSettingsCanonicalTab(activeTab)
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
	// With the continuous optimization on, the queue/cache fields are handled
	// by Rextto/Gextto: show them as "Auto" (read-only), like rextto does.
	if active == "performance" && settingsBool(cfg, "libtorrent_auto_optimize", false) {
		for index := range page.Fields {
			if uiManagedSetting[page.Fields[index].Key] {
				page.Fields[index].Managed = true
			}
		}
	}
	// The transfer backend is a fixed choice: render it as a select instead of
	// a free-text box.
	if active == "backend" {
		for index := range page.Fields {
			if page.Fields[index].Key == "torrent_backend" {
				page.Fields[index].Kind = "select"
				page.Fields[index].Options = uiTorrentBackendOptions(page.Fields[index].Value)
			}
		}
	}
	if active == "rename" {
		page.Rename = uiRenameEditorFrom(cfg)
	}
	uiHideDependentFields(page.Fields)
	page.Groups = uiSettingsGroups(active, page.Fields)
	if active == "backend" {
		// The binary path is not appended: the sentence stays translatable and
		// «Stato qBittorrent-nox» shows where the binary is installed.
		hint := "Scegli il motore dalla tendina «Motore torrent». Il binario gestito è scaricato e aggiornato da Gextto."
		qbittorrentButtons := []uiActionButton{
			{Label: "Installa / Ottimizza qBittorrent-nox", Class: "primary", Method: "POST", Path: "/api/torrent-backend/qbittorrent/update", Body: "{}", Hint: "Scarica o aggiorna qBittorrent-nox nella cartella dell'app e imposta le opzioni ottimali (URL locale, utente admin, gestione automatica)."},
			{Label: "Stato qBittorrent-nox", Method: "GET", Path: "/api/torrent-backend/qbittorrent/update", Body: "{}", Hint: "Mostra dove è installato qBittorrent-nox e se c'è un aggiornamento."},
			{Label: "Test connessione", Method: "POST", Path: "/api/torrent-backend/test", Body: "{}", Hint: "Verifica la connessione al qBittorrent-nox configurato."},
		}
		engineButtons := []uiActionButton{
			{Label: "Applica motore", Class: "primary", Method: "POST", Path: "/api/torrent-backend", Body: "{}", Hint: "Verifica il motore configurato e indica se serve un riavvio."},
		}
		for index := range page.Groups {
			switch {
			case strings.HasPrefix(page.Groups[index].Title, "qBittorrent"):
				page.Groups[index].Hint = hint
				page.Groups[index].Buttons = qbittorrentButtons
			case page.Groups[index].Title == "Motore torrent":
				page.Groups[index].Hint = "Dopo aver cambiato motore salva la scelta e premi «Applica motore»: Gextto verifica il motore e indica se serve un riavvio."
				page.Groups[index].Buttons = engineButtons
			}
		}
	}
	page.ShowSources = active == "sources"
	page.ShowI18n = active == "system"
	page.ListEditors = uiSettingsTabEditors(active)
	page.ShowEditors = len(page.ListEditors) > 0
	applyNow := uiActionButton{Label: "Applica ora", Method: "POST", Path: "/api/torrents/apply_settings", Body: "{}", Hint: "Riapplica subito le impostazioni libtorrent alla sessione attiva."}
	switch active {
	case "sources":
		// The Indexer Torznab editor and FlareSolverr live only in Integrazioni
		// (as in extto); here we keep the feed and the web-search/filter
		// settings, so the same form is not shown twice.
		page.EditorsFirst = true
		page.CheckboxGroups = uiSourcesCheckboxGroups(cfg)
	case "libtorrent":
		page.Actions = &uiActionSection{
			Label:   "Applica alla sessione",
			Hint:    "Le impostazioni salvate valgono dal ciclo successivo; «Applica ora» le riapplica subito alla sessione attiva.",
			Buttons: []uiActionButton{applyNow},
		}
		for index := range page.Groups {
			if page.Groups[index].Title == "Porte e interfacce" {
				page.Groups[index].Hint = "Porta peer usata dal motore torrent. «Test porte» verifica che sia in ascolto e inoltrata dal router (disponibile con gx-torrent)."
				page.Groups[index].Buttons = []uiActionButton{
					{Label: "Test porte", Method: "POST", Path: "/api/torrent-backend/portcheck", Body: "{}", Hint: "Controlla che la porta peer sia in ascolto e che il router la inoltri."},
				}
			}
		}
	case "performance":
		page.Actions = &uiActionSection{
			Label: "Ottimizzazione",
			Hint:  "Ottimizza calcola cache e buffer in base alla RAM e adatta la coda dinamica durante il funzionamento. Con l'ottimizzazione continua attiva i campi Download attivi, Seed attivi, Limite torrent attivi e Cache disco sono gestiti automaticamente (Auto).",
			Buttons: []uiActionButton{
				{Label: "Ottimizza", Class: "primary", Method: "POST", Path: "/api/torrents/optimize_settings", Body: "{}", Hint: "Applica una base sicura e suggerisce cache e buffer in base alla RAM."},
				applyNow,
			},
		}
	}

	entries := make([]uiSearchEntry, 0, len(cfg.Settings))
	for _, def := range uiSettingsIndex {
		entries = append(entries, uiSearchEntry{Key: def.Key, Label: def.Label, Tab: def.Tab, Terms: uiSettingSearchTerms[def.Key]})
	}
	for _, def := range uiScoreSettingDefs {
		entries = append(entries, uiSearchEntry{Key: def.Key, Label: def.Label, Tab: def.Tab})
	}
	for _, key := range keys {
		if strings.HasPrefix(strings.ToLower(key), "score") {
			entries = append(entries, uiSearchEntry{Key: key, Label: uiSettingAutoLabel(key), Tab: "scores"})
		}
	}
	// Special editors have no single setting key: point the search at their tab.
	entries = append(entries, uiSettingsEditorSearchEntries...)
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
		group := uiSettingGroup{Title: title, Fields: grouped[title], Collapsed: uiCollapsedSettingGroups[title]}
		// The score weights read best as a compact two-column grid.
		if tab == "scores" && len(grouped[title]) > 0 {
			_, group.Grid = uiScoreGroupTitle(grouped[title][0].Key)
		}
		groups = append(groups, group)
	}
	return groups
}

// uiSourcesCheckboxGroups builds the two checkbox editors of the Sorgenti tab
// (Motori web, Filtri contenuto) from the stored JSON arrays.
func uiSourcesCheckboxGroups(cfg *Config) []uiCheckboxGroup {
	engines := uiCheckboxGroup{
		Key:       "websearch_engines",
		Title:     "Motori web",
		Hint:      "Spunta i motori di ricerca da usare per i gap.",
		TestQuery: "1080p",
		TestKind:  "engine",
		TestLabel: "Testa motori",
	}
	for _, option := range uiWebsearchEngines {
		option.Selected = settingListContains(cfg, "websearch_engines", option.Value)
		engines.Options = append(engines.Options, option)
	}

	filters := uiCheckboxGroup{
		Key:               "content_filters",
		Title:             "Filtri contenuto esclusi",
		Hint:              "Le release che contengono queste parole o script (es. [non-latino], [porno]) vengono escluse. Le modifiche si salvano subito.",
		Custom:            true,
		CustomPlaceholder: "Filtro personalizzato",
	}
	known := map[string]bool{}
	for _, option := range uiContentFilterOptions {
		option.Selected = settingListContains(cfg, "content_filters", option.Value)
		known[strings.ToLower(option.Value)] = true
		filters.Options = append(filters.Options, option)
	}
	// Custom filters already stored (not one of the presets) stay visible and
	// removable like the preset ones.
	for _, value := range settingList(cfg, "content_filters") {
		if known[strings.ToLower(value)] {
			continue
		}
		filters.Options = append(filters.Options, uiCheckboxOption{Value: value, Label: value, Selected: true})
	}
	return []uiCheckboxGroup{engines, filters}
}

// settingList parses a JSON-array setting into its string items.
func settingList(cfg *Config, key string) []string {
	if cfg == nil {
		return nil
	}
	items, ok := uiJSONScalarList(cfg.Settings[key])
	if !ok {
		return nil
	}
	return items
}

// settingListContains reports whether a JSON-array setting contains a value.
func settingListContains(cfg *Config, key, value string) bool {
	for _, item := range settingList(cfg, key) {
		if strings.EqualFold(strings.TrimSpace(item), value) {
			return true
		}
	}
	return false
}

// uiSettingAutoLabel prettifies the label of a setting that is not in the
// curated index (score weights, internal keys) so a raw key is never shown as
// the field name.
func uiSettingAutoLabel(key string) string {
	for _, prefix := range []string{"score_res_", "score_source_", "score_codec_", "score_audio_", "score_bonus_"} {
		if strings.HasPrefix(key, prefix) {
			return strings.TrimPrefix(key, prefix)
		}
	}
	if strings.HasPrefix(key, "score_group_") {
		return "Gruppo " + strings.TrimPrefix(key, "score_group_")
	}
	return key
}

// uiSettingGroupTitle assigns a setting to a labelled group. The libtorrent tab
// is the only one large enough to need sub-groups; the others render a single
// panel with the tab name.
func uiSettingGroupTitle(tab, key string) string {
	if group := uiSettingGroupByKey[key]; group != "" {
		return group
	}
	// Only the score weights are not in the curated index (user-saved score
	// overrides included): they are grouped by their prefix.
	if tab == "scores" {
		if title, ok := uiScoreGroupTitle(key); ok {
			return title
		}
		return "Punteggi qualità"
	}
	return "Impostazioni"
}

// uiScoreGroupTitle returns the panel of a score weight from its prefix.
func uiScoreGroupTitle(key string) (string, bool) {
	lowered := strings.ToLower(key)
	switch {
	case strings.HasPrefix(lowered, "score_res_"):
		return "Risoluzione", true
	case strings.HasPrefix(lowered, "score_source_"):
		return "Sorgente", true
	case strings.HasPrefix(lowered, "score_codec_"):
		return "Codec", true
	case strings.HasPrefix(lowered, "score_audio_"):
		return "Audio", true
	case strings.HasPrefix(lowered, "score_bonus_"):
		return "Bonus", true
	case strings.HasPrefix(lowered, "score_group_"):
		return "Gruppi custom", true
	}
	return "", false
}

// uiCollapsedSettingGroups are panels closed by default: rarely needed knobs.
var uiCollapsedSettingGroups = map[string]bool{"Per esperti": true}

// uiHideDependentFields hides the rows whose controlling switch is off. A
// switch that is managed or not used by the active engine never hides
// anything: its own value is not what decides.
func uiHideDependentFields(fields []uiSettingField) {
	parents := map[string]uiSettingField{}
	for _, field := range fields {
		parents[field.Key] = field
	}
	for index := range fields {
		parent, ok := parents[fields[index].DependsOn]
		if !ok || parent.Kind != "bool" || parent.Managed || parent.Disabled {
			continue
		}
		fields[index].Hidden = !parent.BoolValue
	}
}

// uiSettingsTabEditors returns the structured list editors shown on a tab.
func uiSettingsTabEditors(tab string) []uiListEditor {
	paths := map[string][]string{
		"sources": {"/api/config/source-filters", "/api/watched-folders"},
		"rename":  {"/api/tag-dir-rules"},
		"notify":  {"/api/event-hooks"},
	}[tab]
	editors := []uiListEditor{}
	for _, path := range paths {
		for _, editor := range uiAdvancedEditors {
			if editor.GetPath == path {
				editors = append(editors, editor)
			}
		}
	}
	return editors
}

// uiSettingsEditorSearchEntries point the settings search at the structured
// editors, which have no single setting key.
var uiSettingsEditorSearchEntries = []uiSearchEntry{
	{Key: "", Label: "Feed RSS", Tab: "sources"},
	{Key: "", Label: "Motori web", Tab: "sources"},
	{Key: "", Label: "Filtri contenuto esclusi", Tab: "sources"},
	{Key: "", Label: "Filtri per sorgente", Tab: "sources"},
	{Key: "", Label: "Cartelle osservate", Tab: "sources"},
	{Key: "", Label: "Gruppi custom", Tab: "scores"},
	{Key: "", Label: "Composizione del nome", Tab: "rename"},
	{Key: "", Label: "Regole tag → cartella", Tab: "rename"},
	{Key: "", Label: "Event hook", Tab: "notify"},
	{Key: "", Label: "Traduzioni", Tab: "system"},
}

// uiManagedSetting lists the settings the continuous optimization drives: with
// `libtorrent_auto_optimize` on they render as "Auto" and cannot be edited.
var uiManagedSetting = map[string]bool{
	"libtorrent_active_downloads": true,
	"libtorrent_active_seeds":     true,
	"libtorrent_active_limit":     true,
	"libtorrent_cache_size":       true,
}

// uiLibtorrentEngineOnlySettings lists the options read only by the embedded
// libtorrent session (see libtorrent.go). Seed, stall, queue, speed, directory
// and RAM-disk options are intentionally excluded: the automation layer applies
// those with every engine, so they must stay editable.
//
// The network, queue and cache options are excluded too: gx-torrent reads the
// same libtorrent_* keys (gxNetworkArgs and gxQueuePolicy), so they must stay
// editable whichever engine is selected. Only knobs specific to the libtorrent
// settings_pack remain listed here.
var uiLibtorrentEngineOnlySettings = map[string]bool{
	"libtorrent_enabled":                           true,
	"libtorrent_auto_optimize":                     true,
	"libtorrent_extra_settings":                    true,
	"libtorrent_aio_threads":                       true,
	"libtorrent_alert_queue_size":                  true,
	"libtorrent_allow_multiple_connections_per_ip": true,
	"libtorrent_announce_interval":                 true,
	"libtorrent_announce_to_all_tiers":             true,
	"libtorrent_announce_to_all_trackers":          true,
	"libtorrent_connections_limit":                 true,
	"libtorrent_half_open_limit":                   true,
	"libtorrent_max_uploads_per_torrent":           true,
	"libtorrent_prefer_rc4":                        true,
	"libtorrent_torrent_connect_boost":             true,
	"libtorrent_upload_slots_limit":                true,
}

// uiSettingAllowedBackends lists the torrent engines a setting applies to. A
// nil/empty result means every engine.
func uiSettingAllowedBackends(key string) []string {
	switch {
	case strings.HasPrefix(key, "qbittorrent_"):
		return []string{BackendQbittorrent}
	case strings.HasPrefix(key, "gxtorrent_"):
		return []string{BackendGxTorrent}
	case uiLibtorrentEngineOnlySettings[key]:
		return []string{BackendEmbedded}
	}
	return nil
}

func uiBackendAllows(allowed []string, backend string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, candidate := range allowed {
		if candidate == backend {
			return true
		}
	}
	return false
}

// uiActiveTorrentBackend returns the configured transfer backend, defaulting to
// the embedded engine.
func uiActiveTorrentBackend(cfg *Config) string {
	return TorrentBackendName(cfg)
}

// uiRenameEditorFrom builds the rename-composition editor of the Rinomina tab.
func uiRenameEditorFrom(cfg *Config) *uiRenameEditor {
	format := strings.TrimSpace(cfg.RenameFormat)
	if format == "" {
		format = defaultRenameFormatValue
	}
	template := cfg.RenameTemplate
	if strings.TrimSpace(template) == "" {
		template = defaultRenameTemplateValue
	}
	return &uiRenameEditor{
		Format:   format,
		Template: template,
		Formats: []uiFormOption{
			{Value: "base", Label: "Base", Selected: format == "base"},
			{Value: "standard", Label: "Standard", Selected: format == "standard"},
			{Value: "full", Label: "Completo", Selected: format == "full" || format == "completo"},
			{Value: "custom", Label: "Personalizzato", Selected: format == "custom"},
		},
		Tokens: []uiRenameToken{
			{Token: "{Serie}", Label: "Serie"},
			{Token: "{Stagione}", Label: "Stagione"},
			{Token: "{Episodio}", Label: "Episodio"},
			{Token: "{Titolo}", Label: "Titolo"},
			{Token: "{Source}", Label: "Sorgente"},
			{Token: "{Gruppo}", Label: "Gruppo"},
			{Token: "{Risoluzione}", Label: "Risoluzione"},
			{Token: "{VideoCodec}", Label: "Video codec"},
			{Token: "{Audio}", Label: "Audio"},
			{Token: "{AudioCodec}", Label: "Audio codec"},
			{Token: "{Canali}", Label: "Canali"},
			{Token: "{HDR}", Label: "HDR"},
			{Token: "{Lingue}", Label: "Lingue"},
		},
	}
}

// uiTorrentBackendOptions is the combo list of the transfer engines.
func uiTorrentBackendOptions(value string) []uiFormOption {
	value = strings.TrimSpace(value)
	if value == "" {
		value = DefaultTorrentBackend
	}
	if strings.EqualFold(value, "anacrolix") {
		// Removed backend: at runtime it is normalised to the embedded engine.
		value = BackendEmbedded
	}
	options := []uiFormOption{}
	// The embedded libtorrent engine only exists in a build compiled with it;
	// when it is absent the option is not offered.
	if LibtorrentCompiled() {
		options = append(options, uiFormOption{Value: BackendEmbedded, Label: "libtorrent (integrato)", Selected: value == BackendEmbedded})
	}
	options = append(options,
		uiFormOption{Value: BackendQbittorrent, Label: uiBackendLabel(BackendQbittorrent), Selected: value == BackendQbittorrent},
		uiFormOption{Value: BackendGxTorrent, Label: "gx-torrent (predefinito)", Selected: value == BackendGxTorrent},
	)
	if value != BackendQbittorrent && value != BackendGxTorrent && !(LibtorrentCompiled() && value == BackendEmbedded) {
		options = append([]uiFormOption{{Value: value, Label: value + " (non valido)", Selected: true}}, options...)
		for index := 1; index < len(options); index++ {
			options[index].Selected = false
		}
	}
	return options
}

// uiSettingFieldFor builds the editable field of a setting from its stored
// value ("" when never saved), then applies the presentation metadata.
func uiSettingFieldFor(key, label, value string) uiSettingField {
	field := uiSettingFieldBase(key, label, value)
	uiApplySettingMeta(&field, value)
	return field
}

// uiApplySettingMeta sets the unit, special-value note, dependency, control
// type and default of a field. stored is the raw saved value ("" if unset).
func uiApplySettingMeta(field *uiSettingField, stored string) {
	meta := uiSettingMetaByKey[field.Key]
	field.Unit, field.Zero, field.DependsOn = meta.Unit, meta.Zero, meta.DependsOn
	def := uiSettingDefault(field.Key)
	switch field.Kind {
	case "secret", "structured", "tags", "area":
		return
	}
	switch meta.Kind {
	case "time":
		field.Kind = "time"
	case "days":
		field.Kind = "days"
		field.Options = uiDaysOptions(field.Value)
	case "url":
		if field.Kind == "text" {
			field.Kind = "url"
		}
	case "select":
		selected := strings.TrimSpace(field.Value)
		options := make([]uiFormOption, 0, len(meta.Options)+1)
		found := false
		for _, option := range meta.Options {
			option.Selected = option.Value == selected
			found = found || option.Selected
			options = append(options, option)
		}
		if selected != "" && !found {
			options = append([]uiFormOption{{Value: selected, Label: selected + " (personalizzata)", Selected: true}}, options...)
		}
		field.Kind, field.Options = "select", options
	}
	// The stored value alone cannot tell a boolean or a number when the key
	// was never saved or uses another spelling ("1"): the default can.
	if field.Kind == "text" {
		defLower := strings.ToLower(strings.TrimSpace(def))
		switch {
		case uiIsBoolSpelling(defLower) && !uiIsNumber(defLower) && (strings.TrimSpace(field.Value) == "" || uiIsBoolSpelling(field.Value)):
			field.Kind = "bool"
			field.BoolValue, field.TrueValue, field.FalseValue = uiBoolValues(field.Value)
		case uiIsNumber(def) && (strings.TrimSpace(field.Value) == "" || uiIsNumber(field.Value)):
			field.Kind = "number"
		}
	}
	if field.Kind == "number" && field.Step == "" {
		field.Step = "any"
		if uiScoreSettingKeys[field.Key] {
			field.Step = "1"
		}
	}
	if def == "" || uiSettingIsSecret(field.Key) || uiSettingNoPrefill[field.Key] {
		return
	}
	field.Default = def
	if field.Kind == "bool" {
		defBool, _, _ := uiBoolValues(def)
		field.Default = field.FalseValue
		if defBool {
			field.Default = field.TrueValue
		}
	}
	if strings.TrimSpace(stored) != "" && !uiSettingSameValue(*field, stored, def) {
		field.Modified = true
	}
}

func uiSettingFieldBase(key, label, value string) uiSettingField {
	// Mirror rextto/extto: when the key was never saved, show the documented
	// default instead of an empty control. Secrets and list-like values keep the
	// default only as a placeholder so they are never written by accident.
	placeholder := ""
	if strings.TrimSpace(value) == "" {
		if def := uiSettingDefault(key); def != "" {
			placeholder = def
			if !uiSettingIsSecret(key) && !uiSettingNoPrefill[key] {
				value = def
			}
		}
	}
	field := uiSettingField{
		Key:         key,
		Label:       label,
		Value:       value,
		Placeholder: placeholder,
		Kind:        uiSettingKind(key, value),
		Hint:        uiSettingTooltip(key),
	}
	if key == "cleanup_action" {
		selected := strings.TrimSpace(value)
		if selected != "delete" {
			selected = "move"
		}
		field.Kind = "select"
		field.Options = []uiFormOption{
			{Value: "move", Label: "Sposta nel trash", Selected: selected == "move"},
			{Value: "delete", Label: "Elimina definitivamente", Selected: selected == "delete"},
		}
	}
	if key == "libtorrent_encryption" {
		selected := strings.TrimSpace(value)
		if selected != "0" && selected != "2" {
			selected = "1"
		}
		field.Kind = "select"
		field.Options = []uiFormOption{
			{Value: "0", Label: "Disattivata", Selected: selected == "0"},
			{Value: "1", Label: "Attivata", Selected: selected == "1"},
			{Value: "2", Label: "Forzata", Selected: selected == "2"},
		}
	}
	if key == "libtorrent_outgoing_interface" {
		selected := strings.TrimSpace(value)
		field.Kind = "select"
		field.Options = []uiFormOption{{
			Value: "", Label: "Auto (interfaccia predefinita)", Selected: selected == "",
		}}
		found := false
		for _, iface := range internalutils.NetworkInterfaces() {
			if iface.Name == selected {
				found = true
			}
			label := iface.Name
			if iface.Kind != "" {
				label += " · " + iface.Kind
			}
			if iface.IP != "" {
				label += " · " + iface.IP
			}
			field.Options = append(field.Options, uiFormOption{Value: iface.Name, Label: label, Selected: iface.Name == selected})
		}
		if selected != "" && !found {
			field.Options = append([]uiFormOption{{
				Value: selected, Label: selected + " (non rilevata)", Selected: true,
			}}, field.Options...)
		}
	}
	if key == "tvdb_language" || key == "tmdb_language" || key == "default_language" {
		field.Kind = "select"
		field.Options = uiLanguageOptions(key, strings.TrimSpace(value))
	}
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

func uiLanguageOptions(key, selected string) []uiFormOption {
	type languageOption struct {
		Value string
		Label string
	}
	var options []languageOption
	if key == "tmdb_language" {
		options = []languageOption{
			{Value: "it-IT", Label: "Italiano"},
			{Value: "en-US", Label: "Inglese (USA)"},
			{Value: "en-GB", Label: "Inglese (Regno Unito)"},
			{Value: "es-ES", Label: "Spagnolo"},
			{Value: "fr-FR", Label: "Francese"},
			{Value: "de-DE", Label: "Tedesco"},
			{Value: "pt-PT", Label: "Portoghese"},
			{Value: "pt-BR", Label: "Portoghese (Brasile)"},
			{Value: "ja-JP", Label: "Giapponese"},
			{Value: "ko-KR", Label: "Coreano"},
			{Value: "zh-CN", Label: "Cinese"},
			{Value: "ru-RU", Label: "Russo"},
		}
	} else {
		options = []languageOption{
			{Value: "ita", Label: "Italiano"},
			{Value: "eng", Label: "Inglese"},
			{Value: "spa", Label: "Spagnolo"},
			{Value: "fra", Label: "Francese"},
			{Value: "deu", Label: "Tedesco"},
			{Value: "por", Label: "Portoghese"},
			{Value: "jpn", Label: "Giapponese"},
			{Value: "kor", Label: "Coreano"},
			{Value: "zho", Label: "Cinese"},
			{Value: "rus", Label: "Russo"},
		}
	}
	result := make([]uiFormOption, 0, len(options)+1)
	found := false
	for _, option := range options {
		isSelected := option.Value == selected
		if isSelected {
			found = true
		}
		result = append(result, uiFormOption{Value: option.Value, Label: option.Label, Selected: isSelected})
	}
	if selected != "" && !found {
		result = append([]uiFormOption{{Value: selected, Label: selected + " (personalizzata)", Selected: true}}, result...)
	}
	return result
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

// uiSettingIsSecret reports whether a setting key must never be rendered in
// clear nor prefilled with a default.
func uiSettingIsSecret(key string) bool {
	lowered := strings.ToLower(key)
	for _, secret := range []string{"password", "token", "api_key", "secret"} {
		if strings.Contains(lowered, secret) {
			return true
		}
	}
	// A proxy URL can embed credentials; a PIN is a credential too.
	return strings.HasSuffix(lowered, "_proxy") || strings.HasSuffix(lowered, "_pin")
}

func uiSettingKind(key, value string) string {
	lowered := strings.ToLower(key)
	if uiSettingIsSecret(key) {
		return "secret"
	}
	if uiScoreSettingKeys[key] {
		return "number"
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
			Class:    "series-table",
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
				{Key: "", Label: "Stato", Format: "series_status"},
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
			Class:    "movie-table",
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
	case "movie-history":
		return uiTableSpec{
			Title:    "Film scaricati",
			Class:    "movie-history-table",
			Endpoint: "/api/movies/history",
			ItemsKey: "items",
			ColumnsJSON: uiJSON([]uiColumn{
				{Key: "name", Label: "Titolo", Format: "truncate", Sortable: true},
				{Key: "year", Label: "Anno", Format: "number", Sortable: true},
				{Key: "quality_score", Label: "Punteggio", Format: "number", Sortable: true},
				{Key: "size_bytes", Label: "Dimensione", Format: "bytes", Sortable: true},
				{Key: "downloaded_at", Label: "Scaricato", Format: "datetime", Sortable: true},
			}),
			Empty:  "Nessun film scaricato.",
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
			Class:    "archive-table",
			Endpoint: "/api/archive",
			ItemsKey: "items",
			ColumnsJSON: uiJSON([]uiColumn{
				{Key: "title", Label: "Titolo", Format: "truncate"},
				{Key: "source", Label: "Sorgente", Format: "source"},
				{Key: "quality_score", Label: "Punteggio", Format: "number"},
				{Key: "added_at", Label: "Aggiunto", Format: "date"},
			}),
			Empty:       "Archivio vuoto.",
			Search:      true,
			SearchParam: "q",
			PageSize:    100,
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
				{Label: "Modifica", Kind: "comic-edit", Method: "POST", Path: "/api/comics", Body: "{}"},
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

// Maintenance and Integrations are rendered as panels pages
// (uiMaintenanceSections / uiIntegrationSections); the old action-page builders
// were superseded and removed.
