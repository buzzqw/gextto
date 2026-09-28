package gextto

// uiweb.go is the first concrete slice of the UI migration to Go + server-side
// rendering for the server-side UI migration.
//
// It is purely additive: the Leptos SPA keeps serving `/`, while the new UI
// lives under `/ui`. The shell is a plain HTML document; the dynamic regions are
// fetched from `/ui/partial/...` endpoints and the actions reuse
// the existing JSON APIs, so the API contract and all current behaviour are
// untouched.
//
// The document and its JSON calls are served without an authentication layer;
// the daemon is meant to listen on a trusted interface (default 127.0.0.1).

import (
	"bytes"
	"embed"
	"html/template"
	"io/fs"
	"math"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

//go:embed uiweb/templates/*.html uiweb/static/*
var uiwebFS embed.FS

//go:embed LICENSE
var uiLicenseText string

//go:embed docs/MANUAL.it.md
var uiManualTextIT string

//go:embed docs/MANUAL.en.md
var uiManualTextEN string

var uiwebTemplates = template.Must(template.New("ui").Funcs(template.FuncMap{
	"humanBytes":  logging.HumanBytesI64,
	"humanBytesU": func(value uint64) string { return logging.HumanBytesI64(saturatingInt64(value)) },
	"humanRate":   func(value uint64) string { return logging.HumanRate(saturatingInt64(value)) },
	"derefUint64": func(value *uint64) uint64 {
		if value == nil {
			return 0
		}
		return *value
	},
	"json":        uiJSON,
	"sourceLabel": uiSourceLabel,
}).ParseFS(uiwebFS, "uiweb/templates/*.html"))

// uiNavItem is one navigation entry of the new shell.
type uiNavItem struct {
	ID           string
	Label        string
	Href         string
	Active       bool
	Optional     bool
	MobileHidden bool
	Count        int
}

type uiNavGroup struct {
	Label  string
	Items  []uiNavItem
	Open   bool
	System bool
}

// uiShellData renders the application frame.
type uiShellData struct {
	Title   string
	Page    string
	Groups  []uiNavGroup
	Content any
	Chrome  uiShellChrome
}

// uiDashboardData is the view-model of the dashboard partial.
type uiDashboardData struct {
	Active            bool
	DryRun            bool
	Backend           string
	LibtorrentVersion string
	Torrents          int
	Downloading       int
	Seeding           int
	Paused            int
	Errors            int
	DownloadRate      uint64
	UploadRate        uint64
	UpdatedAt         string
	CycleScraped      int
	CycleCandidates   int
	CycleStarted      int
	CycleGaps         int
	CycleErrors       int
	// Parity data from the classic dashboard.
	SeriesConfigured int
	MoviesConfigured int
	ArchiveTotal     int64
	FreeSpace        uint64
	TrashBytes       uint64
	SeenGroups       int64
	TrashCount       int64
	NextCycle        string
	Consumption      uiConsumption
	Recent           []uiRecentDownload
	FeedMatches      []uiFeedMatch
	Upcoming         []uiUpcoming
	Panels           []uiPageSection
}

type uiConsumption struct {
	TotalBytes int64
	Last30     int64
	Last7      int64
}

type uiRecentDownload struct {
	Name         string
	Kind         string
	Season       int64
	Episode      int64
	QualityScore int64
	SizeBytes    int64
	DownloadedAt string
}

type uiFeedMatch struct {
	Title  string
	Source string
	Kind   string
}

type uiUpcoming struct {
	Series  string
	Episode string
	AirDate string
}

// uiTorrentRow is one row of the Scarico table.
type uiTorrentRow struct {
	Hash        string
	Name        string
	Tag         string
	Tags        []string
	State       string
	StateClass  string
	Progress    float64
	ProgressPct string
	// ProgressClass drives the progress bar colour (active|seed|paused).
	ProgressClass string
	DownloadRate  uint64
	UploadRate    uint64
	ETA           string
	ETASeconds    int64
	NumPeers      int
	NumSeeds      int
	Ratio         float64
	TotalSize     int64
	TotalDone     int64
	Paused        bool
	// Archived/reason/source mirror the "origin" line of the classic UI; they
	// come from torrent_meta and never change the session data.
	Archived bool
	Reason   string
	Source   string
	// SeedInfinite marks a seeding torrent configured to seed without a ratio
	// or time limit, rendered as the "seed ∞" badge.
	SeedInfinite bool
}

type uiTorrentsData struct {
	Rows          []uiTorrentRow
	HTTPDownloads []ComicDownload
	Count         int
	UpdatedAt     string
}

// uiHealthData is the view-model of the Salute page.
type uiHealthData struct {
	Health        Health
	StatusReason  string
	DiskUsedPct   string
	Uptime        string
	ProcessUptime string
	Panels        []uiPageSection
}

// uiLogsData is the view-model of the Log page.
type uiLogsData struct {
	Lines []string
	Count int
}

// uiMdLine is one line of the minimal manual renderer.
type uiMdLine struct {
	Heading int
	Text    string
}

// uiManualData is the view-model of the Manuale page.
type uiManualData struct {
	Lines []uiMdLine
}

// uiLicenseData is the view-model of the Licenza page.
type uiLicenseData struct {
	Text string
}

// uiNavDefinition mirrors the NAV_GROUPS of the current UI so the two
// interfaces expose exactly the same pages (parity).
type uiNavDefinition struct {
	Label string
	Items []uiNavDefinitionItem
}

type uiNavDefinitionItem struct {
	ID           string
	Label        string
	Optional     bool
	MobileHidden bool
}

var uiNavGroups = []uiNavDefinition{
	{Label: "Panoramica", Items: []uiNavDefinitionItem{{ID: "dashboard", Label: "Dashboard"}}},
	{Label: "Download", Items: []uiNavDefinitionItem{{ID: "downloads", Label: "Scarico"}}},
	{Label: "Libreria", Items: []uiNavDefinitionItem{
		{ID: "series", Label: "Serie TV"},
		{ID: "movies", Label: "Film"},
		{ID: "gaps", Label: "Mancanti", MobileHidden: true},
	}},
	{Label: "Scoperta", Items: []uiNavDefinitionItem{
		{ID: "search", Label: "Esplora", MobileHidden: true},
		{ID: "archive", Label: "Archivio"},
		{ID: "comics", Label: "Fumetti"},
	}},
	{Label: "Sistema", Items: []uiNavDefinitionItem{
		{ID: "settings", Label: "Configurazione"},
		{ID: "integrations", Label: "Integrazioni"},
		{ID: "maintenance", Label: "Manutenzione"},
		{ID: "health", Label: "Salute"},
		{ID: "logs", Label: "Log"},
		{ID: "blocklist", Label: "Blocklist", Optional: true},
		{ID: "manual", Label: "Manuale"},
		{ID: "license", Label: "Licenza", Optional: true},
	}},
}

func uiPageLabel(view string) string {
	for _, group := range uiNavGroups {
		for _, item := range group.Items {
			if item.ID == view {
				return item.Label
			}
		}
	}
	return "Dashboard"
}

func uiIsSystemPage(view string) bool {
	for _, item := range uiNavGroups[len(uiNavGroups)-1].Items {
		if item.ID == view {
			return true
		}
	}
	return false
}

func uiNavigation(view string, counts map[string]int) []uiNavGroup {
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

func uiRender(w http.ResponseWriter, status int, name string, data any) {
	var buffer bytes.Buffer
	if err := uiwebTemplates.ExecuteTemplate(&buffer, name, data); err != nil {
		logging.Error("new UI template render failed", "template", name, "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buffer.Bytes())
}

// UiPage serves the new shell with the requested page rendered server-side, so
// the interface is usable even without JavaScript. The optional client script
// only adds polling and actions.
func UiPage(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := strings.TrimSpace(r.URL.Query().Get("view"))
	if view == "" {
		view = "dashboard"
	}
	content := uiPageContent(s, r, view)
	renderPage := view
	switch content.(type) {
	case uiSeriesDetail:
		renderPage = "series-detail"
	case uiMovieDetail:
		renderPage = "movie-detail"
	case uiPanelsPage:
		renderPage = "panels"
	case uiDownloadsPage:
		renderPage = "downloads"
	}
	cfg := latestConfig(s)
	uiRender(w, http.StatusOK, "shell", uiShellData{
		Title:   uiPageLabel(view),
		Page:    renderPage,
		Groups:  uiNavigation(view, uiNavCounts(s, cfg)),
		Content: content,
		Chrome:  uiShellChromeFrom(s),
	})
}

// uiPageContent builds the view-model of the requested page. Pages that are not
// migrated yet get the placeholder model, which links back to the legacy UI so
// no functionality is ever missing.
func uiPageContent(s *AppState, r *http.Request, view string) any {
	switch view {
	case "dashboard":
		return uiDashboardDataFrom(s)
	case "downloads":
		return uiDownloadsPageFor(s)
	case "health":
		return uiHealthDataFrom(s)
	case "logs":
		return uiLogsDataFrom(s)
	case "manual":
		return uiManualDataFrom(s)
	case "license":
		return uiLicenseData{Text: uiLicenseText}
	case "settings":
		tab := ""
		if r != nil {
			tab = r.URL.Query().Get("tab")
		}
		return uiSettingsPageFrom(s, tab)
	}
	if view == "series" && r != nil {
		if detail, ok := uiSeriesDetailFrom(s, r); ok {
			return detail
		}
	}
	if view == "movies" && r != nil {
		if detail, ok := uiMovieDetailFrom(s, r); ok {
			return detail
		}
	}
	if page, ok := uiPanelsPageFor(view, s); ok {
		page.Groups = uiGroupSections(page.Sections)
		return page
	}
	if spec, ok := uiTableSpecFor(view); ok {
		if spec.Search && r != nil {
			spec.Query = r.URL.Query().Get(spec.SearchParam)
		}
		return spec
	}
	if page, ok := uiSearchPageFor(view); ok {
		return page
	}
	return map[string]any{"Title": uiPageLabel(view)}
}

// UiPartialDashboard renders the dashboard with server-side data.
func UiPartialDashboard(w http.ResponseWriter, r *http.Request, s *AppState) {
	uiRender(w, http.StatusOK, "dashboard", uiDashboardDataFrom(s))
}

// UiPartialTorrents renders the Scarico table with server-side data.
func UiPartialTorrents(w http.ResponseWriter, r *http.Request, s *AppState) {
	uiRender(w, http.StatusOK, "torrents", uiTorrentsDataFrom(s))
}

// UiPartialUnavailable is the honest placeholder for pages not migrated yet:
// it never pretends the feature is gone, it points to the legacy UI.
func UiPartialUnavailable(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := strings.TrimSpace(r.URL.Query().Get("view"))
	uiRender(w, http.StatusOK, "unavailable", map[string]any{"Title": uiPageLabel(view)})
}

func uiDashboardDataFrom(s *AppState) uiDashboardData {
	cfg := latestConfig(s)
	engine := s.activeEngine()
	views := engine.List()
	data := uiDashboardData{
		Active:            cfg.Active,
		DryRun:            cfg.DryRun,
		Backend:           uiBackendLabel(engine.Name()),
		LibtorrentVersion: LibtorrentVersion(),
		Torrents:          len(views),
		UpdatedAt:         uiNowClock(),
	}
	for _, view := range views {
		switch view.State {
		case "downloading", "downloading_metadata", "stalled":
			data.Downloading++
		case "seeding", "finished":
			data.Seeding++
		case "paused", "queued":
			data.Paused++
		case "error":
			data.Errors++
		}
		data.DownloadRate += view.DownloadRate
		data.UploadRate += view.UploadRate
	}
	cycle := s.last_cycle.Snapshot()
	data.CycleScraped = cycle.Scraped
	data.CycleCandidates = cycle.Candidates
	data.CycleStarted = cycle.DownloadsStarted
	data.CycleGaps = cycle.GapsFilled
	data.CycleErrors = cycle.Errors

	// Classic-dashboard data.
	data.SeriesConfigured = len(cfg.Series)
	data.MoviesConfigured = len(cfg.Movies)
	if seenMovies, seenSeries, err := s.db.SeenCounts(); err == nil {
		data.SeenGroups = seenMovies + seenSeries
	}
	if page, err := s.archive.BrowsePage("", 1, 1); err == nil && page != nil {
		data.ArchiveTotal = page.Total
	}
	if stats, err := s.db.ConsumptionStats(); err == nil {
		data.Consumption = uiConsumption{
			TotalBytes: stats.TotalBytes,
			Last30:     stats.Last30DaysBytes,
			Last7:      stats.Last7DaysBytes,
		}
	}
	if recent, err := s.db.RecentDownloads(8); err == nil {
		for _, item := range recent {
			data.Recent = append(data.Recent, uiRecentDownload{
				Name:         item.Name,
				Kind:         item.Kind,
				Season:       uiDerefInt64(item.Season),
				Episode:      uiDerefInt64(item.Episode),
				QualityScore: item.QualityScore,
				SizeBytes:    item.SizeBytes,
				DownloadedAt: item.DownloadedAt,
			})
		}
	}
	trash := ""
	if s.cfg.TrashPath != nil {
		trash = *s.cfg.TrashPath
	}
	ramdisk := ""
	if value, ok := s.cfg.Settings["libtorrent_ramdisk_dir"]; ok {
		ramdisk = value
	}
	health := CheckWithPaths(&HealthPaths{
		DataDir:      s.cfg.DataDir,
		TrashPath:    trash,
		DownloadPath: s.cfg.LibtorrentDir,
		ArchiveRoot:  gh3DerefString(s.cfg.ArchiveRoot),
		RamdiskPath:  ramdisk,
	})
	data.FreeSpace = health.DiskFreeBytes
	data.TrashBytes = health.TrashBytes
	if cfg.RefreshSecs > 0 {
		start, ok := s.db.LastCycleAt()
		if snapshot := s.last_cycle.Snapshot(); snapshot.LastStartedAt != nil && (!ok || snapshot.LastStartedAt.After(start)) {
			start, ok = *snapshot.LastStartedAt, true
		}
		if ok {
			remaining := int64(start.Add(durationFromSeconds(cfg.RefreshSecs)).Sub(time.Now()).Seconds())
			if remaining < 0 {
				remaining = 0
			}
			data.NextCycle = logging.HumanDuration(remaining)
		}
	}
	data.Panels = []uiPageSection{
		sectionTable(uiTableSpec{
			Title:    "Prossime uscite",
			Endpoint: "/api/calendar",
			ItemsKey: "items",
			ColumnsJSON: uiJSON([]uiColumn{
				{Key: "series", Label: "Serie"}, {Key: "episode", Label: "Episodio"},
			}),
			Empty: "Nessuna uscita in programma.",
		}),
	}
	return data
}

// uiHealthDataFrom builds the Salute view-model from the same health check used
// by the JSON API, so the two pages cannot diverge.
func uiHealthDataFrom(s *AppState) uiHealthData {
	trash := ""
	if s.cfg.TrashPath != nil {
		trash = *s.cfg.TrashPath
	}
	ramdisk := ""
	if value, ok := s.cfg.Settings["libtorrent_ramdisk_dir"]; ok {
		ramdisk = value
	}
	health := CheckWithPaths(&HealthPaths{
		DataDir:      s.cfg.DataDir,
		TrashPath:    trash,
		DownloadPath: s.cfg.LibtorrentDir,
		ArchiveRoot:  gh3DerefString(s.cfg.ArchiveRoot),
		RamdiskPath:  ramdisk,
	})
	usedPct := "n/d"
	if health.DiskTotalBytes > 0 {
		used := health.DiskTotalBytes - health.DiskFreeBytes
		usedPct = strconv.FormatFloat(float64(used)/float64(health.DiskTotalBytes)*100, 'f', 1, 64) + "%"
	}
	return uiHealthData{
		Health:        health,
		StatusReason:  healthStatusReason(health),
		DiskUsedPct:   usedPct,
		Uptime:        logging.HumanDuration(saturatingInt64(health.UptimeSeconds)),
		ProcessUptime: logging.HumanDuration(saturatingInt64(health.ProcessUptimeSeconds)),
		Panels: []uiPageSection{
			sectionTable(uiTableSpec{
				Title:    "Stato sorgenti",
				Endpoint: "/api/sources/health",
				ItemsKey: "items",
				ColumnsJSON: uiJSON([]uiColumn{
					{Key: "kind", Label: "Tipo"},
					{Key: "name", Label: "Nome", Format: "truncate"},
					{Key: "results", Label: "Risultati", Format: "number"},
					{Key: "ok", Label: "Esito", Format: "status_badge"},
					{Key: "", Label: "Dettaglio", Format: "source_detail"},
				}),
				Empty:       "Nessuna sorgente da verificare.",
				Search:      true,
				SearchParam: "q",
				Note:        "Premi Aggiorna per verificare tutte le sorgenti; usa la ricerca per provare una query su feed, indexer e motori web.",
			}),
			sectionTable(uiTableSpec{
				Title:    "Stato provider",
				Endpoint: "/api/providers/status",
				ItemsKey: "items",
				ColumnsJSON: uiJSON([]uiColumn{
					{Key: "provider", Label: "Provider"},
					{Key: "kind", Label: "Tipo"},
					{Key: "level", Label: "Livello", Format: "number"},
					{Key: "disabled_till", Label: "Disabilitato fino a"},
					{Key: "last_error", Label: "Ultimo errore", Format: "truncate"},
				}),
				ActionsJSON: uiJSON([]uiAction{
					{Label: "Azzera", Class: "primary", Method: "POST", Path: "/api/providers/status", Body: `{"provider":"{provider}"}`},
				}),
				Empty: "Nessun provider in backoff.",
				Note:  "Backoff crescente sui provider che falliscono; Azzera li riabilita subito.",
			}),
		},
	}
}

// healthStatusReason explains a non-ok health status (or lists the missing
// paths) so the Salute page does not show a bare "degraded".
func healthStatusReason(health Health) string {
	problems := []string{}
	if !health.DataDirWritable {
		problems = append(problems, "cartella dati non scrivibile")
	}
	for _, path := range health.Paths {
		if !path.Exists {
			problems = append(problems, path.Label+" assente")
		} else if !path.Writable {
			problems = append(problems, path.Label+" non scrivibile")
		}
	}
	return strings.Join(problems, "; ")
}

// uiLogsDataFrom reads the same log tail the JSON API exposes.
func uiLogsDataFrom(s *AppState) uiLogsData {
	lines := coreTailLines(filepath.Join(s.cfg.DataDir, "gextto.log"), 500)
	return uiLogsData{Lines: lines, Count: len(lines)}
}

// uiManualDataFrom renders the bundled manual in the language selected by the
// interface, with a minimal, escaped Markdown pass (headings and paragraphs
// only; lists and code stay readable).
func uiManualDataFrom(s *AppState) uiManualData {
	text := uiManualTextIT
	if s != nil {
		if lang, err := s.i18n.Language(); err == nil && strings.EqualFold(strings.TrimSpace(lang), "en") {
			text = uiManualTextEN
		}
	}
	return uiManualData{Lines: uiManualLines(text)}
}

func uiManualLines(text string) []uiMdLine {
	lines := make([]uiMdLine, 0, 256)
	for _, raw := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			level := 0
			for level < len(trimmed) && trimmed[level] == '#' {
				level++
			}
			if level > 6 {
				level = 6
			}
			lines = append(lines, uiMdLine{Heading: level, Text: strings.TrimSpace(trimmed[level:])})
			continue
		}
		lines = append(lines, uiMdLine{Text: trimmed})
	}
	return lines
}

// uiTorrentsDataFrom builds the Scarico view-model for both the full page and
// the polling partial.
func uiTorrentsDataFrom(s *AppState) uiTorrentsData {
	rows := uiTorrentRows(s)
	return uiTorrentsData{
		Rows:          rows,
		HTTPDownloads: HTTPDownloads(),
		Count:         len(rows),
		UpdatedAt:     uiNowClock(),
	}
}

func uiTorrentRows(s *AppState) []uiTorrentRow {
	views := s.activeEngine().List()
	tags := make(map[string]string)
	if pairs, err := s.db.TorrentTags(); err == nil {
		for _, pair := range pairs {
			tags[strings.ToLower(pair[0])] = pair[1]
		}
	}
	hashes := make([]string, 0, len(views))
	for _, view := range views {
		hashes = append(hashes, view.Hash)
	}
	aux, _ := s.db.TorrentAuxBulk(hashes)
	rows := make([]uiTorrentRow, 0, len(views))
	for _, view := range views {
		progress := view.Progress
		if progress < 0 {
			progress = 0
		}
		if progress > 100 {
			progress = 100
		}
		ratio := 0.0
		if view.AllTimeDownload > 0 {
			ratio = float64(view.AllTimeUpload) / float64(view.AllTimeDownload)
		}
		tag := tags[strings.ToLower(view.Hash)]
		row := uiTorrentRow{
			Hash:          view.Hash,
			Name:          view.Name,
			Tag:           tag,
			State:         uiStateLabel(view.State),
			StateClass:    uiStateClass(view.State),
			Progress:      progress,
			ProgressPct:   strconv.FormatFloat(progress, 'f', 1, 64),
			ProgressClass: uiProgressClass(view.State),
			DownloadRate:  view.DownloadRate,
			UploadRate:    view.UploadRate,
			NumPeers:      view.NumPeers,
			NumSeeds:      view.NumSeeds,
			Ratio:         ratio,
			TotalSize:     view.TotalSize,
			TotalDone:     view.TotalDone,
			Paused:        view.State == "paused",
		}
		row.ETA, row.ETASeconds = uiTorrentETA(view)
		row.Tags = uiSplitTags(tag)
		if values, ok := aux[strings.ToLower(view.Hash)]; ok {
			row.Archived = strings.TrimSpace(values[0]) != ""
			row.Source = values[1]
			row.Reason = values[2]
		}
		if view.State == "seeding" || view.State == "finished" {
			row.SeedInfinite = view.SeedRatio == 0 || view.SeedDays == 0
		}
		rows = append(rows, row)
	}
	// Default order in Scarico: alphabetical by name (case-insensitive), with
	// the hash as a stable tie-breaker. The client can still re-sort by any
	// column header.
	sort.SliceStable(rows, func(i, j int) bool {
		left, right := strings.ToLower(rows[i].Name), strings.ToLower(rows[j].Name)
		if left == right {
			return rows[i].Hash < rows[j].Hash
		}
		return left < right
	})
	return rows
}

// uiSplitTags splits a comma separated torrent tag into clean chips.
func uiSplitTags(tag string) []string {
	out := []string{}
	for _, part := range strings.Split(tag, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// uiTorrentETA renders the estimated time to completion like the classic UI:
// an em dash when the torrent is not actively downloading. The numeric value is
// returned too so the table column can be sorted.
func uiTorrentETA(view models.TorrentView) (string, int64) {
	if view.DownloadRate == 0 || view.TotalSize <= view.TotalDone {
		return "—", 0
	}
	remaining := view.TotalSize - view.TotalDone
	seconds := remaining / int64(view.DownloadRate)
	if seconds <= 0 {
		return "—", 0
	}
	return logging.HumanDuration(seconds), seconds
}

// uiStateLabel mirrors torrent_state_label of the legacy UI so users see the
// same words in both interfaces.
func uiStateLabel(state string) string {
	switch state {
	case "checking_files":
		return "Verifica file"
	case "downloading_metadata":
		return "Metadata"
	case "downloading":
		return "In scarico"
	case "finished":
		return "Completato"
	case "seeding":
		return "In seed"
	case "checking_resume_data":
		return "Ripristino"
	case "queued":
		return "In coda"
	case "stalled":
		return "In attesa di seed"
	case "moving":
		return "Spostamento"
	}
	lowered := strings.ToLower(state)
	switch {
	case strings.Contains(lowered, "paus"):
		return "In pausa"
	case strings.Contains(lowered, "coda"):
		return "In coda"
	default:
		return "Sconosciuto"
	}
}

// uiProgressClass selects the progress bar colour, mirroring the classic UI.
func uiProgressClass(state string) string {
	switch state {
	case "seeding", "finished":
		return "seed"
	case "paused", "queued", "stalled", "checking_files", "checking_resume_data":
		return "paused"
	}
	return "active"
}

func uiStateClass(state string) string {
	switch state {
	case "seeding", "finished":
		return "ok"
	case "downloading":
		return "ok"
	case "error":
		return "err"
	case "stalled", "paused", "queued", "moving":
		return "warn"
	default:
		return ""
	}
}

func uiBackendLabel(name string) string {
	switch name {
	case BackendQbittorrent:
		return "qBittorrent-nox"
	case BackendAnacrolix:
		return "anacrolix"
	default:
		return "libtorrent integrato"
	}
}

func uiNowClock() string {
	return time.Now().Format("15:04:05")
}

func saturatingInt64(value uint64) int64 {
	if value > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(value)
}

func uiDerefInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

// uiwebStaticFS exposes the new UI static assets (/ui/static/*).
func uiwebStaticFS() fs.FS {
	sub, err := fs.Sub(uiwebFS, "uiweb/static")
	if err != nil {
		return nil
	}
	return sub
}
