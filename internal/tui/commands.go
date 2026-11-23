package tui

import "strings"

// command is one entry of the ':' palette. The id is also a search term, so a
// command can be found by typing its English name in any interface language.
type command struct {
	id       string
	labelKey string
	run      func(*Model) Action
}

func commandsList() []command {
	goTo := func(tab Tab) func(*Model) Action {
		return func(m *Model) Action {
			m.setTab(tab)
			return m.loadTabAction()
		}
	}
	return []command{
		{"refresh", "cmd.refresh", func(m *Model) Action { m.Loading = true; return Action{Kind: ActionRefresh} }},
		{"cycle", "cmd.cycle", func(m *Model) Action { m.Prompt = newPrompt(PromptCycle, ""); return Action{} }},
		{"search", "cmd.search", func(m *Model) Action { m.Prompt = newPrompt(PromptSearch, ""); return Action{} }},
		{"magnet", "cmd.magnet", func(m *Model) Action { m.Prompt = newPrompt(PromptMagnet, ""); return Action{} }},
		{"file", "cmd.file", func(m *Model) Action { m.Prompt = newPrompt(PromptFile, ""); return Action{} }},
		{"events", "cmd.events", func(m *Model) Action { return Action{Kind: ActionLoadEvents} }},
		{"settings", "cmd.settings", func(m *Model) Action {
			m.Overlay = OverlaySettings
			m.SettingsSelected = 0
			if m.Config == nil {
				return Action{Kind: ActionLoadConfig}
			}
			return Action{}
		}},
		{"help", "cmd.help", func(m *Model) Action { m.Overlay = OverlayHelp; m.HelpScroll = 0; return Action{} }},
		{"status", "cmd.status", goTo(TabStatus)},
		{"torrents", "cmd.torrents", goTo(TabTorrents)},
		{"logs", "cmd.logs", goTo(TabLogs)},
		{"health", "cmd.health", goTo(TabHealth)},
		{"archive", "cmd.archive", goTo(TabArchive)},
		{"missing", "cmd.missing", goTo(TabMissing)},
		{"blocklist", "cmd.blocklist", goTo(TabBlocklist)},
		{"library", "cmd.library", goTo(TabLibrary)},
		{"maintenance", "cmd.maintenance", goTo(TabMaintenance)},
		{"quit", "cmd.quit", func(*Model) Action { return Action{Kind: ActionQuit} }},
	}
}

// visibleCommands returns the palette entries matching what the user typed,
// searching both the translated label and the stable id.
func visibleCommands(tr *Translator, typed string) []command {
	all := commandsList()
	needle := strings.ToLower(strings.TrimSpace(typed))
	if needle == "" {
		return all
	}
	matched := make([]command, 0, len(all))
	for _, cmd := range all {
		if strings.Contains(strings.ToLower(tr.T(cmd.labelKey)), needle) || strings.Contains(cmd.id, needle) {
			matched = append(matched, cmd)
		}
	}
	return matched
}

func (m *Model) moveCommandSelection(delta int) {
	count := len(visibleCommands(m.Tr, m.PromptText()))
	if count == 0 {
		m.CommandSelected = 0
		return
	}
	m.CommandSelected = (m.CommandSelected + delta + count) % count
}

// PromptText is the current input buffer, or an empty string outside a prompt.
func (m *Model) PromptText() string {
	if m.Prompt == nil {
		return ""
	}
	return m.Prompt.Buffer
}
