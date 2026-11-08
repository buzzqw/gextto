package gextto

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestV2SearchRedirectTarget(t *testing.T) {
	cases := map[string]string{
		"/?view=search":      "/?view=search",
		"/?view=dashboard":   "/?view=dashboard",
		"/?view=downloads":   "/?view=downloads",
		"https://evil.test/": "/?view=search",
		"//evil.test":        "/?view=search",
		"":                   "/?view=search",
	}
	for value, want := range cases {
		if got := v2SearchRedirectTarget(value, "/?view=search"); got != want {
			t.Errorf("v2SearchRedirectTarget(%q) = %q, want %q", value, got, want)
		}
	}
}

// TestSearchRowsQueueWithoutLeavingTheList: the search returns a long list and
// the operator adds several releases one by one. The Aggiungi button must stay
// on the page with a toast, not reload it and lose the results.
func TestSearchRowsQueueWithoutLeavingTheList(t *testing.T) {
	view := v2SearchView{
		Redirect: "/?view=search",
		Searched: true,
		Results: []v2SearchResult{{
			Title:       "Example S01E01 1080p ITA",
			Addable:     true,
			AddBody:     `{"release":{"title":"Example S01E01 1080p ITA"}}`,
			ReleaseJSON: `{"title":"Example S01E01 1080p ITA"}`,
			ExplainJSON: `{"title":"Example S01E01 1080p ITA"}`,
		}},
	}
	var buffer bytes.Buffer
	if err := v2Templates.ExecuteTemplate(&buffer, "v2_search_results", view); err != nil {
		t.Fatalf("render: %v", err)
	}
	body := buffer.String()
	for _, want := range []string{
		`name="path" value="/api/search/add"`,
		`name="stay" value="1"`,
		`name="stay_title" value="Download"`,
		`name="body"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
}

// TestSearchAddFlashNamesTheRelease: the toast after adding a result must say
// what happened instead of the generic "Operazione completata.".
func TestSearchAddFlashNamesTheRelease(t *testing.T) {
	message, isError := v2ActionFlash("/api/search/add", []byte(`{"ok":true}`), http.StatusAccepted)
	if isError || message != "Release aggiunta ai download." {
		t.Fatalf("flash = %q error=%v", message, isError)
	}
}
