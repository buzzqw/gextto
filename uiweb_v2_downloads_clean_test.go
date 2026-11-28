package gextto

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

type emptyCleanupEngine struct{ TorrentEngine }

func (emptyCleanupEngine) Name() string               { return BackendGxTorrent }
func (emptyCleanupEngine) List() []models.TorrentView { return nil }
func (emptyCleanupEngine) Trackers(string) ([]models.TrackerView, bool, error) {
	return nil, true, nil
}

// TestV2ClearCompletedRemovesFinishedHTTPDownloads locks the manual cleanup to
// the HTTP downloads too: they never seed, so a completed one must not stay in
// the list waiting for a seed limit that does not exist.
func TestV2ClearCompletedRemovesFinishedHTTPDownloads(t *testing.T) {
	state := newTestAppState(t)
	state.setActiveEngine(emptyCleanupEngine{})
	completed := registerHTTPDownload("Finished HTTP", "http", "https://example.test/finished.cbz")
	active := registerHTTPDownload("Active HTTP", "http", "https://example.test/active.cbz")
	updateHTTPDownload(completed, func(download *ComicDownload) { download.Status = "completed" })
	updateHTTPDownload(active, func(download *ComicDownload) { download.Status = "downloading" })
	t.Cleanup(func() {
		removeHTTPDownload(completed)
		removeHTTPDownload(active)
	})

	server := httptest.NewServer(Router(state))
	defer server.Close()
	code, body := v2Request(t, server, http.MethodPost, "/downloads/settings", url.Values{
		"op": {"clear_completed"},
	})
	if code != http.StatusOK {
		t.Fatalf("clear completed -> %d: %s", code, body)
	}
	if !strings.Contains(body, "download HTTP completat") || !strings.Contains(body, "Finished HTTP") {
		t.Fatalf("cleanup message does not mention the HTTP download: %s", body)
	}
	remaining := map[string]string{}
	for _, download := range HTTPDownloads() {
		remaining[download.ID] = download.Status
	}
	if _, ok := remaining[completed]; ok {
		t.Fatal("the completed HTTP download is still in the list")
	}
	if status, ok := remaining[active]; !ok || status != "downloading" {
		t.Fatalf("the active HTTP download was removed or changed: %+v", remaining)
	}
}
