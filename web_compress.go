package gextto

// Response compression for the web interface.
//
// On a slow network the HTML pages and the shared CSS/JS assets dominate the
// load time: the archive page alone is ~470 KB and the assets ~290 KB, all
// served uncompressed. Gzip cuts them to 7–32% of their size, so the interface
// becomes usable on a weak link while the server cost stays negligible (a few
// milliseconds on a page, and only when the client asks for it).

import (
	"compress/gzip"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

const (
	// minCompressBytes skips tiny responses: the gzip header and the extra
	// bookkeeping would cost more than they save.
	minCompressBytes = 1024
	// gzipLevel keeps the transfer size small: the network, not the CPU, is
	// the bottleneck for the interface. The archive page drops from ~473 KB to
	// ~35 KB at this level (BestSpeed would stop at ~50 KB).
	gzipLevel = gzip.DefaultCompression
)

var gzipWriterPool = sync.Pool{New: func() any {
	writer, _ := gzip.NewWriterLevel(io.Discard, gzipLevel)
	return writer
}}

// streamingPaths carry Server-Sent Events: compressing them would require
// flushing on every event and could delay the stream, while their payload is
// already tiny.
func streamingPath(path string) bool {
	switch path {
	case "/api/logs/stream", "/api/notifications/stream":
		return true
	}
	return false
}

// acceptsGzip reports whether the Accept-Encoding header allows gzip. A
// "q=0" (or any q lower than 0.001) means the client refuses it.
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(part, ";")
		if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
			continue
		}
		if params = strings.ReplaceAll(params, " ", ""); params != "" {
			if index := strings.Index(params, "q="); index >= 0 {
				if quality, err := strconv.ParseFloat(params[index+2:], 64); err == nil && quality <= 0 {
					return false
				}
			}
		}
		return true
	}
	return false
}

// compressibleType lists the response types worth compressing. Images (PNG,
// JPEG, WebP), archives and media are already compressed and are left alone.
func compressibleType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	if mediaType == "text/event-stream" {
		return false
	}
	switch {
	case strings.HasPrefix(mediaType, "text/"):
		return true
	case mediaType == "application/json",
		mediaType == "application/javascript",
		mediaType == "application/x-javascript",
		mediaType == "application/manifest+json",
		mediaType == "application/xml",
		mediaType == "application/xhtml+xml",
		mediaType == "image/svg+xml":
		return true
	case strings.HasSuffix(mediaType, "+json"), strings.HasSuffix(mediaType, "+xml"):
		return true
	}
	return false
}

// compressResponses wraps the whole web application. Handlers that already set
// a Content-Encoding, return a body without content (204/206/304) or answer
// with a compressible type below the threshold are passed through untouched.
func compressResponses(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead ||
			r.Header.Get("Range") != "" ||
			streamingPath(r.URL.Path) ||
			!acceptsGzip(r.Header.Get("Accept-Encoding")) {
			next.ServeHTTP(w, r)
			return
		}
		writer := &compressWriter{ResponseWriter: w}
		defer writer.close()
		next.ServeHTTP(writer, r)
	})
}

type compressWriter struct {
	http.ResponseWriter
	gzip    *gzip.Writer
	decided bool
}

func (w *compressWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// addVary appends a header to Vary without duplicating it.
func addVary(header http.Header, value string) {
	for _, existing := range header.Values("Vary") {
		for _, token := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(token), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}

func (w *compressWriter) WriteHeader(status int) {
	if w.decided {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.decided = true
	header := w.Header()
	compress := status >= 200 && status < 300 && status != http.StatusNoContent && status != http.StatusPartialContent && status != http.StatusNotModified
	if compress && header.Get("Content-Encoding") == "" && compressibleType(header.Get("Content-Type")) {
		if length, err := strconv.Atoi(header.Get("Content-Length")); err == nil && length < minCompressBytes {
			compress = false
		}
	}
	if !compress {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	header.Del("Content-Length")
	header.Set("Content-Encoding", "gzip")
	addVary(header, "Accept-Encoding")
	w.ResponseWriter.WriteHeader(status)
	w.gzip = gzipWriterPool.Get().(*gzip.Writer)
	w.gzip.Reset(w.ResponseWriter)
}

func (w *compressWriter) Write(payload []byte) (int, error) {
	if !w.decided {
		// A handler that writes without setting a type would otherwise look
		// uncompressible: detect it here exactly like net/http would.
		if w.Header().Get("Content-Type") == "" && len(payload) > 0 {
			w.Header().Set("Content-Type", http.DetectContentType(payload))
		}
		w.WriteHeader(http.StatusOK)
	}
	if w.gzip != nil {
		return w.gzip.Write(payload)
	}
	return w.ResponseWriter.Write(payload)
}

func (w *compressWriter) Flush() {
	if w.gzip != nil {
		_ = w.gzip.Flush()
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *compressWriter) close() {
	if w.gzip == nil {
		return
	}
	_ = w.gzip.Close()
	gzipWriterPool.Put(w.gzip)
	w.gzip = nil
}
