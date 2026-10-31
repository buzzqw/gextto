package gextto

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/buzzqw/gextto/internal/notify"
)

// notificationRecorder is a fake HTTP server that records the requests the
// notification channels send, so one test can check every dedicated channel at
// once.
type notificationRecorder struct {
	mu       sync.Mutex
	requests []recordedNotification
}

type recordedNotification struct {
	path    string
	body    string
	headers http.Header
}

func (r *notificationRecorder) handler(w http.ResponseWriter, request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	r.mu.Lock()
	r.requests = append(r.requests, recordedNotification{
		path:    request.URL.Path,
		body:    string(body),
		headers: request.Header.Clone(),
	})
	r.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (r *notificationRecorder) snapshot() []recordedNotification {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedNotification(nil), r.requests...)
}

func (r *notificationRecorder) reset() {
	r.mu.Lock()
	r.requests = nil
	r.mu.Unlock()
}

// TestNotifierDedicatedChannels sends one event through every channel that
// Gextto shares with indexarr: Discord, Slack, Gotify, ntfy, Pushover, Apprise
// and the custom script. Each one must receive its own payload.
func TestNotifierDedicatedChannels(t *testing.T) {
	recorder := &notificationRecorder{}
	server := httptest.NewServer(http.HandlerFunc(recorder.handler))
	defer server.Close()

	// Pushover always posts to its API base; point it at the fake server.
	previousPushover := notify.PushoverAPIBase
	notify.PushoverAPIBase = server.URL + "/pushover"
	defer func() { notify.PushoverAPIBase = previousPushover }()

	// The custom script writes the environment it received to a file, so the
	// test can read the event without parsing stdout.
	dir := t.TempDir()
	script := filepath.Join(dir, "notify.sh")
	output := filepath.Join(dir, "event.txt")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s|%s' \"$GEXTTO_EVENT\" \"$GEXTTO_MESSAGE\" > '"+output+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	n := NewNotifier()
	n.extra = notify.Config{
		App:           "Gextto",
		DiscordURL:    server.URL + "/discord",
		SlackURL:      server.URL + "/slack",
		GotifyURL:     server.URL,
		GotifyToken:   "gotify-token",
		NtfyURL:       server.URL,
		NtfyTopic:     "gextto",
		NtfyToken:     "ntfy-token",
		PushoverToken: "pushover-token",
		PushoverUser:  "pushover-user",
		AppriseURL:    server.URL + "/apprise",
		AppriseKey:    "apprise-key",
		Script:        script,
	}
	if !n.Enabled() {
		t.Fatal("a notifier with dedicated channels must be enabled")
	}
	if err := n.NotifyEvent("torrent_completed", map[string]any{"title": "Dune", "kind": "movie"}); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	paths := map[string]recordedNotification{}
	for _, request := range recorder.snapshot() {
		paths[request.path] = request
	}
	for _, path := range []string{"/discord", "/slack", "/message", "/gextto", "/pushover", "/apprise/notify/apprise-key"} {
		if _, ok := paths[path]; !ok {
			t.Errorf("channel %s was not called; got %v", path, paths)
		}
	}
	if got := paths["/message"].headers.Get("X-Gotify-Key"); got != "gotify-token" {
		t.Errorf("Gotify key = %q", got)
	}
	if got := paths["/gextto"].headers.Get("Authorization"); got != "Bearer ntfy-token" {
		t.Errorf("ntfy authorization = %q", got)
	}
	if !strings.Contains(paths["/pushover"].body, "pushover-user") {
		t.Errorf("Pushover payload = %q", paths["/pushover"].body)
	}
	if !strings.Contains(paths["/apprise/notify/apprise-key"].body, `"title"`) {
		t.Errorf("Apprise payload = %q", paths["/apprise/notify/apprise-key"].body)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("script did not run: %v", err)
	}
	if !strings.Contains(string(raw), "torrent_completed") {
		t.Errorf("script environment = %q", raw)
	}
}

// TestNotifierEventSwitchesChecksThatAnEventGroupLeftOffSendsNothing, and that
// turning it back on delivers again. The hooks are not involved here.
func TestNotifierEventSwitches(t *testing.T) {
	var calls int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	target := server.URL
	n := NewNotifier()
	n.webhookURL = &target
	n.events = notifyEventSwitches{}

	if err := n.NotifyEvent("torrent_completed", map[string]any{"title": "Dune"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 0 {
		t.Fatalf("a switched-off event group was delivered (%d calls)", got)
	}

	n.events.Downloads = true
	if err := n.NotifyEvent("torrent_completed", map[string]any{"title": "Dune"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got = calls
	mu.Unlock()
	if got != 1 {
		t.Fatalf("switched-on downloads delivered %d events, want 1", got)
	}

	// A plain message and a test are not part of a group and always pass.
	if err := n.NotifyEvent("message", map[string]any{"text": "ciao"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got = calls
	mu.Unlock()
	if got != 2 {
		t.Fatalf("a plain message was filtered out (%d calls)", got)
	}
}

// TestNotifierAntiSpamWindow checks the cooldown: the same event is not sent
// twice within the window, while a different episode still is.
func TestNotifierAntiSpamWindow(t *testing.T) {
	resetNotifyCooldown()
	defer resetNotifyCooldown()

	var calls int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	target := server.URL
	n := NewNotifier()
	n.webhookURL = &target
	n.cooldownMinutes = 60

	event := map[string]any{"series": "FBI", "season": 1, "episode": 2, "title": "FBI S01E02"}
	for i := 0; i < 3; i++ {
		if err := n.NotifyEvent("torrent_completed", event); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 1 {
		t.Fatalf("the same event reached the channel %d times, want 1", got)
	}

	if err := n.NotifyEvent("torrent_completed", map[string]any{"series": "FBI", "season": 1, "episode": 3, "title": "FBI S01E03"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got = calls
	mu.Unlock()
	if got != 2 {
		t.Fatalf("a different episode was suppressed (%d calls)", got)
	}

	// 0 means no anti-spam window at all.
	n.cooldownMinutes = 0
	for i := 0; i < 2; i++ {
		if err := n.NotifyEvent("torrent_completed", event); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	got = calls
	mu.Unlock()
	if got != 4 {
		t.Fatalf("cooldown 0 still suppressed an event (%d calls)", got)
	}
}

// TestNotifierMessageLanguage checks that the message bodies follow the chosen
// language, independently of the interface language.
func TestNotifierMessageLanguage(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	target := server.URL
	n := NewNotifier()
	n.webhookURL = &target
	n.webhookFormat = "ntfy"
	n.language = "en"
	if err := n.NotifyEvent("download_started", map[string]any{"kind": "movie", "title": "Dune", "quality_score": 100}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "MOVIE DOWNLOAD STARTED") {
		t.Fatalf("English message expected, got %q", body)
	}
}

// TestNotifySettingsRoundTrip loads the new settings through the real config
// loader: the values the settings page saves must reach the typed fields the
// notifier reads.
func TestNotifySettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	conn, err := OpenConfigDB(filepath.Join(dir, "gextto_config.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlExec(conn, "CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);"); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	values := map[string]string{
		"notify_discord_url":        "https://discord.example/hook",
		"notify_slack_url":          "https://slack.example/hook",
		"notify_gotify_url":         "https://gotify.example",
		"notify_gotify_token":       "gotify-token",
		"notify_ntfy_url":           "https://ntfy.example",
		"notify_ntfy_topic":         "gextto",
		"notify_ntfy_token":         "ntfy-token",
		"notify_pushover_token":     "pushover-token",
		"notify_pushover_user":      "pushover-user",
		"notify_apprise_url":        "https://apprise.example",
		"notify_apprise_key":        "apprise-key",
		"notify_script_path":        "/usr/local/bin/notify",
		"notify_script_args":        "--flag value",
		"notify_events_downloads":   "false",
		"notify_events_errors":      "false",
		"notify_events_health":      "true",
		"notify_events_maintenance": "false",
		"notify_cooldown_minutes":   "15",
		"notify_language":           "en",
	}
	for key, value := range values {
		if err := sqlExec(conn, "INSERT INTO settings(key,value) VALUES (?1,?2)", key, value); err != nil {
			conn.Close()
			t.Fatal(err)
		}
	}
	conn.Close()

	cfg := DefaultConfig()
	cfg.DataDir = dir
	if err := cfg.loadConfigDB(); err != nil {
		t.Fatal(err)
	}
	if got := stringValue(cfg.NotifyDiscordURL); got != values["notify_discord_url"] {
		t.Errorf("Discord URL = %q", got)
	}
	if got := stringValue(cfg.NotifyScriptArgs); got != values["notify_script_args"] {
		t.Errorf("script args = %q", got)
	}
	if cfg.NotifyEventDownloads || cfg.NotifyEventErrors || cfg.NotifyEventMaintenance {
		t.Errorf("event switches not read: %+v", cfg)
	}
	if !cfg.NotifyEventHealth {
		t.Error("health switch should be on")
	}
	if cfg.NotifyCooldownMinutes != 15 || cfg.NotifyLanguage != "en" {
		t.Errorf("cooldown = %d, language = %q", cfg.NotifyCooldownMinutes, cfg.NotifyLanguage)
	}

	n := FromConfig(&cfg)
	if !n.Enabled() {
		t.Fatal("the notifier built from the saved settings is disabled")
	}
	if n.events.Downloads || !n.events.Health {
		t.Errorf("the notifier read the event switches wrong: %+v", n.events)
	}
	if n.cooldownMinutes != 15 || n.notifyLang() != "en" {
		t.Errorf("the notifier read cooldown/language wrong: %d %q", n.cooldownMinutes, n.notifyLang())
	}
}
