package gextto

// uiweb_v2.go implements the "v2" user interface: a server-rendered (SSR)
// interface driven by HTMX instead of the removed legacy browser bundle.
//
// It is the official server-rendered interface:
//
//   - the shell lives at / and every fragment/action route is registered at
//     the root by registerV2Routes(s, mux); the legacy /v2 prefix permanent-
//     redirects to the same path without it;
//   - templates and static files are embedded from uiweb/v2;
//   - it reuses the existing Go view-models (uiDashboardDataFrom,
//     uiTorrentsDataFrom, uiSettingsPageFrom, ...) and the shared CSS, so the
//     data and the look come from the same source as the classic UI;
//   - the JSON APIs are untouched (the TUI and external clients keep working).
//
// The v2 handlers are the official UI at "/"; the /v2 prefix is kept only as a
// permanent redirect for compatibility.
//
// Unknown views answer with v2_unavailable instead of pretending the feature
// exists.

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/messages"
	"github.com/buzzqw/gextto/internal/models"
)

//go:embed uiweb/v2/templates/*.html
var v2TemplatesFS embed.FS

//go:embed uiweb/v2/static/*
var v2StaticFS embed.FS

var v2Templates = template.Must(template.New("v2").Funcs(template.FuncMap{
	"bytes":       logging.HumanBytesI64,
	"bytesU":      func(value uint64) string { return logging.HumanBytesI64(saturatingInt64(value)) },
	"rate":        func(value uint64) string { return logging.HumanRate(saturatingInt64(value)) },
	"arrow":       v2SortArrow,
	"sourceLabel": uiSourceLabel,
	"ternary": func(cond bool, whenTrue, whenFalse string) string {
		if cond {
			return whenTrue
		}
		return whenFalse
	},
	"addOne": func(value int) int { return value + 1 },
	"kib": func(value int64) int64 {
		if value < 0 {
			return value
		}
		return value / 1024
	},
	"dict": v2Dict,
}).ParseFS(v2TemplatesFS, "uiweb/v2/templates/*.html"))

// v2Dict builds a map from key/value pairs, used to pass several arguments to a
// shared sub-template (the classic Go templates have no struct literal).
func v2Dict(values ...any) (map[string]any, error) {
	if len(values)%2 != 0 {
		return nil, fmt.Errorf("dict needs an even number of arguments")
	}
	out := make(map[string]any, len(values)/2)
	for index := 0; index < len(values); index += 2 {
		key, ok := values[index].(string)
		if !ok {
			return nil, fmt.Errorf("dict keys must be strings")
		}
		out[key] = values[index+1]
	}
	return out, nil
}

func v2StaticFSRoot() fs.FS {
	sub, err := fs.Sub(v2StaticFS, "uiweb/v2/static")
	if err != nil {
		return nil
	}
	return sub
}

// v2Handle registers a v2 route without adding it to the public API route
// table: like /ui, the v2 interface is internal and must not leak into
// docs/API.md or the API route-parity test. It installs the same middleware as
// the shared handle() helper.
func v2Handle(s *AppState, mux *http.ServeMux, pattern string, fn HandlerFunc) {
	core := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		}
		guardHandler(pattern, fn)(w, r, s)
	})
	mux.Handle(pattern, UiNoCache(core))
}

// registerV2Routes installs the isolated /v2 interface.
func registerV2Routes(s *AppState, mux *http.ServeMux) {
	// The v2 handlers call the existing JSON APIs through this router so the
	// list pages reuse the same data access as the classic UI.
	v2Routers.Store(s, mux)
	mux.Handle("GET /static/", UiNoCache(http.StripPrefix("/static/", http.FileServer(http.FS(v2StaticFSRoot())))))
	// Legacy /v2 prefix: permanent redirect that strips it (query preserved), so
	// old bookmarks and already-loaded pages keep working.
	v2Handle(s, mux, "/v2", v2LegacyRedirect)
	v2Handle(s, mux, "/v2/", v2LegacyRedirect)
	v2Handle(s, mux, "GET /empty", V2Empty)
	v2Handle(s, mux, "GET /manifest.webmanifest", V2Manifest)
	v2Handle(s, mux, "GET /sw.js", V2ServiceWorker)
	v2Handle(s, mux, "GET /share", V2Share)
	v2Handle(s, mux, "POST /language", V2SetLanguage)
	v2Handle(s, mux, "POST /run-cycle", V2RunCycle)
	v2Handle(s, mux, "POST /dashboard/backup", V2DashboardBackup)
	v2Handle(s, mux, "POST /dashboard/search", V2DashboardSearch)
	v2Handle(s, mux, "GET /dashboard/feed", V2DashboardFeed)
	v2Handle(s, mux, "GET /dashboard/problems", V2DashboardProblems)
	v2Handle(s, mux, "POST /dashboard/feed/add", V2DashboardFeedAdd)

	// Scarico.
	v2Handle(s, mux, "POST /downloads/table", V2DownloadsTable)
	v2Handle(s, mux, "POST /downloads/settings", V2DownloadsSettings)
	v2Handle(s, mux, "GET /downloads/http-detail", V2HTTPDownloadDetail)
	v2Handle(s, mux, "GET /downloads/detail", V2DownloadsDetail)
	v2Handle(s, mux, "GET /downloads/detail/panel", V2DownloadsDetailPanel)
	v2Handle(s, mux, "POST /downloads/detail/action", V2DownloadsDetailAction)
	v2Handle(s, mux, "GET /downloads/remove", V2DownloadsRemoveModal)
	v2Handle(s, mux, "POST /downloads/remove", V2DownloadsRemove)

	// Configurazione.
	v2Handle(s, mux, "GET /settings/body", V2SettingsBody)
	v2Handle(s, mux, "GET /settings/search", V2SettingsSearch)
	v2Handle(s, mux, "POST /settings/save", V2SettingsSave)
	v2Handle(s, mux, "POST /settings/score-groups", V2SettingsScoreGroup)
	v2Handle(s, mux, "POST /settings/feed", V2SettingsFeed)
	v2Handle(s, mux, "POST /settings/checkbox", V2SettingsCheckbox)
	v2Handle(s, mux, "GET /settings/editor-row", V2SettingsEditorRow)
	v2Handle(s, mux, "POST /settings/editor-save", V2SettingsEditorSave)
	v2Handle(s, mux, "POST /settings/editor-test", V2SettingsEditorTest)
	v2Handle(s, mux, "GET /settings/source-test", V2SettingsSourceTest)
	v2Handle(s, mux, "POST /settings/rename-token", V2SettingsRenameToken)
	v2Handle(s, mux, "POST /settings/rename-preview", V2SettingsRenamePreview)
	v2Handle(s, mux, "POST /settings/rename-save", V2SettingsRenameSave)
	v2Handle(s, mux, "GET /settings/ipfilter", V2SettingsIPFilterStatus)
	v2Handle(s, mux, "POST /settings/ipfilter", V2SettingsIPFilterApply)
	v2Handle(s, mux, "GET /settings/i18n", V2SettingsI18nTable)
	v2Handle(s, mux, "POST /settings/i18n/import", V2SettingsI18nImport)
	v2Handle(s, mux, "POST /settings/i18n/delete", V2SettingsI18nDelete)
	v2Handle(s, mux, "GET /settings/content-archive", V2SettingsContentArchiveSearch)
	v2Handle(s, mux, "POST /settings/content-archive/delete", V2SettingsContentArchiveDelete)

	// Log (frammento aggiornabile).
	v2Handle(s, mux, "GET /partial/logs", V2LogsPartial)
	v2Handle(s, mux, "GET /maintenance/gx-torrent/log", V2GxLogsPartial)
	v2Handle(s, mux, "GET /partial/chrome", V2ChromePartial)
	v2Handle(s, mux, "GET /partial/chrome/sse", V2ChromeSSE)
	// Salute: singoli riquadri con cadenze di aggiornamento diverse.
	v2Handle(s, mux, "GET /partial/health/tile", V2HealthTilePartial)

	// Tabelle generiche (Serie TV, Film, Mancanti, Archivio, Blocklist, Fumetti).
	v2Handle(s, mux, "GET /table", V2Table)
	v2Handle(s, mux, "POST /table/action", V2TableAction)
	v2Handle(s, mux, "POST /table/library", V2TableLibrary)
	v2Handle(s, mux, "POST /table/gap-search", V2TableGapSearch)

	// Pannelli (Manutenzione, Integrazioni).
	v2Handle(s, mux, "POST /section/action", V2SectionAction)
	v2Handle(s, mux, "POST /section/form", V2SectionForm)
	v2Handle(s, mux, "POST /section/test-ftp", V2SectionTestFTP)

	// Esplora (ricerca release).
	v2Handle(s, mux, "POST /search", V2Search)
	v2Handle(s, mux, "POST /search/add", V2SearchAdd)
	v2Handle(s, mux, "POST /search/explain", V2SearchExplain)

	// Esplora TMDB (calendario, tendenze, ricerca).
	v2Handle(s, mux, "GET /tmdb/calendar", V2TmdbCalendar)
	v2Handle(s, mux, "POST /tmdb/discover", V2TmdbDiscover)
	v2Handle(s, mux, "POST /tmdb/search", V2TmdbSearch)
	v2Handle(s, mux, "POST /tmdb/add", V2TmdbAdd)
	v2Handle(s, mux, "GET /tmdb/manual", V2TmdbManual)

	// Fumetti: link finder, download diretto e modifica della libreria.
	v2Handle(s, mux, "POST /comics/links", V2ComicsLinks)
	v2Handle(s, mux, "POST /comics/download", V2ComicsDownload)
	v2Handle(s, mux, "POST /comics/weekly/force", V2ComicsWeeklyForce)
	v2Handle(s, mux, "POST /comics/explore/download", V2ComicsExploreDownload)
	v2Handle(s, mux, "GET /comics/explore/select", V2ComicsExploreSelect)
	v2Handle(s, mux, "POST /comics/explore/add", V2ComicsExploreAdd)
	v2Handle(s, mux, "GET /comics/edit", V2ComicsEdit)
	v2Handle(s, mux, "POST /comics/save", V2ComicsSave)

	// Dettagli Serie/Film.
	v2Handle(s, mux, "POST /series/save", V2SeriesSave)
	v2Handle(s, mux, "GET /series/sources", V2SeriesSources)
	v2Handle(s, mux, "GET /series/history", V2SeriesHistory)
	v2Handle(s, mux, "POST /series/episode-search", V2SeriesEpisodeSearch)
	v2Handle(s, mux, "GET /series/episode-search-online", V2SeriesEpisodeSearchOnline)
	v2Handle(s, mux, "GET /series/rename-preview", V2SeriesRenamePreview)
	v2Handle(s, mux, "POST /series/rename-execute", V2SeriesRenameExecute)

	// Manutenzione: cestino e verifica sorgenti.
	v2Handle(s, mux, "GET /partial/trash", V2TrashList)
	v2Handle(s, mux, "POST /trash/delete", V2TrashDelete)
	v2Handle(s, mux, "GET /partial/sources-check", V2SourcesCheck)

	// Manutenzione: widget dedicati (duplicati, database, RAM disk, rinomina).
	v2Handle(s, mux, "POST /maintenance/duplicates", V2Duplicates)
	v2Handle(s, mux, "POST /maintenance/db", V2DBMaintenance)
	v2Handle(s, mux, "POST /maintenance/ramdisk", V2Ramdisk)
	v2Handle(s, mux, "GET /maintenance/update", V2Update)
	v2Handle(s, mux, "POST /setup/auth", V2SetupAuth)
	v2Handle(s, mux, "POST /setup/paths", V2SetupPaths)
	v2Handle(s, mux, "POST /setup/sources", V2SetupSources)
	v2Handle(s, mux, "POST /setup/finish", V2SetupFinish)
	v2Handle(s, mux, "POST /maintenance/update", V2Update)
	v2Handle(s, mux, "POST /maintenance/folder-rename/scan", V2FolderRenameScan)
	v2Handle(s, mux, "POST /maintenance/folder-rename/apply", V2FolderRenameApply)
	v2Handle(s, mux, "GET /partial/rename-progress", V2RenameProgress)

	// OAuth / PIN (Simkl).
	v2Handle(s, mux, "POST /oauth/start", V2OAuthStart)
	v2Handle(s, mux, "POST /oauth/poll", V2OAuthPoll)

	// Job in background (progresso e annullamento).
	v2Handle(s, mux, "GET /partial/jobs", V2JobsPartial)
	v2Handle(s, mux, "POST /jobs/cancel", V2JobCancel)

	// Aggiunta torrent (magnet/URL o file .torrent).
	v2Handle(s, mux, "POST /downloads/add", V2DownloadsAdd)

	// Traduzioni: modifica per chiave.
	v2Handle(s, mux, "POST /settings/i18n/set", V2SettingsI18nSet)
}

// v2LegacyRedirect keeps the old /v2 prefix working: it strips it and redirects
// to the equivalent root path, preserving the query string. GET/HEAD get a
// permanent redirect; other methods (e.g. a stale open page posting a form) use
// a temporary redirect that preserves the method and body.
func v2LegacyRedirect(w http.ResponseWriter, r *http.Request, s *AppState) {
	target := strings.TrimPrefix(r.URL.Path, "/v2")
	if target == "" {
		target = "/"
	}
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	status := http.StatusMovedPermanently
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		status = http.StatusTemporaryRedirect
	}
	http.Redirect(w, r, target, status)
}

// ---------------------------------------------------------------------------
// Rendering + i18n
// ---------------------------------------------------------------------------

type v2ShellData struct {
	Title   string
	Page    string
	Groups  []uiNavGroup
	Content any
	Chrome  uiShellChrome
	Body    string
	// Flash carries a one-shot feedback message (usually set by V2SectionAction
	// through the redirect URL) rendered as a toast on every page.
	Flash    string
	FlashErr bool
	// Update drives the "update available" badge next to the donate button.
	Update UpdateStatus
	// ClientI18n is the JSON dictionary window.__v2i18n for the strings the
	// client script renders at runtime (see uiweb_v2_client_i18n.go).
	ClientI18n template.JS
}

func v2Render(w http.ResponseWriter, status int, name string, data any, dict, eng map[string]string) {
	var buffer bytes.Buffer
	if err := v2Templates.ExecuteTemplate(&buffer, name, data); err != nil {
		logging.Error("v2 template render failed", "template", name, "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	body := v2TranslateHTML(buffer.String(), dict, eng)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// v2PageLabel returns the navigation label for a view, falling back to the view
// name itself for unknown views.
func v2PageLabel(view string) string {
	for _, group := range uiNavGroups {
		for _, item := range group.Items {
			if item.ID == view {
				return item.Label
			}
		}
	}
	if view == "setup" {
		return "Configurazione iniziale"
	}
	return view
}

// v2NavGroups mirrors uiNavigation but points the links at /v2.
func v2NavGroups(view string, counts map[string]string) []uiNavGroup {
	groups := make([]uiNavGroup, 0, len(uiNavGroups))
	for index, group := range uiNavGroups {
		out := uiNavGroup{Label: group.Label}
		for _, item := range group.Items {
			out.Items = append(out.Items, uiNavItem{
				ID:           item.ID,
				Label:        item.Label,
				Href:         "/?view=" + item.ID,
				Active:       item.ID == view,
				Optional:     item.Optional,
				MobileHidden: item.MobileHidden,
				MobileAlways: item.MobileAlways,
				Count:        counts[item.ID],
			})
		}
		if index == len(uiNavGroups)-1 {
			out.Open = uiIsSystemPage(view)
			out.System = true
		}
		groups = append(groups, out)
	}
	return groups
}

// The dashboard, health and downloads pages embed their base view-model and add
// the server-rendered panel tables (upcoming releases, source health, providers,
// download history), which the classic UI loaded with JavaScript.
type v2DashboardView struct {
	uiDashboardData
	Calendar    []v2DashboardCalendarItem
	PanelTables []v2TableData
	Jobs        *v2JobsView
	Search      v2SearchView
}

type v2DashboardCalendarItem struct {
	Series  string
	Poster  string
	Season  int
	Episode int
	AirDate string
	// TmdbURL/TvdbURL link the title to its own page; either may be empty.
	TmdbURL string
	TvdbURL string
}

// v2DashboardCalendarFrom keeps the calendar presentation aligned with the
// classic UI and rextto: the existing API already returns the next episode,
// its air date and the TMDB poster URL.
func v2DashboardCalendarFrom(s *AppState) []v2DashboardCalendarItem {
	raw, status := v2InternalJSON(s, http.MethodGet, "/api/calendar", nil, nil)
	if status >= http.StatusBadRequest {
		return nil
	}
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	cfg := latestConfig(s)
	out := make([]v2DashboardCalendarItem, 0, len(payload.Items))
	for _, item := range payload.Items {
		episode, _ := item["episode"].(map[string]any)
		name := v2String(item["series"])
		tmdb, tvdb := "", ""
		if series := gh3FindSeries(cfg, name); series != nil {
			tmdb = tmdbURL(series.TmdbID, "tv")
			tvdb = tvdbURL(series.TvdbID, "series")
		}
		out = append(out, v2DashboardCalendarItem{
			Series:  name,
			Poster:  v2SafeHref(v2String(item["poster"])),
			Season:  int(v2Float(episode["season_number"])),
			Episode: int(v2Float(episode["episode_number"])),
			AirDate: v2String(episode["air_date"]),
			TmdbURL: tmdb,
			TvdbURL: tvdb,
		})
		if len(out) >= 6 {
			break
		}
	}
	return out
}

type v2HealthView struct {
	uiHealthData
	PanelTables []v2TableData
	Engine      uiEngineStats
}

type v2DownloadsView struct {
	v2TorrentsView
	History  []v2TableData
	Flash    string
	FlashErr bool
	// AddLink pre-fills the add form (a link shared from another app).
	AddLink string
}

func v2SectionTables(s *AppState, r *http.Request, sections []uiPageSection, views []string) []v2TableData {
	out := []v2TableData{}
	index := 0
	for _, section := range sections {
		if section.Kind != "table" {
			continue
		}
		view := ""
		if index < len(views) {
			view = views[index]
		}
		out = append(out, v2TableDataFrom(s, r, view, section.Table))
		index++
	}
	return out
}

func v2DashboardViewFrom(s *AppState, r *http.Request) v2DashboardView {
	base := uiDashboardDataFrom(s)
	jobs := v2JobsViewFrom(s)
	return v2DashboardView{
		uiDashboardData: base,
		Calendar:        v2DashboardCalendarFrom(s),
		Jobs:            &jobs,
		Search:          v2SearchView{Redirect: "/?view=dashboard"},
	}
}

func v2HealthViewFrom(s *AppState, r *http.Request) v2HealthView {
	base := uiHealthDataFrom(s)
	return v2HealthView{
		uiHealthData: base,
		PanelTables:  v2SectionTables(s, r, base.Panels, []string{"health-sources", "health-providers"}),
		Engine:       uiEngineStatsFrom(s),
	}
}

type v2ComicsView struct {
	Groups    []v2Group
	Downloads v2TableData
}

func v2ComicsDownloadSpec(spec uiTableSpec) uiTableSpec {
	return uiTableSpec{
		Title:       "Download fumetti",
		Endpoint:    "/api/comics/downloads",
		ItemsKey:    "",
		ColumnsJSON: spec.DownloadsColumnsJSON,
		ActionsJSON: spec.DownloadsActionsJSON,
		Empty:       "Nessun download HTTP in corso.",
		RefreshHint: "auto",
	}
}

func v2ComicsViewFrom(s *AppState, r *http.Request) v2ComicsView {
	page, _ := uiPanelsPageFor("comics", s)
	groups := v2SectionGroups(s, r, "comics", page.Sections)
	spec, _ := uiTableSpecFor("comics")
	return v2ComicsView{
		Groups:    groups,
		Downloads: v2TableDataFrom(s, r, "comics-downloads", v2ComicsDownloadSpec(spec)),
	}
}

type v2PanelsView struct {
	Groups []v2Group
	Jobs   *v2JobsView
	Stack  bool
}

// v2MovieLibraryView keeps the monitored library and its download history in
// one Film page, selected through the same in-page tabs used by rextto.
type v2MovieLibraryView struct {
	Tab       string
	Monitored v2PanelsView
	History   v2TableData
}

func v2MovieLibraryViewFrom(s *AppState, r *http.Request) v2MovieLibraryView {
	page := v2MovieLibraryView{Tab: r.FormValue("tab")}
	if page.Tab == "downloaded" {
		spec, _ := uiTableSpecFor("movie-history")
		page.History = v2TableDataFrom(s, r, "movie-history", spec)
		return page
	}
	page.Tab = "monitored"
	page.Monitored = v2PanelsViewFrom(s, r, "movies")
	return page
}

func v2PanelsViewFrom(s *AppState, r *http.Request, view string) v2PanelsView {
	page := v2PanelsView{}
	if panels, ok := uiPanelsPageFor(view, s); ok {
		page.Groups = v2SectionGroups(s, r, view, panels.Sections)
		page.Stack = panels.Stack
	}
	if view == "maintenance" {
		jobs := v2JobsViewFrom(s)
		page.Jobs = &jobs
	}
	return page
}

func v2DownloadsViewFrom(s *AppState, r *http.Request) v2DownloadsView {
	return v2DownloadsView{
		v2TorrentsView: v2TorrentsViewFrom(s, r, "", false),
		History:        v2SectionTables(s, r, uiDownloadsPageFor(s).Panels, []string{"downloads-history"}),
		Flash:          strings.TrimSpace(r.FormValue("msg")),
		FlashErr:       r.FormValue("msg_err") == "1",
		AddLink:        strings.TrimSpace(r.FormValue("add")),
	}
}

// v2Content maps a view to its body template and view-model.
func v2Content(s *AppState, r *http.Request, view string) (string, any) {
	// A selected series/movie is a detail page; only the bare menu views use
	// the section-composed list page below.
	if view == "series" || view == "movies" {
		if body, content, ok := v2ContentDetail(s, r, view); ok {
			return body, content
		}
	}
	switch view {
	case "dashboard":
		return "v2_dashboard", v2DashboardViewFrom(s, r)
	case "downloads":
		return "v2_downloads", v2DownloadsViewFrom(s, r)
	case "settings":
		return "v2_settings", v2SettingsViewFrom(s, r.FormValue("tab"), r.FormValue("highlight"))
	case "health":
		return "v2_health", v2HealthViewFrom(s, r)
	case "logs":
		linesNum, _ := strconv.Atoi(r.FormValue("lines"))
		return "v2_logs", v2LogsViewFrom(s, r.FormValue("filter"), linesNum, r.FormValue("log"), r.FormValue("level") == "problems")
	case "manual":
		return "v2_manual", uiManualDataFrom(s)
	case "license":
		return "v2_license", uiLicenseData{Text: uiLicenseText}
	case "movies":
		return "v2_movies", v2MovieLibraryViewFrom(s, r)
	case "series", "gaps", "archive", "blocklist", "maintenance", "integrations":
		return "v2_panels_page", v2PanelsViewFrom(s, r, view)
	case "search":
		return "v2_search", v2SearchView{}
	case "setup":
		return "v2_setup", v2SetupViewFrom(s, r)
	case "comics":
		return "v2_comics", v2ComicsViewFrom(s, r)
	}
	if spec, ok := uiTableSpecFor(view); ok {
		return "v2_table_page", v2TableDataFrom(s, r, view, spec)
	}
	return "v2_unavailable", map[string]any{"Title": v2PageLabel(view), "View": view}
}

// V2Page renders the requested v2 page inside the shell.
func V2Page(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := strings.TrimSpace(r.URL.Query().Get("view"))
	if view == "" {
		// A fresh installation starts from the setup wizard.
		if r.URL.RawQuery == "" && v2SetupNeeded(s) {
			http.Redirect(w, r, "/?view=setup", http.StatusSeeOther)
			return
		}
		view = "dashboard"
	}
	body, content := v2Content(s, r, view)
	cfg := latestConfig(s)
	page := v2ShellData{
		Title:    uiText(s, v2PageLabel(view)),
		Page:     view,
		Groups:   v2NavGroups(view, uiNavCounts(s, cfg)),
		Content:  content,
		Chrome:   uiShellChromeFrom(s),
		Body:     body,
		Flash:    strings.TrimSpace(r.FormValue("toast")),
		FlashErr: r.FormValue("toast_err") == "1",
		Update:   updates.Status(cfg),

		ClientI18n: v2ClientI18nJSON(s),
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_shell", page, dict, eng)
}

// V2ChromePartial refreshes the live performance metrics without reloading the
// page or replacing the controls in the top bar.
func V2ChromePartial(w http.ResponseWriter, r *http.Request, s *AppState) {
	templateName := "v2_live_top_metrics"
	if r.URL.Query().Get("status") == "1" {
		templateName = "v2_live_status"
	} else if r.URL.Query().Get("mobile") == "1" {
		templateName = "v2_live_mobile_metrics"
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, templateName, uiShellChromeFrom(s), dict, eng)
}

func V2ChromeSSE(w http.ResponseWriter, r *http.Request, s *AppState) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			chrome := uiShellChromeFrom(s)
			// The live chrome goes through the same translation as the page;
			// otherwise every update would put the Italian labels back.
			dict, eng := v2Dictionaries(s)

			var topBuf bytes.Buffer
			v2Templates.ExecuteTemplate(&topBuf, "v2_live_top_metrics", chrome)
			fmt.Fprintf(w, "event: chrome-top\ndata: %s\n\n", strings.ReplaceAll(v2TranslateHTML(topBuf.String(), dict, eng), "\n", ""))

			var mobBuf bytes.Buffer
			v2Templates.ExecuteTemplate(&mobBuf, "v2_live_mobile_metrics", chrome)
			fmt.Fprintf(w, "event: chrome-mobile\ndata: %s\n\n", strings.ReplaceAll(v2TranslateHTML(mobBuf.String(), dict, eng), "\n", ""))

			var statBuf bytes.Buffer
			v2Templates.ExecuteTemplate(&statBuf, "v2_live_status", chrome)
			fmt.Fprintf(w, "event: chrome-status\ndata: %s\n\n", strings.ReplaceAll(v2TranslateHTML(statBuf.String(), dict, eng), "\n", ""))

			flusher.Flush()
		}
	}
}

// V2HealthTilePartial refreshes a single Salute tile without reloading the page.
// The caller picks the tile with `name`; Stato, Memoria and Uptime poll every
// few seconds, while Disco polls hourly.
func V2HealthTilePartial(w http.ResponseWriter, r *http.Request, s *AppState) {
	var (
		templateName string
		data         any
	)
	switch r.URL.Query().Get("name") {
	case "engine":
		templateName = "v2_health_engine"
		data = uiEngineStatsFrom(s)
	case "status":
		templateName = "v2_health_tile_status"
		data = uiHealthStatusTileFrom(s)
	case "memory":
		templateName = "v2_health_tile_memory"
		data = uiHealthMemoryTileFrom(s)
	case "uptime":
		templateName = "v2_health_tile_uptime"
		data = uiHealthUptimeTileFrom(s)
	case "disk":
		templateName = "v2_health_tile_disk"
		data = uiHealthDiskTileFrom(s)
	default:
		http.NotFound(w, r)
		return
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, templateName, data, dict, eng)
}

// V2Empty clears a modal container.
func V2Empty(w http.ResponseWriter, r *http.Request, s *AppState) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
}

// V2SetLanguage persists the interface language and goes back to the referer.
func V2SetLanguage(w http.ResponseWriter, r *http.Request, s *AppState) {
	lang := strings.TrimSpace(r.FormValue("lang"))
	switch lang {
	case "it", "en", "de", "fr", "es", "pl":
		if err := s.i18n.SetLanguage(lang); err == nil {
			// Keep notifications and backend messages in the same language,
			// like the JSON endpoint does.
			messages.SetLanguage(lang)
		}
	}
	target := r.Header.Get("Referer")
	if target == "" {
		target = "/"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// v2DiscardWriter swallows a JSON response when a v2 action reuses an existing
// JSON handler internally.
type v2DiscardWriter struct {
	code int
	buf  bytes.Buffer
}

func (d *v2DiscardWriter) Header() http.Header         { return http.Header{} }
func (d *v2DiscardWriter) Write(b []byte) (int, error) { return d.buf.Write(b) }
func (d *v2DiscardWriter) WriteHeader(code int)        { d.code = code }

// V2RunCycle starts a manual cycle by reusing the existing RunNow handler and
// returning a modal confirmation to the dashboard action that requested it.
func V2RunCycle(w http.ResponseWriter, r *http.Request, s *AppState) {
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/?view=dashboard", http.StatusSeeOther)
		return
	}
	query := r.URL.Query()
	domain := strings.TrimSpace(r.FormValue("domain"))
	query.Set("domain", domain)
	r.URL.RawQuery = query.Encode()
	result := &v2DiscardWriter{}
	RunNow(result, r, s)
	labels := map[string]string{"series": "Serie TV", "movies": "Film", "comics": "Fumetti"}
	label := labels[domain]
	if label == "" {
		label = "completo"
	}
	notice := map[string]any{
		"Title":   "Monitoraggio " + label,
		"Message": "Monitoraggio " + label + " avviato.",
		"Error":   result.code >= http.StatusBadRequest,
	}
	var response struct {
		Queued bool   `json:"queued"`
		Error  string `json:"error"`
	}
	if json.Unmarshal(result.buf.Bytes(), &response) == nil {
		if response.Queued {
			notice["Message"] = "Monitoraggio " + label + " già in corso: richiesta accodata."
		} else if response.Error != "" {
			notice["Message"] = response.Error
		}
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_action_notice", notice, dict, eng)
}

// V2DashboardBackup creates a backup and reports its result in the same
// confirmation modal used by the dashboard's manual monitoring actions.
func V2DashboardBackup(w http.ResponseWriter, r *http.Request, s *AppState) {
	raw, status := v2InternalJSON(s, http.MethodPost, "/api/backup", nil, []byte(`{}`))
	notice := map[string]any{
		"Title":   "Backup",
		"Message": "Backup completato.",
		"Error":   status >= http.StatusBadRequest,
	}
	if status >= http.StatusBadRequest {
		notice["Message"] = v2JSONError(raw)
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_action_notice", notice, dict, eng)
}

// --- i18n: translate the rendered HTML exactly like the classic client did ---

func v2Language(s *AppState) string {
	if s != nil && s.i18n != nil {
		if lang, err := s.i18n.Language(); err == nil && lang != "" {
			return lang
		}
	}
	return "it"
}

func v2Dictionary(s *AppState, lang string) map[string]string {
	if s == nil || s.i18n == nil || lang == "" || lang == "it" {
		return nil
	}
	// Dictionary caches the map per language: the renderer must not rebuild it
	// from SQLite on every page and every HTMX partial.
	dict, _ := s.i18n.Dictionary(lang)
	return dict
}

// v2Dictionaries returns the active-language dictionary and the English
// fallback used when the active language lacks a key.
func v2Dictionaries(s *AppState) (map[string]string, map[string]string) {
	lang := v2Language(s)
	if lang == "it" || lang == "" {
		return nil, nil
	}
	dict := v2Dictionary(s, lang)
	if lang == "en" {
		return dict, nil
	}
	return dict, v2Dictionary(s, "en")
}

func v2IsSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

// v2TranslateText replaces a whole (trimmed) text value that matches a catalog
// key, preserving the surrounding whitespace, exactly like the classic client.
func v2TranslateText(value string, dict, eng map[string]string) string {
	if value == "" {
		return value
	}
	start := 0
	for start < len(value) && v2IsSpace(value[start]) {
		start++
	}
	end := len(value)
	for end > start && v2IsSpace(value[end-1]) {
		end--
	}
	if start >= end {
		return value
	}
	core := value[start:end]
	translated := dict[core]
	if translated == "" && len(eng) > 0 {
		translated = eng[core]
	}
	if translated == "" || translated == core {
		return value
	}
	return value[:start] + translated + value[end:]
}

func v2TranslatableAttr(name string) bool {
	// "label" names the <optgroup> areas of the settings section picker;
	// hx-confirm and the toast attributes are shown to the user as they are.
	switch name {
	case "title", "placeholder", "aria-label", "label", "hx-confirm", "data-v2-toast-title", "data-v2-toast-message":
		return true
	}
	return false
}

// v2TranslateHTML walks the rendered HTML and translates text nodes and the
// translatable attributes. It never touches <script>/<style> content, so the
// inline theme bootstrap stays byte-for-byte identical.
func v2TranslateHTML(raw string, dict, eng map[string]string) string {
	if len(dict) == 0 {
		return raw
	}
	var out bytes.Buffer
	tokenizer := xhtml.NewTokenizer(strings.NewReader(raw))
	rawTag := ""
	for {
		tokenType := tokenizer.Next()
		if tokenType == xhtml.ErrorToken {
			break
		}
		switch tokenType {
		case xhtml.TextToken:
			if rawTag != "" {
				out.Write(tokenizer.Raw())
				continue
			}
			out.WriteString(xhtml.EscapeString(v2TranslateText(string(tokenizer.Text()), dict, eng)))
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			name, hasAttr := tokenizer.TagName()
			tag := strings.ToLower(string(name))
			out.WriteByte('<')
			out.Write(name)
			titleTranslated := false
			for hasAttr {
				key, value, more := tokenizer.TagAttr()
				attrName := strings.ToLower(string(key))
				text := string(value)
				if v2TranslatableAttr(attrName) {
					translated := v2TranslateText(text, dict, eng)
					if attrName == "title" && translated != text {
						titleTranslated = true
					}
					text = translated
				}
				out.WriteByte(' ')
				out.Write(key)
				out.WriteString(`="`)
				out.WriteString(xhtml.EscapeString(text))
				out.WriteByte('"')
				hasAttr = more
			}
			if titleTranslated {
				out.WriteString(` data-v2-title-translated="true"`)
			}
			if tokenType == xhtml.SelfClosingTagToken {
				out.WriteString("/>")
			} else {
				out.WriteByte('>')
				if tag == "script" || tag == "style" {
					rawTag = tag
				}
			}
		case xhtml.EndTagToken:
			name, _ := tokenizer.TagName()
			out.WriteString("</")
			out.Write(name)
			out.WriteByte('>')
			if rawTag != "" && strings.EqualFold(string(name), rawTag) {
				rawTag = ""
			}
		case xhtml.CommentToken, xhtml.DoctypeToken:
			out.Write(tokenizer.Raw())
		}
	}
	return out.String()
}

// ---------------------------------------------------------------------------
// Scarico (downloads)
// ---------------------------------------------------------------------------

type v2Column struct{ Key, Label string }

var v2TorrentColumns = []v2Column{
	{Key: "name", Label: "Nome"},
	{Key: "state", Label: "Stato"},
	{Key: "progress", Label: "Progresso"},
	{Key: "dl", Label: "↓ Download"},
	{Key: "ul", Label: "↑ Upload"},
	{Key: "eta", Label: "ETA"},
	{Key: "peers", Label: "Peer / Seed"},
	{Key: "ratio", Label: "Ratio"},
}

type v2TorrentsView struct {
	Rows                []uiTorrentRow
	HTTPDownloads       []ComicDownload
	Count               int
	UpdatedAt           string
	Sort                string
	Dir                 string
	Filter              string
	Auto                bool
	Columns             []v2Column
	TagOptions          []string
	TagFilter           string
	TempDL              int64
	TempUL              int64
	TempMinutes         int64
	TempActive          bool
	SchedActive         bool
	TempShadowed        bool
	SchedDL             int64
	SchedUL             int64
	BaseDL              int64
	BaseUL              int64
	AutoRemoveCompleted bool
	Message             string
	Error               bool
}

func v2SortArrow(sortKey, dir, key string) string {
	if sortKey != key {
		return ""
	}
	if dir == "desc" {
		return " ▼"
	}
	return " ▲"
}

func v2FlipDir(dir string) string {
	if dir == "desc" {
		return "asc"
	}
	return "desc"
}

func v2ValidSort(key string) bool {
	for _, column := range v2TorrentColumns {
		if column.Key == key {
			return true
		}
	}
	return false
}

func v2SortRows(rows []uiTorrentRow, key, dir string) {
	less := func(i, j int) bool { return rows[i].Name < rows[j].Name }
	switch key {
	case "state":
		less = func(i, j int) bool {
			left, right := strings.ToLower(rows[i].State), strings.ToLower(rows[j].State)
			if left == right {
				return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
			}
			return left < right
		}
	case "progress":
		less = func(i, j int) bool { return rows[i].Progress < rows[j].Progress }
	case "dl":
		less = func(i, j int) bool { return rows[i].DownloadRate < rows[j].DownloadRate }
	case "ul":
		less = func(i, j int) bool { return rows[i].UploadRate < rows[j].UploadRate }
	case "eta":
		less = func(i, j int) bool { return rows[i].ETASeconds < rows[j].ETASeconds }
	case "peers":
		less = func(i, j int) bool { return rows[i].NumPeers < rows[j].NumPeers }
	case "ratio":
		less = func(i, j int) bool { return rows[i].Ratio < rows[j].Ratio }
	default:
		less = func(i, j int) bool {
			left, right := strings.ToLower(rows[i].Name), strings.ToLower(rows[j].Name)
			if left == right {
				return rows[i].Hash < rows[j].Hash
			}
			return left < right
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if dir == "desc" {
			return less(j, i)
		}
		return less(i, j)
	})
}

func v2FilterRows(rows []uiTorrentRow, filter string) []uiTorrentRow {
	terms := strings.Fields(strings.ToLower(filter))
	if len(terms) == 0 {
		return rows
	}
	out := make([]uiTorrentRow, 0, len(rows))
	for _, row := range rows {
		haystack := strings.ToLower(row.Name + " " + row.Tag + " " + row.Source + " " + row.Reason)
		keep := true
		for _, term := range terms {
			if strings.HasPrefix(term, "-") {
				if strings.Contains(haystack, strings.TrimPrefix(term, "-")) {
					keep = false
					break
				}
				continue
			}
			if !strings.Contains(haystack, term) {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, row)
		}
	}
	return out
}

func v2TorrentsViewFrom(s *AppState, r *http.Request, message string, isErr bool) v2TorrentsView {
	data := uiTorrentsDataFrom(s)

	sortKey := strings.TrimSpace(r.FormValue("sort"))
	dir := strings.TrimSpace(r.FormValue("dir"))
	if click := strings.TrimSpace(r.FormValue("sort_click")); click != "" {
		if click == sortKey {
			dir = v2FlipDir(dir)
		} else {
			dir = "asc"
		}
		sortKey = click
	}
	if !v2ValidSort(sortKey) {
		sortKey = "name"
	}
	if dir != "desc" {
		dir = "asc"
	}
	filter := r.FormValue("filter")
	rows := v2FilterRows(data.Rows, filter)
	tagFilter := strings.TrimSpace(r.FormValue("tag_filter"))
	if tagFilter != "" {
		filtered := rows[:0]
		for _, row := range rows {
			matched := tagFilter == "__none__" && len(row.Tags) == 0
			if tagFilter != "__none__" {
				for _, tag := range row.Tags {
					if strings.EqualFold(strings.TrimSpace(tag), tagFilter) {
						matched = true
						break
					}
				}
			}
			if matched {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	v2SortRows(rows, sortKey, dir)
	settings := uiDownloadsPageFor(s)
	auto := r.FormValue("auto")
	if auto == "" {
		auto = r.FormValue("auto_state")
	}
	// Keep the live download list updating on first entry. The explicit `0`
	// value from the Auto button is still respected and disables polling.
	autoEnabled := auto == "" || auto == "1"
	return v2TorrentsView{
		Rows:                rows,
		HTTPDownloads:       data.HTTPDownloads,
		Count:               data.Count,
		UpdatedAt:           data.UpdatedAt,
		Sort:                sortKey,
		Dir:                 dir,
		Filter:              filter,
		Auto:                autoEnabled,
		Columns:             v2TorrentColumns,
		TagOptions:          uiDownloadTagOptions(s, latestConfig(s)),
		TagFilter:           tagFilter,
		TempDL:              settings.TempDL,
		TempUL:              settings.TempUL,
		TempMinutes:         settings.TempMinutes,
		TempActive:          settings.TempActive,
		SchedActive:         settings.SchedActive,
		TempShadowed:        settings.TempShadowed,
		SchedDL:             settings.SchedDL,
		SchedUL:             settings.SchedUL,
		BaseDL:              settings.BaseDL,
		BaseUL:              settings.BaseUL,
		AutoRemoveCompleted: settings.AutoRemoveCompleted,
		Message:             message,
		Error:               isErr,
	}
}

// V2DownloadsTable is the single POST endpoint the torrent table form talks to.
func V2DownloadsTable(w http.ResponseWriter, r *http.Request, s *AppState) {
	var message string
	var isErr bool
	switch {
	case r.FormValue("row_action") != "":
		message, isErr = v2TorrentRowAction(s, r.FormValue("row_action"))
	case r.FormValue("http_action") != "":
		message, isErr = v2HTTPAction(r.FormValue("http_action"))
	case r.FormValue("bulk") != "":
		message, isErr = v2TorrentBulkAction(s, r, r.FormValue("bulk"))
	}
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/?view=downloads", http.StatusSeeOther)
		return
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_torrents_wrap", v2TorrentsViewFrom(s, r, message, isErr), dict, eng)
}

func v2TorrentRowAction(s *AppState, value string) (string, bool) {
	action, hash, ok := strings.Cut(value, ":")
	if !ok || hash == "" {
		return "azione non valida", true
	}
	engine := s.activeEngine()
	switch action {
	case "pause":
		if _, err := engine.Pause(hash); err != nil {
			return "pausa non riuscita: " + err.Error(), true
		}
		return "torrent in pausa", false
	case "resume":
		if _, err := engine.Resume(hash); err != nil {
			return "ripresa non riuscita: " + err.Error(), true
		}
		return "torrent ripreso", false
	case "recheck":
		if ok, err := engine.ForceRecheck(hash); err != nil || !ok {
			return "recheck non avviato", true
		}
		return "recheck avviato", false
	case "reannounce":
		if _, err := engine.Reannounce(hash); err != nil {
			return "riannuncio non riuscito: " + err.Error(), true
		}
		return "riannuncio inviato", false
	}
	return "azione sconosciuta: " + action, true
}

func v2HTTPAction(value string) (string, bool) {
	action, id, ok := strings.Cut(value, ":")
	if !ok || id == "" {
		return "azione HTTP non valida", true
	}
	switch action {
	case "pause":
		if !PauseHTTPDownload(id) {
			return "pausa HTTP non riuscita", true
		}
		return "download HTTP in pausa", false
	case "resume":
		if !ResumeHTTPDownload(id) {
			return "ripresa HTTP non riuscita", true
		}
		return "download HTTP ripreso", false
	case "remove":
		if !CancelHTTPDownload(id, false) {
			return "rimozione HTTP non riuscita", true
		}
		return "download HTTP rimosso", false
	}
	return "azione HTTP sconosciuta: " + action, true
}

func v2TorrentBulkAction(s *AppState, r *http.Request, action string) (string, bool) {
	hashes := r.Form["hash"]
	if len(hashes) == 0 {
		return "nessun torrent selezionato", true
	}
	engine := s.activeEngine()
	done := 0
	for _, hash := range hashes {
		switch action {
		case "pause":
			if _, err := engine.Pause(hash); err == nil {
				done++
			}
		case "resume":
			if _, err := engine.Resume(hash); err == nil {
				done++
			}
		case "recheck":
			if ok, err := engine.ForceRecheck(hash); err == nil && ok {
				done++
			}
		case "remove":
			if removed, err := engine.Remove(hash, false); err == nil && removed {
				done++
			}
		case "unpin":
			if raw, status := v2InternalJSON(s, http.MethodPost, "/api/torrents/unpin", nil, []byte(`{}`)); status < 400 && raw != nil {
				done++
			}
		case "tag", "untag":
			tag := strings.TrimSpace(r.FormValue("tag"))
			if action == "tag" && tag == "__new__" {
				tag = strings.TrimSpace(r.FormValue("new_tag"))
				if tag != "" {
					catalogBody, _ := json.Marshal(map[string]string{"tag": tag})
					v2InternalJSON(s, http.MethodPost, "/api/download-tags", nil, catalogBody)
				}
			}
			if action == "untag" {
				tag = ""
			}
			if err := s.db.SetTorrentTag(hash, tag); err == nil {
				done++
			}
		}
	}
	return fmt.Sprintf("%s: %d/%d torrent", action, done, len(hashes)), false
}

type v2DetailTab struct {
	ID, Label string
	Active    bool
}

type v2KV struct{ Label, Value string }

// v2PieceRun is one run of consecutive pieces sharing a state, with the width
// (percentage) used to draw the piece map.
type v2PieceRun struct {
	Begin int
	End   int
	State string
	Pct   string
}

type v2DetailView struct {
	Hash     string
	Name     string
	Tab      string
	Torrent  models.TorrentView
	Magnet   string
	NoRename bool
	Tabs     []v2DetailTab
	General  []v2KV
	Trackers []models.TrackerView
	Files    []models.FileView
	Peers    []models.PeerView
	// AcqID is the short acquisition ID printed in the log (`acq: 7f3a2c`).
	AcqID string
	// History is the stored story of this download (tab "history").
	History []AcquisitionEvent
	// Pieces is the piece map for backends that report it (gx-torrent).
	Pieces      []v2PieceRun
	PieceCount  int
	PiecesDone  int
	PiecesReady bool
	Error       string
	// TabError explains why a tab is empty (engine unreachable, unsupported).
	TabError string
	Caps     v2DetailCaps
}

// v2DetailCaps tells the detail form which controls the active engine
// supports, so it never offers an action that can only fail.
type v2DetailCaps struct {
	Backend      string
	SuperSeeding bool
	WebSeeds     bool
	RateLimits   bool
	Connections  bool
	// Pieces: the engine can report per-piece diagnostics.
	Pieces bool
	// FileLevels: priority levels; without it only skip/download.
	FileLevels bool
	// FileNote is shown above the file list.
	FileNote string
	// LimitsNote is shown in the limits tab.
	LimitsNote string
	// TrackerNote is shown in the trackers tab.
	TrackerNote string
	// GeneralNote is shown in the general tab (data the engine cannot report).
	GeneralNote string
}

func v2DetailCapsFor(backend string) v2DetailCaps {
	switch backend {
	case BackendGxTorrent:
		return v2DetailCaps{
			Backend: "gx-torrent", SuperSeeding: true, WebSeeds: true, Pieces: true, RateLimits: true, Connections: true,
			FileNote:    "gx-torrent scarica o salta ogni file (nessun livello di priorità); cambiare la selezione riavvia il torrent per un attimo.",
			LimitsNote:  "I limiti valgono per questo torrent: -1 usa il limite globale, 0 è illimitato.",
			TrackerNote: "gx-torrent sostituisce l'intera lista dei tracker: le righe cancellate vengono rimosse.",
			GeneralNote: "Con gx-torrent non è disponibile l'upload/share mode. Il download sequenziale e il super-seeding (BEP 16) si attivano anche a caldo, la prima/ultima parte solo quando aggiungi il torrent; il super-seeding vale solo a torrent completato e riduce di proposito l'upload del seed.",
		}
	case BackendQbittorrent:
		return v2DetailCaps{
			Backend: "qBittorrent-nox", SuperSeeding: true, RateLimits: true, FileLevels: true,
			LimitsNote: "qBittorrent non ha connessioni e slot di upload per singolo torrent: si impostano solo globalmente.",
		}
	default:
		return v2DetailCaps{Backend: "libtorrent", SuperSeeding: true, WebSeeds: true, RateLimits: true, Connections: true, FileLevels: true}
	}
}

func v2FindTorrent(s *AppState, hash string) (models.TorrentView, bool) {
	for _, torrent := range s.activeEngine().List() {
		if strings.EqualFold(torrent.Hash, hash) {
			return torrent, true
		}
	}
	return models.TorrentView{}, false
}

func v2DetailViewFrom(s *AppState, hash, tab string) v2DetailView {
	view := v2DetailView{Hash: hash, Tab: tab, Caps: v2DetailCapsFor(s.activeEngine().Name())}
	view.Tabs = []v2DetailTab{
		{ID: "general", Label: "Generale"}, {ID: "trackers", Label: "Tracker"},
		{ID: "files", Label: "Contenuto"}, {ID: "peers", Label: "Peers"},
		{ID: "limits", Label: "Limiti"}, {ID: "storage", Label: "Storage"},
	}
	if _, ok := s.activeEngine().(TorrentPieceInspector); ok {
		view.Tabs = append(view.Tabs, v2DetailTab{ID: "pieces", Label: "Pezzi"})
	}
	view.Tabs = append(view.Tabs, v2DetailTab{ID: "history", Label: "Storia"})
	view.AcqID = logging.AcqID(hash)
	for index := range view.Tabs {
		view.Tabs[index].Active = view.Tabs[index].ID == tab
	}
	torrent, ok := v2FindTorrent(s, hash)
	if !ok {
		view.Error = "torrent non trovato nella sessione"
		return view
	}
	view.Name = torrent.Name
	view.Torrent = torrent
	magnet := ""
	if meta, err := s.db.TorrentMeta(hash); err == nil && meta != nil {
		magnet = meta.Release.Magnet
	}
	noRename, _ := s.db.TorrentNoRename(hash)
	view.NoRename = noRename
	view.Magnet = magnet
	archived := false
	if processed, err := s.db.TorrentProcessed(hash); err == nil && processed != nil && strings.TrimSpace(*processed) != "" {
		archived = true
	}
	isCompleted := archived
	if !isCompleted {
		if status, err := s.db.TorrentStatus(hash); err == nil && status != nil && *status == "completed" {
			isCompleted = true
		}
	}
	stateLabel := uiStateLabel(torrent.State)
	progressVal := torrent.Progress
	doneVal := torrent.TotalDone
	if isCompleted {
		progressVal = 100.0
		if torrent.TotalSize > 0 && doneVal < torrent.TotalSize {
			doneVal = torrent.TotalSize
		}
		if torrent.State == "paused" {
			stateLabel = "Completato"
		}
	}
	view.General = []v2KV{
		{Label: "Stato", Value: stateLabel},
		{Label: "Progresso", Value: fmt.Sprintf("%.1f%%", progressVal)},
		{Label: "Dimensione", Value: logging.HumanBytesI64(torrent.TotalSize)},
		{Label: "Scaricato", Value: logging.HumanBytesI64(doneVal)},
		{Label: "Download totale", Value: logging.HumanBytesI64(torrent.AllTimeDownload)},
		{Label: "Caricato", Value: logging.HumanBytesI64(torrent.AllTimeUpload)},
		{Label: "Ratio", Value: fmt.Sprintf("%.2f", uiTorrentRatio(torrent.AllTimeUpload, torrent.AllTimeDownload, doneVal, torrent.TotalSize))},
		{Label: "↓ / ↑", Value: logging.HumanRate(saturatingInt64(torrent.DownloadRate)) + " / " + logging.HumanRate(saturatingInt64(torrent.UploadRate))},
		{Label: "Peer / Seed", Value: fmt.Sprintf("%d / %d", torrent.NumPeers, torrent.NumSeeds)},
		{Label: "Sciame (seed / peer)", Value: v2SwarmLabel(torrent.NumComplete, torrent.NumIncomplete)},
		{Label: "Posizione coda", Value: fmt.Sprintf("%d", torrent.QueuePosition)},
		{Label: "Metadata", Value: ternaryString(torrent.HasMetadata, "presenti", "in attesa")},
		{Label: "Versione torrent", Value: torrent.TorrentVersion},
		{Label: "Auto-managed", Value: ternaryString(torrent.AutoManaged, "sì", "no")},
		{Label: "Percorso di salvataggio", Value: torrent.SavePath},
		{Label: "Tracker corrente", Value: torrent.CurrentTracker},
		{Label: "Non rinominare", Value: fmt.Sprintf("%t", noRename)},
		{Label: "Magnet", Value: magnet},
		{Label: "ID acquisizione", Value: view.AcqID},
	}
	if torrent.Error != "" {
		view.General = append(view.General, v2KV{Label: "Errore", Value: torrent.Error})
	}
	view.General = append(view.General, v2KV{Label: "Motore", Value: view.Caps.Backend})
	for index := range view.General {
		if strings.TrimSpace(view.General[index].Value) == "" {
			view.General[index].Value = "—"
		}
	}
	var tabErr error
	switch tab {
	case "trackers":
		view.Trackers, _, tabErr = s.activeEngine().Trackers(hash)
	case "files":
		view.Files, _, tabErr = s.activeEngine().Files(hash)
	case "peers":
		view.Peers, _, tabErr = s.activeEngine().Peers(hash)
	case "history":
		var historyErr error
		if view.History, historyErr = s.db.AcquisitionEvents(hash); historyErr != nil {
			view.TabError = "Storia non disponibile: " + historyErr.Error()
		}
	case "pieces":
		if inspector, ok := s.activeEngine().(TorrentPieceInspector); ok {
			var runs []TorrentPieceRun
			runs, _, tabErr = inspector.PieceRuns(hash)
			total := 0
			for _, run := range runs {
				total += run.End - run.Begin + 1
			}
			view.PieceCount = total
			view.PiecesReady = total > 0
			for _, run := range runs {
				length := run.End - run.Begin + 1
				pct := "0"
				if total > 0 {
					pct = strconv.FormatFloat(float64(length)*100/float64(total), 'f', 3, 64)
				}
				view.Pieces = append(view.Pieces, v2PieceRun{Begin: run.Begin, End: run.End, State: run.State, Pct: pct})
				if run.State == "have" {
					view.PiecesDone += length
				}
			}
		}
	}
	if tabErr != nil {
		view.TabError = "Il motore non ha risposto: " + tabErr.Error()
	}
	return view
}

// v2SwarmLabel shows the tracker scrape, "n/d" when no tracker answered.
func v2SwarmLabel(seeds, peers int) string {
	if seeds < 0 && peers < 0 {
		return "n/d"
	}
	format := func(value int) string {
		if value < 0 {
			return "?"
		}
		return strconv.Itoa(value)
	}
	return format(seeds) + " / " + format(peers)
}

// V2DownloadsDetail renders the whole detail modal.
func V2DownloadsDetail(w http.ResponseWriter, r *http.Request, s *AppState) {
	tab := r.FormValue("tab")
	if tab == "" {
		tab = "general"
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_detail_modal", v2DetailViewFrom(s, r.FormValue("hash"), tab), dict, eng)
}

// V2DownloadsDetailPanel renders only the tab panel.
func V2DownloadsDetailPanel(w http.ResponseWriter, r *http.Request, s *AppState) {
	tab := r.FormValue("tab")
	if tab == "" {
		tab = "general"
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_detail_panel", v2DetailViewFrom(s, r.FormValue("hash"), tab), dict, eng)
}

// V2DownloadsDetailAction keeps all torrent-detail mutations inside the v2
// modal. The browser submits ordinary form fields; this handler translates them
// into the existing JSON API payloads and returns the refreshed active tab.
func V2DownloadsDetailAction(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := strings.TrimSpace(r.FormValue("hash"))
	op := strings.TrimSpace(r.FormValue("op"))
	tab := strings.TrimSpace(r.FormValue("tab"))
	if tab == "" {
		tab = "general"
	}
	if hash == "" {
		http.Error(w, "torrent mancante", http.StatusBadRequest)
		return
	}
	path := ""
	body := []byte(r.FormValue("body"))
	if body == nil || len(body) == 0 {
		body = []byte(`{}`)
	}
	switch op {
	case "no_rename", "reannounce", "restart", "mark_failed", "super-seeding":
		path = "/api/torrents/" + url.PathEscape(hash) + "/" + op
	case "pin":
		path = "/api/torrents/pin"
		body, _ = json.Marshal(map[string]any{"hash": hash})
	case "tag":
		path = "/api/torrent-tags"
		body, _ = json.Marshal(map[string]string{"hash": hash, "tag": r.FormValue("tag")})
	case "limits":
		path = "/api/torrents/" + url.PathEscape(hash) + "/limits"
		download := v2ParseInt(r.FormValue("download_limit"), -1) * 1024
		upload := v2ParseInt(r.FormValue("upload_limit"), -1) * 1024
		ratio := v2ParseFloat(r.FormValue("seed_ratio"), -1)
		days := v2ParseInt(r.FormValue("seed_days"), -1)
		maxConnections := v2ParseInt(r.FormValue("max_connections"), -1)
		maxUploads := v2ParseInt(r.FormValue("max_uploads"), -1)
		body, _ = json.Marshal(map[string]any{"download_limit": download, "upload_limit": upload,
			"seed_ratio": ratio, "seed_days": days, "max_connections": maxConnections, "max_uploads": maxUploads})
	case "storage":
		path = "/api/torrents/" + url.PathEscape(hash) + "/storage"
		body, _ = json.Marshal(StoragePath{Path: strings.TrimSpace(r.FormValue("path"))})
	case "web-seeds":
		path = "/api/torrents/" + url.PathEscape(hash) + "/web-seeds"
		urls := strings.Fields(strings.TrimSpace(r.FormValue("urls")))
		body, _ = json.Marshal(WebSeedsInput{Urls: urls, Remove: r.FormValue("remove") == "true"})
	case "trackers":
		path = "/api/torrents/" + url.PathEscape(hash) + "/trackers"
		entries := []TrackerEntryInput{}
		for _, line := range strings.Split(r.FormValue("trackers"), "\n") {
			parts := strings.SplitN(strings.TrimSpace(line), "|", 2)
			if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
				continue
			}
			tier, _ := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 32)
			entries = append(entries, TrackerEntryInput{Tier: int32(tier), Url: strings.TrimSpace(parts[1])})
		}
		body, _ = json.Marshal(TrackersInput{Trackers: entries})
	case "files-priority":
		path = "/api/torrents/" + url.PathEscape(hash) + "/files/priority"
		files, _, _ := s.activeEngine().Files(hash)
		priorities := make([]int32, len(files))
		for index, file := range files {
			priorities[index] = int32(file.Priority)
		}
		parsedIndex, err := strconv.Atoi(strings.TrimSpace(r.FormValue("index")))
		if err != nil || parsedIndex < 0 || parsedIndex >= len(priorities) {
			http.Error(w, "indice file non valido", http.StatusBadRequest)
			return
		}
		p := v2ParseInt32(r.FormValue("priority"), 0)
		if p < 0 {
			p = 0
		} else if p > 7 {
			p = 7
		}
		priorities[parsedIndex] = p
		body, _ = json.Marshal(FilePrioritiesInput{Priorities: priorities})
	default:
		http.Error(w, "azione dettaglio non valida", http.StatusBadRequest)
		return
	}
	raw, status := v2InternalJSON(s, http.MethodPost, path, nil, body)
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/?view=downloads", http.StatusSeeOther)
		return
	}
	if status >= 400 {
		view := v2DetailViewFrom(s, hash, tab)
		view.Error = v2JSONError(raw)
		dict, eng := v2Dictionaries(s)
		v2Render(w, http.StatusOK, "v2_detail_panel", view, dict, eng)
		return
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_detail_panel", v2DetailViewFrom(s, hash, tab), dict, eng)
}

func v2ParseInt32(value string, fallback int32) int32 {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 32)
	if err != nil {
		return fallback
	}
	return int32(parsed)
}

// safeV2Redirect validates that a redirect target is a safe relative path
// within the /v2 tree, guarding against open-redirect attacks.
// safeInternalRedirect accepts any same-site path ("/...") while rejecting
// protocol-relative ("//host"), absolute URLs and traversal segments, so an
// action can return to the page it came from without opening a redirect.
func safeInternalRedirect(raw, fallback string) string {
	cleaned := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	if !strings.HasPrefix(cleaned, "/") || strings.HasPrefix(cleaned, "//") {
		return fallback
	}
	parsed, err := url.Parse(cleaned)
	if err != nil || parsed.Hostname() != "" || parsed.Scheme != "" {
		return fallback
	}
	// Browsers normalize "/..//host" to a protocol-relative URL, so a traversal
	// segment would escape the site: refuse it.
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == ".." {
			return fallback
		}
	}
	return cleaned
}

func v2ParseInt(value string, fallback int64) int64 {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func v2ParseFloat(value string, fallback float64) float64 {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return fallback
	}
	return parsed
}

// V2DownloadsRemoveModal renders the remove-with-options dialog.
func V2DownloadsRemoveModal(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := r.FormValue("hash")
	name := hash
	onRamdisk := false
	if torrent, ok := v2FindTorrent(s, hash); ok {
		name = torrent.Name
		onRamdisk = v2TorrentOnRamdisk(torrent.SavePath, latestConfig(s))
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_remove_modal", map[string]any{"Hash": hash, "Name": name, "OnRamdisk": onRamdisk}, dict, eng)
}

// v2TorrentOnRamdisk reports whether a torrent's payload resides in the
// configured RAM-disk tree. filepath.Rel avoids false positives such as
// /mnt/ramdisk-old matching /mnt/ramdisk.
func v2TorrentOnRamdisk(savePath string, cfg *Config) bool {
	if cfg == nil || cfg.RamdiskDir() == nil || strings.TrimSpace(savePath) == "" {
		return false
	}
	root := filepath.Clean(*cfg.RamdiskDir())
	relative, err := filepath.Rel(root, filepath.Clean(savePath))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// V2DownloadsRemove removes a torrent and swaps the table back in, clearing the
// modal with an out-of-band swap.
func V2DownloadsRemove(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := strings.ToLower(strings.TrimSpace(r.FormValue("hash")))
	if !isHexString(hash) || strings.Contains(hash, "..") {
		http.Error(w, "invalid torrent hash", http.StatusBadRequest)
		return
	}
	mode := r.FormValue("mode")
	blocklist := mode == "blocklist" || mode == "files_blocklist"
	deleteFiles := mode == "files" || mode == "files_blocklist"

	if blocklist {
		if meta, err := s.db.TorrentMeta(hash); err == nil && meta != nil {
			_ = s.db.Blocklist(&meta.Release, "manual")
		}
	}
	cfg := latestConfig(s)
	var removed bool
	var err error
	message := "torrent rimosso"
	isErr := false

	if mode == "archive" {
		removed, err = ArchiveAndRemoveTorrent(s, cfg, hash)
		if err != nil || !removed {
			if err != nil {
				message = err.Error()
			} else {
				message = "archiviazione non riuscita"
			}
			isErr = true
		} else {
			message = "torrent archiviato e rimosso dal seed"
		}
	} else {
		removed, err = SafeRemoveTorrent(s, cfg, hash, deleteFiles)
		if err != nil || !removed {
			message, isErr = "rimozione non riuscita", true
		}
	}

	dict, eng := v2Dictionaries(s)
	var buffer bytes.Buffer
	if err := v2Templates.ExecuteTemplate(&buffer, "v2_torrents_wrap", v2TorrentsViewFrom(s, r, message, isErr)); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	body := v2TranslateHTML(buffer.String(), dict, eng)
	body += `<div id="v2-modal" hx-swap-oob="true"></div>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

// ---------------------------------------------------------------------------
// Configurazione (settings)
// ---------------------------------------------------------------------------

type v2Field struct {
	uiSettingField
	Status string
	Error  bool
}

// DescribedBy lists the IDs of the texts that describe the control (hint,
// unit, special value, save result), for aria-describedby.
func (field v2Field) DescribedBy() string {
	ids := []string{}
	if field.Hint != "" {
		ids = append(ids, "hint-"+field.Key)
	}
	if field.Unit != "" {
		ids = append(ids, "unit-"+field.Key)
	}
	if field.Zero != "" {
		ids = append(ids, "zero-"+field.Key)
	}
	if field.Status != "" || field.Disabled {
		ids = append(ids, "status-"+field.Key)
	}
	return strings.Join(ids, " ")
}

// Browse reports whether the field is a server folder with a «Sfoglia» picker.
func (field v2Field) Browse() bool {
	switch field.Key {
	case "archive_root", "trash_path", "libtorrent_dir", "libtorrent_temp_dir", "libtorrent_torrent_copy_dir", "libtorrent_ramdisk_dir":
		return true
	}
	return false
}

// Resettable reports whether the row offers «Predefinito»: whenever the field
// has a registered default (even an empty one), so the button is available on
// every resetting field and a user who changed something can always restore it.
func (field v2Field) Resettable() bool {
	return field.HasDefault && !field.Managed && !field.Disabled && field.Kind != "structured" && field.Kind != "secret"
}

// DefaultInline reports whether the default can be shown inline in the row
// header. Long or multi-line defaults (blacklist, extra settings) would break
// the layout, so they only get the button.
func (field v2Field) DefaultInline() bool {
	return field.Default != "" && !strings.Contains(field.Default, "\n") && len([]rune(field.Default)) <= 80
}

// v2UnusedSettingsHint explains the closed panels of options the active
// torrent engine ignores (each row still says which engine is active).
const v2UnusedSettingsHint = "Valgono solo con un altro motore torrent: restano salvate e tornano attive cambiando motore."

type v2SettingGroup struct {
	Title     string
	Hint      string
	Fields    []v2Field
	Buttons   []v2Button
	Grid      bool
	Collapsed bool
	// Inactive marks a whole panel that the active torrent engine ignores.
	Inactive bool
	// ID is the anchor of the panel, used by the section's group index.
	ID string
}

// v2Fields wraps raw settings fields with the transient save status.
func v2Fields(fields []uiSettingField) []v2Field {
	out := make([]v2Field, 0, len(fields))
	for _, field := range fields {
		out = append(out, v2Field{uiSettingField: field})
	}
	return out
}

// v2SettingsArea is one heading of the settings navigation with its tabs.
type v2SettingsArea struct {
	Name, Label string
	Tabs        []uiSettingsTabRef
}

type v2SettingsView struct {
	Tabs        []uiSettingsTabRef
	Areas       []v2SettingsArea
	ActiveID    string
	ActiveLabel string
	Intro       string
	Groups      []v2SettingGroup
	Note        string
	Empty       string
	Highlight   string
	Actions     *v2SettingsActions
	ScoreGroups []v2ScoreGroup
	ScoreNotice string
	ScoreError  bool
	// Structured editors (feeds, checkbox groups, list editors, rename,
	// translations), rendered on the tab that owns them.
	EditorsFirst   bool
	ShowFeeds      bool
	Feeds          string
	CheckboxGroups []v2CheckboxGroup
	ListEditors    []v2ListEditorView
	Rename         *v2RenameView
	I18n           *v2I18nView
}

// v2SettingsActions is the toolbar of API actions of a tab (Ottimizza, ...).
type v2SettingsActions struct {
	Title, Hint string
	Buttons     []v2Button
}

type v2ScoreGroup struct {
	Name  string
	Score string
}

func v2ScoreGroupsFrom(s *AppState) []v2ScoreGroup {
	cfg := latestConfig(s)
	groups := []v2ScoreGroup{}
	for key, value := range cfg.Settings {
		lower := strings.ToLower(key)
		if !strings.HasPrefix(lower, "score_group_") || len(lower) == len("score_group_") {
			continue
		}
		groups = append(groups, v2ScoreGroup{Name: strings.TrimPrefix(lower, "score_group_"), Score: value})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	return groups
}

func v2SettingsViewFrom(s *AppState, tab, highlight string) v2SettingsView {
	page := uiSettingsPageFrom(s, tab)
	view := v2SettingsView{
		Tabs:        page.Tabs,
		ActiveID:    page.ActiveID,
		Highlight:   highlight,
		ScoreGroups: v2ScoreGroupsFrom(s),
		Empty:       "Nessuna impostazione in questa sezione.",
	}
	for _, area := range uiSettingsAreas {
		converted := v2SettingsArea{Name: area.Name, Label: area.Label}
		for _, ref := range page.Tabs {
			if ref.Area == area.Name {
				converted.Tabs = append(converted.Tabs, ref)
			}
		}
		if len(converted.Tabs) > 0 {
			view.Areas = append(view.Areas, converted)
		}
	}
	for _, ref := range page.Tabs {
		if ref.Active {
			view.ActiveLabel = ref.Label
			view.Intro = ref.Intro
		}
	}
	// The tab is reached again from its own buttons: the redirect keeps it.
	redirect := "/?view=settings&tab=" + page.ActiveID
	buttons := func(list []uiActionButton) []v2Button {
		out := v2Buttons("settings", list)
		for index := range out {
			out[index].Redirect = redirect
		}
		return out
	}
	if page.Actions != nil {
		view.Actions = &v2SettingsActions{Title: page.Actions.Label, Hint: page.Actions.Hint, Buttons: buttons(page.Actions.Buttons)}
	}
	// Settings the active torrent engine ignores never sit among the ones
	// that matter: a panel made only of them is closed, and the stray ones of
	// a mixed panel move to a closed panel at the end of the tab.
	unused := v2SettingGroup{Title: "Non usate dal motore attivo", Collapsed: true, Inactive: true}
	for _, group := range page.Groups {
		converted := v2SettingGroup{Title: group.Title, Hint: group.Hint, Grid: group.Grid, Collapsed: group.Collapsed, Buttons: buttons(group.Buttons)}
		allDisabled := len(group.Fields) > 0
		for _, field := range group.Fields {
			allDisabled = allDisabled && field.Disabled
		}
		for _, field := range group.Fields {
			if field.Disabled && !allDisabled {
				unused.Fields = append(unused.Fields, v2Field{uiSettingField: field})
				continue
			}
			converted.Fields = append(converted.Fields, v2Field{uiSettingField: field})
		}
		if allDisabled {
			converted.Collapsed, converted.Inactive = true, true
			if converted.Hint == "" {
				converted.Hint = v2UnusedSettingsHint
			}
		}
		if len(converted.Fields) > 0 {
			view.Groups = append(view.Groups, converted)
		}
	}
	if len(unused.Fields) > 0 {
		unused.Hint = v2UnusedSettingsHint
		view.Groups = append(view.Groups, unused)
	}
	// Stable anchors for the in-section group index.
	for i := range view.Groups {
		view.Groups[i].ID = fmt.Sprintf("v2-settings-group-%d", i)
	}
	view.EditorsFirst = page.EditorsFirst
	if page.ShowSources {
		view.ShowFeeds = true
		view.Feeds = v2FeedsFrom(s)
		view.CheckboxGroups = v2CheckboxGroupsFrom(page.CheckboxGroups)
	}
	for _, editor := range page.ListEditors {
		if key, ok := v2ListEditorKey(editor); ok {
			view.ListEditors = append(view.ListEditors, v2ListEditorViewFrom(s, editor, key, "settings", page.ActiveID))
		}
	}
	if page.Rename != nil {
		selected := page.Rename.Format
		formats := make([]uiFormOption, 0, len(page.Rename.Formats))
		for _, format := range page.Rename.Formats {
			formats = append(formats, uiFormOption{Value: format.Value, Label: format.Label, Selected: format.Value == selected})
		}
		view.Rename = &v2RenameView{Format: selected, Template: page.Rename.Template, Formats: formats, Tokens: page.Rename.Tokens}
		view.Rename.Preview = v2RenameCompose(view.Rename.Format, view.Rename.Template)
	}
	if page.ShowI18n {
		i18nView := v2I18nViewFrom(s, "")
		view.I18n = &i18nView
	}
	return view
}

// V2SettingsScoreGroup adds, updates or removes one user-defined release-group
// score. Group suffixes use the same lower-case spelling as release parsing.
func V2SettingsScoreGroup(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := strings.ToLower(strings.TrimSpace(r.FormValue("name")))
	op := r.FormValue("op")
	notice := ""
	isError := false
	validName := name != "" && name != "unknown" && len(name) <= 64
	for _, char := range name {
		// parseQuality recognizes final release-group tags as alphanumeric
		// suffixes, so accepting punctuation here would create a group that
		// can never match a release.
		if !((char >= 'a' && char <= 'z') || (char >= '0' && char <= '9')) {
			validName = false
			break
		}
	}
	if !validName {
		notice, isError = "Nome gruppo non valido: usa solo lettere e numeri (max 64 caratteri; «unknown» è riservato).", true
	} else {
		key := "score_group_" + name
		switch op {
		case "", "save", "add":
			value := strings.TrimSpace(r.FormValue("score"))
			if _, err := strconv.ParseInt(value, 10, 64); err != nil {
				notice, isError = "Il punteggio deve essere un numero intero.", true
			} else if op == "add" && latestConfig(s).Settings[key] != "" {
				notice, isError = "Il gruppo esiste già; modifica il punteggio nella sua riga.", true
			} else if err := saveConfigSetting(s.cfg.DataDir, key, value); err != nil {
				notice, isError = err.Error(), true
			} else {
				notice = "Punteggio gruppo salvato."
			}
		case "delete":
			removed, err := DeleteSetting(s.cfg.DataDir, key)
			if err != nil {
				notice, isError = err.Error(), true
			} else if !removed {
				notice, isError = "Gruppo non trovato.", true
			} else {
				notice = "Gruppo rimosso."
			}
		default:
			notice, isError = "Operazione gruppo non riconosciuta.", true
		}
	}
	data := struct {
		ScoreGroups []v2ScoreGroup
		ScoreNotice string
		ScoreError  bool
	}{ScoreGroups: v2ScoreGroupsFrom(s), ScoreNotice: notice, ScoreError: isError}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_score_groups", data, dict, eng)
}

// v2ListEditorKey maps a list editor to the stable key the row/save endpoints use.
func v2ListEditorKey(editor uiListEditor) (string, bool) {
	switch editor.GetPath {
	case "/api/config":
		return "indexers", true
	case "/api/config/source-filters":
		return "source_filters", true
	case "/api/tag-dir-rules":
		return "tag_dir_rules", true
	case "/api/event-hooks":
		return "event_hooks", true
	case "/api/watched-folders":
		return "watched_folders", true
	}
	return "", false
}

// V2SettingsBody swaps the settings navigation and section (tab navigation,
// search results). The address bar follows the section actually shown.
func V2SettingsBody(w http.ResponseWriter, r *http.Request, s *AppState) {
	dict, eng := v2Dictionaries(s)
	view := v2SettingsViewFrom(s, r.FormValue("tab"), r.FormValue("highlight"))
	if r.Header.Get("HX-Request") != "" {
		w.Header().Set("HX-Push-Url", "/?view=settings&tab="+view.ActiveID)
	}
	v2Render(w, http.StatusOK, "v2_settings_body", view, dict, eng)
}

type v2SettingMatch struct {
	Key, Label, Tab, TabLabel string
}

func v2SettingLabelFor(key string) string {
	for _, def := range append(append([]uiSettingDef{}, uiSettingsIndex...), uiScoreSettingDefs...) {
		if def.Key == key {
			return def.Label
		}
	}
	return uiSettingAutoLabel(key)
}

// V2SettingsSearch filters the settings index server-side.
func V2SettingsSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	query := strings.ToLower(strings.TrimSpace(r.FormValue("q")))
	labels := map[string]string{}
	for _, tab := range uiSettingsTabs {
		labels[tab.ID] = tab.Label
	}
	out := struct {
		Query   string
		Matches []v2SettingMatch
	}{Query: r.FormValue("q")}
	dict, eng := v2Dictionaries(s)
	if query == "" {
		v2Render(w, http.StatusOK, "v2_settings_search", out, dict, eng)
		return
	}
	seen := map[string]bool{}
	defs := append(append([]uiSettingDef{}, uiSettingsIndex...), uiScoreSettingDefs...)
	for _, def := range defs {
		haystack := strings.ToLower(def.Key + " " + def.Label + " " + uiSettingSearchTerms[def.Key])
		if !strings.Contains(haystack, query) || seen[def.Key] {
			continue
		}
		seen[def.Key] = true
		out.Matches = append(out.Matches, v2SettingMatch{Key: def.Key, Label: def.Label, Tab: def.Tab, TabLabel: labels[def.Tab]})
	}
	// The structured editors have no key: their result opens the section.
	for _, entry := range uiSettingsEditorSearchEntries {
		if strings.Contains(strings.ToLower(entry.Label), query) {
			out.Matches = append(out.Matches, v2SettingMatch{Label: entry.Label, Tab: entry.Tab, TabLabel: labels[entry.Tab]})
		}
	}
	v2Render(w, http.StatusOK, "v2_settings_search", out, dict, eng)
}

// V2SettingsSave saves one setting and re-renders its field with the result.
func V2SettingsSave(w http.ResponseWriter, r *http.Request, s *AppState) {
	key := strings.TrimSpace(r.FormValue("key"))
	value := r.FormValue("value")
	field := uiSettingFieldFor(key, v2SettingLabelFor(key), value)
	dict, eng := v2Dictionaries(s)

	render := func(status string, isErr bool) {
		if r.Header.Get("HX-Request") == "" {
			http.Redirect(w, r, "/?view=settings", http.StatusSeeOther)
			return
		}
		v2Render(w, http.StatusOK, "v2_setting_field", v2Field{uiSettingField: field, Status: status, Error: isErr}, dict, eng)
	}

	if !gh7_setting_key_allowed(key) || key == "" || len(key) > 128 {
		render("chiave non modificabile", true)
		return
	}
	if uiScoreSettingKeys[key] {
		if _, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err != nil {
			render("il punteggio deve essere un numero intero", true)
			return
		}
	}
	if uiSettingIsSecret(key) && strings.TrimSpace(value) == "" {
		render("non modificata", false)
		return
	}
	// The weekdays are one checkbox each: join them back into "0,1,4".
	if uiSettingMetaByKey[key].Kind == "days" {
		_ = r.ParseForm()
		value = uiDaysValue(r.Form["value"])
		field = uiSettingFieldFor(key, v2SettingLabelFor(key), value)
	}
	if _, ok := uiJSONScalarList(latestConfig(s).Settings[key]); ok {
		items := []string{}
		for _, line := range strings.FieldsFunc(value, func(r rune) bool { return r == '\n' || r == ',' }) {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				items = append(items, trimmed)
			}
		}
		if encoded, err := json.Marshal(items); err == nil {
			value = string(encoded)
		}
	}
	if len(value) > 4096 {
		render("valore troppo grande", true)
		return
	}
	if err := validateBackendSetting(key, value); err != nil {
		render(err.Error(), true)
		return
	}
	if err := saveConfigSetting(s.cfg.DataDir, key, value); err != nil {
		render(err.Error(), true)
		return
	}
	field = uiSettingFieldFor(key, v2SettingLabelFor(key), value)
	render("salvata", false)
}

// ---------------------------------------------------------------------------
// Log
// ---------------------------------------------------------------------------

// v2LogsView is the v2 log tail with server-side level highlighting.
type v2LogsView struct {
	Lines    []template.HTML
	Count    int
	Filter   string
	LinesNum int
	// Log is the selected log file name; LogFiles lists the files available in
	// the data directory (current first, then rotated backups). ActiveLog is the
	// name of the file currently being written, so the selector can mark it.
	Log       string
	ActiveLog string
	LogFiles  []string
	// ProblemsOnly keeps only the WARN and ERROR lines.
	ProblemsOnly bool
}

var v2LogHighlight = []struct {
	re    *regexp.Regexp
	class string
}{
	{regexp.MustCompile(`\b(ERROR)\b`), "hl-err"},
	{regexp.MustCompile(`\b(WARN|WARNING)\b`), "hl-warn"},
	{regexp.MustCompile(`\b(completed|completato|archived|archiviato|moved|approved|approvato)\b`), "hl-ok"},
	{regexp.MustCompile(`\b(indexer|feed|source|sorgente|scraping|engine)\b`), "hl-src"},
	{regexp.MustCompile(`\b(upgrade|score|punteggio)\b`), "hl-score"},
	{regexp.MustCompile(`\b(filter|filtered|rejected|scartato|skipped|blocklist|stalled)\b`), "hl-filter"},
}

func v2HighlightLogLine(line string) template.HTML {
	escaped := stdhtml.EscapeString(line)
	for _, rule := range v2LogHighlight {
		if rule.re.MatchString(escaped) {
			escaped = rule.re.ReplaceAllString(escaped, `<span class="`+rule.class+`">$1</span>`)
		}
	}
	return template.HTML(escaped)
}

// v2LogLineIsProblem reports whether a log line is a WARN or ERROR line
// ("2026-10-07 04:21:19  WARN …").
func v2LogLineIsProblem(line string) bool {
	fields := strings.Fields(line)
	return len(fields) >= 3 && (fields[2] == "WARN" || fields[2] == "ERROR")
}

func v2LogsViewFrom(s *AppState, filter string, linesNum int, logName string, problemsOnly ...bool) v2LogsView {
	if linesNum <= 0 || linesNum > 5000 {
		linesNum = 500
	}
	path, selected := coreResolveLog(s.cfg.DataDir, logName)
	raw := coreTailLines(path, linesNum)
	view := v2LogsView{
		Filter:    filter,
		LinesNum:  linesNum,
		Log:       selected,
		ActiveLog: coreLogBaseName,
		LogFiles:  coreLogFiles(s.cfg.DataDir),
	}
	view.ProblemsOnly = len(problemsOnly) > 0 && problemsOnly[0]
	v2FillLogLines(&view, raw)
	return view
}

// v2GxLogsViewFrom is the gx-torrent daemon counterpart of v2LogsViewFrom: the
// daemon writes its own rotating log under <data>/gx-torrent/.
func v2GxLogsViewFrom(s *AppState, filter string, linesNum int, logName string, problemsOnly bool) v2LogsView {
	if linesNum <= 0 || linesNum > 5000 {
		linesNum = 500
	}
	path, selected := coreResolveGxLog(s.cfg.DataDir, logName)
	raw := coreTailLines(path, linesNum)
	view := v2LogsView{
		Filter:       filter,
		LinesNum:     linesNum,
		Log:          selected,
		ActiveLog:    coreGxLogBaseName,
		LogFiles:     coreGxLogFiles(s.cfg.DataDir),
		ProblemsOnly: problemsOnly,
	}
	v2FillLogLines(&view, raw)
	return view
}

// v2FillLogLines applies the text filter and the WARN/ERROR selection to raw
// log lines and stores the highlighted result.
func v2FillLogLines(view *v2LogsView, raw []string) {
	needle := strings.ToLower(view.Filter)
	for _, line := range raw {
		if needle != "" && !strings.Contains(strings.ToLower(line), needle) {
			continue
		}
		if view.ProblemsOnly && !v2LogLineIsProblem(line) {
			continue
		}
		view.Lines = append(view.Lines, v2HighlightLogLine(line))
	}
	view.Count = len(view.Lines)
}

// V2LogsPartial re-renders the log view (filter, refresh and periodic poll). It
// also updates the line count out of band, since only the <pre> is swapped.
func V2LogsPartial(w http.ResponseWriter, r *http.Request, s *AppState) {
	linesNum, _ := strconv.Atoi(r.FormValue("lines"))
	view := v2LogsViewFrom(s, r.FormValue("filter"), linesNum, r.FormValue("log"), r.FormValue("level") == "problems")
	dict, eng := v2Dictionaries(s)
	var buffer bytes.Buffer
	if err := v2Templates.ExecuteTemplate(&buffer, "v2_logs_view", view); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	body := v2TranslateHTML(buffer.String(), dict, eng)
	body += fmt.Sprintf(`<span id="v2-logs-count" hx-swap-oob="true">%d</span>`, view.Count)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

// V2GxLogsPartial re-renders the gx-torrent log panel in Maintenance (filter,
// refresh and the log-file selector). Like V2LogsPartial it updates the line
// count out of band, since only the <pre> is swapped.
func V2GxLogsPartial(w http.ResponseWriter, r *http.Request, s *AppState) {
	linesNum, _ := strconv.Atoi(r.FormValue("lines"))
	view := v2GxLogsViewFrom(s, r.FormValue("filter"), linesNum, r.FormValue("log"), r.FormValue("level") == "problems")
	dict, eng := v2Dictionaries(s)
	var buffer bytes.Buffer
	if err := v2Templates.ExecuteTemplate(&buffer, "v2_gx_log_view", view); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	body := v2TranslateHTML(buffer.String(), dict, eng)
	body += fmt.Sprintf(`<span id="v2-gx-logs-count" hx-swap-oob="true">%d</span>`, view.Count)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}
