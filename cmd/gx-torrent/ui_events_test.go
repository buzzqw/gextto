package main

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestHandleUIEventsStreams(t *testing.T) {
	d := newQuietDaemon(t)
	if d.session == nil {
		t.Skip("no session")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the handler returns as soon as the client is gone

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ui/events", nil).WithContext(ctx)
	d.handleUIEvents(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
}
