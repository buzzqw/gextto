package gextto

// movie_metadata_refresh_test.go covers «Aggiorna TMDB» on a movie added by
// hand: the page posts no TMDB id, so the handler resolves it by name and year,
// saves the metadata and reads the alternative titles in the same action. That
// is what makes the «Titoli alternativi (TMDB)» row appear straight away.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestApplyMovieMetadataResolvesTMDBIDAndAliases(t *testing.T) {
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/movie":
			if got := r.URL.Query().Get("query"); got != "Hand Added" {
				t.Errorf("search query = %q", got)
			}
			if got := r.URL.Query().Get("year"); got != "2026" {
				t.Errorf("search year = %q", got)
			}
			tmdbWriteJSON(t, w, `{"results":[{"id":424242,"title":"Hand Added","original_title":"Aggiunto a mano","release_date":"2026-03-01"}]}`)
		case "/movie/424242":
			tmdbWriteJSON(t, w, `{"id":424242,"title":"Hand Added","original_title":"Aggiunto a mano","overview":"Trama","poster_path":"/p.jpg","release_date":"2026-03-01"}`)
		case "/movie/424242/alternative_titles":
			tmdbWriteJSON(t, w, `{"titles":[{"title":"Hand Added","iso_3166_1":"IT"},{"title":"Foreign Name","iso_3166_1":"US"}]}`)
		case "/movie/424242/translations":
			tmdbWriteJSON(t, w, `{"translations":[{"iso_639_1":"en","data":{"title":"Foreign Name"}},{"iso_639_1":"it","data":{"title":"Aggiunto a mano"}},{"iso_639_1":"fr","data":{"title":"Nom étranger"}}]}`)
		default:
			t.Errorf("unexpected TMDB path %q", r.URL.Path)
			tmdbWriteJSON(t, w, `{}`)
		}
	})

	state := newTestAppState(t)
	key := "test-key"
	cfg := *state.cfg
	cfg.TmdbAPIKey = &key
	cfg.Movies = []MovieConfig{{ID: 9, Name: "Hand Added", Year: "2026", Enabled: true}}
	state.config_cache = &ConfigCache{generation: ConfigGeneration(), cfg: &cfg, valid: true}

	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	status, body := webPostJSON(t, server, "/api/movies/9/metadata", `{"source":"tmdb"}`)
	if status != http.StatusOK {
		t.Fatalf("refresh -> %d %s", status, body)
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	movie, _ := decoded["movie"].(map[string]any)
	if got := movie["tmdb_id"]; got != "424242" {
		t.Fatalf("tmdb_id = %v, want 424242", got)
	}
	aliases, _ := movie["aliases"].([]any)
	if len(aliases) != 1 || aliases[0] != "Foreign Name" {
		t.Fatalf("aliases = %v, want [Foreign Name]", aliases)
	}
	translations, _ := movie["title_translations"].(map[string]any)
	if translations["en"] != "Foreign Name" || translations["it"] != "Aggiunto a mano" {
		t.Fatalf("title_translations = %v", translations)
	}

	// The saved library carries the same titles, and the movie page shows them.
	saved := latestConfig(state)
	if len(saved.Movies) != 1 || saved.Movies[0].TmdbID != "424242" || len(saved.Movies[0].Aliases) != 1 {
		t.Fatalf("saved movie = %+v", saved.Movies)
	}
	if got := saved.Movies[0].TitleTranslations; got["en"] != "Foreign Name" || got["fr"] != "Nom étranger" {
		t.Fatalf("saved title translations = %#v", got)
	}
	code, page := v2Request(t, server, http.MethodGet, "/?view=movies&movie=9", nil)
	if code != http.StatusOK || !strings.Contains(page, "Titoli alternativi (TMDB)") || !strings.Contains(page, "Foreign Name") {
		t.Fatalf("movie page does not show the titles: %d", code)
	}
}
