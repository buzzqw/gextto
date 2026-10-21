package gextto

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStaticAssetsAreCacheable checks the validator and the cache lifetime of
// the shared CSS/JS: with the build version the browser may keep the file for
// a year, and a repeat request is answered with a bodyless 304.
func TestStaticAssetsAreCacheable(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	response, err := http.Get(server.URL + "/static/v2.css?v=1943")
	if err != nil {
		t.Fatalf("GET versioned asset: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("versioned asset -> %d", response.StatusCode)
	}
	if got := response.Header.Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("Cache-Control = %q, want an immutable lifetime", got)
	}
	etag := response.Header.Get("ETag")
	if etag == "" {
		t.Fatal("static asset has no ETag")
	}

	request, _ := http.NewRequest(http.MethodGet, server.URL+"/static/v2.css?v=1943", nil)
	request.Header.Set("If-None-Match", etag)
	revalidated, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("revalidate asset: %v", err)
	}
	revalidated.Body.Close()
	if revalidated.StatusCode != http.StatusNotModified {
		t.Fatalf("revalidation -> %d, want 304", revalidated.StatusCode)
	}

	// A URL without the version (old bookmark or old page) stays safe: a short
	// lifetime and the same validator.
	plain, err := http.Get(server.URL + "/static/v2.css")
	if err != nil {
		t.Fatalf("GET plain asset: %v", err)
	}
	plain.Body.Close()
	if got := plain.Header.Get("Cache-Control"); strings.Contains(got, "immutable") {
		t.Fatalf("an unversioned asset must not be immutable: %q", got)
	}
	if plain.Header.Get("ETag") != etag {
		t.Fatal("the ETag must not depend on the query string")
	}
}

// TestShellVersionsStaticAssets checks the page points the browser at the
// versioned asset URLs, which is what makes the long cache safe.
func TestShellVersionsStaticAssets(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/?view=dashboard", nil)
	if code != http.StatusOK {
		t.Fatalf("dashboard -> %d", code)
	}
	for _, want := range []string{
		`/static/gextto-ui.css?v=`, `/static/v2.css?v=`,
		`/static/htmx.min.js?v=`, `static/v2-core.js?v=`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("page is missing the versioned asset %q", want)
		}
	}
}

// TestCompressedStaticAssetThroughTheServer checks the production wrapper:
// the compressed asset decodes to the embedded file.
func TestCompressedStaticAssetThroughTheServer(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(compressResponses(Router(state)))
	t.Cleanup(server.Close)

	request, _ := http.NewRequest(http.MethodGet, server.URL+"/static/v2.css?v=1943", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET compressed asset: %v", err)
	}
	defer response.Body.Close()
	if response.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", response.Header.Get("Content-Encoding"))
	}
	reader, err := gzip.NewReader(response.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(content), "Gextto UI") {
		t.Fatal("the compressed asset does not decode to the stylesheet")
	}
	if len(content) < 50000 {
		t.Fatalf("decoded stylesheet looks truncated: %d bytes", len(content))
	}
}
