package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/ed2k"
)

type fakeSearcher struct {
	files []ed2k.SearchFile
	err   error
	nodes []ed2k.Contact
	id    ed2k.ID
}

func (f *fakeSearcher) Search(context.Context, string) ([]ed2k.SearchFile, error) {
	return f.files, f.err
}
func (f *fakeSearcher) Stats() ed2k.Stats {
	return ed2k.Stats{Nodes: len(f.nodes), Bootstrapped: len(f.nodes) > 0, Queries: 3}
}
func (f *fakeSearcher) Nodes() []ed2k.Contact { return f.nodes }
func (f *fakeSearcher) SelfID() ed2k.ID       { return f.id }
func (f *fakeSearcher) UDPPort() int          { return 4672 }
func (f *fakeSearcher) Close() error          { return nil }

func testDaemon(t *testing.T) (*Daemon, *fakeSearcher) {
	t.Helper()
	id, _ := ed2k.ParseID("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	node, _ := ed2k.ParseID("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	searcher := &fakeSearcher{
		files: []ed2k.SearchFile{{
			Name: "Rare Movie 1974.avi", Size: 700, Hash: "cccccccccccccccccccccccccccccccc", Sources: 5, Origin: "kad",
		}},
		nodes: []ed2k.Contact{{ID: node, UDPPort: 4672, TCPPort: 4662, Version: 3}},
		id:    id,
	}
	return newDaemon(daemonOptions{DataDir: t.TempDir(), Mode: modeStandalone, Lang: "en"}, searcher), searcher
}

func TestHealthSaysSearchOnly(t *testing.T) {
	daemon, _ := testDaemon(t)
	recorder := httptest.NewRecorder()
	daemon.handleHealth(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["download"] != false || body["search"] != true {
		t.Fatalf("capabilities = %+v", body)
	}
	if body["kad_id"] != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("kad_id = %v", body["kad_id"])
	}
}

func TestSearchEndpoint(t *testing.T) {
	daemon, _ := testDaemon(t)
	recorder := httptest.NewRecorder()
	daemon.handleSearch(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/search?q=rare%20movie", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		OK      bool        `json:"ok"`
		Results []apiResult `json:"results"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.OK || len(body.Results) != 1 {
		t.Fatalf("body = %+v", body)
	}
	if !strings.HasPrefix(body.Results[0].Link, "ed2k://|file|Rare") {
		t.Fatalf("link = %q", body.Results[0].Link)
	}
}

func TestLinkEndpoint(t *testing.T) {
	daemon, _ := testDaemon(t)
	valid := `{"link":"ed2k://|file|Rare%20Movie.avi|700|cccccccccccccccccccccccccccccccc|/"}`
	recorder := httptest.NewRecorder()
	daemon.handleLink(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/link", strings.NewReader(valid)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("valid link status = %d: %s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	daemon.handleLink(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/link", strings.NewReader(`{"link":"nope"}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid link status = %d", recorder.Code)
	}
}

func TestAPIAuthentication(t *testing.T) {
	daemon, _ := testDaemon(t)
	daemon.opts.Token = "secret"
	handler := daemon.withAuth(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("without token = %d", recorder.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.Header.Set("X-Gx-Token", "secret")
	recorder = httptest.NewRecorder()
	handler(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("with token = %d", recorder.Code)
	}
}

func TestPageTemplateStringsAreTranslated(t *testing.T) {
	raw, err := pageFS.ReadFile("page.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	templateKeys := map[string]bool{}
	for _, match := range regexp.MustCompile(`\{\{\.T\.([A-Za-z0-9_]+)\}\}`).FindAllStringSubmatch(string(raw), -1) {
		templateKeys[match[1]] = true
	}
	if len(templateKeys) == 0 {
		t.Fatal("no template keys found")
	}
	for key := range templateKeys {
		english, ok := uiEnglish[key]
		if !ok || strings.TrimSpace(english) == "" {
			t.Errorf("missing English string for %q", key)
		}
		values, ok := uiCatalog[key]
		if !ok {
			t.Errorf("missing catalog entry for %q", key)
			continue
		}
		for column, value := range values {
			if strings.TrimSpace(value) == "" {
				t.Errorf("%q has an empty translation in column %d", key, column)
			}
		}
	}
	for _, key := range clientKeys {
		if _, ok := uiEnglish[key]; !ok {
			t.Errorf("client key %q has no English string", key)
		}
		if _, ok := uiCatalog[key]; !ok {
			t.Errorf("client key %q has no translations", key)
		}
	}
}

func TestEveryOptionHasATooltip(t *testing.T) {
	raw, err := pageFS.ReadFile("page.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	control := regexp.MustCompile(`<(?:input|select|textarea)\b[^>]*>`)
	for _, tag := range control.FindAllString(text, -1) {
		lower := strings.ToLower(tag)
		if strings.Contains(lower, `type="hidden"`) || strings.Contains(lower, `type="submit"`) || strings.Contains(lower, `type="button"`) {
			continue
		}
		if !strings.Contains(lower, " title=") && !strings.Contains(lower, " aria-label=") {
			t.Errorf("control without a tooltip: %s", tag)
		}
	}
}

func TestUITemplateRenders(t *testing.T) {
	daemon, _ := testDaemon(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/?lang=it", nil)
	daemon.handleUI(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	body, _ := io.ReadAll(recorder.Body)
	if !strings.Contains(string(body), "Ricerca eD2k/Kad") {
		t.Fatalf("Italian page missing translated subtitle: %s", string(body)[:200])
	}
	if !strings.Contains(string(body), "gx-ed2k") {
		t.Fatal("brand missing")
	}
}
