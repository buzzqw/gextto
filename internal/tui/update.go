package tui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Update applies a key press and returns the action the runner must execute.
func (m *Model) Update(k Key) Action {
	if m.Prompt != nil {
		return m.updatePrompt(k)
	}
	if m.Confirm != nil {
		return m.updateConfirm(k)
	}
	switch m.Overlay {
	case OverlayHelp:
		switch {
		case k.Kind == KeyEsc || k.Kind == KeyEnter:
			m.Overlay = OverlayNone
		case k.Kind == KeyRune && (k.Rune == '?' || k.Rune == 'q'):
			m.Overlay = OverlayNone
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
	}

	switch {
	case k.Kind == KeyCtrlC:
		return Action{Kind: ActionQuit}
	case k.Kind == KeyRune && k.Rune == '?':
		m.Overlay = OverlayHelp
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
	case k.Kind == KeyTab:
		m.Tab = Tab((int(m.Tab) + 1) % tabCount)
		return m.loadTabAction()
	case k.Kind == KeyBackTab:
		m.Tab = Tab((int(m.Tab) + tabCount - 1) % tabCount)
		return m.loadTabAction()
	case k.Kind == KeyRune && k.Rune >= '1' && k.Rune <= '7':
		m.Tab = Tab(k.Rune - '1')
		return m.loadTabAction()
	}

	switch m.Tab {
	case TabTorrents:
		return m.updateTorrents(k)
	case TabLogs:
		return m.updateLogs(k)
	case TabHealth:
		if k.Kind == KeyRune && k.Rune == 'x' {
			m.Confirm = &confirm{MessageKey: "prompt.cleantrash", Action: Action{Kind: ActionCleanTrash}}
		}
	case TabArchive:
		return m.updateArchive(k)
	case TabMissing:
		return m.updateMissing(k)
	case TabBlocklist:
		return m.updateBlocklist(k)
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

func (m *Model) updateConfirm(k Key) Action {
	yes := false
	switch k.Kind {
	case KeyEnter:
		yes = true
	case KeyEsc:
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
		return Action{Kind: ActionQueueRelease, Release: m.SearchResults[m.SearchSelected]}
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
	count := len(m.VisibleTorrents())
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
		torrent := m.SelectedTorrent()
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
		case 'u':
			return Action{Kind: ActionUnpin}
		case 'X':
			m.Confirm = &confirm{MessageKey: "prompt.cleancomp", Action: Action{Kind: ActionCleanCompleted}}
		case 'L':
			m.Prompt = newPrompt(PromptLimits, "")
		case 'o':
			m.Sort = SortMode((int(m.Sort) + 1) % 5)
		case 'O':
			m.SortDesc = !m.SortDesc
		case 'F':
			m.Prompt = newPrompt(PromptTorrentFilter, m.Filter)
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

func (m *Model) loadTabAction() Action {
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

func (m *Model) updateMissing(k Key) Action {
	if k.Kind == KeyUp || k.Kind == KeyDown || k.Kind == KeyPgUp || k.Kind == KeyPgDn || k.Kind == KeyHome || k.Kind == KeyEnd {
		m.MissingSelected = moveSelection(m.MissingSelected, len(m.Missing), k.Kind)
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
