package gextto

// uiweb_v2_notifications_test.go covers the Notifications page: the attempts
// are listed newest first, the filter keeps one outcome, and the clear action
// empties the log and reports it.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestV2NotificationsPageListsTheAttempts(t *testing.T) {
	state := newTestAppState(t)
	if err := state.db.RecordNotification(NotificationRecord{
		At: time.Now().Add(-time.Minute), Event: "torrent_completed", Title: "Serie — S01E01",
		Channels: []string{"telegram"}, Outcome: notificationSent,
	}); err != nil {
		t.Fatal(err)
	}
	if err := state.db.RecordNotification(NotificationRecord{
		At: time.Now(), Event: "download_failed", Title: "Serie — S01E02",
		Channels: []string{"webhook"}, Outcome: notificationFailed, Detail: "webhook: HTTP 500",
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	defer server.Close()

	code, page := v2Request(t, server, http.MethodGet, "/?view=notifications", nil)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	for _, want := range []string{"Serie — S01E01", "Serie — S01E02", "webhook: HTTP 500", "telegram"} {
		if !strings.Contains(page, want) {
			t.Fatalf("page misses %q", want)
		}
	}
	// Newest first: the failed attempt comes before the completed one.
	if strings.Index(page, "Serie — S01E02") > strings.Index(page, "Serie — S01E01") {
		t.Fatal("the newest attempt must come first")
	}

	// The filter keeps one outcome only.
	code, page = v2Request(t, server, http.MethodGet, "/?view=notifications&outcome=failed", nil)
	if code != http.StatusOK {
		t.Fatalf("filtered status = %d", code)
	}
	if !strings.Contains(page, "Serie — S01E02") || strings.Contains(page, "Serie — S01E01") {
		t.Fatal("the outcome filter did not narrow the list")
	}
}

func TestV2NotificationsClearEmptiesTheLog(t *testing.T) {
	state := newTestAppState(t)
	if err := state.db.RecordNotification(NotificationRecord{
		At: time.Now(), Event: "torrent_completed", Title: "Serie", Outcome: notificationSent,
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	defer server.Close()

	code, _ := v2Request(t, server, http.MethodPost, "/notifications/clear", url.Values{})
	if code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 for an HTMX request", code)
	}
	records, err := state.db.NotificationLog(notificationLogRows, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("records after clear = %+v", records)
	}
}
