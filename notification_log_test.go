package gextto

// notification_log_test.go locks the record behind the Notifications page: what
// was sent, when, over which channels, with what outcome, and the trimming that
// keeps the table a page.

import (
	"fmt"
	"testing"
	"time"
)

func TestNotificationLogRecordsAndFilters(t *testing.T) {
	db, err := OpenDatabase(t.TempDir() + "/gextto.db")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	attempts := []NotificationRecord{
		{At: now.Add(-2 * time.Minute), Event: "torrent_completed", Title: "Serie — S01E01", Channels: []string{"telegram", "discord"}, Outcome: notificationSent},
		{At: now.Add(-time.Minute), Event: "download_failed", Title: "Serie — S01E02", Channels: []string{"webhook"}, Outcome: notificationFailed, Detail: "webhook: HTTP 500"},
		{At: now, Event: "gap_filled", Title: "Serie — S01E03", Outcome: notificationSuppressed, Detail: "the group switch of this event is off"},
	}
	for _, attempt := range attempts {
		if err := db.RecordNotification(attempt); err != nil {
			t.Fatal(err)
		}
	}
	all, err := db.NotificationLog(notificationLogRows, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("rows = %d, want 3", len(all))
	}
	// Newest first.
	if all[0].Event != "gap_filled" || all[2].Event != "torrent_completed" {
		t.Fatalf("order = %q, %q, %q", all[0].Event, all[1].Event, all[2].Event)
	}
	if len(all[2].Channels) != 2 || all[2].Channels[0] != "telegram" {
		t.Fatalf("channels = %v", all[2].Channels)
	}
	if all[0].At.IsZero() || all[0].At.Sub(now).Abs() > 2*time.Second {
		t.Fatalf("time lost: %v", all[0].At)
	}
	failed, err := db.NotificationLog(notificationLogRows, notificationFailed)
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 || failed[0].Detail != "webhook: HTTP 500" {
		t.Fatalf("failed filter = %+v", failed)
	}
}

func TestNotificationLogKeepsTheNewestRows(t *testing.T) {
	db, err := OpenDatabase(t.TempDir() + "/gextto.db")
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < notificationLogRows+5; index++ {
		record := NotificationRecord{
			At:      time.Now(),
			Event:   fmt.Sprintf("event_%d", index),
			Outcome: notificationSent,
		}
		if err := db.RecordNotification(record); err != nil {
			t.Fatal(err)
		}
	}
	records, err := db.NotificationLog(notificationLogRows*2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != notificationLogRows {
		t.Fatalf("rows = %d, want %d", len(records), notificationLogRows)
	}
	if records[0].Event != fmt.Sprintf("event_%d", notificationLogRows+4) {
		t.Fatalf("newest = %q", records[0].Event)
	}
}

func TestNotificationLogClearAndPrune(t *testing.T) {
	db, err := OpenDatabase(t.TempDir() + "/gextto.db")
	if err != nil {
		t.Fatal(err)
	}
	old := NotificationRecord{At: time.Now().Add(-notificationLogRetention - time.Hour), Event: "old", Outcome: notificationSent}
	fresh := NotificationRecord{At: time.Now(), Event: "fresh", Outcome: notificationSent}
	for _, record := range []NotificationRecord{old, fresh} {
		if err := db.RecordNotification(record); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := db.PruneNotificationLog(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("pruned = %d, want 1", removed)
	}
	records, err := db.NotificationLog(notificationLogRows, "")
	if err != nil || len(records) != 1 || records[0].Event != "fresh" {
		t.Fatalf("after prune = %+v (%v)", records, err)
	}
	if err := db.ClearNotificationLog(); err != nil {
		t.Fatal(err)
	}
	records, err = db.NotificationLog(notificationLogRows, "")
	if err != nil || len(records) != 0 {
		t.Fatalf("after clear = %+v (%v)", records, err)
	}
}
