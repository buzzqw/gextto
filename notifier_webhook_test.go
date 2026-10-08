package gextto

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNotifierWebhookFormats checks that the webhook format selector produces
// the payload each provider expects (Discord, Slack, ntfy, Gotify, Pushover)
// and keeps the signed Gextto envelope as the default.
func TestNotifierWebhookFormats(t *testing.T) {
	var gotBody, gotType, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		gotType = r.Header.Get("Content-Type")
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	newNotifier := func(format string) *Notifier {
		url := server.URL
		n := NewNotifier()
		n.webhookURL = &url
		n.webhookFormat = format
		return n
	}

	data := map[string]any{"text": "hello"}

	// Default: signed JSON envelope.
	if err := newNotifier("gextto").NotifyEvent("message", data); err != nil {
		t.Fatalf("gextto: %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(gotBody), &envelope); err != nil || envelope["event"] != "message" {
		t.Fatalf("gextto payload = %q", gotBody)
	}

	if err := newNotifier("discord").NotifyEvent("message", data); err != nil {
		t.Fatalf("discord: %v", err)
	}
	if !strings.Contains(gotBody, `"content"`) || !strings.HasPrefix(gotType, "application/json") {
		t.Fatalf("discord payload = %q type=%q", gotBody, gotType)
	}

	if err := newNotifier("slack").NotifyEvent("message", data); err != nil {
		t.Fatalf("slack: %v", err)
	}
	if !strings.Contains(gotBody, `"text"`) {
		t.Fatalf("slack payload = %q", gotBody)
	}

	if err := newNotifier("gotify").NotifyEvent("message", data); err != nil {
		t.Fatalf("gotify: %v", err)
	}
	if !strings.Contains(gotBody, `"message"`) || !strings.Contains(gotBody, `"title"`) {
		t.Fatalf("gotify payload = %q", gotBody)
	}

	// ntfy posts the plain text and uses the secret as the Bearer token.
	n := newNotifier("ntfy")
	token := "tk_test"
	n.webhookSecret = &token
	if err := n.NotifyEvent("message", data); err != nil {
		t.Fatalf("ntfy: %v", err)
	}
	if !strings.HasPrefix(gotType, "text/plain") || gotBody != "hello" {
		t.Fatalf("ntfy payload = %q type=%q", gotBody, gotType)
	}
	if gotAuth != "Bearer tk_test" {
		t.Fatalf("ntfy authorization = %q", gotAuth)
	}

	if err := newNotifier("pushover").NotifyEvent("message", data); err != nil {
		t.Fatalf("pushover: %v", err)
	}
	if !strings.HasPrefix(gotType, "application/x-www-form-urlencoded") || !strings.Contains(gotBody, "message=hello") {
		t.Fatalf("pushover payload = %q type=%q", gotBody, gotType)
	}
}
