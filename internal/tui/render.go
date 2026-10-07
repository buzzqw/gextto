package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const tabCount = 9

// Render produces a frame for the given terminal size. It is pure: the same
// state always renders the same lines, which makes the layout testable.
func (m *Model) Render(width, height int) Screen {
	if width < 12 || height < 6 {
		return Screen{Lines: []Line{{Text: m.Tr.T("msg.termtoolsmall"), Style: StyleHeader}}, ColorsEnabled: m.ColorsEnabled, HighContrast: m.HighContrast}
	}
	lines := make([]Line, 0, height)
	headerStyle := StyleHeader
	if m.DaemonKnown && !m.DaemonConnected {
		headerStyle = StyleErr
	}
	lines = append(lines, Line{Text: m.headerLine(width), Style: headerStyle})
	lines = append(lines, Line{Text: m.truncate(m.tabsLine(width), width), Style: StyleNormal})
	lines = append(lines, Line{Text: m.truncate(m.hints(), width), Style: StyleMuted})

	contentHeight := height - 4
	if contentHeight < 1 {
		contentHeight = 1
	}
	var content []Line
	pageScroll := false
	switch {
	case m.Overlay == OverlayHelp:
		content = m.renderHelp(width)
		// The help is longer than a 24-row terminal: let it scroll.
		if limit := max(0, len(content)-contentHeight); m.HelpScroll > limit {
			m.HelpScroll = limit
		}
		content = content[m.HelpScroll:]
	case m.Overlay == OverlaySearch:
		content = m.renderSearch(width, contentHeight)
	case m.Overlay == OverlayEvents:
		content = m.renderEvents(width, contentHeight)
	case m.Overlay == OverlaySettings:
		content = m.renderSettings(width, contentHeight)
	case m.Overlay == OverlayHistory:
		content = m.renderHistory(width, contentHeight)
	case m.Overlay == OverlayMaintenance:
		content = m.renderMaintenanceReport(width, contentHeight)
	case m.Form != nil:
		content = m.renderForm(width, contentHeight)
	case m.Detail != nil:
		content = m.renderDetail(width, contentHeight)
	case m.ArchiveDetail != nil:
		content = m.renderArchiveDetail(width)
	case m.Tab == TabStatus:
		content = m.renderStatus(width, contentHeight)
	case m.Tab == TabTorrents:
		content = m.renderTorrents(width, contentHeight)
	case m.Tab == TabLogs:
		content = m.renderLogs(width, contentHeight)
	case m.Tab == TabArchive:
		content = m.renderArchive(width, contentHeight)
	case m.Tab == TabMissing:
		content = m.renderMissing(width, contentHeight)
	case m.Tab == TabBlocklist:
		content = m.renderBlocklist(width, contentHeight)
	case m.Tab == TabLibrary && m.SeriesView != nil:
		content = m.renderSeriesView(width, contentHeight)
	case m.Tab == TabLibrary && m.MovieView != nil:
		content = m.renderMovieView(width, contentHeight)
	case m.Tab == TabLibrary:
		content = m.renderLibrary(width, contentHeight)
	case m.Tab == TabMaintenance:
		content = m.renderMaintenance(width, contentHeight)
	default:
		content = m.renderHealth(width)
		pageScroll = true
	}
	content = expandLines(content, width)
	if pageScroll {
		content = m.pageWindow(content, contentHeight)
	}
	for index := 0; index < contentHeight; index++ {
		if index < len(content) {
			lines = append(lines, Line{Text: m.truncate(content[index].Text, width), Style: content[index].Style})
		} else {
			lines = append(lines, Line{})
		}
	}

	screen := Screen{ColorsEnabled: m.ColorsEnabled, HighContrast: m.HighContrast}
	if m.Confirm != nil {
		lines = append(lines, Line{Text: m.truncate(m.ConfirmMessage(), width), Style: StyleWarn})
	} else if m.Prompt != nil {
		lines = append(lines, Line{Text: m.promptLine(width), Style: StyleSelected})
		screen.CursorVisible = true
	} else {
		message := m.Message
		footerStyle := StyleOK
		if m.Notification != "" && time.Now().Before(m.NotificationUntil) {
			if message != "" {
				message += " · "
			}
			message += m.Notification
			footerStyle = StyleWarn
		}
		if m.Err != "" {
			if message != "" {
				message += " · "
			}
			message += m.Tr.Format("msg.offline", m.Err)
			footerStyle = StyleErr
		}
		if m.Loading {
			if message != "" {
				message += " · "
			}
			message += m.Tr.T("msg.loading")
		}
		lines = append(lines, Line{Text: m.footerWithMeter(message, width), Style: footerStyle})
	}
	for len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, Line{})
	}
	screen.Lines = lines
	if screen.CursorVisible {
		screen.CursorRow = len(lines) - 1
		screen.CursorCol = min(m.promptCursorColumn(width), width-1)
	}
	return screen
}

func (m *Model) headerLine(width int) string {
	name := "Gextto TUI"
	mode := m.Tr.T("mode.loading")
	modeStyle := ""
	_ = modeStyle
	if m.Status != nil {
		if m.Status.Version != "" {
			name = "Gextto TUI v" + m.Status.Version
		}
		switch {
		case m.Status.DryRun:
			mode = m.Tr.T("mode.dryrun")
		case m.Status.Active:
			mode = m.Tr.T("mode.active")
		default:
			mode = m.Tr.T("mode.standby")
		}
	}
	next := ""
	if m.Status != nil && m.Status.NextCycleAt != nil {
		if remaining := nextCycleSeconds(*m.Status.NextCycleAt); remaining >= 0 {
			next = " · " + m.Tr.T("label.nextcycle") + ": " + HumanDuration(float64(remaining))
		}
	}
	connection := m.Tr.T("label.connecting")
	if m.DaemonKnown {
		connection = m.Tr.T("label.online")
		if !m.DaemonConnected {
			connection = m.Tr.T("label.offline")
			if m.DaemonReconnects > 1 {
				connection += fmt.Sprintf(" #%d", m.DaemonReconnects)
			}
		}
	}
	next = " · " + connection + next
	if !m.LogStreamConnected && m.StreamReconnect > 0 {
		next += fmt.Sprintf(" · %s #%d", m.Tr.T("label.reconnect"), m.StreamReconnect)
	}
	return m.truncate(fmt.Sprintf("%s  [%s]%s", name, mode, next), width)
}

func nextCycleSeconds(value string) int64 {
	if value == "" {
		return -1
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return -1
	}
	return int64(time.Until(parsed).Seconds())
}

func (m *Model) tabsLine(width int) string {
	labels := []string{m.Tr.T("tab.status"), m.Tr.T("tab.torrents"), m.Tr.T("tab.logs"), m.Tr.T("tab.health"), m.Tr.T("tab.archive"), m.Tr.T("tab.missing"), m.Tr.T("tab.blocklist"), m.Tr.T("tab.library"), m.Tr.T("tab.maintenance")}
	parts := make([]string, 0, tabCount)
	for index, label := range labels {
		if Tab(index) == m.Tab {
			parts = append(parts, fmt.Sprintf("[%d:%s]", index+1, label))
		} else {
			parts = append(parts, fmt.Sprintf(" %d:%s ", index+1, label))
		}
	}
	full := "  " + strings.Join(parts, " ")
	if withHint := full + "  (" + m.Tr.T("hint.tabs") + ")"; utf8.RuneCountInString(withHint) <= width {
		return withHint
	}
	if utf8.RuneCountInString(full) <= width {
		return full
	}
	compact := make([]string, 0, tabCount)
	for index, label := range labels {
		if Tab(index) == m.Tab {
			compact = append(compact, fmt.Sprintf("[%d:%s]", index+1, label))
		} else {
			compact = append(compact, fmt.Sprintf("%d", index+1))
		}
	}
	compactLine := " " + strings.Join(compact, " ")
	if utf8.RuneCountInString(compactLine) <= width {
		return compactLine
	}
	return fmt.Sprintf("[%d:%s]", int(m.Tab)+1, labels[m.Tab])
}

func (m *Model) hints() string {
	// The view-specific keys come first: on an 80-column SSH window the tail
	// of this line is cut, and the global keys are also listed in the help.
	hints := ""
	switch {
	case m.Overlay == OverlayHelp:
		hints = m.Tr.T("hint.help")
	case m.Overlay == OverlayEvents:
		hints = m.Tr.T("hint.events")
	case m.Overlay == OverlaySettings:
		hints = m.Tr.T("hint.settings")
	case m.Overlay == OverlayHistory:
		hints = m.Tr.T("hint.history")
	case m.Overlay == OverlayMaintenance:
		hints = m.Tr.T("hint.report")
	case m.Overlay == OverlaySearch && m.SearchKind != SearchReleases:
		hints = m.Tr.T("hint.tmdb")
	case m.Overlay == OverlaySearch:
		hints = m.Tr.T("hint.search")
	case m.Form != nil:
		hints = m.Tr.T("hint.form")
	case m.Detail != nil && m.DetailView == DetailFiles:
		hints = m.Tr.T("hint.detailfiles")
	case m.Detail != nil && m.DetailView == DetailTrackers:
		hints = m.Tr.T("hint.detailtrackers")
	case m.Detail != nil:
		hints = m.Tr.T("hint.details")
	case m.ArchiveDetail != nil:
		hints = m.Tr.T("hint.archivedetail")
	case m.Tab == TabStatus:
		// The home page also lists the global actions; elsewhere they are in
		// the help, so the line keeps room for the keys of the current view.
		hints = m.Tr.T("hint.status") + " · " + m.Tr.T("hint.actions")
	case m.Tab == TabTorrents && len(m.MarkedHashes()) > 0:
		hints = m.Tr.T("hint.torrentsmarked")
	case m.Tab == TabTorrents:
		hints = m.Tr.T("hint.torrents")
	case m.Tab == TabLogs:
		hints = m.Tr.T("hint.logs")
	case m.Tab == TabHealth:
		hints = m.Tr.T("hint.health")
	case m.Tab == TabArchive:
		hints = m.Tr.T("hint.archive")
	case m.Tab == TabMissing:
		hints = m.Tr.T("hint.missing")
	case m.Tab == TabBlocklist:
		hints = m.Tr.T("hint.blocklist")
	case m.Tab == TabMaintenance:
		hints = m.Tr.T("hint.maintenance")
	case m.Tab == TabLibrary && m.SeriesView != nil:
		hints = m.Tr.T("hint.series")
	case m.Tab == TabLibrary && m.MovieView != nil:
		hints = m.Tr.T("hint.movie")
	case m.Tab == TabLibrary && m.Library == LibraryComics:
		hints = m.Tr.T("hint.library")
	case m.Tab == TabLibrary:
		hints = m.Tr.T("hint.librarymanage")
	}
	if hints == "" {
		return m.Tr.T("hint.global")
	}
	return hints + " · " + m.Tr.T("hint.global")
}

// statusLabelWidth is the label column of the Status page; wrapped values
// continue under the value column.
const statusLabelWidth = 12

// renderStatus builds the dashboard: daemon, system, cycle, feeds, downloads,
// traffic, problems and the active transfers, with the latest log lines kept
// at the bottom. The upper part scrolls when the window is too short.
func (m *Model) renderStatus(width, contentHeight int) []Line {
	if m.Status == nil {
		lines := []Line{{Text: m.Tr.T("msg.loading"), Style: StyleMuted}}
		if m.Err != "" {
			lines = append(lines, Line{Text: m.Tr.Format("status.offline", m.Err), Style: StyleErr, Wrap: true, Indent: 2})
		}
		return lines
	}
	top := expandLines(m.statusInfoLines(width), width)

	logBlock := m.statusLogLines(width, contentHeight)
	room := contentHeight - len(logBlock)
	if room < 3 {
		logBlock, room = nil, contentHeight
	}
	window := m.pageWindow(top, room)
	if len(logBlock) > 0 {
		// Pin the log block to the bottom so it does not jump around while
		// the rows above come and go.
		for len(window) < room {
			window = append(window, Line{})
		}
	}
	return append(window, logBlock...)
}

func (m *Model) statusRow(label, value string, style Style) Line {
	return Line{Text: PadRight(label+":", statusLabelWidth) + value, Style: style, Wrap: true, Indent: statusLabelWidth}
}

func (m *Model) statusInfoLines(width int) []Line {
	status := m.Status
	tr := m.Tr
	lines := []Line{}

	// Daemon: version, mode, uptime and own resources.
	mode := tr.T("mode.standby")
	switch {
	case status.DryRun:
		mode = tr.T("mode.dryrun")
	case status.Active:
		mode = tr.T("mode.active")
	}
	daemon := []string{}
	if status.Version != "" {
		daemon = append(daemon, "v"+status.Version)
	}
	daemon = append(daemon, mode)
	health := m.Health
	if health != nil {
		if health.ProcessUptimeSeconds > 0 {
			daemon = append(daemon, tr.Format("status.uptime", HumanDuration(float64(health.ProcessUptimeSeconds))))
		}
		if health.ProcessID > 0 {
			daemon = append(daemon, tr.Format("status.pid", health.ProcessID))
		}
		daemon = append(daemon, tr.T("label.cpu")+" "+OptionalNumber(floatPointerValue(health.ProcessCPUPercent), "%", 1))
		if health.ResidentBytes > 0 {
			daemon = append(daemon, tr.T("label.ram")+" "+HumanBytes(float64(health.ResidentBytes)))
		}
	}
	modeStyle := StyleNormal
	if status.DryRun {
		modeStyle = StyleWarn
	}
	lines = append(lines, m.statusRow(tr.T("status.daemon"), strings.Join(daemon, " · "), modeStyle))

	// System: CPU, load, memory, main disk, trash.
	diskUsed := -1.0
	if health != nil {
		system := []string{tr.T("label.cpu") + " " + OptionalNumber(floatPointerValue(health.CPUPercent), "%", 1)}
		if health.LoadAverage != nil {
			system = append(system, tr.Format("status.load", *health.LoadAverage))
		}
		if health.MemoryTotalBytes > 0 {
			system = append(system, tr.Format("status.ramfree", HumanBytes(float64(health.MemoryAvailableBytes)), HumanBytes(float64(health.MemoryTotalBytes))))
		}
		if health.DiskTotalBytes > 0 {
			diskUsed = (1 - float64(health.DiskFreeBytes)/float64(health.DiskTotalBytes)) * 100
			system = append(system, tr.Format("status.disk", HumanBytes(float64(health.DiskFreeBytes)), HumanBytes(float64(health.DiskTotalBytes)), diskUsed))
		}
		if health.TrashFileCount > 0 {
			system = append(system, tr.Format("status.trash", health.TrashFileCount, HumanBytes(float64(health.TrashBytes))))
		}
		lines = append(lines, m.statusRow(tr.T("status.system"), strings.Join(system, " · "), StyleMuted))
	}

	// Cycle: countdown, last run and its counters.
	cycle := status.LastCycle
	cycleParts := []string{}
	if status.NextCycleAt != nil {
		if remaining := nextCycleSeconds(*status.NextCycleAt); remaining >= 0 {
			cycleParts = append(cycleParts, tr.Format("status.next", HumanDuration(float64(remaining))))
		} else if *status.NextCycleAt != "" {
			cycleParts = append(cycleParts, tr.T("status.running"))
		}
	}
	if cycle.LastStartedAt != nil {
		if started, err := time.Parse(time.RFC3339, *cycle.LastStartedAt); err == nil {
			cycleParts = append(cycleParts, tr.Format("status.laststart", started.Local().Format("02/01 15:04"), HumanDuration(time.Since(started).Seconds())))
		}
	}
	cycleParts = append(cycleParts, fmt.Sprintf("%s %d · %s %d · %s %d · %s %d · %s %d",
		tr.T("label.scraped"), cycle.Scraped,
		tr.T("label.candidates"), cycle.Candidates,
		tr.T("label.downloads"), cycle.DownloadsStarted,
		tr.T("label.gaps"), cycle.GapsFilled,
		tr.T("label.errors"), cycle.Errors))
	lines = append(lines, m.statusRow(tr.T("status.cycle"), strings.Join(cycleParts, " · "), StyleNormal))
	lines = append(lines, m.statusRow(tr.T("status.feeds"), fmt.Sprintf("%s %d · %s %d · %s %d",
		tr.T("label.groups"), status.Seen.Groups,
		tr.T("label.movies"), status.Seen.Movies,
		tr.T("label.series"), status.Seen.Series), StyleMuted))

	// Downloads: active means really transferring (either direction); an
	// unfinished torrent at 0 B/s is counted as stuck, not as active.
	stats := status.TorrentStats
	var downloads string
	if len(m.Torrents) > 0 {
		transferring, idle, waiting, done := 0, 0, 0, 0
		for _, torrent := range m.Torrents {
			switch {
			case torrentTransferring(torrent):
				transferring++
			case torrentIdle(torrent):
				idle++
			case torrent.Progress >= 100:
				done++
			default:
				waiting++
			}
		}
		downloads = fmt.Sprintf("%s: %s · %s · %s · %s", tr.Format("status.torrentcount", len(m.Torrents)),
			tr.Format("status.transferring", transferring), tr.Format("status.idle", idle),
			tr.Format("status.waiting", waiting), tr.Format("status.done", done))
	} else {
		downloads = fmt.Sprintf("%s: %d %s · %d %s · %d %s", tr.Format("status.torrentcount", stats.Count),
			stats.Downloading, tr.T("label.downloading"),
			stats.Queued, tr.T("label.queued"),
			stats.Seeding, tr.T("label.seeding"))
	}
	httpActive := 0
	for _, item := range m.HTTPDownloads {
		if item.SpeedBytes > 0 {
			httpActive++
		}
	}
	if httpActive > 0 {
		downloads += fmt.Sprintf(" · %s %d", tr.T("label.http"), httpActive)
	}
	lines = append(lines, m.statusRow(tr.T("status.downloads"), downloads, StyleNormal))

	// Traffic and consumption.
	lines = append(lines, m.renderMetricLines(width)...)

	// Attention: everything that needs a look, or an explicit "all good".
	problems, severity := m.statusProblems(diskUsed)
	if len(problems) == 0 {
		lines = append(lines, m.statusRow(tr.T("status.attention"), tr.T("status.allgood"), StyleOK))
	} else {
		lines = append(lines, m.statusRow(tr.T("status.attention"), strings.Join(problems, " · "), severity))
	}

	// Active transfers, fastest first.
	lines = append(lines, Line{Text: tr.T("status.active") + ":", Style: StyleHeader})
	active := m.statusActiveLines(width)
	if len(active) == 0 {
		lines = append(lines, Line{Text: "  " + tr.T("status.noactive"), Style: StyleMuted})
	}
	lines = append(lines, active...)
	idleNames := []string{}
	for _, torrent := range m.Torrents {
		if torrentIdle(torrent) {
			idleNames = append(idleNames, fmt.Sprintf("%s (%.0f%%)", Sanitize(torrent.Name), torrent.Progress))
		}
	}
	if len(idleNames) > 0 {
		sort.Strings(idleNames)
		lines = append(lines, m.statusRow(tr.T("status.idlelist"), strings.Join(idleNames, " · "), StyleWarn))
	}
	return lines
}

// statusProblems lists the conditions worth attention and their severity.
func (m *Model) statusProblems(diskUsed float64) ([]string, Style) {
	tr := m.Tr
	problems := []string{}
	severity := StyleWarn
	if m.DaemonKnown && !m.DaemonConnected && m.Err != "" {
		problems = append(problems, tr.Format("status.offline", m.Err))
		severity = StyleErr
	}
	errored := 0
	for _, torrent := range m.Torrents {
		if strings.EqualFold(torrent.State, "error") || strings.TrimSpace(torrent.Error) != "" {
			errored++
		}
	}
	for _, item := range m.HTTPDownloads {
		if strings.EqualFold(item.Status, "error") {
			errored++
		}
	}
	if errored > 0 {
		problems = append(problems, tr.Format("status.torrenterrs", errored))
		severity = StyleErr
	}
	if m.Status.LastCycle.Errors > 0 {
		problems = append(problems, tr.Format("status.cycleerrors", m.Status.LastCycle.Errors))
	}
	if health := m.Health; health != nil {
		if health.Status != "" && !strings.EqualFold(health.Status, "ok") {
			problems = append(problems, tr.Format("status.health", strings.ToUpper(health.Status)))
		}
		bad := 0
		for _, path := range health.Paths {
			if !path.Exists || !path.Writable {
				bad++
			}
		}
		if bad > 0 {
			problems = append(problems, tr.Format("status.badpaths", bad))
			severity = StyleErr
		}
		if diskUsed >= 90 {
			problems = append(problems, tr.Format("status.diskfull", diskUsed))
		}
		if count := len(health.LastErrors); count > 0 {
			problems = append(problems, tr.Format("status.lasterror", health.LastErrors[count-1]))
		}
	}
	return problems, severity
}

// statusActiveLines shows up to five running transfers with a progress bar.
func (m *Model) statusActiveLines(width int) []Line {
	type activeRow struct {
		name     string
		state    string
		progress float64
		rate     float64
		upload   float64
		eta      float64
	}
	rows := []activeRow{}
	for _, torrent := range m.Torrents {
		if !torrentTransferring(torrent) {
			continue
		}
		eta := -1.0
		if torrent.DownloadRate > 0 && torrent.TotalSize > torrent.TotalDone {
			eta = float64(torrent.TotalSize-torrent.TotalDone) / float64(torrent.DownloadRate)
		}
		rows = append(rows, activeRow{torrent.Name, torrent.State, torrent.Progress, float64(torrent.DownloadRate), float64(torrent.UploadRate), eta})
	}
	for _, item := range m.HTTPDownloads {
		if item.SpeedBytes == 0 {
			continue
		}
		eta := -1.0
		if item.ETASeconds != nil {
			eta = float64(*item.ETASeconds)
		}
		rows = append(rows, activeRow{firstNonEmpty(item.Title, item.ID), item.Status, item.Progress, float64(item.SpeedBytes), 0, eta})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].rate+rows[i].upload != rows[j].rate+rows[j].upload {
			return rows[i].rate+rows[i].upload > rows[j].rate+rows[j].upload
		}
		return rows[i].progress > rows[j].progress
	})
	if len(rows) > 5 {
		rows = rows[:5]
	}
	barWidth := 10
	if width < 60 {
		barWidth = 6
	}
	lines := make([]Line, 0, len(rows))
	for _, row := range rows {
		eta := "-"
		if row.eta >= 0 {
			eta = HumanDuration(row.eta)
		}
		prefix := fmt.Sprintf("  %s %s ↓%s ↑%s %s ", progressBar(row.progress, barWidth), PadLeft(fmt.Sprintf("%.0f%%", row.progress), 4),
			PadLeft(HumanRate(row.rate), 10), PadLeft(HumanRate(row.upload), 10), PadRight(m.Tr.Format("status.eta", eta), 10))
		lines = append(lines, Line{Text: prefix + Sanitize(row.name), Style: torrentStyle(row.state), Wrap: true, Indent: min(StringWidth(prefix), width/2)})
	}
	return lines
}

// progressBar draws a fixed-width bar; ASCII terminals get '#' and '.'.
func progressBar(percent float64, width int) string {
	if width <= 0 {
		return ""
	}
	filled := int(percent/100*float64(width) + 0.5)
	filled = min(max(filled, 0), width)
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

// statusLogLines returns the pinned "recent log" block: a separator and the
// last four log lines, wrapped, never taller than half of the content area.
func (m *Model) statusLogLines(width, contentHeight int) []Line {
	entries := 4
	switch {
	case contentHeight < 10:
		return nil
	case contentHeight < 16:
		entries = 2
	}
	title := " " + m.Tr.T("status.recentlogs") + " "
	rule := max(0, width-StringWidth(title)-2)
	header := Line{Text: "──" + title + strings.Repeat("─", rule), Style: StyleMuted}
	if len(m.Logs) == 0 {
		return []Line{header, {Text: "  " + m.Tr.T("status.nologs"), Style: StyleMuted}}
	}
	// Whole entries only, newest last: an entry that does not fit is left
	// out rather than shown from the middle. The newest one is always shown,
	// cut with an ellipsis if it alone is taller than the block.
	budget := max(entries, contentHeight/2-1)
	body := []Line{}
	for index := len(m.Logs) - 1; index >= max(0, len(m.Logs)-entries); index-- {
		entry := m.Logs[index]
		rows := expandLines([]Line{{Text: entry, Style: logStyle(entry), Wrap: true, Indent: 2}}, width)
		if len(body)+len(rows) > budget {
			if len(body) == 0 {
				rows = rows[:budget]
				rows[budget-1].Text = Shorten(rows[budget-1].Text+" …", width)
				body = rows
			}
			break
		}
		body = append(rows, body...)
	}
	return append([]Line{header}, body...)
}

// logStyle colours a log line by its level.
func logStyle(line string) Style {
	switch {
	case strings.Contains(line, "ERROR"):
		return StyleErr
	case strings.Contains(line, "WARN"):
		return StyleWarn
	default:
		return StyleNormal
	}
}

func (m *Model) renderMetricLines(width int) []Line {
	download, hasDownload, upload, hasUpload := m.transferRates()
	peers, hasPeers := 0.0, false
	if m.Metrics != nil {
		peers, hasPeers = metricNumber(m.Metrics.TorrentStats, "active_peers", "num_connections", "num_incoming_connections")
	}
	if !hasPeers && len(m.Torrents) > 0 {
		for _, torrent := range m.Torrents {
			peers += float64(torrent.NumPeers)
		}
		hasPeers = true
	}

	httpRate := 0.0
	httpActive := 0
	for _, item := range m.HTTPDownloads {
		if strings.EqualFold(item.Status, "downloading") || strings.EqualFold(item.Status, "paused") || strings.EqualFold(item.Status, "stalled") {
			httpActive++
		}
		httpRate += float64(item.SpeedBytes)
	}
	if httpRate > 0 {
		download += httpRate
		hasDownload = true
	}
	transfer := make([]string, 0, 4)
	if hasDownload {
		transfer = append(transfer, m.Tr.T("label.down")+" "+HumanRate(download))
	}
	if hasUpload {
		transfer = append(transfer, m.Tr.T("label.up")+" "+HumanRate(upload))
	}
	if hasPeers {
		transfer = append(transfer, m.Tr.T("label.peers")+" "+fmt.Sprintf("%.0f", peers))
	}
	if httpActive > 0 {
		transfer = append(transfer, "HTTP "+fmt.Sprintf("%d", httpActive))
	}
	graphWidth := 24
	if width < 72 {
		graphWidth = 12
	}
	if len(m.TransferHistory) > 1 {
		downloads := make([]float64, 0, len(m.TransferHistory))
		uploads := make([]float64, 0, len(m.TransferHistory))
		for _, sample := range m.TransferHistory {
			downloads = append(downloads, sample.Download)
			uploads = append(uploads, sample.Upload)
		}
		transfer = append(transfer, fmt.Sprintf("%s ↓%s ↑%s", strings.ToLower(m.Tr.T("label.trend")), sparkline(downloads, graphWidth), sparkline(uploads, graphWidth)))
	}
	lines := make([]Line, 0, 2)
	if len(transfer) > 0 {
		lines = append(lines, m.statusRow(m.Tr.T("status.transfer"), strings.Join(transfer, " · "), StyleNormal))
	}
	if m.Metrics == nil {
		return lines
	}
	if consumption := m.Metrics.Consumption; consumption != nil {
		value := fmt.Sprintf("7d %s · 30d %s · %s %s",
			HumanBytes(float64(consumption.Last7DaysBytes)),
			HumanBytes(float64(consumption.Last30DaysBytes)),
			m.Tr.T("label.total"), HumanBytes(float64(consumption.TotalBytes)))
		if len(consumption.Daily7d) > 1 {
			daily := make([]float64, 0, len(consumption.Daily7d))
			for _, item := range consumption.Daily7d {
				daily = append(daily, float64(item.Bytes))
			}
			value += " · 7d " + sparkline(daily, min(24, max(8, width-28)))
		}
		lines = append(lines, m.statusRow(m.Tr.T("status.consumption"), value, StyleMuted))
	}
	return lines
}

func sparkline(values []float64, width int) string {
	if width <= 0 || len(values) == 0 {
		return ""
	}
	if len(values) > width {
		values = values[len(values)-width:]
	}
	minValue, maxValue := values[0], values[0]
	for _, value := range values[1:] {
		minValue = minFloat(minValue, value)
		maxValue = maxFloat(maxValue, value)
	}
	levels := []rune("▁▂▃▄▅▆▇█")
	var builder strings.Builder
	for _, value := range values {
		level := 0
		if maxValue > minValue {
			level = int((value - minValue) / (maxValue - minValue) * float64(len(levels)-1))
		}
		builder.WriteRune(levels[min(max(level, 0), len(levels)-1)])
	}
	return builder.String()
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func metricNumber(values map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			if number, valid := toFloat(value); valid {
				return number, true
			}
		}
	}
	return 0, false
}

func (m *Model) renderTorrents(width, contentHeight int) []Line {
	items := m.VisibleDownloads()
	sortName := []string{m.Tr.T("sort.name"), m.Tr.T("sort.progress"), m.Tr.T("sort.state"), m.Tr.T("sort.rate"), m.Tr.T("sort.size")}[m.Sort]
	direction := "↑"
	if m.SortDesc {
		direction = "↓"
	}
	title := fmt.Sprintf("%s: %d", m.Tr.T("tab.torrents"), len(items))
	if len(items) > 0 {
		title = fmt.Sprintf("%s: %d/%d", m.Tr.T("tab.torrents"), m.Selected+1, len(items))
	}
	if marked := len(m.MarkedHashes()); marked > 0 {
		title += "  · " + m.Tr.Format("label.marked", marked)
	}
	title += fmt.Sprintf("  · %s %s%s", m.Tr.T("label.sort"), sortName, direction)
	if m.Filter != "" {
		title += fmt.Sprintf("  · %s '%s'", m.Tr.T("label.filter"), m.Filter)
	}
	if policy := m.speedPolicyLabel(); policy != "" {
		title += "  · " + policy
	}
	if m.SpeedPolicy != nil && m.SpeedPolicy.AutoRemoveCompleted {
		title += "  · " + m.Tr.T("label.autoremove")
	}
	lines := []Line{{Text: title, Style: StyleHeader}}
	if len(items) == 0 {
		lines = append(lines, Line{Text: m.Tr.T("msg.emptytorrents"), Style: StyleMuted})
		return lines
	}
	m.Selected = min(max(m.Selected, 0), len(items)-1)
	window := func(visibleItems int) (int, int) {
		start, end := catalogWindow(m.Selected, m.TorrentScroll, len(items), visibleItems)
		m.TorrentScroll = start
		return start, end
	}
	// Two cells: the cursor, then the bulk mark.
	marker := func(index int) string {
		cursor, mark := " ", " "
		if index == m.Selected {
			cursor = ">"
		}
		if row := items[index]; row.Torrent != nil && m.Marked[row.Torrent.Hash] {
			mark = "*"
		}
		return cursor + mark
	}
	rowStyle := func(index int, _ string) Style {
		if index == m.Selected {
			return StyleSelected
		}
		return downloadRowStyle(items[index])
	}
	compactRow := func(index int, item DownloadRow) string {
		name := m.rowName(item)
		return fmt.Sprintf("%s %s %.1f%% %s", marker(index), downloadStateLabel(m.Tr, item), downloadRowProgress(item), name)
	}
	if width < 50 {
		budget := max(1, contentHeight-1)
		expanded := selectedRows(compactRow(m.Selected, items[m.Selected]), width, 2, budget-2)
		start, end := window(budget - (len(expanded) - 1))
		for index := start; index < end; index++ {
			_, state, _ := downloadRowFields(items[index])
			if index == m.Selected {
				for _, row := range expanded {
					lines = append(lines, Line{Text: row, Style: StyleSelected})
				}
				continue
			}
			lines = append(lines, Line{Text: compactRow(index, items[index]), Style: rowStyle(index, state)})
		}
		return lines
	}
	if width < 74 {
		budget := max(2, contentHeight-1)
		expanded := selectedRows(compactRow(m.Selected, items[m.Selected]), width, 2, budget-4)
		start, end := window(max(1, (budget-(len(expanded)-1))/2))
		for index := start; index < end; index++ {
			item := items[index]
			_, state, _ := downloadRowFields(item)
			if index == m.Selected {
				for _, row := range expanded {
					lines = append(lines, Line{Text: row, Style: StyleSelected})
				}
			} else {
				lines = append(lines, Line{Text: compactRow(index, item), Style: rowStyle(index, state)})
			}
			lines = append(lines, Line{Text: "   " + downloadDetailLine(m.Tr, item) + " · " + downloadRowETA(item) + " · " + downloadRowSwarm(item), Style: StyleMuted})
		}
		return lines
	}
	// The info-hash says nothing to a person: the columns are the ones that
	// answer "how big, how fast, when, from how many".
	nameWidth := max(10, width-78)
	header := fmt.Sprintf("   %s %s %s %s %s %s %s  %s",
		PadRight(m.Tr.T("label.state"), 14),
		PadLeft(m.Tr.T("label.progress"), 6), PadLeft(m.Tr.T("label.size"), 10),
		PadLeft(m.Tr.T("label.down"), 10), PadLeft(m.Tr.T("label.up"), 10),
		PadLeft(m.Tr.T("label.etaratio"), 9), PadLeft(m.Tr.T("label.seedspeers"), 9), m.Tr.T("label.name"))
	lines = append(lines, Line{Text: header, Style: StyleHeader})
	tableRow := func(index int, item DownloadRow) string {
		_, down, up := downloadRowValues(item)
		return fmt.Sprintf("%s %s %s %s %s %s %s %s  ",
			marker(index), PadRight(downloadStateLabel(m.Tr, item), 14),
			PadLeft(fmt.Sprintf("%.1f%%", downloadRowProgress(item)), 6), PadLeft(downloadRowSizeLabel(item), 10),
			PadLeft(HumanRate(down), 10), PadLeft(HumanRate(up), 10),
			PadLeft(downloadRowETA(item), 9), PadLeft(downloadRowSwarm(item), 9))
	}
	budget := max(1, contentHeight-len(lines))
	selectedName := m.rowName(items[m.Selected])
	nameRows := selectedRows(selectedName, nameWidth, 0, budget-2)
	start, end := window(budget - (len(nameRows) - 1))
	for index := start; index < end; index++ {
		item := items[index]
		_, state, _ := downloadRowFields(item)
		name := m.rowName(item)
		prefix := tableRow(index, item)
		if index != m.Selected {
			lines = append(lines, Line{Text: prefix + Shorten(name, nameWidth), Style: rowStyle(index, state)})
			continue
		}
		// The selected name continues under the name column.
		indent := strings.Repeat(" ", StringWidth(prefix))
		for row, text := range nameRows {
			if row == 0 {
				lines = append(lines, Line{Text: prefix + text, Style: StyleSelected})
			} else {
				lines = append(lines, Line{Text: indent + text, Style: StyleSelected})
			}
		}
	}
	return lines
}

// rowName is the displayed name, prefixed by the download tag.
func (m *Model) rowName(row DownloadRow) string {
	name, _, _ := downloadRowFields(row)
	if row.Torrent != nil {
		if tag := m.TorrentTags[row.Torrent.Hash]; tag != "" {
			return "#" + tag + " " + name
		}
	}
	return name
}

// speedPolicyLabel describes the global limits in force, like the badge
// above the web download list.
func (m *Model) speedPolicyLabel() string {
	policy := m.SpeedPolicy
	if policy == nil {
		return ""
	}
	if minutes, active := m.TempLimitMinutes(); active {
		down, up := kibLabel(m.Tr, policy.TempDownloadKib), kibLabel(m.Tr, policy.TempUploadKib)
		if minutes == 0 {
			return m.Tr.Format("policy.tempkeep", down, up)
		}
		return m.Tr.Format("policy.temp", down, up, minutes)
	}
	if policy.SchedActive && policy.Source == "schedule" {
		return m.Tr.Format("policy.sched", kibLabel(m.Tr, policy.DownloadKib), kibLabel(m.Tr, policy.UploadKib))
	}
	return m.Tr.Format("policy.base", kibLabel(m.Tr, policy.BaseDownloadKib), kibLabel(m.Tr, policy.BaseUploadKib))
}

// kibLabel shows a KiB/s limit, with 0 meaning no limit.
func kibLabel(tr *Translator, kib int64) string {
	if kib <= 0 {
		return tr.T("label.unlimited")
	}
	return fmt.Sprintf("%d KiB/s", kib)
}

func downloadStateLabel(tr *Translator, row DownloadRow) string {
	if row.Torrent != nil {
		if torrentIdle(*row.Torrent) {
			return tr.T("state.idle")
		}
		return tr.StateLabel(row.Torrent.State)
	}
	return tr.StateLabel(row.HTTP.Status)
}

// torrentTransferring and torrentIdle mirror the daemon's definitions: only a
// torrent moving data (either direction) is active; an unfinished one at
// 0 B/s both ways is stuck, whatever its state says.
func torrentTransferring(torrent Torrent) bool {
	return torrent.DownloadRate > 0 || torrent.UploadRate > 0
}

func torrentIdle(torrent Torrent) bool {
	if torrentTransferring(torrent) || torrent.Progress >= 100 {
		return false
	}
	switch strings.ToLower(torrent.State) {
	case "downloading", "downloading_metadata", "stalled", "checking_files", "checking_resume_data":
		return true
	}
	return false
}

// rowStyle colours a list row; a stuck download is highlighted as a warning.
func downloadRowStyle(row DownloadRow) Style {
	if row.Torrent != nil && torrentIdle(*row.Torrent) {
		return StyleWarn
	}
	_, state, _ := downloadRowFields(row)
	return torrentStyle(state)
}

// downloadRowSizeLabel is the total size, or what is known of it.
func downloadRowSizeLabel(row DownloadRow) string {
	if size := downloadRowSize(row); size > 0 {
		return HumanBytes(float64(size))
	}
	return "-"
}

// downloadRowETA is the time left while downloading, or the share ratio of a
// complete torrent ("r 1.25").
func downloadRowETA(row DownloadRow) string {
	if row.HTTP != nil {
		if row.HTTP.ETASeconds != nil {
			return HumanDuration(float64(*row.HTTP.ETASeconds))
		}
		return "-"
	}
	torrent := row.Torrent
	if torrent == nil {
		return "-"
	}
	if torrent.Progress >= 100 {
		base := torrent.AllTimeDownload
		if base == 0 {
			base = torrent.TotalSize
		}
		if base == 0 {
			return "-"
		}
		return fmt.Sprintf("r %.2f", float64(torrent.AllTimeUpload)/float64(base))
	}
	if torrent.DownloadRate == 0 || torrent.TotalSize <= torrent.TotalDone {
		return "∞"
	}
	return HumanDuration(float64(torrent.TotalSize-torrent.TotalDone) / float64(torrent.DownloadRate))
}

// downloadRowSwarm is "seeds/peers" connected to a torrent.
func downloadRowSwarm(row DownloadRow) string {
	if row.Torrent == nil {
		return "-"
	}
	return fmt.Sprintf("%d/%d", row.Torrent.NumSeeds, row.Torrent.NumPeers)
}

func downloadRowValues(row DownloadRow) (done uint64, down, up float64) {
	if row.Torrent != nil {
		return row.Torrent.TotalDone, float64(row.Torrent.DownloadRate), float64(row.Torrent.UploadRate)
	}
	if row.HTTP != nil {
		return row.HTTP.DownloadedBytes, float64(row.HTTP.SpeedBytes), 0
	}
	return 0, 0, 0
}

func downloadDetailLine(tr *Translator, row DownloadRow) string {
	done, down, up := downloadRowValues(row)
	if row.HTTP != nil {
		total := "-"
		if row.HTTP.TotalBytes != nil {
			total = HumanBytes(float64(*row.HTTP.TotalBytes))
		}
		return fmt.Sprintf("%s %s/%s · %s %s · %s", tr.T("label.done"), HumanBytes(float64(done)), total, tr.T("label.down"), HumanRate(down), row.HTTP.Method)
	}
	return fmt.Sprintf("%s %s · %s %s · %s %s", tr.T("label.done"), HumanBytes(float64(done)), tr.T("label.down"), HumanRate(down), tr.T("label.up"), HumanRate(up))
}

func torrentStyle(state string) Style {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "error":
		return StyleErr
	case "stalled", "checking_files", "checking_resume_data":
		return StyleWarn
	case "seeding", "finished":
		return StyleOK
	case "paused":
		return StyleMuted
	default:
		return StyleNormal
	}
}

func (m *Model) renderLogs(width, contentHeight int) []Line {
	lines := m.FilteredLogs()
	header := fmt.Sprintf("%s: %d %s", m.Tr.T("tab.logs"), len(lines), m.Tr.T("label.lines"))
	if m.LogFilter != "" {
		header += fmt.Sprintf(" · %s '%s'", m.Tr.T("label.filter"), m.LogFilter)
	}
	if m.LogProblemsOnly {
		header += " · " + m.Tr.T("label.problemsonly")
	}
	if m.LogFollow {
		if m.LogStreamConnected {
			header += " · " + m.Tr.T("label.live")
		} else {
			header += " · " + m.Tr.T("label.follow")
		}
	}
	out := []Line{{Text: header, Style: StyleHeader}}
	visible := max(1, contentHeight-1)
	if len(lines) == 0 {
		return out
	}
	// Long lines wrap, so the window is measured in screen rows: walk back
	// from the newest visible entry until the rows are full. LogScroll still
	// counts entries, so following and scrolling keep their meaning.
	wrapped := func(entry string) []Line {
		return expandLines([]Line{{Text: entry, Style: logStyle(entry), Wrap: true, Indent: 2}}, width)
	}
	firstPage, rows := 0, 0
	for firstPage < len(lines) && rows < visible {
		rows += len(wrapped(lines[firstPage]))
		firstPage++
	}
	m.LogScroll = min(max(m.LogScroll, 0), max(0, len(lines)-firstPage))
	end := len(lines) - m.LogScroll
	window := []Line{}
	for index := end - 1; index >= 0 && len(window) < visible; index-- {
		window = append(wrapped(lines[index]), window...)
	}
	if len(window) > visible {
		window = window[len(window)-visible:]
	}
	return append(out, window...)
}

func catalogWindow(selected, scroll, length, visible int) (int, int) {
	visible = max(1, visible)
	maxScroll := max(0, length-visible)
	start := min(max(scroll, 0), maxScroll)
	if selected < start {
		start = selected
	}
	if selected >= start+visible {
		start = selected - visible + 1
	}
	start = min(max(start, 0), maxScroll)
	return start, min(length, start+visible)
}

// selectedRows wraps the highlighted entry so its full title is readable,
// keeping at most maxExtra continuation rows. The other entries stay cut to
// one row each, so scrolling still moves by entries.
func selectedRows(text string, width, indent, maxExtra int) []string {
	rows := WrapText(Sanitize(text), width, indent)
	if extra := min(max(maxExtra, 0), 3); len(rows) > extra+1 {
		rows = rows[:extra+1]
		rows[extra] = Shorten(rows[extra]+" …", width)
	}
	return rows
}

// catalogRows renders a selectable list with the selected entry expanded.
func catalogRows(texts []string, selected int, scroll *int, height, width int) []Line {
	return styledRows(texts, nil, selected, scroll, height, width)
}

// styledRows is catalogRows with a style for each entry that is not selected.
func styledRows(texts []string, styles []Style, selected int, scroll *int, height, width int) []Line {
	if len(texts) == 0 {
		return nil
	}
	height = max(1, height)
	selected = min(max(selected, 0), len(texts)-1)
	expanded := selectedRows("> "+texts[selected], width, 4, height-2)
	start, end := catalogWindow(selected, *scroll, len(texts), height-(len(expanded)-1))
	*scroll = start
	lines := make([]Line, 0, end-start+len(expanded))
	for index := start; index < end; index++ {
		if index == selected {
			for _, row := range expanded {
				lines = append(lines, Line{Text: row, Style: StyleSelected})
			}
			continue
		}
		style := StyleNormal
		if index < len(styles) {
			style = styles[index]
		}
		lines = append(lines, Line{Text: "  " + texts[index], Style: style})
	}
	return lines
}

func (m *Model) renderArchive(width, contentHeight int) []Line {
	sortNames := []string{m.Tr.T("sort.archive_title"), m.Tr.T("sort.archive_source"), m.Tr.T("sort.archive_quality"), m.Tr.T("sort.archive_added")}
	direction := "↑"
	if m.ArchiveSortDesc {
		direction = "↓"
	}
	page := max(1, m.ArchivePage)
	pages := max(1, m.ArchivePages)
	title := fmt.Sprintf("%s: %d · %s %d/%d · %s %s%s", m.Tr.T("tab.archive"), m.ArchiveTotal,
		m.Tr.T("label.page"), page, pages, m.Tr.T("label.sort"), sortNames[m.ArchiveSort], direction)
	if m.ArchiveFilter != "" {
		title += fmt.Sprintf(" · %s '%s'", m.Tr.T("label.filter"), m.ArchiveFilter)
	}
	lines := []Line{{Text: title, Style: StyleHeader}}
	items := m.VisibleArchive()
	if len(items) == 0 {
		return append(lines, Line{Text: m.Tr.T("msg.emptyarchive"), Style: StyleMuted})
	}
	texts := make([]string, len(items))
	for index, entry := range items {
		texts[index] = fmt.Sprintf("%s · %s · %s", entry.Title, entry.Source, firstNonEmpty(entry.AddedAt, "-"))
	}
	return append(lines, catalogRows(texts, m.ArchiveSelected, &m.ArchiveScroll, contentHeight-1, width)...)
}

func (m *Model) renderArchiveDetail(width int) []Line {
	if m.ArchiveDetail == nil {
		return nil
	}
	entry := m.ArchiveDetail
	rows := [][2]string{
		{m.Tr.T("label.archiveid"), fmt.Sprintf("%d", entry.ID)},
		{m.Tr.T("label.name"), entry.Title},
		{m.Tr.T("label.source"), firstNonEmpty(entry.Source, "-")},
		{m.Tr.T("label.quality"), fmt.Sprintf("%d", entry.QualityScore)},
		{m.Tr.T("label.added"), firstNonEmpty(entry.AddedAt, "-")},
		{m.Tr.T("label.magnet"), firstNonEmpty(entry.Magnet, "-")},
	}
	lines := []Line{{Text: entry.Title, Style: StyleHeader, Wrap: true}}
	start := min(m.ArchiveDetailScroll, max(0, len(rows)-1))
	for _, row := range rows[start:] {
		lines = append(lines, Line{Text: PadRight(row[0]+":", 20) + " " + row[1], Style: StyleNormal, Wrap: true, Indent: 21})
	}
	return lines
}

func (m *Model) renderMissing(width, contentHeight int) []Line {
	lines := []Line{{Text: fmt.Sprintf("%s: %d", m.Tr.T("tab.missing"), len(m.Missing)), Style: StyleHeader}}
	if len(m.Missing) == 0 {
		return append(lines, Line{Text: m.Tr.T("msg.emptymissing"), Style: StyleMuted})
	}
	texts := make([]string, len(m.Missing))
	for index, gap := range m.Missing {
		texts[index] = fmt.Sprintf("%s · S%02dE%02d · %s", gap.Series, gap.Season, gap.Episode, firstNonEmpty(gap.AirDate, "-"))
	}
	return append(lines, catalogRows(texts, m.MissingSelected, &m.MissingScroll, contentHeight-1, width)...)
}

func (m *Model) renderBlocklist(width, contentHeight int) []Line {
	lines := []Line{{Text: fmt.Sprintf("%s: %d", m.Tr.T("tab.blocklist"), len(m.Blocklist)), Style: StyleHeader}}
	if len(m.Blocklist) == 0 {
		return append(lines, Line{Text: m.Tr.T("msg.emptyblocklist"), Style: StyleMuted})
	}
	texts := make([]string, len(m.Blocklist))
	for index, entry := range m.Blocklist {
		texts[index] = fmt.Sprintf("%s · %s · %s", entry.Title, firstNonEmpty(entry.Reason, "-"), entry.CreatedAt)
	}
	return append(lines, catalogRows(texts, m.BlocklistSelected, &m.BlocklistScroll, contentHeight-1, width)...)
}

func (m *Model) renderLibrary(width, contentHeight int) []Line {
	kind := m.Tr.T("library.series")
	if m.Library == LibraryMovies {
		kind = m.Tr.T("library.movies")
	}
	if m.Library == LibraryComics {
		kind = m.Tr.T("library.comics")
	}
	items := m.VisibleLibraryRows()
	title := fmt.Sprintf("%s · %s: %d", m.Tr.T("tab.library"), kind, len(items))
	if m.LibraryFilter != "" {
		title += " · " + m.Tr.T("label.filter") + " '" + m.LibraryFilter + "'"
	}
	// A sub-tab bar: inside the library the digits pick series, movies or
	// comics, not the main tabs, and this must be visible.
	kinds := []struct {
		kind  LibraryKind
		label string
		count int
	}{
		{LibrarySeries, m.Tr.T("library.series"), len(m.Series)},
		{LibraryMovies, m.Tr.T("library.movies"), len(m.Movies)},
		{LibraryComics, m.Tr.T("library.comics"), len(m.Comics)},
	}
	bar := make([]string, len(kinds))
	for index, item := range kinds {
		label := fmt.Sprintf("%d %s (%d)", index+1, item.label, item.count)
		if item.kind == m.Library {
			bar[index] = "[" + label + "]"
		} else {
			bar[index] = " " + label + " "
		}
	}
	lines := []Line{{Text: strings.Join(bar, "  ") + "   " + m.Tr.T("library.switch"), Style: StyleSelected}, {Text: title, Style: StyleHeader}}
	if len(items) == 0 {
		return append(lines, Line{Text: m.Tr.T("msg.emptylibrary"), Style: StyleMuted})
	}
	texts := make([]string, len(items))
	for index, item := range items {
		state := m.Tr.T("label.disabled")
		if item.Enabled {
			state = m.Tr.T("label.enabled")
		}
		texts[index] = fmt.Sprintf("%s %s · %s", PadRight(state, 4), item.Name, firstNonEmpty(item.Meta, "-"))
	}
	return append(lines, catalogRows(texts, m.LibrarySelected, &m.LibraryScroll, contentHeight-len(lines), width)...)
}

func (m *Model) renderHealth(width int) []Line {
	if m.Health == nil {
		return []Line{{Text: m.Tr.T("msg.loading"), Style: StyleMuted}}
	}
	health := m.Health
	status := strings.ToUpper(health.Status)
	if status == "" {
		status = "OFFLINE"
	}
	style := StyleOK
	if !strings.EqualFold(health.Status, "ok") {
		style = StyleWarn
	}
	label := fmt.Sprintf("%s: %s", m.Tr.T("label.health"), status)
	if m.HealthErr != "" {
		label += " (" + m.Tr.T("msg.refreshed") + ": " + m.HealthErr + ")"
	}
	lines := []Line{{Text: label, Style: style, Wrap: true, Indent: 2}}
	lines = append(lines,
		Line{Text: fmt.Sprintf("%s: PID %d · %s %s · %s %s · %s %s", m.Tr.T("label.daemon"),
			health.ProcessID, m.Tr.T("label.uptime"), HumanDuration(float64(health.ProcessUptimeSeconds)),
			m.Tr.T("label.cpu"), OptionalNumber(floatPointerValue(health.ProcessCPUPercent), "%", 1),
			m.Tr.T("label.ram"), HumanBytes(float64(health.ResidentBytes))), Style: StyleNormal},
		Line{Text: fmt.Sprintf("%s: %s %s / %s · %s %s", m.Tr.T("label.disks"),
			HumanBytes(float64(health.DiskFreeBytes)), m.Tr.T("label.free"),
			HumanBytes(float64(health.DiskTotalBytes)),
			m.Tr.T("label.trash"), fmt.Sprintf("%d %s (%s)", health.TrashFileCount, strings.ToLower(m.Tr.T("label.files")), HumanBytes(float64(health.TrashBytes)))), Style: StyleNormal},
	)
	if len(health.Paths) > 0 {
		lines = append(lines, Line{Text: m.Tr.T("label.paths"), Style: StyleHeader})
		for _, path := range health.Paths {
			good := path.Exists && path.Writable
			state := "FAIL"
			pathStyle := StyleErr
			if good {
				state = "OK"
				pathStyle = StyleOK
			}
			lines = append(lines, Line{Text: fmt.Sprintf("%-4s %s %s", state, PadRight(m.pathLabel(path.Label), 14), path.Path), Style: pathStyle, Wrap: true, Indent: 20})
		}
	}
	if len(health.Disks) > 0 {
		lines = append(lines, Line{Text: m.Tr.T("label.disks"), Style: StyleHeader})
		for _, disk := range health.Disks {
			used := 0.0
			if disk.TotalBytes > 0 {
				used = (1 - float64(disk.FreeBytes)/float64(disk.TotalBytes)) * 100
			}
			lines = append(lines, Line{Text: fmt.Sprintf("%s %s %s / %s · %.0f%% %s",
				PadRight(disk.Mount, 18), HumanBytes(float64(disk.FreeBytes)), m.Tr.T("label.free"), HumanBytes(float64(disk.TotalBytes)), used, m.Tr.T("label.used")), Style: StyleNormal, Wrap: true, Indent: 19})
		}
	}
	if health.Ramdisk != nil {
		lines = append(lines, Line{Text: fmt.Sprintf("%s: %s · %s %s / %s", m.Tr.T("label.ramdisk"),
			health.Ramdisk.Path, HumanBytes(float64(health.Ramdisk.FreeBytes)), m.Tr.T("label.free"), HumanBytes(float64(health.Ramdisk.TotalBytes))), Style: StyleNormal, Wrap: true, Indent: 2})
	}
	if len(health.LastErrors) > 0 {
		lines = append(lines, Line{Text: m.Tr.T("label.recenterror"), Style: StyleErr})
		for _, message := range health.LastErrors {
			lines = append(lines, Line{Text: message, Style: StyleErr, Wrap: true, Indent: 2})
		}
	}
	return lines
}

// pathLabel translates the daemon's path names (sent in English).
func (m *Model) pathLabel(label string) string {
	key := "path." + strings.ToLower(strings.TrimSpace(label))
	if translated := m.Tr.T(key); translated != key {
		return translated
	}
	return label
}

func floatPointerValue(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func (m *Model) renderDetail(width, contentHeight int) []Line {
	if m.Detail == nil {
		return nil
	}
	torrent := m.Detail.Torrent
	if m.DetailView == DetailFiles || m.DetailView == DetailTrackers {
		return m.renderDetailSelectable(width, contentHeight)
	}
	if m.DetailView != DetailGeneral {
		labels := map[DetailView]string{DetailTrackers: m.Tr.T("label.trackers"), DetailFiles: m.Tr.T("label.files"), DetailPeers: m.Tr.T("label.peers")}
		lines := []Line{{Text: fmt.Sprintf("%s: %d", labels[m.DetailView], len(m.DetailItems)), Style: StyleHeader}}
		rendered := make([]Line, 0, len(m.DetailItems))
		for _, item := range m.DetailItems {
			switch m.DetailView {
			case DetailTrackers:
				rendered = append(rendered, Line{Text: fmt.Sprintf("tier %s  %s", OptionalNumber(item["tier"], "", 0), stringValue(item["url"])), Style: StyleNormal, Wrap: true, Indent: 4})
			case DetailFiles:
				rendered = append(rendered, Line{Text: fmt.Sprintf("%s / %s  %s",
					HumanBytes(numberValue(item["downloaded"])), HumanBytes(numberValue(item["size"])), stringValue(item["path"])), Style: StyleNormal, Wrap: true, Indent: 4})
			default:
				seed := ""
				if truthy(item["seed"]) {
					seed = "seed"
				}
				rendered = append(rendered, Line{Text: fmt.Sprintf("%s  %s  %s %s  %s %s  %s",
					PadRight(stringValue(item["address"]), 22), PadRight(stringValue(item["client"]), 20),
					m.Tr.T("label.down"), HumanRate(numberValue(item["download_rate"])),
					m.Tr.T("label.up"), HumanRate(numberValue(item["upload_rate"])), seed), Style: StyleNormal})
			}
		}
		start := min(m.DetailScroll, max(0, len(rendered)-1))
		if start < len(rendered) {
			lines = append(lines, rendered[start:]...)
		}
		return lines
	}
	seedLimit := m.Tr.T("label.infinite")
	if torrent.SeedRatio != nil && torrent.SeedDays != nil && (*torrent.SeedRatio != 0 || *torrent.SeedDays != 0) {
		seedLimit = fmt.Sprintf("ratio %s · %s", OptionalNumber(*torrent.SeedRatio, "", 2), OptionalNumber(*torrent.SeedDays, "", 0))
	}
	metadata := m.Tr.T("label.no")
	if torrent.HasMetadata {
		metadata = m.Tr.T("label.yes")
	}
	boolLabel := func(value bool) string {
		if value {
			return m.Tr.T("label.yes")
		}
		return m.Tr.T("label.no")
	}
	rows := [][2]string{
		{m.Tr.T("label.name"), torrent.Name},
		{m.Tr.T("label.hash"), torrent.Hash},
		{m.Tr.T("label.state"), m.Tr.StateLabel(torrent.State)},
		{m.Tr.T("label.progress"), fmt.Sprintf("%.1f%%", torrent.Progress)},
		{m.Tr.T("label.size"), HumanBytes(float64(torrent.TotalSize))},
		{m.Tr.T("label.downloaded"), HumanBytes(float64(torrent.TotalDone))},
		{m.Tr.T("label.alltime"), HumanBytes(float64(torrent.AllTimeDownload)) + " ↓ · " + HumanBytes(float64(torrent.AllTimeUpload)) + " ↑"},
		{m.Tr.T("label.rates"), HumanRate(float64(torrent.DownloadRate)) + " ↓ · " + HumanRate(float64(torrent.UploadRate)) + " ↑"},
		{m.Tr.T("label.peersseeds"), fmt.Sprintf("%d / %d", torrent.NumPeers, torrent.NumSeeds)},
		{m.Tr.T("label.queuepos"), fmt.Sprintf("%d", torrent.QueuePosition)},
		{m.Tr.T("label.seedlimit"), seedLimit},
		{m.Tr.T("label.torrentlimits"), kibLabel(m.Tr, torrent.DownloadLimit/1024) + " ↓ · " + kibLabel(m.Tr, torrent.UploadLimit/1024) + " ↑"},
		{m.Tr.T("label.tag"), firstNonEmpty(m.TorrentTags[torrent.Hash], "-")},
		{m.Tr.T("label.superseeding"), boolLabel(torrent.SuperSeeding)},
		{m.Tr.T("label.metadata"), metadata},
		{m.Tr.T("label.version2"), torrent.TorrentVersion},
		{m.Tr.T("label.automanaged"), boolLabel(torrent.AutoManaged)},
		{m.Tr.T("label.norename"), boolLabel(m.Detail.NoRename)},
		{m.Tr.T("label.archived"), boolLabel(torrent.Archived)},
		{m.Tr.T("label.source"), firstNonEmpty(torrent.Source, "-")},
		{m.Tr.T("label.reason"), firstNonEmpty(torrent.Reason, "-")},
		{m.Tr.T("label.savepath"), firstNonEmpty(torrent.SavePath, "-")},
		{m.Tr.T("label.magnet"), firstNonEmpty(m.Detail.Magnet, "-")},
	}
	lines := []Line{{Text: torrent.Name, Style: StyleHeader, Wrap: true}}
	start := min(m.DetailScroll, max(0, len(rows)-1))
	for _, row := range rows[start:] {
		lines = append(lines, Line{Text: PadRight(row[0]+":", 20) + " " + row[1], Style: StyleNormal})
	}
	return lines
}

// renderDetailSelectable lists the files or trackers of the open torrent
// with a cursor, so a file priority or a tracker can be changed.
func (m *Model) renderDetailSelectable(width, contentHeight int) []Line {
	label := m.Tr.T("label.files")
	if m.DetailView == DetailTrackers {
		label = m.Tr.T("label.trackers")
	}
	lines := []Line{{Text: fmt.Sprintf("%s · %s: %d", Shorten(m.Detail.Torrent.Name, max(10, width/2)), label, len(m.DetailItems)), Style: StyleHeader}}
	if len(m.DetailItems) == 0 {
		return append(lines, Line{Text: m.Tr.T("msg.loading"), Style: StyleMuted})
	}
	texts := make([]string, len(m.DetailItems))
	styles := make([]Style, len(m.DetailItems))
	for index, item := range m.DetailItems {
		styles[index] = StyleNormal
		if m.DetailView == DetailTrackers {
			texts[index] = joinNonEmpty(fmt.Sprintf("tier %s", OptionalNumber(item["tier"], "", 0)), stringValue(item["url"]), stringValue(item["message"]))
			if numberValue(item["fails"]) > 0 {
				styles[index] = StyleWarn
			}
			continue
		}
		priority := int(numberValue(item["priority"]))
		if priority == 0 {
			styles[index] = StyleMuted
		}
		size := numberValue(item["size"])
		percent := 0.0
		if size > 0 {
			percent = numberValue(item["downloaded"]) * 100 / size
		}
		texts[index] = fmt.Sprintf("%-8s %5.1f%% %9s  %s", m.priorityLabel(priority), percent, HumanBytes(size), stringValue(item["path"]))
	}
	return append(lines, styledRows(texts, styles, m.DetailSelected, &m.DetailScroll, contentHeight-1, width)...)
}

// priorityLabel names a libtorrent file priority (0 skip … 7 top).
func (m *Model) priorityLabel(priority int) string {
	switch {
	case priority <= 0:
		return m.Tr.T("priority.skip")
	case priority < 4:
		return m.Tr.T("priority.low")
	case priority == 4:
		return m.Tr.T("priority.normal")
	case priority < 7:
		return m.Tr.T("priority.high")
	default:
		return m.Tr.T("priority.top")
	}
}

func (m *Model) renderHelp(width int) []Line {
	keys := []string{
		"help.title", "", "help.global", "help.global2", "help.torrents", "help.torrents2",
		"help.torrents3", "help.torrents4", "help.details", "help.status", "help.logs", "help.health", "help.settings", "help.archive", "help.missing", "help.blocklist", "help.library", "help.series", "help.movie", "help.maintenance", "help.form", "help.terminal", "help.bandwidth", "",
		"help.close",
	}
	lines := make([]Line, 0, len(keys))
	for _, key := range keys {
		if key == "" {
			lines = append(lines, Line{})
			continue
		}
		style := StyleNormal
		if key == "help.title" {
			style = StyleHeader
		}
		for _, wrapped := range WrapText(m.Tr.T(key), width, 10) {
			lines = append(lines, Line{Text: wrapped, Style: style})
		}
	}
	return lines
}

func (m *Model) renderSearch(width, contentHeight int) []Line {
	lines := []Line{{Text: m.Tr.Format("msg.search", len(m.SearchResults)) + fmt.Sprintf("  '%s'", m.SearchQuery), Style: StyleHeader}}
	if len(m.SearchResults) == 0 {
		lines = append(lines, Line{Text: m.Tr.T("msg.nosearch"), Style: StyleMuted})
		return lines
	}
	texts := make([]string, len(m.SearchResults))
	for index, result := range m.SearchResults {
		if m.SearchKind != SearchReleases {
			texts[index] = m.tmdbResultText(result)
			continue
		}
		parts := []string{stringValue(result["title"]), stringValue(result["source"]), qualityLabel(result["quality"])}
		if _, ok := result["score"]; ok {
			parts = append(parts, m.Tr.Format("label.score", int64(numberValue(result["score"]))))
		}
		if size := numberValue(result["size_bytes"]); size > 0 {
			parts = append(parts, HumanBytes(size))
		}
		if seeders := numberValue(result["seeders"]); seeders > 0 {
			parts = append(parts, m.Tr.Format("label.seeders", int64(seeders)))
		}
		texts[index] = joinNonEmpty(parts...)
	}
	return append(lines, catalogRows(texts, m.SearchSelected, &m.SearchScroll, contentHeight-1, width)...)
}

func (m *Model) renderEvents(width, contentHeight int) []Line {
	lines := []Line{{Text: m.Tr.Format("msg.events", len(m.Events)), Style: StyleHeader}}
	if len(m.Events) == 0 {
		return lines
	}
	visible := max(1, contentHeight-1)
	maxScroll := max(0, len(m.Events)-visible)
	m.EventScroll = min(max(m.EventScroll, 0), maxScroll)
	end := min(len(m.Events), m.EventScroll+visible)
	for _, event := range m.Events[m.EventScroll:end] {
		line := fmt.Sprintf("%s %s", PadRight(event.Kind, 22), firstNonEmpty(event.Name, event.Hash))
		if event.SavePath != "" {
			line += " · " + event.SavePath
		}
		lines = append(lines, Line{Text: line, Style: StyleNormal, Wrap: true, Indent: 23})
	}
	return lines
}

func (m *Model) renderSettings(width, contentHeight int) []Line {
	lines := []Line{{Text: m.Tr.T("settings.title"), Style: StyleHeader}}
	if m.Config == nil {
		return append(lines, Line{Text: m.Tr.T("msg.loading"), Style: StyleMuted})
	}
	config := m.Config
	rows := []string{
		fmt.Sprintf("%d  %s: %s", 1, m.Tr.T("settings.language"), m.Tr.T("settings.uilang")),
		fmt.Sprintf("%d  %s: %s", 2, m.Tr.T("settings.refresh"), HumanDuration(float64(config.RefreshSecs))),
		fmt.Sprintf("%d  %s: %s / %s", 3, m.Tr.T("settings.limits"), kibLabel(m.Tr, int64(config.DownloadLimitKib)), kibLabel(m.Tr, int64(config.UploadLimitKib))),
		fmt.Sprintf("%d  %s: %s", 4, m.Tr.T("settings.dryrun"), boolWord(m.Tr, config.DryRun)),
		fmt.Sprintf("%d  %s: %s", 5, m.Tr.T("settings.colors"), boolWord(m.Tr, m.ColorsEnabled)),
		fmt.Sprintf("%d  %s: %s", 6, m.Tr.T("settings.contrast"), boolWord(m.Tr, m.HighContrast)),
	}
	for index, row := range rows {
		style := StyleNormal
		if index == m.SettingsSelected {
			style = StyleSelected
		}
		lines = append(lines, Line{Text: row, Style: style})
	}
	if contentHeight > len(lines) {
		lines = append(lines, Line{Text: m.Tr.T("settings.hint"), Style: StyleMuted})
	}
	return lines
}

func boolWord(tr *Translator, value bool) string {
	if value {
		return tr.T("label.yes")
	}
	return tr.T("label.no")
}

func qualityLabel(value any) string {
	quality, ok := value.(map[string]any)
	if !ok {
		return "-"
	}
	if resolution := stringValue(quality["resolution"]); resolution != "" {
		return resolution
	}
	if source := stringValue(quality["source"]); source != "" {
		return source
	}
	return "-"
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(typed)
	}
}

func numberValue(value any) float64 {
	number, _ := toFloat(value)
	return number
}

func truthy(value any) bool {
	if flag, ok := value.(bool); ok {
		return flag
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (m *Model) truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	return Shorten(Sanitize(value), width)
}

// expandLines breaks Wrap lines into terminal-wide rows. Text is sanitized
// first so the width is measured on what the terminal will really show.
func expandLines(lines []Line, width int) []Line {
	expanded := make([]Line, 0, len(lines))
	for _, line := range lines {
		if !line.Wrap {
			expanded = append(expanded, line)
			continue
		}
		for _, row := range WrapText(Sanitize(line.Text), width, line.Indent) {
			expanded = append(expanded, Line{Text: row, Style: line.Style})
		}
	}
	return expanded
}

// footerWithMeter keeps the bandwidth meter at the right of the footer and
// shortens the message to make room. Only on very narrow windows, where the
// message would be left with fewer than 20 cells, does the meter give way.
func (m *Model) footerWithMeter(message string, width int) string {
	meter := Sanitize(m.Bandwidth)
	room := width - StringWidth(meter) - 2
	if meter == "" || room < 20 {
		return m.truncate(message, width)
	}
	message = m.truncate(message, room)
	return message + strings.Repeat(" ", width-StringWidth(message)-StringWidth(meter)) + meter
}

// pageWindow shows the part of a free-form page selected by PageScroll and
// says how much is left below, since a small SSH window may not fit it all.
func (m *Model) pageWindow(lines []Line, height int) []Line {
	if height <= 0 || len(lines) <= height {
		m.PageScroll = 0
		return lines
	}
	m.PageScroll = min(max(m.PageScroll, 0), len(lines)-height)
	start, end := m.PageScroll, m.PageScroll+height
	if end >= len(lines) {
		return lines[start:]
	}
	end--
	window := append([]Line(nil), lines[start:end]...)
	return append(window, Line{Text: m.Tr.Format("status.more", len(lines)-end), Style: StyleMuted})
}

// promptLine renders the active prompt. A buffer longer than the screen (a
// pasted magnet easily is) scrolls horizontally so the cursor stays visible.
func (m *Model) promptLine(width int) string {
	label := Sanitize(m.PromptLabel())
	buffer := []rune(Sanitize(m.Prompt.Buffer))
	start := m.promptStart(width)
	text := label + string(buffer[start:])
	if start > 0 {
		text = label + "…" + string(buffer[start+1:])
	}
	return Shorten(text, width)
}

func (m *Model) promptCursorColumn(width int) int {
	label := Sanitize(m.PromptLabel())
	buffer := []rune(Sanitize(m.Prompt.Buffer))
	cursor := min(max(m.Prompt.Cursor, 0), len(buffer))
	start := m.promptStart(width)
	return StringWidth(label) + StringWidth(string(buffer[start:cursor]))
}

// promptStart is the first buffer rune shown so that the cursor fits.
func (m *Model) promptStart(width int) int {
	label := Sanitize(m.PromptLabel())
	buffer := []rune(Sanitize(m.Prompt.Buffer))
	cursor := min(max(m.Prompt.Cursor, 0), len(buffer))
	room := width - StringWidth(label) - 1
	if room < 4 {
		return 0
	}
	start := 0
	for StringWidth(string(buffer[start:cursor])) > room && start < cursor {
		start++
	}
	return start
}
