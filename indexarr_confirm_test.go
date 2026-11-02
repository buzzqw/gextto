package gextto

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestIndexarrConfirmURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"base", "http://indexarr:8788", "http://indexarr:8788/api/v2/downloads/confirm"},
		{"trailing slash", "http://indexarr:8788/", "http://indexarr:8788/api/v2/downloads/confirm"},
		{"api path", "http://indexarr:8788/api", "http://indexarr:8788/api/v2/downloads/confirm"},
		{"torznab path", "http://indexarr:8788/torznab/api", "http://indexarr:8788/api/v2/downloads/confirm"},
		{"prowlarr path", "http://indexarr:8788/api/v1/search", "http://indexarr:8788/api/v2/downloads/confirm"},
		{"subpath", "https://host.example/indexarr/api", "https://host.example/indexarr/api/v2/downloads/confirm"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := indexarrConfirmURL(tc.in); got != tc.want {
				t.Fatalf("indexarrConfirmURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIndexarrConfirmTargets(t *testing.T) {
	cfg := &Config{Indexers: []IndexerConfig{
		{Name: "indexarr", URL: "http://indexarr:8788", APIKey: "key-one", Enabled: true, Manager: ManagerIndexarr},
		{Name: "indexarr duplicate", URL: "http://indexarr:8788/api", APIKey: "key-one", Enabled: true, Manager: ManagerIndexarr},
		{Name: "indexarr disabled", URL: "http://indexarr:8789", APIKey: "key-two", Enabled: false, Manager: ManagerIndexarr},
		{Name: "indexarr no key", URL: "http://indexarr:8790", APIKey: "", Enabled: true, Manager: ManagerIndexarr},
		{Name: "jackett", URL: "http://jackett:9117", APIKey: "key-three", Enabled: true, Manager: ManagerJackett},
	}}
	targets := indexarrConfirmTargets(cfg)
	if len(targets) != 1 {
		t.Fatalf("targets = %+v, want the one enabled indexarr entry", targets)
	}
	if targets[0].URL != "http://indexarr:8788/api/v2/downloads/confirm" || targets[0].APIKey != "key-one" {
		t.Fatalf("target = %+v", targets[0])
	}
	if got := indexarrConfirmTargets(nil); len(got) != 0 {
		t.Fatalf("nil config produced %+v", got)
	}
}

func TestSendIndexarrConfirmation(t *testing.T) {
	var gotMethod, gotPath, gotKey, gotAgent string
	var got indexarrConfirmation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotKey = r.Header.Get("X-Api-Key")
		gotAgent = r.Header.Get("User-Agent")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"event":"download.completed","matched":true}`))
	}))
	defer server.Close()

	payload := indexarrConfirmation{
		Event:       indexarrConfirmEvent,
		Protocol:    "torrent",
		Hash:        "0123456789abcdef0123456789abcdef01234567",
		Title:       "Release title",
		Client:      "gextto",
		CompletedAt: "2026-11-02T14:00:00Z",
	}
	target := indexarrConfirmTarget{URL: server.URL + "/api/v2/downloads/confirm", APIKey: "secret-key"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := sendIndexarrConfirmation(ctx, server.Client(), target, payload); err != nil {
		t.Fatalf("sendIndexarrConfirmation: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v2/downloads/confirm" {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
	if gotKey != "secret-key" || gotAgent != "gextto" {
		t.Fatalf("headers = key %q agent %q", gotKey, gotAgent)
	}
	if got != payload {
		t.Fatalf("payload = %+v, want %+v", got, payload)
	}
}

func TestNotifyIndexarrCompletionSendsOnce(t *testing.T) {
	var calls atomic.Int64
	done := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case done <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &Config{Indexers: []IndexerConfig{
		{Name: "indexarr", URL: server.URL, APIKey: "key", Enabled: true, Manager: ManagerIndexarr},
	}}
	hash := "fedcba9876543210fedcba9876543210fedcba98"
	notifyIndexarrCompletion(cfg, hash, "torrent", "Release", 0, time.Now())
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the confirmation was not sent")
	}
	// A second completion event for the same download is suppressed by this
	// process, because indexarr has already confirmed it.
	notifyIndexarrCompletion(cfg, hash, "torrent", "Release", 0, time.Now())
	select {
	case <-done:
		t.Fatal("the confirmation was sent twice")
	case <-time.After(150 * time.Millisecond):
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

func TestIndexarrProtocolAndTitle(t *testing.T) {
	if got := indexarrProtocol("ed2k", "0123456789abcdef0123456789abcdef"); got != "ed2k" {
		t.Errorf("ed2k network = %q", got)
	}
	if got := indexarrProtocol("", "0123456789abcdef0123456789abcdef"); got != "ed2k" {
		t.Errorf("32-char hash = %q, want ed2k", got)
	}
	if got := indexarrProtocol("torrent", "0123456789abcdef0123456789abcdef01234567"); got != "torrent" {
		t.Errorf("torrent network = %q", got)
	}
	if got := indexarrCompletionTitle("  Release  ", "fallback"); got != "Release" {
		t.Errorf("title = %q", got)
	}
	if got := indexarrCompletionTitle("", "fallback"); got != "fallback" {
		t.Errorf("fallback title = %q", got)
	}
}
