package gextto

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestReadLimitedBodyWithinLimit(t *testing.T) {
	data, err := readLimitedBody(strings.NewReader("hello"), 5)
	if err != nil {
		t.Fatalf("readLimitedBody within limit: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("body = %q, want hello", string(data))
	}
}

func TestReadLimitedBodyOverLimit(t *testing.T) {
	if _, err := readLimitedBody(strings.NewReader("hello!"), 5); err == nil {
		t.Fatal("readLimitedBody must reject a body larger than the limit")
	}
}

func TestReadLimitedBodyHintCapsUntrustedPreallocation(t *testing.T) {
	payload, err := readLimitedBodyHint(strings.NewReader("x"), 8<<20, 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	if cap(payload) > 1<<20 {
		t.Fatalf("a one-byte body reserved %d bytes from an untrusted size hint", cap(payload))
	}
}

func TestReadLimitedBodyIntoReusesTheBuffer(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 100)
	first, err := readLimitedBodyInto(make([]byte, 0, 64), bytes.NewReader(data), 1000)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if len(first) != len(data) {
		t.Fatalf("first read length = %d, want %d", len(first), len(data))
	}
	capacity := cap(first)
	second, err := readLimitedBodyInto(first, strings.NewReader("hello"), 1000)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if string(second) != "hello" {
		t.Fatalf("second body = %q, want hello", second)
	}
	if cap(second) != capacity {
		t.Fatalf("second read reallocated: capacity %d -> %d", capacity, cap(second))
	}
	if _, err := readLimitedBodyInto(second, strings.NewReader("hello!"), 5); err == nil {
		t.Fatal("readLimitedBodyInto must reject a body larger than the limit")
	}
}

// unsizedReader hides the length of the source, like a chunked HTTP response.
type unsizedReader struct{ r io.Reader }

func (u unsizedReader) Read(p []byte) (int, error) { return u.r.Read(p) }

// TestReadLimitedBodyGrowsInFewSteps pins that an unknown-size body is not
// copied once per doubling from a tiny buffer: a cycle reads tens of these.
func TestReadLimitedBodyGrowsInFewSteps(t *testing.T) {
	data := make([]byte, 1<<20)
	var readErr error
	allocs := testing.AllocsPerRun(5, func() {
		_, readErr = readLimitedBody(unsizedReader{bytes.NewReader(data)}, int64(len(data)))
	})
	if readErr != nil {
		t.Fatalf("readLimitedBody: %v", readErr)
	}
	if allocs > 10 {
		t.Fatalf("a 1 MiB body allocated %v times, want at most 10", allocs)
	}
}

func TestCopyLimitedWithinLimit(t *testing.T) {
	var buffer bytes.Buffer
	if err := copyLimited(&buffer, strings.NewReader("hello"), 5); err != nil {
		t.Fatalf("copyLimited within limit: %v", err)
	}
	if buffer.String() != "hello" {
		t.Fatalf("copied = %q, want hello", buffer.String())
	}
}

func TestCopyLimitedOverLimit(t *testing.T) {
	var buffer bytes.Buffer
	if err := copyLimited(&buffer, strings.NewReader("hello!"), 5); err == nil {
		t.Fatal("copyLimited must reject a source larger than the limit")
	}
}

func TestRedirectDropsCredentialsAcrossOrigins(t *testing.T) {
	var apiKey, plexToken, authorization string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey = r.Header.Get("X-Api-Key")
		plexToken = r.Header.Get("X-Plex-Token")
		authorization = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, "ok")
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer origin.Close()

	response, err := HTTPGet(context.Background(), origin.URL, map[string]string{
		"X-Api-Key":     "secret-api-key",
		"X-Plex-Token":  "secret-plex-token",
		"Authorization": "Bearer secret-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if apiKey != "" || plexToken != "" || authorization != "" {
		t.Fatalf("cross-origin redirect received credentials: api_key=%q plex_token=%q authorization=%q", apiKey, plexToken, authorization)
	}
}

func TestSafeRedirectRejectsHTTPSDowngrade(t *testing.T) {
	origin := &url.URL{Scheme: "https", Host: "example.test"}
	target := &url.URL{Scheme: "http", Host: "example.test"}
	req := &http.Request{URL: target, Header: make(http.Header)}
	if err := safeRedirect(req, []*http.Request{{URL: origin}}); err == nil {
		t.Fatal("an HTTPS to HTTP redirect must be refused")
	}
}
