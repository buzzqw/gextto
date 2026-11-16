package tui

// render_blocklist_test.go covers the Blocklist tab: the code the daemon writes
// must never reach the reader, and a code this version does not know is shown
// as it is instead of disappearing.

import (
	"strings"
	"testing"
)

func TestBlocklistTabSaysTheReasonInWords(t *testing.T) {
	model := NewModel(NewTranslator(LangEN))
	model.Blocklist = []BlocklistEntry{{Hash: "abc", Title: "Blocked release", Reason: "dead_swarm", CreatedAt: "2026-11-16"}}
	lines := model.renderBlocklist(120, 10)
	text := ""
	for _, line := range lines {
		text += line.Text + "\n"
	}
	if strings.Contains(text, "dead_swarm") {
		t.Fatalf("the internal code reached the tab:\n%s", text)
	}
	if !strings.Contains(text, "nobody was sharing it") {
		t.Fatalf("the reason is not written in words:\n%s", text)
	}

	model.Blocklist = []BlocklistEntry{{Hash: "abc", Title: "Blocked release", Reason: "a_code_from_the_future", CreatedAt: "2026-11-16"}}
	text = ""
	for _, line := range model.renderBlocklist(120, 10) {
		text += line.Text + "\n"
	}
	if !strings.Contains(text, "a_code_from_the_future") {
		t.Fatalf("an unknown reason must stay visible:\n%s", text)
	}
}
