package tui

import (
	"strings"
	"testing"
)

func runeKey(value rune) Key    { return Key{Kind: KeyRune, Rune: value} }
func kindKey(value KeyKind) Key { return Key{Kind: value} }

func typeText(m *Model, text string) {
	for _, char := range text {
		m.Update(runeKey(char))
	}
}

func sampleTorrents() []Torrent {
	return []Torrent{
		{Hash: "bbbb", Name: "Bravo", State: "downloading", Progress: 10, TotalSize: 2048, TotalDone: 1024, DownloadRate: 100},
		{Hash: "aaaa", Name: "Alfa", State: "seeding", Progress: 100, TotalSize: 4096, TotalDone: 4096, UploadRate: 50},
		{Hash: "cccc", Name: "Charlie", State: "paused", Progress: 50},
	}
}

func TestTabsAndGlobalKeys(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	if action := m.Update(runeKey('2')); action.Kind != ActionRefresh || m.Tab != TabTorrents {
		t.Fatalf("digit 2 should switch to Torrents: tab=%v action=%+v", m.Tab, action)
	}
	m.Update(kindKey(KeyTab))
	if m.Tab != TabLogs {
		t.Fatalf("Tab should cycle to Logs, got %v", m.Tab)
	}
	m.Update(kindKey(KeyBackTab))
	if m.Tab != TabTorrents {
		t.Fatalf("BackTab should cycle back, got %v", m.Tab)
	}
	if action := m.Update(runeKey('q')); action.Kind != ActionQuit {
		t.Fatalf("q should quit, got %+v", action)
	}
	if action := m.Update(kindKey(KeyCtrlC)); action.Kind != ActionQuit {
		t.Fatalf("Ctrl-C should quit, got %+v", action)
	}
	if action := m.Update(runeKey('r')); action.Kind != ActionRefresh {
		t.Fatalf("r should refresh, got %+v", action)
	}
	if action := m.Update(runeKey('5')); action.Kind != ActionLoadArchive || m.Tab != TabArchive {
		t.Fatalf("5 should open archive and load it, tab=%v action=%+v", m.Tab, action)
	}
	if action := m.Update(runeKey('l')); action.Kind != ActionRefresh || m.Tab != TabLibrary {
		t.Fatalf("l should open Library: tab=%v action=%+v", m.Tab, action)
	}
}

func TestHelpOverlay(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Update(runeKey('?'))
	if m.Overlay != OverlayHelp {
		t.Fatal("? should open help")
	}
	m.Update(kindKey(KeyEsc))
	if m.Overlay != OverlayNone {
		t.Fatal("Esc should close help")
	}
}

func TestTorrentNavigationAndFilter(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabTorrents
	m.SetTorrents(sampleTorrents())
	// Sorted by name: Alfa, Bravo, Charlie.
	if got := m.VisibleTorrents()[0].Name; got != "Alfa" {
		t.Fatalf("expected Alfa first, got %s", got)
	}
	m.Update(kindKey(KeyDown))
	if m.Selected != 1 {
		t.Fatalf("down should select index 1, got %d", m.Selected)
	}
	m.Update(kindKey(KeyEnd))
	if m.Selected != 2 {
		t.Fatalf("End should select last, got %d", m.Selected)
	}
	m.Update(kindKey(KeyHome))
	if m.Selected != 0 {
		t.Fatalf("Home should select first, got %d", m.Selected)
	}
	// Filter prompt.
	m.Update(runeKey('F'))
	if m.Prompt == nil || m.Prompt.Kind != PromptTorrentFilter {
		t.Fatal("F should open the torrent filter prompt")
	}
	typeText(m, "char")
	m.Update(kindKey(KeyEnter))
	if m.Filter != "char" {
		t.Fatalf("filter = %q", m.Filter)
	}
	if items := m.VisibleTorrents(); len(items) != 1 || items[0].Name != "Charlie" {
		t.Fatalf("filtered torrents = %+v", items)
	}
}

func TestSortCycle(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabTorrents
	m.SetTorrents(sampleTorrents())
	m.Update(runeKey('o'))
	if m.Sort != SortProgress {
		t.Fatalf("o should switch to progress sort, got %v", m.Sort)
	}
	if got := m.VisibleTorrents()[0].Name; got != "Alfa" {
		t.Fatalf("progress sort should put 100%% first, got %s", got)
	}
	m.Update(runeKey('O'))
	if !m.SortDesc {
		t.Fatal("O should toggle descending")
	}
	if got := m.VisibleTorrents()[0].Name; got != "Bravo" {
		t.Fatalf("ascending progress should put 10%% first, got %s", got)
	}
}

func TestTorrentActions(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabTorrents
	m.SetTorrents(sampleTorrents())
	m.Update(kindKey(KeyHome)) // Alfa

	if action := m.Update(runeKey('p')); action.Kind != ActionPauseToggle || action.Hash != "aaaa" {
		t.Fatalf("p should pause Alfa, got %+v", action)
	}
	if action := m.Update(runeKey('b')); action.Kind != ActionRestart || action.Hash != "aaaa" {
		t.Fatalf("b should restart, got %+v", action)
	}
	if action := m.Update(runeKey('k')); action.Kind != ActionRecheck {
		t.Fatalf("k should recheck, got %+v", action)
	}
	if action := m.Update(runeKey('R')); action.Kind != ActionReannounce {
		t.Fatalf("R should reannounce, got %+v", action)
	}
	if action := m.Update(runeKey('n')); action.Kind != ActionNoRenameToggle {
		t.Fatalf("n should toggle no-rename, got %+v", action)
	}
	if action := m.Update(runeKey('i')); action.Kind != ActionPin {
		t.Fatalf("i should pin, got %+v", action)
	}
	if action := m.Update(runeKey('u')); action.Kind != ActionUnpin {
		t.Fatalf("u should unpin, got %+v", action)
	}
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionOpenDetails || action.Hash != "aaaa" {
		t.Fatalf("Enter should open details, got %+v", action)
	}
}

func TestRemoveConfirmation(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabTorrents
	m.SetTorrents(sampleTorrents())
	m.Update(kindKey(KeyHome))

	m.Update(runeKey('d'))
	if m.Confirm == nil || m.Confirm.Action.Kind != ActionRemove {
		t.Fatalf("d should ask for confirmation, got %+v", m.Confirm)
	}
	if action := m.Update(runeKey('n')); action.Kind != ActionNone {
		t.Fatalf("n should cancel, got %+v", action)
	}
	if m.Confirm != nil {
		t.Fatal("confirmation should be cleared after n")
	}
	m.Update(runeKey('d'))
	if action := m.Update(runeKey('y')); action.Kind != ActionRemove || action.DeleteFiles {
		t.Fatalf("y should remove without files, got %+v", action)
	}
	m.Update(runeKey('D'))
	if action := m.Update(runeKey('s')); action.Kind != ActionRemove || !action.DeleteFiles {
		t.Fatalf("D+s should remove with files, got %+v", action)
	}
}

func TestLimitsAndCyclePrompts(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabTorrents
	m.SetTorrents(sampleTorrents())

	m.Update(runeKey('L'))
	typeText(m, "100 50")
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionSetLimits || action.DL != 100 || action.UL != 50 {
		t.Fatalf("limits action = %+v", action)
	}
	// Invalid limits keep the prompt closed but set a message.
	m.Update(runeKey('L'))
	typeText(m, "10 x")
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionNone {
		t.Fatalf("invalid limits should not emit an action, got %+v", action)
	}
	if m.Message != m.Tr.T("msg.limitsinvalid") {
		t.Fatalf("message = %q", m.Message)
	}
	// Cycle prompt.
	m.Update(runeKey('c'))
	if action := m.Update(runeKey('s')); action.Kind != ActionRunCycle || action.Domain != "series" {
		t.Fatalf("cycle action = %+v", action)
	}
	m.Update(runeKey('c'))
	typeText(m, "bogus")
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionNone || m.Message != m.Tr.T("msg.cycleinvalid") {
		t.Fatalf("invalid cycle should not emit, got %+v message=%q", action, m.Message)
	}
}

func TestMagnetPromptValidation(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Update(runeKey('a'))
	typeText(m, "not-a-magnet")
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionNone || m.Message != m.Tr.T("msg.magnetinvalid") {
		t.Fatalf("invalid magnet: %+v %q", action, m.Message)
	}
	m.Update(runeKey('a'))
	typeText(m, "magnet:?xt=urn:btih:abc")
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionAddMagnet || action.Text != "magnet:?xt=urn:btih:abc" {
		t.Fatalf("magnet action = %+v", action)
	}
	// Esc cancels.
	m.Update(runeKey('t'))
	typeText(m, "/tmp/x.torrent")
	m.Update(kindKey(KeyEsc))
	if m.Prompt != nil {
		t.Fatal("Esc should cancel the prompt")
	}
}

func TestPromptEditing(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Update(runeKey('s'))
	typeText(m, "abcd")
	m.Update(kindKey(KeyLeft))
	m.Update(kindKey(KeyLeft))
	m.Update(runeKey('X'))
	if got := m.Prompt.Buffer; got != "abXcd" {
		t.Fatalf("middle insertion = %q", got)
	}
	m.Update(kindKey(KeyEnd))
	m.Update(kindKey(KeyCtrlW))
	if got := m.Prompt.Buffer; got != "" {
		t.Fatalf("Ctrl-W should remove the previous word, got %q", got)
	}
}

func TestSearchOverlay(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Update(runeKey('s'))
	typeText(m, "query")
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionSearch || action.Text != "query" {
		t.Fatalf("search action = %+v", action)
	}
	m.SetSearchResults("query", []map[string]any{
		{"title": "One", "source": "idx", "quality": map[string]any{"resolution": "1080p"}},
		{"title": "Two", "source": "web"},
	})
	if m.Overlay != OverlaySearch {
		t.Fatal("search results should open the overlay")
	}
	m.Update(kindKey(KeyDown))
	if m.SearchSelected != 1 {
		t.Fatalf("down should select 1, got %d", m.SearchSelected)
	}
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionQueueRelease || action.Release["title"] != "Two" {
		t.Fatalf("Enter should queue the selected release, got %+v", action)
	}
	m.Update(kindKey(KeyEsc))
	if m.Overlay != OverlayNone {
		t.Fatal("Esc should close the search overlay")
	}
}

func TestDetailViews(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabTorrents
	m.SetTorrents(sampleTorrents())
	m.Update(kindKey(KeyHome))
	m.Update(kindKey(KeyEnter))
	m.SetDetail(TorrentDetail{NoRename: true, Torrent: Torrent{Hash: "aaaa", Name: "Alfa", State: "seeding"}})
	if m.Detail == nil {
		t.Fatal("detail should be set")
	}
	if action := m.Update(runeKey('2')); action.Kind != ActionLoadDetail || action.DetailKind != "trackers" {
		t.Fatalf("2 should load trackers, got %+v", action)
	}
	m.SetDetailItems([]map[string]any{{"tier": 1, "url": "udp://tracker"}})
	m.Update(runeKey('3'))
	if m.DetailView != DetailFiles {
		t.Fatalf("3 should switch to files, got %v", m.DetailView)
	}
	m.Update(kindKey(KeyEsc))
	if m.Detail != nil {
		t.Fatal("Esc should close the details")
	}
}

func TestLogsFilterAndFollow(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabLogs
	m.SetLogs([]string{"INFO one", "ERROR two", "INFO three"})
	m.Update(runeKey('/'))
	typeText(m, "ERROR")
	m.Update(kindKey(KeyEnter))
	if m.LogFilter != "ERROR" {
		t.Fatalf("log filter = %q", m.LogFilter)
	}
	if lines := m.FilteredLogs(); len(lines) != 1 || !strings.Contains(lines[0], "ERROR") {
		t.Fatalf("filtered logs = %+v", lines)
	}
	if !m.LogFollow {
		t.Fatal("filter reset should follow again")
	}
	m.Update(runeKey('f'))
	if m.LogFollow {
		t.Fatal("f should stop following")
	}
}

func TestLogsWithoutFollowKeepTheirViewport(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabLogs
	m.SetLogs([]string{"one", "two", "three", "four", "five"})
	m.LogFollow = false
	m.LogScroll = 2
	m.AppendLog("six")
	if m.LogScroll != 3 {
		t.Fatalf("new log line should not move a paused viewport: scroll=%d", m.LogScroll)
	}
}

func TestLogSnapshotKeepsPausedViewportAcrossRollingBuffer(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.LogFollow = false
	m.Logs = []string{"two", "three", "four"}
	m.LogScroll = 2
	m.SetLogs([]string{"three", "four", "five"})
	if m.LogScroll != 3 {
		t.Fatalf("rolling snapshot should advance paused scroll, got %d", m.LogScroll)
	}
}

func TestSetTorrentsKeepsSelectedHash(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabTorrents
	m.SetTorrents(sampleTorrents())
	m.Update(kindKey(KeyDown)) // Bravo after name sorting.
	m.SetTorrents([]Torrent{
		{Hash: "cccc", Name: "Charlie"},
		{Hash: "aaaa", Name: "Alfa"},
		{Hash: "bbbb", Name: "Bravo", Progress: 20},
	})
	if selected := m.SelectedTorrent(); selected == nil || selected.Hash != "bbbb" {
		t.Fatalf("refresh should preserve selected torrent, got %+v", selected)
	}
}

func TestRenderErrorKeepsTheCurrentView(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabTorrents
	m.SetTorrents(sampleTorrents())
	m.SetError("connection refused")
	screen := m.Render(100, 30)
	if !lineContains(screen, "Bravo") || !lineContains(screen, "connection refused") {
		t.Fatalf("transport error should not hide current data: %+v", firstLines(screen, 8))
	}
}

func TestHealthCleanTrashConfirm(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Update(runeKey('4'))
	if m.Tab != TabHealth {
		t.Fatalf("4 should switch to health, got %v", m.Tab)
	}
	m.Update(runeKey('x'))
	if m.Confirm == nil || m.Confirm.Action.Kind != ActionCleanTrash {
		t.Fatalf("x should ask confirmation, got %+v", m.Confirm)
	}
	if action := m.Update(runeKey('y')); action.Kind != ActionCleanTrash {
		t.Fatalf("y should clean trash, got %+v", action)
	}
}

func TestArchiveFilterAndQueue(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabArchive
	m.Archive = []ArchiveEntry{{Title: "Example", Magnet: "magnet:?xt=urn:btih:x", Source: "archive"}}
	m.Update(runeKey('s'))
	if m.Prompt == nil || m.Prompt.Kind != PromptArchiveFilter {
		t.Fatalf("archive s should open the archive filter, got %+v", m.Prompt)
	}
	m.Prompt = nil
	m.Update(runeKey('/'))
	typeText(m, "Example")
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionLoadArchive || action.Text != "Example" {
		t.Fatalf("archive filter action = %+v", action)
	}
	m.Prompt = nil
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionAddMagnet || action.Text == "" {
		t.Fatalf("archive Enter should queue the selected magnet, got %+v", action)
	}
}

func TestArchivePaginationSortingAndDetails(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabArchive
	m.ArchivePage = 1
	m.ArchivePages = 3
	m.Archive = []ArchiveEntry{
		{ID: 1, Title: "Zeta", Source: "b", QualityScore: 100, AddedAt: "2024-01-01", Magnet: "magnet:z"},
		{ID: 2, Title: "Alfa", Source: "a", QualityScore: 900, AddedAt: "2025-01-01", Magnet: "magnet:a"},
	}
	if action := m.Update(kindKey(KeyPgDn)); action.Kind != ActionLoadArchive || action.Page != 2 {
		t.Fatalf("PgDn should load archive page 2, got %+v", action)
	}
	m.ArchivePage = 2
	if action := m.Update(kindKey(KeyPgUp)); action.Kind != ActionLoadArchive || action.Page != 1 {
		t.Fatalf("PgUp should load archive page 1, got %+v", action)
	}
	m.ArchiveSort = ArchiveSortTitle
	if got := m.VisibleArchive()[0].Title; got != "Alfa" {
		t.Fatalf("title sort = %q, want Alfa", got)
	}
	m.ArchiveSort = ArchiveSortQuality
	if got := m.VisibleArchive()[0].QualityScore; got != 100 {
		t.Fatalf("quality sort = %d, want 100", got)
	}
	if action := m.Update(runeKey('d')); action.Kind != ActionNone || m.ArchiveDetail == nil {
		t.Fatalf("d should open archive details: action=%+v detail=%+v", action, m.ArchiveDetail)
	}
	if action := m.Update(runeKey('a')); action.Kind != ActionAddMagnet || action.Text != "magnet:z" {
		t.Fatalf("a should queue archive detail magnet, got %+v", action)
	}
	m.Update(kindKey(KeyEsc))
	if m.ArchiveDetail != nil {
		t.Fatal("Esc should close archive details")
	}
}

func TestBlocklistRemoveConfirmation(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabBlocklist
	m.Blocklist = []BlocklistEntry{{Hash: "abc", Title: "Blocked release"}}
	if action := m.Update(runeKey('d')); action.Kind != ActionNone || m.Confirm == nil {
		t.Fatalf("blocklist d should ask for confirmation: action=%+v confirm=%+v", action, m.Confirm)
	}
	if action := m.Update(runeKey('s')); action.Kind != ActionRemoveBlocklist || action.Hash != "abc" {
		t.Fatalf("confirmation should remove selected entry, got %+v", action)
	}
}

func TestRenderLocalizedTabs(t *testing.T) {
	it := NewModel(NewTranslator("it"))
	screen := it.Render(120, 30)
	if !lineContains(screen, "[1:Stato]") || !lineContains(screen, "2:Torrent") {
		t.Fatalf("Italian tabs missing: %+v", firstLines(screen, 3))
	}
	en := NewModel(NewTranslator("en"))
	screen = en.Render(120, 30)
	if !lineContains(screen, "[1:Status]") || !lineContains(screen, "2:Torrents") {
		t.Fatalf("English tabs missing: %+v", firstLines(screen, 3))
	}
}

func TestRenderTorrentsWithSelection(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabTorrents
	m.SetTorrents(sampleTorrents())
	m.Update(kindKey(KeyDown))
	screen := m.Render(120, 30)
	if len(screen.Lines) != 30 {
		t.Fatalf("expected 30 lines, got %d", len(screen.Lines))
	}
	selected := 0
	for _, line := range screen.Lines {
		if strings.HasPrefix(line.Text, "> ") {
			selected++
		}
	}
	if selected != 1 {
		t.Fatalf("expected exactly one selected row, got %d", selected)
	}
	if !lineContains(screen, "Bravo") {
		t.Fatal("torrent name missing from render")
	}
}

func TestRenderPromptShowsCursor(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Update(runeKey('s'))
	typeText(m, "abc")
	screen := m.Render(80, 24)
	if !screen.CursorVisible {
		t.Fatal("cursor should be visible during prompt input")
	}
	last := screen.Lines[len(screen.Lines)-1].Text
	if !strings.Contains(last, "abc") {
		t.Fatalf("prompt buffer missing: %q", last)
	}
}

func TestUnifiedDownloadsAndHTTPActions(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabTorrents
	m.SetTorrents([]Torrent{{Hash: "torrent", Name: "Torrent", State: "downloading", Progress: 20}})
	m.SetHTTPDownloads([]ComicDownload{{ID: "http-1", Title: "Comic HTTP", Status: "downloading", Progress: 40, SpeedBytes: 1024}})
	items := m.VisibleDownloads()
	if len(items) != 2 || items[0].HTTP == nil || items[1].Torrent == nil {
		t.Fatalf("unexpected unified rows: %+v", items)
	}
	m.Selected = 0
	if action := m.Update(runeKey('p')); action.Kind != ActionHTTPPauseToggle || action.HTTPID != "http-1" {
		t.Fatalf("p should pause HTTP download: %+v", action)
	}
	m.Selected = 1
	if action := m.Update(runeKey('p')); action.Kind != ActionPauseToggle || action.Hash != "torrent" {
		t.Fatalf("p should pause torrent: %+v", action)
	}
}

func TestLibraryViewsAndNotificationTransitions(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabLibrary
	m.SetLibrary([]SeriesLibraryItem{{Name: "Example", Seasons: "1-2", Quality: "1080p", Enabled: true}})
	m.SetLibrary([]MovieLibraryItem{{Name: "Movie", Year: "2026", Quality: "4K", Enabled: true}})
	m.SetLibrary([]ComicLibraryItem{{Title: "Comic", Publisher: "Publisher", Enabled: false}})
	if action := m.Update(kindKey(KeyRight)); action.Kind != ActionNone || m.Library != LibraryMovies {
		t.Fatalf("→ should switch library view: %+v kind=%v", action, m.Library)
	}
	if len(m.VisibleLibraryRows()) != 1 || m.VisibleLibraryRows()[0].Name != "Movie" {
		t.Fatalf("movie rows = %+v", m.VisibleLibraryRows())
	}
	m.Update(runeKey('s'))
	typeText(m, "Movie")
	m.Update(kindKey(KeyEnter))
	if m.LibraryFilter != "Movie" {
		t.Fatalf("library filter = %q", m.LibraryFilter)
	}
	m.SetHTTPDownloads([]ComicDownload{{ID: "x", Title: "X", Status: "downloading"}})
	m.SetHTTPDownloads([]ComicDownload{{ID: "x", Title: "X", Status: "completed"}})
	if !strings.Contains(strings.ToLower(m.Notification), "completato") && !strings.Contains(strings.ToLower(m.Notification), "completed") {
		t.Fatalf("completion notification = %q", m.Notification)
	}

	// Esc with filter clears filter
	m.Update(kindKey(KeyEsc))
	if m.LibraryFilter != "" || m.Tab != TabLibrary {
		t.Fatalf("Esc should clear filter first: filter=%q tab=%v", m.LibraryFilter, m.Tab)
	}
	// Esc without filter returns to TabStatus
	if action := m.Update(kindKey(KeyEsc)); m.Tab != TabStatus || action.Kind != ActionRefresh {
		t.Fatalf("Esc should return to TabStatus: tab=%v action=%+v", m.Tab, action)
	}

	// Switch back to TabLibrary, test numeric key 4 (TabHealth) works from library
	m.Tab = TabLibrary
	if action := m.Update(runeKey('4')); m.Tab != TabHealth {
		t.Fatalf("4 from TabLibrary should switch to TabHealth: tab=%v action=%+v", m.Tab, action)
	}
}

func TestRenderError(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.SetError("connection refused")
	screen := m.Render(100, 30)
	if !lineContains(screen, "connection refused") {
		t.Fatal("error should be rendered")
	}
}

func TestDaemonStateAndStatusMetrics(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.SetStatus(Status{Active: true, TorrentStats: TorrentStats{Count: 2, Downloading: 1, Seeding: 1}})
	m.SetMetrics(DashboardStats{
		TorrentStats: map[string]any{"dl_info_speed": float64(1024), "up_info_speed": float64(512), "active_peers": float64(3)},
		Consumption:  &ConsumptionStats{TotalBytes: 4096, Last7DaysBytes: 2048, Last30DaysBytes: 3072},
	})
	m.SetMetrics(DashboardStats{
		TorrentStats: map[string]any{"dl_info_speed": float64(2048), "up_info_speed": float64(1024), "active_peers": float64(4)},
		Consumption:  &ConsumptionStats{TotalBytes: 8192, Last7DaysBytes: 4096, Last30DaysBytes: 6144, Daily7d: []DailyConsumption{{Bytes: 1}, {Bytes: 2}, {Bytes: 3}}},
	})
	m.SetDaemonState(true, "")
	screen := m.Render(100, 20)
	if !lineContains(screen, "ONLINE") || !lineContains(screen, "Traffic") || !lineContains(screen, "Usage") || !lineContains(screen, "trend") || !lineContains(screen, "2.0 KB/s") {
		t.Fatalf("status metrics missing: %+v", firstLines(screen, 12))
	}
	m.SetDaemonState(false, "connection refused")
	screen = m.Render(100, 20)
	if !lineContains(screen, "OFFLINE") || screen.Lines[0].Style != StyleErr {
		t.Fatalf("offline daemon state not visible: %+v", firstLines(screen, 3))
	}
}

func TestSettingsCopyAndNotifications(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.SetConfig(TUIConfig{DefaultLanguage: "en", RefreshSecs: 2, DryRun: false})
	if action := m.Update(runeKey('g')); action.Kind != ActionNone || m.Overlay != OverlaySettings {
		t.Fatalf("g should open settings, got action=%+v overlay=%v", action, m.Overlay)
	}
	m.SettingsSelected = 3
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionSaveSetting || action.Domain != "dry_run" || action.Text != "true" {
		t.Fatalf("dry-run setting action = %+v", action)
	}
	m.Overlay = OverlayNone
	m.Tab = TabTorrents
	m.SetTorrents(sampleTorrents())
	if action := m.Update(runeKey('y')); action.Kind != ActionCopy || action.Text != "aaaa" {
		t.Fatalf("y should copy selected hash, got %+v", action)
	}
	m.SetDaemonState(true, "")
	m.SetDaemonState(false, "connection refused")
	if !strings.Contains(m.Notification, "offline") {
		t.Fatalf("daemon notification = %q", m.Notification)
	}
	m.ObserveEvent(Event{Kind: "torrent_finished", Name: "Example"})
	if !strings.Contains(m.Notification, "finished") {
		t.Fatalf("event notification = %q", m.Notification)
	}
}

func TestLogsFitTerminalHeight(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.Tab = TabLogs
	logs := make([]string, 100)
	for index := range logs {
		logs[index] = "line " + string(rune('A'+index%26))
	}
	m.SetLogs(logs)
	screen := m.Render(80, 8)
	if len(screen.Lines) != 8 {
		t.Fatalf("expected terminal-sized screen, got %d lines", len(screen.Lines))
	}
	visible := 0
	for _, line := range screen.Lines {
		if strings.HasPrefix(line.Text, "line ") {
			visible++
		}
	}
	if visible != 3 {
		t.Fatalf("expected 3 visible log rows, got %d", visible)
	}
	if got := logLimitForHeight(8); got != 40 {
		t.Fatalf("small terminal log limit = %d, want 40", got)
	}
	if got := logLimitForHeight(200); got != 500 {
		t.Fatalf("large terminal log limit = %d, want 500", got)
	}
}

func TestRenderTerminalTooSmall(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	screen := m.Render(5, 3)
	if len(screen.Lines) != 1 || screen.Lines[0].Text != m.Tr.T("msg.termtoolsmall") {
		t.Fatalf("small terminal render = %+v", screen.Lines)
	}
}

func lineContains(screen Screen, needle string) bool {
	for _, line := range screen.Lines {
		if strings.Contains(line.Text, needle) {
			return true
		}
	}
	return false
}

func firstLines(screen Screen, count int) []string {
	lines := []string{}
	for index := 0; index < count && index < len(screen.Lines); index++ {
		lines = append(lines, screen.Lines[index].Text)
	}
	return lines
}

func TestTempLimitsPrompt(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabTorrents
	m.SetTorrents(sampleTorrents())

	m.Update(runeKey('T'))
	typeText(m, "500 100 30")
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionSetTempLimits || action.DL != 500 || action.UL != 100 || action.Minutes != 30 {
		t.Fatalf("temp limits action = %+v", action)
	}
	// Without minutes the limit stays until removed.
	m.Update(runeKey('T'))
	typeText(m, "200 0")
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionSetTempLimits || action.DL != 200 || action.Minutes != 0 {
		t.Fatalf("temp limits without minutes = %+v", action)
	}
	m.Update(runeKey('T'))
	typeText(m, "off")
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionClearTempLimits {
		t.Fatalf("off should clear the temporary limit, got %+v", action)
	}
	for _, input := range []string{"500", "1 2 3 4", "-1 0", "a b", "10 10 1441"} {
		m.Update(runeKey('T'))
		typeText(m, input)
		if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionNone {
			t.Fatalf("%q should not emit an action, got %+v", input, action)
		}
		if m.Message == "" {
			t.Fatalf("%q should explain the format", input)
		}
		m.Message = ""
	}
}

func TestTempLimitsPrefillAndHeader(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabTorrents
	m.SetTorrents(sampleTorrents())

	m.SetSpeedPolicy(SpeedPolicy{Source: "base", BaseDownloadKib: 0, BaseUploadKib: 200})
	if screen := m.Render(160, 20); !lineContains(screen, "limiti ↓illimitato ↑200 KiB/s") {
		t.Fatalf("base limits missing from header: %+v", screen.Lines)
	}
	m.Update(runeKey('T'))
	if m.Prompt.Buffer != "" {
		t.Fatalf("no temporary limit used yet: prefill = %q", m.Prompt.Buffer)
	}
	m.Update(kindKey(KeyEsc))

	m.SetSpeedPolicy(SpeedPolicy{Source: "temp", TempActive: true, TempDownloadKib: 500, TempUploadKib: 100, TempRemainingSec: 90})
	if screen := m.Render(160, 20); !lineContains(screen, "limite temporaneo ↓500 KiB/s ↑100 KiB/s · ancora 2 min") {
		t.Fatalf("temporary limit missing from header: %+v", screen.Lines)
	}
	m.Update(runeKey('T'))
	if m.Prompt.Buffer != "500 100 2" {
		t.Fatalf("prefill = %q", m.Prompt.Buffer)
	}
	m.Update(kindKey(KeyEsc))

	m.SetSpeedPolicy(SpeedPolicy{Source: "temp", TempActive: true, TempDownloadKib: 500})
	if screen := m.Render(160, 20); !lineContains(screen, "fino a rimozione") {
		t.Fatalf("permanent temporary limit missing from header: %+v", screen.Lines)
	}
}

func TestListCommandsWorkOnHTTPRows(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabTorrents
	m.SetHTTPDownloads([]ComicDownload{{ID: "http-1", Title: "Comic HTTP", Status: "downloading"}})
	m.Selected = 0
	m.Update(runeKey('T'))
	if m.Prompt == nil || m.Prompt.Kind != PromptTempLimits {
		t.Fatalf("T should open the temporary limit prompt on an HTTP row")
	}
	m.Update(kindKey(KeyEsc))
	m.Update(runeKey('X'))
	if m.Confirm == nil {
		t.Fatalf("X should ask to clean completed torrents on an HTTP row")
	}
}

func TestDigitsAlwaysSwitchTab(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabLibrary
	m.Update(runeKey('3'))
	if m.Tab != TabLogs {
		t.Fatalf("3 from the library should open the log, tab = %v", m.Tab)
	}
	m.Update(runeKey('8'))
	if m.Tab != TabLibrary {
		t.Fatalf("8 should open the library, tab = %v", m.Tab)
	}
	m.Update(runeKey('9'))
	if m.Tab != TabMaintenance {
		t.Fatalf("9 should open maintenance, tab = %v", m.Tab)
	}
}

func TestCyclePromptQuickChoice(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Update(runeKey('c'))
	if action := m.Update(runeKey('f')); action.Kind != ActionRunCycle || action.Domain != "movies" || m.Prompt != nil {
		t.Fatalf("f should start a movies cycle: %+v", action)
	}
	m.Update(runeKey('c'))
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionRunCycle || action.Domain != "full" {
		t.Fatalf("Enter should start a full cycle: %+v", action)
	}
}

func TestLogProblemsOnlyAndLineScroll(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabLogs
	m.Logs = []string{"10:00  INFO a", "10:01  WARN b", "10:02  ERROR c", "10:03  INFO d"}
	m.Update(runeKey('w'))
	if got := m.FilteredLogs(); len(got) != 2 || got[0] != "10:01  WARN b" {
		t.Fatalf("problems only = %v", got)
	}
	m.Update(runeKey('w'))
	if len(m.FilteredLogs()) != 4 {
		t.Fatal("w must toggle the filter off")
	}
	m.LogScroll = 3
	m.Update(kindKey(KeyDown))
	if m.LogScroll != 2 {
		t.Fatalf("↓ must scroll one line, LogScroll = %d", m.LogScroll)
	}
}

func TestTorrentRowETAAndRatio(t *testing.T) {
	downloading := DownloadRow{Torrent: &Torrent{Progress: 50, TotalSize: 1000, TotalDone: 500, DownloadRate: 10, NumSeeds: 3, NumPeers: 12}}
	if got := downloadRowETA(downloading); got != HumanDuration(50) {
		t.Fatalf("eta = %q", got)
	}
	if got := downloadRowSwarm(downloading); got != "3/12" {
		t.Fatalf("swarm = %q", got)
	}
	stuck := DownloadRow{Torrent: &Torrent{Progress: 74, TotalSize: 1000, TotalDone: 740}}
	if got := downloadRowETA(stuck); got != "∞" {
		t.Fatalf("stuck eta = %q", got)
	}
	seeding := DownloadRow{Torrent: &Torrent{Progress: 100, TotalSize: 1000, AllTimeDownload: 1000, AllTimeUpload: 1250}}
	if got := downloadRowETA(seeding); got != "r 1.25" {
		t.Fatalf("ratio = %q", got)
	}
}
