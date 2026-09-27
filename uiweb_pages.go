package gextto

import "encoding/json"

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
	Search      bool
	SearchParam string
	Query       string
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
				{Key: "name", Label: "Nome"},
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
			Empty:    "Nessun film monitorizzato.",
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
	default:
		return uiTableSpec{}, false
	}
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
	case "integrations":
		return uiActionsPage{Title: "Integrazioni", Sections: []uiActionSection{
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
			{Label: "Trakt / Simkl", Hint: "I flussi di accesso (OAuth/PIN) restano disponibili nella UI classica.", Buttons: []uiActionButton{
				{Label: "Avvia Trakt", Method: "POST", Path: "/api/trakt/auth/start", Body: "{}"},
				{Label: "Avvia Simkl", Method: "POST", Path: "/api/simkl/auth/start", Body: "{}"},
			}},
		}}, true
	default:
		return uiActionsPage{}, false
	}
}
