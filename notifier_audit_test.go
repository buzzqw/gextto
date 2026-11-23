package gextto

// notifier_audit_test.go locks the record the Notifications page reads: every
// attempt leaves one row, with the channels that were tried, the outcome and
// the reason when it failed or was held back.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// captureNotifications installs a sink in place of the database and returns the
// records it receives. The notifier calls the sink from its own goroutine, so
// both the append and the read need a lock under -race.
func captureNotifications(t *testing.T) func() []NotificationRecord {
	t.Helper()
	var mu sync.Mutex
	records := []NotificationRecord{}
	previous := setNotifyAudit(func(record NotificationRecord) {
		mu.Lock()
		records = append(records, record)
		mu.Unlock()
	})
	t.Cleanup(func() { setNotifyAudit(previous) })
	return func() []NotificationRecord {
		mu.Lock()
		defer mu.Unlock()
		return append([]NotificationRecord(nil), records...)
	}
}

func TestNotificationAttemptIsRecordedWhenItSucceeds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	records := captureNotifications(t)

	target := server.URL
	notifier := NewNotifier()
	notifier.webhookURL = &target
	if err := notifier.NotifyEvent("torrent_completed", map[string]any{"title": "Serie"}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	got := records()
	if len(got) != 1 {
		t.Fatalf("records = %d, want 1", len(got))
	}
	record := got[0]
	if record.Outcome != notificationSent {
		t.Fatalf("outcome = %q, want sent", record.Outcome)
	}
	if record.Event != "torrent_completed" || strings.TrimSpace(record.Title) == "" {
		t.Fatalf("event/title = %q/%q", record.Event, record.Title)
	}
	if len(record.Channels) != 1 || record.Channels[0] != "webhook" {
		t.Fatalf("channels = %v", record.Channels)
	}
	if record.Detail != "" {
		t.Fatalf("detail = %q, want empty", record.Detail)
	}
}

func TestNotificationAttemptIsRecordedWhenItFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	records := captureNotifications(t)

	// The retries sleep one and two units; shorten the unit so the test stays
	// fast, as the other notifier tests do.
	previousUnit := notifierRetryUnit
	notifierRetryUnit = 0
	t.Cleanup(func() { notifierRetryUnit = previousUnit })

	target := server.URL
	notifier := NewNotifier()
	notifier.webhookURL = &target
	if err := notifier.NotifyEvent("download_failed", map[string]any{"title": "Serie"}); err == nil {
		t.Fatal("a rejected webhook must still return the error")
	}
	got := records()
	if len(got) != 1 {
		t.Fatalf("records = %d, want 1", len(got))
	}
	if got[0].Outcome != notificationFailed {
		t.Fatalf("outcome = %q, want failed", got[0].Outcome)
	}
	if !strings.Contains(got[0].Detail, "webhook") || !strings.Contains(got[0].Detail, "500") {
		t.Fatalf("detail = %q, want the channel and the status", got[0].Detail)
	}
}

func TestNotificationAttemptIsRecordedWhenTheSwitchHoldsItBack(t *testing.T) {
	records := captureNotifications(t)
	notifier := NewNotifier()
	notifier.events.Downloads = false
	if err := notifier.NotifyEvent("torrent_completed", map[string]any{"title": "Serie"}); err != nil {
		t.Fatalf("a held-back event must not fail the caller: %v", err)
	}
	got := records()
	if len(got) != 1 || got[0].Outcome != notificationSuppressed {
		t.Fatalf("records = %+v, want one suppressed", got)
	}
	if !strings.Contains(got[0].Detail, "switch") {
		t.Fatalf("detail = %q, want the reason", got[0].Detail)
	}
}

func TestNotificationAttemptWithNoChannelIsRecorded(t *testing.T) {
	records := captureNotifications(t)
	// No channel configured: nothing is sent, and the page must say so
	// instead of showing an empty attempt as delivered.
	if err := NewNotifier().NotifyEvent("torrent_completed", map[string]any{"title": "Serie"}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	got := records()
	if len(got) != 1 || got[0].Outcome != notificationSuppressed {
		t.Fatalf("records = %+v, want one suppressed", got)
	}
	if !strings.Contains(got[0].Detail, "nessun canale") {
		t.Fatalf("detail = %q", got[0].Detail)
	}
}
