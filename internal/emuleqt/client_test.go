package emuleqt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeCore answers like emulecored: an API key on every call but /health,
// strict bodies, paged lists and a search that finishes after a few reads.
type fakeCore struct {
	mu        sync.Mutex
	downloads []Download
	calls     []string
	bodies    map[string]map[string]any
	reads     int
	deleted   []string
}

func (f *fakeCore) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		if r.URL.Path != "/api/v1/health" && r.Header.Get("X-Api-Key") != "key" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"code":401,"message":"bad key"}}`)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("%s %s: body is not JSON: %q", r.Method, r.URL.Path, raw)
			}
		}
		if f.bodies == nil {
			f.bodies = map[string]map[string]any{}
		}
		f.bodies[r.Method+" "+r.URL.Path] = body
		reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		switch {
		case r.URL.Path == "/api/v1/health":
			reply(map[string]any{"status": "ok"})
		case r.URL.Path == "/api/v1/snapshot":
			reply(map[string]any{"version": "0.7.35", "ed2k": map[string]any{"connected": true, "lowID": true, "server": "1.2.3.4:4661"},
				"kad": map[string]any{"running": true, "connected": true}, "rates": map[string]any{"downKiBps": 12.5, "upKiBps": 3}})
		case r.URL.Path == "/api/v1/downloads" && r.Method == http.MethodGet:
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			if limit <= 0 || limit > 1000 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			// Serve two rows per page whatever was asked, to exercise paging.
			end := min(offset+2, len(f.downloads))
			reply(map[string]any{"items": f.downloads[min(offset, end):end], "total": len(f.downloads), "offset": offset, "limit": 2})
		case r.URL.Path == "/api/v1/downloads" && r.Method == http.MethodPost:
			if body["link"] == nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			reply(map[string]any{"added": 1})
		case strings.HasSuffix(r.URL.Path, "/pause"), strings.HasSuffix(r.URL.Path, "/resume"), r.URL.Path == "/api/v1/kad/start":
			if len(raw) > 0 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":{"code":400,"message":"unknown body"}}`)
				return
			}
			reply(map[string]any{})
		case strings.HasSuffix(r.URL.Path, "/cancel"):
			if body["confirm"] != true {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":{"code":400,"message":"confirm must be true"}}`)
				return
			}
			reply(map[string]any{})
		case r.URL.Path == "/api/v1/search" && r.Method == http.MethodPost:
			if body["expression"] == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			reply(map[string]any{"searchID": 7, "type": body["type"]})
		case r.URL.Path == "/api/v1/search/7/results":
			f.reads++
			state := "running"
			if f.reads >= 3 {
				state = "finished"
			}
			reply(map[string]any{"state": state, "items": []map[string]any{
				{"hash": "00112233445566778899aabbccddeeff", "fileName": "Show.S01E02.mkv", "fileSize": 1 << 30, "sourceCount": 9},
				{"hash": "ffeeddccbbaa99887766554433221100", "fileName": "Show.S01E02 torrent", "metaKind": "torrent"},
			}})
		case r.URL.Path == "/api/v1/search/7" && r.Method == http.MethodDelete:
			f.deleted = append(f.deleted, "7")
			reply(map[string]any{})
		case r.URL.Path == "/api/v1/search/8/results":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"code":404,"message":"Search not found"}}`)
		case r.URL.Path == "/api/v1/preferences" && r.Method == http.MethodPatch:
			reply(body)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, fmt.Sprintf(`{"error":{"code":404,"message":"no route %s"}}`, r.URL.Path))
		}
	})
}

func startFake(t *testing.T) (*fakeCore, *Client) {
	t.Helper()
	core := &fakeCore{}
	server := httptest.NewServer(core.handler(t))
	t.Cleanup(server.Close)
	return core, New(Config{BaseURL: server.URL + "/", APIKey: "key"})
}

func TestHealthNeedsNoKeyAndTheRestDoes(t *testing.T) {
	core, client := startFake(t)
	anonymous := New(Config{BaseURL: client.base})
	if err := anonymous.Health(context.Background()); err != nil {
		t.Fatalf("health: %v", err)
	}
	if _, err := anonymous.Snapshot(context.Background()); !errors.Is(err, ErrAuth) {
		t.Fatalf("a call without key = %v, want ErrAuth", err)
	}
	snap, err := client.Snapshot(context.Background())
	if err != nil || !snap.Ed2k.Connected || !snap.Ed2k.LowID || !snap.Kad.Connected || snap.Rates.DownKiBps != 12.5 || snap.Version != "0.7.35" {
		t.Fatalf("snapshot = %+v, %v", snap, err)
	}
	_ = core
}

func TestDownloadsReadsEveryPage(t *testing.T) {
	core, client := startFake(t)
	for i := range 5 {
		core.downloads = append(core.downloads, Download{Hash: fmt.Sprintf("%032d", i), FileName: fmt.Sprintf("f%d.mkv", i), Status: "ready"})
	}
	got, err := client.Downloads(context.Background())
	if err != nil || len(got) != 5 || got[4].FileName != "f4.mkv" {
		t.Fatalf("downloads = %+v, %v", got, err)
	}
}

func TestActionsSendTheBodyTheCoreExpects(t *testing.T) {
	core, client := startFake(t)
	ctx := context.Background()
	hash := "00112233445566778899AABBCCDDEEFF"
	for name, call := range map[string]func() error{
		"pause":  func() error { return client.Pause(ctx, hash) },
		"resume": func() error { return client.Resume(ctx, hash) },
		"cancel": func() error { return client.Cancel(ctx, hash) },
		"kad":    func() error { return client.StartKad(ctx) },
		"add":    func() error { return client.AddLink(ctx, "ed2k://|file|a.mkv|1|"+hash+"|/", 0) },
		"limits": func() error { return client.SetLimits(ctx, 500, 50) },
	} {
		if err := call(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	core.mu.Lock()
	defer core.mu.Unlock()
	if body := core.bodies["PATCH /api/v1/preferences"]; body["maxDownload"] != float64(500) || body["maxUpload"] != float64(50) {
		t.Fatalf("limits body = %v", body)
	}
	if _, ok := core.bodies["POST /api/v1/downloads/00112233445566778899aabbccddeeff/pause"]; !ok {
		t.Fatalf("the hash is not sent in lower case: %v", core.calls)
	}
}

func TestSearchRunsToTheEndKeepsEd2kFilesAndDropsTheSearch(t *testing.T) {
	core, client := startFake(t)
	got, err := client.Search(context.Background(), "Show S01E02", SearchOptions{Type: SearchKad, Poll: time.Millisecond, Wait: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].FileName != "Show.S01E02.mkv" || got[0].Sources != 9 {
		t.Fatalf("results = %+v, want the one eD2k file", got)
	}
	core.mu.Lock()
	defer core.mu.Unlock()
	if core.reads != 3 {
		t.Fatalf("results read %d times, want until finished (3)", core.reads)
	}
	if len(core.deleted) != 1 {
		t.Fatal("the finished search was left on the core")
	}
	if body := core.bodies["POST /api/v1/search"]; body["type"] != "kad" {
		t.Fatalf("search body = %v", body)
	}
}

func TestErrorsCarryTheCoreMessage(t *testing.T) {
	_, client := startFake(t)
	_, _, err := client.searchResults(context.Background(), "8")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 404 || apiErr.Message != "Search not found" {
		t.Fatalf("err = %v", err)
	}
	dead := New(Config{BaseURL: "http://127.0.0.1:1", APIKey: "key", Timeout: time.Second})
	var transport *TransportError
	if err := dead.Health(context.Background()); !errors.As(err, &transport) {
		t.Fatalf("an unreachable core = %v, want a TransportError", err)
	}
	if err := New(Config{}).Health(context.Background()); err == nil {
		t.Fatal("a client without address must fail")
	}
}
