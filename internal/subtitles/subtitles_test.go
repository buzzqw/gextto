package subtitles

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestClientSearchAndDownload(t *testing.T) {
	var logins atomic.Int32
	var searches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			logins.Add(1)
			if r.Header.Get("Api-Key") != "key" || r.Header.Get("User-Agent") != "Gextto Test" {
				t.Errorf("login headers = %v", r.Header)
			}
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["username"] != "user" || body["password"] != "pass" {
				t.Errorf("login body = %v", body)
			}
			_, _ = io.WriteString(w, `{"token":"token-one"}`)
		case "/subtitles":
			searches.Add(1)
			if r.Header.Get("Authorization") != "Bearer token-one" {
				t.Errorf("authorization = %q", r.Header.Get("Authorization"))
			}
			query := r.URL.Query()
			if query.Get("languages") != "it" || query.Get("type") != "episode" || query.Get("season_number") != "1" || query.Get("episode_number") != "2" || query.Get("moviehash") != strings.Repeat("a", 40) {
				t.Errorf("query = %v", query)
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"10","attributes":{"language":"it","releases":["Example.S01E02"],"files":[{"file_id":20,"file_name":"subtitle.srt"}],"moviehash_match":true,"moviebytesize_match":true,"title":"Example","season":1,"episode":2}}]}`)
		case "/download":
			if r.Header.Get("Authorization") != "Bearer token-one" {
				t.Errorf("download authorization = %q", r.Header.Get("Authorization"))
			}
			var body map[string]int64
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["file_id"] != 20 {
				t.Errorf("file id = %v", body)
			}
			_, _ = io.WriteString(w, `{"link":"`+serverURL(r)+`/file.srt","file_name":"subtitle.srt"}`)
		case "/file.srt":
			_, _ = io.WriteString(w, "1\n00:00:00,000 --> 00:00:01,000\nCiao\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, APIKey: "key", Username: "user", Password: "pass", UserAgent: "Gextto Test", Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	results, err := client.Search(context.Background(), SearchQuery{Language: "it", Kind: "episode", Title: "Example", Season: 1, Episode: 2, MovieHash: strings.Repeat("a", 40)})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != 10 || !results[0].HashMatch {
		t.Fatalf("results = %+v", results)
	}
	file, ok := results[0].PrimaryFile()
	if !ok || file.ID != 20 {
		t.Fatalf("primary file = %+v, %v", file, ok)
	}
	download, err := client.Download(context.Background(), int64(file.ID), "srt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(download.Data), "Ciao") || download.FileName != "subtitle.srt" {
		t.Fatalf("download = %+v", download)
	}
	if logins.Load() != 1 || searches.Load() != 1 {
		t.Fatalf("logins=%d searches=%d", logins.Load(), searches.Load())
	}
}

func serverURL(r *http.Request) string {
	return "http://" + r.Host
}

func TestLoginUsesProviderBaseURL(t *testing.T) {
	var searches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			_, _ = io.WriteString(w, `{"token":"token","base_url":"`+serverURL(r)+`"}`)
			return
		}
		searches.Add(1)
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, APIKey: "key", Username: "user", Password: "pass", Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Search(context.Background(), SearchQuery{Title: "Example"}); err != nil {
		t.Fatal(err)
	}
	if searches.Load() != 1 {
		t.Fatalf("searches = %d", searches.Load())
	}
}

// The real OpenSubtitles login answer reports a bare hostname in "base_url".
// It must not replace the configured base: the API version path would be lost
// and the search would land on a different endpoint returning HTTP 503.
func TestLoginBareHostnameKeepsConfiguredBaseURL(t *testing.T) {
	var searches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/login":
			_, _ = io.WriteString(w, `{"token":"token","base_url":"`+strings.TrimPrefix(r.Host, "http://")+`"}`)
		case "/api/v1/subtitles":
			searches.Add(1)
			_, _ = io.WriteString(w, `{"data":[]}`)
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL + "/api/v1", APIKey: "key", Username: "user", Password: "pass", Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Search(context.Background(), SearchQuery{Title: "Example"}); err != nil {
		t.Fatal(err)
	}
	if searches.Load() != 1 {
		t.Fatalf("searches = %d, want the configured /api/v1 endpoint to be used", searches.Load())
	}
}

func TestSearchLogsInAgainAfterUnauthorized(t *testing.T) {
	var logins atomic.Int32
	var searches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			logins.Add(1)
			_, _ = io.WriteString(w, `{"token":"token"}`)
		case "/subtitles":
			if searches.Add(1) == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"data":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, APIKey: "key", Username: "user", Password: "pass", Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Search(context.Background(), SearchQuery{Title: "Example"}); err != nil {
		t.Fatal(err)
	}
	if logins.Load() != 2 || searches.Load() != 2 {
		t.Fatalf("logins=%d searches=%d, want one retry", logins.Load(), searches.Load())
	}
}

// OpenSubtitles sends feature_details.imdb_id as a number, compatible
// providers may send it as a string, and some send null. All three must decode.
func TestSearchAcceptsNumericIMDbID(t *testing.T) {
	for _, raw := range []string{`2188721`, `"tt2188721"`, `null`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"data":[{"id":"10","attributes":{"language":"en","feature_details":{"imdb_id":`+raw+`},"files":[{"file_id":20}]}}]}`)
		}))
		client, err := New(Config{BaseURL: server.URL, APIKey: "key", Client: server.Client()})
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		results, err := client.Search(context.Background(), SearchQuery{Title: "Example"})
		server.Close()
		if err != nil {
			t.Fatalf("imdb_id %s: %v", raw, err)
		}
		if len(results) != 1 {
			t.Fatalf("imdb_id %s: results = %+v", raw, results)
		}
	}
}

func TestHashFileUsesOSHashAndReadsOnlyTheEdges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "video.mkv")
	data := make([]byte, 1<<20)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	hash, gotSize, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if hash != "0000000000100000" || gotSize != int64(len(data)) {
		t.Fatalf("zero OSHash = %q size=%d", hash, gotSize)
	}
	// The middle is never read: changing it cannot affect OSHash.
	for i := 128 << 10; i < (1<<20)-(128<<10); i++ {
		data[i] = 0xff
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	again, _, err := HashFile(path)
	if err != nil || again != hash {
		t.Fatalf("middle changed OSHash: %q -> %q, err=%v", hash, again, err)
	}

	exact := filepath.Join(t.TempDir(), "exact.mkv")
	if err := os.WriteFile(exact, make([]byte, 128<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	if hash, _, err := HashFile(exact); err != nil || hash != "0000000000020000" {
		t.Fatalf("128 KiB OSHash = %q, err=%v", hash, err)
	}
	small := filepath.Join(t.TempDir(), "small.mkv")
	if err := os.WriteFile(small, make([]byte, (128<<10)-1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := HashFile(small); err == nil {
		t.Fatal("a file below 128 KiB must be rejected")
	}
}

func TestSearchRetriesTemporaryProviderFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, APIKey: "key", Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Search(context.Background(), SearchQuery{Title: "Example"}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want one retry", calls.Load())
	}
}

func TestAnonymousSearchUsesOnlyTheAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			t.Error("anonymous search must not log in")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if r.Header.Get("Api-Key") != "key" || r.Header.Get("Authorization") != "" {
			t.Errorf("anonymous headers = %v", r.Header)
		}
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, APIKey: "key", Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Search(context.Background(), SearchQuery{Title: "Example"}); err != nil {
		t.Fatal(err)
	}
}
