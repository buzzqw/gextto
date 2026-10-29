package gextto

// uiweb.go is the first concrete slice of the UI migration to Go + server-side
// rendering for the server-side UI migration.
//
// Shared server-side view-models and formatters used by the official UI.
// Dynamic regions and actions reuse the existing JSON APIs, so the API contract
// and all current behaviour are untouched.
//
// The document and its JSON calls are served without an authentication layer;
// the daemon is meant to listen on a trusted interface (default 127.0.0.1).

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "embed"

	"github.com/buzzqw/gextto/internal/amule"
	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

//go:embed LICENSE
var uiLicenseText string

//go:embed THIRD-PARTY-NOTICES
var uiThirdPartyText string

// uiNavItem is one navigation entry of the new shell.
type uiNavItem struct {
	ID           string
	Label        string
	Href         string
	Active       bool
	Optional     bool
	MobileHidden bool
	MobileAlways bool
	Count        string
	// External marks a link that leaves Gextto (the online manual and the
	// changelog): the template opens it in a new tab.
	External bool
}

type uiNavGroup struct {
	Label  string
	Items  []uiNavItem
	Open   bool
	System bool
}

// uiDashboardData is the view-model of the dashboard partial.
type uiDashboardData struct {
	Active  bool
	DryRun  bool
	Backend string
	// EngineLabel and EngineVersion name the torrent engine actually moving the
	// bytes (e.g. "gx-core 1.1.193", "qBittorrent-nox v5.1.1"); both empty with
	// an engine that does not report a version.
	EngineLabel     string
	EngineVersion   string
	Torrents        int
	Downloading     int
	Idle            int
	Seeding         int
	Paused          int
	Errors          int
	DownloadRate    uint64
	UploadRate      uint64
	UpdatedAt       string
	CycleScraped    int
	CycleCandidates int
	CycleStarted    int
	CycleGaps       int
	CycleErrors     int
	// Parity data from the classic dashboard.
	SeriesConfigured int
	MoviesConfigured int
	ArchiveTotal     int64
	FreeSpace        uint64
	TrashBytes       uint64
	SeenGroups       int64
	TrashCount       int64
	NextCycle        string
	CycleInterval    string
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
	Status       string
	Destination  string
	Season       int64
	Episode      int64
	QualityScore int64
	SizeBytes    int64
	DownloadedAt string
}

// uiRecentDestination keeps only the useful destination folder name while
// hiding the local home prefix and the processed file name from the dashboard.
func uiRecentDestination(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	folder := path
	// ProcessedPath is normally a file, but pack/foreign-torrent records can
	// already contain a folder. Only strip the final component when it has a
	// file extension.
	if filepath.Ext(filepath.Base(path)) != "" {
		folder = filepath.Dir(path)
	}
	if home, err := os.UserHomeDir(); err == nil {
		homePrefix := filepath.Clean(home) + string(filepath.Separator)
		if strings.HasPrefix(folder, homePrefix) {
			folder = strings.TrimPrefix(folder, homePrefix)
		}
	}
	if folder == "trasferimento" || strings.HasPrefix(folder, "trasferimento"+string(filepath.Separator)) {
		return "trasferimento"
	}
	return filepath.Base(filepath.Clean(folder))
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
	// SwarmSeeds/SwarmLeechers are the seed and leecher counts the trackers
	// and the DHT report: not necessarily connected, and zero on a swarm
	// nobody is downloading from.
	SwarmSeeds    int
	SwarmLeechers int
	Ratio         float64
	TotalSize     int64
	TotalDone     int64
	Paused        bool
	// Archived/reason/source mirror the "origin" line of the classic UI; they
	// come from torrent_meta and never change the session data.
	Archived bool
	Reason   string
	Source   string
	// SeedInfinite marks a torrent configured to seed without a ratio or time
	// limit, rendered as the "seed ∞" badge.
	SeedInfinite bool
	// Pinned marks the torrent forced out of the queue (the "📌 pin" badge);
	// it is unpinned from its detail drawer.
	Pinned bool
	// Network is "ed2k" for an eD2k file; empty and "torrent" both mean
	// BitTorrent. Sources is the eD2k source count.
	Network string
	Sources int
	// LastResort marks a missing episode taken outside the quality profile
	// (the "🆘 ultima risorsa" badge).
	LastResort bool
}

type uiTorrentsData struct {
	Rows          []uiTorrentRow
	HTTPDownloads []ComicDownload
	// Ed2kDownloads are aMule's files, shown next to the torrents like the
	// HTTP downloads; Ed2kEnabled says whether the aMule integration is on.
	Ed2kDownloads []uiEd2kRow
	Ed2kEnabled   bool
	Count         int
	UpdatedAt     string
}

// uiEd2kRow is one aMule download on the Scarico page.
type uiEd2kRow struct {
	Hash        string
	Name        string
	State       string
	StateClass  string
	ProgressPct string
	Progress    float64
	Done        int64
	Size        int64
	Uploaded    int64
	Speed       uint64
	// Eta is the time left at the current rate, empty when unknown or done;
	// Ratio is uploaded/downloaded, "—" until something moved, and RatioValue
	// is the same number for sorting ("∞" sorts above everything).
	Eta        string
	Ratio      string
	RatioValue float64
	Sources    int64
	Active     int64
	Paused     bool
	Complete   bool
	// LastResort: see uiTorrentRow.
	LastResort bool
}

// uiEd2kRowsLastResort marks the eD2k rows started as a last resort. The
// reason is in torrent_meta, keyed by the eD2k file hash.
func uiEd2kRowsLastResort(db *Database, rows []uiEd2kRow) {
	if db == nil || len(rows) == 0 {
		return
	}
	hashes := make([]string, 0, len(rows))
	for _, row := range rows {
		hashes = append(hashes, row.Hash)
	}
	aux, err := db.TorrentAuxBulk(hashes)
	if err != nil {
		return
	}
	for index := range rows {
		if values, ok := aux[strings.ToLower(rows[index].Hash)]; ok {
			rows[index].LastResort = values[2] == "last_resort"
		}
	}
}

// uiEd2kRows turns aMule's queue into Scarico rows.
func uiEd2kRows(downloads []amule.Download) []uiEd2kRow {
	rows := make([]uiEd2kRow, 0, len(downloads))
	for _, download := range downloads {
		state := ed2kDownloadState(download)
		label, class := "In coda", "warn"
		switch state {
		case "finished":
			label, class = "Completato", "ok"
		case "paused":
			label, class = "In pausa", "warn"
		case "error":
			label, class = "Errore", "err"
		case "checking":
			label, class = "In verifica", ""
		case "downloading":
			label, class = "In scarico", ""
		}
		progress := download.Progress()
		uploaded := max(download.Uploaded, 0)
		ratio := "—"
		ratioValue := 0.0
		if download.Done > 0 {
			ratioValue = float64(uploaded) / float64(download.Done)
			ratio = strconv.FormatFloat(ratioValue, 'f', 2, 64)
		} else if uploaded > 0 {
			ratio = "∞"
			ratioValue = math.Inf(1)
		}
		eta := ""
		if !download.Complete() && download.Speed > 0 && download.Size > download.Done {
			eta = logging.HumanDuration((download.Size - download.Done) / download.Speed)
		}
		rows = append(rows, uiEd2kRow{
			Hash:        download.Hash,
			Name:        download.Name,
			State:       label,
			StateClass:  class,
			Progress:    progress,
			ProgressPct: strconv.FormatFloat(progress, 'f', 1, 64),
			Done:        download.Done,
			Size:        download.Size,
			Uploaded:    uploaded,
			Speed:       uint64(max(download.Speed, 0)),
			Eta:         eta,
			Ratio:       ratio,
			RatioValue:  ratioValue,
			Sources:     download.Sources,
			Active:      download.Active,
			Paused:      download.Paused(),
			Complete:    download.Complete(),
		})
	}
	return rows
}

// uiHealthData is the view-model of the Salute page.
type uiHealthData struct {
	Health        Health
	StatusReason  string
	DiskUsedPct   string
	Uptime        string
	ProcessUptime string
	Panels        []uiPageSection
	// Cycles is the recent search-cycle history (newest first), for the
	// "Ricerche" panel: duration and per-source outcome over time.
	Cycles []uiCycleRow
	// Queued is the delay-profile queue with the wait time of each title.
	Queued []uiQueueRow
	// FailedImports is the failed-import count per day (last week).
	FailedImports []uiDailyRow
}

// uiQueueRow is one title held by the delay profile, as shown in the Salute
// "Coda titoli" panel.
type uiQueueRow struct {
	Title   string
	Label   string
	Waiting string
	Due     string
}

// uiDailyRow is one day of a daily metric (failed imports).
type uiDailyRow struct {
	Day   string
	Count int
}

// uiCycleRow is one search cycle as shown in the Salute "Ricerche" panel.
type uiCycleRow struct {
	When         string
	Duration     string
	Scraped      int
	Downloads    int
	Errors       int
	FailedCount  int
	SourceDetail string
}

// uiLogsData is the view-model of the Log page.
type uiLogsData struct {
	Lines []string
	Count int
}

// uiLicenseData is the view-model of the Licenza page.
type uiLicenseData struct {
	Text string
	// ThirdParty is THIRD-PARTY-NOTICES, the file shipped in every package: the
	// bundled components and the licences of the dependencies, reproduced so
	// the page credits them too.
	ThirdParty string
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
	MobileAlways bool
}

var uiNavGroups = []uiNavDefinition{
	{Label: "Panoramica", Items: []uiNavDefinitionItem{{ID: "dashboard", Label: "Dashboard"}}},
	{Label: "Download", Items: []uiNavDefinitionItem{
		{ID: "downloads", Label: "Scarico"},
		{ID: "logs", Label: "Log", MobileAlways: true},
	}},
	{Label: "Libreria", Items: []uiNavDefinitionItem{
		{ID: "series", Label: "Serie TV"},
		{ID: "movies", Label: "Film"},
	}},
	{Label: "Scoperta", Items: []uiNavDefinitionItem{
		{ID: "search", Label: "Esplora", MobileHidden: true},
		{ID: "archive", Label: "Archivio", MobileHidden: true},
		{ID: "comics", Label: "Fumetti", MobileHidden: true},
		{ID: "ed2k", Label: "eD2k", MobileHidden: true},
	}},
	{Label: "Sistema", Items: []uiNavDefinitionItem{
		{ID: "settings", Label: "Configurazione"},
		{ID: "integrations", Label: "Integrazioni"},
		{ID: "maintenance", Label: "Manutenzione"},
		{ID: "health", Label: "Salute"},
		{ID: "blocklist", Label: "Blocklist", Optional: true},
		{ID: "manual", Label: "Manuale"},
		{ID: "changelog", Label: "Changelog"},
		{ID: "license", Label: "Licenza", Optional: true},
	}},
}

func uiIsSystemPage(view string) bool {
	for _, item := range uiNavGroups[len(uiNavGroups)-1].Items {
		if item.ID == view {
			return true
		}
	}
	return false
}

func uiDashboardDataFrom(s *AppState) uiDashboardData {
	cfg := latestConfig(s)
	engine := s.activeEngine()
	var views []models.TorrentView
	backend := "-"
	if engine != nil {
		views = SessionSnapshot(engine)
		backend = uiBackendLabel(engine.Name())
	}
	data := uiDashboardData{
		Active:    cfg.Active,
		DryRun:    cfg.DryRun,
		Backend:   backend,
		Torrents:  len(views),
		UpdatedAt: uiNowClock(),
	}
	if identity, ok := engine.(TorrentEngineIdentity); ok {
		data.EngineLabel, data.EngineVersion = identity.EngineVersion()
	}
	for _, view := range views {
		switch view.State {
		case "downloading", "downloading_metadata", "stalled":
			// Only a download that is moving data counts as in progress.
			if TorrentTransferring(view) {
				data.Downloading++
			} else {
				data.Idle++
			}
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
	// aMule's downloads count like the torrents: in progress only while data
	// is moving.
	for _, download := range Ed2kDownloads(cfg) {
		switch ed2kDownloadState(download) {
		case "downloading":
			data.Downloading++
		case "queued", "checking":
			data.Idle++
		case "paused":
			data.Paused++
		case "error":
			data.Errors++
		}
	}
	ed2kDown, ed2kUp := v2Ed2kRates(s)
	data.DownloadRate += ed2kDown
	data.UploadRate += ed2kUp
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
	if recent, _, err := s.db.CompletedTorrents(0, 8, ""); err == nil {
		for _, item := range recent {
			completedAt := item.CompletedAt
			if completedAt == "" {
				completedAt = item.UpdatedAt
			}
			data.Recent = append(data.Recent, uiRecentDownload{
				Name:         gh4_historyDisplayName(item.Name, item.Source),
				Kind:         item.Kind,
				Status:       item.Status,
				Destination:  uiRecentDestination(item.ProcessedPath),
				QualityScore: item.QualityScore,
				SizeBytes:    item.TotalSize,
				DownloadedAt: completedAt,
			})
		}
	}
	trash := ""
	if s.cfg.TrashPath != nil {
		trash = *s.cfg.TrashPath
	}
	ramdisk := ""
	if value, ok := s.cfg.Settings["torrent_ramdisk_dir"]; ok {
		ramdisk = value
	}
	health := CheckWithPaths(&HealthPaths{
		DataDir:      s.cfg.DataDir,
		TrashPath:    trash,
		DownloadPath: s.cfg.DownloadDir,
		ArchiveRoot:  gh3DerefString(s.cfg.ArchiveRoot),
		RamdiskPath:  ramdisk,
	})
	data.FreeSpace = health.DiskFreeBytes
	data.TrashBytes = health.TrashBytes
	if cfg.RefreshSecs > 0 {
		data.CycleInterval = logging.HumanDuration(int64(cfg.RefreshSecs))
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

// uiHealthPathsFrom collects the operational paths inspected by the Salute page
// from the live configuration, shared by the full page and the tile fragments.
func uiHealthPathsFrom(s *AppState) *HealthPaths {
	trash := ""
	if s.cfg.TrashPath != nil {
		trash = *s.cfg.TrashPath
	}
	ramdisk := ""
	if value, ok := s.cfg.Settings["torrent_ramdisk_dir"]; ok {
		ramdisk = value
	}
	return &HealthPaths{
		DataDir:      s.cfg.DataDir,
		TrashPath:    trash,
		DownloadPath: s.cfg.DownloadDir,
		ArchiveRoot:  gh3DerefString(s.cfg.ArchiveRoot),
		RamdiskPath:  ramdisk,
	}
}

// uiHealthDataFrom builds the Salute view-model from the same health check used
// by the JSON API, so the two pages cannot diverge.
func uiHealthDataFrom(s *AppState) uiHealthData {
	health := CheckWithPaths(uiHealthPathsFrom(s))
	usedPct := "n/d"
	if health.DiskTotalBytes > 0 {
		used := health.DiskTotalBytes - health.DiskFreeBytes
		usedPct = strconv.FormatFloat(float64(used)/float64(health.DiskTotalBytes)*100, 'f', 1, 64) + "%"
	}
	data := uiHealthData{
		Health:        health,
		StatusReason:  healthStatusReasonIn(health, func(text string) string { return uiText(s, text) }),
		DiskUsedPct:   usedPct,
		Uptime:        logging.HumanDuration(saturatingInt64(health.UptimeSeconds)),
		ProcessUptime: logging.HumanDuration(saturatingInt64(health.ProcessUptimeSeconds)),
		Panels: []uiPageSection{
			sectionTable(uiTableSpec{
				Title:       "Stato sorgenti",
				Endpoint:    "/api/sources/health",
				ItemsKey:    "items",
				ManualOnly:  true,
				Initial:     "Premi Aggiorna per verificare le sorgenti.",
				RefreshHint: "Esegue ora il controllo delle sorgenti configurate. Senza una ricerca verifica la raggiungibilità; con una ricerca controlla anche i risultati. Non modifica la configurazione.",
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
				SearchHint:  "Inserisci una query (per esempio ita 1080p) per controllare se le sorgenti restituiscono risultati; premi Invio per eseguire la verifica.",
				Note:        "Questa è la verifica delle sorgenti. Premi Aggiorna per controllare feed, indexer e motori web; inserisci una ricerca per misurare anche i risultati. Il controllo è manuale e non cambia le impostazioni.",
			}),
			sectionTable(uiTableSpec{
				Title:    "Stato provider",
				Endpoint: "/api/providers/status",
				ItemsKey: "items",
				ColumnsJSON: uiJSON([]uiColumn{
					{Key: "provider", Label: "Provider", Format: "provider_link"},
					{Key: "kind", Label: "Tipo"},
					{Key: "level", Label: "Livello", Format: "number"},
					{Key: "disabled_till", Label: "Disabilitato fino a"},
					{Key: "user_message", Label: "Situazione", Format: "truncate"},
					{Key: "suggested_action", Label: "Cosa fare", Format: "truncate"},
					// The raw error the suggested actions refer to ("consulta il
					// dettaglio tecnico"): kept apart so the message stays a
					// translatable phrase.
					{Key: "last_error", Label: "Dettaglio tecnico", Format: "truncate"},
				}),
				ActionsJSON: uiJSON([]uiAction{
					{Label: "Azzera", Class: "primary", Method: "POST", Path: "/api/providers/status", Body: `{"provider":"{provider}"}`},
				}),
				Empty: "Nessun provider in backoff.",
				Note:  "Backoff crescente sui provider che falliscono; Azzera li riabilita subito.",
			}),
		},
	}
	if cycles, err := s.db.RecentCycleStats(uiHealthCycleRows); err == nil {
		data.Cycles = uiCycleRowsFrom(cycles, time.Now())
	}
	queue := uiHealthQueueFrom(s)
	data.Queued = queue.Queued
	data.FailedImports = queue.FailedImports
	return data
}

// uiHealthQueueFrom builds the only two panels of the Salute page that refresh
// by themselves: the pending titles and the failed imports. It reads them
// without running the health check (trash walk, statfs on every mount, log
// tail), which the "Coda titoli" fragment does not show and would otherwise pay
// every minute for every open page.
func uiHealthQueueFrom(s *AppState) uiHealthData {
	var data uiHealthData
	if queue, err := s.db.PendingQueue(time.Now().UTC()); err == nil {
		data.Queued = uiQueueRowsFrom(queue)
	}
	if failed, err := s.db.FailedImportsDaily(uiHealthFailedImportDays); err == nil {
		data.FailedImports = uiDailyRowsFrom(failed)
	}
	return data
}

// uiHealthCycleRows is how many recent cycles the Salute "Ricerche" panel shows.
const uiHealthCycleRows = 12

// uiHealthFailedImportDays is the window of the "Import falliti" panel.
const uiHealthFailedImportDays = 7

// uiQueueRowsFrom turns the delay-profile queue into display rows.
func uiQueueRowsFrom(items []PendingQueueItem) []uiQueueRow {
	rows := make([]uiQueueRow, 0, len(items))
	for _, it := range items {
		rows = append(rows, uiQueueRow{
			Title:   it.Title,
			Label:   it.Label,
			Waiting: logging.HumanDuration(int64(max(it.Waiting, 0) / time.Second)),
			Due:     logging.HumanDuration(int64(max(it.Due, 0) / time.Second)),
		})
	}
	return rows
}

// uiDailyRowsFrom turns the daily failed-import counts into display rows.
func uiDailyRowsFrom(daily []DailyCount) []uiDailyRow {
	rows := make([]uiDailyRow, 0, len(daily))
	for _, d := range daily {
		rows = append(rows, uiDailyRow{Day: d.Day, Count: d.Count})
	}
	return rows
}

// uiCycleRowsFrom turns the stored cycles into display rows, computing for each
// the number of sources that failed and their last error (for the tooltip).
func uiCycleRowsFrom(cycles []models.CycleHistoryEntry, now time.Time) []uiCycleRow {
	rows := make([]uiCycleRow, 0, len(cycles))
	for _, cycle := range cycles {
		row := uiCycleRow{
			When:      uiCycleWhen(cycle, now),
			Duration:  logging.HumanDuration(int64(cycle.DurationSeconds)),
			Scraped:   cycle.Scraped,
			Downloads: cycle.DownloadsStarted,
			Errors:    cycle.Errors,
		}
		var failed []string
		for _, source := range cycle.Sources {
			if source.Fail == 0 {
				continue
			}
			row.FailedCount++
			detail := source.Name
			if strings.TrimSpace(source.LastError) != "" {
				detail += ": " + source.LastError
			}
			failed = append(failed, detail)
		}
		row.SourceDetail = strings.Join(failed, "; ")
		rows = append(rows, row)
	}
	return rows
}

// uiCycleWhen renders when a stored cycle ran: the wall-clock row time (UTC),
// falling back to the cycle's own start time.
func uiCycleWhen(cycle models.CycleHistoryEntry, now time.Time) string {
	if at := cycleTime(cycle); !at.IsZero() {
		return v2ProblemTime(at, now)
	}
	return "—"
}

// healthStatusReason explains a non-ok health status (or lists the missing
// paths) so the Salute page does not show a bare "degraded".
func healthStatusReason(health Health) string {
	return healthStatusReasonIn(health, func(text string) string { return text })
}

// healthStatusReasonIn builds the reason with each fixed fragment passed
// through tr, so the web UI can show it in the interface language (the whole
// sentence carries path labels and can never match a catalog key).
func healthStatusReasonIn(health Health, tr func(string) string) string {
	problems := []string{}
	if !health.DataDirWritable {
		problems = append(problems, tr("cartella dati non scrivibile"))
	}
	for _, path := range health.Paths {
		if !path.Exists {
			problems = append(problems, path.Label+" "+tr("assente"))
		} else if !path.Writable {
			problems = append(problems, path.Label+" "+tr("non scrivibile"))
		}
	}
	return strings.Join(problems, "; ")
}

// The Salute tiles refreshed every few seconds have their own builders so the
// 5s polling stays lightweight: each one skips the expensive parts of
// CheckWithPaths (disk scan, trash walk, log read) and computes only its own
// value. The full page still uses the complete health check.

// uiHealthStatusTileFrom builds the Stato tile.
func uiHealthStatusTileFrom(s *AppState) uiHealthData {
	paths := uiHealthPathsFrom(s)
	writable := dataDirWritable(paths.DataDir)
	status := "degraded"
	if writable {
		status = "ok"
	}
	health := Health{
		Status:          status,
		DataDirWritable: writable,
		Paths:           path_checks(paths),
	}
	return uiHealthData{Health: health, StatusReason: healthStatusReasonIn(health, func(text string) string { return uiText(s, text) })}
}

// uiHealthMemoryTileFrom builds the Memoria processo tile.
func uiHealthMemoryTileFrom(s *AppState) uiHealthData {
	memoryTotalBytes, _ := memory_info()
	return uiHealthData{Health: Health{
		ResidentBytes:    healthResidentBytes(),
		MemoryTotalBytes: memoryTotalBytes,
	}}
}

// uiHealthUptimeTileFrom builds the Uptime tile.
func uiHealthUptimeTileFrom(s *AppState) uiHealthData {
	processUptime := process_uptime_seconds()
	uptime := healthUptimeSeconds()
	return uiHealthData{
		Health:        Health{ProcessUptimeSeconds: processUptime, UptimeSeconds: uptime},
		Uptime:        logging.HumanDuration(saturatingInt64(uptime)),
		ProcessUptime: logging.HumanDuration(saturatingInt64(processUptime)),
	}
}

// uiHealthDiskTileFrom builds the Disco dati tile on its own so it can refresh
// hourly without re-running the rest of the health check.
func uiHealthDiskTileFrom(s *AppState) uiHealthData {
	totalBytes, freeBytes := disk_space(uiHealthPathsFrom(s).DownloadPath)
	usedPct := "n/d"
	if totalBytes > 0 {
		used := totalBytes - freeBytes
		usedPct = strconv.FormatFloat(float64(used)/float64(totalBytes)*100, 'f', 1, 64) + "%"
	}
	return uiHealthData{
		Health:      Health{DiskTotalBytes: totalBytes, DiskFreeBytes: freeBytes},
		DiskUsedPct: usedPct,
	}
}

// uiLogsDataFrom reads the same log tail the JSON API exposes.
func uiLogsDataFrom(s *AppState) uiLogsData {
	lines := coreTailLines(filepath.Join(s.cfg.DataDir, "gextto.log"), 500)
	return uiLogsData{Lines: lines, Count: len(lines)}
}

// uiTorrentsDataFrom builds the Scarico view-model for both the full page and
// the polling partial.
func uiTorrentsDataFrom(s *AppState) uiTorrentsData {
	rows := uiTorrentRows(s)
	cfg := latestConfig(s)
	ed2kRows := uiEd2kRows(Ed2kDownloads(cfg))
	uiEd2kRowsLastResort(s.db, ed2kRows)
	return uiTorrentsData{
		Rows:          rows,
		HTTPDownloads: HTTPDownloads(),
		Ed2kDownloads: ed2kRows,
		Ed2kEnabled:   ed2kEnabled(cfg),
		Count:         len(rows),
		UpdatedAt:     uiNowClock(),
	}
}

func uiTorrentRows(s *AppState) []uiTorrentRow {
	views := SessionSnapshot(s.activeEngine())
	cfg := latestConfig(s)
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
	comicsDone := s.comics.CompletedTorrents(hashes)
	rows := make([]uiTorrentRow, 0, len(views))
	policy := newSeedPolicy(cfg, s.db)
	for _, view := range views {
		progress := view.Progress
		if progress < 0 {
			progress = 0
		}
		if progress > 100 {
			progress = 100
		}
		ratio := uiTorrentRatio(view.AllTimeUpload, view.AllTimeDownload, view.TotalDone, view.TotalSize)
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
			SwarmSeeds:    view.NumComplete,
			SwarmLeechers: view.NumIncomplete,
			Ratio:         ratio,
			TotalSize:     view.TotalSize,
			TotalDone:     view.TotalDone,
			Paused:        view.State == "paused",
			Pinned:        view.Pinned,
			Network:       view.Network,
			Sources:       view.Sources,
		}
		row.ETA, row.ETASeconds = uiTorrentETA(view)
		row.Tags = uiSplitTags(tag)
		if values, ok := aux[strings.ToLower(view.Hash)]; ok {
			row.Archived = strings.TrimSpace(values[0]) != ""
			row.Source = values[1]
			row.Reason = values[2]
			row.LastResort = row.Reason == "last_resort"
		}
		// A comic is archived by its own pipeline, which torrent_meta does not
		// see: its completion lives in comics_torrents.
		if comicsDone[strings.ToLower(view.Hash)] {
			row.Archived = true
		}
		// A completed torrent kept in the session (automatic removal off) is
		// reported by the engine as "paused" after the seed limit, and as 0 %
		// after a restart re-check once its files went to the library; show
		// the state the user cares about instead.
		if row.Archived && view.State == "paused" {
			row.State = "Completato · archiviato"
			row.StateClass = "ok"
			row.ProgressClass = "seed"
		}
		if row.Archived || row.State == "Completato" || row.State == "Completato · archiviato" {
			row.Progress = 100.0
			row.ProgressPct = "100.0"
			if row.TotalSize > 0 && row.TotalDone < row.TotalSize {
				row.TotalDone = row.TotalSize
			}
		}
		// The badge describes the seed policy of the torrent, not its current
		// state: a torrent set to seed forever shows it from the moment the
		// limits are saved, while it still downloads too.
		row.SeedInfinite = policy.limitsFor(seedViewForPolicy(view)).none()
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

// uiTorrentRatio computes the share ratio. libtorrent reports all_time_download
// as 0 for torrents restored from resume data, so fall back to the bytes
// actually downloaded, then to the total size, instead of showing 0.
func uiTorrentRatio(uploaded, downloaded, totalDone, totalSize int64) float64 {
	if downloaded <= 0 {
		downloaded = totalDone
	}
	if downloaded <= 0 {
		downloaded = totalSize
	}
	if downloaded <= 0 || uploaded <= 0 {
		return 0
	}
	return float64(uploaded) / float64(downloaded)
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

// uiStateLabel mirrors torrent_state_label so users see the
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
	case BackendGxTorrent:
		return "gx-torrent"
	}
	// Unknown or disabled engine (the recovery placeholder): show the raw
	// identifier instead of naming a removed engine.
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return trimmed
	}
	return "—"
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
