package gextto

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestScrapeFeedCancelledCycleDoesNotBackoff: a feed interrupted because the
// whole cycle was cancelled (shutdown) must not put the provider in backoff.
func TestScrapeFeedCancelledCycleDoesNotBackoff(t *testing.T) {
	db := newTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	engine := NewEngine().WithDB(db)
	engine.scrapeFeed(ctx, &Config{DataDir: t.TempDir()}, server.URL+"/rss", 1, 0, 0)
	statuses, err := db.ProviderStatuses()
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range statuses {
		if status.Level > 0 {
			t.Fatalf("provider in backoff after a cancelled cycle: %+v", status)
		}
	}
}
