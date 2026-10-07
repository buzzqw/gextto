package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Tab identifies the active view.
type Tab int

const (
	TabStatus Tab = iota
	TabTorrents
	TabLogs
	TabHealth
	TabArchive
	TabMissing
	TabBlocklist
	TabLibrary
	TabMaintenance
)

// LibraryKind selects the compact monitored-library view.
type LibraryKind int

const (
	LibrarySeries LibraryKind = iota
	LibraryMovies
	LibraryComics
)

// SortMode selects the torrent list ordering.
type SortMode int

const (
	SortName SortMode = iota
	SortProgress
	SortState
	SortRate
	SortSize
)

// ArchiveSortMode selects the archive list ordering.
type ArchiveSortMode int

const (
	ArchiveSortTitle ArchiveSortMode = iota
	ArchiveSortSource
	ArchiveSortQuality
	ArchiveSortAdded
)

// DetailView is the sub-view of the torrent details overlay.
type DetailView int

const (
	DetailGeneral DetailView = iota
	DetailTrackers
	DetailFiles
	DetailPeers
)

// Overlay identifies a modal panel.
type Overlay int

const (
	OverlayNone Overlay = iota
	OverlayHelp
	OverlaySearch
	OverlayEvents
	OverlaySettings
	OverlayHistory
	OverlayMaintenance
)

// PromptKind identifies the active single-line input.
type PromptKind int

const (
	PromptNone PromptKind = iota
	PromptCycle
	PromptSearch
	PromptMagnet
	PromptFile
	PromptLimits
	PromptLogFilter
	PromptTorrentFilter
	PromptArchiveFilter
	PromptLanguage
	PromptRefresh
	PromptTempLimits
	PromptTmdbSeries
	PromptTmdbMovie
	PromptFormField
	PromptTag
	PromptTorrentLimits
	PromptMoveStorage
	PromptAddTracker
	PromptHistoryFilter
	PromptFolderRename
	PromptRamdisk
)

// ActionKind identifies a side effect the runner must perform.
type ActionKind int

const (
	ActionNone ActionKind = iota
	ActionQuit
	ActionRefresh
	ActionRunCycle
	ActionSearch
	ActionAddMagnet
	ActionAddFile
	ActionPauseToggle
	ActionRestart
	ActionRemove
	ActionRecheck
	ActionReannounce
	ActionNoRenameToggle
	ActionPin
	ActionUnpin
	ActionCleanCompleted
	ActionSetLimits
	ActionCleanTrash
	ActionQueueRelease
	ActionOpenDetails
	ActionLoadDetail
	ActionLoadEvents
	ActionLoadArchive
	ActionLoadMissing
	ActionLoadBlocklist
	ActionRemoveBlocklist
	ActionCopy
	ActionLoadConfig
	ActionSaveSetting
	ActionSetLanguage
	ActionHTTPPauseToggle
	ActionHTTPRemove
	ActionSetTempLimits
	ActionClearTempLimits
	ActionLoadSeries
	ActionLoadMovie
	ActionSaveSeries
	ActionDeleteSeries
	ActionSaveMovie
	ActionDeleteMovie
	ActionAddToLibrary
	ActionTmdbSearch
	ActionToggleSeason
	ActionSeriesSearchMissing
	ActionSeriesMetadata
	ActionRenamePreview
	ActionRenameExecute
	ActionEpisodeSources
	ActionEpisodeSearch
	ActionEpisodeIgnore
	ActionEpisodeRedownload
	ActionMovieSearch
	ActionMovieRedownload
	ActionBulk
	ActionSetTag
	ActionToggleAutoRemove
	ActionLoadHistory
	ActionTorrentLimits
	ActionMoveStorage
	ActionMarkFailed
	ActionSuperSeeding
	ActionFilePriorities
	ActionSetTrackers
	ActionCancelJob
	ActionMaintenance
	ActionFolderRenameApply
)

// Action is a request emitted by Update and executed against the daemon.
type Action struct {
	Kind        ActionKind
	Domain      string
	Text        string
	Hash        string
	HTTPID      string
	DeleteFiles bool
	DL, UL      int64
	Release     map[string]any
	DetailKind  string
	Page        int
	Minutes     int64
	// Library actions: Text is the series name, ID the movie id.
	ID              int64
	Season, Episode int64
	Flag            bool
	Fields          map[string]any
	Movie           *MovieConfig
	// Torrent actions.
	Hashes     []string
	Priorities []int32
	Trackers   []TrackerEntry
	Ratio      *float64
	Days       *int64
}

// TUIConfig contains the small set of daemon settings editable from the TUI.
type TUIConfig struct {
	Active           bool
	DryRun           bool
	RefreshSecs      uint64
	DefaultLanguage  string
	DownloadLimitKib int64
	UploadLimitKib   int64
}

// TransferSample is one in-memory point used by the status sparkline.
type TransferSample struct {
	Download float64
	Upload   float64
	At       time.Time
}

// prompt holds the active input buffer.
type prompt struct {
	Kind   PromptKind
	Buffer string
	Cursor int
}

func newPrompt(kind PromptKind, buffer string) *prompt {
	return &prompt{Kind: kind, Buffer: buffer, Cursor: len([]rune(buffer))}
}

// confirm holds a pending yes/no question.
type confirm struct {
	MessageKey string
	Args       []any
	Action     Action
}

// Line is one rendered row with a semantic style. A Wrap line is broken
// into several rows when it is wider than the terminal instead of being cut;
// continuation rows start Indent cells in.
type Line struct {
	Text   string
	Style  Style
	Wrap   bool
	Indent int
}

// Style is a semantic text style mapped to ANSI by the terminal renderer.
type Style int

const (
	StyleNormal Style = iota
	StyleMuted
	StyleHeader
	StyleOK
	StyleWarn
	StyleErr
	StyleSelected
)

// Screen is a rendered frame.
type Screen struct {
	Lines         []Line
	CursorRow     int
	CursorCol     int
	CursorVisible bool
	ColorsEnabled bool
	HighContrast  bool
}

// Event is one torrent event (alias kept for readability in the model).

// Model is the pure TUI state. It has no terminal or network dependencies, so
// every transition can be tested deterministically.
type Model struct {
	Tr *Translator

	Tab               Tab
	Selected          int
	Filter            string
	Sort              SortMode
	SortDesc          bool
	TorrentScroll     int
	ArchiveFilter     string
	ArchiveSelected   int
	ArchiveScroll     int
	ArchivePage       int
	ArchiveSort       ArchiveSortMode
	ArchiveSortDesc   bool
	MissingSelected   int
	MissingScroll     int
	BlocklistSelected int
	BlocklistScroll   int
	// PageScroll scrolls the free-form Status and Health pages, which can be
	// taller than a small SSH window once long values wrap.
	PageScroll int
	// HelpScroll is the first visible line of the help overlay.
	HelpScroll int

	Status    *Status
	Health    *Health
	HealthErr string

	Metrics           *DashboardStats
	DaemonKnown       bool
	DaemonConnected   bool
	DaemonReconnects  int
	Config            *TUIConfig
	SettingsSelected  int
	ColorsEnabled     bool
	HighContrast      bool
	TransferHistory   []TransferSample
	Notification      string
	NotificationUntil time.Time
	HTTPStates        map[string]string

	Torrents      []Torrent
	HTTPDownloads []ComicDownload
	// Marked torrents (by hash) for the bulk actions.
	Marked map[string]bool
	// TorrentTags maps hashes to download tags; TagCatalog lists the tags.
	TorrentTags     map[string]string
	TagCatalog      []string
	History         []HistoryItem
	HistoryTotal    int
	HistoryFilter   string
	HistorySelected int
	HistoryScroll   int
	// SpeedPolicy is the global limit in force; SpeedPolicyAt is when it was
	// fetched, so the temporary limit's countdown runs between polls.
	SpeedPolicy   *SpeedPolicy
	SpeedPolicyAt time.Time
	Series        []SeriesLibraryItem
	Movies        []MovieConfig
	Comics        []ComicLibraryItem
	Library       LibraryKind
	SeriesView    *SeriesView
	MovieView     *MovieView
	// PendingEdit opens the series edit form once the detail is loaded.
	PendingEdit     bool
	LibrarySelected int
	LibraryScroll   int
	LibraryFilter   string
	Archive         []ArchiveEntry
	ArchiveTotal    int
	ArchivePages    int
	Missing         []Gap
	Blocklist       []BlocklistEntry

	Logs      []string
	LogFilter string
	// LogProblemsOnly keeps only WARN and ERROR lines in the log view.
	LogProblemsOnly bool
	// UnseenProblems counts WARN/ERROR lines that arrived while the Log tab
	// was not open; the header shows them until the log is viewed.
	UnseenProblems     int
	LogFollow          bool
	LogScroll          int
	LogStreamConnected bool
	StreamReconnect    int

	Detail       *TorrentDetail
	DetailView   DetailView
	DetailItems  []map[string]any
	DetailScroll int
	// DetailSelected is the highlighted tracker or file.
	DetailSelected int

	ArchiveDetail       *ArchiveEntry
	ArchiveDetailScroll int

	Overlay          Overlay
	Events           []Event
	EventScroll      int
	EventStreamReady bool

	SearchKind     SearchKind
	SearchResults  []map[string]any
	SearchQuery    string
	SearchSelected int
	SearchScroll   int

	Message string
	Err     string
	Loading bool
	// Bandwidth is the footer meter of the TUI's own traffic.
	Bandwidth string

	Prompt  *prompt
	Confirm *confirm
	Form    *Form

	Maintenance   *MaintenanceData
	MaintSelected int
	MaintScroll   int
	Report        *Report
	lastFolder    string
}

// NewModel builds an empty model with the given translator.
func NewModel(tr *Translator) *Model {
	if tr == nil {
		tr = NewTranslator(LangIT)
	}
	return &Model{
		Tr:            tr,
		LogFollow:     true,
		DetailView:    DetailGeneral,
		ArchivePage:   1,
		ColorsEnabled: true,
		HTTPStates:    make(map[string]string),
	}
}

// --- data setters ---------------------------------------------------------

// SetStatus replaces the daemon summary.
func (m *Model) SetStatus(status Status) { m.Status = &status }

// SetTorrents replaces the torrent list.
func (m *Model) SetTorrents(torrents []Torrent) {
	selectedHash := ""
	if selected := m.SelectedDownload(); selected.Torrent != nil {
		selectedHash = selected.Torrent.Hash
	}
	m.Torrents = torrents
	m.pruneMarks()
	visible := m.VisibleDownloads()
	if selectedHash != "" {
		for index, row := range visible {
			if row.Torrent != nil && row.Torrent.Hash == selectedHash {
				m.Selected = index
				return
			}
		}
	}
	if m.Selected >= len(visible) {
		m.Selected = max(0, len(visible)-1)
	}
}

// SetHTTPDownloads replaces the live HTTP download list and emits notifications
// only for transitions observed after the first snapshot.
func (m *Model) SetHTTPDownloads(downloads []ComicDownload) {
	selectedID := ""
	if selected := m.SelectedDownload(); selected.HTTP != nil {
		selectedID = selected.HTTP.ID
	}
	first := len(m.HTTPStates) == 0 && len(m.HTTPDownloads) == 0
	current := make(map[string]string, len(downloads))
	for _, download := range downloads {
		current[download.ID] = download.Status
		if !first && m.HTTPStates[download.ID] != download.Status {
			name := firstNonEmpty(download.Title, download.ID)
			switch strings.ToLower(download.Status) {
			case "completed", "error", "stalled":
				m.SetNotification(m.Tr.Format("msg.notifyhttp", name, m.Tr.StateLabel(download.Status)))
			}
		}
	}
	m.HTTPStates = current
	m.HTTPDownloads = append([]ComicDownload(nil), downloads...)
	if selectedID != "" {
		for index, item := range m.VisibleDownloads() {
			if item.HTTP != nil && item.HTTP.ID == selectedID {
				m.Selected = index
				return
			}
		}
	}
	m.Selected = min(m.Selected, max(0, len(m.VisibleDownloads())-1))
}

func (m *Model) SetLibrary(items any) {
	switch value := items.(type) {
	case []SeriesLibraryItem:
		m.Series = append([]SeriesLibraryItem(nil), value...)
	case []MovieConfig:
		m.Movies = append([]MovieConfig(nil), value...)
	case []MovieLibraryItem:
		m.Movies = make([]MovieConfig, 0, len(value))
		for _, item := range value {
			m.Movies = append(m.Movies, MovieConfig{ID: item.ID, Name: item.Name, Year: item.Year, Quality: item.Quality, Language: item.Language, LanguageRequirements: item.LanguageRequirements, Enabled: item.Enabled})
		}
	case []ComicLibraryItem:
		m.Comics = append([]ComicLibraryItem(nil), value...)
	}
	m.LibrarySelected = min(m.LibrarySelected, max(0, len(m.VisibleLibraryRows())-1))
}

// SetHealth replaces the health report.
func (m *Model) SetHealth(health Health) { m.Health = &health; m.HealthErr = "" }

// SetHealthError records a health fetch failure.
func (m *Model) SetHealthError(message string) { m.HealthErr = message }

// SetMetrics replaces aggregate transfer and consumption statistics and keeps
// a short in-memory history for the status sparkline.
func (m *Model) SetMetrics(stats DashboardStats) {
	m.Metrics = &stats
	m.sampleTransfer()
}

// sampleTransfer appends one point to the status trend sparkline.
func (m *Model) sampleTransfer() {
	download, hasDownload, upload, hasUpload := m.transferRates()
	download += m.httpDownloadRate()
	if download > 0 {
		hasDownload = true
	}
	if !hasDownload && !hasUpload {
		return
	}
	m.TransferHistory = append(m.TransferHistory, TransferSample{Download: download, Upload: upload, At: time.Now()})
	if len(m.TransferHistory) > 60 {
		m.TransferHistory = m.TransferHistory[len(m.TransferHistory)-60:]
	}
}

// transferRates returns the aggregate torrent rates. The integrated backend
// does not report them in /api/stats, so they fall back to the sum over the
// torrent list (which the Status tab polls anyway).
func (m *Model) transferRates() (download float64, hasDownload bool, upload float64, hasUpload bool) {
	if m.Metrics != nil {
		download, hasDownload = metricNumber(m.Metrics.TorrentStats, "dl_info_speed", "download_rate", "download_speed")
		upload, hasUpload = metricNumber(m.Metrics.TorrentStats, "up_info_speed", "upload_rate", "upload_speed")
	}
	if !hasDownload && !hasUpload && len(m.Torrents) > 0 {
		for _, torrent := range m.Torrents {
			download += float64(torrent.DownloadRate)
			upload += float64(torrent.UploadRate)
		}
		hasDownload, hasUpload = true, true
	}
	return download, hasDownload, upload, hasUpload
}

func (m *Model) httpDownloadRate() float64 {
	rate := 0.0
	for _, item := range m.HTTPDownloads {
		rate += float64(item.SpeedBytes)
	}
	return rate
}

// SetSpeedPolicy replaces the global speed limits in force.
func (m *Model) SetSpeedPolicy(policy SpeedPolicy) {
	m.SpeedPolicy = &policy
	m.SpeedPolicyAt = time.Now()
}

// TempLimitMinutes returns the minutes left on the temporary limit, rounded
// up, and false when it is not active. Zero minutes with true means it stays
// until removed.
func (m *Model) TempLimitMinutes() (int64, bool) {
	policy := m.SpeedPolicy
	if policy == nil || !policy.TempActive {
		return 0, false
	}
	if policy.TempRemainingSec <= 0 {
		return 0, true
	}
	left := policy.TempRemainingSec - int64(time.Since(m.SpeedPolicyAt).Seconds())
	if left <= 0 {
		// Expired since the last poll: the daemon restores the normal limits.
		return 0, false
	}
	return (left + 59) / 60, true
}

// SetConfig replaces the daemon settings shown by the TUI settings panel.
func (m *Model) SetConfig(config TUIConfig) { m.Config = &config }

// SetNotification displays a transient, non-modal notification.
func (m *Model) SetNotification(message string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	m.Notification = message
	m.NotificationUntil = time.Now().Add(8 * time.Second)
}

// ObserveEvent turns relevant torrent lifecycle events into notifications.
func (m *Model) ObserveEvent(event Event) {
	kind := strings.ToLower(strings.TrimSpace(event.Kind))
	name := firstNonEmpty(event.Name, event.Hash, m.Tr.T("label.torrents"))
	switch {
	case strings.Contains(kind, "finished"):
		m.SetNotification(m.Tr.Format("msg.notifyfinished", name))
	case strings.Contains(kind, "stalled"):
		m.SetNotification(m.Tr.Format("msg.notifystalled", name))
	case strings.Contains(kind, "archiv"):
		m.SetNotification(m.Tr.Format("msg.notifyarchived", name))
	case strings.Contains(kind, "error") || strings.Contains(kind, "failed"):
		message := firstNonEmpty(event.Message, m.Tr.T("msg.unknownerror"))
		m.SetNotification(m.Tr.Format("msg.notifyerror", name, message))
	}
}

// SetDaemonState records reachability of the daemon API.
func (m *Model) SetDaemonState(connected bool, message string) {
	wasKnown, wasConnected := m.DaemonKnown, m.DaemonConnected
	m.DaemonKnown = true
	m.DaemonConnected = connected
	if connected {
		m.DaemonReconnects = 0
		m.Err = ""
		if wasKnown && !wasConnected {
			m.SetNotification(m.Tr.T("msg.daemononline"))
		}
		return
	}
	m.DaemonReconnects++
	if message != "" {
		m.Err = message
	}
	if wasKnown && wasConnected {
		m.SetNotification(m.Tr.T("msg.daemonoffline"))
	}
}

// SetLogs replaces the whole log buffer (SSE snapshot fallback).
func (m *Model) SetLogs(lines []string) {
	previous := m.Logs
	m.Logs = append([]string(nil), lines...)
	delta := logAppendDelta(previous, m.Logs)
	if !m.LogFollow {
		m.LogScroll += delta
	}
	// The first snapshot is history, not news.
	if len(previous) > 0 {
		for _, line := range m.Logs[max(0, len(m.Logs)-delta):] {
			m.noteProblem(line)
		}
	}
}

// noteProblem counts a WARN/ERROR line the user has not seen yet.
func (m *Model) noteProblem(line string) {
	if m.Tab != TabLogs && logStyle(line) != StyleNormal {
		m.UnseenProblems++
	}
}

// logAppendDelta estimates how many new entries a polling/SSE snapshot added.
// It handles both an ordinary append and the rolling 500-line buffer.
func logAppendDelta(previous, current []string) int {
	if len(previous) == 0 {
		return len(current)
	}
	if len(current) >= len(previous) {
		matches := true
		for index := range previous {
			if previous[index] != current[index] {
				matches = false
				break
			}
		}
		if matches {
			return len(current) - len(previous)
		}
	}
	maxOverlap := min(len(previous), len(current))
	for overlap := maxOverlap; overlap > 0; overlap-- {
		matches := true
		for index := 0; index < overlap; index++ {
			if previous[len(previous)-overlap+index] != current[index] {
				matches = false
				break
			}
		}
		if matches {
			return len(current) - overlap
		}
	}
	return 0
}

// AppendLog appends a single log line, keeping the last 500.
func (m *Model) AppendLog(line string) {
	m.Logs = append(m.Logs, line)
	m.noteProblem(line)
	if !m.LogFollow {
		// LogScroll is measured from the bottom. Keep the currently visible
		// rows fixed while new entries arrive in the background.
		m.LogScroll++
	}
	if len(m.Logs) > 500 {
		m.Logs = m.Logs[len(m.Logs)-500:]
	}
}

// SetStreamConnected records the SSE connection state.
func (m *Model) SetStreamConnected(connected bool) { m.LogStreamConnected = connected }

// SetStreamReconnect records the number of consecutive SSE reconnects.
func (m *Model) SetStreamReconnect(attempt int) { m.StreamReconnect = max(0, attempt) }

// SetDetail stores the open torrent details.
func (m *Model) SetDetail(detail TorrentDetail) {
	m.Detail = &detail
	m.DetailView = DetailGeneral
	m.DetailItems = nil
	m.DetailScroll = 0
}

// SetDetailItems stores trackers, files or peers.
func (m *Model) SetDetailItems(items []map[string]any) {
	m.DetailItems = items
	m.DetailScroll = 0
	m.DetailSelected = min(m.DetailSelected, max(0, len(items)-1))
}

// SetArchiveDetail opens the selected archive entry details.
func (m *Model) SetArchiveDetail(entry ArchiveEntry) {
	copy := entry
	m.ArchiveDetail = &copy
	m.ArchiveDetailScroll = 0
}

// SetEvents stores the recent torrent events.
func (m *Model) SetEvents(events []Event) {
	m.Events = events
	m.EventScroll = 0
}

// SetSearchResults opens the search overlay with results.
func (m *Model) SetSearchResults(query string, results []map[string]any) {
	m.SetPickerResults(SearchReleases, query, results)
}

// SetPickerResults opens the search overlay on releases or TMDB entries.
func (m *Model) SetPickerResults(kind SearchKind, query string, results []map[string]any) {
	m.SearchKind = kind
	m.SearchQuery = query
	m.SearchResults = results
	m.SearchSelected = 0
	m.SearchScroll = 0
	m.Overlay = OverlaySearch
}

// SetError records a transport error.
func (m *Model) SetError(message string) { m.Err = message }

// SetMessage records a status message.
func (m *Model) SetMessage(message string) { m.Message = message }

// --- queries --------------------------------------------------------------

// VisibleTorrents returns the filtered and sorted torrent list.
func (m *Model) VisibleTorrents() []Torrent {
	items := m.Torrents
	if filter := strings.ToLower(strings.TrimSpace(m.Filter)); filter != "" {
		filtered := make([]Torrent, 0, len(items))
		for _, torrent := range items {
			if strings.Contains(strings.ToLower(torrent.Name), filter) ||
				strings.Contains(strings.ToLower(torrent.Hash), filter) ||
				strings.Contains(strings.ToLower(torrent.State), filter) {
				filtered = append(filtered, torrent)
			}
		}
		items = filtered
	}
	sorted := append([]Torrent(nil), items...)
	switch m.Sort {
	case SortProgress:
		sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Progress > sorted[j].Progress })
	case SortState:
		sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].State < sorted[j].State })
	case SortRate:
		sort.SliceStable(sorted, func(i, j int) bool {
			return sorted[i].DownloadRate+sorted[i].UploadRate > sorted[j].DownloadRate+sorted[j].UploadRate
		})
	case SortSize:
		sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].TotalSize > sorted[j].TotalSize })
	default:
		sort.SliceStable(sorted, func(i, j int) bool {
			return strings.ToLower(sorted[i].Name) < strings.ToLower(sorted[j].Name)
		})
	}
	if m.SortDesc {
		for i, j := 0, len(sorted)-1; i < j; i, j = i+1, j-1 {
			sorted[i], sorted[j] = sorted[j], sorted[i]
		}
	}
	return sorted
}

// VisibleArchive returns the current archive page in the selected order.
func (m *Model) VisibleArchive() []ArchiveEntry {
	items := append([]ArchiveEntry(nil), m.Archive...)
	sort.SliceStable(items, func(i, j int) bool {
		cmp := 0
		switch m.ArchiveSort {
		case ArchiveSortSource:
			cmp = strings.Compare(strings.ToLower(items[i].Source), strings.ToLower(items[j].Source))
		case ArchiveSortQuality:
			if items[i].QualityScore < items[j].QualityScore {
				cmp = -1
			} else if items[i].QualityScore > items[j].QualityScore {
				cmp = 1
			}
		case ArchiveSortAdded:
			cmp = strings.Compare(items[i].AddedAt, items[j].AddedAt)
		default:
			cmp = strings.Compare(strings.ToLower(items[i].Title), strings.ToLower(items[j].Title))
		}
		if m.ArchiveSortDesc {
			return cmp > 0
		}
		return cmp < 0
	})
	return items
}

func (m *Model) visibleTorrents() []Torrent { return m.VisibleTorrents() }

// SelectedTorrent returns the highlighted torrent, if any.
func (m *Model) SelectedTorrent() *Torrent {
	item := m.SelectedDownload()
	if item.Torrent == nil {
		return nil
	}
	copy := *item.Torrent
	return &copy
}

// DownloadRow is a normalized row in the unified torrent/HTTP list.
type DownloadRow struct {
	Torrent *Torrent
	HTTP    *ComicDownload
}

func (m *Model) VisibleDownloads() []DownloadRow {
	rows := make([]DownloadRow, 0, len(m.Torrents)+len(m.HTTPDownloads))
	for index := range m.Torrents {
		item := m.Torrents[index]
		rows = append(rows, DownloadRow{Torrent: &item})
	}
	for index := range m.HTTPDownloads {
		item := m.HTTPDownloads[index]
		rows = append(rows, DownloadRow{HTTP: &item})
	}
	if filter := strings.ToLower(strings.TrimSpace(m.Filter)); filter != "" {
		filtered := rows[:0]
		for _, row := range rows {
			name, state, id := downloadRowFields(row)
			tag := ""
			if row.Torrent != nil {
				tag = m.TorrentTags[row.Torrent.Hash]
			}
			if strings.Contains(strings.ToLower(name), filter) || strings.Contains(strings.ToLower(state), filter) || strings.Contains(strings.ToLower(id), filter) || strings.Contains(strings.ToLower(tag), filter) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	sort.SliceStable(rows, func(i, j int) bool {
		left, right := rows[i], rows[j]
		if m.SortDesc {
			left, right = right, left
		}
		leftName, leftState, _ := downloadRowFields(left)
		rightName, rightState, _ := downloadRowFields(right)
		var less bool
		switch m.Sort {
		case SortProgress:
			less = downloadRowProgress(left) > downloadRowProgress(right)
		case SortState:
			less = leftState < rightState
		case SortRate:
			less = downloadRowRate(left) > downloadRowRate(right)
		case SortSize:
			less = downloadRowSize(left) > downloadRowSize(right)
		default:
			less = strings.ToLower(leftName) < strings.ToLower(rightName)
		}
		return less
	})
	return rows
}

func downloadRowFields(row DownloadRow) (name, state, id string) {
	if row.Torrent != nil {
		return Sanitize(row.Torrent.Name), row.Torrent.State, row.Torrent.Hash
	}
	if row.HTTP != nil {
		return Sanitize(row.HTTP.Title), row.HTTP.Status, row.HTTP.ID
	}
	return "", "", ""
}
func downloadRowProgress(row DownloadRow) float64 {
	if row.Torrent != nil {
		return row.Torrent.Progress
	}
	if row.HTTP != nil {
		return row.HTTP.Progress
	}
	return 0
}
func downloadRowRate(row DownloadRow) float64 {
	if row.Torrent != nil {
		return float64(row.Torrent.DownloadRate + row.Torrent.UploadRate)
	}
	if row.HTTP != nil {
		return float64(row.HTTP.SpeedBytes)
	}
	return 0
}
func downloadRowSize(row DownloadRow) uint64 {
	if row.Torrent != nil {
		return row.Torrent.TotalSize
	}
	if row.HTTP != nil && row.HTTP.TotalBytes != nil {
		return *row.HTTP.TotalBytes
	}
	return 0
}

func (m *Model) SelectedDownload() DownloadRow {
	items := m.VisibleDownloads()
	if m.Selected < 0 || m.Selected >= len(items) {
		return DownloadRow{}
	}
	return items[m.Selected]
}

// LibraryRow is the normalized display row for one monitored title. Key
// identifies it across reloads (series name, "movie:<id>", "comic:<id>").
type LibraryRow struct {
	Key     string
	Name    string
	Meta    string
	Enabled bool
}

func (m *Model) VisibleLibraryRows() []LibraryRow {
	rows := []LibraryRow{}
	filter := strings.ToLower(strings.TrimSpace(m.LibraryFilter))
	add := func(row LibraryRow) {
		if filter == "" || strings.Contains(strings.ToLower(row.Name), filter) || strings.Contains(strings.ToLower(row.Meta), filter) {
			rows = append(rows, row)
		}
	}
	switch m.Library {
	case LibraryMovies:
		for _, item := range m.Movies {
			add(LibraryRow{Key: movieKey(item.ID), Name: item.Name, Meta: joinNonEmpty(item.Year, item.Quality, item.Language), Enabled: item.Enabled})
		}
	case LibraryComics:
		for _, item := range m.Comics {
			add(LibraryRow{Key: fmt.Sprintf("comic:%d", item.ID), Name: item.Title, Meta: joinNonEmpty(item.Publisher, item.LatestDownloadedTitle), Enabled: item.Enabled})
		}
	default:
		for _, item := range m.Series {
			episodes := ""
			if item.EpisodesTotal > 0 {
				episodes = m.Tr.Format("library.episodes", item.EpisodesDownloaded, item.EpisodesTotal)
			}
			last := ""
			if item.LastDownloadedAt != nil && len(*item.LastDownloadedAt) >= 10 {
				last = m.Tr.Format("library.last", (*item.LastDownloadedAt)[:10])
			}
			add(LibraryRow{Key: item.Name, Name: item.Name, Meta: joinNonEmpty(episodes, item.Seasons, item.Quality, item.Language, last), Enabled: item.Enabled})
		}
	}
	return rows
}

// joinNonEmpty joins the non-blank values with " · ".
func joinNonEmpty(values ...string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, " · ")
}

// FilteredLogs returns the log lines matching the active filter.
func (m *Model) FilteredLogs() []string {
	if m.LogFilter == "" && !m.LogProblemsOnly {
		return m.Logs
	}
	needle := strings.ToLower(m.LogFilter)
	filtered := make([]string, 0, len(m.Logs))
	for _, line := range m.Logs {
		if m.LogProblemsOnly && logStyle(line) == StyleNormal {
			continue
		}
		if needle == "" || strings.Contains(strings.ToLower(line), needle) {
			filtered = append(filtered, line)
		}
	}
	return filtered
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
