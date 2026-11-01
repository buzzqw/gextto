package main

// The native add endpoint takes a magnet, a .torrent URL or a URL that only
// redirects to a magnet: indexarr and Jackett answer `302 Location: magnet:…`
// for a source that has no .torrent to serve, and the release must still be
// added. The reader that understands the redirects is fetchTorrentSource, the
// same one the page and the qBittorrent-compatible API use; these tests keep
// the native endpoint on it.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// postAdd sends one /api/v1/add request with the given source.
func postAdd(t *testing.T, d *Daemon, source string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"magnet": {source}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	d.handleAdd(rec, req)
	return rec
}

// TestAddResolvesASourceThatRedirectsToAMagnet: indexarr answers the search
// result with a 302 to a magnet. Handing that URL to the engine made it follow
// the redirect with a plain HTTP client and fail with `unsupported protocol
// scheme "magnet"`, so the release was lost. This daemon has no engine: the
// request reaching it is the proof that the redirect became a magnet.
func TestAddResolvesASourceThatRedirectsToAMagnet(t *testing.T) {
	const magnet = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567" +
		"&dn=Some%20Release&tr=udp%3A%2F%2Ftracker.one%3A1337%2Fannounce"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, magnet, http.StatusFound)
	}))
	defer server.Close()

	d := standaloneTestDaemon(t, "", true)
	d.opts.DownloadDir = t.TempDir()
	d.opts.LinkDir = t.TempDir()
	rec := postAdd(t, d, server.URL+"/dl/3043135.torrent?apikey=k")
	body := rec.Body.String()
	if strings.Contains(body, "unsupported protocol scheme") {
		t.Fatalf("the magnet redirect went to the engine: %s", body)
	}
	if rec.Code != http.StatusBadRequest || !strings.Contains(body, "session not available") {
		t.Fatalf("status %d, body %s: the redirect was not resolved to a magnet", rec.Code, body)
	}
}

// TestAddKeepsAMagnetAsItIs: a magnet is not fetched, it goes straight on.
func TestAddKeepsAMagnetAsItIs(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.DownloadDir = t.TempDir()
	d.opts.LinkDir = t.TempDir()
	const magnet = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"
	rec := postAdd(t, d, magnet)
	if !strings.Contains(rec.Body.String(), "session not available") {
		t.Fatalf("a magnet did not reach the add path: %s", rec.Body.String())
	}
}

// TestAddRefusesSomethingThatIsNotASource: garbage is refused with a sentence
// that says what the endpoint takes, instead of being handed to the engine.
func TestAddRefusesSomethingThatIsNotASource(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	rec := postAdd(t, d, "just some words")
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	if rec.Code != http.StatusBadRequest || !strings.Contains(body.Error, "not a magnet link") {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body.String())
	}
}
