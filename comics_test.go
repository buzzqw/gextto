package gextto

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func openTestComicsDb(t *testing.T) *ComicsDb {
	t.Helper()
	path := filepath.Join(t.TempDir(), "comics.db")
	db, err := OpenComicsDb(path)
	if err != nil {
		t.Fatalf("open comics db: %v", err)
	}
	return db
}

func TestCleansComicSearchTitle(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Poison Ivy #41 (2026)", "Poison Ivy"},
		{"Spawn #373 (2026)", "Spawn"},
		{"Batman (2026)", "Batman"},
		{"The Amazing Spider-Man #1", "The Amazing Spider Man"},
		{"", ""},
	}
	for _, testCase := range cases {
		if got := cleanSearchTitle(testCase.input); got != testCase.want {
			t.Errorf("cleanSearchTitle(%q) = %q, want %q", testCase.input, got, testCase.want)
		}
	}
}

func TestHTTPDownloadTagIsTrimmedAndReportedWhenMissing(t *testing.T) {
	id := registerHTTPDownload("Test Comic", "http", "https://example.test/file.cbz")
	if !SetHTTPDownloadTag(id, "  fumetti  ") {
		t.Fatal("set tag should succeed for a registered download")
	}
	var download *ComicDownload
	for _, item := range HTTPDownloads() {
		if item.ID == id {
			copied := item
			download = &copied
		}
	}
	if download == nil {
		t.Fatal("registered download not found")
	}
	if download.Tag != "fumetti" {
		t.Errorf("tag = %q, want %q", download.Tag, "fumetti")
	}
	if download.URL != "https://example.test/file.cbz" {
		t.Errorf("url = %q", download.URL)
	}
	if SetHTTPDownloadTag("missing-download", "x") {
		t.Error("set tag should fail for a missing download")
	}
	removeHTTPDownload(id)
}

func TestCancellingADownloadRemovesItAndDeletesFilesWhenAsked(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "file.cbz")
	temporary := filepath.Join(dir, "file.part")
	if err := os.WriteFile(destination, []byte("done"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temporary, []byte("part"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := registerHTTPDownload("Cancel Comic", "http", "https://example.test/file.cbz")
	control := installHTTPControl(id, &http.Client{}, "https://example.test/file.cbz", dir, "Cancel Comic")
	control.pathsMu.Lock()
	control.destination = &destination
	control.temporary = &temporary
	control.pathsMu.Unlock()
	if !CancelHTTPDownload(id, true) {
		t.Fatal("cancel should return true")
	}
	for _, item := range HTTPDownloads() {
		if item.ID == id {
			t.Fatal("download should have been removed")
		}
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Error("destination should have been deleted")
	}
	if _, err := os.Stat(temporary); !os.IsNotExist(err) {
		t.Error("temporary should have been deleted")
	}
}

func TestClearFinishedHTTPDownloadsSkipsActiveRows(t *testing.T) {
	finished := registerHTTPDownload("Done Comic", "http", "https://example.test/a")
	active := registerHTTPDownload("Active Comic", "http", "https://example.test/b")
	updateHTTPDownload(finished, func(download *ComicDownload) { download.Status = "completed" })
	updateHTTPDownload(active, func(download *ComicDownload) { download.Status = "downloading" })
	if removed := ClearFinishedHTTPDownloads(); removed < 1 {
		t.Fatalf("clear removed %d downloads, want >= 1", removed)
	}
	remaining := HTTPDownloads()
	for _, item := range remaining {
		if item.ID == finished {
			t.Error("finished download should have been removed")
		}
	}
	foundActive := false
	for _, item := range remaining {
		if item.ID == active {
			foundActive = true
		}
	}
	if !foundActive {
		t.Error("active download should remain")
	}
	removeHTTPDownload(active)
}

func TestMonitoredIssueDoesNotAcceptNewerIssueOrCollection(t *testing.T) {
	issue := issueNumber("Poison Ivy #41 (2026)")
	if issue == nil || *issue != "41" {
		t.Fatalf("issueNumber = %v, want 41", issue)
	}
	if !matchesMonitoredIssue("Poison Ivy #41 (2026)", "Poison Ivy #41 (2026)") {
		t.Error("same issue should match")
	}
	if matchesMonitoredIssue("Poison Ivy #41 (2026)", "Poison Ivy #47 (2026)") {
		t.Error("newer issue should not match")
	}
	if matchesMonitoredIssue("Poison Ivy #41 (2026)", "Poison Ivy Vol. 7 – Amuse-Bouche (TPB) (2026)") {
		t.Error("collection should not match a numbered issue")
	}
	if !matchesMonitoredIssue("Batman (2026)", "Batman Vol. 1 (2026)") {
		t.Error("unnumbered monitor should match a collection")
	}
}

func TestSearchResultsKeepTheSelectedPostMetadata(t *testing.T) {
	html := `
            <article class="post">
              <h2 class="post-title"><a href="/dc/poison-ivy-41-2026/">Poison Ivy #41 (2026)</a></h2>
              <img class="wp-post-image" src="/cover.jpg">
              <time datetime="2026-02-04">February 4, 2026</time>
              <div class="post-info"><p>Digital comic description.</p></div>
              <a href="/tag/poison-ivy-41/">Poison Ivy #41</a>
              <a href="/cat/dc/">DC Comics</a>
            </article>
        `
	posts := parseComicArticles(html, "", "https://getcomics.org/?s=Poison+Ivy")
	if len(posts) != 1 {
		t.Fatalf("posts = %d, want 1", len(posts))
	}
	if posts[0].URL != "https://getcomics.org/dc/poison-ivy-41-2026/" {
		t.Errorf("url = %q", posts[0].URL)
	}
	if posts[0].TagURL != "https://getcomics.org/tag/poison-ivy-41/" {
		t.Errorf("tag_url = %q", posts[0].TagURL)
	}
	if posts[0].Publisher != "DC Comics" {
		t.Errorf("publisher = %q", posts[0].Publisher)
	}
	if posts[0].Description != "Digital comic description." {
		t.Errorf("description = %q", posts[0].Description)
	}
}

func TestDebugWeeklyLinksNetwork(t *testing.T) {
	t.Skip("network test skipped (requires external access)")
}

func TestParseLinksIgnoresInvalidHrefs(t *testing.T) {
	html := `<a href="magnet:?xt=urn:btih:0123456789012345678901234567890123456789">ok</a><a href="http://[bad">x</a><a href="/file.torrent">t</a>`
	links, err := parseComicLinks(html, "https://getcomics.org/post/")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(links.Magnets) != 1 {
		t.Errorf("magnets = %d, want 1", len(links.Magnets))
	}
	if len(links.Torrents) != 1 {
		t.Errorf("torrents = %d, want 1", len(links.Torrents))
	}
}

func TestParseLinksSkipsHelpPageAndKeepsGetComicsFileRedirect(t *testing.T) {
	html := `
            <a href="https://getcomics.info/how-to-download/">how-to download page</a>
            <a href="https://getcomics.org/dls/pixeldrain-token">PIXELDRAIN</a>
            <a href="https://datanodes.to/file/example.cbr">DATANODES</a>
        `
	links, err := parseComicLinks(html, "https://getcomics.org/post/example")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []string{
		"https://getcomics.org/dls/pixeldrain-token",
		"https://datanodes.to/file/example.cbr",
	}
	if len(links.Direct) != len(want) {
		t.Fatalf("direct = %v, want %v", links.Direct, want)
	}
	for index, value := range want {
		if links.Direct[index] != value {
			t.Errorf("direct[%d] = %q, want %q", index, links.Direct[index], value)
		}
	}
}

func TestParseLinksIdentifiesDownloadNowButton(t *testing.T) {
	html := `
            <a href="" title="Download Now">DOWNLOAD NOW</a>
            <a href="/dls/download-now-token">Download Now</a>
            <a href="/dls/download-now-title" title="Download Now">file host</a>
            <a href="https://datanodes.to/file/example.cbr">Alternative download</a>
        `
	links, err := parseComicLinks(html, "https://getcomics.org/post/example")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	wantNow := []string{
		"https://getcomics.org/dls/download-now-token",
		"https://getcomics.org/dls/download-now-title",
	}
	if len(links.DownloadNow) != len(wantNow) {
		t.Fatalf("download_now = %v, want %v", links.DownloadNow, wantNow)
	}
	for index, value := range wantNow {
		if links.DownloadNow[index] != value {
			t.Errorf("download_now[%d] = %q, want %q", index, links.DownloadNow[index], value)
		}
	}
	if len(links.Direct) != 3 {
		t.Errorf("direct = %d, want 3", len(links.Direct))
	}
}

func TestDownloadHTTPResumesAfterTruncatedBody(t *testing.T) {
	payload := make([]byte, 200000)
	for index := range payload {
		payload[index] = byte(index % 251)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	var connections int32
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func(connection net.Conn) {
				defer connection.Close()
				buffer := make([]byte, 4096)
				read, _ := connection.Read(buffer)
				request := strings.ToLower(string(buffer[:read]))
				offset := 0
				for _, line := range strings.Split(request, "\r\n") {
					if strings.HasPrefix(line, "range: bytes=") {
						value := strings.TrimPrefix(line, "range: bytes=")
						value = strings.SplitN(value, "-", 2)[0]
						if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
							offset = parsed
						}
					}
				}
				if offset >= len(payload) {
					_, _ = connection.Write([]byte("HTTP/1.1 416 Range Not Satisfiable\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
					return
				}
				count := atomic.AddInt32(&connections, 1)
				length := len(payload) - offset
				writeEnd := len(payload)
				if count == 1 {
					writeEnd = offset + length/2
				}
				status := "200 OK"
				if offset > 0 {
					status = "206 Partial Content"
				}
				header := fmt.Sprintf("HTTP/1.1 %s\r\nContent-Type: application/octet-stream\r\nContent-Length: %d\r\nAccept-Ranges: bytes\r\nConnection: close\r\n\r\n", status, length)
				_, _ = connection.Write([]byte(header))
				_, _ = connection.Write(payload[offset:writeEnd])
			}(connection)
		}
	}()

	dir := t.TempDir()
	client := &http.Client{Timeout: 15 * time.Second}
	url := fmt.Sprintf("http://%s/file.cbz", listener.Addr().String())
	path, err := DownloadHTTP(client, url, dir, "Resume Test")
	if err != nil {
		t.Fatalf("download should resume and complete: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(payload) {
		t.Fatalf("downloaded %d bytes, want %d", len(got), len(payload))
	}
	for index := range payload {
		if got[index] != payload[index] {
			t.Fatalf("payload mismatch at byte %d", index)
		}
	}
	if _, err := os.Stat(withPartExtension(path)); !os.IsNotExist(err) {
		t.Error(".part file should not exist after completion")
	}
}

func TestUpsertWeeklyFillsMissingLinksAndReportsEligibility(t *testing.T) {
	db := openTestComicsDb(t)
	// Row recorded without links: pack found but torrent not ready yet.
	eligible, err := db.UpsertWeeklyLinks("2026-09-16", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if eligible {
		t.Error("empty links should not be eligible")
	}
	pending, err := db.PendingWeekly()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending = %d, want 0", len(pending))
	}
	// The magnet arrives: the existing row is filled and becomes eligible.
	eligible, err = db.UpsertWeeklyLinks("2026-09-16", "magnet:?xt=urn:btih:0123456789012345678901234567890123456789", "")
	if err != nil {
		t.Fatal(err)
	}
	if !eligible {
		t.Error("magnet should make the row eligible")
	}
	pending, err = db.PendingWeekly()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}
	// After sending it is no longer pending.
	if err := db.MarkWeeklySent("2026-09-16"); err != nil {
		t.Fatal(err)
	}
	pending, err = db.PendingWeekly()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("pending = %d, want 0", len(pending))
	}
}

func TestMonitoredHistoryAndWeeklyRecordsAreIdempotent(t *testing.T) {
	db := openTestComicsDb(t)
	id, err := db.AddMonitored("Example", "tag/example", "2026-01-01", "comics")
	if err != nil {
		t.Fatal(err)
	}
	again, err := db.AddMonitored("Example updated", "tag/example", "2026-02-01", "comics")
	if err != nil {
		t.Fatal(err)
	}
	if again != id {
		t.Errorf("id = %d, want %d", again, id)
	}
	monitored, err := db.ListMonitored(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(monitored) != 1 {
		t.Fatalf("monitored = %d, want 1", len(monitored))
	}
	added, err := db.AddHistory(id, "https://example/post", "Example 001", "magnet:?xt=urn:btih:0123456789012345678901234567890123456789", "")
	if err != nil {
		t.Fatal(err)
	}
	if !added {
		t.Error("first history insert should succeed")
	}
	added, err = db.AddHistory(id, "https://example/post", "Example 001", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if added {
		t.Error("duplicate history insert should be ignored")
	}
	sent, err := db.AlreadySent("https://example/post")
	if err != nil {
		t.Fatal(err)
	}
	if !sent {
		t.Error("post should be marked as already sent")
	}
	added, err = db.AddWeekly("2026-02-01", "", "https://example/weekly")
	if err != nil {
		t.Fatal(err)
	}
	if !added {
		t.Error("first weekly insert should succeed")
	}
	added, err = db.AddWeekly("2026-02-01", "", "https://example/weekly")
	if err != nil {
		t.Fatal(err)
	}
	if added {
		t.Error("duplicate weekly insert should be ignored")
	}
	changed, err := db.SetEnabled(id, false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("set enabled should report a change")
	}
	monitored, err = db.ListMonitored(true)
	if err != nil {
		t.Fatal(err)
	}
	for _, comic := range monitored {
		if comic.ID == id {
			t.Error("disabled comic should not be listed")
		}
	}
	removed, err := db.RemoveMonitored(id)
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Error("remove should report a change")
	}
	removed, err = db.RemoveMonitored(id)
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Error("second remove should report no change")
	}
}

func TestComicsHistoryLimitPrunesBothHistories(t *testing.T) {
	db := openTestComicsDb(t)
	if err := db.SetSetting("comics_history_limit", "2"); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		if _, err := db.AddHistory(0, fmt.Sprintf("https://example/post/%d", i), fmt.Sprintf("Comic %d", i), "", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := db.AddWeekly(fmt.Sprintf("2026-09-%02d", i), "", fmt.Sprintf("https://example/weekly/%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	history, err := db.History(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0]["title"] != "Comic 3" {
		t.Fatalf("comic history = %#v, want newest two rows", history)
	}
	weekly, err := db.Weekly(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(weekly) != 2 || weekly[0]["pack_date"] != "2026-09-03" {
		t.Fatalf("weekly history = %#v, want newest two rows", weekly)
	}
}

func TestParsesPostsAndClassifiesDownloadLinks(t *testing.T) {
	html := `<article class='post'><h2 class='post-title'><a href='/post/example'>Example</a></h2><time datetime='2026-09-19T10:00:00'></time><img src='/cover.jpg'></article><a href='magnet:?xt=urn:btih:0123456789012345678901234567890123456789'>torrent</a><a href='/dlds/1'>MEGA</a><a href='/file.torrent'>torrent file</a><a href='/download/file'>Download now</a>`
	posts := parseComicArticles(html, "2026-01-01", "https://getcomics.org/tag/example/")
	if len(posts) == 0 {
		t.Fatal("expected one post")
	}
	if posts[0].Title != "Example" {
		t.Errorf("title = %q", posts[0].Title)
	}
	if posts[0].Date != "2026-09-19" {
		t.Errorf("date = %q, want 2026-09-19", posts[0].Date)
	}
	links, err := parseComicLinks(html, "https://getcomics.org/post/example")
	if err != nil {
		t.Fatal(err)
	}
	if len(links.Magnets) != 1 {
		t.Errorf("magnets = %d, want 1", len(links.Magnets))
	}
	if len(links.Mega) != 1 {
		t.Errorf("mega = %d, want 1", len(links.Mega))
	}
	if len(links.Torrents) != 1 {
		t.Errorf("torrents = %d, want 1", len(links.Torrents))
	}
	if len(links.Direct) != 1 {
		t.Errorf("direct = %d, want 1", len(links.Direct))
	}
}

func TestMonitoredComicRejectsRemoteTagHost(t *testing.T) {
	db := openTestComicsDb(t)
	if _, err := db.AddMonitored("Example", "https://example.invalid/tag/example", "", ""); err == nil {
		t.Error("remote tag host should be rejected")
	}
}
