package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
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
