package gextto

// uiweb.go is the first concrete slice of the UI migration to Go + server-side
// rendering (see docs/migrazione-ui.md and docs/migrazione-ui-inventario.md).
//
// It is purely additive: the Leptos SPA keeps serving `/`, while the new UI
// lives under `/ui`. The shell is a plain HTML document; the dynamic regions are
// fetched from authenticated `/ui/partial/...` endpoints and the actions reuse
// the existing JSON APIs, so the API contract and all current behaviour are
// untouched.
//
// Security model (unchanged): the document is public like the legacy shell, and
// every data/action request carries the API token in `X-Gextto-Token`, which the
// small client script reads from `localStorage["gextto_api_token"]`.

import (
	"bytes"
	"embed"
	"html/template"
	"io/fs"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

//go:embed uiweb/templates/*.html uiweb/static/*
var uiwebFS embed.FS

var uiwebTemplates = template.Must(template.New("ui").Funcs(template.FuncMap{
	"humanBytes": logging.HumanBytesI64,
	"humanRate":  func(value uint64) string { return logging.HumanRate(saturatingInt64(value)) },
}).ParseFS(uiwebFS, "uiweb/templates/*.html"))

// uiNavItem is one navigation entry of the new shell.
type uiNavItem struct {
	ID           string
	Label        string
	Href         string
	Active       bool
	Optional     bool
	MobileHidden bool
}

type uiNavGroup struct {
	Label string
	Items []uiNavItem
	Open  bool
}

// uiShellData renders the application frame.
type uiShellData struct {
	Title  string
	Page   string
	Groups []uiNavGroup
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
}

// uiTorrentRow is one row of the Scarico table.
type uiTorrentRow struct {
	Hash         string
	Name         string
	State        string
	StateClass   string
	Progress     float64
	ProgressPct  string
	DownloadRate uint64
	UploadRate   uint64
	NumPeers     int
	NumSeeds     int
	Ratio        float64
	TotalSize    int64
	TotalDone    int64
	Paused       bool
}

type uiTorrentsData struct {
	Rows      []uiTorrentRow
	Count     int
	UpdatedAt string
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

func uiNavigation(view string) []uiNavGroup {
	groups := make([]uiNavGroup, 0, len(uiNavGroups))
	for index, group := range uiNavGroups {
		out := uiNavGroup{Label: group.Label}
		for _, item := range group.Items {
			out.Items = append(out.Items, uiNavItem{
				ID:           item.ID,
				Label:        item.Label,
				Href:         "/ui?view=" + item.ID,
				Active:       item.ID == view,
				Optional:     item.Optional,
				MobileHidden: item.MobileHidden,
			})
		}
		if index == len(uiNavGroups)-1 {
			out.Open = uiIsSystemPage(view)
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

// UiPage serves the new shell. The dynamic region is filled by the client from
// the authenticated partials, exactly like the legacy SPA loads its data.
func UiPage(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := strings.TrimSpace(r.URL.Query().Get("view"))
	if view == "" {
		view = "dashboard"
	}
	uiRender(w, http.StatusOK, "shell", uiShellData{
		Title:  uiPageLabel(view),
		Page:   view,
		Groups: uiNavigation(view),
	})
}

// UiPartialDashboard renders the dashboard with server-side data.
func UiPartialDashboard(w http.ResponseWriter, r *http.Request, s *AppState) {
	uiRender(w, http.StatusOK, "dashboard", uiDashboardDataFrom(s))
}

// UiPartialTorrents renders the Scarico table with server-side data.
func UiPartialTorrents(w http.ResponseWriter, r *http.Request, s *AppState) {
	rows := uiTorrentRows(s)
	uiRender(w, http.StatusOK, "torrents", uiTorrentsData{
		Rows:      rows,
		Count:     len(rows),
		UpdatedAt: uiNowClock(),
	})
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
	return data
}

func uiTorrentRows(s *AppState) []uiTorrentRow {
	views := s.activeEngine().List()
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
		rows = append(rows, uiTorrentRow{
			Hash:         view.Hash,
			Name:         view.Name,
			State:        uiStateLabel(view.State),
			StateClass:   uiStateClass(view.State),
			Progress:     progress,
			ProgressPct:  strconv.FormatFloat(progress, 'f', 1, 64),
			DownloadRate: view.DownloadRate,
			UploadRate:   view.UploadRate,
			NumPeers:     view.NumPeers,
			NumSeeds:     view.NumSeeds,
			Ratio:        ratio,
			TotalSize:    view.TotalSize,
			TotalDone:    view.TotalDone,
			Paused:       view.State == "paused",
		})
	}
	return rows
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

// uiwebStaticFS exposes the new UI static assets (/ui/static/*).
func uiwebStaticFS() fs.FS {
	sub, err := fs.Sub(uiwebFS, "uiweb/static")
	if err != nil {
		return nil
	}
	return sub
}
