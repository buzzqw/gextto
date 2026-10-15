package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// uiPost posts a page form the way the page script does (X-GX-Fetch) and
// returns the JSON outcome.
func uiPost(t *testing.T, srv *httptest.Server, path string, form url.Values) (bool, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-GX-Fetch", "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		OK  bool   `json:"ok"`
		Msg string `json:"msg"`
	}
	raw, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: not JSON: %s", path, raw)
	}
	return out.OK, out.Msg
}

func uiTestTorrent(t *testing.T, d *Daemon, name string) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "src")
	hash, _, err := d.add(addRequest{TorrentData: makeTorrent(t, src, name, 80_000), Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hash)
	return hash
}

// TestUIBulkContextMenuActions covers the context-menu operations added to
// /ui/bulk: force start (pin + resume), sequential and first/last toggles,
// queue bottom and super-seeding.
func TestUIBulkContextMenuActions(t *testing.T) {
	d := newTestDaemon(t)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	a := uiTestTorrent(t, d, "a.bin")
	b := uiTestTorrent(t, d, "b.bin")

	meta := func(hash string) torrentMeta {
		d.mu.Lock()
		defer d.mu.Unlock()
		_, m := d.findLocked(hash)
		return *m
	}
	for _, op := range []string{"force-start", "sequential", "first-last", "super-seed-on"} {
		if ok, msg := uiPost(t, srv, "/ui/bulk", url.Values{"op": {op}, "hashes": {a, b}}); !ok {
			t.Fatalf("%s: %s", op, msg)
		}
	}
	for _, h := range []string{a, b} {
		m := meta(h)
		if !m.Pinned || !m.Sequential || !m.FirstLast || !m.SuperSeeding {
			t.Fatalf("%s after the bulk toggles: pinned=%v sequential=%v firstlast=%v superseed=%v", h, m.Pinned, m.Sequential, m.FirstLast, m.SuperSeeding)
		}
	}
	if ok, msg := uiPost(t, srv, "/ui/bulk", url.Values{"op": {"unforce"}, "hashes": {a}}); !ok || meta(a).Pinned {
		t.Fatalf("unforce: ok=%v %s pinned=%v", ok, msg, meta(a).Pinned)
	}
	if ok, msg := uiPost(t, srv, "/ui/bulk", url.Values{"op": {"bottom"}, "hashes": {a}}); !ok {
		t.Fatalf("bottom: %s", msg)
	}
	if ok, _ := uiPost(t, srv, "/ui/bulk", url.Values{"op": {"nope"}, "hashes": {a}}); ok {
		t.Fatal("an unknown bulk operation must fail")
	}
}

// TestUIBulkLimitsAndGlobalLimits covers the "Limit speed…" dialog (several
// torrents, -1 = follow the global limit) and the status-bar global limits.
func TestUIBulkLimitsAndGlobalLimits(t *testing.T) {
	d := newTestDaemon(t)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	a := uiTestTorrent(t, d, "a.bin")
	b := uiTestTorrent(t, d, "b.bin")

	if ok, msg := uiPost(t, srv, "/ui/bulk-limits", url.Values{"hashes": {a, b}, "download_limit": {"300"}, "upload_limit": {"-1"}}); !ok {
		t.Fatalf("bulk-limits: %s", msg)
	}
	for _, h := range []string{a, b} {
		d.mu.Lock()
		_, m := d.findLocked(h)
		down, up := m.DownloadLimitKib, m.UploadLimitKib
		d.mu.Unlock()
		if down == nil || *down != 300 {
			t.Fatalf("%s download limit = %v, want 300", h, down)
		}
		if up != nil && *up >= 0 {
			t.Fatalf("%s upload limit = %d, want the global one", h, *up)
		}
	}
	if ok, _ := uiPost(t, srv, "/ui/bulk-limits", url.Values{"download_limit": {"1"}}); ok {
		t.Fatal("limits without a selection must fail")
	}

	if ok, msg := uiPost(t, srv, "/ui/global-limits", url.Values{"download_limit": {"2048"}, "upload_limit": {"512"}}); !ok {
		t.Fatalf("global-limits: %s", msg)
	}
	if cfg, _, _ := d.config(); cfg.SpeedLimitDownload != 2048 || cfg.SpeedLimitUpload != 512 {
		t.Fatalf("global limits = %d/%d, want 2048/512", cfg.SpeedLimitDownload, cfg.SpeedLimitUpload)
	}
	page, err := d.uiPageData(0)
	if err != nil {
		t.Fatal(err)
	}
	if page.GlobalDownKib != 2048 || page.GlobalUpKib != 512 {
		t.Fatalf("status bar limits = %d/%d", page.GlobalDownKib, page.GlobalUpKib)
	}
}

// TestUIPageCountsEveryStatusFilter checks the sidebar counts follow the same
// predicates as the page filter, and the tracker filter groups torrents
// without a working tracker under the empty host.
func TestUIPageCountsEveryStatusFilter(t *testing.T) {
	d := newTestDaemon(t)
	a := uiTestTorrent(t, d, "a.bin")
	uiTestTorrent(t, d, "b.bin")
	// The data is on disk: wait for both to be verified complete first.
	waitFor(t, "both complete", func() bool {
		page, _ := d.uiPageData(0)
		return page.CountCompleted == 2
	})
	if err := d.pause(a); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "paused in the snapshot", func() bool {
		page, _ := d.uiPageData(0)
		return page.CountPaused == 1
	})
	page, err := d.uiPageData(0)
	if err != nil {
		t.Fatal(err)
	}
	if page.CountAll != 2 || page.CountRunning != 1 || page.CountCompleted != 2 || page.CountActive+page.CountInactive != 2 {
		t.Fatalf("counts: all=%d running=%d completed=%d active=%d inactive=%d", page.CountAll, page.CountRunning, page.CountCompleted, page.CountActive, page.CountInactive)
	}
	if len(page.TrackerHosts) != 1 || page.TrackerHosts[0].Name != "" || page.TrackerHosts[0].Count != 2 {
		t.Fatalf("tracker hosts = %+v, want the two torrents without a working tracker", page.TrackerHosts)
	}
	if got := uiTrackerHost("udp://tracker.example.org:6969/announce"); got != "tracker.example.org" {
		t.Fatalf("uiTrackerHost = %q", got)
	}
}

// TestUIPeersAndFilePriorities covers the Peers tab (ban, invalid add) and
// the Files tab's bulk priority (several index fields in one post).
func TestUIPeersAndFilePriorities(t *testing.T) {
	d := newTestDaemon(t)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	h := uiTestTorrent(t, d, "a.bin")

	if ok, msg := uiPost(t, srv, "/ui/peers", url.Values{"hash": {h}, "op": {"ban"}, "peers": {"203.0.113.9:6881"}}); !ok {
		t.Fatalf("ban: %s", msg)
	}
	if ok, _ := uiPost(t, srv, "/ui/peers", url.Values{"hash": {h}, "op": {"add"}, "peers": {"not-an-address"}}); ok {
		t.Fatal("adding a peer without a port must fail")
	}
	if ok, _ := uiPost(t, srv, "/ui/peers", url.Values{"hash": {h}, "op": {"add"}}); ok {
		t.Fatal("adding no peer must fail")
	}

	if ok, msg := uiPost(t, srv, "/ui/file-priority", url.Values{"hash": {h}, "index": {"0"}, "priority": {"6"}}); !ok {
		t.Fatalf("file priority: %s", msg)
	}
	d.mu.Lock()
	_, m := d.findLocked(h)
	prios := append([]int(nil), m.FilePriorities...)
	d.mu.Unlock()
	if len(prios) == 0 || prios[0] != 6 {
		t.Fatalf("file priorities = %v, want the first at 6", prios)
	}
}
