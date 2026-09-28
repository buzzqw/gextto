package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const tabCount = 7

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
	switch {
	case m.Overlay == OverlayHelp:
		content = m.renderHelp(width)
	case m.Overlay == OverlaySearch:
		content = m.renderSearch(width, contentHeight)
	case m.Overlay == OverlayEvents:
		content = m.renderEvents(width, contentHeight)
	case m.Overlay == OverlaySettings:
		content = m.renderSettings(width, contentHeight)
	case m.Detail != nil:
		content = m.renderDetail(width)
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
	default:
		content = m.renderHealth(width)
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
		label := m.PromptLabel()
		lines = append(lines, Line{Text: m.truncate(label+m.Prompt.Buffer, width), Style: StyleSelected})
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
		lines = append(lines, Line{Text: m.truncate(message, width), Style: footerStyle})
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
		screen.CursorCol = len([]rune(m.PromptLabel())) + m.Prompt.Cursor
		if screen.CursorCol >= width {
			screen.CursorCol = width - 1
		}
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
	labels := []string{m.Tr.T("tab.status"), m.Tr.T("tab.torrents"), m.Tr.T("tab.logs"), m.Tr.T("tab.health"), m.Tr.T("tab.archive"), m.Tr.T("tab.missing"), m.Tr.T("tab.blocklist")}
	parts := make([]string, 0, tabCount)
	for index, label := range labels {
		if Tab(index) == m.Tab {
			parts = append(parts, fmt.Sprintf("[%d:%s]", index+1, label))
		} else {
			parts = append(parts, fmt.Sprintf(" %d:%s ", index+1, label))
		}
	}
	full := "  " + strings.Join(parts, " ") + "  (" + m.Tr.T("hint.tabs") + ")"
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
	hints := m.Tr.T("hint.global")
	switch {
	case m.Overlay == OverlaySettings:
		hints += " · " + m.Tr.T("hint.settings")
	case m.Detail != nil:
		hints += " · " + m.Tr.T("hint.details")
	case m.ArchiveDetail != nil:
		hints += " · " + m.Tr.T("hint.archivedetail")
	case m.Tab == TabTorrents:
		hints += " · " + m.Tr.T("hint.torrents")
	case m.Tab == TabLogs:
		hints += " · " + m.Tr.T("hint.logs")
	case m.Tab == TabHealth:
		hints += " · " + m.Tr.T("hint.health")
	case m.Tab == TabArchive:
		hints += " · " + m.Tr.T("hint.archive")
	case m.Tab == TabMissing:
		hints += " · " + m.Tr.T("hint.missing")
	case m.Tab == TabBlocklist:
		hints += " · " + m.Tr.T("hint.blocklist")
	}
	return hints
}

func (m *Model) renderError() []Line {
	return []Line{
		{Text: m.Tr.Format("msg.offline", m.Err), Style: StyleErr},
		{Text: m.Tr.T("msg.sethint"), Style: StyleMuted},
	}
}

func (m *Model) renderStatus(width, contentHeight int) []Line {
	if m.Status == nil {
		return []Line{{Text: m.Tr.T("msg.loading"), Style: StyleMuted}}
	}
	status := m.Status
	mode := m.Tr.T("mode.standby")
	switch {
	case status.DryRun:
		mode = m.Tr.T("mode.dryrun")
	case status.Active:
		mode = m.Tr.T("mode.active")
	}
	lines := []Line{
		{Text: fmt.Sprintf("%s: %s", m.Tr.T("label.mode"), mode), Style: StyleNormal},
		{Text: fmt.Sprintf("%s: %d (%d %s, %d %s, %d %s)", m.Tr.T("label.torrents"),
			status.TorrentStats.Count, status.TorrentStats.Downloading, m.Tr.T("label.downloading"),
			status.TorrentStats.Queued, m.Tr.T("label.queued"),
			status.TorrentStats.Seeding, m.Tr.T("label.seeding")), Style: StyleNormal},
		{Text: fmt.Sprintf("%s: %d", m.Tr.T("label.stalled"), status.TorrentStats.Stalled), Style: StyleMuted},
	}
	if m.Metrics != nil {
		lines = append(lines, m.renderMetricLines(width)...)
	}
	if status.NextCycleAt != nil {
		if remaining := nextCycleSeconds(*status.NextCycleAt); remaining >= 0 {
			lines = append(lines, Line{Text: fmt.Sprintf("%s: %s", m.Tr.T("label.nextcycle"), HumanDuration(float64(remaining))), Style: StyleNormal})
		}
	}
	cycle := status.LastCycle
	lines = append(lines,
		Line{Text: fmt.Sprintf("%s: %s %d | %s %d | %s %d | %s %d | %s %d",
			m.Tr.T("label.lastcycle"),
			m.Tr.T("label.scraped"), cycle.Scraped,
			m.Tr.T("label.candidates"), cycle.Candidates,
			m.Tr.T("label.downloads"), cycle.DownloadsStarted,
			m.Tr.T("label.gaps"), cycle.GapsFilled,
			m.Tr.T("label.errors"), cycle.Errors), Style: StyleNormal},
		Line{Text: fmt.Sprintf("%s: %s %d · %s %d · %s %d",
			m.Tr.T("label.seen"),
			m.Tr.T("label.groups"), status.Seen.Groups,
			m.Tr.T("label.movies"), status.Seen.Movies,
			m.Tr.T("label.series"), status.Seen.Series), Style: StyleMuted},
	)
	if contentHeight > 0 && len(lines) > contentHeight {
		lines = lines[:contentHeight]
	}
	return lines
}

func (m *Model) renderMetricLines(width int) []Line {
	metrics := m.Metrics.TorrentStats
	download, hasDownload := metricNumber(metrics, "dl_info_speed", "download_rate", "download_speed")
	upload, hasUpload := metricNumber(metrics, "up_info_speed", "upload_rate", "upload_speed")
	peers, hasPeers := metricNumber(metrics, "active_peers", "num_connections", "num_incoming_connections")

	transfer := make([]string, 0, 3)
	if hasDownload {
		transfer = append(transfer, m.Tr.T("label.down")+" "+HumanRate(download))
	}
	if hasUpload {
		transfer = append(transfer, m.Tr.T("label.up")+" "+HumanRate(upload))
	}
	if hasPeers {
		transfer = append(transfer, m.Tr.T("label.peers")+" "+fmt.Sprintf("%.0f", peers))
	}
	lines := make([]Line, 0, 2)
	if len(transfer) > 0 {
		if width < 72 {
			for _, value := range transfer {
				lines = append(lines, Line{Text: value, Style: StyleNormal})
			}
		} else {
			lines = append(lines, Line{Text: m.Tr.T("label.transfer") + ": " + strings.Join(transfer, " · "), Style: StyleNormal})
		}
	}
	if len(m.TransferHistory) > 1 {
		graphWidth := 24
		if width < 72 {
			graphWidth = 12
		}
		downloads := make([]float64, 0, len(m.TransferHistory))
		uploads := make([]float64, 0, len(m.TransferHistory))
		for _, sample := range m.TransferHistory {
			downloads = append(downloads, sample.Download)
			uploads = append(uploads, sample.Upload)
		}
		lines = append(lines, Line{Text: fmt.Sprintf("%s: ↓%s ↑%s", m.Tr.T("label.trend"), sparkline(downloads, graphWidth), sparkline(uploads, graphWidth)), Style: StyleMuted})
	}
	if consumption := m.Metrics.Consumption; consumption != nil {
		value := fmt.Sprintf("%s: 7d %s · 30d %s · %s %s",
			m.Tr.T("label.consumption"),
			HumanBytes(float64(consumption.Last7DaysBytes)),
			HumanBytes(float64(consumption.Last30DaysBytes)),
			m.Tr.T("label.total"), HumanBytes(float64(consumption.TotalBytes)))
		lines = append(lines, Line{Text: value, Style: StyleMuted})
		if len(consumption.Daily7d) > 1 {
			daily := make([]float64, 0, len(consumption.Daily7d))
			for _, item := range consumption.Daily7d {
				daily = append(daily, float64(item.Bytes))
			}
			lines = append(lines, Line{Text: m.Tr.T("label.consumption7d") + ": " + sparkline(daily, min(24, max(8, width-28))), Style: StyleMuted})
		}
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
	items := m.VisibleTorrents()
	sortName := []string{m.Tr.T("sort.name"), m.Tr.T("sort.progress"), m.Tr.T("sort.state"), m.Tr.T("sort.rate"), m.Tr.T("sort.size")}[m.Sort]
	direction := "↑"
	if m.SortDesc {
		direction = "↓"
	}
	title := fmt.Sprintf("%s: %d", m.Tr.T("tab.torrents"), len(items))
	if len(items) > 0 {
		title = fmt.Sprintf("%s: %d/%d", m.Tr.T("tab.torrents"), m.Selected+1, len(items))
	}
	title += fmt.Sprintf("  · %s %s%s", m.Tr.T("label.sort"), sortName, direction)
	if m.Filter != "" {
		title += fmt.Sprintf("  · %s '%s'", m.Tr.T("label.filter"), m.Filter)
	}
	lines := []Line{{Text: title, Style: StyleHeader}}
	if len(items) == 0 {
		lines = append(lines, Line{Text: m.Tr.T("msg.emptytorrents"), Style: StyleMuted})
		return lines
	}
	if width < 50 {
		visibleItems := max(1, contentHeight-1)
		maxScroll := max(0, len(items)-visibleItems)
		start := min(max(m.TorrentScroll, 0), maxScroll)
		if m.Selected < start {
			start = m.Selected
		}
		if m.Selected >= start+visibleItems {
			start = m.Selected - visibleItems + 1
		}
		m.TorrentScroll = min(max(start, 0), maxScroll)
		end := min(len(items), m.TorrentScroll+visibleItems)
		for index, torrent := range items[m.TorrentScroll:end] {
			index += m.TorrentScroll
			style := torrentStyle(torrent.State)
			marker := " "
			if index == m.Selected {
				style = StyleSelected
				marker = ">"
			}
			lines = append(lines, Line{Text: fmt.Sprintf("%s %s %.1f%% %s", marker, m.Tr.StateLabel(torrent.State), torrent.Progress, torrent.Name), Style: style})
		}
		return lines
	}
	if width < 74 {
		visibleItems := max(1, (contentHeight-1)/2)
		maxScroll := max(0, len(items)-visibleItems)
		start := min(max(m.TorrentScroll, 0), maxScroll)
		if m.Selected < start {
			start = m.Selected
		}
		if m.Selected >= start+visibleItems {
			start = m.Selected - visibleItems + 1
		}
		m.TorrentScroll = min(max(start, 0), maxScroll)
		end := min(len(items), m.TorrentScroll+visibleItems)
		selected := m.Selected - m.TorrentScroll
		for index, torrent := range items[m.TorrentScroll:end] {
			style := torrentStyle(torrent.State)
			if index == selected {
				style = StyleSelected
			}
			marker := " "
			if index == selected {
				marker = ">"
			}
			lines = append(lines, Line{Text: fmt.Sprintf("%s %s %.1f%% %s", marker, m.Tr.StateLabel(torrent.State), torrent.Progress, torrent.Name), Style: style})
			lines = append(lines, Line{Text: fmt.Sprintf("   %s · %s %s · %s %s · %s %s",
				Shorten(torrent.Hash, 12), m.Tr.T("label.done"), HumanBytes(float64(torrent.TotalDone)),
				m.Tr.T("label.down"), HumanRate(float64(torrent.DownloadRate)),
				m.Tr.T("label.up"), HumanRate(float64(torrent.UploadRate))), Style: StyleMuted})
		}
		return lines
	}
	nameWidth := max(10, width-72)
	header := fmt.Sprintf("  %s %s %s %s %s %s  %s",
		PadRight(m.Tr.T("label.hash"), 9), PadRight(m.Tr.T("label.state"), 14),
		PadLeft(m.Tr.T("label.progress"), 6), PadLeft(m.Tr.T("label.done"), 10),
		PadLeft(m.Tr.T("label.down"), 11), PadLeft(m.Tr.T("label.up"), 11), m.Tr.T("label.name"))
	lines = append(lines, Line{Text: header, Style: StyleHeader})
	visibleItems := max(1, contentHeight-len(lines))
	maxScroll := max(0, len(items)-visibleItems)
	start := min(max(m.TorrentScroll, 0), maxScroll)
	if m.Selected < start {
		start = m.Selected
	}
	if m.Selected >= start+visibleItems {
		start = m.Selected - visibleItems + 1
	}
	m.TorrentScroll = min(max(start, 0), maxScroll)
	end := min(len(items), m.TorrentScroll+visibleItems)
	for index, torrent := range items[m.TorrentScroll:end] {
		index += m.TorrentScroll
		style := torrentStyle(torrent.State)
		marker := " "
		if index == m.Selected {
			style = StyleSelected
			marker = ">"
		}
		row := fmt.Sprintf("%s %s %s %s %s %s %s  %s",
			marker, PadRight(Shorten(torrent.Hash, 9), 9), PadRight(m.Tr.StateLabel(torrent.State), 14),
			PadLeft(fmt.Sprintf("%.1f%%", torrent.Progress), 6), PadLeft(HumanBytes(float64(torrent.TotalDone)), 10),
			PadLeft(HumanRate(float64(torrent.DownloadRate)), 11), PadLeft(HumanRate(float64(torrent.UploadRate)), 11),
			Shorten(torrent.Name, nameWidth))
		lines = append(lines, Line{Text: row, Style: style})
	}
	return lines
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
	if m.LogFollow {
		if m.LogStreamConnected {
			header += " · " + m.Tr.T("label.live")
		} else {
			header += " · " + m.Tr.T("label.follow")
		}
	}
	out := []Line{{Text: header, Style: StyleHeader}}
	visible := max(1, contentHeight-1)
	maxScroll := max(0, len(lines)-visible)
	m.LogScroll = min(max(m.LogScroll, 0), maxScroll)
	end := len(lines) - m.LogScroll
	start := max(0, end-visible)
	for _, line := range lines[start:end] {
		style := StyleNormal
		switch {
		case strings.Contains(line, "ERROR"):
			style = StyleErr
		case strings.Contains(line, "WARN"):
			style = StyleWarn
		}
		out = append(out, Line{Text: line, Style: style})
	}
	return out
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
	start, end := catalogWindow(m.ArchiveSelected, m.ArchiveScroll, len(items), contentHeight-1)
	m.ArchiveScroll = start
	for index, entry := range items[start:end] {
		index += start
		style := StyleNormal
		marker := " "
		if index == m.ArchiveSelected {
			style = StyleSelected
			marker = ">"
		}
		lines = append(lines, Line{Text: fmt.Sprintf("%s %s · %s · %s", marker, entry.Title, entry.Source, firstNonEmpty(entry.AddedAt, "-")), Style: style})
	}
	return lines
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
	lines := []Line{{Text: entry.Title, Style: StyleHeader}}
	start := min(m.ArchiveDetailScroll, max(0, len(rows)-1))
	valueWidth := max(1, width-23)
	for _, row := range rows[start:] {
		lines = append(lines, Line{Text: PadRight(row[0]+":", 20) + " " + Shorten(row[1], valueWidth), Style: StyleNormal})
	}
	return lines
}

func (m *Model) renderMissing(width, contentHeight int) []Line {
	lines := []Line{{Text: fmt.Sprintf("%s: %d", m.Tr.T("tab.missing"), len(m.Missing)), Style: StyleHeader}}
	if len(m.Missing) == 0 {
		return append(lines, Line{Text: m.Tr.T("msg.emptymissing"), Style: StyleMuted})
	}
	start, end := catalogWindow(m.MissingSelected, m.MissingScroll, len(m.Missing), contentHeight-1)
	m.MissingScroll = start
	for index, gap := range m.Missing[start:end] {
		index += start
		style := StyleNormal
		marker := " "
		if index == m.MissingSelected {
			style = StyleSelected
			marker = ">"
		}
		airDate := firstNonEmpty(gap.AirDate, "-")
		lines = append(lines, Line{Text: fmt.Sprintf("%s %s · S%02dE%02d · %s", marker, gap.Series, gap.Season, gap.Episode, airDate), Style: style})
	}
	return lines
}

func (m *Model) renderBlocklist(width, contentHeight int) []Line {
	lines := []Line{{Text: fmt.Sprintf("%s: %d", m.Tr.T("tab.blocklist"), len(m.Blocklist)), Style: StyleHeader}}
	if len(m.Blocklist) == 0 {
		return append(lines, Line{Text: m.Tr.T("msg.emptyblocklist"), Style: StyleMuted})
	}
	start, end := catalogWindow(m.BlocklistSelected, m.BlocklistScroll, len(m.Blocklist), contentHeight-1)
	m.BlocklistScroll = start
	for index, entry := range m.Blocklist[start:end] {
		index += start
		style := StyleNormal
		marker := " "
		if index == m.BlocklistSelected {
			style = StyleSelected
			marker = ">"
		}
		reason := firstNonEmpty(entry.Reason, "-")
		lines = append(lines, Line{Text: fmt.Sprintf("%s %s · %s · %s", marker, entry.Title, reason, entry.CreatedAt), Style: style})
	}
	return lines
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
	lines := []Line{{Text: label, Style: style}}
	lines = append(lines,
		Line{Text: fmt.Sprintf("%s: PID %d · %s %s · %s %s · %s %s", m.Tr.T("label.daemon"),
			health.ProcessID, m.Tr.T("label.uptime"), HumanDuration(float64(health.ProcessUptimeSeconds)),
			m.Tr.T("label.cpu"), OptionalNumber(floatPointerValue(health.ProcessCPUPercent), "%", 1),
			m.Tr.T("label.ram"), HumanBytes(float64(health.ResidentBytes))), Style: StyleNormal},
		Line{Text: fmt.Sprintf("%s: %s %s / %s · %s %s", m.Tr.T("label.disks"),
			HumanBytes(float64(health.DiskFreeBytes)), m.Tr.T("label.free"),
			HumanBytes(float64(health.DiskTotalBytes)),
			m.Tr.T("label.trash"), fmt.Sprintf("%d %s (%s)", health.TrashFileCount, m.Tr.T("label.files"), HumanBytes(float64(health.TrashBytes)))), Style: StyleNormal},
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
			lines = append(lines, Line{Text: fmt.Sprintf("%-4s %s %s", state, PadRight(path.Label, 14), path.Path), Style: pathStyle})
		}
	}
	if len(health.Disks) > 0 {
		lines = append(lines, Line{Text: m.Tr.T("label.disks"), Style: StyleHeader})
		for _, disk := range health.Disks {
			used := 0.0
			if disk.TotalBytes > 0 {
				used = (1 - float64(disk.FreeBytes)/float64(disk.TotalBytes)) * 100
			}
			lines = append(lines, Line{Text: fmt.Sprintf("%s %s free / %s · %.0f%% %s",
				PadRight(disk.Mount, 18), HumanBytes(float64(disk.FreeBytes)), HumanBytes(float64(disk.TotalBytes)), used, m.Tr.T("label.used")), Style: StyleNormal})
		}
	}
	if health.Ramdisk != nil {
		lines = append(lines, Line{Text: fmt.Sprintf("%s: %s · %s free / %s", m.Tr.T("label.ramdisk"),
			health.Ramdisk.Path, HumanBytes(float64(health.Ramdisk.FreeBytes)), HumanBytes(float64(health.Ramdisk.TotalBytes))), Style: StyleNormal})
	}
	if len(health.LastErrors) > 0 {
		lines = append(lines, Line{Text: m.Tr.T("label.recenterror"), Style: StyleErr})
		for _, message := range health.LastErrors {
			lines = append(lines, Line{Text: message, Style: StyleErr})
		}
	}
	return lines
}

func floatPointerValue(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func (m *Model) renderDetail(width int) []Line {
	if m.Detail == nil {
		return nil
	}
	torrent := m.Detail.Torrent
	if m.DetailView != DetailGeneral {
		labels := map[DetailView]string{DetailTrackers: m.Tr.T("label.trackers"), DetailFiles: m.Tr.T("label.files"), DetailPeers: m.Tr.T("label.peers")}
		lines := []Line{{Text: fmt.Sprintf("%s: %d", labels[m.DetailView], len(m.DetailItems)), Style: StyleHeader}}
		rendered := make([]Line, 0, len(m.DetailItems))
		for _, item := range m.DetailItems {
			switch m.DetailView {
			case DetailTrackers:
				rendered = append(rendered, Line{Text: fmt.Sprintf("tier %s  %s", OptionalNumber(item["tier"], "", 0), stringValue(item["url"])), Style: StyleNormal})
			case DetailFiles:
				rendered = append(rendered, Line{Text: fmt.Sprintf("%s / %s  %s",
					HumanBytes(numberValue(item["downloaded"])), HumanBytes(numberValue(item["size"])), stringValue(item["path"])), Style: StyleNormal})
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
	lines := []Line{{Text: torrent.Name, Style: StyleHeader}}
	start := min(m.DetailScroll, max(0, len(rows)-1))
	for _, row := range rows[start:] {
		lines = append(lines, Line{Text: PadRight(row[0]+":", 20) + " " + row[1], Style: StyleNormal})
	}
	return lines
}

func (m *Model) renderHelp(width int) []Line {
	keys := []string{
		"help.title", "", "help.global", "help.global2", "help.torrents", "help.torrents2",
		"help.torrents3", "help.torrents4", "help.details", "help.logs", "help.health", "help.settings", "help.archive", "help.missing", "help.blocklist", "",
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
		for _, wrapped := range wrapText(m.Tr.T(key), width) {
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
	visible := max(1, contentHeight-1)
	maxScroll := max(0, len(m.SearchResults)-visible)
	start := min(max(m.SearchScroll, 0), maxScroll)
	if m.SearchSelected < start {
		start = m.SearchSelected
	}
	if m.SearchSelected >= start+visible {
		start = m.SearchSelected - visible + 1
	}
	m.SearchScroll = min(max(start, 0), maxScroll)
	end := min(len(m.SearchResults), m.SearchScroll+visible)
	for index, result := range m.SearchResults[m.SearchScroll:end] {
		index += m.SearchScroll
		style := StyleNormal
		marker := " "
		if index == m.SearchSelected {
			style = StyleSelected
			marker = ">"
		}
		title := stringValue(result["title"])
		source := stringValue(result["source"])
		quality := qualityLabel(result["quality"])
		line := fmt.Sprintf("%s %s · %s · %s", marker, title, source, quality)
		lines = append(lines, Line{Text: line, Style: style})
	}
	return lines
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
		lines = append(lines, Line{Text: line, Style: StyleNormal})
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
		fmt.Sprintf("%d  %s: %s", 1, m.Tr.T("settings.language"), firstNonEmpty(config.DefaultLanguage, "-")),
		fmt.Sprintf("%d  %s: %ds", 2, m.Tr.T("settings.refresh"), config.RefreshSecs),
		fmt.Sprintf("%d  %s: %d/%d KiB/s", 3, m.Tr.T("settings.limits"), config.DownloadLimitKib, config.UploadLimitKib),
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

func wrapText(value string, width int) []string {
	if width <= 0 {
		return []string{value}
	}
	words := strings.Fields(value)
	if len(words) == 0 {
		return []string{""}
	}
	lines := []string{}
	current := ""
	for _, word := range words {
		if current == "" {
			current = word
			continue
		}
		if len(current)+1+len(word) <= width {
			current += " " + word
		} else {
			lines = append(lines, current)
			current = word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func (m *Model) truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	return Shorten(value, width)
}
