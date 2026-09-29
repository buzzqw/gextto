package gextto

// uiweb_sections.go defines the reusable page sections (table, actions, form,
// progress, comics link finder) and builds them for every menu, so the new UI
// exposes the same data and actions as the classic one.

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"
)

// uiPageSection is one block of a panel page.
type uiPageSection struct {
	Kind     string // table | actions | form | progress | comics_links | links | oauth | settings | list_editor | duplicates | ramdisk | folder_rename
	Group    string // optional grouping label: consecutive same-group blocks render side by side
	Table    uiTableSpec
	Action   uiActionSection
	Form     uiFormSection
	Progress uiProgressSection
	Links    uiLinksSection
	OAuth    uiOAuthSection
	Settings uiSettingsSection
	Editor   uiListEditor
	// Integration groups several sections in one provider tile (Trakt, Simkl).
	Integration uiIntegrationCardSection
}

// uiIntegrationCardSection renders a provider as a single tile: a header with
// the connection status and the child sections (OAuth, settings, watchlist,
// calendar) stacked inside the same panel.
type uiIntegrationCardSection struct {
	Title       string
	Status      string
	StatusClass string // ok | warn | ""
	Children    []uiPageSection
}

func sectionIntegration(card uiIntegrationCardSection) uiPageSection {
	return uiPageSection{Kind: "integration", Integration: card}
}

// integrationStatus returns the status label and badge class of a provider.
func integrationStatus(cfg *Config, prefix string) (string, string) {
	if settingsNonEmpty(cfg, prefix+"_access_token") {
		return "autenticato", "ok"
	}
	if settingsNonEmpty(cfg, prefix+"_client_id") {
		return "configurato, da autenticare", "warn"
	}
	return "non configurato", ""
}

// uiSettingsSection renders a group of individual setting rows, reusing the
// settings save machinery (one POST /api/config/settings per field). Buttons
// are optional actions rendered at the bottom of the same panel.
type uiSettingsSection struct {
	Title   string
	Hint    string
	Fields  []uiSettingField
	Buttons []uiActionButton
}

func sectionSettingsActions(title, hint string, fields []uiSettingField, buttons []uiActionButton) uiPageSection {
	return uiPageSection{Kind: "settings", Settings: uiSettingsSection{Title: title, Hint: hint, Fields: fields, Buttons: buttons}}
}

func sectionEditor(editor uiListEditor) uiPageSection {
	return uiPageSection{Kind: "list_editor", Editor: editor}
}

// uiSettingFields builds settings rows for the given keys, using the label from
// the generated settings index when available.
func uiSettingFields(cfg *Config, keys ...string) []uiSettingField {
	fields := make([]uiSettingField, 0, len(keys))
	for _, key := range keys {
		value := ""
		if cfg != nil {
			value = cfg.Settings[key]
		}
		fields = append(fields, uiSettingFieldFor(key, uiSettingLabel(key), value))
	}
	return fields
}

func uiSettingLabel(key string) string {
	for _, def := range uiSettingsIndex {
		if def.Key == key {
			return def.Label
		}
	}
	if label, ok := map[string]string{
		"jellyfin_url":     "Jellyfin URL",
		"jellyfin_api_key": "Jellyfin API key",
		"plex_url":         "Plex URL",
		"plex_token":       "Plex token",
		"flaresolverr_url": "FlareSolverr URL",
	}[key]; ok {
		return label
	}
	return key
}

// uiPageGroup is a set of sections that belong together. A group with more than
// one section is rendered as a two-column grid, like rextto's panels.
type uiPageGroup struct {
	Title    string
	Sections []uiPageSection
}

// uiGroupSections collects the sections that share a Group label, preserving
// first-seen order. Ungrouped sections stay full width.
func uiGroupSections(sections []uiPageSection) []uiPageGroup {
	groups := make([]uiPageGroup, 0, len(sections))
	positions := map[string]int{}
	for _, section := range sections {
		if section.Group == "" {
			groups = append(groups, uiPageGroup{Sections: []uiPageSection{section}})
			continue
		}
		if position, ok := positions[section.Group]; ok {
			groups[position].Sections = append(groups[position].Sections, section)
			continue
		}
		positions[section.Group] = len(groups)
		groups = append(groups, uiPageGroup{Title: section.Group, Sections: []uiPageSection{section}})
	}
	return groups
}

type uiLinkItem struct {
	Label string
	Href  string
	Hint  string
}

type uiLinksSection struct {
	Title string
	Hint  string
	Links []uiLinkItem
}

type uiOAuthSection struct {
	Name      string
	StartPath string
	PollPath  string
	Buttons   []uiActionButton
}

func sectionOAuth(section uiOAuthSection) uiPageSection {
	return uiPageSection{Kind: "oauth", OAuth: section}
}

type uiFormOption struct {
	Value    string
	Label    string
	Selected bool
}

type uiFormField struct {
	Name        string
	Label       string
	Placeholder string
	Value       string
	Kind        string // text | number | area | select | hidden
	Options     []uiFormOption
	Hint        string
	Browse      bool
}

type uiFormSection struct {
	Title  string
	Hint   string
	Path   string
	Method string
	Wrap   string // optional JSON object key around submitted fields
	Submit string
	Render string // optional result renderer: tmdb | releases
	// ManualAdd, when set, adds a secondary "Aggiungi manualmente" button that
	// opens the requirements completion modal for that kind.
	ManualAdd string
	Fields    []uiFormField
}

type uiProgressSection struct {
	Title    string
	Endpoint string
}

// uiPanelsPage is a page made of reusable sections.
type uiPanelsPage struct {
	Sections []uiPageSection
	Groups   []uiPageGroup
	// Stack renders every group as a full-width column instead of rextto's
	// two-column grid. Integrations, Manutenzione and Fumetti opt in so their
	// panels keep the same single-column rhythm as the Configurazione page.
	Stack bool
}

type uiDownloadsPage struct {
	Torrents uiTorrentsData
	Panels   []uiPageSection
	// Temporary speed limits, prefilled from the persisted settings so the
	// toolbar shows the values currently in force.
	TempDL      int64
	TempUL      int64
	TempActive  bool
	TempMinutes int64
	// Tag catalog and selection used by the toolbar filters.
	TagOptions          []string
	AutoRemoveCompleted bool
}

func sectionTable(spec uiTableSpec) uiPageSection {
	return uiPageSection{Kind: "table", Table: spec}
}

func sectionActions(section uiActionSection) uiPageSection {
	return uiPageSection{Kind: "actions", Action: section}
}

func sectionForm(form uiFormSection) uiPageSection {
	if form.Method == "" {
		form.Method = "POST"
	}
	return uiPageSection{Kind: "form", Form: form}
}

func sectionProgress(title, endpoint string) uiPageSection {
	return uiPageSection{Kind: "progress", Progress: uiProgressSection{Title: title, Endpoint: endpoint}}
}

func sectionComicsLinks() uiPageSection {
	return uiPageSection{Kind: "comics_links"}
}

func sectionSourcesCheck() uiPageSection {
	return uiPageSection{Kind: "sources_check"}
}

func sectionLinks(section uiLinksSection) uiPageSection {
	return uiPageSection{Kind: "links", Links: section}
}

func boolField(name, label string, value bool) uiFormField {
	return uiFormField{Name: name, Label: label, Kind: "select", Options: []uiFormOption{
		{Value: "true", Label: "Sì", Selected: value},
		{Value: "false", Label: "No", Selected: !value},
	}}
}

// boolFieldHint is boolField with the tooltip shown on the label.
func boolFieldHint(name, label, hint string, value bool) uiFormField {
	field := boolField(name, label, value)
	field.Hint = hint
	return field
}

// uiPanelsPageFor builds the section list of the pages that need more than one
// panel (library, archive, comics, maintenance, integrations).
func uiPanelsPageFor(view string, s *AppState) (uiPanelsPage, bool) {
	cfg := latestConfig(s)
	switch view {
	case "series":
		spec, _ := uiTableSpecFor("series")
		return uiPanelsPage{Sections: []uiPageSection{
			sectionForm(uiFormSection{
				Title: "Aggiungi una serie",
				Hint:  "Scrivi il titolo e cerca su TMDB, oppure usa \"Aggiungi manualmente\" per compilare i requisiti.",
				Path:  "/api/tmdb/search", Submit: "Cerca su TMDB", Render: "tmdb",
				ManualAdd: "series",
				Fields: []uiFormField{
					{Name: "kind", Kind: "hidden", Value: "series"},
					{Name: "query", Label: "Titolo", Placeholder: "Nome serie", Hint: "Titolo della serie da cercare su TMDB."},
				},
			}),
			sectionTable(spec),
		}}, true
	case "movies":
		spec, _ := uiTableSpecFor("movies")
		return uiPanelsPage{Sections: []uiPageSection{
			sectionForm(uiFormSection{
				Title: "Aggiungi un film",
				Hint:  "Scrivi il titolo e cerca su TMDB, oppure usa \"Aggiungi manualmente\" per compilare i requisiti.",
				Path:  "/api/tmdb/search", Submit: "Cerca su TMDB", Render: "tmdb",
				ManualAdd: "movie",
				Fields: []uiFormField{
					{Name: "kind", Kind: "hidden", Value: "movie"},
					{Name: "query", Label: "Titolo", Placeholder: "Titolo film", Hint: "Titolo del film da cercare su TMDB."},
				},
			}),
			sectionTable(spec),
		}}, true
	case "gaps":
		spec, _ := uiTableSpecFor("gaps")
		return uiPanelsPage{Sections: []uiPageSection{
			sectionTable(spec),
			sectionForm(uiFormSection{
				Title: "Cerca un episodio mancante",
				Hint:  "Cerca una release specifica per serie/stagione/episodio e accodala dai risultati.",
				Path:  "/api/missing/search", Submit: "Cerca", Render: "releases",
				Fields: []uiFormField{
					{Name: "series", Label: "Serie"},
					{Name: "season", Label: "Stagione", Kind: "number"},
					{Name: "episode", Label: "Episodio", Kind: "number"},
				},
			}),
		}}, true
	case "archive":
		spec, _ := uiTableSpecFor("archive")
		spec.ActionsJSON = uiJSON([]uiAction{
			{Label: "Scarica", Method: "POST", Path: "/api/archive/batch-download", Body: `{"items":[{"title":"{title}","magnet":"{magnet}","source":"{source}"}]}`},
			{Label: "Perché non questo?", Kind: "release-explain", Method: "POST", Path: ""},
			{Label: "Elimina", Class: "danger", Method: "POST", Path: "/api/archive/delete", Body: `{"magnet":"{magnet}"}`, Confirm: "Eliminare questa voce dall'archivio?"},
		})
		return uiPanelsPage{Sections: []uiPageSection{
			sectionTable(spec),
			sectionForm(uiFormSection{
				Title: "Aggiungi all'archivio",
				Hint:  "Incolla un magnet e (opzionale) titolo e sorgente per registrarlo nell'archivio.",
				Path:  "/api/archive/add", Submit: "Aggiungi",
				Fields: []uiFormField{
					{Name: "title", Label: "Titolo"},
					{Name: "magnet", Label: "Magnet", Kind: "area", Placeholder: "magnet:?xt=urn:btih:…"},
					{Name: "source", Label: "Sorgente"},
				},
			}),
		}}, true
	case "blocklist":
		spec, _ := uiTableSpecFor("blocklist")
		return uiPanelsPage{Sections: []uiPageSection{sectionTable(spec)}}, true
	case "comics":
		spec, _ := uiTableSpecFor("comics")
		weeklyEnabled := false
		weeklyFromDate := ""
		historyLimit := int64(100)
		if s.comics != nil {
			if value, err := s.comics.Setting("weekly_enabled", "no"); err == nil {
				weeklyEnabled = value == "yes" || value == "true" || value == "1"
			}
			if value, err := s.comics.Setting("weekly_from_date", ""); err == nil {
				weeklyFromDate = strings.TrimSpace(value)
			}
			historyLimit = s.comics.HistoryLimit()
		}
		group := func(name string, section uiPageSection) uiPageSection {
			section.Group = name
			return section
		}
		return uiPanelsPage{Stack: true, Sections: []uiPageSection{
			group("Aggiungi fumetto", sectionForm(uiFormSection{
				Title: "Esplora GetComics", Hint: "Cerca il titolo, poi scarica con Download Now o aggiungi il fumetto alla libreria.", Path: "/api/comics/explore", Submit: "Trova", Render: "comics",
				Fields: []uiFormField{{Name: "query", Label: "Titolo", Placeholder: "es. Poison Ivy #41"}},
			})),
			sectionTable(spec),
			group("Weekly pack", sectionForm(uiFormSection{
				Title: "Pianificazione settimanale", Hint: "Attiva il controllo automatico dei Weekly Pack. La data limita i pack da scaricare; non avvia il download di quelli precedenti.", Path: "/api/comics/weekly/settings", Submit: "Salva weekly",
				Fields: []uiFormField{
					boolField("enabled", "Weekly attivo", weeklyEnabled),
					{Name: "from_date", Label: "Scarica weekly pack a partire dal", Kind: "date", Value: weeklyFromDate, Hint: "Non cercare o scaricare Weekly Pack con data precedente a questa."},
				},
			})),
			group("Weekly pack", sectionForm(uiFormSection{
				Title: "Cerca un Weekly Pack", Hint: "Cerca il post del Weekly Pack su GetComics per la data scelta ed estrae i magnet/.torrent disponibili. Non avvia il download.", Path: "/api/comics/weekly/links", Submit: "Cerca weekly",
				Fields: []uiFormField{{Name: "date", Label: "Data pacchetto", Kind: "date", Hint: "Data del Weekly Pack da cercare."}},
			})),
			group("Download", sectionTable(uiTableSpec{
				Title:    "Storico fumetti",
				Endpoint: "/api/comics/history",
				ItemsKey: "items",
				ColumnsJSON: uiJSON([]uiColumn{
					{Key: "title", Label: "Titolo"}, {Key: "sent_at", Label: "Inviato"},
					{Key: "size_bytes", Label: "Dimensione", Format: "bytes"}, {Key: "post_url", Label: "Post"},
				}),
				ActionsJSON: uiJSON([]uiAction{
					{Label: "Elimina", Class: "danger", Method: "POST", Path: "/api/comics/history/delete", Body: `{"url":"{post_url}"}`, Confirm: "Eliminare questa voce dallo storico?"},
				}),
				FooterActions: []uiActionButton{{Label: "Svuota storico fumetti", Class: "danger", Method: "POST", Path: "/api/comics/history/clear", Body: "{}", Confirm: "Eliminare tutto lo storico degli scarichi dei fumetti?", Hint: "Elimina tutte le voci dello storico fumetti, senza toccare i Weekly Pack."}},
				FooterForm: &uiFormSection{
					Hint: "Numero massimo di elementi da conservare nello Storico fumetti e nello Storico Weekly Pack (1–500).",
					Path: "/api/comics/weekly/settings", Submit: "Salva storico",
					Fields: []uiFormField{{Name: "history_limit", Label: "Storico da conservare", Kind: "number", Value: strconv.FormatInt(historyLimit, 10)}},
				},
				Empty: "Storico vuoto.",
			})),
			group("Weekly pack", sectionTable(uiTableSpec{
				Title:    "Storico Weekly Pack",
				Endpoint: "/api/comics/weekly",
				ItemsKey: "items",
				ColumnsJSON: uiJSON([]uiColumn{
					{Key: "pack_date", Label: "Data"},
					{Key: "sent_at", Label: "Stato", Format: "weekly_status"},
				}),
				ActionsJSON: uiJSON([]uiAction{
					{Label: "Forza", Kind: "comic-weekly-force", Method: "POST", Path: "/api/comics/download", Body: "{}"},
				}),
				FooterForm: &uiFormSection{
					Hint: "Numero massimo di elementi da conservare nello Storico fumetti e nello Storico Weekly Pack (1–500).",
					Path: "/api/comics/weekly/settings", Submit: "Salva storico",
					Fields: []uiFormField{{Name: "history_limit", Label: "Storico da conservare", Kind: "number", Value: strconv.FormatInt(historyLimit, 10)}},
				},
				FooterActions: []uiActionButton{{Label: "Svuota storico Weekly Pack", Class: "danger", Method: "POST", Path: "/api/comics/weekly/history/clear", Body: "{}", Confirm: "Eliminare tutto lo storico dei Weekly Pack?", Hint: "Elimina tutte le voci dello storico Weekly Pack, senza toccare lo storico fumetti."}},
				Empty:         "Nessun Weekly Pack registrato.",
			})),
			sectionComicsLinks(),
		}}, true
	case "maintenance":
		return uiPanelsPage{Sections: uiMaintenanceSections(s, cfg), Stack: true}, true
	case "integrations":
		return uiPanelsPage{Sections: uiIntegrationSections(s, cfg), Stack: true}, true
	}
	return uiPanelsPage{}, false
}

func uiMaintenanceSections(s *AppState, cfg *Config) []uiPageSection {
	group := func(name string, section uiPageSection) uiPageSection {
		section.Group = name
		return section
	}
	return []uiPageSection{
		sectionActions(uiActionSection{Label: "Azioni", Hint: "Operazioni di manutenzione del daemon e della libreria.", Buttons: []uiActionButton{
			{Label: "Backup ora", Class: "primary", Method: "POST", Path: "/api/backup", Body: "{}", Confirm: "Creare ora uno snapshot di backup dei database?", Hint: "Crea subito uno snapshot di backup dei database."},
			{Label: "Pulisci trash", Method: "POST", Path: "/api/maintenance/clean-trash", Body: "{}", Confirm: "Eliminare definitivamente gli elementi nel cestino?", Hint: "Elimina definitivamente gli elementi nel cestino."},
			{Label: "Ricalcola punteggi", Method: "POST", Path: "/api/database/rescore", Body: "{}", Confirm: "Ricalcolare i punteggi delle release archiviate con i pesi attuali?", Hint: "Ricalcola lo score delle release archiviate con i pesi attuali."},
			{Label: "Scansiona archivi", Method: "POST", Path: "/api/scan-all-archives", Body: "{}", Confirm: "Rileggere le cartelle archivio e aggiornare la libreria?", Hint: "Rilegge le cartelle archivio e aggiorna la libreria."},
			{Label: "Aggiorna MediaInfo", Method: "POST", Path: "/api/maintenance/backfill-media-info", Body: "{}", Confirm: "Analizzare con ffprobe i file archiviati senza MediaInfo?", Hint: "Analizza con ffprobe i file archiviati senza MediaInfo."},
			{Label: "Rinomina tutto", Method: "POST", Path: "/api/rename-all", Body: "{}", Confirm: "Rinominare tutti i file archiviati secondo il formato configurato?", Hint: "Rinomina tutti i file archiviati secondo il formato configurato."},
			{Label: "Housekeeping", Method: "POST", Path: "/api/maintenance/housekeeping", Body: "{}", Confirm: "Eseguire l'housekeeping (pulizia dati tecnici e storico)?", Hint: "Pulizia dati tecnici e storico, senza toccare la libreria."},
			{Label: "Importa setup", Method: "POST", Path: "/api/setup/import", Body: "{}", Confirm: "Importare la configurazione di setup?", Hint: "Importa un setup esistente (extto)."},
			{Label: "Riavvia servizio", Class: "danger", Method: "POST", Path: "/api/service/restart", Body: "{}", Confirm: "Riavviare il servizio gextto?", Hint: "Riavvia il daemon Gextto."},
		}}),
		uiPageSection{Kind: "folder_rename"},
		sectionProgress("Progresso rinomina", "/api/rename-progress"),
		group("Libreria", uiPageSection{Kind: "duplicates"}),
		group("Libreria", uiPageSection{Kind: "db_optimize"}),
		uiPageSection{Kind: "ramdisk"},
		group("Pulizie", uiPageSection{Kind: "trash_panel"}),
		group("Pulizie", sectionForm(uiFormSection{
			Title: "Pulizia database", Hint: "Applica la retention a ciclo storico e log errori.", Path: "/api/db/prune", Submit: "Pulisci",
			Fields: []uiFormField{
				{Name: "retain_cycles", Label: "Cicli da conservare", Kind: "number", Value: "50", Hint: "Numero di statistiche dei cicli da conservare."},
				{Name: "error_age_days", Label: "Giorni errori", Kind: "number", Value: "7", Hint: "Elimina i log di errore più vecchi di N giorni."},
			},
		})),
		group("Backup", sectionForm(uiFormSection{
			Title: "Impostazioni backup",
			Hint:  "I valori vengono salvati nel formato usato dal daemon; lascia vuota la password per non modificarla.",
			Path:  "/api/backup/settings", Wrap: "values", Submit: "Salva backup",
			Fields: []uiFormField{
				{Name: "backup_retention", Label: "Backup da conservare", Kind: "number", Value: settingsOr(cfg, "backup_retention", "5")},
				{Name: "backup_schedule_hours", Label: "Intervallo (ore)", Kind: "number", Value: settingsOr(cfg, "backup_schedule_hours", "0")},
				{Name: "backup_schedule_at", Label: "Orario (HH:MM)", Value: settingsOr(cfg, "backup_schedule_at", "")},
				{Name: "backup_ftp_host", Label: "FTP host", Value: settingsOr(cfg, "backup_ftp_host", "")},
				{Name: "backup_ftp_user", Label: "FTP utente", Value: settingsOr(cfg, "backup_ftp_user", "")},
				{Name: "backup_ftp_path", Label: "FTP percorso", Value: settingsOr(cfg, "backup_ftp_path", "")},
				{Name: "backup_cloud_dir", Label: "Cartella cloud", Value: settingsOr(cfg, "backup_cloud_dir", "")},
				{Name: "backup_send_telegram", Kind: "select", Label: "Invia su Telegram", Options: []uiFormOption{{Value: "true", Label: "Sì", Selected: settingsBool(cfg, "backup_send_telegram", false)}, {Value: "false", Label: "No", Selected: !settingsBool(cfg, "backup_send_telegram", false)}}},
			},
		})),
		group("Backup", sectionTable(uiTableSpec{
			Title:    "Backup disponibili",
			Endpoint: "/api/backup/list",
			ItemsKey: "items",
			ColumnsJSON: uiJSON([]uiColumn{
				{Key: "label", Label: "Etichetta"}, {Key: "name", Label: "Nome"},
				{Key: "size_bytes", Label: "Dimensione", Format: "bytes"}, {Key: "modified", Label: "Modificato"},
			}),
			Empty: "Nessun backup creato.",
		})),
		uiPageSection{Kind: "sources_probe"},
	}
}

func uiIntegrationSections(s *AppState, cfg *Config) []uiPageSection {
	traktCalendarDays := settingsOr(cfg, "trakt_calendar_days", "7")
	simklCalendarDays := settingsOr(cfg, "simkl_calendar_days", "7")
	traktStatus, traktStatusClass := integrationStatus(cfg, "trakt")
	simklStatus, simklStatusClass := integrationStatus(cfg, "simkl")
	group := func(name string, section uiPageSection) uiPageSection {
		section.Group = name
		return section
	}
	return []uiPageSection{
		sectionIntegration(uiIntegrationCardSection{
			Title: "Trakt", Status: traktStatus, StatusClass: traktStatusClass,
			Children: []uiPageSection{
				sectionOAuth(uiOAuthSection{Name: "Accesso", StartPath: "/api/trakt/auth/start", PollPath: "/api/trakt/auth/poll", Buttons: []uiActionButton{
					{Label: "Refresh token", Method: "POST", Path: "/api/trakt/auth/refresh", Body: "{}", Hint: "Rinnova il token di accesso Trakt."},
					{Label: "Revoca", Class: "danger", Method: "POST", Path: "/api/trakt/auth/revoke", Body: "{}", Hint: "Revoca l'accesso e rimuove il token salvato."},
					{Label: "Importa watchlist", Method: "POST", Path: "/api/trakt/watchlist/import", Body: "{}", Hint: "Importa le serie della watchlist Trakt nella libreria."},
				}}),
				sectionForm(uiFormSection{
					Title: "Impostazioni", Hint: "Crea un'app API su trakt.tv e incolla client ID e secret.", Path: "/api/trakt/settings", Submit: "Salva Trakt",
					Wrap: "values",
					Fields: []uiFormField{
						{Name: "trakt_client_id", Label: "Client ID", Value: settingsOr(cfg, "trakt_client_id", ""), Hint: "Client ID dell'app creata su trakt.tv."},
						{Name: "trakt_client_secret", Label: "Client secret", Kind: "text", Hint: "Client secret dell'app creata su trakt.tv (non visualizzato)."},
						{Name: "trakt_calendar_days", Label: "Giorni calendario", Value: traktCalendarDays, Hint: "Quanti giorni avanti mostrare nel calendario Trakt."},
						boolFieldHint("trakt_watchlist_sync", "Sincronizza watchlist", "Sincronizza automaticamente la watchlist Trakt ad ogni ciclo.", settingsBool(cfg, "trakt_watchlist_sync", false)),
						boolFieldHint("trakt_scrobble_enabled", "Scrobble", "Invia a Trakt gli episodi visti (scrobble).", settingsBool(cfg, "trakt_scrobble_enabled", false)),
					},
				}),
				sectionTable(uiTableSpec{
					Title:    "Watchlist Trakt",
					Endpoint: "/api/trakt/watchlist",
					ItemsKey: "",
					ColumnsJSON: uiJSON([]uiColumn{
						{Key: "show", Label: "Serie"}, {Key: "movie", Label: "Film"}, {Key: "listed_at", Label: "Aggiunto"},
					}),
					Empty: "Watchlist vuota o Trakt non configurato.",
				}),
				sectionTable(uiTableSpec{
					Title:    "Calendario Trakt",
					Endpoint: "/api/trakt/calendar",
					ItemsKey: "",
					ColumnsJSON: uiJSON([]uiColumn{
						{Key: "first_aired", Label: "Quando"}, {Key: "episode", Label: "Episodio"}, {Key: "show", Label: "Serie"},
					}),
					Empty: "Nessuna uscita o Trakt non configurato.",
				}),
			},
		}),
		sectionIntegration(uiIntegrationCardSection{
			Title: "Simkl", Status: simklStatus, StatusClass: simklStatusClass,
			Children: []uiPageSection{
				sectionOAuth(uiOAuthSection{Name: "Accesso", StartPath: "/api/simkl/auth/start", PollPath: "/api/simkl/auth/poll", Buttons: []uiActionButton{
					{Label: "Revoca", Class: "danger", Method: "POST", Path: "/api/simkl/auth/revoke", Body: "{}", Hint: "Revoca l'accesso e rimuove il token salvato."},
					{Label: "Importa watchlist", Method: "POST", Path: "/api/simkl/watchlist/import", Body: "{}", Hint: "Importa le serie della watchlist Simkl nella libreria."},
				}}),
				sectionForm(uiFormSection{
					Title: "Impostazioni", Hint: "Usa il PIN dell'app Simkl per collegare l'account.", Path: "/api/simkl/settings", Submit: "Salva Simkl",
					Wrap: "values",
					Fields: []uiFormField{
						{Name: "simkl_client_id", Label: "Client ID", Value: settingsOr(cfg, "simkl_client_id", ""), Hint: "Client ID dell'app Simkl."},
						{Name: "simkl_calendar_days", Label: "Giorni calendario", Value: simklCalendarDays, Hint: "Quanti giorni avanti mostrare nel calendario Simkl."},
						{Name: "simkl_watchlist_status", Label: "Stato watchlist", Kind: "select", Hint: "Stato assegnato alle serie importate nella watchlist Simkl.", Options: []uiFormOption{
							{Value: "plantowatch", Label: "Da guardare", Selected: settingsOr(cfg, "simkl_watchlist_status", "plantowatch") == "plantowatch"},
							{Value: "watching", Label: "In visione", Selected: settingsOr(cfg, "simkl_watchlist_status", "") == "watching"},
							{Value: "completed", Label: "Completato", Selected: settingsOr(cfg, "simkl_watchlist_status", "") == "completed"},
						}},
						boolFieldHint("simkl_mark_watched", "Segna come visto", "Segna come visti su Simkl gli episodi scaricati.", settingsBool(cfg, "simkl_mark_watched", false)),
					},
				}),
				sectionTable(uiTableSpec{
					Title:       "Watchlist Simkl",
					Endpoint:    "/api/simkl/watchlist",
					ItemsKey:    "shows",
					ColumnsJSON: uiJSON([]uiColumn{{Key: "show", Label: "Serie"}}),
					Empty:       "Watchlist vuota o Simkl non configurato.",
				}),
				sectionTable(uiTableSpec{
					Title:    "Calendario Simkl",
					Endpoint: "/api/simkl/calendar",
					ItemsKey: "",
					ColumnsJSON: uiJSON([]uiColumn{
						{Key: "date", Label: "Quando"}, {Key: "episode", Label: "Episodio"}, {Key: "show", Label: "Serie"},
					}),
					Empty: "Nessuna uscita o Simkl non configurato.",
				}),
			},
		}),
		group("Media server", sectionSettingsActions("Jellyfin", "URL del server e API key (Jellyfin → Dashboard → API Keys).", uiSettingFields(cfg, "jellyfin_url", "jellyfin_api_key"), []uiActionButton{
			{Label: "Test connessione", Method: "POST", Path: "/api/jellyfin/test", Body: "{}", Hint: "Verifica che Jellyfin risponda."},
			{Label: "Aggiorna libreria", Method: "POST", Path: "/api/jellyfin/refresh", Body: "{}", Hint: "Chiede a Jellyfin di aggiornare la libreria."},
		})),
		group("Media server", sectionSettingsActions("Plex", "URL del server e token X-Plex-Token.", uiSettingFields(cfg, "plex_url", "plex_token"), []uiActionButton{
			{Label: "Test connessione", Method: "POST", Path: "/api/plex/test", Body: "{}", Hint: "Verifica che Plex risponda."},
			{Label: "Aggiorna libreria", Method: "POST", Path: "/api/plex/refresh", Body: "{}", Hint: "Chiede a Plex di aggiornare la libreria."},
		})),
		sectionEditor(uiIndexerEditor),
		sectionSettingsActions("FlareSolverr", "Proxy usato per superare Cloudflare sui siti di ricerca.", uiSettingFields(cfg, "flaresolverr_url"), []uiActionButton{
			{Label: "Test FlareSolverr", Method: "POST", Path: "/api/flaresolverr/test", Body: "{}", Hint: "Verifica che FlareSolverr risponda."},
		}),
		sectionSourcesCheck(),
		sectionLinks(uiLinksSection{
			Title: "Handler del browser",
			Hint:  "Scarica gli script per aprire magnet e file .torrent direttamente in Gextto.",
			Links: []uiLinkItem{
				{Label: "Magnet handler", Href: "/api/browser-handlers/download?file=gextto-magnet"},
				{Label: "Torrent handler", Href: "/api/browser-handlers/download?file=gextto-torrent"},
				{Label: "Magnet .desktop", Href: "/api/browser-handlers/download?file=gextto-magnet.desktop"},
				{Label: "Torrent .desktop", Href: "/api/browser-handlers/download?file=gextto-torrent.desktop"},
				{Label: "install.sh", Href: "/api/browser-handlers/download?file=install.sh"},
			},
		}),
	}
}

// uiDownloadsPageFor builds the download page with the torrent table plus the
// tag and temporary-limit panels of the classic interface.
func uiDownloadsPageFor(s *AppState) uiDownloadsPage {
	cfg := latestConfig(s)
	page := uiDownloadsPage{
		Torrents:            uiTorrentsDataFrom(s),
		TempDL:              uiSettingInt(cfg, "libtorrent_temp_dl_limit"),
		TempUL:              uiSettingInt(cfg, "libtorrent_temp_ul_limit"),
		AutoRemoveCompleted: settingsBool(cfg, "auto_remove_completed", false),
		TagOptions:          uiDownloadTagOptions(s, cfg),
	}
	if uiSettingString(cfg, "libtorrent_temp_limit_enabled") == "1" {
		until := uiSettingInt(cfg, "libtorrent_temp_limit_until")
		if remaining := until - time.Now().Unix(); remaining > 0 {
			page.TempActive = true
			page.TempMinutes = (remaining + 59) / 60
		}
	}
	page.Panels = []uiPageSection{
		sectionTable(uiTableSpec{
			Title:    "Storico download",
			Class:    "download-history-table",
			Endpoint: "/api/torrents/history",
			ItemsKey: "items",
			ColumnsJSON: uiJSON([]uiColumn{
				{Key: "name", Label: "Nome"}, {Key: "kind", Label: "Tipo"},
				{Key: "tag", Label: "Tag NAS", Format: "nas_tag"}, {Key: "quality_score", Label: "Punteggio", Format: "number"},
				{Key: "status", Label: "Stato"},
				{Key: "processed_path", Label: "Cartella libreria / NAS", Format: "folder"},
				{Key: "source", Label: "Sorgente", Format: "source"}, {Key: "completed_at", Label: "Concluso", Format: "datetime"},
			}),
			Empty: "Nessun download nello storico.", Search: true, SearchParam: "q",
		}),
	}
	return page
}

// uiSettingString returns a setting value or the empty string.
func uiSettingString(cfg *Config, key string) string {
	if cfg == nil {
		return ""
	}
	return cfg.Settings[key]
}

// uiSettingInt parses an integer setting, returning 0 when unset or invalid.
func uiSettingInt(cfg *Config, key string) int64 {
	value := strings.TrimSpace(uiSettingString(cfg, key))
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

// uiDownloadTagOptions merges the registered download tags with the tags in use
// by the current torrents, so the toolbar filter and the assign list are the
// same catalog the classic UI builds.
func uiDownloadTagOptions(s *AppState, cfg *Config) []string {
	seen := map[string]string{}
	add := func(tag string) {
		trimmed := strings.TrimSpace(tag)
		if trimmed == "" {
			return
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; !ok {
			seen[key] = trimmed
		}
	}
	if cfg != nil {
		if raw := strings.TrimSpace(cfg.Settings["download_tags"]); raw != "" {
			var items []string
			if err := json.Unmarshal([]byte(raw), &items); err == nil {
				for _, item := range items {
					add(item)
				}
			}
		}
	}
	if s != nil && s.db != nil {
		if pairs, err := s.db.TorrentTags(); err == nil {
			for _, pair := range pairs {
				for _, part := range strings.Split(pair[1], ",") {
					add(part)
				}
			}
		}
	}
	options := make([]string, 0, len(seen))
	for _, value := range seen {
		options = append(options, value)
	}
	sort.Slice(options, func(i, j int) bool { return strings.ToLower(options[i]) < strings.ToLower(options[j]) })
	return options
}
