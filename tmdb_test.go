package gextto

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

// tmdbTestServer points tmdbAPIBaseURL at a hermetic httptest server for the
// duration of the test.
func tmdbTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	previous := tmdbAPIBaseURL
	tmdbAPIBaseURL = server.URL
	t.Cleanup(func() {
		tmdbAPIBaseURL = previous
		server.Close()
	})
	return server
}

func tmdbWriteJSON(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(body)); err != nil {
		t.Errorf("write tmdb response: %v", err)
	}
}

// assertJSONEqual compares any Go value against a JSON literal after a
// round-trip through encoding/json, so numeric flavours and key order do not
// matter.
func assertJSONEqual(t *testing.T, got any, wantJSON string) {
	t.Helper()
	gotBytes, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal got: %v", err)
	}
	var gotValue, wantValue any
	if err := json.Unmarshal(gotBytes, &gotValue); err != nil {
		t.Fatalf("unmarshal got: %v", err)
	}
	if err := json.Unmarshal([]byte(wantJSON), &wantValue); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("JSON mismatch:\n got: %s\nwant: %s", gotBytes, wantJSON)
	}
}

func tmdbTestKey() *string {
	key := "test-key"
	return &key
}

func TestTmdbSearchSeriesParsesFieldsAndPosterURL(t *testing.T) {
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if r.URL.Path != "/search/tv" {
			t.Errorf("path = %q, want /search/tv", r.URL.Path)
		}
		query := r.URL.Query()
		if got := query.Get("query"); got != "Il Trono di Spade" {
			t.Errorf("query = %q", got)
		}
		if got := query.Get("language"); got != "it-IT" {
			t.Errorf("language = %q, want it-IT", got)
		}
		if got := query.Get("api_key"); got != "test-key" {
			t.Errorf("api_key = %q", got)
		}
		tmdbWriteJSON(t, w, `{"results":[{
			"id":1399,
			"name":"Il Trono di Spade",
			"title":null,
			"overview":"Una storia epica.",
			"poster_path":"/abc.jpg",
			"first_air_date":"2011-04-17",
			"release_date":null,
			"vote_average":8.4
		}]}`)
	})

	items, err := NewTmdbClient(tmdbTestKey()).SearchSeries(context.Background(), "Il Trono di Spade")
	if err != nil {
		t.Fatalf("SearchSeries: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	item := items[0]
	if item.ID != 1399 {
		t.Errorf("ID = %d, want 1399", item.ID)
	}
	if item.Name == nil || *item.Name != "Il Trono di Spade" {
		t.Errorf("Name = %v", item.Name)
	}
	if item.Title != nil {
		t.Errorf("Title = %v, want nil", *item.Title)
	}
	if item.Overview == nil || *item.Overview != "Una storia epica." {
		t.Errorf("Overview = %v", item.Overview)
	}
	if item.PosterPath == nil || *item.PosterPath != "/abc.jpg" {
		t.Errorf("PosterPath = %v", item.PosterPath)
	}
	if item.FirstAirDate == nil || *item.FirstAirDate != "2011-04-17" {
		t.Errorf("FirstAirDate = %v", item.FirstAirDate)
	}
	if item.ReleaseDate != nil {
		t.Errorf("ReleaseDate = %v, want nil", *item.ReleaseDate)
	}
	if item.VoteAverage == nil || *item.VoteAverage != 8.4 {
		t.Errorf("VoteAverage = %v", item.VoteAverage)
	}

	// The web layer builds poster URLs as "<base>/w154<path>" (the web module line).
	posterURL := TmdbImageBaseURL + "/w154" + *item.PosterPath
	if posterURL != "https://image.tmdb.org/t/p/w154/abc.jpg" {
		t.Errorf("poster URL = %q", posterURL)
	}
}

func TestTmdbSearchMoviesUsesMovieEndpoint(t *testing.T) {
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/movie" {
			t.Errorf("path = %q, want /search/movie", r.URL.Path)
		}
		tmdbWriteJSON(t, w, `{"results":[{"id":603,"title":"Matrix","release_date":"1999-03-31","vote_average":8.2}]}`)
	})

	items, err := NewTmdbClient(tmdbTestKey()).SearchMovies(context.Background(), "Matrix")
	if err != nil {
		t.Fatalf("SearchMovies: %v", err)
	}
	if len(items) != 1 || items[0].ID != 603 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Title == nil || *items[0].Title != "Matrix" {
		t.Fatalf("Title = %v", items[0].Title)
	}
	if items[0].ReleaseDate == nil || *items[0].ReleaseDate != "1999-03-31" {
		t.Fatalf("ReleaseDate = %v", items[0].ReleaseDate)
	}
}

func TestTmdbLanguageDefaultAndStoredOriginal(t *testing.T) {
	cases := []struct {
		name         string
		constructor  func(key *string) *TmdbClient
		wantLanguage string
	}{
		{"constructor defaults to it-IT", NewTmdbClient, "it-IT"},
		{"blank falls back to it-IT", func(key *string) *TmdbClient {
			return NewTmdbClientWithLanguage(key, "")
		}, "it-IT"},
		{"whitespace falls back to it-IT", func(key *string) *TmdbClient {
			return NewTmdbClientWithLanguage(key, "   ")
		}, "it-IT"},
		{"explicit language is stored untrimmed", func(key *string) *TmdbClient {
			return NewTmdbClientWithLanguage(key, " en-US ")
		}, " en-US "},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				if got := r.URL.Query().Get("language"); got != testCase.wantLanguage {
					t.Errorf("request language = %q, want %q", got, testCase.wantLanguage)
				}
				tmdbWriteJSON(t, w, `{"results":[]}`)
			})
			client := testCase.constructor(tmdbTestKey())
			if client.language != testCase.wantLanguage {
				t.Errorf("client.language = %q, want %q", client.language, testCase.wantLanguage)
			}
			if _, err := client.SearchSeries(context.Background(), "x"); err != nil {
				t.Fatalf("SearchSeries: %v", err)
			}
		})
	}
}

func TestTmdbNilKeySkipsRequests(t *testing.T) {
	var hits atomic.Int32
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		tmdbWriteJSON(t, w, `{"results":[{"id":1}]}`)
	})
	client := NewTmdbClient(nil)
	if items, err := client.SearchSeries(context.Background(), "x"); err != nil || len(items) != 0 {
		t.Fatalf("SearchSeries = %v, %v", items, err)
	}
	if items, err := client.SearchMovies(context.Background(), "x"); err != nil || len(items) != 0 {
		t.Fatalf("SearchMovies = %v, %v", items, err)
	}
	if items, err := client.Trending(context.Background(), "tv", "week"); err != nil || len(items) != 0 {
		t.Fatalf("Trending = %v, %v", items, err)
	}
	if items, err := client.Popular(context.Background(), "tv"); err != nil || len(items) != 0 {
		t.Fatalf("Popular = %v, %v", items, err)
	}
	if item, err := client.SearchMovie(context.Background(), "x", nil); err != nil || item != nil {
		t.Fatalf("SearchMovie = %v, %v", item, err)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("HTTP hits = %d, want 0", got)
	}
}

func TestTmdbMissingKeyErrorFromGetJSON(t *testing.T) {
	client := NewTmdbClient(nil)
	var target map[string]any
	err := client.getJSON(context.Background(), tmdbAPIBaseURL+"/x", nil, &target)
	if err == nil || err.Error() != "TMDB API key is not configured" {
		t.Fatalf("error = %v", err)
	}
}

func TestTmdbMovieDetailsParsesFields(t *testing.T) {
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/movie/603" {
			t.Errorf("path = %q, want /movie/603", r.URL.Path)
		}
		if got := r.URL.Query().Get("language"); got != "it-IT" {
			t.Errorf("language = %q", got)
		}
		tmdbWriteJSON(t, w, `{
			"id":603,
			"title":"Matrix",
			"original_title":"The Matrix",
			"overview":"Un hacker.",
			"poster_path":"/matrix.jpg",
			"release_date":"1999-03-31"
		}`)
	})

	details, err := NewTmdbClient(tmdbTestKey()).MovieDetails(context.Background(), " 603 ")
	if err != nil {
		t.Fatalf("MovieDetails: %v", err)
	}
	if details.ID != 603 {
		t.Errorf("ID = %d", details.ID)
	}
	if details.Title == nil || *details.Title != "Matrix" {
		t.Errorf("Title = %v", details.Title)
	}
	if details.OriginalTitle == nil || *details.OriginalTitle != "The Matrix" {
		t.Errorf("OriginalTitle = %v", details.OriginalTitle)
	}
	if details.PosterPath == nil || *details.PosterPath != "/matrix.jpg" {
		t.Errorf("PosterPath = %v", details.PosterPath)
	}
	if details.ReleaseDate == nil || *details.ReleaseDate != "1999-03-31" {
		t.Errorf("ReleaseDate = %v", details.ReleaseDate)
	}
}

func TestTmdbMovieDetailsInvalidID(t *testing.T) {
	_, err := NewTmdbClient(tmdbTestKey()).MovieDetails(context.Background(), "not-a-number")
	if err == nil || err.Error() != "invalid TMDB movie id" {
		t.Fatalf("error = %v", err)
	}
}

func TestTmdbMovieCreditsSortsAndTruncates(t *testing.T) {
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/movie/603/credits" {
			t.Errorf("path = %q", r.URL.Path)
		}
		tmdbWriteJSON(t, w, `{"cast":[
			{"name":"A5","character":"c","order":5},
			{"name":"A1","character":"c","order":1},
			{"name":"A3","character":"c","order":3},
			{"name":"A-nil","character":"c","order":null},
			{"name":"A2","character":"c","order":2},
			{"name":"A4","character":"c","order":4},
			{"name":"A6","character":"c","order":6},
			{"name":"A7","character":"c","order":7},
			{"name":"A8","character":"c","order":8},
			{"name":"A9","character":"c","order":9},
			{"name":"A10","character":"c","order":10},
			{"name":"A11","character":"c","order":11},
			{"name":"A12","character":"c","order":12},
			{"name":"A13","character":"c","order":13}
		]}`)
	})

	cast, err := NewTmdbClient(tmdbTestKey()).MovieCredits(context.Background(), "603")
	if err != nil {
		t.Fatalf("MovieCredits: %v", err)
	}
	if len(cast) != 12 {
		t.Fatalf("len(cast) = %d, want 12", len(cast))
	}
	for index := 0; index < len(cast); index++ {
		if cast[index].Order == nil {
			t.Fatalf("cast[%d].Order is nil", index)
		}
		want := int64(index + 1)
		if *cast[index].Order != want {
			t.Fatalf("cast[%d].Order = %d, want %d", index, *cast[index].Order, want)
		}
	}
	if cast[0].Name == nil || *cast[0].Name != "A1" {
		t.Fatalf("cast[0].Name = %v", cast[0].Name)
	}
}

func TestTmdbMovieCreditsInvalidIDIsEmpty(t *testing.T) {
	var hits atomic.Int32
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	})
	cast, err := NewTmdbClient(tmdbTestKey()).MovieCredits(context.Background(), "abc")
	if err != nil {
		t.Fatalf("MovieCredits: %v", err)
	}
	if cast == nil || len(cast) != 0 {
		t.Fatalf("cast = %#v, want empty non-nil", cast)
	}
	if hits.Load() != 0 {
		t.Fatalf("HTTP hits = %d, want 0", hits.Load())
	}
}

func TestTmdbSeasonCountsEpisodesAndNextEpisode(t *testing.T) {
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tv/1399":
			tmdbWriteJSON(t, w, `{
				"seasons":[
					{"season_number":0,"episode_count":0},
					{"season_number":1,"episode_count":10},
					{"season_number":2,"episode_count":null},
					{"season_number":null,"episode_count":10},
					{"season_number":8,"episode_count":6}
				],
				"next_episode_to_air":{
					"id":999,
					"name":"Prossimo",
					"season_number":8,
					"episode_number":7,
					"air_date":"2026-05-01"
				}
			}`)
		case "/tv/1399/season/1":
			tmdbWriteJSON(t, w, `{"episodes":[
				{"id":1,"name":"Ep1","season_number":1,"episode_number":1,"air_date":"2011-04-17"},
				{"id":2,"name":"Ep2","season_number":1,"episode_number":2,"air_date":"2011-04-24"}
			]}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	client := NewTmdbClient(tmdbTestKey())
	counts, err := client.SeasonCounts(context.Background(), "1399")
	if err != nil {
		t.Fatalf("SeasonCounts: %v", err)
	}
	wantCounts := map[int64]int64{0: 0, 1: 10, 8: 6}
	if !reflect.DeepEqual(counts, wantCounts) {
		t.Fatalf("counts = %v, want %v", counts, wantCounts)
	}

	next, err := client.NextEpisode(context.Background(), "1399")
	if err != nil {
		t.Fatalf("NextEpisode: %v", err)
	}
	if next == nil || next.ID == nil || *next.ID != 999 {
		t.Fatalf("next = %+v", next)
	}
	if next.Name == nil || *next.Name != "Prossimo" {
		t.Fatalf("next.Name = %v", next.Name)
	}
	if next.SeasonNumber == nil || *next.SeasonNumber != 8 || next.EpisodeNumber == nil || *next.EpisodeNumber != 7 {
		t.Fatalf("next numbers = %v/%v", next.SeasonNumber, next.EpisodeNumber)
	}
	if next.AirDate == nil || *next.AirDate != "2026-05-01" {
		t.Fatalf("next.AirDate = %v", next.AirDate)
	}

	episodes, err := client.SeasonEpisodes(context.Background(), "1399", 1)
	if err != nil {
		t.Fatalf("SeasonEpisodes: %v", err)
	}
	if len(episodes) != 2 || episodes[1].EpisodeNumber == nil || *episodes[1].EpisodeNumber != 2 {
		t.Fatalf("episodes = %+v", episodes)
	}
}

func TestTmdbSeasonEpisodesRejectsNonPositiveSeason(t *testing.T) {
	var hits atomic.Int32
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	})
	episodes, err := NewTmdbClient(tmdbTestKey()).SeasonEpisodes(context.Background(), "1399", 0)
	if err != nil {
		t.Fatalf("SeasonEpisodes: %v", err)
	}
	if len(episodes) != 0 {
		t.Fatalf("episodes = %+v", episodes)
	}
	if hits.Load() != 0 {
		t.Fatalf("HTTP hits = %d, want 0", hits.Load())
	}
}

func TestTmdbNextEpisodeInvalidIDReturnsNil(t *testing.T) {
	next, err := NewTmdbClient(tmdbTestKey()).NextEpisode(context.Background(), "abc")
	if err != nil || next != nil {
		t.Fatalf("NextEpisode = %v, %v", next, err)
	}
}

func TestTmdbGetJSONRetriesOn429ThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		tmdbWriteJSON(t, w, `{"results":[{"id":42,"name":"Ritentato"}]}`)
	})

	items, err := NewTmdbClient(tmdbTestKey()).SearchSeries(context.Background(), "x")
	if err != nil {
		t.Fatalf("SearchSeries: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
	if len(items) != 1 || items[0].ID != 42 {
		t.Fatalf("items = %+v", items)
	}
}

func TestTmdbGetJSONGivesUpAfterThreeServerErrors(t *testing.T) {
	var calls atomic.Int32
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	_, err := NewTmdbClient(tmdbTestKey()).SearchSeries(context.Background(), "x")
	if err == nil || err.Error() != "TMDB request failed: HTTP 503" {
		t.Fatalf("error = %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestTmdbNonRetryableHTTPError(t *testing.T) {
	var calls atomic.Int32
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	})
	_, err := NewTmdbClient(tmdbTestKey()).SearchSeries(context.Background(), "x")
	if err == nil || err.Error() != "TMDB request failed: HTTP 404" {
		t.Fatalf("error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1 (404 is not retryable)", calls.Load())
	}
}

func TestTmdbSearchMovieYearAndEmptyResults(t *testing.T) {
	var seenYear atomic.Bool
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("year") == "1999" {
			seenYear.Store(true)
		}
		tmdbWriteJSON(t, w, `{"results":[]}`)
	})

	year := int64(1999)
	item, err := NewTmdbClient(tmdbTestKey()).SearchMovie(context.Background(), "x", &year)
	if err != nil {
		t.Fatalf("SearchMovie: %v", err)
	}
	if item != nil {
		t.Fatalf("item = %+v, want nil", item)
	}
	if !seenYear.Load() {
		t.Fatal("year query parameter was not sent")
	}
}

func TestTmdbTrendingPopularAndCategoryPaths(t *testing.T) {
	var paths []string
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		tmdbWriteJSON(t, w, `{"results":[]}`)
	})

	client := NewTmdbClient(tmdbTestKey())
	ctx := context.Background()
	if _, err := client.Trending(ctx, "movie", "day"); err != nil {
		t.Fatalf("Trending movie/day: %v", err)
	}
	if _, err := client.Trending(ctx, "tv", "week"); err != nil {
		t.Fatalf("Trending tv/week: %v", err)
	}
	if _, err := client.Popular(ctx, "movie"); err != nil {
		t.Fatalf("Popular movie: %v", err)
	}
	if _, err := client.Popular(ctx, "series"); err != nil {
		t.Fatalf("Popular series: %v", err)
	}
	if _, err := client.Category(ctx, "movie", "top_rated"); err != nil {
		t.Fatalf("Category movie/top_rated: %v", err)
	}
	if _, err := client.Category(ctx, "series", "now_playing"); err != nil {
		t.Fatalf("Category series/now_playing: %v", err)
	}
	if _, err := client.Category(ctx, "series", "upcoming"); err != nil {
		t.Fatalf("Category series/upcoming: %v", err)
	}

	want := []string{
		"/trending/movie/day",
		"/trending/tv/week",
		"/movie/popular",
		"/tv/popular",
		"/movie/top_rated",
		"/tv/on_the_air",
		"/tv/airing_today",
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}

	if _, err := client.Category(ctx, "movie", "bogus"); err == nil || err.Error() != "unsupported TMDB category: bogus" {
		t.Fatalf("error = %v", err)
	}
}

func TestTmdbCategoryPath(t *testing.T) {
	cases := []struct {
		kind, category, want string
		ok                   bool
	}{
		{"movie", "top_rated", "movie/top_rated", true},
		{"series", "top_rated", "tv/top_rated", true},
		{"MOVIE", "top_rated", "movie/top_rated", true},
		{"movie", "now_playing", "movie/now_playing", true},
		{"series", "now_playing", "tv/on_the_air", true},
		{"movie", "upcoming", "movie/upcoming", true},
		{"series", "upcoming", "tv/airing_today", true},
		{"movie", "unknown", "", false},
	}
	for _, testCase := range cases {
		got, ok := categoryPath(testCase.kind, testCase.category)
		if ok != testCase.ok || got != testCase.want {
			t.Errorf("categoryPath(%q, %q) = %q, %v; want %q, %v",
				testCase.kind, testCase.category, got, ok, testCase.want, testCase.ok)
		}
	}
}

func TestTmdbResolveSeriesIDCachesResult(t *testing.T) {
	var hits atomic.Int32
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		tmdbWriteJSON(t, w, `{"results":[{"id":1399,"name":"x","poster_path":"/p.jpg"}]}`)
	})

	client := NewTmdbClient(tmdbTestKey())
	ctx := context.Background()
	first, err := client.ResolveSeriesID(ctx, "x")
	if err != nil || first == nil || *first != "1399" {
		t.Fatalf("ResolveSeriesID = %v, %v", first, err)
	}
	// A clone shares the cache (Arc<Mutex<...>> semantics).
	second, err := client.Clone().ResolveSeriesID(ctx, "x")
	if err != nil || second == nil || *second != "1399" {
		t.Fatalf("cloned ResolveSeriesID = %v, %v", second, err)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", hits.Load())
	}
}

func TestTmdbResolveSeriesIDCachesMiss(t *testing.T) {
	var hits atomic.Int32
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		tmdbWriteJSON(t, w, `{"results":[]}`)
	})
	client := NewTmdbClient(tmdbTestKey())
	ctx := context.Background()
	if id, err := client.ResolveSeriesID(ctx, "missing"); err != nil || id != nil {
		t.Fatalf("ResolveSeriesID = %v, %v", id, err)
	}
	if id, err := client.ResolveSeriesID(ctx, "missing"); err != nil || id != nil {
		t.Fatalf("cached ResolveSeriesID = %v, %v", id, err)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1 (a cached None is remembered)", hits.Load())
	}
}

func TestTmdbPosterForSeriesCaches(t *testing.T) {
	var hits atomic.Int32
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		tmdbWriteJSON(t, w, `{"results":[{"id":1,"poster_path":"/poster.jpg"}]}`)
	})
	client := NewTmdbClient(tmdbTestKey())
	ctx := context.Background()
	poster, err := client.PosterForSeries(ctx, "x")
	if err != nil || poster == nil || *poster != "/poster.jpg" {
		t.Fatalf("PosterForSeries = %v, %v", poster, err)
	}
	if _, err := client.PosterForSeries(ctx, "x"); err != nil {
		t.Fatalf("cached PosterForSeries: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", hits.Load())
	}
}

func TestTmdbSeriesInfoUsesNumericIDOrResolves(t *testing.T) {
	t.Run("numeric id skips search", func(t *testing.T) {
		var searched atomic.Bool
		tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/search/tv" {
				searched.Store(true)
			}
			if r.URL.Path != "/tv/1399" {
				t.Errorf("path = %q", r.URL.Path)
			}
			tmdbWriteJSON(t, w, `{"id":1399,"name":"Serie"}`)
		})
		got, err := NewTmdbClient(tmdbTestKey()).SeriesInfo(context.Background(), "ignored", ptr(" 1399 "))
		if err != nil {
			t.Fatalf("SeriesInfo: %v", err)
		}
		assertJSONEqual(t, got, `{"id":1399,"name":"Serie"}`)
		if searched.Load() {
			t.Fatal("numeric TMDB id must not trigger a search")
		}
	})

	t.Run("non numeric id resolves by name", func(t *testing.T) {
		tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/search/tv":
				tmdbWriteJSON(t, w, `{"results":[{"id":1399,"name":"Serie"}]}`)
			case "/tv/1399":
				tmdbWriteJSON(t, w, `{"id":1399,"name":"Serie"}`)
			default:
				t.Errorf("unexpected path %q", r.URL.Path)
			}
		})
		got, err := NewTmdbClient(tmdbTestKey()).SeriesInfo(context.Background(), "Serie", ptr("tvdb-77"))
		if err != nil {
			t.Fatalf("SeriesInfo: %v", err)
		}
		assertJSONEqual(t, got, `{"id":1399,"name":"Serie"}`)
	})

	t.Run("unresolved name returns nil", func(t *testing.T) {
		tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/search/tv" {
				t.Errorf("path = %q", r.URL.Path)
			}
			tmdbWriteJSON(t, w, `{"results":[]}`)
		})
		got, err := NewTmdbClient(tmdbTestKey()).SeriesInfo(context.Background(), "missing", nil)
		if err != nil || got != nil {
			t.Fatalf("SeriesInfo = %v, %v", got, err)
		}
	})
}

func TestTmdbSeriesCastSortsAndCapsAtEight(t *testing.T) {
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tv/5/aggregate_credits" {
			t.Errorf("path = %q", r.URL.Path)
		}
		tmdbWriteJSON(t, w, `{"cast":[
			{"id":10,"name":"N10","order":10},
			{"id":1,"name":"N1","order":1},
			{"id":2,"name":"N2","order":2},
			{"id":3,"name":null,"order":3},
			{"id":4,"name":"N4","order":4},
			{"id":5,"name":"N5","order":5},
			{"id":6,"name":"N6","order":6},
			{"id":7,"name":"N7","order":7},
			{"id":8,"name":"N8","order":8},
			{"id":9,"name":"N9","order":9}
		]}`)
	})
	cast, err := NewTmdbClient(tmdbTestKey()).SeriesCast(context.Background(), "5")
	if err != nil {
		t.Fatalf("SeriesCast: %v", err)
	}
	if len(cast) != 8 {
		t.Fatalf("len(cast) = %d, want 8", len(cast))
	}
	wantIDs := []int64{1, 2, 4, 5, 6, 7, 8, 9}
	for index, entry := range cast {
		if entry.ID != wantIDs[index] {
			t.Fatalf("cast[%d].ID = %d, want %d", index, entry.ID, wantIDs[index])
		}
	}
	if cast[0].Name != "N1" {
		t.Fatalf("cast[0].Name = %q", cast[0].Name)
	}
}

func TestTmdbEpisodeTitleCachesAndFiltersBlank(t *testing.T) {
	var hits atomic.Int32
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/tv/7/season/2/episode/3" {
			t.Errorf("path = %q", r.URL.Path)
		}
		tmdbWriteJSON(t, w, `{"name":"Titolo"}`)
	})
	client := NewTmdbClient(tmdbTestKey())
	ctx := context.Background()
	title, err := client.EpisodeTitle(ctx, "7", 2, 3)
	if err != nil || title == nil || *title != "Titolo" {
		t.Fatalf("EpisodeTitle = %v, %v", title, err)
	}
	if _, err := client.EpisodeTitle(ctx, "7", 2, 3); err != nil {
		t.Fatalf("cached EpisodeTitle: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", hits.Load())
	}

	// A blank name is cached as nil and never returned.
	var blankHits atomic.Int32
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		blankHits.Add(1)
		tmdbWriteJSON(t, w, `{"name":"   "}`)
	})
	blankClient := NewTmdbClient(tmdbTestKey())
	if title, err := blankClient.EpisodeTitle(ctx, "7", 2, 3); err != nil || title != nil {
		t.Fatalf("blank EpisodeTitle = %v, %v", title, err)
	}
	if title, err := blankClient.EpisodeTitle(ctx, "7", 2, 3); err != nil || title != nil {
		t.Fatalf("cached blank EpisodeTitle = %v, %v", title, err)
	}
	if blankHits.Load() != 1 {
		t.Fatalf("blank hits = %d, want 1", blankHits.Load())
	}
}

func TestTmdbEpisodeTitleInvalidIDReturnsNil(t *testing.T) {
	title, err := NewTmdbClient(tmdbTestKey()).EpisodeTitle(context.Background(), "abc", 1, 1)
	if err != nil || title != nil {
		t.Fatalf("EpisodeTitle = %v, %v", title, err)
	}
}

func TestTmdbCloneNil(t *testing.T) {
	var client *TmdbClient
	if client.Clone() != nil {
		t.Fatal("Clone of nil should be nil")
	}
}

func TestTmdbOrderOrMax(t *testing.T) {
	var nilOrder *int64
	if got := tmdbOrderOrMax(nilOrder); got != 1<<63-1 {
		t.Fatalf("nil order = %d", got)
	}
	value := int64(7)
	if got := tmdbOrderOrMax(&value); got != 7 {
		t.Fatalf("order = %d", got)
	}
}

func ptr[T any](value T) *T {
	return &value
}
