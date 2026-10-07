package gextto

import (
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

func TestDecoratedTorrentsCarryStallState(t *testing.T) {
	db := newTestDB(t)
	since := time.Date(2026, time.October, 6, 20, 36, 0, 0, time.UTC)
	next := since.Add(3 * time.Hour)
	if err := db.SaveStallWatch("ABC", StallWatch{lastProgressAt: since, stalledSince: &since, nextRetryAt: next}); err != nil {
		t.Fatal(err)
	}
	s := &AppState{db: db}
	items := gh3DecorateTorrents(s, []models.TorrentView{{Hash: "abc", Name: "stuck"}, {Hash: "def", Name: "fine"}})
	if items[0]["stalled_since"] != since.Format(time.RFC3339) || items[0]["next_retry_at"] != next.Format(time.RFC3339) {
		t.Fatalf("stalled torrent = %v", items[0])
	}
	if _, ok := items[1]["stalled_since"]; ok {
		t.Fatalf("a running torrent must not carry stall fields: %v", items[1])
	}
}
