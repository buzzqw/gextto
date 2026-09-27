package tui

import (
	"fmt"
	"strings"
	"time"
)

const tabCount = 4

// Render produces a frame for the given terminal size. It is pure: the same
// state always renders the same lines, which makes the layout testable.
func (m *Model) Render(width, height int) Screen {
	if width < 12 || height < 6 {
		return Screen{Lines: []Line{{Text: m.Tr.T("msg.termtoolsmall"), Style: StyleHeader}}}
	}
	lines := make([]Line, 0, height)
	lines = append(lines, Line{Text: m.headerLine(width), Style: StyleNormal})
	lines = append(lines, Line{Text: m.truncate(m.tabsLine(), width), Style: StyleNormal})
	lines = append(lines, Line{Text: m.truncate(m.hints(), width), Style: StyleMuted})

	contentHeight := height - 4
	if contentHeight < 1 {
		contentHeight = 1
	}
	var content []Line
	switch {
	case m.Err != "":
		content = m.renderError()
	case m.Overlay == OverlayHelp:
		content = m.renderHelp(width)
	case m.Overlay == OverlaySearch:
		content = m.renderSearch(width)
	case m.Overlay == OverlayEvents:
		content = m.renderEvents(width)
	case m.Detail != nil:
		content = m.renderDetail(width)
	case m.Tab == TabStatus:
		content = m.renderStatus()
	case m.Tab == TabTorrents:
		content = m.renderTorrents(width)
	case m.Tab == TabLogs:
		content = m.renderLogs(width, contentHeight)
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

	screen := Screen{}
	if m.Confirm != nil {
		lines = append(lines, Line{Text: m.truncate(m.ConfirmMessage(), width), Style: StyleWarn})
	} else if m.Prompt != nil {
		label := m.PromptLabel()
		lines = append(lines, Line{Text: m.truncate(label+m.Prompt.Buffer, width), Style: StyleSelected})
		screen.CursorVisible = true
	} else {
		message := m.Message
		if m.Loading {
			if message != "" {
				message += " · "
			}
			message += m.Tr.T("msg.loading")
		}
		lines = append(lines, Line{Text: m.truncate(message, width), Style: StyleOK})
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
		screen.CursorCol = len([]rune(m.PromptLabel() + m.Prompt.Buffer))
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

func (m *Model) tabsLine() string {
	labels := []string{m.Tr.T("tab.status"), m.Tr.T("tab.torrents"), m.Tr.T("tab.logs"), m.Tr.T("tab.health")}
	parts := make([]string, 0, tabCount)
	for index, label := range labels {
		if Tab(index) == m.Tab {
			parts = append(parts, fmt.Sprintf("[%d:%s]", index+1, label))
		} else {
			parts = append(parts, fmt.Sprintf(" %d:%s ", index+1, label))
		}
	}
	return "  " + strings.Join(parts, " ") + "  (" + m.Tr.T("hint.tabs") + ")"
}

func (m *Model) hints() string {
	hints := m.Tr.T("hint.global")
	switch {
	case m.Detail != nil:
		hints += " · " + m.Tr.T("hint.details")
	case m.Tab == TabTorrents:
		hints += " · " + m.Tr.T("hint.torrents")
	case m.Tab == TabLogs:
		hints += " · " + m.Tr.T("hint.logs")
	case m.Tab == TabHealth:
		hints += " · " + m.Tr.T("hint.health")
	}
	return hints
}

func (m *Model) renderError() []Line {
	return []Line{
		{Text: m.Tr.Format("msg.offline", m.Err), Style: StyleErr},
		{Text: m.Tr.T("msg.sethint"), Style: StyleMuted},
	}
}

func (m *Model) renderStatus() []Line {
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
	return lines
}

func (m *Model) renderTorrents(width int) []Line {
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
	if width < 74 {
		for index, torrent := range items {
			style := StyleNormal
			if index == m.Selected {
				style = StyleSelected
			}
			marker := " "
			if index == m.Selected {
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
	for index, torrent := range items {
		style := StyleNormal
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
		"help.torrents3", "help.torrents4", "help.details", "help.logs", "help.health", "",
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

func (m *Model) renderSearch(width int) []Line {
	lines := []Line{{Text: m.Tr.Format("msg.search", len(m.SearchResults)) + fmt.Sprintf("  '%s'", m.SearchQuery), Style: StyleHeader}}
	if len(m.SearchResults) == 0 {
		lines = append(lines, Line{Text: m.Tr.T("msg.nosearch"), Style: StyleMuted})
		return lines
	}
	for index, result := range m.SearchResults {
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

func (m *Model) renderEvents(width int) []Line {
	lines := []Line{{Text: m.Tr.Format("msg.events", len(m.Events)), Style: StyleHeader}}
	if len(m.Events) == 0 {
		return lines
	}
	for _, event := range m.Events {
		line := fmt.Sprintf("%s %s", PadRight(event.Kind, 22), firstNonEmpty(event.Name, event.Hash))
		if event.SavePath != "" {
			line += " · " + event.SavePath
		}
		lines = append(lines, Line{Text: line, Style: StyleNormal})
	}
	return lines
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
