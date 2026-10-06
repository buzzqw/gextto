package gextto

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestComicDownloadAbortsWhenTheServerStalls(t *testing.T) {
	previous := comicsHTTPStallTimeout
	comicsHTTPStallTimeout = 200 * time.Millisecond
	defer func() { comicsHTTPStallTimeout = previous }()

	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "1000000")
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		select { // then never send anything else
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)

	id := registerHTTPDownload("stall", "http", server.URL)
	control := installHTTPControl(id, server.Client(), server.URL, t.TempDir(), "stall")
	started := time.Now()
	_, _, err := downloadHTTPOnce(server.Client(), id, server.URL, t.TempDir(), "stall", control)
	if err == nil || !strings.Contains(err.Error(), "stalled") {
		t.Fatalf("expected a stall error, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("stall not detected in time: %s", elapsed)
	}
}
