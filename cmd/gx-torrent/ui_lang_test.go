package main

// ui_lang_test.go covers the language picker in the sidebar of the standalone
// page: it must be on the first page (not only in Settings), it must submit the
// chosen language to /ui/lang, and the periodic refresh of the filters must not
// carry it (or the choice would be reset while the operator uses it).

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestUILanguagePickerOnThePage checks the sidebar picker is rendered on the
// page, translated, with the current language selected.
func TestUILanguagePickerOnThePage(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set("lang", "de")
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/", nil)
	page := uiPageData{Standalone: true, LangOptions: uiLanguageChoices(d.uiLang(req))}

	rec := httptest.NewRecorder()
	d.renderUI(rec, req, "", page)
	body := rec.Body.String()

	for _, want := range []string{
		`action="/ui/lang"`,
		`name="lang"`,
		`<option value="de" selected`,
		`<option value="it">`,
		`<html lang="de">`,
		"Sprache", // the section heading, translated
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q", want)
		}
	}

	// The filters travel as a fragment every couple of seconds: the picker
	// must stay out of it, so a refresh cannot reset the open select.
	rec = httptest.NewRecorder()
	d.renderUI(rec, req, "fragments", page)
	if strings.Contains(rec.Body.String(), "/ui/lang") {
		t.Error("the refresh fragment carries the language picker")
	}
}

// TestUILanguagePickerIsAbsentWhenManaged checks the picker belongs to the
// standalone page only: Gextto passes the language to its daemon.
func TestUILanguagePickerIsAbsentWhenManaged(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/", nil)
	rec := httptest.NewRecorder()
	d.renderUI(rec, req, "", uiPageData{LangOptions: uiLanguageChoices("en")})
	if strings.Contains(rec.Body.String(), "/ui/lang") {
		t.Error("managed page shows the standalone language picker")
	}
}

// TestUILanguageSubmissionChangesThePage checks /ui/lang stores the choice:
// the next request is translated, like the settings page's field does.
func TestUILanguageSubmissionChangesThePage(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	resp, err := client.PostForm(srv.URL+"/ui/lang", url.Values{"lang": {"it"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := d.opts.Settings.Get("lang", ""); got != "it" {
		t.Fatalf("saved language = %q, want it", got)
	}
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "ok=1") {
		t.Errorf("redirect = %q, want a success notice", loc)
	}

	// An unknown code is refused and leaves the stored language alone.
	resp, err = client.PostForm(srv.URL+"/ui/lang", url.Values{"lang": {"xx"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := d.opts.Settings.Get("lang", ""); got != "it" {
		t.Fatalf("language overwritten by an unknown code: %q", got)
	}
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "ok=0") {
		t.Errorf("redirect = %q, want an error notice", loc)
	}
}

// TestUILanguageHandlerNeedsStandalone checks the route answers 404 where the
// language is not the daemon's own setting.
func TestUILanguageHandlerNeedsStandalone(t *testing.T) {
	d := &Daemon{opts: Options{Mode: ModeManaged}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8890/ui/lang", nil)
	d.handleUILang(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
