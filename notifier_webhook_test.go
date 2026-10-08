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
// and keeps the signed Gextto envelope as the default. Credentials come from
// the dedicated token/user fields, never from the URL.
func TestNotifierWebhookFormats(t *testing.T) {
	var gotBody, gotType string
	var gotHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		gotType = r.Header.Get("Content-Type")
		gotHeaders = r.Header.Clone()
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
	envelopeNotifier := newNotifier("gextto")
	secret := "s3cr3t"
	envelopeNotifier.webhookSecret = &secret
	if err := envelopeNotifier.NotifyEvent("message", data); err != nil {
		t.Fatalf("gextto: %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(gotBody), &envelope); err != nil || envelope["event"] != "message" {
		t.Fatalf("gextto payload = %q", gotBody)
	}
	if gotHeaders.Get("x-gextto-signature") == "" {
		t.Fatal("gextto payload not signed")
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

	// Gotify: the application token travels in the X-Gotify-Key header.
	gotify := newNotifier("gotify")
	token := "apptoken"
	gotify.webhookToken = &token
	if err := gotify.NotifyEvent("message", data); err != nil {
		t.Fatalf("gotify: %v", err)
	}
	if !strings.Contains(gotBody, `"message"`) || gotHeaders.Get("X-Gotify-Key") != "apptoken" {
		t.Fatalf("gotify payload = %q key=%q", gotBody, gotHeaders.Get("X-Gotify-Key"))
	}

	// ntfy: plain text, token as the Bearer token.
	ntfy := newNotifier("ntfy")
	ntfy.webhookToken = &token
	if err := ntfy.NotifyEvent("message", data); err != nil {
		t.Fatalf("ntfy: %v", err)
	}
	if !strings.HasPrefix(gotType, "text/plain") || gotBody != "hello" {
		t.Fatalf("ntfy payload = %q type=%q", gotBody, gotType)
	}
	if gotHeaders.Get("Authorization") != "Bearer apptoken" {
		t.Fatalf("ntfy authorization = %q", gotHeaders.Get("Authorization"))
	}

	// Pushover: form body with the token and the user key.
	pushover := newNotifier("pushover")
	user := "userkey"
	pushover.webhookToken = &token
	pushover.webhookUser = &user
	if err := pushover.NotifyEvent("message", data); err != nil {
		t.Fatalf("pushover: %v", err)
	}
	if !strings.HasPrefix(gotType, "application/x-www-form-urlencoded") ||
		!strings.Contains(gotBody, "message=hello") ||
		!strings.Contains(gotBody, "token=apptoken") ||
		!strings.Contains(gotBody, "user=userkey") {
		t.Fatalf("pushover payload = %q type=%q", gotBody, gotType)
	}
}
