package gextto

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A listing whose rows the parser no longer reads must not pass for an uploader
// with nothing new: the state says the page changed.
func TestFeedStatsSayPageChangedWhenRowsYieldNoRelease(t *testing.T) {
	state := newTestAppState(t)
	lines := []string{"https://one.example/feed.xml"}
	encoded, _ := json.Marshal(lines)
	if err := saveConfigSetting(state.cfg.DataDir, "url", string(encoded)); err != nil {
		t.Fatalf("save feed setting: %v", err)
	}
	key := feedStatKey("https://one.example/feed.xml")
	if err := state.db.RecordSourceAttempt(SourceAttempt{Kind: "feed", Provider: key, OK: true, Results: 0, Rows: 50}); err != nil {
		t.Fatalf("record attempt: %v", err)
	}
	table, _, _ := v2FeedStatsFrom(state)
	if len(table) != 1 {
		t.Fatalf("feed table = %+v, want one row", table)
	}
	row := table[0]
	if row.State != "pagina cambiata" || row.StateClass != "warn" || !row.PageChanged {
		t.Fatalf("state = %q (%q, page changed %v), want the page-changed warning",
			row.State, row.StateClass, row.PageChanged)
	}
	if row.LastRows != 50 || row.LastResults != 0 {
		t.Fatalf("last read = %d rows, %d releases, want 50 and 0", row.LastRows, row.LastResults)
	}

	// The same feed, read again after the page went back to normal, is healthy
	// again: the warning belongs to the last read only.
	if err := state.db.RecordSourceAttempt(SourceAttempt{Kind: "feed", Provider: key, OK: true, Results: 50, Rows: 50}); err != nil {
		t.Fatalf("record second attempt: %v", err)
	}
	table, _, _ = v2FeedStatsFrom(state)
	if table[0].State == "pagina cambiata" || table[0].PageChanged {
		t.Fatalf("state after a good read = %q, want the feed healthy", table[0].State)
	}
}

func TestFeedStatsPanelShowsActivityAndDisabledFrame(t *testing.T) {
	state := newTestAppState(t)
	lines := []string{
		"https://one.example/feed.xml",
		"# https://two.example/feed.xml",
	}
	encoded, _ := json.Marshal(lines)
	if err := saveConfigSetting(state.cfg.DataDir, "url", string(encoded)); err != nil {
		t.Fatalf("save feed setting: %v", err)
	}
	if err := state.db.RecordSourceAttempt(SourceAttempt{Kind: "feed", Provider: feedStatKey("https://one.example/feed.xml"), OK: true, Results: 4, Rows: 50}); err != nil {
		t.Fatalf("record attempt: %v", err)
	}
	if err := state.db.RecordSourceAccepted("feed", feedStatKey("https://one.example/feed.xml")); err != nil {
		t.Fatalf("record accepted: %v", err)
	}
	if err := state.db.MarkSourceAutoDisabled("feed", feedStatKey("https://two.example/feed.xml"), "7", time.Now().UTC()); err != nil {
		t.Fatalf("mark disabled: %v", err)
	}

	server := httptest.NewServer(Router(state))
	defer server.Close()
	code, body := v2Request(t, server, http.MethodGet, "/settings/body?tab=sources", nil)
	if code != http.StatusOK {
		t.Fatalf("sources body = %d: %s", code, body)
	}
	for _, want := range []string{
		"Feed disabilitati",
		"Questi feed non vengono letti",
		"Disabilitato automaticamente",
		"two.example",
		"L&#39;andamento di ogni feed è in",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("sources panel missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Ultimo successo") {
		t.Error("the activity table is still on the Sources tab")
	}

	code, body = v2Request(t, server, http.MethodGet, "/?view=health", nil)
	if code != http.StatusOK {
		t.Fatalf("health page = %d: %s", code, body)
	}
	for _, want := range []string{
		"Attività dei feed",
		"Ultimo successo",
		"one.example",
		"1/1/0/1",
		"Feed disabilitati",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("health feed panel missing %q:\n%s", want, body)
		}
	}
}
