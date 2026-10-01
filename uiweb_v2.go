package gextto

// uiweb_v2.go implements the "v2" user interface: a server-rendered (SSR)
// interface driven by HTMX instead of uiweb/static/gextto-ui.js.
//
// It is deliberately isolated and additive, so the classic UI keeps working
// unchanged while v2 grows:
//
//   - every route lives under /v2 and is registered by the single
//     registerV2Routes(s, mux) call in web_router.go;
//   - templates and static files are embedded from uiweb/v2 and never touch
//     uiweb/templates or uiweb/static;
//   - it reuses the existing Go view-models (uiDashboardDataFrom,
//     uiTorrentsDataFrom, uiSettingsPageFrom, ...) and the shared CSS, so the
//     data and the look come from the same source as the classic UI;
//   - the JSON APIs are untouched (the TUI and external clients keep working).
//
// Promotion to the default UI later is a routing change only: point "/" at
// these handlers once v2 reaches parity and is approved.
//
// Migration is delivered in verified batches. Views that are not migrated yet
// answer with v2_unavailable and link back to the classic page instead of
// pretending the feature is gone.

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

	xhtml "golang.org/x/net/html"

	"github.com/buzzqw/gextto/internal/logging"
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
	mux.Handle("GET /v2/static/", UiNoCache(http.StripPrefix("/v2/static/", http.FileServer(http.FS(v2StaticFSRoot())))))
	v2Handle(s, mux, "GET /v2", V2Page)
	v2Handle(s, mux, "GET /v2/", V2Page)
	v2Handle(s, mux, "GET /v2/empty", V2Empty)
	v2Handle(s, mux, "POST /v2/language", V2SetLanguage)
	v2Handle(s, mux, "POST /v2/run-cycle", V2RunCycle)
	v2Handle(s, mux, "POST /v2/dashboard/search", V2DashboardSearch)
	v2Handle(s, mux, "GET /v2/dashboard/feed", V2DashboardFeed)
	v2Handle(s, mux, "POST /v2/dashboard/feed/add", V2DashboardFeedAdd)

	// Scarico.
	v2Handle(s, mux, "POST /v2/downloads/table", V2DownloadsTable)
	v2Handle(s, mux, "POST /v2/downloads/settings", V2DownloadsSettings)
	v2Handle(s, mux, "GET /v2/downloads/http-detail", V2HTTPDownloadDetail)
	v2Handle(s, mux, "GET /v2/downloads/detail", V2DownloadsDetail)
	v2Handle(s, mux, "GET /v2/downloads/detail/panel", V2DownloadsDetailPanel)
	v2Handle(s, mux, "POST /v2/downloads/detail/action", V2DownloadsDetailAction)
	v2Handle(s, mux, "GET /v2/downloads/remove", V2DownloadsRemoveModal)
	v2Handle(s, mux, "POST /v2/downloads/remove", V2DownloadsRemove)

	// Configurazione.
	v2Handle(s, mux, "GET /v2/settings/body", V2SettingsBody)
	v2Handle(s, mux, "GET /v2/settings/search", V2SettingsSearch)
	v2Handle(s, mux, "POST /v2/settings/save", V2SettingsSave)
	v2Handle(s, mux, "POST /v2/settings/feed", V2SettingsFeed)
	v2Handle(s, mux, "POST /v2/settings/checkbox", V2SettingsCheckbox)
	v2Handle(s, mux, "GET /v2/settings/editor-row", V2SettingsEditorRow)
	v2Handle(s, mux, "POST /v2/settings/editor-save", V2SettingsEditorSave)
	v2Handle(s, mux, "POST /v2/settings/editor-test", V2SettingsEditorTest)
	v2Handle(s, mux, "GET /v2/settings/source-test", V2SettingsSourceTest)
	v2Handle(s, mux, "POST /v2/settings/rename-token", V2SettingsRenameToken)
	v2Handle(s, mux, "POST /v2/settings/rename-preview", V2SettingsRenamePreview)
	v2Handle(s, mux, "POST /v2/settings/rename-save", V2SettingsRenameSave)
	v2Handle(s, mux, "GET /v2/settings/i18n", V2SettingsI18nTable)
	v2Handle(s, mux, "POST /v2/settings/i18n/import", V2SettingsI18nImport)
	v2Handle(s, mux, "POST /v2/settings/i18n/delete", V2SettingsI18nDelete)

	// Log (frammento aggiornabile).
	v2Handle(s, mux, "GET /v2/partial/logs", V2LogsPartial)

	// Tabelle generiche (Serie TV, Film, Mancanti, Archivio, Blocklist, Fumetti).
	v2Handle(s, mux, "GET /v2/table", V2Table)
	v2Handle(s, mux, "POST /v2/table/action", V2TableAction)
	v2Handle(s, mux, "POST /v2/table/library", V2TableLibrary)
	v2Handle(s, mux, "POST /v2/table/gap-search", V2TableGapSearch)

	// Pannelli (Manutenzione, Integrazioni).
	v2Handle(s, mux, "POST /v2/section/action", V2SectionAction)
	v2Handle(s, mux, "POST /v2/section/form", V2SectionForm)
	v2Handle(s, mux, "POST /v2/section/test-ftp", V2SectionTestFTP)

	// Esplora (ricerca release).
	v2Handle(s, mux, "POST /v2/search", V2Search)
	v2Handle(s, mux, "POST /v2/search/add", V2SearchAdd)
	v2Handle(s, mux, "POST /v2/search/explain", V2SearchExplain)

	// Esplora TMDB (calendario, tendenze, ricerca).
	v2Handle(s, mux, "GET /v2/tmdb/calendar", V2TmdbCalendar)
	v2Handle(s, mux, "POST /v2/tmdb/discover", V2TmdbDiscover)
	v2Handle(s, mux, "POST /v2/tmdb/search", V2TmdbSearch)
	v2Handle(s, mux, "POST /v2/tmdb/add", V2TmdbAdd)
	v2Handle(s, mux, "GET /v2/tmdb/manual", V2TmdbManual)

	// Fumetti: link finder, download diretto e modifica della libreria.
	v2Handle(s, mux, "POST /v2/comics/links", V2ComicsLinks)
	v2Handle(s, mux, "POST /v2/comics/download", V2ComicsDownload)
	v2Handle(s, mux, "POST /v2/comics/explore/download", V2ComicsExploreDownload)
	v2Handle(s, mux, "GET /v2/comics/explore/select", V2ComicsExploreSelect)
	v2Handle(s, mux, "POST /v2/comics/explore/add", V2ComicsExploreAdd)
	v2Handle(s, mux, "GET /v2/comics/edit", V2ComicsEdit)
	v2Handle(s, mux, "POST /v2/comics/save", V2ComicsSave)

	// Dettagli Serie/Film.
	v2Handle(s, mux, "POST /v2/series/save", V2SeriesSave)
	v2Handle(s, mux, "GET /v2/series/sources", V2SeriesSources)
	v2Handle(s, mux, "GET /v2/series/rename-preview", V2SeriesRenamePreview)
	v2Handle(s, mux, "POST /v2/series/rename-execute", V2SeriesRenameExecute)

	// Manutenzione: cestino e verifica sorgenti.
	v2Handle(s, mux, "GET /v2/partial/trash", V2TrashList)
	v2Handle(s, mux, "POST /v2/trash/delete", V2TrashDelete)
	v2Handle(s, mux, "GET /v2/partial/sources-check", V2SourcesCheck)

	// Manutenzione: widget dedicati (duplicati, database, RAM disk, rinomina).
	v2Handle(s, mux, "POST /v2/maintenance/duplicates", V2Duplicates)
	v2Handle(s, mux, "POST /v2/maintenance/db", V2DBMaintenance)
	v2Handle(s, mux, "POST /v2/maintenance/ramdisk", V2Ramdisk)
	v2Handle(s, mux, "POST /v2/maintenance/folder-rename/scan", V2FolderRenameScan)
	v2Handle(s, mux, "POST /v2/maintenance/folder-rename/apply", V2FolderRenameApply)
	v2Handle(s, mux, "GET /v2/partial/rename-progress", V2RenameProgress)

	// OAuth / PIN (Trakt, Simkl).
	v2Handle(s, mux, "POST /v2/oauth/start", V2OAuthStart)
	v2Handle(s, mux, "POST /v2/oauth/poll", V2OAuthPoll)

	// Job in background (progresso e annullamento).
	v2Handle(s, mux, "GET /v2/partial/jobs", V2JobsPartial)
	v2Handle(s, mux, "POST /v2/jobs/cancel", V2JobCancel)

	// Aggiunta torrent (magnet/URL o file .torrent).
	v2Handle(s, mux, "POST /v2/downloads/add", V2DownloadsAdd)

	// Traduzioni: modifica per chiave.
	v2Handle(s, mux, "POST /v2/settings/i18n/set", V2SettingsI18nSet)
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
	return view
}

// v2NavGroups mirrors uiNavigation but points the links at /v2.
func v2NavGroups(view string, counts map[string]int) []uiNavGroup {
	groups := make([]uiNavGroup, 0, len(uiNavGroups))
	for index, group := range uiNavGroups {
		out := uiNavGroup{Label: group.Label}
		for _, item := range group.Items {
			out.Items = append(out.Items, uiNavItem{
				ID:           item.ID,
				Label:        item.Label,
				Href:         "/v2?view=" + item.ID,
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
	PanelTables []v2TableData
	Jobs        *v2JobsView
	Search      v2SearchView
}

type v2HealthView struct {
	uiHealthData
	PanelTables []v2TableData
}

type v2DownloadsView struct {
	v2TorrentsView
	History  []v2TableData
	Flash    string
	FlashErr bool
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
		PanelTables:     v2SectionTables(s, r, base.Panels, []string{"dashboard-calendar"}),
		Jobs:            &jobs,
		Search:          v2SearchView{Redirect: "/v2?view=dashboard"},
	}
}

func v2HealthViewFrom(s *AppState, r *http.Request) v2HealthView {
	base := uiHealthDataFrom(s)
	return v2HealthView{
		uiHealthData: base,
		PanelTables:  v2SectionTables(s, r, base.Panels, []string{"health-sources", "health-providers"}),
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
		return "v2_logs", v2LogsViewFrom(s, r.FormValue("filter"), linesNum)
	case "manual":
		return "v2_manual", uiManualDataFrom(s)
	case "license":
		return "v2_license", uiLicenseData{Text: uiLicenseText}
	case "series", "movies", "gaps", "archive", "blocklist", "maintenance", "integrations":
		return "v2_panels_page", v2PanelsViewFrom(s, r, view)
	case "search":
		return "v2_search", v2SearchView{}
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
		view = "dashboard"
	}
	body, content := v2Content(s, r, view)
	cfg := latestConfig(s)
	page := v2ShellData{
		Title:   v2PageLabel(view),
		Page:    view,
		Groups:  v2NavGroups(view, uiNavCounts(s, cfg)),
		Content: content,
		Chrome:  uiShellChromeFrom(s),
		Body:    body,
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_shell", page, dict, eng)
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
		_ = s.i18n.SetLanguage(lang)
	}
	target := r.Header.Get("Referer")
	if target == "" {
		target = "/v2"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// v2DiscardWriter swallows a JSON response when a v2 action reuses an existing
// JSON handler internally.
type v2DiscardWriter struct{ code int }

func (d *v2DiscardWriter) Header() http.Header         { return http.Header{} }
func (d *v2DiscardWriter) Write(b []byte) (int, error) { return len(b), nil }
func (d *v2DiscardWriter) WriteHeader(code int)        { d.code = code }

// V2RunCycle starts a manual cycle by reusing the existing RunNow handler and
// answering 204 (HTMX then reloads the page).
func V2RunCycle(w http.ResponseWriter, r *http.Request, s *AppState) {
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/v2?view=dashboard", http.StatusSeeOther)
		return
	}
	query := r.URL.Query()
	query.Set("domain", strings.TrimSpace(r.FormValue("domain")))
	r.URL.RawQuery = query.Encode()
	RunNow(&v2DiscardWriter{}, r, s)
	w.WriteHeader(http.StatusNoContent)
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
	items, err := s.i18n.List(lang)
	if err != nil {
		return nil
	}
	dict := make(map[string]string, len(items))
	for _, item := range items {
		if item.Key != "" && item.Value != "" {
			dict[item.Key] = item.Value
		}
	}
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
	switch name {
	case "title", "placeholder", "aria-label":
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
	return v2TorrentsView{
		Rows:                rows,
		HTTPDownloads:       data.HTTPDownloads,
		Count:               data.Count,
		UpdatedAt:           data.UpdatedAt,
		Sort:                sortKey,
		Dir:                 dir,
		Filter:              filter,
		Auto:                r.FormValue("auto") == "1",
		Columns:             v2TorrentColumns,
		TagOptions:          uiDownloadTagOptions(s, latestConfig(s)),
		TagFilter:           tagFilter,
		TempDL:              settings.TempDL,
		TempUL:              settings.TempUL,
		TempMinutes:         settings.TempMinutes,
		TempActive:          settings.TempActive,
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
		http.Redirect(w, r, "/v2?view=downloads", http.StatusSeeOther)
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
	Error    string
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
	view := v2DetailView{Hash: hash, Tab: tab}
	view.Tabs = []v2DetailTab{
		{ID: "general", Label: "Generale"}, {ID: "trackers", Label: "Tracker"},
		{ID: "files", Label: "Contenuto"}, {ID: "peers", Label: "Peers"},
		{ID: "limits", Label: "Limiti"}, {ID: "storage", Label: "Storage"},
	}
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
	view.General = []v2KV{
		{Label: "Stato", Value: uiStateLabel(torrent.State)},
		{Label: "Progresso", Value: fmt.Sprintf("%.1f%%", torrent.Progress)},
		{Label: "Dimensione", Value: logging.HumanBytesI64(torrent.TotalSize)},
		{Label: "Scaricato", Value: logging.HumanBytesI64(torrent.TotalDone)},
		{Label: "↓ / ↑", Value: logging.HumanRate(saturatingInt64(torrent.DownloadRate)) + " / " + logging.HumanRate(saturatingInt64(torrent.UploadRate))},
		{Label: "Peer / Seed", Value: fmt.Sprintf("%d / %d", torrent.NumPeers, torrent.NumSeeds)},
		{Label: "Posizione coda", Value: fmt.Sprintf("%d", torrent.QueuePosition)},
		{Label: "Metadata", Value: ternaryString(torrent.HasMetadata, "presenti", "in attesa")},
		{Label: "Versione torrent", Value: torrent.TorrentVersion},
		{Label: "Auto-managed", Value: ternaryString(torrent.AutoManaged, "sì", "no")},
		{Label: "Save path", Value: torrent.SavePath},
		{Label: "Tracker corrente", Value: torrent.CurrentTracker},
		{Label: "Non rinominare", Value: fmt.Sprintf("%t", noRename)},
		{Label: "Magnet", Value: magnet},
	}
	if torrent.Error != "" {
		view.General = append(view.General, v2KV{Label: "Errore", Value: torrent.Error})
	}
	switch tab {
	case "trackers":
		view.Trackers, _, _ = s.activeEngine().Trackers(hash)
	case "files":
		view.Files, _, _ = s.activeEngine().Files(hash)
	case "peers":
		view.Peers, _, _ = s.activeEngine().Peers(hash)
	}
	return view
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
		index := int(v2ParseInt(r.FormValue("index"), -1))
		if index < 0 || index >= len(priorities) {
			http.Error(w, "indice file non valido", http.StatusBadRequest)
			return
		}
		priorities[index] = int32(v2ParseInt(r.FormValue("priority"), 0))
		body, _ = json.Marshal(FilePrioritiesInput{Priorities: priorities})
	default:
		http.Error(w, "azione dettaglio non valida", http.StatusBadRequest)
		return
	}
	raw, status := v2InternalJSON(s, http.MethodPost, path, nil, body)
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/v2?view=downloads", http.StatusSeeOther)
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
	if torrent, ok := v2FindTorrent(s, hash); ok {
		name = torrent.Name
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_remove_modal", map[string]any{"Hash": hash, "Name": name}, dict, eng)
}

// V2DownloadsRemove removes a torrent and swaps the table back in, clearing the
// modal with an out-of-band swap.
func V2DownloadsRemove(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := r.FormValue("hash")
	mode := r.FormValue("mode")
	blocklist := mode == "blocklist" || mode == "files_blocklist"
	deleteFiles := mode == "files" || mode == "files_blocklist"

	if blocklist {
		if meta, err := s.db.TorrentMeta(hash); err == nil && meta != nil {
			_ = s.db.Blocklist(&meta.Release, "manual")
		}
	}
	removed, err := s.activeEngine().Remove(hash, deleteFiles)
	message := "torrent rimosso"
	isErr := false
	if err != nil || !removed {
		message, isErr = "rimozione non riuscita", true
	} else {
		_ = s.db.MarkTorrentRemoved(hash)
		_ = s.db.ForgetRemovedTorrent(hash)
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

type v2SettingGroup struct {
	Title  string
	Hint   string
	Fields []v2Field
}

// v2Fields wraps raw settings fields with the transient save status.
func v2Fields(fields []uiSettingField) []v2Field {
	out := make([]v2Field, 0, len(fields))
	for _, field := range fields {
		out = append(out, v2Field{uiSettingField: field})
	}
	return out
}

type v2SettingsView struct {
	Tabs        []uiSettingsTabRef
	ActiveID    string
	ActiveLabel string
	Groups      []v2SettingGroup
	FieldsGrid  bool
	Note        string
	Empty       string
	Highlight   string
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

func v2SettingsViewFrom(s *AppState, tab, highlight string) v2SettingsView {
	page := uiSettingsPageFrom(s, tab)
	view := v2SettingsView{
		Tabs:       page.Tabs,
		ActiveID:   page.ActiveID,
		FieldsGrid: page.FieldsGrid,
		Highlight:  highlight,
		Empty:      "Nessuna impostazione in questa sezione.",
	}
	for _, ref := range page.Tabs {
		if ref.Active {
			view.ActiveLabel = ref.Label
		}
	}
	for _, group := range page.Groups {
		converted := v2SettingGroup{Title: group.Title, Hint: group.Hint}
		for _, field := range group.Fields {
			converted.Fields = append(converted.Fields, v2Field{uiSettingField: field})
		}
		view.Groups = append(view.Groups, converted)
	}
	view.EditorsFirst = page.EditorsFirst
	if page.ShowSources {
		view.ShowFeeds = true
		view.Feeds = v2FeedsFrom(s)
		view.CheckboxGroups = v2CheckboxGroupsFrom(page.CheckboxGroups)
	}
	for _, editor := range page.ListEditors {
		if key, ok := v2ListEditorKey(editor); ok {
			view.ListEditors = append(view.ListEditors, v2ListEditorViewFrom(s, editor, key, "settings", "advanced"))
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

// V2SettingsBody swaps only the settings body (tab navigation).
func V2SettingsBody(w http.ResponseWriter, r *http.Request, s *AppState) {
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_settings_body", v2SettingsViewFrom(s, r.FormValue("tab"), r.FormValue("highlight")), dict, eng)
}

type v2SettingMatch struct {
	Key, Label, Tab, TabLabel string
}

func v2SettingLabelFor(key string) string {
	for _, def := range uiSettingsIndex {
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
	for _, def := range uiSettingsIndex {
		haystack := strings.ToLower(def.Key + " " + def.Label + " " + uiSettingSearchTerms[def.Key])
		if !strings.Contains(haystack, query) || seen[def.Key] {
			continue
		}
		seen[def.Key] = true
		out.Matches = append(out.Matches, v2SettingMatch{Key: def.Key, Label: def.Label, Tab: def.Tab, TabLabel: labels[def.Tab]})
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
			http.Redirect(w, r, "/v2?view=settings", http.StatusSeeOther)
			return
		}
		v2Render(w, http.StatusOK, "v2_setting_field", v2Field{uiSettingField: field, Status: status, Error: isErr}, dict, eng)
	}

	if !gh7_setting_key_allowed(key) || key == "" || len(key) > 128 {
		render("chiave non modificabile", true)
		return
	}
	if uiSettingIsSecret(key) && strings.TrimSpace(value) == "" {
		render("non modificata", false)
		return
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

func v2LogsViewFrom(s *AppState, filter string, linesNum int) v2LogsView {
	if linesNum <= 0 || linesNum > 5000 {
		linesNum = 500
	}
	raw := coreTailLines(filepath.Join(s.cfg.DataDir, "gextto.log"), linesNum)
	view := v2LogsView{Filter: filter, LinesNum: linesNum}
	needle := strings.ToLower(filter)
	for _, line := range raw {
		if needle != "" && !strings.Contains(strings.ToLower(line), needle) {
			continue
		}
		view.Lines = append(view.Lines, v2HighlightLogLine(line))
	}
	view.Count = len(view.Lines)
	return view
}

// V2LogsPartial re-renders the log view (filter, refresh and periodic poll). It
// also updates the line count out of band, since only the <pre> is swapped.
func V2LogsPartial(w http.ResponseWriter, r *http.Request, s *AppState) {
	linesNum, _ := strconv.Atoi(r.FormValue("lines"))
	view := v2LogsViewFrom(s, r.FormValue("filter"), linesNum)
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
