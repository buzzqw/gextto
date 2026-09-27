package tui

import (
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
		}
		return Action{}
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

	switch {
	case k.Kind == KeyRune && k.Rune == 'r':
		m.Loading = true
		return Action{Kind: ActionRefresh}
	case k.Kind == KeyRune && k.Rune == 'a':
		m.Prompt = &prompt{Kind: PromptMagnet}
		return Action{}
	case k.Kind == KeyRune && k.Rune == 't':
		m.Prompt = &prompt{Kind: PromptFile}
		return Action{}
	case k.Kind == KeyRune && k.Rune == 'c':
		m.Prompt = &prompt{Kind: PromptCycle}
		return Action{}
	case k.Kind == KeyRune && k.Rune == 's':
		m.Prompt = &prompt{Kind: PromptSearch}
		return Action{}
	case k.Kind == KeyRune && k.Rune == 'e':
		return Action{Kind: ActionLoadEvents}
	case k.Kind == KeyTab:
		m.Tab = Tab((int(m.Tab) + 1) % 4)
		return Action{}
	case k.Kind == KeyBackTab:
		m.Tab = Tab((int(m.Tab) + 3) % 4)
		return Action{}
	case k.Kind == KeyRune && k.Rune >= '1' && k.Rune <= '4':
		m.Tab = Tab(k.Rune - '1')
		return Action{}
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
	}
	return Action{}
}

func (m *Model) updatePrompt(k Key) Action {
	switch k.Kind {
	case KeyEsc:
		m.Prompt = nil
		m.Message = m.Tr.T("msg.cancelled")
	case KeyEnter:
		return m.submitPrompt()
	case KeyBackspace:
		if len(m.Prompt.Buffer) > 0 {
			m.Prompt.Buffer = m.Prompt.Buffer[:len(m.Prompt.Buffer)-1]
		}
	case KeyRune:
		if k.Rune >= 32 {
			m.Prompt.Buffer += string(k.Rune)
		}
	}
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
	case k.Kind == KeyEsc:
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
			m.Prompt = &prompt{Kind: PromptLimits}
		case 'o':
			m.Sort = SortMode((int(m.Sort) + 1) % 5)
		case 'O':
			m.SortDesc = !m.SortDesc
		case 'F':
			m.Prompt = &prompt{Kind: PromptTorrentFilter, Buffer: m.Filter}
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
			m.Prompt = &prompt{Kind: PromptLogFilter, Buffer: m.LogFilter}
		case 'f':
			m.LogFollow = !m.LogFollow
			if m.LogFollow {
				m.LogScroll = 0
			}
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
