package tui

import (
	"fmt"
	"strconv"
	"strings"
)

// MarkedHashes returns the marked torrents still in the session, in list
// order.
func (m *Model) MarkedHashes() []string {
	hashes := []string{}
	for _, row := range m.VisibleDownloads() {
		if row.Torrent != nil && m.Marked[row.Torrent.Hash] {
			hashes = append(hashes, row.Torrent.Hash)
		}
	}
	return hashes
}

// pruneMarks drops marks of torrents that left the session.
func (m *Model) pruneMarks() {
	if len(m.Marked) == 0 {
		return
	}
	present := make(map[string]bool, len(m.Torrents))
	for _, torrent := range m.Torrents {
		present[torrent.Hash] = true
	}
	for hash := range m.Marked {
		if !present[hash] {
			delete(m.Marked, hash)
		}
	}
}

// SetTorrentTags replaces the hash → tag map and the tag catalog.
func (m *Model) SetTorrentTags(tags map[string]string, catalog []string) {
	m.TorrentTags = tags
	if catalog != nil {
		m.TagCatalog = catalog
	}
}

// updateTorrentExtras handles the marks, bulk actions, tags, auto-removal and
// history keys of the downloads list. It reports whether it consumed k.
func (m *Model) updateTorrentExtras(k Key) (Action, bool) {
	if k.Kind == KeyEsc && len(m.Marked) > 0 {
		m.Marked = nil
		m.Message = m.Tr.T("msg.marksclear")
		return Action{}, true
	}
	if k.Kind != KeyRune {
		return Action{}, false
	}
	row := m.SelectedDownload()
	switch k.Rune {
	case ' ':
		if row.Torrent == nil {
			return Action{}, true
		}
		if m.Marked == nil {
			m.Marked = map[string]bool{}
		}
		if m.Marked[row.Torrent.Hash] {
			delete(m.Marked, row.Torrent.Hash)
		} else {
			m.Marked[row.Torrent.Hash] = true
		}
		m.Selected = min(m.Selected+1, max(0, len(m.VisibleDownloads())-1))
		return Action{}, true
	case '*':
		visible := []string{}
		for _, item := range m.VisibleDownloads() {
			if item.Torrent != nil {
				visible = append(visible, item.Torrent.Hash)
			}
		}
		if len(m.MarkedHashes()) == len(visible) {
			m.Marked = nil
		} else {
			m.Marked = map[string]bool{}
			for _, hash := range visible {
				m.Marked[hash] = true
			}
		}
		return Action{}, true
	case 'A':
		if m.SpeedPolicy == nil {
			m.Message = m.Tr.T("msg.loadingsettings")
			return Action{}, true
		}
		return Action{Kind: ActionToggleAutoRemove, Flag: !m.SpeedPolicy.AutoRemoveCompleted}, true
	case 'H':
		m.Overlay = OverlayHistory
		m.HistorySelected, m.HistoryScroll = 0, 0
		return Action{Kind: ActionLoadHistory, Text: m.HistoryFilter}, true
	case '#':
		hashes := m.MarkedHashes()
		current := ""
		if len(hashes) == 0 {
			if row.Torrent == nil {
				m.Message = m.Tr.T("msg.notorrent")
				return Action{}, true
			}
			current = m.TorrentTags[row.Torrent.Hash]
		}
		m.Prompt = newPrompt(PromptTag, current)
		return Action{}, true
	}
	hashes := m.MarkedHashes()
	if len(hashes) == 0 {
		return Action{}, false
	}
	switch k.Rune {
	case 'p':
		return Action{Kind: ActionBulk, Domain: "pause", Hashes: hashes}, true
	case 'P':
		return Action{Kind: ActionBulk, Domain: "resume", Hashes: hashes}, true
	case 'k':
		return Action{Kind: ActionBulk, Domain: "recheck", Hashes: hashes}, true
	case 'd':
		m.Confirm = &confirm{MessageKey: "prompt.bulkremove", Args: []any{len(hashes)}, Action: Action{Kind: ActionBulk, Domain: "remove", Hashes: hashes}}
		return Action{}, true
	case 'D':
		m.Confirm = &confirm{MessageKey: "prompt.bulkremovefiles", Args: []any{len(hashes)}, Action: Action{Kind: ActionBulk, Domain: "removefiles", Hashes: hashes}}
		return Action{}, true
	}
	return Action{}, false
}

// submitTag turns the tag prompt into an action for the marked torrents or
// the selected one. An empty tag removes it.
func (m *Model) submitTag(value string) Action {
	hashes := m.MarkedHashes()
	if len(hashes) == 0 {
		if m.Detail != nil {
			hashes = []string{m.Detail.Torrent.Hash}
		} else if torrent := m.SelectedTorrent(); torrent != nil {
			hashes = []string{torrent.Hash}
		}
	}
	if len(hashes) == 0 {
		return Action{}
	}
	isNew := value != ""
	for _, tag := range m.TagCatalog {
		if strings.EqualFold(tag, value) {
			value, isNew = tag, false
			break
		}
	}
	return Action{Kind: ActionSetTag, Hashes: hashes, Text: value, Flag: isNew}
}

// submitTorrentLimits parses "DL UL [ratio] [days]" (KiB/s, 0 = unlimited).
func (m *Model) submitTorrentLimits(value string) Action {
	if m.Detail == nil {
		return Action{}
	}
	fields := strings.Fields(value)
	if len(fields) < 2 || len(fields) > 4 {
		m.Message = m.Tr.T("msg.torrentlimitsinvalid")
		return Action{}
	}
	dl, err1 := strconv.ParseInt(fields[0], 10, 64)
	ul, err2 := strconv.ParseInt(fields[1], 10, 64)
	if err1 != nil || err2 != nil || dl < 0 || ul < 0 {
		m.Message = m.Tr.T("msg.torrentlimitsinvalid")
		return Action{}
	}
	action := Action{Kind: ActionTorrentLimits, Hash: m.Detail.Torrent.Hash, DL: dl, UL: ul}
	if len(fields) > 2 {
		ratio, err := strconv.ParseFloat(strings.ReplaceAll(fields[2], ",", "."), 64)
		if err != nil || ratio < 0 {
			m.Message = m.Tr.T("msg.torrentlimitsinvalid")
			return Action{}
		}
		action.Ratio = &ratio
	}
	if len(fields) > 3 {
		days, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil || days < 0 {
			m.Message = m.Tr.T("msg.torrentlimitsinvalid")
			return Action{}
		}
		action.Days = &days
	}
	return action
}

func (m *Model) torrentLimitsPrefill() string {
	torrent := m.Detail.Torrent
	value := bytesToKib(torrent.DownloadLimit) + " " + bytesToKib(torrent.UploadLimit)
	if torrent.SeedRatio != nil || torrent.SeedDays != nil {
		ratio, days := 0.0, 0.0
		if torrent.SeedRatio != nil {
			ratio = *torrent.SeedRatio
		}
		if torrent.SeedDays != nil {
			days = *torrent.SeedDays
		}
		value += " " + strconv.FormatFloat(ratio, 'f', -1, 64) + " " + strconv.FormatFloat(days, 'f', 0, 64)
	}
	return value
}

// filePriorities reads the current priorities of the files view.
func (m *Model) filePriorities() []int32 {
	priorities := make([]int32, len(m.DetailItems))
	for index, item := range m.DetailItems {
		priorities[index] = int32(numberValue(item["priority"]))
	}
	return priorities
}

// trackerEntries reads the current trackers of the trackers view.
func (m *Model) trackerEntries() []TrackerEntry {
	entries := make([]TrackerEntry, 0, len(m.DetailItems))
	for _, item := range m.DetailItems {
		if address := stringValue(item["url"]); address != "" {
			entries = append(entries, TrackerEntry{Tier: int32(numberValue(item["tier"])), URL: address})
		}
	}
	return entries
}

// updateDetailExtras handles the torrent actions of the details view. It
// reports whether it consumed k.
func (m *Model) updateDetailExtras(k Key) (Action, bool) {
	torrent := m.Detail.Torrent
	hash := torrent.Hash
	selectable := m.DetailView == DetailFiles || m.DetailView == DetailTrackers
	if selectable {
		switch k.Kind {
		case KeyUp, KeyDown, KeyPgUp, KeyPgDn, KeyHome, KeyEnd:
			m.DetailSelected = moveSelection(m.DetailSelected, len(m.DetailItems), k.Kind)
			return Action{}, true
		}
	}
	if k.Kind != KeyRune {
		return Action{}, false
	}
	isPriorityKey := k.Rune == ' ' || k.Rune == '+' || k.Rune == '-'
	if m.DetailView == DetailFiles && isPriorityKey && m.DetailSelected >= 0 && m.DetailSelected < len(m.DetailItems) {
		// Space skips a file or brings it back to normal priority (4);
		// +/- step through libtorrent's 0..7 scale.
		priorities := m.filePriorities()
		current := priorities[m.DetailSelected]
		next := current
		switch {
		case k.Rune == ' ' && current == 0:
			next = 4
		case k.Rune == ' ':
			next = 0
		case k.Rune == '+':
			next = min32(current+1, 7)
		default:
			next = max32(current-1, 0)
		}
		if next == current {
			return Action{}, true
		}
		priorities[m.DetailSelected] = next
		m.DetailItems[m.DetailSelected]["priority"] = float64(next)
		return Action{Kind: ActionFilePriorities, Hash: hash, Priorities: priorities}, true
	}
	if m.DetailView == DetailTrackers {
		switch k.Rune {
		case 'a':
			m.Prompt = newPrompt(PromptAddTracker, "")
			return Action{}, true
		case 'd':
			entries := m.trackerEntries()
			if m.DetailSelected < 0 || m.DetailSelected >= len(entries) {
				return Action{}, true
			}
			removed := entries[m.DetailSelected]
			kept := append(append([]TrackerEntry(nil), entries[:m.DetailSelected]...), entries[m.DetailSelected+1:]...)
			m.Confirm = &confirm{MessageKey: "prompt.trackerremove", Args: []any{Shorten(removed.URL, 50)}, Action: Action{Kind: ActionSetTrackers, Hash: hash, Trackers: kept}}
			return Action{}, true
		}
	}
	switch k.Rune {
	case 'p':
		return Action{Kind: ActionPauseToggle, Hash: hash}, true
	case 'b':
		return Action{Kind: ActionRestart, Hash: hash}, true
	case 'k':
		return Action{Kind: ActionRecheck, Hash: hash}, true
	case 'R':
		return Action{Kind: ActionReannounce, Hash: hash}, true
	case 'n':
		return Action{Kind: ActionNoRenameToggle, Hash: hash}, true
	case 'i':
		return Action{Kind: ActionPin, Hash: hash}, true
	case 'L':
		m.Prompt = newPrompt(PromptTorrentLimits, m.torrentLimitsPrefill())
		return Action{}, true
	case 'm':
		m.Prompt = newPrompt(PromptMoveStorage, torrent.SavePath)
		return Action{}, true
	case '#':
		m.Prompt = newPrompt(PromptTag, m.TorrentTags[hash])
		return Action{}, true
	case 'S':
		return Action{Kind: ActionSuperSeeding, Hash: hash, Flag: !torrent.SuperSeeding}, true
	case 'F':
		m.Confirm = &confirm{MessageKey: "prompt.markfailed", Args: []any{Shorten(torrent.Name, 40)}, Action: Action{Kind: ActionMarkFailed, Hash: hash}}
		return Action{}, true
	}
	return Action{}, false
}

// submitTracker adds a tracker URL in a new last tier.
func (m *Model) submitTracker(value string) Action {
	if m.Detail == nil || value == "" {
		return Action{}
	}
	if !strings.Contains(value, "://") {
		m.Message = m.Tr.T("msg.trackerinvalid")
		return Action{}
	}
	entries := m.trackerEntries()
	tier := int32(0)
	for _, entry := range entries {
		if entry.URL == value {
			m.Message = m.Tr.T("msg.trackerexists")
			return Action{}
		}
		tier = max32(tier, entry.Tier+1)
	}
	entries = append(entries, TrackerEntry{Tier: tier, URL: value})
	return Action{Kind: ActionSetTrackers, Hash: m.Detail.Torrent.Hash, Trackers: entries}
}

func (m *Model) updateHistory(k Key) Action {
	switch {
	case k.Kind == KeyEsc || (k.Kind == KeyRune && k.Rune == 'q'):
		m.Overlay = OverlayNone
	case k.Kind == KeyRune && (k.Rune == '/' || k.Rune == 's'):
		m.Prompt = newPrompt(PromptHistoryFilter, m.HistoryFilter)
	case k.Kind == KeyRune && k.Rune == 'r':
		return Action{Kind: ActionLoadHistory, Text: m.HistoryFilter}
	default:
		m.HistorySelected = moveSelection(m.HistorySelected, len(m.History), k.Kind)
	}
	return Action{}
}

func (m *Model) renderHistory(width, contentHeight int) []Line {
	title := m.Tr.Format("history.title", len(m.History), m.HistoryTotal)
	if m.HistoryFilter != "" {
		title += fmt.Sprintf(" · %s '%s'", m.Tr.T("label.filter"), m.HistoryFilter)
	}
	lines := []Line{{Text: title, Style: StyleHeader}}
	if len(m.History) == 0 {
		return append(lines, Line{Text: m.Tr.T("history.empty"), Style: StyleMuted})
	}
	texts := make([]string, len(m.History))
	styles := make([]Style, len(m.History))
	for index, item := range m.History {
		date := item.CompletedAt
		if len(date) > 16 {
			date = date[:16]
		}
		styles[index] = StyleNormal
		status := m.Tr.StateLabel(item.Status)
		if item.Status == "error" || item.Error != "" {
			styles[index] = StyleErr
			status = joinNonEmpty(status, item.Error)
		}
		texts[index] = joinNonEmpty(date, item.Kind, labelled("#", item.Tag), item.Name, status, item.ProcessedPath)
	}
	return append(lines, styledRows(texts, styles, m.HistorySelected, &m.HistoryScroll, contentHeight-1, width)...)
}

func min32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

func max32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}
