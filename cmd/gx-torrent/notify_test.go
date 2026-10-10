package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNotifyPostsWebhook(t *testing.T) {
	got := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		var p map[string]any
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Errorf("decode: %v", err)
		}
		got <- p
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	d := &Daemon{opts: Options{NotifyURL: srv.URL}}
	d.notify("feed_match", map[string]any{"feed": "f", "added": 2, "titles": []string{"a", "b"}})

	select {
	case p := <-got:
		if p["app"] != "gx-torrent" || p["event"] != "feed_match" || p["feed"] != "f" {
			t.Fatalf("payload = %v", p)
		}
		if added, ok := p["added"].(float64); !ok || added != 2 {
			t.Fatalf("added = %v", p["added"])
		}
		if _, ok := p["time"].(string); !ok {
			t.Errorf("missing time: %v", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the webhook was not called")
	}
}

func TestNotifyDisabledWithoutURL(t *testing.T) {
	// No URL: notify must be a no-op and never block or panic.
	d := &Daemon{opts: Options{}}
	d.notify("feed_error", map[string]any{"error": "x"})
	if d.notifyURL() != "" {
		t.Fatal("notifyURL() should be empty")
	}
}
