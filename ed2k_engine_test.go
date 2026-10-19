package gextto

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestEd2kEngineListAndEvents(t *testing.T) {
	var mu sync.Mutex
	transfers := []ed2kTransfer{{
		ID: "0123456789abcdef0123456789abcdef", Hash: "0123456789abcdef0123456789abcdef",
		Name: "film.mkv", Size: 1000, Done: 500, State: "downloading", Sources: 4,
	}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path != "/api/v1/transfers" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "transfers": transfers})
	}))
	defer server.Close()

	cfg := &Config{Settings: map[string]string{"ed2k_url": server.URL, "ed2k_enabled": "true"}}
	engine, err := newEd2kEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rows := engine.List()
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	row := rows[0]
	if row.Network != "ed2k" || row.Progress != 50 || row.State != "downloading" || row.Sources != 4 {
		t.Fatalf("row = %+v", row)
	}
	if row.SavePath != "" {
		t.Fatalf("save path without a completed file = %q", row.SavePath)
	}

	if events := engine.PollEvents(); len(events) != 0 {
		t.Fatalf("first poll events = %+v", events)
	}
	mu.Lock()
	transfers[0].State = "completed"
	transfers[0].Done = 1000
	transfers[0].Path = "/downloads/film.mkv"
	mu.Unlock()
	events := engine.PollEvents()
	if len(events) != 1 || events[0].Kind != "torrent_finished" || events[0].Hash != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("events = %+v", events)
	}
	if events[0].SavePath != "/downloads" {
		t.Fatalf("save path = %q", events[0].SavePath)
	}
}

func TestEd2kEngineAddAndRemove(t *testing.T) {
	var added, removed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/transfers":
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload["link"] == "" {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			added = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/transfers/0123456789abcdef0123456789abcdef":
			removed = true
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := &Config{Settings: map[string]string{"ed2k_url": server.URL}}
	engine, err := newEd2kEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	link := "ed2k://|file|film.mkv|1000|0123456789abcdef0123456789abcdef|/"
	ok, err := engine.AddWithOptions(link, cfg, nil, AddOptions{})
	if err != nil || !ok || !added {
		t.Fatalf("add = %v, %v (request %v)", ok, err, added)
	}
	if ok, err := engine.Remove("0123456789abcdef0123456789abcdef", false); err != nil || !ok || !removed {
		t.Fatalf("remove = %v, %v (request %v)", ok, err, removed)
	}
	if _, err := engine.AddWithOptions("magnet:?xt=urn:btih:abc", cfg, nil, AddOptions{}); err == nil {
		t.Fatal("magnet accepted by the eD2k engine")
	}
}

func TestMultiEngineLabelsAndRoutesEd2kRows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "transfers": []ed2kTransfer{{
			Hash: "0123456789abcdef0123456789abcdef", Name: "film.mkv", Size: 10, Done: 5, State: "downloading",
		}}})
	}))
	defer server.Close()
	cfg := &Config{Settings: map[string]string{"ed2k_url": server.URL}}
	adapter, err := newEd2kEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	multi := newMultiEngine(nullEngine{}, adapter, nil)
	rows := multi.List()
	if len(rows) != 1 || rows[0].Network != networkEd2k {
		t.Fatalf("rows = %+v", rows)
	}
	if multi.NetworkOf("0123456789abcdef0123456789abcdef") != networkEd2k {
		t.Fatal("route not learned from the list")
	}
}
