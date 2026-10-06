package gextto

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAsyncNotifierDoesNotBlockTheCaller(t *testing.T) {
	delivered := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Second)
		w.WriteHeader(http.StatusOK)
		delivered <- struct{}{}
	}))
	defer server.Close()
	notifier := NewNotifier()
	url := server.URL
	notifier.webhookURL = &url
	async := notifier.Async()

	started := time.Now()
	if err := async.NotifyEvent("download_started", map[string]any{"title": "x"}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("async notification blocked the caller for %s", elapsed)
	}
	select {
	case <-delivered:
	case <-time.After(5 * time.Second):
		t.Fatal("queued notification was never delivered")
	}
	if notifier.async {
		t.Fatal("Async must not modify the original notifier")
	}
}
