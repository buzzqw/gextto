package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/rss"
	"github.com/buzzqw/gextto/internal/settings"
)

func TestMigrateLegacyRulesDisablesThenStops(t *testing.T) {
	store, err := settings.Load(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	store.Set(rulesSettingKey, `[{"name":"a","match":{"include":["x"]}},`+
		`{"name":"b","enabled":false,"match":{}}]`)

	if n := migrateLegacyRules(store); n != 1 {
		t.Fatalf("disabled = %d, want 1", n)
	}
	var rules []rss.Rule
	if err := json.Unmarshal([]byte(store.Get(rulesSettingKey, "[]")), &rules); err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 || rss.RuleEnabled(rules[0]) || rss.RuleEnabled(rules[1]) {
		t.Fatalf("rules after migration = %+v, want both disabled", rules)
	}
	if got := store.Get(rulesMergedKey, ""); got != "1" {
		t.Fatalf("notice count = %q, want 1", got)
	}

	// The operator deliberately re-enables one: a later start must not turn it
	// off again, and the notice is not shown twice.
	store.Set(rulesSettingKey, `[{"name":"a","match":{}}]`)
	if n := migrateLegacyRules(store); n != 0 {
		t.Fatalf("second migration disabled = %d, want 0", n)
	}
	if !rss.RuleEnabled(mustParseRules(t, store.Get(rulesSettingKey, "[]"))[0]) {
		t.Fatal("the migration ran twice and disabled a rule the operator re-enabled")
	}
}

func mustParseRules(t *testing.T, raw string) []rss.Rule {
	t.Helper()
	var rules []rss.Rule
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		t.Fatal(err)
	}
	return rules
}

func TestRulesNoticeAppearsAndCanBeDismissed(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	d.opts.Settings.Set(rulesMergedKey, "2")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	page := func() string {
		t.Helper()
		resp, err := http.Get(srv.URL + "/ui/wanted")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return string(body)
	}
	if body := page(); !strings.Contains(body, "rules were switched off") || !strings.Contains(body, "/ui/rules-notice") {
		t.Fatal("the Series & movies page does not report the switched-off rules")
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.PostForm(srv.URL+"/ui/rules-notice", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/ui/wanted" {
		t.Fatalf("dismiss status = %d, location = %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if body := page(); strings.Contains(body, "rules were switched off") {
		t.Fatal("the notice came back after it was dismissed")
	}
}

func TestRuleEditorMovedToWantedPage(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	for path, want := range map[string]string{
		"/ui/rss":    `id="p-rules"`,
		"/ui/wanted": `id="advanced-rules"`,
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		has := strings.Contains(string(body), want)
		if (path == "/ui/rss" && has) || (path == "/ui/wanted" && !has) {
			t.Fatalf("%s: rules section presence = %v, want marker %q", path, has, want)
		}
	}
}
