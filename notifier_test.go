package gextto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/buzzqw/gextto/internal/messages"
)

// TestNotifierDisabledWhenNothingConfigured ports the
// `disabled_notifier_is_a_noop` test and checks a partially configured
// Telegram channel does not count as enabled.
func TestNotifierDisabledWhenNothingConfigured(t *testing.T) {
	if (&Notifier{}).Enabled() {
		t.Fatal("a zero Notifier should be disabled")
	}
	if NewNotifier().Enabled() {
		t.Fatal("NewNotifier should be disabled")
	}

	cfg := DefaultConfig()
	if FromConfig(&cfg).Enabled() {
		t.Fatal("DefaultConfig should not enable the notifier")
	}

	// Telegram enabled without credentials is still disabled.
	cfg.NotifyTelegram = true
	if FromConfig(&cfg).Enabled() {
		t.Fatal("Telegram without token/chat id should not be enabled")
	}
}

// TestNotifierHexBytes ports the `hmac_encoding_is_lowercase_hex` test.
func TestNotifierHexBytes(t *testing.T) {
	if got, want := hexBytes([]byte{0, 15, 255}), "000fff"; got != want {
		t.Fatalf("hexBytes = %q, want %q", got, want)
	}
}

// TestNotifierWebhookSignatureAndBody posts an event to an httptest webhook and
// verifies the HMAC-SHA256 signature header and the expected JSON envelope.
func TestNotifierWebhookSignatureAndBody(t *testing.T) {
	secret := "top-secret"
	bodyCh := make(chan []byte, 1)
	signatureCh := make(chan string, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
			http.Error(w, "content-type", http.StatusBadRequest)
			return
		}
		bodyCh <- body
		signatureCh <- r.Header.Get("x-gextto-signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.NotifyWebhookURL = &server.URL
	cfg.NotifyWebhookSecret = &secret
	notifier := FromConfig(&cfg)
	if !notifier.Enabled() {
		t.Fatal("a configured webhook should enable the notifier")
	}

	event := "comic_completed"
	data := map[string]any{"title": "Batman", "size_bytes": 1234, "method": "direct"}
	if err := notifier.NotifyEvent(event, data); err != nil {
		t.Fatalf("NotifyEvent: %v", err)
	}

	body := <-bodyCh
	signature := <-signatureCh

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	wantSignature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if signature != wantSignature {
		t.Fatalf("x-gextto-signature = %q, want %q", signature, wantSignature)
	}

	var envelope struct {
		Event string         `json:"event"`
		Data  map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode webhook body %q: %v", body, err)
	}
	if envelope.Event != event {
		t.Fatalf("event = %q, want %q", envelope.Event, event)
	}
	if envelope.Data["title"] != "Batman" {
		t.Fatalf("data.title = %#v", envelope.Data["title"])
	}
	if got, ok := envelope.Data["size_bytes"].(float64); !ok || got != 1234 {
		t.Fatalf("data.size_bytes = %#v, want 1234", envelope.Data["size_bytes"])
	}
	if envelope.Data["method"] != "direct" {
		t.Fatalf("data.method = %#v", envelope.Data["method"])
	}
}

// TestNotifierNoSignatureWithoutSecret checks the header is absent when no
// secret is configured.
func TestNotifierNoSignatureWithoutSecret(t *testing.T) {
	headerCh := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		headerCh <- r.Header.Get("x-gextto-signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.NotifyWebhookURL = &server.URL
	notifier := FromConfig(&cfg)
	if err := notifier.NotifyEvent("message", map[string]any{"text": "hi"}); err != nil {
		t.Fatalf("NotifyEvent: %v", err)
	}
	if signature := <-headerCh; signature != "" {
		t.Fatalf("expected no signature header, got %q", signature)
	}
}

// TestNotifierRetriesAfterServerError checks that a 500 response is retried.
// The first request fails, the retry succeeds, so the delivered count is two.
func TestNotifierRetriesAfterServerError(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		if atomic.AddInt32(&requests, 1) == 1 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.NotifyWebhookURL = &server.URL
	notifier := FromConfig(&cfg)

	if err := notifier.NotifyEvent("message", map[string]any{"text": "retry me"}); err != nil {
		t.Fatalf("NotifyEvent should recover on retry: %v", err)
	}
	if got := atomic.LoadInt32(&requests); got != 2 {
		t.Fatalf("requests = %d, want 2 (one failure + one retry)", got)
	}
}

// TestNotifierReportsPersistentServerError checks the error surfaces after the
// retries are exhausted. It costs the built-in backoff (1s + 2s).
func TestNotifierReportsPersistentServerError(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		atomic.AddInt32(&requests, 1)
		http.Error(w, "still broken", http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.NotifyWebhookURL = &server.URL
	notifier := FromConfig(&cfg)

	err := notifier.NotifyEvent("message", map[string]any{"text": "always fails"})
	if err == nil {
		t.Fatal("expected an error after retries are exhausted")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("error = %v, want HTTP 500", err)
	}
	if got := atomic.LoadInt32(&requests); got != 3 {
		t.Fatalf("requests = %d, want 3 (initial + 2 retries)", got)
	}
}

// TestNotifierFormatEventItalian ports the
// `formats_known_events_in_italian` test.
func TestNotifierFormatEventItalian(t *testing.T) {
	previous := messages.Language()
	t.Cleanup(func() { messages.SetLanguage(previous) })
	messages.SetLanguage("it")

	episode := map[string]any{"series": "FBI", "season": 2, "episode": 5}
	started := formatEvent("download_started", episode)
	if !strings.Contains(started, "Serie: FBI") {
		t.Fatalf("download_started missing series: %q", started)
	}
	if !strings.Contains(started, "S02E05") {
		t.Fatalf("download_started missing episode code: %q", started)
	}
	if got := formatEvent("gap_filled", episode); !strings.HasPrefix(got, "Gextto: gap riempito") {
		t.Fatalf("gap_filled = %q", got)
	}
	if got := formatEvent("message", map[string]any{"text": "ciao"}); got != "ciao" {
		t.Fatalf("message = %q, want ciao", got)
	}
	movie := map[string]any{"kind": "movie", "title": "Dune"}
	if got := formatEvent("download_started", movie); !strings.Contains(got, "Dune") {
		t.Fatalf("movie download_started missing title: %q", got)
	}
}

// TestNotifierFormatHelpers covers the human formatting helpers used in the
// notification bodies.
func TestNotifierFormatHelpers(t *testing.T) {
	if got, want := formatBytes(0), "0 B"; got != want {
		t.Fatalf("formatBytes(0) = %q, want %q", got, want)
	}
	if got, want := formatBytes(1536), "1.50 KB"; got != want {
		t.Fatalf("formatBytes(1536) = %q, want %q", got, want)
	}
	if got, want := formatBytes(-5), "0 B"; got != want {
		t.Fatalf("formatBytes(-5) = %q, want %q", got, want)
	}
	if got, want := formatDuration(3661), "1h 1m 1s"; got != want {
		t.Fatalf("formatDuration(3661) = %q, want %q", got, want)
	}
	if got, want := formatDuration(61), "1m 1s"; got != want {
		t.Fatalf("formatDuration(61) = %q, want %q", got, want)
	}
	if got, want := formatDuration(9), "9s"; got != want {
		t.Fatalf("formatDuration(9) = %q, want %q", got, want)
	}
}
