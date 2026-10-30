package gextto

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
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

// TestMonitoredSeriesFollowsLaterIssues locks in that a numbered monitor starts
// the series at that issue: the number itself is not a one-shot, so new issues
// are found by the periodic check instead of only by a manual search.
func TestMonitoredSeriesFollowsLaterIssues(t *testing.T) {
	issue := issueNumber("Poison Ivy #41 (2026)")
	if issue == nil || *issue != "41" {
		t.Fatalf("issueNumber = %v, want 41", issue)
	}
	if !matchesMonitoredSeries("Poison Ivy #41 (2026)", "Poison Ivy #41 (2026)") {
		t.Error("same issue should match")
	}
	if !matchesMonitoredSeries("Poison Ivy #41 (2026)", "Poison Ivy #47 (2026)") {
		t.Error("a later issue of the same series should match")
	}
	if matchesMonitoredSeries("Poison Ivy #41 (2026)", "Poison Ivy #40 (2026)") {
		t.Error("an older issue should not match")
	}
	if matchesMonitoredSeries("Poison Ivy #41 (2026)", "Poison Ivy Vol. 7 – Amuse-Bouche (TPB) (2026)") {
		t.Error("collection should not match a numbered issue")
	}
	if matchesMonitoredSeries("Poison Ivy #41 (2026)", "Spawn #41 (2026)") {
		t.Error("an issue of another series should not match")
	}
	if !matchesMonitoredSeries("Batman (2026)", "Batman Vol. 1 (2026)") {
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

func TestDownloadHTTPRejectsTorrentPayloadWithoutRetrying(t *testing.T) {
	// This is a minimal bencoded metainfo document. GetComics serves these from
	// opaque `/dls/` URLs as application/octet-stream, so the content type alone
	// cannot distinguish it from a comic archive.
	payload := []byte("d4:infod4:name1:xee")
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		attempts++
		writer.Header().Set("Content-Type", "application/octet-stream")
		_, _ = writer.Write(payload)
	}))
	defer server.Close()

	dir := t.TempDir()
	_, err := DownloadHTTP(server.Client(), server.URL+"/opaque-download", dir, "Weekly Pack 2026-09-23")
	if !errors.Is(err, errComicDownloadTorrent) {
		t.Fatalf("DownloadHTTP error = %v, want torrent payload error", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 for a permanent content error", attempts)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("download directory contains %v, want no mislabeled torrent", entries)
	}
}

// TestDownloadHTTPNamesTheArchiveAfterItsBytes: the file name comes from the
// post title, which does not say whether the download is a CBZ (a ZIP) or a
// CBR (a RAR). A RAR stored as ".cbz" is refused by Komga and Kavita, so the
// extension follows the magic bytes of what arrived.
func TestDownloadHTTPNamesTheArchiveAfterItsBytes(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
		want    string
	}{
		{name: "cbz", payload: append([]byte("PK\x03\x04"), make([]byte, 2048)...), want: ".cbz"},
		{name: "cbr", payload: append([]byte("Rar!\x1a\x07\x00"), make([]byte, 2048)...), want: ".cbr"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/octet-stream")
				_, _ = writer.Write(tc.payload)
			}))
			defer server.Close()

			path, err := DownloadHTTP(server.Client(), server.URL+"/dls/opaque", t.TempDir(), "Batman: The Long Halloween #1 (2024)")
			if err != nil {
				t.Fatalf("DownloadHTTP: %v", err)
			}
			if got := filepath.Ext(path); got != tc.want {
				t.Fatalf("saved as %q, want the %q extension", path, tc.want)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("the finished comic is missing: %v", err)
			}
		})
	}
}

func TestDownloadTorrentFileRejectsNonTorrentPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/octet-stream")
		_, _ = writer.Write([]byte("this is not bencoded torrent metainfo"))
	}))
	defer server.Close()

	_, err := DownloadTorrentFile(server.Client(), server.URL+"/torrent", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "valid torrent metainfo") {
		t.Fatalf("DownloadTorrentFile error = %v, want invalid metainfo error", err)
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

func TestWeeklyPackInDownloadListRecognisesTheTorrentName(t *testing.T) {
	date := "2026-10-14"
	if got := weeklyPackName(date); got != "2026.10.14 Weekly Pack" {
		t.Fatalf("weeklyPackName = %q", got)
	}
	if !weeklyPackInDownloadList(&mockTorrentEngine{torrents: []models.TorrentView{{Name: " 2026.10.14 Weekly Pack "}}}, date) {
		t.Fatal("the pack in the engine list should be recognised")
	}
	if weeklyPackInDownloadList(&mockTorrentEngine{torrents: []models.TorrentView{{Name: "2026.10.07 Weekly Pack"}}}, date) {
		t.Fatal("another week's pack must not match")
	}
}

func TestWeeklyPackDownloadedReadsGexttoHistory(t *testing.T) {
	db := openTestComicsDb(t)
	defer db.Close()
	date := "2026-10-14"
	if _, done := weeklyPackDownloaded(db, date); done {
		t.Fatal("an unknown pack is not in the history")
	}
	// A pack that was queued but whose weekly row lost sent_at is still in the
	// history through the torrent it queued.
	if err := db.AddTorrent("84714ea099ed5feb13a3636f6f0686c89e0e4e71", "weekly:"+date, "Weekly Pack "+date, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	when, done := weeklyPackDownloaded(db, date)
	if !done || when == "" {
		t.Fatal("the queued torrent should count as already downloaded")
	}
	if err := db.MarkWeeklySent(date); err != nil {
		t.Fatal(err)
	}
	if _, done := weeklyPackDownloaded(db, date); !done {
		t.Fatal("a sent pack should count as already downloaded")
	}
	// The day shown in the log is the date part of the stored timestamp.
	if got := weeklyDownloadDay("2026-10-23 14:25:19"); got != "2026-10-23" {
		t.Fatalf("weeklyDownloadDay = %q", got)
	}
}

func TestQueueWeeklyPackSkipsAPackAlreadyDownloadedEvenWithoutItsFolder(t *testing.T) {
	db := openTestComicsDb(t)
	defer db.Close()
	date := "2026-10-14"
	if _, err := db.UpsertWeeklyLinks(date, "magnet:?xt=urn:btih:0123456789012345678901234567890123456789", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkWeeklySent(date); err != nil {
		t.Fatal(err)
	}
	// No pack folder on disk: the history alone must be enough. The client,
	// notifier and main database are nil on purpose, so a download attempt
	// would fail the test.
	if !queueWeeklyPack(db, nil, &mockTorrentEngine{}, nil, t.TempDir(), nil, nil, date, "magnet:?xt=urn:btih:0123456789012345678901234567890123456789", "", "", "test") {
		t.Fatal("a pack already in the history should count as handled")
	}
}

func TestQueueWeeklyPackSkipsAPackAlreadyInTheDownloadList(t *testing.T) {
	db := openTestComicsDb(t)
	defer db.Close()
	date := "2026-10-14"
	engine := &mockTorrentEngine{torrents: []models.TorrentView{{Name: weeklyPackName(date)}}}
	if !queueWeeklyPack(db, nil, engine, nil, t.TempDir(), nil, nil, date, "magnet:?xt=urn:btih:0123456789012345678901234567890123456789", "", "", "test") {
		t.Fatal("a pack already in the download list should count as handled")
	}
}

func TestWeeklyLinksReportsTheLinksAlreadyRecorded(t *testing.T) {
	db := openTestComicsDb(t)
	defer db.Close()
	if _, _, known, err := db.WeeklyLinks("2026-10-14"); err != nil || known {
		t.Fatalf("unknown pack: known=%v err=%v", known, err)
	}
	if _, err := db.UpsertWeeklyLinks("2026-10-14", "", ""); err != nil {
		t.Fatal(err)
	}
	magnet, torrent, known, err := db.WeeklyLinks("2026-10-14")
	if err != nil || !known || magnet != "" || torrent != "" {
		t.Fatalf("recorded without links: magnet=%q torrent=%q known=%v err=%v", magnet, torrent, known, err)
	}
	if _, err := db.UpsertWeeklyLinks("2026-10-14", "magnet:?xt=urn:btih:0123456789012345678901234567890123456789", "https://example/t.torrent"); err != nil {
		t.Fatal(err)
	}
	magnet, torrent, _, err = db.WeeklyLinks("2026-10-14")
	if err != nil || magnet == "" || torrent != "https://example/t.torrent" {
		t.Fatalf("links not recorded: magnet=%q torrent=%q err=%v", magnet, torrent, err)
	}
}

func TestComicsCycleLogLinesAreReadable(t *testing.T) {
	var captured bytes.Buffer
	logging.SetOutput(&captured)
	defer logging.SetOutput(os.Stdout)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tag/test-comic/":
			fmt.Fprint(w, `<html><body><article class="post">
  <h1 class="post-title"><a href="/test-comic-1/">Test Comic #1 (2026)</a></h1>
  <time datetime="2026-10-20">2026-10-20</time>
</article></body></html>`)
		case "/test-comic-1/":
			fmt.Fprint(w, `<html><body><a href="/test-comic-1.cbz">Download</a></body></html>`)
		case "/test-comic-1.cbz":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("comic pages"))
		default:
			fmt.Fprint(w, `<html><body></body></html>`)
		}
	}))
	defer server.Close()

	db := openTestComicsDb(t)
	defer db.Close()
	if _, err := db.AddMonitored("Test Comic #1 (2026)", "/tag/test-comic/", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddMonitored("Spawn #9 (2026)", "/tag/spawn/", "", ""); err != nil {
		t.Fatal(err)
	}
	client := &GetComicsClient{client: server.Client(), baseURL: server.URL}
	downloadDir := t.TempDir()
	downloaded, err := RunComicsCycle(context.Background(), db, client, NewNotifier(), downloadDir, &mockTorrentEngine{}, nil, &Config{})
	if err != nil {
		t.Fatalf("comics cycle: %v", err)
	}
	if downloaded != 1 {
		t.Fatalf("downloaded = %d, want 1", downloaded)
	}
	logText := captured.String()
	for _, want := range []string{
		"📚 Checking 2 monitored comics on GetComics",
		"📥 Downloading «Test Comic #1 (2026)» — new comic on GetComics",
		"«Spawn #9 (2026)»: nothing new on GetComics",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("missing %q in the log:\n%s", want, logText)
		}
	}
	for _, unwanted := range []string{"comics:", " · ", "queued:"} {
		if strings.Contains(logText, unwanted) {
			t.Fatalf("the log still contains the technical style %q:\n%s", unwanted, logText)
		}
	}

	// The next run finds the same post already in the history and says so.
	offset := captured.Len()
	downloaded, err = RunComicsCycle(context.Background(), db, client, NewNotifier(), downloadDir, &mockTorrentEngine{}, nil, &Config{})
	if err != nil {
		t.Fatalf("second comics cycle: %v", err)
	}
	if downloaded != 0 {
		t.Fatalf("second run downloaded = %d, want 0", downloaded)
	}
	if !strings.Contains(captured.String()[offset:], "«Test Comic #1 (2026)» is already up to date") {
		t.Fatalf("missing the already-up-to-date line:\n%s", captured.String()[offset:])
	}
}

// TestComicsCycleDownloadsLaterIssuesOfAMonitoredSeries is the user-facing
// case: a monitor named after one issue used to wait for that issue only, so
// the periodic check found nothing while a manual search found the new issues.
func TestComicsCycleDownloadsLaterIssuesOfAMonitoredSeries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tag/test-comic/":
			fmt.Fprint(w, `<html><body>
<article class="post">
  <h1 class="post-title"><a href="/test-comic-1/">Test Comic #1 (2026)</a></h1>
  <time datetime="2026-10-01">2026-10-01</time>
</article>
<article class="post">
  <h1 class="post-title"><a href="/test-comic-2/">Test Comic #2 (2026)</a></h1>
  <time datetime="2026-10-20">2026-10-20</time>
</article>
</body></html>`)
		case "/test-comic-2/":
			fmt.Fprint(w, `<html><body><a href="/test-comic-2.cbz">Download</a></body></html>`)
		case "/test-comic-2.cbz":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("comic pages"))
		default:
			fmt.Fprint(w, `<html><body></body></html>`)
		}
	}))
	defer server.Close()

	db := openTestComicsDb(t)
	defer db.Close()
	id, err := db.AddMonitored("Test Comic #1 (2026)", "/tag/test-comic/", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// The first issue is already downloaded: only the later one is new.
	if _, err := db.AddHistory(id, server.URL+"/test-comic-1/", "Test Comic #1 (2026)", "", ""); err != nil {
		t.Fatal(err)
	}
	client := &GetComicsClient{client: server.Client(), baseURL: server.URL}
	downloadDir := t.TempDir()
	downloaded, err := RunComicsCycle(context.Background(), db, client, NewNotifier(), downloadDir, &mockTorrentEngine{}, nil, &Config{})
	if err != nil {
		t.Fatalf("comics cycle: %v", err)
	}
	if downloaded != 1 {
		t.Fatalf("downloaded = %d, want 1 (the later issue)", downloaded)
	}
	sent, err := db.AlreadySent(server.URL + "/test-comic-2/")
	if err != nil {
		t.Fatal(err)
	}
	if !sent {
		t.Fatal("the later issue was not recorded in the comics history")
	}
}

// comicTestTorrentEngine records the magnets handed to it, so a test can see
// that a weekly pack was queued.
type comicTestTorrentEngine struct {
	mockTorrentEngine
	added []string
}

func (e *comicTestTorrentEngine) AddWithPath(magnet string, cfg *Config, preferredPath *string) (bool, error) {
	e.added = append(e.added, magnet)
	return true, nil
}

// TestRetryPendingWeeklyPacksTakesThePackWhenLinksAppear covers the weekly pack
// that GetComics publishes before its download links: the comics check is
// weekly, so without the retry the pack waited for the next full check.
func TestRetryPendingWeeklyPacksTakesThePackWhenLinksAppear(t *testing.T) {
	const packDate = "2026-10-21"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/blog/"+packDate+"-weekly-pack/" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `<html><body><a href="magnet:?xt=urn:btih:8fd17ab42ed591c4cda1e92a8657dd9b13981411&amp;dn=2026.10.21%20Weekly%20Pack">MAGNET</a></body></html>`)
	}))
	defer server.Close()

	db := openTestComicsDb(t)
	defer db.Close()
	if err := db.SetSetting("weekly_enabled", "yes"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting("weekly_from_date", "2026-09-07"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertWeeklyLinks(packDate, "", ""); err != nil {
		t.Fatal(err)
	}
	mainDB, err := OpenDatabase(filepath.Join(t.TempDir(), "gextto.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer mainDB.Close()
	engine := &comicTestTorrentEngine{}
	cfg := &Config{DataDir: t.TempDir(), DownloadDir: t.TempDir()}
	client := &GetComicsClient{client: server.Client(), baseURL: server.URL}

	retryPendingWeeklyPacks(context.Background(), cfg, mainDB, db, NewNotifier(), engine, client)

	sent, err := db.WeeklySent(packDate)
	if err != nil {
		t.Fatal(err)
	}
	if !sent {
		t.Fatal("the pack with a link now available was not marked as sent")
	}
	if len(engine.added) != 1 {
		t.Fatalf("magnets queued = %d, want 1", len(engine.added))
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

func TestParsesGetComicsTorrentRedirectAsTorrent(t *testing.T) {
	html := `<a href="/dls/opaque-torrent-token" title="TORRENT">TORRENT</a><a href="magnet:?xt=urn:btih:0123456789012345678901234567890123456789">MAGNET LINK</a>`
	links, err := parseComicLinks(html, "https://getcomics.org/other-comics/2026-09-23-weekly-pack/")
	if err != nil {
		t.Fatal(err)
	}
	if len(links.Torrents) != 1 || links.Torrents[0] != "https://getcomics.org/dls/opaque-torrent-token" {
		t.Errorf("torrents = %#v, want opaque torrent redirect", links.Torrents)
	}
	if len(links.Direct) != 0 {
		t.Errorf("direct = %#v, torrent redirect must not be a direct comic download", links.Direct)
	}
	if len(links.Magnets) != 1 {
		t.Errorf("magnets = %#v, want magnet link", links.Magnets)
	}
}

func TestWeeklyPackLinksSkipsIndividualComicPages(t *testing.T) {
	links := weeklyPackLinks(ComicLinks{
		Magnets: []string{"magnet:?xt=urn:btih:0123456789012345678901234567890123456789"},
		Direct: []string{
			"https://getcomics.org/dc/adventures-of-superman-book-of-el-12-2026/",
			"https://getcomics.org/dls/pack-download-token",
			"https://files.example/weekly-pack.cbz",
		},
		DownloadNow: []string{"https://getcomics.org/marvel/example-1-2026/"},
	})
	if len(links.Direct) != 2 {
		t.Fatalf("direct = %#v, want only pack download candidates", links.Direct)
	}
	if links.Direct[0] != "https://getcomics.org/dls/pack-download-token" || links.Direct[1] != "https://files.example/weekly-pack.cbz" {
		t.Errorf("direct = %#v", links.Direct)
	}
	if len(links.DownloadNow) != 0 {
		t.Errorf("download_now = %#v, want no individual comic page", links.DownloadNow)
	}
	if len(links.Magnets) != 1 {
		t.Errorf("magnets = %#v, must be preserved", links.Magnets)
	}
}

func TestMonitoredComicRejectsRemoteTagHost(t *testing.T) {
	db := openTestComicsDb(t)
	if _, err := db.AddMonitored("Example", "https://example.invalid/tag/example", "", ""); err == nil {
		t.Error("remote tag host should be rejected")
	}
}
