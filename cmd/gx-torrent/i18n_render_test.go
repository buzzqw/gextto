package main

// i18n_render_test.go renders the daemon pages in a language other than
// English and fails when a label is still written in the source language. The
// static guard (i18n_coverage_test.go) covers the templates; this one covers
// the markup the handlers build in Go, which no scan of the assets can see.

import (
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// daemonPageBodies fetches the pages a standalone daemon serves on the local
// network, in the language set in the store.
func daemonPageBodies(t *testing.T) map[string]string {
	t.Helper()
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	d.opts.Settings.Set("lang", "de")
	// One of everything, so the rows the handlers build in Go are rendered too.
	d.opts.Settings.Set(feedsSettingKey, `[{"name":"xFeed","url":"http://127.0.0.1:1/rss"}]`)
	d.opts.Settings.Set(rulesSettingKey, `[{"name":"xRule","match":{"include":["720p"],"exclude":["cam"]},"action":{"savepath":"/media","tags":["ita"]}}]`)
	d.opts.Settings.Set(indexersSettingKey, `[{"name":"xIndexer","url":"http://127.0.0.1:1","apikey":"k"}]`)
	d.opts.Settings.Set(wantedSettingKey, `[{"name":"xShow","kind":"series","seasons":"1+"}]`)
	d.mu.Lock()
	d.state.Categories = map[string]string{"xCategory": ""}
	d.mu.Unlock()
	srv := httptest.NewServer(d.routes())
	t.Cleanup(srv.Close)
	out := map[string]string{}
	for path := range map[string]bool{"/": true, "/ui/rss": true, "/ui/settings": true, "/ui/wanted": true, "/ui/archive": true, "/ui/stats": true} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d", path, resp.StatusCode)
		}
		out[path] = string(body)
	}
	return out
}

// untranslatedKeys returns the text and the attributes of a page that are still
// written as a catalog key, so they were never translated.
func untranslatedKeys(body, lang string, except map[string]bool) []string {
	text := uiI18nStrip(body)
	var problems []string
	seen := map[string]bool{}
	record := func(value string) {
		value = strings.TrimSpace(html.UnescapeString(value))
		if value == "" || seen[value] || except[value] || uiI18nAllowlist[value] {
			return
		}
		if _, ok := uiCatalog[value]; !ok {
			return
		}
		// A phrase whose translation reads the same is translated already.
		if translated := uiDictionary(lang)[value]; translated == "" || translated == value {
			return
		}
		seen[value] = true
		problems = append(problems, value)
	}
	for _, m := range uiI18nTextNode.FindAllStringSubmatch(text, -1) {
		if !strings.Contains(m[1], "{{") {
			record(m[1])
		}
	}
	for _, name := range uiI18nTranslatableAttrs {
		attr := regexp.MustCompile(`\s` + regexp.QuoteMeta(name) + `="([^"]*)"`)
		for _, m := range attr.FindAllStringSubmatch(text, -1) {
			record(m[1])
		}
	}
	sort.Strings(problems)
	return problems
}

// TestDaemonPagesTranslateEveryLabel renders the pages in German: a label that
// is still an English catalog key means a fragment built in Go was never
// translated.
func TestDaemonPagesTranslateEveryLabel(t *testing.T) {
	for path, body := range daemonPageBodies(t) {
		problems := untranslatedKeys(body, "de", nil)
		for _, problem := range problems {
			t.Errorf("%s: label not translated: %q", path, problem)
		}
	}
}

// TestDaemonPageKeepsDataUntranslated is the guard of the other direction: a
// value is printed as it is, even when it is exactly an English label. The
// page used to be translated after rendering, so a category named "All" was
// served as "Alle".
func TestDaemonPageKeepsDataUntranslated(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	d.opts.Settings.Set("lang", "de")
	d.mu.Lock()
	d.state.Categories = map[string]string{"All": "/media/all", "Dateien": "/media/de"}
	d.mu.Unlock()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/", nil)
	page, err := d.uiPageData(0, "de")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	d.renderUI(rec, req, "", page)
	body := rec.Body.String()
	// A category button carries the name as data and as its label: both stay
	// as the operator typed them.
	button := regexp.MustCompile(`data-cat="All"[^>]*>.*?<span class="lbl">([^<]*)</span>`).FindStringSubmatch(body)
	if button == nil {
		t.Fatal("the category was not rendered")
	}
	if got := button[1]; got != "All" {
		t.Errorf("a category named All was translated to %q", got)
	}
	if strings.Contains(body, `data-cat="Dateien"`) && !strings.Contains(body, `>Dateien</span>`) {
		t.Fatal("a category named Dateien was translated")
	}
	// The filter labels around the data are translated.
	if !strings.Contains(body, `>`+uiTr("de", "Uncategorized")+`</span>`) {
		t.Error("the uncategorized filter is not translated")
	}
}
