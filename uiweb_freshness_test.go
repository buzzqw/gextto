package gextto

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestDataDirWritableCache checks the chrome reuses the data-dir writability
// result for the TTL instead of doing a create+fsync+delete on every render.
func TestDataDirWritableCache(t *testing.T) {
	oldTTL := dataDirWritableTTL
	defer func() { dataDirWritableTTL = oldTTL }()
	dataDirWritableCache = sync.Map{}

	dir := t.TempDir()
	if !dataDirWritable(dir) {
		t.Fatal("a writable dir was reported not writable")
	}
	if dataDirWritable(filepath.Join(dir, "missing")) {
		t.Fatal("a missing dir was reported writable")
	}

	// Within the TTL the result is cached: removing the dir is not seen.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if !dataDirWritable(dir) {
		t.Fatal("cached writable result not reused")
	}

	// With the cache disabled the change is seen.
	dataDirWritableTTL = 0
	if dataDirWritable(dir) {
		t.Fatal("a removed dir was still reported writable after the TTL")
	}
}

// TestV2ClientI18nMemoIsReset checks the per-AppState client dictionary cache can
// be dropped, so tests building many AppStates do not accumulate entries.
func TestV2ClientI18nMemoIsReset(t *testing.T) {
	state := newTestAppState(t)
	_ = v2ClientI18nJSON(state)
	count := func() int {
		n := 0
		v2ClientI18nMemo.Range(func(_, _ any) bool { n++; return true })
		return n
	}
	if count() == 0 {
		t.Fatal("client i18n memo empty after a render")
	}
	resetV2ClientI18nMemo()
	if count() != 0 {
		t.Fatalf("memo not reset: %d entries", count())
	}
}

// TestV2HealthRefreshesWhenDataChanges checks the Salute page is rendered fresh:
// data added after the first render appears on the next one (the GUI is updated
// when needed, not served stale).
func TestV2HealthRefreshesWhenDataChanges(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	_, body := v2Request(t, server, http.MethodGet, "/?view=health", nil)
	if strings.Contains(body, "Late Show") {
		t.Fatal("the queue shows a title before it exists")
	}

	now := time.Now().UTC()
	insertPendingEpisode(t, state.db, "Late Show", 1, 1, now.Add(-time.Minute), now.Add(time.Hour))

	_, body = v2Request(t, server, http.MethodGet, "/?view=health", nil)
	if !strings.Contains(body, "Late Show") {
		t.Fatal("the health page did not pick up the newly queued title")
	}
}
