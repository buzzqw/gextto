package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// TestNotifyAppriseChannel checks the Apprise channel of the settings page:
// the address takes /notify and the payload carries the title, the text and
// the type Apprise understands.
func TestNotifyAppriseChannel(t *testing.T) {
	type request struct {
		path string
		body map[string]any
	}
	got := make(chan request, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		got <- request{path: r.URL.Path, body: body}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(notifyAppriseURLKey, srv.URL)
	d.notify("feed_match", map[string]any{"feed": "f", "added": 2, "titles": []string{"a", "b"}})

	select {
	case req := <-got:
		if req.path != "/notify" {
			t.Fatalf("path = %q, want /notify", req.path)
		}
		if req.body["title"] != "Feed f: 2 torrent(s) added" {
			t.Errorf("title = %v", req.body["title"])
		}
		if req.body["type"] != "success" {
			t.Errorf("type = %v, want success", req.body["type"])
		}
		if msg, _ := req.body["body"].(string); !strings.Contains(msg, "Feed f") || !strings.Contains(msg, "• a") {
			t.Errorf("body = %q", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the Apprise server was not called")
	}
}

// TestNotifyAppriseFailureIsReportedAsFailure covers the type of an event that
// went wrong: Apprise shows it differently from a success.
func TestNotifyAppriseFailureIsReportedAsFailure(t *testing.T) {
	got := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		got <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(notifyAppriseURLKey, srv.URL)
	d.notify("feed_error", map[string]any{"feed": "f", "error": "boom"})

	select {
	case body := <-got:
		if body["type"] != "failure" {
			t.Fatalf("type = %v, want failure", body["type"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the Apprise server was not called")
	}
}

// TestNotifyGotifyChannel checks the dedicated Gotify channel: the server
// address gets /message and the application token travels in the header.
func TestNotifyGotifyChannel(t *testing.T) {
	type request struct {
		path  string
		token string
		body  map[string]any
	}
	got := make(chan request, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		got <- request{path: r.URL.Path, token: r.Header.Get("X-Gotify-Key"), body: body}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(notifyGotifyURLKey, srv.URL+"/")
	d.opts.Settings.Set(notifyGotifyTokenKey, "app-token")
	d.notify("download_complete", map[string]any{"name": "Movie.mkv"})

	select {
	case req := <-got:
		if req.path != "/message" {
			t.Fatalf("path = %q, want /message", req.path)
		}
		if req.token != "app-token" {
			t.Errorf("X-Gotify-Key = %q", req.token)
		}
		if msg, _ := req.body["message"].(string); !strings.Contains(msg, "Movie.mkv") {
			t.Errorf("message = %q", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the Gotify server was not called")
	}
}

// TestNotifyCustomScriptReceivesTheEvent runs a real script: the event must
// travel in the GX_TORRENT_* variables, never on the command line.
func TestNotifyCustomScriptReceivesTheEvent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test script is a POSIX shell script")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "event.txt")
	script := filepath.Join(dir, "notify.sh")
	body := "#!/bin/sh\nprintf '%s\\n' \"$GX_TORRENT_EVENT\" \"$GX_TORRENT_TITLE\" > \"$1\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(notifyScriptKey, script)
	d.opts.Settings.Set(notifyScriptArgsKey, out)
	d.notify("download_complete", map[string]any{"name": "Movie.mkv"})

	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(out)
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) < 2 || lines[0] != "download_complete" || lines[1] != "Download complete: Movie.mkv" {
				t.Fatalf("script received %q", data)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the script was not run: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
