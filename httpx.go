package gextto

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// defaultHTTPClient is the shared client for provider/source requests. It
// follows the reqwest defaults used by gextto (redirects, gzip automatic).
var defaultHTTPClient = &http.Client{
	Timeout: 90 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if req.URL != nil && strings.EqualFold(req.URL.Scheme, "magnet") {
			return http.ErrUseLastResponse
		}
		return nil
	},
}

// Response size limits. They are intentionally generous: the goal is to avoid
// unbounded memory use from a misconfigured feed or a hostile server, not to
// cap legitimate content.
const (
	maxAPIResponseBytes  = 32 << 20  // JSON/API replies
	maxFeedResponseBytes = 64 << 20  // RSS/HTML feeds and .torrent files
	maxDecompressedBytes = 512 << 20 // decompressed archives (e.g. blocklists)
	maxUpdateBytes       = 512 << 20 // self-update payloads
	// torrentFetchReasonLimit bounds the reason carried from a failed
	// `.torrent` download into the log and the notification.
	torrentFetchReasonLimit = 300
)

// readLimitedBody reads at most limit bytes from r; a larger body is an error
// so a misconfigured or hostile response cannot exhaust memory.
func readLimitedBody(r io.Reader, limit int64) ([]byte, error) {
	return readLimitedBodyHint(r, limit, 0)
}

// readLimitedBodyHint is readLimitedBody with the size the client expects
// (an HTTP Content-Length). Without it the buffer grows by doubling, which for
// a multi-MB feed is most of the body copied several times. When the size is
// unknown (a chunked response) a 64 KiB start still saves most doublings. A
// hint larger than the limit is ignored: the limit still decides.
func readLimitedBodyHint(r io.Reader, limit, hint int64) ([]byte, error) {
	if hint <= 0 {
		if sized, ok := r.(interface{ Len() int }); ok {
			hint = int64(sized.Len())
		}
	}
	if hint <= 0 {
		hint = 64 << 10
	}
	if hint > limit {
		hint = limit
	}
	if hint < 0 {
		hint = 0
	}
	return readLimitedBodyInto(make([]byte, 0, int(hint)), r, limit)
}

// readLimitedBodyInto is readLimitedBody reusing buf, so a caller that reads
// several pages in a row does not allocate a fresh buffer for each one. buf is
// reset; its capacity is kept and grown only when the body needs it.
func readLimitedBodyInto(buf []byte, r io.Reader, limit int64) ([]byte, error) {
	buf = buf[:0]
	if cap(buf) == 0 {
		buf = make([]byte, 0, 64<<10)
	}
	for {
		if len(buf) == cap(buf) {
			next := cap(buf) * 2
			if int64(next) > limit+1 {
				next = int(limit) + 1
			}
			grown := make([]byte, len(buf), next)
			copy(grown, buf)
			buf = grown
		}
		n, err := r.Read(buf[len(buf):cap(buf)])
		buf = buf[:len(buf)+n]
		if int64(len(buf)) > limit {
			return nil, fmt.Errorf("response body exceeds the %d byte limit", limit)
		}
		if err == io.EOF {
			return buf, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// copyLimited streams src into dst, failing when the source is larger than
// limit. It never buffers the whole body.
func copyLimited(dst io.Writer, src io.Reader, limit int64) error {
	written, err := io.Copy(dst, io.LimitReader(src, limit+1))
	if err != nil {
		return err
	}
	if written > limit {
		return fmt.Errorf("response body exceeds the %d byte limit", limit)
	}
	return nil
}

// HTTPRequest performs an HTTP request with the given headers and body.
func HTTPRequest(ctx context.Context, method, rawURL string, headers map[string]string, body []byte, contentType string) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	return defaultHTTPClient.Do(request)
}

// HTTPGet performs a GET request.
func HTTPGet(ctx context.Context, rawURL string, headers map[string]string) (*http.Response, error) {
	return HTTPRequest(ctx, http.MethodGet, rawURL, headers, nil, "")
}

// HTTPGetBytes performs a GET and returns the body plus status code.
func HTTPGetBytes(ctx context.Context, rawURL string, headers map[string]string) ([]byte, int, error) {
	response, err := HTTPGet(ctx, rawURL, headers)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	payload, err := readLimitedBodyHint(response.Body, maxFeedResponseBytes, response.ContentLength)
	if err != nil {
		return nil, response.StatusCode, err
	}
	return payload, response.StatusCode, nil
}

// HTTPGetString performs a GET and returns the body as a string.
func HTTPGetString(ctx context.Context, rawURL string, headers map[string]string) (string, int, error) {
	payload, status, err := HTTPGetBytes(ctx, rawURL, headers)
	return string(payload), status, err
}

// HTTPPostJSON posts a JSON payload and returns the response body and status.
func HTTPPostJSON(ctx context.Context, rawURL string, headers map[string]string, payload []byte) ([]byte, int, error) {
	response, err := HTTPRequest(ctx, http.MethodPost, rawURL, headers, payload, "application/json")
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	body, err := readLimitedBodyHint(response.Body, maxAPIResponseBytes, response.ContentLength)
	if err != nil {
		return nil, response.StatusCode, err
	}
	return body, response.StatusCode, nil
}

// HTTPDownload streams a URL into a file writer, returning the byte count.
// The stream is capped so a misconfigured or hostile source cannot fill the
// target volume through an unbounded copy.
func HTTPDownload(ctx context.Context, rawURL string, headers map[string]string, writer io.Writer) (int64, error) {
	response, err := HTTPGet(ctx, rawURL, headers)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return 0, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	written, err := io.Copy(writer, io.LimitReader(response.Body, maxDecompressedBytes+1))
	if err != nil {
		return 0, err
	}
	if written > maxDecompressedBytes {
		return 0, fmt.Errorf("response body exceeds the %d byte limit", maxDecompressedBytes)
	}
	return written, nil
}
