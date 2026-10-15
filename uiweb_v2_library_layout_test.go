package gextto

// uiweb_v2_library_layout_test.go covers the poster view of the Series and
// Movies lists: the switch, the cards built from the same data, the cookie that
// remembers the choice, and the fact that filtering applies to both.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// v2RequestWithCookie is v2Request plus a cookie, returning the response so its
// Set-Cookie can be inspected.
func v2RequestWithCookie(t *testing.T, server *httptest.Server, path, cookie string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	request.Header.Set("HX-Request", "true")
	if cookie != "" {
		request.Header.Set("Cookie", cookie)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return response
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestV2LibraryLayoutSwitchAndCards: ?layout=cards renders the poster cards with
// the title's own data, remembers the choice in a cookie, and the pages that do
// not carry the parameter keep reading the library that way.
func TestV2LibraryLayoutSwitchAndCards(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveLibrary(state.cfg.DataDir,
		[]SeriesConfig{{Name: "Test Show", Quality: "1080p", Language: "ita"}},
		[]MovieConfig{{ID: 7, Name: "Test Movie", Year: "2024", PosterPath: "/poster.jpg"}},
	); err != nil {
		t.Fatalf("SaveLibrary: %v", err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	// The list stays the default, and the switch is offered.
	code, body := v2Request(t, server, http.MethodGet, "/?view=movies", nil)
	if code != http.StatusOK {
		t.Fatalf("movies -> %d", code)
	}
	if !strings.Contains(body, "layout=cards") || !strings.Contains(body, "Locandine") {
		t.Fatal("the list does not offer the poster view")
	}
	if strings.Contains(body, `class="tmdb-grid library-cards"`) {
		t.Fatal("the list must not render the cards")
	}

	// The card view: one card per title, the stored poster, the title's own data
	// and the switch back to the list.
	code, body = v2Request(t, server, http.MethodGet, "/?view=movies&layout=cards", nil)
	if code != http.StatusOK {
		t.Fatalf("movies cards -> %d", code)
	}
	for _, marker := range []string{
		`class="tmdb-grid library-cards"`,
		"https://image.tmdb.org/t/p/w342/poster.jpg",
		"Test Movie",
		"/?view=movies&amp;movie=7",
		"layout=list",
		"Elenco",
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("the card view is missing %q", marker)
		}
	}

	// The choice is remembered: the response stores the cookie, and a later page
	// that does not carry the parameter is rendered with the cards.
	response := v2RequestWithCookie(t, server, "/?view=movies&layout=cards", "")
	stored := false
	for _, cookie := range response.Cookies() {
		if cookie.Name == v2LayoutCookie && cookie.Value == "cards" {
			stored = true
		}
	}
	readBody(t, response)
	if !stored {
		t.Fatal("the chosen layout is not remembered in a cookie")
	}
	response = v2RequestWithCookie(t, server, "/?view=series", "gx_layout=cards")
	body = readBody(t, response)
	if !strings.Contains(body, `class="tmdb-grid library-cards"`) || !strings.Contains(body, "Test Show") {
		t.Fatal("the remembered layout did not survive to the next page")
	}
}

// TestV2LibraryCardsFollowTheFilter: the cards come from the filtered items, so
// the poster view hides what the list hides.
func TestV2LibraryCardsFollowTheFilter(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveLibrary(state.cfg.DataDir, nil,
		[]MovieConfig{{ID: 1, Name: "Alpha"}, {ID: 2, Name: "Beta"}},
	); err != nil {
		t.Fatalf("SaveLibrary: %v", err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/?view=movies&layout=cards&f=beta", nil)
	if code != http.StatusOK {
		t.Fatalf("filtered cards -> %d", code)
	}
	if !strings.Contains(body, "Beta") || strings.Contains(body, "Alpha") {
		t.Fatal("the filter did not apply to the cards")
	}
}
