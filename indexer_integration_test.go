package gextto

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestExplicitManagerOverridesHeuristic(t *testing.T) {
	// A Prowlarr instance on a non-standard port with an arbitrary name: the
	// explicit manager must win over the URL/port/name heuristic.
	prowlarr := IndexerConfig{Name: "custom", URL: "http://host:5001", Enabled: true, Manager: "prowlarr"}
	if got := torznab_endpoint(prowlarr); got != "http://host:5001/api/v1/search" {
		t.Fatalf("explicit prowlarr endpoint = %q", got)
	}
	jackett := IndexerConfig{Name: "custom", URL: "http://host:5001", Enabled: true, Manager: "jackett"}
	if got := torznab_endpoint(jackett); got != "http://host:5001/api/v2.0/indexers/all/results/torznab/api" {
		t.Fatalf("explicit jackett endpoint = %q", got)
	}
	// Without a manager, the historical heuristic still applies.
	legacy := IndexerConfig{Name: "my-indexer", URL: "http://host:9696", Enabled: true}
	if got := torznab_endpoint(legacy); got != "http://host:9696/api/v1/search" {
		t.Fatalf("heuristic endpoint = %q", got)
	}
}

func TestIndexerTestHandler(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<caps><server/></caps>`)
	}))
	defer good.Close()
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer broken.Close()

	run := func(body string) (int, map[string]any) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/indexer/test", strings.NewReader(body))
		IndexerTest(recorder, request, nil)
		var payload map[string]any
		_ = json.Unmarshal(recorder.Body.Bytes(), &payload)
		return recorder.Code, payload
	}

	code, payload := run(`{"name":"jackett","url":"` + good.URL + `","api_key":"k","enabled":true}`)
	if code != http.StatusOK || payload["ok"] != true {
		t.Fatalf("healthy indexer test: code=%d payload=%v", code, payload)
	}
	code, payload = run(`{"name":"jackett","url":"` + broken.URL + `","api_key":"k","enabled":true}`)
	if code != http.StatusOK || payload["ok"] != false {
		t.Fatalf("broken indexer test: code=%d payload=%v", code, payload)
	}
	if code, _ := run(`{"url":""}`); code != http.StatusBadRequest {
		t.Fatalf("missing url code = %d, want 400", code)
	}
}

func TestManagerSourceSkipsFlareSolverr(t *testing.T) {
	var flareCalls atomic.Int32
	flare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flareCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer flare.Close()

	manager := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer manager.Close()

	// A Jackett/Prowlarr entry: Cloudflare is handled inside the manager, so the
	// (local) endpoint must never be routed through FlareSolverr.
	indexer := IndexerConfig{Name: "prowlarr", URL: manager.URL, APIKey: "k", Enabled: true}
	if _, err := FetchTorznabFlareSolverr(context.Background(), indexer, "q", nil, &flare.URL); err == nil {
		t.Fatal("expected the manager error to be returned")
	}
	if calls := flareCalls.Load(); calls != 0 {
		t.Fatalf("FlareSolverr called %d times for a manager source", calls)
	}
}

func TestProwlarrIndexerHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/indexer":
			_, _ = io.WriteString(w, `[
			  {"id":12,"name":"Knaben","enable":false},
			  {"id":4,"name":"LimeTorrents","enable":true}
			]`)
		case "/api/v1/indexerstatus":
			_, _ = io.WriteString(w, `[{"indexerId":12,"disabledTill":"2026-09-29T10:19:39Z","mostRecentFailure":"2026-09-29T10:04:39Z"}]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	items, err := ProwlarrIndexerHealth(context.Background(), server.URL, "secret")
	if err != nil {
		t.Fatalf("ProwlarrIndexerHealth: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	byName := map[string]map[string]any{}
	for _, item := range items {
		byName[item["name"].(string)] = item
	}
	if item, ok := byName["Knaben"]; !ok || item["ok"] != false || !strings.Contains(item["error"].(string), "disabilitato") {
		t.Fatalf("Knaben health = %+v", byName["Knaben"])
	}
	if item, ok := byName["LimeTorrents"]; !ok || item["ok"] != true {
		t.Fatalf("LimeTorrents health = %+v", byName["LimeTorrents"])
	}
}

func TestSlowIndexerDoesNotBlockTheSearch(t *testing.T) {
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<rss><channel><item><title>Example.Show.S01E01.1080p.WEB-DL</title><link>magnet:?xt=urn:btih:0123456789012345678901234567890123456789</link></item></channel></rss>`)
	}))
	defer fast.Close()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1 * time.Second)
		_, _ = io.WriteString(w, `<rss><channel></channel></rss>`)
	}))
	defer slow.Close()

	old := indexerRequestTimeout
	indexerRequestTimeout = 200 * time.Millisecond
	defer func() { indexerRequestTimeout = old }()

	state := newTestAppState(t)
	cfg := DefaultConfig()
	cfg.DataDir = state.cfg.DataDir
	cfg.Indexers = []IndexerConfig{
		{Name: "fast", URL: fast.URL, Enabled: true},
		{Name: "slow", URL: slow.URL, Enabled: true},
	}
	cfg.WebsearchEngines = nil

	start := time.Now()
	releases := searchOneWithDB(context.Background(), &cfg, "Example Show S01E01", nil, nil, state.db, false)
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Fatalf("search took %s: the slow indexer was not bounded", elapsed)
	}
	if len(releases) == 0 {
		t.Fatal("results from the healthy indexer were lost")
	}
	// The timed-out source is recorded as failing (backoff), not mistaken for a
	// lifecycle cancellation.
	blocked, err := state.db.BlockedProviders()
	if err != nil {
		t.Fatalf("BlockedProviders: %v", err)
	}
	if _, ok := blocked[[2]string{"indexer", "slow"}]; !ok {
		t.Fatalf("slow indexer was not recorded as failing: %+v", blocked)
	}
}
