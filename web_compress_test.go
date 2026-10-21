package gextto

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAcceptsGzip(t *testing.T) {
	cases := map[string]bool{
		"":               false,
		"gzip":           true,
		"deflate, gzip":  true,
		"gzip;q=0.8, br": true,
		"gzip; q=0":      false,
		"gzip;q=0.000":   false,
		"br, deflate":    false,
		"GZIP":           true,
	}
	for header, want := range cases {
		if got := acceptsGzip(header); got != want {
			t.Fatalf("acceptsGzip(%q) = %v, want %v", header, got, want)
		}
	}
}

func TestCompressibleType(t *testing.T) {
	for _, contentType := range []string{
		"text/html; charset=utf-8", "text/css", "application/json",
		"application/javascript", "image/svg+xml", "application/manifest+json",
	} {
		if !compressibleType(contentType) {
			t.Fatalf("%q should be compressible", contentType)
		}
	}
	for _, contentType := range []string{
		"image/png", "image/jpeg", "application/zip", "video/mp4",
		"text/event-stream", "",
	} {
		if compressibleType(contentType) {
			t.Fatalf("%q must not be compressed", contentType)
		}
	}
}

func TestCompressResponsesGzipsText(t *testing.T) {
	const body = "pagina html ripetuta più volte per superare la soglia. "
	handler := compressResponses(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Length", "4096")
		_, _ = io.WriteString(w, strings.Repeat(body, 100))
	}))
	request := httptest.NewRequest(http.MethodGet, "/?view=dashboard", nil)
	request.Header.Set("Accept-Encoding", "gzip, deflate")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if got := recorder.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := recorder.Header().Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Fatalf("Vary = %q, want Accept-Encoding", got)
	}
	if got := recorder.Header().Get("Content-Length"); got != "" {
		t.Fatalf("Content-Length = %q, it must be dropped when compressing", got)
	}
	reader, err := gzip.NewReader(recorder.Body)
	if err != nil {
		t.Fatalf("gzip body: %v", err)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}
	if string(decoded) != strings.Repeat(body, 100) {
		t.Fatal("the decompressed body differs from the original")
	}
}

func TestCompressResponsesLeavesOtherBodiesAlone(t *testing.T) {
	small := compressResponses(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Length", "5")
		_, _ = io.WriteString(w, "short")
	}))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	small.ServeHTTP(recorder, request)
	if recorder.Header().Get("Content-Encoding") != "" {
		t.Fatal("a tiny response must not be compressed")
	}

	stream := compressResponses(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Repeat("data: x\n\n", 200))
	}))
	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/logs/stream", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	stream.ServeHTTP(recorder, request)
	if recorder.Header().Get("Content-Encoding") != "" {
		t.Fatal("Server-Sent Events must not be compressed")
	}

	rangeRequest := compressResponses(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, strings.Repeat("x", 4096))
	}))
	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/static/v2.css", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	request.Header.Set("Range", "bytes=0-99")
	rangeRequest.ServeHTTP(recorder, request)
	if recorder.Header().Get("Content-Encoding") != "" {
		t.Fatal("a partial response must not be compressed")
	}
}
