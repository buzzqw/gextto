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
	Kind     string // table | actions | form | progress | comics_links | links | oauth
	Group    string // optional grouping label: consecutive same-group blocks render side by side
	Table    uiTableSpec
	Action   uiActionSection
	Form     uiFormSection
	Progress uiProgressSection
	Links    uiLinksSection
	OAuth    uiOAuthSection
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
	Kind        string // text | number | area | select
	Options     []uiFormOption
}

type uiFormSection struct {
	Title  string
	Hint   string
	Path   string
	Method string
	Wrap   string // optional JSON object key around submitted fields
	Submit string
	Render string // optional result renderer: tmdb | releases
	Fields []uiFormField
}

type uiProgressSection struct {
	Title    string
	Endpoint string
}

// uiPanelsPage is a page made of reusable sections.
type uiPanelsPage struct {
	Sections []uiPageSection
	Groups   []uiPageGroup
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

func sectionLinks(section uiLinksSection) uiPageSection {
	return uiPageSection{Kind: "links", Links: section}
}

func boolField(name, label string, value bool) uiFormField {
	return uiFormField{Name: name, Label: label, Kind: "select", Options: []uiFormOption{
		{Value: "true", Label: "Sì", Selected: value},
		{Value: "false", Label: "No", Selected: !value},
	}}
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
				Title: "Cerca una serie (TMDB)",
				Hint:  "Cerca il titolo e aggiungi direttamente il risultato trovato.",
				Path:  "/api/tmdb/search", Submit: "Cerca su TMDB", Render: "tmdb",
				Fields: []uiFormField{
					{Name: "kind", Kind: "select", Label: "Tipo", Options: []uiFormOption{{Value: "series", Label: "Serie TV", Selected: true}}},
					{Name: "query", Label: "Titolo", Placeholder: "Nome serie"},
				},
			}),
			sectionForm(uiFormSection{
				Title: "Aggiungi serie manualmente",
				Hint:  "Usa questa scheda se hai già un ID TMDB/TVDB o vuoi compilare i valori a mano.",
				Path:  "/api/tmdb/add", Submit: "Aggiungi serie",
				Fields: []uiFormField{
					{Name: "kind", Kind: "select", Label: "Tipo", Options: []uiFormOption{{Value: "series", Label: "Serie TV", Selected: true}, {Value: "movie", Label: "Film"}}},
					{Name: "name", Label: "Nome", Placeholder: "Nome serie"},
					{Name: "tmdb_id", Label: "TMDB ID", Placeholder: "es. 1399"},
					{Name: "year", Label: "Anno"},
					{Name: "quality", Label: "Qualità", Placeholder: "es. 1080p"},
					{Name: "language", Label: "Lingua", Placeholder: "es. ita"},
					{Name: "seasons", Label: "Stagioni", Placeholder: "es. 1-5 o *"},
					{Name: "exclude", Label: "Escludi", Placeholder: "parole da escludere"},
				},
			}),
			sectionTable(spec),
		}}, true
	case "movies":
		spec, _ := uiTableSpecFor("movies")
		return uiPanelsPage{Sections: []uiPageSection{
			sectionForm(uiFormSection{
				Title: "Cerca un film (TMDB)",
				Hint:  "Cerca il titolo e aggiungi direttamente il risultato trovato.",
				Path:  "/api/tmdb/search", Submit: "Cerca su TMDB", Render: "tmdb",
				Fields: []uiFormField{
					{Name: "kind", Kind: "select", Label: "Tipo", Options: []uiFormOption{{Value: "movie", Label: "Film", Selected: true}}},
					{Name: "query", Label: "Titolo", Placeholder: "Titolo film"},
				},
			}),
			sectionForm(uiFormSection{
				Title: "Aggiungi film manualmente",
				Path:  "/api/tmdb/add", Submit: "Aggiungi film",
				Fields: []uiFormField{
					{Name: "kind", Kind: "select", Label: "Tipo", Options: []uiFormOption{{Value: "movie", Label: "Film", Selected: true}, {Value: "series", Label: "Serie TV"}}},
					{Name: "name", Label: "Titolo"},
					{Name: "tmdb_id", Label: "TMDB ID"},
					{Name: "year", Label: "Anno"},
					{Name: "quality", Label: "Qualità"},
					{Name: "language", Label: "Lingua"},
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
		return uiPanelsPage{Sections: []uiPageSection{
			sectionTable(spec),
			sectionActions(uiActionSection{Label: "Ciclo fumetti", Buttons: []uiActionButton{
				{Label: "Avvia ciclo fumetti", Class: "primary", Method: "POST", Path: "/api/comics/cycle", Body: "{}"},
			}}),
			sectionForm(uiFormSection{
				Title: "Esplora GetComics", Path: "/api/comics/explore", Submit: "Cerca",
				Fields: []uiFormField{{Name: "query", Label: "Titolo fumetto", Placeholder: "es. Poison Ivy #41"}},
			}),
			sectionTable(uiTableSpec{
				Title:    "Download in corso",
				Endpoint: "/api/comics/downloads",
				ItemsKey: "",
				ColumnsJSON: uiJSON([]uiColumn{
					{Key: "title", Label: "Titolo"}, {Key: "method", Label: "Metodo"}, {Key: "status", Label: "Stato"},
					{Key: "progress", Label: "Avanzamento", Format: "percent"},
					{Key: "downloaded_bytes", Label: "Scaricato", Format: "bytes"},
					{Key: "speed_bytes", Label: "Velocità", Format: "rate"}, {Key: "tag", Label: "Tag"},
				}),
				ActionsJSON: uiJSON([]uiAction{
					{Label: "Pausa", Method: "POST", Path: "/api/comics/downloads/{id}/pause", Body: "{}"},
					{Label: "Riprendi", Method: "POST", Path: "/api/comics/downloads/{id}/resume", Body: "{}"},
					{Label: "Rimuovi", Class: "danger", Method: "POST", Path: "/api/comics/downloads/{id}/remove", Body: "{}", Confirm: "Rimuovere questo download?"},
				}),
				Empty: "Nessun download in corso.",
			}),
			sectionTable(uiTableSpec{
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
				Empty: "Storico vuoto.",
			}),
			sectionForm(uiFormSection{
				Title: "Pianificazione settimanale", Path: "/api/comics/weekly/settings", Submit: "Salva weekly",
				Fields: []uiFormField{
					boolField("enabled", "Weekly attivo", false),
					{Name: "from_date", Label: "Dal (YYYY.MM.DD)"},
				},
			}),
			sectionForm(uiFormSection{
				Title: "Link del weekly", Path: "/api/comics/weekly/links", Submit: "Trova link",
				Fields: []uiFormField{{Name: "date", Label: "Data pacchetto (YYYY.MM.DD)"}},
			}),
			sectionComicsLinks(),
		}}, true
	case "maintenance":
		return uiPanelsPage{Sections: uiMaintenanceSections(s, cfg)}, true
	case "integrations":
		return uiPanelsPage{Sections: uiIntegrationSections(s, cfg)}, true
	}
	return uiPanelsPage{}, false
}

func uiMaintenanceSections(s *AppState, cfg *Config) []uiPageSection {
	group := func(name string, section uiPageSection) uiPageSection {
		section.Group = name
		return section
	}
	return []uiPageSection{
		group("Database", sectionActions(uiActionSection{Label: "Database", Hint: "Operazioni sui database applicativi.", Buttons: []uiActionButton{
			{Label: "Ricalcola punteggi", Class: "primary", Method: "POST", Path: "/api/database/rescore", Body: "{}"},
			{Label: "Pulizia duplicati", Method: "POST", Path: "/api/maintenance/clean-duplicates", Body: "{}"},
			{Label: "Pota database", Method: "POST", Path: "/api/db/prune", Body: "{}"},
			{Label: "VACUUM", Method: "POST", Path: "/api/db/action", Body: `{"action":"vacuum"}`},
			{Label: "ANALYZE", Method: "POST", Path: "/api/db/action", Body: `{"action":"analyze"}`},
		}})),
		group("Database", sectionTable(uiTableSpec{
			Title:    "Database",
			Endpoint: "/api/db/info",
			ItemsKey: "files",
			ColumnsJSON: uiJSON([]uiColumn{
				{Key: "name", Label: "File"}, {Key: "size_bytes", Label: "Dimensione", Format: "bytes"},
				{Key: "exists", Label: "Presente", Format: "bool"},
			}),
			Empty: "Nessun database.",
		})),
		group("Archivio e rinomina", sectionActions(uiActionSection{Label: "Archivio e rinomina", Buttons: []uiActionButton{
			{Label: "Scansiona archivi", Method: "POST", Path: "/api/scan-all-archives", Body: "{}"},
			{Label: "Rinomina tutto", Method: "POST", Path: "/api/rename-all", Body: "{}"},
			{Label: "Pulisci trash", Method: "POST", Path: "/api/maintenance/clean-trash", Body: "{}"},
			{Label: "Housekeeping", Method: "POST", Path: "/api/maintenance/housekeeping", Body: "{}"},
			{Label: "Backfill MediaInfo", Method: "POST", Path: "/api/maintenance/backfill-media-info", Body: "{}"},
		}})),
		group("Archivio e rinomina", sectionProgress("Progresso rinomina", "/api/rename-progress")),
		group("Servizio e installazione", sectionActions(uiActionSection{Label: "Servizio e installazione", Hint: "Operazioni sensibili eseguite dal daemon.", Buttons: []uiActionButton{
			{Label: "Controlla porte", Method: "GET", Path: "/api/config/check-ports", Body: ""},
			{Label: "Importa setup", Method: "POST", Path: "/api/setup/import", Body: "{}", Confirm: "Importare la configurazione di setup?"},
			{Label: "Riavvia servizio", Class: "danger", Method: "POST", Path: "/api/service/restart", Body: "{}", Confirm: "Riavviare il servizio gextto?"},
		}})),
		group("Servizio e installazione", sectionTable(uiTableSpec{
			Title:    "Porte",
			Endpoint: "/api/config/check-ports",
			ItemsKey: "ports",
			ColumnsJSON: uiJSON([]uiColumn{
				{Key: "port", Label: "Porta"}, {Key: "available", Label: "Libera", Format: "bool"},
				{Key: "tcp_available", Label: "TCP", Format: "bool"}, {Key: "udp_available", Label: "UDP", Format: "bool"},
			}),
			Empty: "Nessuna porta da verificare.",
		})),
		group("RAM disk", sectionTable(uiTableSpec{
			Title:    "RAM disk",
			Endpoint: "/api/ramdisk",
			ItemsKey: "paths",
			ColumnsJSON: uiJSON([]uiColumn{
				{Key: "path", Label: "Percorso"}, {Key: "filesystem", Label: "Filesystem"},
				{Key: "exists", Label: "Presente", Format: "bool"},
				{Key: "free_bytes", Label: "Liberi", Format: "bytes"},
				{Key: "total_bytes", Label: "Totali", Format: "bytes"},
			}),
			Empty: "Nessun RAM disk configurato.",
		})),
		group("RAM disk", sectionForm(uiFormSection{
			Title: "Seleziona RAM disk",
			Hint:  "Inserisci un percorso tmpfs/ramfs già esistente e scrivibile.",
			Path:  "/api/ramdisk/select", Submit: "Seleziona",
			Fields: []uiFormField{{Name: "path", Label: "Percorso", Placeholder: "/dev/shm/gextto"}},
		})),
		group("RAM disk", sectionActions(uiActionSection{Label: "RAM disk automatico", Hint: "Crea /dev/shm/gextto e lo configura come destinazione temporanea.", Buttons: []uiActionButton{
			{Label: "Crea RAM disk", Class: "primary", Method: "POST", Path: "/api/ramdisk/create", Body: `{"path":"/dev/shm/gextto"}`},
		}})),
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
		sectionActions(uiActionSection{Label: "Cestino", Hint: "Il cestino conserva i file sostituiti o scartati.", Buttons: []uiActionButton{
			{Label: "Pulisci cestino", Method: "POST", Path: "/api/maintenance/clean-trash", Body: "{}"},
		}}),
		sectionTable(uiTableSpec{
			Title:    "Cestino",
			Endpoint: "/api/trash",
			ItemsKey: "items",
			ColumnsJSON: uiJSON([]uiColumn{
				{Key: "name", Label: "Nome"}, {Key: "size_bytes", Label: "Dimensione", Format: "bytes"},
				{Key: "is_dir", Label: "Cartella", Format: "bool"},
			}),
			ActionsJSON: uiJSON([]uiAction{
				{Label: "Elimina", Class: "danger", Method: "POST", Path: "/api/trash/delete", Body: `{"names":["{name}"]}`, Confirm: "Eliminare definitivamente questo file?"},
			}),
			Empty: "Cestino vuoto.",
		}),
		sectionTable(uiTableSpec{
			Title:    "Stato sorgenti",
			Endpoint: "/api/sources/health",
			ItemsKey: "items",
			ColumnsJSON: uiJSON([]uiColumn{
				{Key: "kind", Label: "Tipo"}, {Key: "name", Label: "Nome"},
				{Key: "ok", Label: "Esito", Format: "bool"}, {Key: "detail", Label: "Dettaglio"},
			}),
			Empty: "Nessuna sorgente da verificare.",
		}),
	}
}

func uiIntegrationSections(s *AppState, cfg *Config) []uiPageSection {
	traktCalendarDays := settingsOr(cfg, "trakt_calendar_days", "7")
	simklCalendarDays := settingsOr(cfg, "simkl_calendar_days", "7")
	sections := []uiPageSection{
		sectionOAuth(uiOAuthSection{Name: "Trakt", StartPath: "/api/trakt/auth/start", PollPath: "/api/trakt/auth/poll", Buttons: []uiActionButton{
			{Label: "Refresh token", Method: "POST", Path: "/api/trakt/auth/refresh", Body: "{}"},
			{Label: "Revoca", Class: "danger", Method: "POST", Path: "/api/trakt/auth/revoke", Body: "{}"},
			{Label: "Importa watchlist", Method: "POST", Path: "/api/trakt/watchlist/import", Body: "{}"},
		}}),
		sectionForm(uiFormSection{
			Title: "Trakt — impostazioni", Path: "/api/trakt/settings", Submit: "Salva Trakt",
			Wrap: "values",
			Fields: []uiFormField{
				{Name: "trakt_client_id", Label: "Client ID", Value: settingsOr(cfg, "trakt_client_id", "")},
				{Name: "trakt_client_secret", Label: "Client secret", Kind: "text"},
				{Name: "trakt_calendar_days", Label: "Giorni calendario", Value: traktCalendarDays},
				boolField("trakt_watchlist_sync", "Sincronizza watchlist", settingsBool(cfg, "trakt_watchlist_sync", false)),
				boolField("trakt_scrobble_enabled", "Scrobble", settingsBool(cfg, "trakt_scrobble_enabled", false)),
			},
		}),
		sectionOAuth(uiOAuthSection{Name: "Simkl", StartPath: "/api/simkl/auth/start", PollPath: "/api/simkl/auth/poll", Buttons: []uiActionButton{
			{Label: "Revoca", Class: "danger", Method: "POST", Path: "/api/simkl/auth/revoke", Body: "{}"},
			{Label: "Importa watchlist", Method: "POST", Path: "/api/simkl/watchlist/import", Body: "{}"},
		}}),
		sectionForm(uiFormSection{
			Title: "Simkl — impostazioni", Path: "/api/simkl/settings", Submit: "Salva Simkl",
			Wrap: "values",
			Fields: []uiFormField{
				{Name: "simkl_client_id", Label: "Client ID", Value: settingsOr(cfg, "simkl_client_id", "")},
				{Name: "simkl_calendar_days", Label: "Giorni calendario", Value: simklCalendarDays},
				{Name: "simkl_watchlist_status", Label: "Stato watchlist", Kind: "select", Options: []uiFormOption{
					{Value: "plantowatch", Label: "Da guardare", Selected: settingsOr(cfg, "simkl_watchlist_status", "plantowatch") == "plantowatch"},
					{Value: "watching", Label: "In visione", Selected: settingsOr(cfg, "simkl_watchlist_status", "") == "watching"},
					{Value: "completed", Label: "Completato", Selected: settingsOr(cfg, "simkl_watchlist_status", "") == "completed"},
				}},
				boolField("simkl_mark_watched", "Segna come visto", settingsBool(cfg, "simkl_mark_watched", false)),
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
		sectionActions(uiActionSection{Label: "Browser e media server", Hint: "Installa gli handler magnet/.torrent nel browser o aggiorna le librerie.", Buttons: []uiActionButton{
			{Label: "Test Jellyfin", Method: "POST", Path: "/api/jellyfin/test", Body: "{}"},
			{Label: "Aggiorna Jellyfin", Method: "POST", Path: "/api/jellyfin/refresh", Body: "{}"},
			{Label: "Test Plex", Method: "POST", Path: "/api/plex/test", Body: "{}"},
			{Label: "Aggiorna Plex", Method: "POST", Path: "/api/plex/refresh", Body: "{}"},
			{Label: "Test FlareSolverr", Method: "POST", Path: "/api/flaresolverr/test", Body: "{}"},
			{Label: "Notifica di test", Method: "POST", Path: "/api/test-notification", Body: "{}"},
		}}),
	}
	// Group each integration's panels together so the page renders them side by
	// side (Trakt / Simkl), like the classic layout.
	for index := range sections {
		switch index {
		case 0, 1, 4, 5:
			sections[index].Group = "Trakt"
		case 2, 3, 6, 7:
			sections[index].Group = "Simkl"
		}
	}
	return sections
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
			Endpoint: "/api/torrents/history",
			ItemsKey: "items",
			ColumnsJSON: uiJSON([]uiColumn{
				{Key: "name", Label: "Nome"}, {Key: "kind", Label: "Tipo"},
				{Key: "tag", Label: "Tag NAS"}, {Key: "quality_score", Label: "Punteggio", Format: "number"},
				{Key: "status", Label: "Stato"},
				{Key: "processed_path", Label: "Cartella libreria / NAS"}, {Key: "completed_at", Label: "Concluso"},
			}),
			Empty: "Nessun download nello storico.", Search: true, SearchParam: "q",
		}),
		sectionActions(uiActionSection{Label: "Motore torrent", Hint: "Applica o ottimizza le impostazioni libtorrent.", Buttons: []uiActionButton{
			{Label: "Applica impostazioni", Method: "POST", Path: "/api/torrents/apply_settings", Body: "{}"},
			{Label: "Ottimizza", Method: "POST", Path: "/api/torrents/optimize_settings", Body: "{}"},
			{Label: "Aggiorna IP filter", Method: "POST", Path: "/api/torrents/ipfilter_update", Body: "{}"},
		}}),
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
