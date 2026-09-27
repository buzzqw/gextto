package gextto

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// tvdbTestServer points tvdbAPI at a hermetic httptest server for the duration
// of the test.
func tvdbTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	previous := tvdbAPI
	tvdbAPI = server.URL
	t.Cleanup(func() {
		tvdbAPI = previous
		server.Close()
	})
	return server
}

func tvdbWriteJSON(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(body)); err != nil {
		t.Errorf("write tvdb response: %v", err)
	}
}

// tvdbLoginAndSearch wires a login endpoint returning tok and a search endpoint
// returning body for the requested type.
func tvdbLoginAndSearch(t *testing.T, token string, searchType string, body string) *httptest.Server {
	t.Helper()
	return tvdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			if r.Method != http.MethodPost {
				t.Errorf("login method = %q, want POST", r.Method)
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode login body: %v", err)
			}
			tvdbWriteJSON(t, w, `{"data":{"token":"`+token+`"}}`)
		case "/search":
			if got := r.Header.Get("Authorization"); got != "Bearer "+token {
				t.Errorf("Authorization = %q, want Bearer %s", got, token)
			}
			if got := r.URL.Query().Get("type"); got != searchType {
				t.Errorf("type = %q, want %q", got, searchType)
			}
			tvdbWriteJSON(t, w, body)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func TestTvdbConfiguredReportsKeyState(t *testing.T) {
	if New(nil).Configured() {
		t.Fatal("nil key should not be configured")
	}
	if New(ptr("   ")).Configured() {
		t.Fatal("blank key should not be configured")
	}
	if !New(ptr("abc")).Configured() {
		t.Fatal("non-blank key should be configured")
	}
}

func TestTvdbLoginTokenIsCachedAndHeadersAreSent(t *testing.T) {
	var logins, searches atomic.Int32
	tvdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			logins.Add(1)
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode login body: %v", err)
			}
			if payload["apikey"] != "key" {
				t.Errorf("apikey = %v", payload["apikey"])
			}
			tvdbWriteJSON(t, w, `{"data":{"token":"tok-1"}}`)
		case "/search":
			searches.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer tok-1" {
				t.Errorf("Authorization = %q", got)
			}
			if got := r.Header.Get("Accept-Language"); got != "ita" {
				t.Errorf("Accept-Language = %q, want ita", got)
			}
			if got := r.URL.Query().Get("query"); got != "breaking" {
				t.Errorf("query = %q", got)
			}
			tvdbWriteJSON(t, w, `{"data":[
				{"tvdb_id":81189,"name":"Breaking Bad","year":"2008","image_url":"b.jpg","overview":"Un insegnante."}
			]}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	client := New(ptr("key"))
	ctx := context.Background()
	first, err := client.SearchSeries(ctx, "breaking")
	if err != nil {
		t.Fatalf("SearchSeries: %v", err)
	}
	assertJSONEqual(t, first, `[{"tvdb_id":81189,"name":"Breaking Bad","year":"2008","image":"b.jpg","overview":"Un insegnante."}]`)

	second, err := client.SearchSeries(ctx, "breaking")
	if err != nil {
		t.Fatalf("second SearchSeries: %v", err)
	}
	assertJSONEqual(t, second, `[{"tvdb_id":81189,"name":"Breaking Bad","year":"2008","image":"b.jpg","overview":"Un insegnante."}]`)

	if logins.Load() != 1 {
		t.Fatalf("logins = %d, want 1 (token must be cached)", logins.Load())
	}
	if searches.Load() != 2 {
		t.Fatalf("searches = %d, want 2", searches.Load())
	}
}

func TestTvdbLanguageFallbackAndAcceptLanguage(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"blank", "", "ita"},
		{"whitespace", "   ", "ita"},
		{"explicit", "eng", "eng"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			client := WithLanguage(ptr("key"), testCase.in)
			if client.language != testCase.want {
				t.Fatalf("language = %q, want %q", client.language, testCase.want)
			}
			tvdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/login":
					tvdbWriteJSON(t, w, `{"data":{"token":"tok"}}`)
				case "/search":
					if got := r.Header.Get("Accept-Language"); got != testCase.want {
						t.Errorf("Accept-Language = %q, want %q", got, testCase.want)
					}
					tvdbWriteJSON(t, w, `{"data":[]}`)
				default:
					t.Errorf("unexpected path %q", r.URL.Path)
				}
			})
			if _, err := client.SearchSeries(context.Background(), "x"); err != nil {
				t.Fatalf("SearchSeries: %v", err)
			}
		})
	}
}

func TestTvdbSearchSeriesFieldFallbacks(t *testing.T) {
	tvdbLoginAndSearch(t, "tok", "series", `{"data":[
		{"tvdb_id":81189,"name":"A","year":2008,"image_url":"a.jpg","overview":null},
		{"objectID":"77","name":"B","thumbnail":"t.jpg"}
	]}`)

	items, err := New(ptr("key")).SearchSeries(context.Background(), "x")
	if err != nil {
		t.Fatalf("SearchSeries: %v", err)
	}
	assertJSONEqual(t, items, `[
		{"tvdb_id":81189,"name":"A","year":2008,"image":"a.jpg","overview":null},
		{"tvdb_id":"77","name":"B","year":null,"image":"t.jpg","overview":null}
	]`)
}

func TestTvdbSearchMoviesNormalisesFields(t *testing.T) {
	tvdbLoginAndSearch(t, "tok", "movie", `{"data":[
		{"movie_id":42,"name":"Film","originalName":"Film Originale","year":"1999","overview":"x","image_url":"p.jpg"},
		{"tvdb_id":7,"name":"Altro","thumbnail":"q.jpg"}
	]}`)

	items, err := New(ptr("key")).SearchMovies(context.Background(), "x")
	if err != nil {
		t.Fatalf("SearchMovies: %v", err)
	}
	assertJSONEqual(t, items, `[
		{"id":42,"title":"Film","original_title":"Film Originale","release_date":"1999","overview":"x","poster_path":"p.jpg"},
		{"id":7,"title":"Altro","original_title":null,"release_date":null,"overview":null,"poster_path":"q.jpg"}
	]`)
}

func TestTvdbMovieDetailsReturnsData(t *testing.T) {
	var hits atomic.Int32
	tvdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/login":
			tvdbWriteJSON(t, w, `{"data":{"token":"tok"}}`)
		case "/movies/12/extended":
			if got := r.Header.Get("Authorization"); got != "Bearer tok" {
				t.Errorf("Authorization = %q", got)
			}
			if got := r.Header.Get("Accept-Language"); got != "ita" {
				t.Errorf("Accept-Language = %q", got)
			}
			tvdbWriteJSON(t, w, `{"data":{"id":12,"name":"Film"}}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	})

	got, err := New(ptr("key")).MovieDetails(context.Background(), " 12 ")
	if err != nil {
		t.Fatalf("MovieDetails: %v", err)
	}
	assertJSONEqual(t, got, `{"id":12,"name":"Film"}`)
	if hits.Load() != 2 {
		t.Fatalf("hits = %d, want 2 (login + details)", hits.Load())
	}
}

func TestTvdbSeriesExtendedReturnsDataOrWholeValue(t *testing.T) {
	t.Run("data present", func(t *testing.T) {
		tvdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/login":
				tvdbWriteJSON(t, w, `{"data":{"token":"tok"}}`)
			case "/series/9/extended":
				tvdbWriteJSON(t, w, `{"data":{"id":9,"name":"Serie"}}`)
			default:
				t.Errorf("unexpected path %q", r.URL.Path)
			}
		})
		got, err := New(ptr("key")).SeriesExtended(context.Background(), 9)
		if err != nil {
			t.Fatalf("SeriesExtended: %v", err)
		}
		assertJSONEqual(t, got, `{"id":9,"name":"Serie"}`)
	})

	t.Run("data absent falls back to whole body", func(t *testing.T) {
		tvdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/login":
				tvdbWriteJSON(t, w, `{"data":{"token":"tok"}}`)
			case "/series/9/extended":
				tvdbWriteJSON(t, w, `{"name":"SenzaData"}`)
			default:
				t.Errorf("unexpected path %q", r.URL.Path)
			}
		})
		got, err := New(ptr("key")).SeriesExtended(context.Background(), 9)
		if err != nil {
			t.Fatalf("SeriesExtended: %v", err)
		}
		assertJSONEqual(t, got, `{"name":"SenzaData"}`)
	})
}

func TestTvdbSeriesCharactersFiltersAndCapsAtTen(t *testing.T) {
	// iterates `characters.iter().take(10)` and then filters, so only the
	// first ten input records are considered. The blank and fractional ids are
	// skipped and the two extra records are never reached.
	characters := `[
		{"personName":"Attore1","peopleId":1},
		{"name":"Attrice2","peopleId":2},
		{"personName":"SenzaId"},
		{"personName":"Frazionario","peopleId":3.5},
		{"personName":"Attore3","peopleId":3},
		{"personName":"Attore4","peopleId":4},
		{"personName":"Attore5","peopleId":5},
		{"personName":"Attore6","peopleId":6},
		{"personName":"Attore7","peopleId":7},
		{"personName":"Attore8","peopleId":8},
		{"personName":"Attore9","peopleId":9},
		{"personName":"Attore10","peopleId":10},
		{"personName":"Attore11","peopleId":11}
	]`
	tvdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			tvdbWriteJSON(t, w, `{"data":{"token":"tok"}}`)
		case "/series/9/extended":
			tvdbWriteJSON(t, w, `{"data":{"characters":`+characters+`}}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	})

	got, err := New(ptr("key")).SeriesCharacters(context.Background(), 9)
	if err != nil {
		t.Fatalf("SeriesCharacters: %v", err)
	}
	assertJSONEqual(t, got, `[
		{"name":"Attore1","tvdb_id":1},
		{"name":"Attrice2","tvdb_id":2},
		{"name":"Attore3","tvdb_id":3},
		{"name":"Attore4","tvdb_id":4},
		{"name":"Attore5","tvdb_id":5},
		{"name":"Attore6","tvdb_id":6},
		{"name":"Attore7","tvdb_id":7},
		{"name":"Attore8","tvdb_id":8}
	]`)
}

func TestTvdbSeriesCharactersOutputCappedAtTen(t *testing.T) {
	characters := `[
		{"personName":"Attore1","peopleId":1},
		{"personName":"Attore2","peopleId":2},
		{"personName":"Attore3","peopleId":3},
		{"personName":"Attore4","peopleId":4},
		{"personName":"Attore5","peopleId":5},
		{"personName":"Attore6","peopleId":6},
		{"personName":"Attore7","peopleId":7},
		{"personName":"Attore8","peopleId":8},
		{"personName":"Attore9","peopleId":9},
		{"personName":"Attore10","peopleId":10},
		{"personName":"Attore11","peopleId":11},
		{"personName":"Attore12","peopleId":12}
	]`
	tvdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			tvdbWriteJSON(t, w, `{"data":{"token":"tok"}}`)
		case "/series/9/extended":
			tvdbWriteJSON(t, w, `{"data":{"characters":`+characters+`}}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	})

	got, err := New(ptr("key")).SeriesCharacters(context.Background(), 9)
	if err != nil {
		t.Fatalf("SeriesCharacters: %v", err)
	}
	if len(got) != 10 {
		t.Fatalf("len = %d, want 10", len(got))
	}
	assertJSONEqual(t, got, `[
		{"name":"Attore1","tvdb_id":1},
		{"name":"Attore2","tvdb_id":2},
		{"name":"Attore3","tvdb_id":3},
		{"name":"Attore4","tvdb_id":4},
		{"name":"Attore5","tvdb_id":5},
		{"name":"Attore6","tvdb_id":6},
		{"name":"Attore7","tvdb_id":7},
		{"name":"Attore8","tvdb_id":8},
		{"name":"Attore9","tvdb_id":9},
		{"name":"Attore10","tvdb_id":10}
	]`)
}

func TestTvdbKeyError(t *testing.T) {
	for _, key := range []*string{nil, ptr("   ")} {
		_, err := New(key).SearchSeries(context.Background(), "x")
		if err == nil || err.Error() != "TVDB API key non configurata" {
			t.Fatalf("key %v: error = %v", key, err)
		}
	}
}

func TestTvdbLoginHTTPError(t *testing.T) {
	tvdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	_, err := New(ptr("key")).SearchSeries(context.Background(), "x")
	if err == nil || err.Error() != "TVDB login HTTP 401" {
		t.Fatalf("error = %v", err)
	}
}

func TestTvdbLoginWithoutToken(t *testing.T) {
	tvdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		tvdbWriteJSON(t, w, `{"data":{}}`)
	})
	_, err := New(ptr("key")).SearchSeries(context.Background(), "x")
	if err == nil || err.Error() != "TVDB login senza token" {
		t.Fatalf("error = %v", err)
	}
}

func TestTvdbSearchHTTPError(t *testing.T) {
	tvdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			tvdbWriteJSON(t, w, `{"data":{"token":"tok"}}`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	_, err := New(ptr("key")).SearchSeries(context.Background(), "x")
	if err == nil || err.Error() != "TVDB search HTTP 500" {
		t.Fatalf("error = %v", err)
	}
}

func TestTvdbMovieSearchHTTPError(t *testing.T) {
	tvdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			tvdbWriteJSON(t, w, `{"data":{"token":"tok"}}`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	_, err := New(ptr("key")).SearchMovies(context.Background(), "x")
	if err == nil || err.Error() != "TVDB movie search HTTP 500" {
		t.Fatalf("error = %v", err)
	}
}

func TestTvdbInvalidMovieID(t *testing.T) {
	_, err := New(ptr("key")).MovieDetails(context.Background(), "not-an-id")
	if err == nil || err.Error() != "invalid TVDB movie id" {
		t.Fatalf("error = %v", err)
	}
}

func TestTvdbMovieDetailsHTTPError(t *testing.T) {
	tvdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			tvdbWriteJSON(t, w, `{"data":{"token":"tok"}}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	_, err := New(ptr("key")).MovieDetails(context.Background(), "12")
	if err == nil || err.Error() != "TVDB movie details HTTP 404" {
		t.Fatalf("error = %v", err)
	}
}

func TestTvdbSeriesHTTPError(t *testing.T) {
	for _, call := range []struct {
		name string
		run  func(*TvdbClient) error
	}{
		{"extended", func(c *TvdbClient) error {
			_, err := c.SeriesExtended(context.Background(), 9)
			return err
		}},
		{"characters", func(c *TvdbClient) error {
			_, err := c.SeriesCharacters(context.Background(), 9)
			return err
		}},
	} {
		t.Run(call.name, func(t *testing.T) {
			tvdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/login" {
					tvdbWriteJSON(t, w, `{"data":{"token":"tok"}}`)
					return
				}
				w.WriteHeader(http.StatusInternalServerError)
			})
			err := call.run(New(ptr("key")))
			if err == nil || err.Error() != "TVDB series HTTP 500" {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestTvdbJSONHelpers(t *testing.T) {
	if _, ok := tvdbInt64(json.Number("3.5")); ok {
		t.Fatal("fractional json.Number must be rejected by tvdbInt64")
	}
	if value, ok := tvdbInt64(json.Number("42")); !ok || value != 42 {
		t.Fatalf("tvdbInt64(42) = %d, %v", value, ok)
	}
	if value, ok := tvdbInt64(float64(7)); !ok || value != 7 {
		t.Fatalf("tvdbInt64(7.0) = %d, %v", value, ok)
	}
	if text, ok := tvdbString("x"); !ok || text != "x" {
		t.Fatalf("tvdbString = %q, %v", text, ok)
	}
	if _, ok := tvdbString(1); ok {
		t.Fatal("tvdbString must reject non-strings")
	}
	value, err := tvdbJSON([]byte(`{"a":1,"b":[true,null]}`))
	if err != nil {
		t.Fatalf("tvdbJSON: %v", err)
	}
	if got := tvdbArray(tvdbGet(value, "b")); len(got) != 2 {
		t.Fatalf("tvdbArray = %v", got)
	}
	if got := tvdbGet(value, "missing"); got != nil {
		t.Fatalf("tvdbGet missing = %v", got)
	}
	if got := tvdbFirst(map[string]any{"a": 1, "b": 2}, "missing", "b"); got != 2 {
		t.Fatalf("tvdbFirst = %v", got)
	}
}
