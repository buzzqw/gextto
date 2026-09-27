package tui

import (
	"sort"
	"strings"
)

// Tab identifies the active view.
type Tab int

const (
	TabStatus Tab = iota
	TabTorrents
	TabLogs
	TabHealth
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
)

// Action is a request emitted by Update and executed against the daemon.
type Action struct {
	Kind        ActionKind
	Domain      string
	Text        string
	Hash        string
	DeleteFiles bool
	DL, UL      int64
	Release     map[string]any
	DetailKind  string
}

// prompt holds the active input buffer.
type prompt struct {
	Kind   PromptKind
	Buffer string
}

// confirm holds a pending yes/no question.
type confirm struct {
	MessageKey string
	Args       []any
	Action     Action
}

// Line is one rendered row with a semantic style.
type Line struct {
	Text  string
	Style Style
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
}

// Event is one torrent event (alias kept for readability in the model).
type modelEvent = Event

// Model is the pure TUI state. It has no terminal or network dependencies, so
// every transition can be tested deterministically.
type Model struct {
	Tr *Translator

	Tab      Tab
	Selected int
	Filter   string
	Sort     SortMode
	SortDesc bool

	Status    *Status
	Health    *Health
	HealthErr string

	Torrents []Torrent

	Logs               []string
	LogFilter          string
	LogFollow          bool
	LogScroll          int
	LogStreamConnected bool

	Detail       *TorrentDetail
	DetailView   DetailView
	DetailItems  []map[string]any
	DetailScroll int

	Overlay Overlay
	Events  []Event

	SearchResults  []map[string]any
	SearchQuery    string
	SearchSelected int

	Message string
	Err     string
	Loading bool

	Prompt  *prompt
	Confirm *confirm
}

// NewModel builds an empty model with the given translator.
func NewModel(tr *Translator) *Model {
	if tr == nil {
		tr = NewTranslator(LangIT)
	}
	return &Model{
		Tr:         tr,
		LogFollow:  true,
		DetailView: DetailGeneral,
	}
}

// --- data setters ---------------------------------------------------------

// SetStatus replaces the daemon summary.
func (m *Model) SetStatus(status Status) { m.Status = &status }

// SetTorrents replaces the torrent list.
func (m *Model) SetTorrents(torrents []Torrent) {
	m.Torrents = torrents
	if m.Selected >= len(m.visibleTorrents()) {
		m.Selected = max(0, len(m.visibleTorrents())-1)
	}
}

// SetHealth replaces the health report.
func (m *Model) SetHealth(health Health) { m.Health = &health; m.HealthErr = "" }

// SetHealthError records a health fetch failure.
func (m *Model) SetHealthError(message string) { m.HealthErr = message }

// SetLogs replaces the whole log buffer (SSE snapshot fallback).
func (m *Model) SetLogs(lines []string) { m.Logs = append([]string(nil), lines...) }

// AppendLog appends a single log line, keeping the last 500.
func (m *Model) AppendLog(line string) {
	m.Logs = append(m.Logs, line)
	if len(m.Logs) > 500 {
		m.Logs = m.Logs[len(m.Logs)-500:]
	}
}

// SetStreamConnected records the SSE connection state.
func (m *Model) SetStreamConnected(connected bool) { m.LogStreamConnected = connected }

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
}

// SetEvents stores the recent torrent events.
func (m *Model) SetEvents(events []Event) { m.Events = events }

// SetSearchResults opens the search overlay with results.
func (m *Model) SetSearchResults(query string, results []map[string]any) {
	m.SearchQuery = query
	m.SearchResults = results
	m.SearchSelected = 0
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

func (m *Model) visibleTorrents() []Torrent { return m.VisibleTorrents() }

// SelectedTorrent returns the highlighted torrent, if any.
func (m *Model) SelectedTorrent() *Torrent {
	items := m.VisibleTorrents()
	if m.Selected < 0 || m.Selected >= len(items) {
		return nil
	}
	return &items[m.Selected]
}

// FilteredLogs returns the log lines matching the active filter.
func (m *Model) FilteredLogs() []string {
	if m.LogFilter == "" {
		return m.Logs
	}
	needle := strings.ToLower(m.LogFilter)
	filtered := make([]string, 0, len(m.Logs))
	for _, line := range m.Logs {
		if strings.Contains(strings.ToLower(line), needle) {
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
