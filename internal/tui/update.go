package tui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Update applies a key press and returns the action the runner must execute.
func (m *Model) Update(k Key) Action {
	if k.Kind == KeyPaste {
		return m.updatePaste(k.Text)
	}
	if m.Prompt != nil {
		return m.updatePrompt(k)
	}
	if m.Confirm != nil {
		return m.updateConfirm(k)
	}
	if m.Form != nil && m.Overlay == OverlayNone {
		if k.Kind == KeyCtrlC {
			return Action{Kind: ActionQuit}
		}
		return m.updateForm(k)
	}
	switch m.Overlay {
	case OverlayHelp:
		switch {
		case k.Kind == KeyEsc || k.Kind == KeyEnter:
			m.Overlay = OverlayNone
		case k.Kind == KeyRune && (k.Rune == '?' || k.Rune == 'q'):
			m.Overlay = OverlayNone
		case k.Kind == KeyUp:
			m.HelpScroll = max(0, m.HelpScroll-1)
		case k.Kind == KeyDown:
			m.HelpScroll++
		case k.Kind == KeyPgUp:
			m.HelpScroll = max(0, m.HelpScroll-10)
		case k.Kind == KeyPgDn:
			m.HelpScroll += 10
		case k.Kind == KeyHome:
			m.HelpScroll = 0
		}
		return Action{}
	case OverlaySearch:
		return m.updateSearchOverlay(k)
	case OverlayEvents:
		switch {
		case k.Kind == KeyEsc:
			m.Overlay = OverlayNone
		case k.Kind == KeyRune && k.Rune == 'q':
			m.Overlay = OverlayNone
		case k.Kind == KeyRune && k.Rune == 'r':
			return Action{Kind: ActionLoadEvents}
		case k.Kind == KeyUp:
			m.EventScroll = max(0, m.EventScroll-1)
		case k.Kind == KeyDown:
			m.EventScroll = min(max(0, len(m.Events)-1), m.EventScroll+1)
		case k.Kind == KeyPgUp:
			m.EventScroll = max(0, m.EventScroll-10)
		case k.Kind == KeyPgDn:
			m.EventScroll = min(max(0, len(m.Events)-1), m.EventScroll+10)
		case k.Kind == KeyHome:
			m.EventScroll = 0
		case k.Kind == KeyEnd:
			m.EventScroll = max(0, len(m.Events)-1)
		}
		return Action{}
	case OverlaySettings:
		return m.updateSettings(k)
	case OverlayHistory:
		return m.updateHistory(k)
	case OverlayMaintenance:
		return m.updateMaintenanceReport(k)
	}

	switch {
	case k.Kind == KeyCtrlC:
		return Action{Kind: ActionQuit}
	case k.Kind == KeyRune && k.Rune == '?':
		m.Overlay = OverlayHelp
		m.HelpScroll = 0
		return Action{}
	case k.Kind == KeyRune && k.Rune == 'q':
		return Action{Kind: ActionQuit}
	}

	if m.Detail != nil {
		return m.updateDetail(k)
	}
	if m.ArchiveDetail != nil {
		return m.updateArchiveDetail(k)
	}
	if action, handled := m.updateTabContext(k); handled {
		return action
	}

	switch {
	case k.Kind == KeyRune && k.Rune == 'r':
		m.Loading = true
		return Action{Kind: ActionRefresh}
	case k.Kind == KeyRune && k.Rune == 'a':
		m.Prompt = newPrompt(PromptMagnet, "")
		return Action{}
	case k.Kind == KeyRune && k.Rune == 't':
		m.Prompt = newPrompt(PromptFile, "")
		return Action{}
	case k.Kind == KeyRune && k.Rune == 'c':
		m.Prompt = newPrompt(PromptCycle, "")
		return Action{}
	case k.Kind == KeyRune && unicode.ToLower(k.Rune) == 's':
		if m.Tab == TabArchive {
			m.Prompt = newPrompt(PromptArchiveFilter, m.ArchiveFilter)
		} else {
			m.Prompt = newPrompt(PromptSearch, "")
		}
		return Action{}
	case k.Kind == KeyRune && k.Rune == 'e':
		return Action{Kind: ActionLoadEvents}
	case k.Kind == KeyRune && k.Rune == 'g':
		m.Overlay = OverlaySettings
		m.SettingsSelected = 0
		if m.Config == nil {
			return Action{Kind: ActionLoadConfig}
		}
		return Action{}
	case k.Kind == KeyRune && k.Rune == 'l':
		m.Tab = TabLibrary
		return m.loadTabAction()
	case k.Kind == KeyTab:
		m.Tab = Tab((int(m.Tab) + 1) % tabCount)
		return m.loadTabAction()
	case k.Kind == KeyBackTab:
		m.Tab = Tab((int(m.Tab) + tabCount - 1) % tabCount)
		return m.loadTabAction()
	case k.Kind == KeyRune && k.Rune >= '1' && k.Rune <= '3' && m.Tab != TabLibrary:
		m.Tab = Tab(k.Rune - '1')
		return m.loadTabAction()
	case k.Kind == KeyRune && k.Rune >= '4' && k.Rune <= '7':
		m.Tab = Tab(k.Rune - '1')
		return m.loadTabAction()
	case k.Kind == KeyRune && k.Rune == '8' && m.Tab != TabLibrary:
		m.Tab = TabLibrary
		return m.loadTabAction()
	case k.Kind == KeyRune && k.Rune == '9':
		m.Tab = TabMaintenance
		return m.loadTabAction()
	}

	switch m.Tab {
	case TabTorrents:
		return m.updateTorrents(k)
	case TabLogs:
		return m.updateLogs(k)
	case TabStatus:
		m.scrollPage(k.Kind)
	case TabHealth:
		if k.Kind == KeyRune && k.Rune == 'x' {
			m.Confirm = &confirm{MessageKey: "prompt.cleantrash", Action: Action{Kind: ActionCleanTrash}}
		}
		m.scrollPage(k.Kind)
	case TabArchive:
		return m.updateArchive(k)
	case TabMissing:
		return m.updateMissing(k)
	case TabBlocklist:
		return m.updateBlocklist(k)
	case TabLibrary:
		return m.updateLibrary(k)
	case TabMaintenance:
		return m.updateMaintenance(k)
	}
	return Action{}
}

func (m *Model) updatePrompt(k Key) Action {
	buffer := []rune(m.Prompt.Buffer)
	if m.Prompt.Cursor < 0 || m.Prompt.Cursor > len(buffer) {
		m.Prompt.Cursor = len(buffer)
	}
	switch k.Kind {
	case KeyEsc:
		m.Prompt = nil
		m.Message = m.Tr.T("msg.cancelled")
		return Action{}
	case KeyEnter:
		return m.submitPrompt()
	case KeyBackspace:
		if m.Prompt.Cursor > 0 {
			buffer = append(buffer[:m.Prompt.Cursor-1], buffer[m.Prompt.Cursor:]...)
			m.Prompt.Cursor--
		}
	case KeyDelete:
		if m.Prompt.Cursor < len(buffer) {
			buffer = append(buffer[:m.Prompt.Cursor], buffer[m.Prompt.Cursor+1:]...)
		}
	case KeyLeft:
		m.Prompt.Cursor = max(0, m.Prompt.Cursor-1)
	case KeyRight:
		m.Prompt.Cursor = min(len(buffer), m.Prompt.Cursor+1)
	case KeyHome, KeyCtrlA:
		m.Prompt.Cursor = 0
	case KeyEnd, KeyCtrlE:
		m.Prompt.Cursor = len(buffer)
	case KeyCtrlK:
		buffer = buffer[:m.Prompt.Cursor]
	case KeyCtrlU:
		buffer = nil
		m.Prompt.Cursor = 0
	case KeyCtrlW:
		end := m.Prompt.Cursor
		for end > 0 && buffer[end-1] == ' ' {
			end--
		}
		for end > 0 && buffer[end-1] != ' ' {
			end--
		}
		buffer = append(buffer[:end], buffer[m.Prompt.Cursor:]...)
		m.Prompt.Cursor = end
	case KeyRune:
		if k.Rune >= 32 {
			buffer = append(buffer, 0)
			copy(buffer[m.Prompt.Cursor+1:], buffer[m.Prompt.Cursor:])
			buffer[m.Prompt.Cursor] = k.Rune
			m.Prompt.Cursor++
		}
	}
	m.Prompt.Buffer = string(buffer)
	return Action{}
}

func (m *Model) submitPrompt() Action {
	active := m.Prompt
	m.Prompt = nil
	value := strings.TrimSpace(active.Buffer)
	switch active.Kind {
	case PromptMagnet:
		if value == "" {
			return Action{}
		}
		if !(strings.HasPrefix(value, "magnet:") || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")) {
			m.Message = m.Tr.T("msg.magnetinvalid")
			return Action{}
		}
		return Action{Kind: ActionAddMagnet, Text: value}
	case PromptFile:
		if value == "" {
			return Action{}
		}
		return Action{Kind: ActionAddFile, Text: value}
	case PromptCycle:
		domain := strings.ToLower(value)
		if domain == "" {
			domain = "full"
		}
		if domain != "full" && domain != "series" && domain != "movies" && domain != "comics" {
			m.Message = m.Tr.T("msg.cycleinvalid")
			return Action{}
		}
		return Action{Kind: ActionRunCycle, Domain: domain}
	case PromptSearch:
		if value == "" {
			return Action{}
		}
		if m.Tab == TabLibrary {
			m.LibraryFilter = value
			m.LibrarySelected, m.LibraryScroll = 0, 0
			return Action{}
		}
		return Action{Kind: ActionSearch, Text: value}
	case PromptLimits:
		fields := strings.Fields(value)
		if len(fields) != 2 {
			m.Message = m.Tr.T("msg.twoValues")
			return Action{}
		}
		dl, err1 := strconv.ParseInt(fields[0], 10, 64)
		ul, err2 := strconv.ParseInt(fields[1], 10, 64)
		if err1 != nil || err2 != nil || dl < 0 || ul < 0 {
			m.Message = m.Tr.T("msg.limitsinvalid")
			return Action{}
		}
		return Action{Kind: ActionSetLimits, DL: dl, UL: ul}
	case PromptTempLimits:
		return m.submitTempLimits(value)
	case PromptTmdbSeries, PromptTmdbMovie:
		if value == "" {
			return Action{}
		}
		kind := "series"
		if active.Kind == PromptTmdbMovie {
			kind = "movie"
		}
		return Action{Kind: ActionTmdbSearch, Domain: kind, Text: value}
	case PromptFormField:
		if m.Form != nil {
			m.Form.Fields[m.Form.Selected].Value = strings.TrimSpace(active.Buffer)
		}
	case PromptTag:
		return m.submitTag(value)
	case PromptTorrentLimits:
		return m.submitTorrentLimits(value)
	case PromptMoveStorage:
		if value != "" && m.Detail != nil && value != m.Detail.Torrent.SavePath {
			return Action{Kind: ActionMoveStorage, Hash: m.Detail.Torrent.Hash, Text: value}
		}
	case PromptAddTracker:
		return m.submitTracker(value)
	case PromptFolderRename:
		if value == "" {
			return Action{}
		}
		m.lastFolder = value
		return Action{Kind: ActionMaintenance, Domain: "folderscan", Text: value}
	case PromptRamdisk:
		if value == "" {
			return Action{}
		}
		return Action{Kind: ActionMaintenance, Domain: "ramdisk", Text: value}
	case PromptHistoryFilter:
		m.HistoryFilter = value
		m.HistorySelected, m.HistoryScroll = 0, 0
		return Action{Kind: ActionLoadHistory, Text: value}
	case PromptLogFilter:
		m.LogFilter = value
		m.LogScroll = 0
		m.LogFollow = true
	case PromptTorrentFilter:
		m.Filter = value
		m.Selected = 0
		m.TorrentScroll = 0
	case PromptArchiveFilter:
		m.ArchiveFilter = value
		m.ArchiveSelected = 0
		m.ArchiveScroll = 0
		m.ArchivePage = 1
		return Action{Kind: ActionLoadArchive, Text: value, Page: 1}
	case PromptLanguage:
		if value != LangIT && value != LangEN {
			m.Message = m.Tr.T("msg.languageinvalid")
			return Action{}
		}
		return Action{Kind: ActionSetLanguage, Text: value}
	case PromptRefresh:
		seconds, err := strconv.ParseUint(value, 10, 64)
		if err != nil || seconds < 1 || seconds > 86400 {
			m.Message = m.Tr.T("msg.refreshinvalid")
			return Action{}
		}
		return Action{Kind: ActionSaveSetting, Domain: "refresh_interval", Text: strconv.FormatUint(seconds, 10)}
	}
	return Action{}
}

// maxTempLimitMinutes is the longest temporary limit the daemon accepts.
const maxTempLimitMinutes = 24 * 60

// submitTempLimits parses "DL UL [minutes]" (KiB/s, 0 = unlimited; no minutes
// or 0 keeps the limit until removed) or "off" to remove the temporary limit.
func (m *Model) submitTempLimits(value string) Action {
	switch strings.ToLower(value) {
	case "":
		return Action{}
	case "off", "-", "x", "no":
		return Action{Kind: ActionClearTempLimits}
	}
	fields := strings.Fields(value)
	if len(fields) != 2 && len(fields) != 3 {
		m.Message = m.Tr.T("msg.tempinvalid")
		return Action{}
	}
	numbers := make([]int64, 3)
	for index, field := range fields {
		number, err := strconv.ParseInt(field, 10, 64)
		if err != nil || number < 0 {
			m.Message = m.Tr.T("msg.tempinvalid")
			return Action{}
		}
		numbers[index] = number
	}
	if numbers[2] > maxTempLimitMinutes {
		m.Message = m.Tr.Format("msg.tempminutes", maxTempLimitMinutes)
		return Action{}
	}
	return Action{Kind: ActionSetTempLimits, DL: numbers[0], UL: numbers[1], Minutes: numbers[2]}
}

// tempLimitsPrefill starts the prompt from the active temporary limit (with
// the minutes left), or from the last one used when none is active.
func (m *Model) tempLimitsPrefill() string {
	policy := m.SpeedPolicy
	if policy == nil {
		return ""
	}
	if minutes, active := m.TempLimitMinutes(); active {
		return fmt.Sprintf("%d %d %d", policy.TempDownloadKib, policy.TempUploadKib, minutes)
	}
	if policy.TempDownloadKib == 0 && policy.TempUploadKib == 0 {
		return ""
	}
	return fmt.Sprintf("%d %d", policy.TempDownloadKib, policy.TempUploadKib)
}

func (m *Model) updateConfirm(k Key) Action {
	// Only an explicit y/s confirms: Enter is excluded because it is also
	// what opens and submits things, so an auto-repeated or doubled Enter
	// (easy over a laggy SSH link) must never delete a torrent and its files.
	yes := false
	switch k.Kind {
	case KeyEsc, KeyEnter:
		yes = false
	case KeyRune:
		lowered := unicode.ToLower(k.Rune)
		if lowered == 'y' || lowered == 's' {
			yes = true
		} else if lowered == 'n' {
			yes = false
		} else {
			return Action{}
		}
	default:
		return Action{}
	}
	pending := m.Confirm
	m.Confirm = nil
	if yes {
		m.Message = m.Tr.T("msg.confirmed")
		return pending.Action
	}
	m.Message = m.Tr.T("msg.cancelled")
	return Action{}
}

func (m *Model) updateSearchOverlay(k Key) Action {
	switch {
	case k.Kind == KeyEsc || (k.Kind == KeyRune && k.Rune == 'q'):
		m.Overlay = OverlayNone
	case k.Kind == KeyRune && k.Rune == 'q':
		m.Overlay = OverlayNone
	case k.Kind == KeyUp || k.Kind == KeyLeft:
		m.SearchSelected = max(0, m.SearchSelected-1)
	case k.Kind == KeyDown || k.Kind == KeyRight:
		m.SearchSelected = min(max(0, len(m.SearchResults)-1), m.SearchSelected+1)
	case k.Kind == KeyEnter || (k.Kind == KeyRune && k.Rune == 'a'):
		if len(m.SearchResults) == 0 {
			m.Message = m.Tr.T("msg.nosearch")
			return Action{}
		}
		selected := m.SearchResults[m.SearchSelected]
		if m.SearchKind != SearchReleases {
			if truthy(selected["in_library"]) {
				m.Message = m.Tr.T("msg.inlibrary")
				return Action{}
			}
			m.Overlay = OverlayNone
			m.Form = addForm(m.Tr, m.SearchKind, selected)
			return Action{}
		}
		return Action{Kind: ActionQueueRelease, Release: selected}
	case k.Kind == KeyRune && k.Rune == 'y':
		if len(m.SearchResults) > 0 && m.SearchSelected >= 0 && m.SearchSelected < len(m.SearchResults) {
			magnet := stringValue(m.SearchResults[m.SearchSelected]["magnet"])
			if magnet != "" {
				return Action{Kind: ActionCopy, Text: magnet}
			}
		}
	}
	return Action{}
}

func (m *Model) updateDetail(k Key) Action {
	if action, handled := m.updateDetailExtras(k); handled {
		return action
	}
	switch {
	case k.Kind == KeyEsc || k.Kind == KeyEnter:
		m.Detail = nil
	case k.Kind == KeyUp:
		m.DetailScroll = max(0, m.DetailScroll-1)
	case k.Kind == KeyDown:
		m.DetailScroll++
	case k.Kind == KeyPgUp:
		m.DetailScroll = max(0, m.DetailScroll-10)
	case k.Kind == KeyPgDn:
		m.DetailScroll += 10
	case k.Kind == KeyRune && k.Rune == 'r':
		if torrent := m.SelectedTorrent(); torrent != nil {
			return Action{Kind: ActionOpenDetails, Hash: torrent.Hash}
		}
	case k.Kind == KeyRune && k.Rune >= '1' && k.Rune <= '4':
		view := DetailView(k.Rune - '1')
		m.DetailView = view
		m.DetailSelected = 0
		if view == DetailGeneral {
			m.DetailScroll = 0
			return Action{}
		}
		if torrent := m.SelectedTorrent(); torrent != nil {
			kind := map[DetailView]string{DetailTrackers: "trackers", DetailFiles: "files", DetailPeers: "peers"}[view]
			return Action{Kind: ActionLoadDetail, Hash: torrent.Hash, DetailKind: kind}
		}
	case k.Kind == KeyRune && k.Rune == 'y':
		if m.Detail.Magnet != "" {
			return Action{Kind: ActionCopy, Text: m.Detail.Magnet}
		}
	}
	return Action{}
}

func (m *Model) updateTorrents(k Key) Action {
	if action, handled := m.updateTorrentExtras(k); handled {
		return action
	}
	items := m.VisibleDownloads()
	count := len(items)
	page := 10
	switch {
	case k.Kind == KeyDown:
		m.Selected = min(max(0, count-1), m.Selected+1)
	case k.Kind == KeyUp:
		m.Selected = max(0, m.Selected-1)
	case k.Kind == KeyPgDn:
		m.Selected = min(max(0, count-1), m.Selected+page)
	case k.Kind == KeyPgUp:
		m.Selected = max(0, m.Selected-page)
	case k.Kind == KeyHome:
		m.Selected = 0
	case k.Kind == KeyEnd:
		m.Selected = max(0, count-1)
	case k.Kind == KeyEnter:
		if torrent := m.SelectedTorrent(); torrent != nil && torrent.Hash != "" {
			return Action{Kind: ActionOpenDetails, Hash: torrent.Hash}
		}
	case k.Kind != KeyRune:
		return Action{}
	default:
		// List-wide commands work whatever row is selected, HTTP ones too.
		switch k.Rune {
		case 'u':
			return Action{Kind: ActionUnpin}
		case 'X':
			m.Confirm = &confirm{MessageKey: "prompt.cleancomp", Action: Action{Kind: ActionCleanCompleted}}
			return Action{}
		case 'L':
			m.Prompt = newPrompt(PromptLimits, "")
			return Action{}
		case 'T':
			m.Prompt = newPrompt(PromptTempLimits, m.tempLimitsPrefill())
			return Action{}
		case 'o':
			m.Sort = SortMode((int(m.Sort) + 1) % 5)
			return Action{}
		case 'O':
			m.SortDesc = !m.SortDesc
			return Action{}
		case 'F':
			m.Prompt = newPrompt(PromptTorrentFilter, m.Filter)
			return Action{}
		}
		row := m.SelectedDownload()
		torrent := row.Torrent
		if row.HTTP != nil {
			switch k.Rune {
			case 'p':
				return Action{Kind: ActionHTTPPauseToggle, HTTPID: row.HTTP.ID}
			case 'd', 'D':
				m.Confirm = &confirm{MessageKey: "prompt.httpremove", Args: []any{Shorten(row.HTTP.Title, 40)}, Action: Action{Kind: ActionHTTPRemove, HTTPID: row.HTTP.ID}}
			}
			return Action{}
		}
		hash := ""
		if torrent != nil {
			hash = torrent.Hash
		}
		switch k.Rune {
		case 'p':
			if hash == "" {
				m.Message = m.Tr.T("msg.notorrent")
				return Action{}
			}
			return Action{Kind: ActionPauseToggle, Hash: hash}
		case 'b':
			if hash != "" {
				return Action{Kind: ActionRestart, Hash: hash}
			}
		case 'd':
			if torrent != nil {
				m.Confirm = &confirm{MessageKey: "prompt.remove", Args: []any{Shorten(torrent.Name, 40)}, Action: Action{Kind: ActionRemove, Hash: hash}}
			} else {
				m.Message = m.Tr.T("msg.notorrent")
			}
		case 'D':
			if torrent != nil {
				m.Confirm = &confirm{MessageKey: "prompt.removefiles", Args: []any{Shorten(torrent.Name, 40)}, Action: Action{Kind: ActionRemove, Hash: hash, DeleteFiles: true}}
			} else {
				m.Message = m.Tr.T("msg.notorrent")
			}
		case 'k':
			if hash != "" {
				return Action{Kind: ActionRecheck, Hash: hash}
			}
		case 'R':
			if hash != "" {
				return Action{Kind: ActionReannounce, Hash: hash}
			}
		case 'n':
			if hash != "" {
				return Action{Kind: ActionNoRenameToggle, Hash: hash}
			}
		case 'i':
			if hash != "" {
				return Action{Kind: ActionPin, Hash: hash}
			}
		case 'y':
			if torrent != nil && torrent.Hash != "" {
				return Action{Kind: ActionCopy, Text: torrent.Hash}
			}
		}
	}
	return Action{}
}

func (m *Model) updateLogs(k Key) Action {
	lines := m.FilteredLogs()
	maxScroll := max(0, len(lines)-1)
	switch k.Kind {
	case KeyUp:
		m.LogScroll = min(maxScroll, m.LogScroll+1)
		m.LogFollow = false
	case KeyDown, KeyPgDn:
		m.LogScroll = max(0, m.LogScroll-10)
		m.LogFollow = m.LogScroll == 0
	case KeyPgUp:
		m.LogScroll = min(maxScroll, m.LogScroll+10)
		m.LogFollow = false
	case KeyHome:
		m.LogScroll = maxScroll
		m.LogFollow = false
	case KeyEnd:
		m.LogScroll = 0
		m.LogFollow = true
	case KeyRune:
		switch k.Rune {
		case '/':
			m.Prompt = newPrompt(PromptLogFilter, m.LogFilter)
		case 'f':
			m.LogFollow = !m.LogFollow
			if m.LogFollow {
				m.LogScroll = 0
			}
		}
	}
	return Action{}
}

func (m *Model) updateSettings(k Key) Action {
	const settingCount = 6
	switch {
	case k.Kind == KeyEsc || (k.Kind == KeyRune && k.Rune == 'q'):
		m.Overlay = OverlayNone
	case k.Kind == KeyUp:
		m.SettingsSelected = (m.SettingsSelected + settingCount - 1) % settingCount
	case k.Kind == KeyDown:
		m.SettingsSelected = (m.SettingsSelected + 1) % settingCount
	case k.Kind == KeyRune && k.Rune == 'c':
		m.ColorsEnabled = !m.ColorsEnabled
	case k.Kind == KeyRune && k.Rune == 'h':
		m.HighContrast = !m.HighContrast
	case k.Kind == KeyEnter:
		switch m.SettingsSelected {
		case 0:
			language := ""
			if m.Config != nil {
				language = m.Config.DefaultLanguage
			}
			m.Prompt = newPrompt(PromptLanguage, language)
		case 1:
			seconds := "2"
			if m.Config != nil && m.Config.RefreshSecs > 0 {
				seconds = strconv.FormatUint(m.Config.RefreshSecs, 10)
			}
			m.Prompt = newPrompt(PromptRefresh, seconds)
		case 2:
			value := "0 0"
			if m.Config != nil {
				value = fmt.Sprintf("%d %d", m.Config.DownloadLimitKib, m.Config.UploadLimitKib)
			}
			m.Prompt = newPrompt(PromptLimits, value)
		case 3:
			if m.Config != nil {
				value := "false"
				if !m.Config.DryRun {
					value = "true"
				}
				return Action{Kind: ActionSaveSetting, Domain: "dry_run", Text: value}
			}
		}
	}
	return Action{}
}

// scrollPage moves the Status/Health page; Render clamps the bottom end.
func (m *Model) scrollPage(kind KeyKind) {
	switch kind {
	case KeyUp:
		m.PageScroll = max(0, m.PageScroll-1)
	case KeyDown:
		m.PageScroll++
	case KeyPgUp:
		m.PageScroll = max(0, m.PageScroll-10)
	case KeyPgDn:
		m.PageScroll += 10
	case KeyHome:
		m.PageScroll = 0
	case KeyEnd:
		m.PageScroll = 1 << 20
	}
}

func (m *Model) loadTabAction() Action {
	m.PageScroll = 0
	switch m.Tab {
	case TabArchive:
		return Action{Kind: ActionLoadArchive, Text: m.ArchiveFilter, Page: m.ArchivePage}
	case TabMissing:
		return Action{Kind: ActionLoadMissing}
	case TabBlocklist:
		return Action{Kind: ActionLoadBlocklist}
	default:
		return Action{Kind: ActionRefresh}
	}
}

func moveSelection(selected, length int, k KeyKind) int {
	if length == 0 {
		return 0
	}
	switch k {
	case KeyUp:
		return max(0, selected-1)
	case KeyDown:
		return min(length-1, selected+1)
	case KeyPgUp:
		return max(0, selected-10)
	case KeyPgDn:
		return min(length-1, selected+10)
	case KeyHome:
		return 0
	case KeyEnd:
		return length - 1
	default:
		return selected
	}
}

func (m *Model) updateArchive(k Key) Action {
	switch {
	case k.Kind == KeyRune && k.Rune == '/':
		m.Prompt = newPrompt(PromptArchiveFilter, m.ArchiveFilter)
	case k.Kind == KeyEnter:
		items := m.VisibleArchive()
		if m.ArchiveSelected >= 0 && m.ArchiveSelected < len(items) && items[m.ArchiveSelected].Magnet != "" {
			return Action{Kind: ActionAddMagnet, Text: items[m.ArchiveSelected].Magnet}
		}
	case k.Kind == KeyPgUp:
		if m.ArchivePage > 1 {
			m.ArchivePage--
			m.ArchiveSelected = 0
			m.ArchiveScroll = 0
			return Action{Kind: ActionLoadArchive, Text: m.ArchiveFilter, Page: m.ArchivePage}
		}
	case k.Kind == KeyPgDn:
		if m.ArchivePage < max(1, m.ArchivePages) {
			m.ArchivePage++
			m.ArchiveSelected = 0
			m.ArchiveScroll = 0
			return Action{Kind: ActionLoadArchive, Text: m.ArchiveFilter, Page: m.ArchivePage}
		}
	case k.Kind == KeyRune && (k.Rune == 'o' || k.Rune == 'O'):
		if k.Rune == 'o' {
			m.ArchiveSort = (m.ArchiveSort + 1) % 4
		} else {
			m.ArchiveSortDesc = !m.ArchiveSortDesc
		}
		m.ArchiveSelected = 0
		m.ArchiveScroll = 0
	case k.Kind == KeyRune && k.Rune == 'd':
		items := m.VisibleArchive()
		if m.ArchiveSelected >= 0 && m.ArchiveSelected < len(items) {
			m.SetArchiveDetail(items[m.ArchiveSelected])
		}
	case k.Kind == KeyUp, k.Kind == KeyDown, k.Kind == KeyHome, k.Kind == KeyEnd:
		m.ArchiveSelected = moveSelection(m.ArchiveSelected, len(m.Archive), k.Kind)
	}
	return Action{}
}

func (m *Model) updateArchiveDetail(k Key) Action {
	switch {
	case k.Kind == KeyEsc || k.Kind == KeyEnter:
		m.ArchiveDetail = nil
		m.ArchiveDetailScroll = 0
	case k.Kind == KeyUp:
		m.ArchiveDetailScroll = max(0, m.ArchiveDetailScroll-1)
	case k.Kind == KeyDown:
		m.ArchiveDetailScroll++
	case k.Kind == KeyPgUp:
		m.ArchiveDetailScroll = max(0, m.ArchiveDetailScroll-10)
	case k.Kind == KeyPgDn:
		m.ArchiveDetailScroll += 10
	case k.Kind == KeyHome:
		m.ArchiveDetailScroll = 0
	case k.Kind == KeyEnd:
		m.ArchiveDetailScroll = 1000000
	case k.Kind == KeyRune && unicode.ToLower(k.Rune) == 'a':
		if m.ArchiveDetail != nil && m.ArchiveDetail.Magnet != "" {
			return Action{Kind: ActionAddMagnet, Text: m.ArchiveDetail.Magnet}
		}
	case k.Kind == KeyRune && k.Rune == 'y':
		if m.ArchiveDetail != nil && m.ArchiveDetail.Magnet != "" {
			return Action{Kind: ActionCopy, Text: m.ArchiveDetail.Magnet}
		}
	}
	return Action{}
}

func (m *Model) updateBlocklist(k Key) Action {
	switch {
	case k.Kind == KeyUp, k.Kind == KeyDown, k.Kind == KeyPgUp, k.Kind == KeyPgDn, k.Kind == KeyHome, k.Kind == KeyEnd:
		m.BlocklistSelected = moveSelection(m.BlocklistSelected, len(m.Blocklist), k.Kind)
	case k.Kind == KeyRune && k.Rune == 'd':
		if m.BlocklistSelected >= 0 && m.BlocklistSelected < len(m.Blocklist) {
			entry := m.Blocklist[m.BlocklistSelected]
			m.Confirm = &confirm{MessageKey: "prompt.blocklistremove", Args: []any{Shorten(entry.Title, 45)}, Action: Action{Kind: ActionRemoveBlocklist, Hash: entry.Hash}}
		}
	}
	return Action{}
}

// updatePaste handles a bracketed paste. In a text prompt the text is
// inserted as typed; elsewhere a pasted magnet or URL opens the "add" prompt
// pre-filled. A paste is never interpreted as a sequence of commands, and it
// never answers a confirmation.
func (m *Model) updatePaste(text string) Action {
	if m.Prompt != nil {
		m.insertPromptText(singleLine(text))
		return Action{}
	}
	if m.Confirm != nil {
		return Action{}
	}
	value := strings.TrimSpace(singleLine(text))
	if strings.HasPrefix(value, "magnet:") || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		m.Overlay = OverlayNone
		m.Prompt = newPrompt(PromptMagnet, value)
		return Action{}
	}
	if value != "" {
		m.Message = m.Tr.T("msg.pasteignored")
	}
	return Action{}
}

// singleLine joins pasted lines with spaces and drops control characters.
func singleLine(text string) string {
	text = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(text)
	return Sanitize(text)
}

func (m *Model) insertPromptText(text string) {
	buffer := []rune(m.Prompt.Buffer)
	cursor := min(max(m.Prompt.Cursor, 0), len(buffer))
	inserted := []rune(text)
	buffer = append(buffer[:cursor], append(inserted, buffer[cursor:]...)...)
	m.Prompt.Buffer = string(buffer)
	m.Prompt.Cursor = cursor + len(inserted)
}

// PromptLabel returns the localized label of the active prompt.
func (m *Model) PromptLabel() string {
	if m.Prompt == nil {
		return ""
	}
	switch m.Prompt.Kind {
	case PromptCycle:
		return m.Tr.T("prompt.cycle")
	case PromptSearch:
		return m.Tr.T("prompt.search")
	case PromptMagnet:
		return m.Tr.T("prompt.magnet")
	case PromptFile:
		return m.Tr.T("prompt.file")
	case PromptLimits:
		return m.Tr.T("prompt.limits")
	case PromptLogFilter:
		return m.Tr.T("prompt.logfilter")
	case PromptTorrentFilter:
		return m.Tr.T("prompt.torrentfilter")
	case PromptArchiveFilter:
		return m.Tr.T("prompt.archivefilter")
	case PromptLanguage:
		return m.Tr.T("prompt.language")
	case PromptRefresh:
		return m.Tr.T("prompt.refresh")
	case PromptTempLimits:
		return m.Tr.T("prompt.templimits")
	case PromptTag:
		known := strings.Join(m.TagCatalog, ", ")
		if known == "" {
			known = "-"
		}
		if count := len(m.MarkedHashes()); count > 0 && m.Detail == nil {
			return m.Tr.Format("prompt.tagbulk", count, known)
		}
		return m.Tr.Format("prompt.tag", known)
	case PromptTorrentLimits:
		return m.Tr.T("prompt.torrentlimits")
	case PromptMoveStorage:
		return m.Tr.T("prompt.movestorage")
	case PromptAddTracker:
		return m.Tr.T("prompt.addtracker")
	case PromptHistoryFilter:
		return m.Tr.T("prompt.historyfilter")
	case PromptFolderRename:
		return m.Tr.T("prompt.folderrename")
	case PromptRamdisk:
		return m.Tr.T("prompt.ramdisk")
	case PromptTmdbSeries:
		return m.Tr.T("prompt.tmdbseries")
	case PromptTmdbMovie:
		return m.Tr.T("prompt.tmdbmovie")
	case PromptFormField:
		if m.Form != nil {
			return m.Form.Fields[m.Form.Selected].Label + ": "
		}
	}
	return ""
}

// ConfirmMessage returns the localized confirmation text.
func (m *Model) ConfirmMessage() string {
	if m.Confirm == nil {
		return ""
	}
	return m.Tr.Format(m.Confirm.MessageKey, m.Confirm.Args...)
}
