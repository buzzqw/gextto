package tui

import "testing"

func cmdRune(r rune) Key     { return Key{Kind: KeyRune, Rune: r} }
func cmdPlain(k KeyKind) Key { return Key{Kind: k} }

func TestCommandPaletteOpensFiltersAndRuns(t *testing.T) {
	model := NewModel(NewTranslator("en"))
	model.Update(cmdRune(':'))
	if model.Overlay != OverlayCommands || model.Prompt == nil || model.Prompt.Kind != PromptCommand {
		t.Fatalf("':' did not open the command palette: overlay %v prompt %+v", model.Overlay, model.Prompt)
	}
	for _, r := range "logs" {
		model.Update(cmdRune(r))
	}
	visible := visibleCommands(model.Tr, model.PromptText())
	if len(visible) != 1 || visible[0].id != "logs" {
		t.Fatalf("filter found %+v", visible)
	}
	model.Update(cmdPlain(KeyEnter))
	if model.Overlay != OverlayNone || model.Prompt != nil {
		t.Fatalf("palette did not close: overlay %v prompt %+v", model.Overlay, model.Prompt)
	}
	if model.Tab != TabLogs {
		t.Fatalf("command did not switch tab: %v", model.Tab)
	}
}

func TestCommandPaletteNavigation(t *testing.T) {
	model := NewModel(NewTranslator("en"))
	model.Update(cmdRune(':'))
	model.Update(cmdPlain(KeyDown))
	action := model.Update(cmdPlain(KeyEnter))
	if action.Kind != ActionNone || model.Prompt == nil || model.Prompt.Kind != PromptCycle {
		t.Fatalf("Down did not pick the second command: action %+v prompt %+v", action, model.Prompt)
	}
}

func TestCommandPaletteEscClosesEverything(t *testing.T) {
	model := NewModel(NewTranslator("en"))
	model.Update(cmdRune(':'))
	model.Update(cmdPlain(KeyEsc))
	if model.Overlay != OverlayNone || model.Prompt != nil {
		t.Fatalf("Esc did not close the palette: overlay %v prompt %+v", model.Overlay, model.Prompt)
	}
}

func TestPromptHistoryRecall(t *testing.T) {
	model := NewModel(NewTranslator("en"))
	model.Prompt = newPrompt(PromptSearch, "first query")
	model.Update(cmdPlain(KeyEnter))
	model.Prompt = newPrompt(PromptSearch, "")
	model.Update(cmdPlain(KeyUp))
	if model.Prompt.Buffer != "first query" {
		t.Fatalf("Up recalled %q", model.Prompt.Buffer)
	}
	model.Update(cmdPlain(KeyDown))
	if model.Prompt.Buffer != "" {
		t.Fatalf("Down did not return to the draft: %q", model.Prompt.Buffer)
	}
}
