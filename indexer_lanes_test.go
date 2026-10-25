package gextto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeProwlarr serves an indexer list and per-indexer searches. LimeTorrents
// answers at once, TorrentLeech after slow; Knaben is disabled and TorrentsCSV
// failing, so neither may be searched.
type fakeProwlarr struct {
	slow       time.Duration
	searches   sync.Map // indexerIds -> *atomic.Int64
	inFlight   sync.Map // indexerIds -> *atomic.Int64
	maxFlight  sync.Map // indexerIds -> *atomic.Int64
	aggregates atomic.Int64
}

func (f *fakeProwlarr) counter(m *sync.Map, id string) *atomic.Int64 {
	value, _ := m.LoadOrStore(id, &atomic.Int64{})
	return value.(*atomic.Int64)
}

func (f *fakeProwlarr) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/indexer":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 4, "name": "LimeTorrents", "enable": true, "protocol": "torrent", "definitionName": "limetorrents"},
				{"id": 14, "name": "TorrentLeech", "enable": true, "protocol": "torrent", "definitionName": "torrentleech"},
				{"id": 12, "name": "Knaben", "enable": false, "protocol": "torrent", "definitionName": "Knaben"},
				{"id": 22, "name": "TorrentsCSV", "enable": true, "protocol": "torrent", "definitionName": "TorrentsCSV"},
			})
		case "/api/v1/indexerstatus":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"indexerId": 22, "disabledTill": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)},
			})
		case "/api/v1/search":
			id := r.URL.Query().Get("indexerIds")
			if id == "" {
				f.aggregates.Add(1)
			}
			f.counter(&f.searches, id).Add(1)
			now := f.counter(&f.inFlight, id).Add(1)
			defer f.counter(&f.inFlight, id).Add(-1)
			if peak := f.counter(&f.maxFlight, id); now > peak.Load() {
				peak.Store(now)
			}
			name := map[string]string{"4": "LimeTorrents", "14": "TorrentLeech"}[id]
			if id == "14" {
				select {
				case <-time.After(f.slow):
				case <-r.Context().Done():
					return
				}
			}
			query := r.URL.Query().Get("query")
			hash := fmt.Sprintf("%040x", len(id)*1000+len(query))
			if id == "14" {
				hash = fmt.Sprintf("%040x", 7777+len(query))
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"title":     query + " 1080p WEB-DL " + name,
				"magnetUrl": "magnet:?xt=urn:btih:" + hash,
				"indexer":   name,
				"protocol":  "torrent",
				"size":      2_000_000_000,
			}})
		default:
			http.NotFound(w, r)
		}
	})
}

// fakeJackett lists LimeTorrents (also in Prowlarr) and Knaben.
type fakeJackett struct {
	searched sync.Map // indexer id -> *atomic.Int64
}

func (f *fakeJackett) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/api/v2.0/indexers/all/results/torznab") && r.URL.Query().Get("t") == "indexers" {
			fmt.Fprint(w, `<?xml version="1.0"?><indexers><indexer id="limetorrents" configured="true"><title>LimeTorrents</title></indexer><indexer id="knaben" configured="true"><title>Knaben</title></indexer></indexers>`)
			return
		}
		parts := strings.Split(path, "/")
		if len(parts) > 4 && parts[3] == "indexers" {
			value, _ := f.searched.LoadOrStore(parts[4], &atomic.Int64{})
			value.(*atomic.Int64).Add(1)
			if r.URL.Query().Get("t") == "caps" {
				fmt.Fprint(w, `<caps><searching><search available="yes" supportedParams="q"/><tv-search available="yes" supportedParams="q,season,ep"/></searching></caps>`)
				return
			}
			fmt.Fprint(w, `<?xml version="1.0"?><rss><channel></channel></rss>`)
			return
		}
		http.NotFound(w, r)
	})
}

func resetLaneState() {
	laneListMu.Lock()
	laneListCache = map[string]laneListEntry{}
	laneListMu.Unlock()
	laneLatencyMu.Lock()
	laneLatency = map[string]float64{}
	laneLatencyMu.Unlock()
}

// TestManagerSearchedPerIndexer pins the lanes: Prowlarr is asked indexer by
// indexer (never the aggregate), the disabled and the failing indexers are
// skipped, a slow indexer that times out costs only its own results, and its
// failure carries its own name.
func TestManagerSearchedPerIndexer(t *testing.T) {
	resetLaneState()
	saved := managerRequestTimeout
	managerRequestTimeout = 300 * time.Millisecond
	t.Cleanup(func() { managerRequestTimeout = saved })
	fake := &fakeProwlarr{slow: 2 * time.Second}
	server := httptest.NewServer(fake.handler())
	t.Cleanup(server.Close)
	cfg := &Config{Indexers: []IndexerConfig{{Name: "prowlarr", URL: server.URL, APIKey: "k", Enabled: true, Manager: ManagerProwlarr}}}

	got := searchOneWithDBType(context.Background(), cfg, "Show S01E01", nil, nil, nil, true, searchTypeTV, false)
	if len(got) != 1 || !strings.Contains(got[0].Title, "LimeTorrents") {
		t.Fatalf("results = %+v, want the LimeTorrents one only (TorrentLeech timed out)", got)
	}
	if fake.aggregates.Load() != 0 {
		t.Fatalf("the aggregate was queried %d times", fake.aggregates.Load())
	}
	for _, id := range []string{"12", "22"} {
		if n := fake.counter(&fake.searches, id).Load(); n != 0 {
			t.Fatalf("indexer %s (disabled or failing) searched %d times", id, n)
		}
	}
	lanes := expandSearchLanes(context.Background(), cfg.Indexers)
	names := []string{}
	for _, lane := range lanes {
		names = append(names, lane.health)
	}
	if strings.Join(names, ",") != "prowlarr · LimeTorrents,prowlarr · TorrentLeech" {
		t.Fatalf("lanes = %v", names)
	}
}

// TestProwlarrLaneServesOneSearchAtATime pins the lane slots: concurrent title
// searches never send Prowlarr more than one search per indexer at a time.
func TestProwlarrLaneServesOneSearchAtATime(t *testing.T) {
	resetLaneState()
	fake := &fakeProwlarr{slow: 50 * time.Millisecond}
	server := httptest.NewServer(fake.handler())
	t.Cleanup(server.Close)
	cfg := &Config{Indexers: []IndexerConfig{{Name: "prowlarr", URL: server.URL, APIKey: "k", Enabled: true, Manager: ManagerProwlarr}}}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			searchOneWithDBType(context.Background(), cfg, fmt.Sprintf("Show %d S01E01", i), nil, nil, nil, true, searchTypeTV, false)
		}(i)
	}
	wg.Wait()
	if peak := fake.counter(&fake.maxFlight, "14").Load(); peak != 1 {
		t.Fatalf("TorrentLeech served %d searches at once, want 1", peak)
	}
	if n := fake.counter(&fake.searches, "14").Load(); n != 6 {
		t.Fatalf("TorrentLeech searched %d times, want 6", n)
	}
}

// TestTrackerInBothManagersSearchedOnce pins the dedup: LimeTorrents is in
// Jackett and in Prowlarr, and only one of the two lanes searches it.
func TestTrackerInBothManagersSearchedOnce(t *testing.T) {
	resetLaneState()
	prowlarr := &fakeProwlarr{}
	jackett := &fakeJackett{}
	ps := httptest.NewServer(prowlarr.handler())
	js := httptest.NewServer(jackett.handler())
	t.Cleanup(ps.Close)
	t.Cleanup(js.Close)
	cfg := &Config{Indexers: []IndexerConfig{
		{Name: "jackett", URL: js.URL, APIKey: "k", Enabled: true, Manager: "jackett"},
		{Name: "prowlarr", URL: ps.URL, APIKey: "k", Enabled: true, Manager: ManagerProwlarr},
	}}
	searchOneWithDBType(context.Background(), cfg, "Show S01E01", nil, nil, nil, true, searchTypeTV, false)
	jackettLime := int64(0)
	if value, ok := jackett.searched.Load("limetorrents"); ok {
		jackettLime = value.(*atomic.Int64).Load()
	}
	prowlarrLime := prowlarr.counter(&prowlarr.searches, "4").Load()
	if (jackettLime > 0) == (prowlarrLime > 0) {
		t.Fatalf("LimeTorrents searched by jackett=%d prowlarr=%d, want exactly one of them", jackettLime, prowlarrLime)
	}
	if value, ok := jackett.searched.Load("knaben"); !ok || value.(*atomic.Int64).Load() == 0 {
		t.Fatal("Knaben (only in Jackett) was not searched")
	}
}

// TestManagerWithoutListFallsBackToTheAggregate pins the fallback: a manager
// whose indexer list cannot be read is searched as before.
func TestManagerWithoutListFallsBackToTheAggregate(t *testing.T) {
	resetLaneState()
	var aggregate atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/search" && r.URL.Query().Get("indexerIds") == "" {
			aggregate.Add(1)
			fmt.Fprint(w, `[]`)
			return
		}
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	cfg := &Config{Indexers: []IndexerConfig{{Name: "prowlarr", URL: server.URL, APIKey: "k", Enabled: true, Manager: ManagerProwlarr}}}
	searchOneWithDBType(context.Background(), cfg, "Show S01E01", nil, nil, nil, true, searchTypeTV, false)
	if aggregate.Load() == 0 {
		t.Fatal("the aggregate was not queried after the list failed")
	}
}

// TestIndexarrSearchedPerIndexer pins indexarr's first-class lane behaviour:
// the indexer list and the per-indexer searches use the Jackett-compatible
// paths, while the aggregate /api endpoint is not touched.
func TestIndexarrSearchedPerIndexer(t *testing.T) {
	resetLaneState()
	var listCalls, laneCalls, aggregateCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v2.0/indexers/all/results/torznab"):
			listCalls.Add(1)
			fmt.Fprint(w, `<?xml version="1.0"?><indexers><indexer id="limetorrents" configured="true"><title>LimeTorrents</title></indexer><indexer id="1337x" configured="true"><title>1337x</title></indexer></indexers>`)
		case strings.HasPrefix(r.URL.Path, "/api/v2.0/indexers/"):
			parts := strings.Split(r.URL.Path, "/")
			id := ""
			if len(parts) > 4 {
				id = parts[4]
			}
			laneCalls.Add(1)
			if r.URL.Query().Get("t") == "caps" {
				fmt.Fprint(w, `<caps><searching><search available="yes" supportedParams="q"/><tv-search available="yes" supportedParams="q,season,ep"/></searching></caps>`)
				return
			}
			hash := fmt.Sprintf("%040x", len(id)*1000)
			fmt.Fprintf(w, `<?xml version="1.0"?><rss><channel><item><title>Show S01E01 1080p %s</title><link>magnet:?xt=urn:btih:%s</link></item></channel></rss>`, id, hash)
		case r.URL.Path == "/api":
			aggregateCalls.Add(1)
			fmt.Fprint(w, `<?xml version="1.0"?><rss><channel><item><title>SHOULD-NOT-BE-USED</title><link>magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa</link></item></channel></rss>`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	cfg := &Config{Indexers: []IndexerConfig{{Name: "indexarr", URL: server.URL, APIKey: "k", Enabled: true, Manager: ManagerIndexarr}}}
	got := searchOneWithDBType(context.Background(), cfg, "Show S01E01", nil, nil, nil, true, searchTypeTV, false)
	if listCalls.Load() == 0 || laneCalls.Load() == 0 {
		t.Fatalf("indexarr list=%d lanes=%d, want both used", listCalls.Load(), laneCalls.Load())
	}
	if aggregateCalls.Load() != 0 {
		t.Fatalf("the aggregate /api was queried %d times with working lanes", aggregateCalls.Load())
	}
	if len(got) == 0 {
		t.Fatal("no release from the indexarr lanes")
	}
	for _, release := range got {
		if strings.Contains(release.Title, "SHOULD-NOT-BE-USED") {
			t.Fatal("the aggregate result was used")
		}
	}
	names := []string{}
	for _, lane := range expandSearchLanes(context.Background(), cfg.Indexers) {
		names = append(names, lane.health)
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "indexarr · LimeTorrents") || !strings.Contains(joined, "indexarr · 1337x") {
		t.Fatalf("lanes = %v", names)
	}
}

// TestIndexarrAggregateUsesNativeEndpoint pins the fallback: when the indexer
// list cannot be read, the aggregate goes to indexarr's own /api endpoint, not
// to the Jackett all-indexers path.
func TestIndexarrAggregateUsesNativeEndpoint(t *testing.T) {
	resetLaneState()
	var aggregate, jackettPath atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" {
			aggregate.Add(1)
			fmt.Fprint(w, `<?xml version="1.0"?><rss><channel></channel></rss>`)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v2.0/indexers/all/results/torznab") {
			jackettPath.Add(1)
			http.Error(w, "nope", http.StatusUnauthorized)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	cfg := &Config{Indexers: []IndexerConfig{{Name: "indexarr", URL: server.URL, APIKey: "k", Enabled: true, Manager: ManagerIndexarr}}}
	searchOneWithDBType(context.Background(), cfg, "Show S01E01", nil, nil, nil, true, searchTypeTV, false)
	if aggregate.Load() == 0 {
		t.Fatal("the native /api aggregate was not queried after the list failed")
	}
	if jackettPath.Load() != 1 {
		t.Fatalf("the indexer list was queried %d times, want 1", jackettPath.Load())
	}
}

// TestLaneSkippedWhenItsQueueOutlastsTheSearch pins the budget check: with the
// lane's only slot taken and its searches averaging longer than what is left
// of the title's budget, the title skips it at once instead of waiting; with
// room in the budget it waits for the slot.
func TestLaneSkippedWhenItsQueueOutlastsTheSearch(t *testing.T) {
	resetLaneState()
	lane := searchLane{key: "test-busy-lane", slots: 1}
	noteLaneLatency(lane.key, 10*time.Second)
	release, err := acquireLane(context.Background(), lane)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	if _, err := acquireLane(ctx, lane); !errors.Is(err, errLaneBusy) {
		t.Fatalf("acquire = %v, want errLaneBusy", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("the busy lane was waited for")
	}
	long, cancelLong := context.WithTimeout(context.Background(), time.Minute)
	defer cancelLong()
	go func() {
		time.Sleep(50 * time.Millisecond)
		release()
	}()
	got, err := acquireLane(long, lane)
	if err != nil {
		t.Fatalf("acquire with budget = %v", err)
	}
	got()
}
