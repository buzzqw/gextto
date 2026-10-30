package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestFetchTorrentSourceShapes covers the three shapes an indexer hands out for
// one release: the .torrent bytes, a magnet link as the body, and a redirect to
// a magnet instead of a file. The last one used to fail with
// `unsupported protocol scheme "magnet"`, losing the release.
func TestFetchTorrentSourceShapes(t *testing.T) {
	const magnet = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Test&tr=udp%3A%2F%2Ftracker.test%3A80"
	torrent := []byte("d4:infod4:name4:testee")

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, magnet, http.StatusFound)
		case "/redirect-temporary":
			http.Redirect(w, r, magnet, http.StatusTemporaryRedirect)
		case "/chain":
			// Two hops: the magnet is the second redirect, so following a
			// normal chain must keep working.
			http.Redirect(w, r, srv.URL+"/redirect", http.StatusMovedPermanently)
		case "/body":
			_, _ = io.WriteString(w, "\n  "+magnet+"\n")
		case "/file":
			_, _ = w.Write(torrent)
		case "/html":
			_, _ = io.WriteString(w, "<html><body>login</body></html>")
		case "/empty":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	d := &Daemon{}
	cases := []struct {
		path       string
		wantMagnet string
		wantData   []byte
		wantErr    bool
	}{
		{path: "/redirect", wantMagnet: magnet},
		{path: "/redirect-temporary", wantMagnet: magnet},
		{path: "/chain", wantMagnet: magnet},
		{path: "/body", wantMagnet: magnet},
		{path: "/file", wantData: torrent},
		{path: "/html", wantErr: true},
		{path: "/empty", wantErr: true},
		{path: "/missing", wantErr: true},
	}
	for _, tc := range cases {
		data, magnet, err := d.fetchTorrentSource(srv.URL + tc.path)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s: expected an error, got data %q and magnet %q", tc.path, data, magnet)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tc.path, err)
			continue
		}
		if magnet != tc.wantMagnet {
			t.Errorf("%s: magnet = %q, want %q", tc.path, magnet, tc.wantMagnet)
		}
		if !bytes.Equal(data, tc.wantData) {
			t.Errorf("%s: data = %q, want %q", tc.path, data, tc.wantData)
		}
	}
}

// TestQbitSearchPrunesOrphanJobs: a client that never calls search/delete must
// not pin its results in memory. A new search drops the finished jobs past the
// TTL and keeps the recent ones and the running ones.
func TestQbitSearchPrunesOrphanJobs(t *testing.T) {
	now := time.Now()
	d := &Daemon{qbitSearch: map[int]*qbitSearchJob{
		1: {status: "Stopped", finishedAt: now.Add(-2 * qbitSearchJobTTL)},
		2: {status: "Stopped", finishedAt: now.Add(-qbitSearchJobTTL / 2)},
		3: {status: "Running"},
		// A job stopped by a client that never finished it: no timestamp means
		// the goroutine still has to write it, so it is not dropped yet.
		4: {status: "Stopped"},
	}}

	d.pruneQbitSearchLocked(now)

	if _, ok := d.qbitSearch[1]; ok {
		t.Error("the orphaned job was kept")
	}
	for _, id := range []int{2, 3, 4} {
		if _, ok := d.qbitSearch[id]; !ok {
			t.Errorf("job %d was dropped", id)
		}
	}
}

// TestFetchTorrentSourceRefusesNonMagnetSchemes: behind a redirect only a
// magnet is a torrent. Any other scheme is refused and named in the error,
// instead of being handed to the magnet parser as if it were one.
func TestFetchTorrentSourceRefusesNonMagnetSchemes(t *testing.T) {
	targets := []string{
		"file:///etc/passwd",
		"ftp://tracker.test/file.torrent",
		"data:text/plain;base64,SGVsbG8=",
	}
	for _, target := range targets {
		t.Run(target[:4], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target, http.StatusFound)
			}))
			defer server.Close()

			d := &Daemon{}
			data, magnet, err := d.fetchTorrentSource(server.URL + "/dls/1")
			if err == nil {
				t.Fatalf("redirect to %q was accepted (data %q, magnet %q)", target, data, magnet)
			}
			if magnet != "" {
				t.Fatalf("redirect to %q was returned as a magnet: %q", target, magnet)
			}
			if !errors.Is(err, errNotATorrent) {
				t.Fatalf("error = %v, want errNotATorrent", err)
			}
		})
	}
}

// TestFetchTorrentSourceStopsRunawayRedirects: a link that redirects to itself
// must end in an error, not in an endless follow.
func TestFetchTorrentSourceStopsRunawayRedirects(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Redirect(w, r, "/loop", http.StatusFound)
	}))
	defer server.Close()

	d := &Daemon{}
	if _, _, err := d.fetchTorrentSource(server.URL + "/loop"); err == nil {
		t.Fatal("a redirect loop returned no error")
	}
	if got := atomic.LoadInt32(&hits); got > maxTorrentRedirects+1 {
		t.Fatalf("the client made %d requests, want at most %d", got, maxTorrentRedirects+1)
	}
}

// TestFetchTorrentSourceKeepsTheMagnetTrackers: the link is returned byte for
// byte. Re-encoding the query would split or drop the trackers (`tr=`) and the
// torrent would arrive with none.
func TestFetchTorrentSourceKeepsTheMagnetTrackers(t *testing.T) {
	const magnet = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567" +
		"&dn=Some%20Release%20%5B1080p%5D" +
		"&tr=udp%3A%2F%2Ftracker.one%3A1337%2Fannounce" +
		"&tr=udp%3A%2F%2Ftracker.two%3A6969%2Fannounce" +
		"&tr=https%3A%2F%2Ftracker.three%2Fannounce"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, magnet, http.StatusFound)
	}))
	defer server.Close()

	d := &Daemon{}
	_, got, err := d.fetchTorrentSource(server.URL + "/dls/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != magnet {
		t.Fatalf("magnet changed in transit:\n got %q\nwant %q", got, magnet)
	}
}

// TestFetchTorrentSourceSendsTheStoredCookies: a private tracker hands out the
// .torrent only to a session cookie, and the operator stores that cookie in the
// settings. The page and the API share this fetch, so the jar is attached here.
func TestFetchTorrentSourceSendsTheStoredCookies(t *testing.T) {
	payload := []byte("d4:infod4:name4:testee")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie("session"); err != nil || cookie.Value != "abc123" {
			http.Error(w, "no session", http.StatusForbidden)
			return
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	d := standaloneTestDaemon(t, "", true)
	raw, err := json.Marshal([]storedCookie{{Name: "session", Value: "abc123", Domain: "127.0.0.1"}})
	if err != nil {
		t.Fatal(err)
	}
	d.opts.Settings.Set(cookiesSettingKey, string(raw))

	data, magnet, err := d.fetchTorrentSource(server.URL + "/dls/private")
	if err != nil {
		t.Fatalf("the stored cookie was not sent: %v", err)
	}
	if magnet != "" || string(data) != string(payload) {
		t.Fatalf("got data %q and magnet %q", data, magnet)
	}
}
