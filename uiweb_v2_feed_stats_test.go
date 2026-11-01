package gextto

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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
	if err := state.db.RecordSourceAttempt("feed", "one.example", true, 4, ""); err != nil {
		t.Fatalf("record attempt: %v", err)
	}
	if err := state.db.RecordSourceAccepted("feed", "one.example"); err != nil {
		t.Fatalf("record accepted: %v", err)
	}
	if err := state.db.MarkSourceAutoDisabled("feed", "two.example", "7", time.Now().UTC()); err != nil {
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
		"Attività dei feed",
		"Ultimo successo",
		"one.example",
		"1/1/0/1",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("feed panel missing %q:\n%s", want, body)
		}
	}
}
